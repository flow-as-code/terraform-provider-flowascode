// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package flowmodel

import (
	"fmt"
	"math/big"

	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// Unknown stands for a value Terraform has not computed yet: an expression
// over a resource that does not exist, or (in the validate walk) any
// variable or reference.
type Unknown struct{}

// Decode turns a Terraform value into plain Go: nil for null, Unknown for an
// unknown value, string, *big.Float for a number, bool, []any for a list,
// set or tuple, and map[string]any for a map or object.
func Decode(v tftypes.Value) (any, error) {
	if !v.IsKnown() {
		return Unknown{}, nil
	}
	if v.IsNull() {
		return nil, nil
	}
	t := v.Type()
	switch {
	case t.Is(tftypes.String):
		var s string
		err := v.As(&s)
		return s, err
	case t.Is(tftypes.Number):
		var n big.Float
		err := v.As(&n)
		return &n, err
	case t.Is(tftypes.Bool):
		var b bool
		err := v.As(&b)
		return b, err
	case t.Is(tftypes.List{}), t.Is(tftypes.Set{}), t.Is(tftypes.Tuple{}):
		var items []tftypes.Value
		if err := v.As(&items); err != nil {
			return nil, err
		}
		out := make([]any, len(items))
		for i, item := range items {
			d, err := Decode(item)
			if err != nil {
				return nil, err
			}
			out[i] = d
		}
		return out, nil
	case t.Is(tftypes.Map{}), t.Is(tftypes.Object{}):
		var items map[string]tftypes.Value
		if err := v.As(&items); err != nil {
			return nil, err
		}
		out := make(map[string]any, len(items))
		for k, item := range items {
			d, err := Decode(item)
			if err != nil {
				return nil, err
			}
			out[k] = d
		}
		return out, nil
	case t.Is(tftypes.DynamicPseudoType):
		return nil, fmt.Errorf("flowmodel: a dynamic value with no concrete type")
	}
	return nil, fmt.Errorf("flowmodel: cannot decode a %s", t)
}

// ToTerraform is Decode's inverse, guided by the type: plain Go values (nil,
// string, *big.Float or float64, bool, []any, map[string]any) as a Terraform
// value of type t, every attribute of an object the value lacks set null.
func ToTerraform(v any, t tftypes.Type) (tftypes.Value, error) {
	if v == nil {
		return tftypes.NewValue(t, nil), nil
	}
	switch {
	case t.Is(tftypes.String):
		s, ok := v.(string)
		if !ok {
			return tftypes.Value{}, fmt.Errorf("flowmodel: want a string, got %T", v)
		}
		return tftypes.NewValue(t, s), nil
	case t.Is(tftypes.Number):
		switch n := v.(type) {
		case *big.Float:
			return tftypes.NewValue(t, n), nil
		case float64:
			return tftypes.NewValue(t, big.NewFloat(n)), nil
		}
		return tftypes.Value{}, fmt.Errorf("flowmodel: want a number, got %T", v)
	case t.Is(tftypes.Bool):
		return tftypes.NewValue(t, v), nil
	case t.Is(tftypes.List{}):
		list, ok := v.([]any)
		if !ok {
			return tftypes.Value{}, fmt.Errorf("flowmodel: want a list, got %T", v)
		}
		elem := t.(tftypes.List).ElementType
		items := make([]tftypes.Value, len(list))
		for i, item := range list {
			tv, err := ToTerraform(item, elem)
			if err != nil {
				return tftypes.Value{}, err
			}
			items[i] = tv
		}
		return tftypes.NewValue(t, items), nil
	case t.Is(tftypes.Map{}):
		m, ok := v.(map[string]any)
		if !ok {
			return tftypes.Value{}, fmt.Errorf("flowmodel: want a map, got %T", v)
		}
		elem := t.(tftypes.Map).ElementType
		items := make(map[string]tftypes.Value, len(m))
		for k, item := range m {
			tv, err := ToTerraform(item, elem)
			if err != nil {
				return tftypes.Value{}, err
			}
			items[k] = tv
		}
		return tftypes.NewValue(t, items), nil
	case t.Is(tftypes.Object{}):
		m, ok := v.(map[string]any)
		if !ok {
			return tftypes.Value{}, fmt.Errorf("flowmodel: want an object, got %T", v)
		}
		attrs := t.(tftypes.Object).AttributeTypes
		items := make(map[string]tftypes.Value, len(attrs))
		for k, at := range attrs {
			tv, err := ToTerraform(m[k], at)
			if err != nil {
				return tftypes.Value{}, fmt.Errorf("%s: %w", k, err)
			}
			items[k] = tv
		}
		for k := range m {
			if _, ok := attrs[k]; !ok {
				return tftypes.Value{}, fmt.Errorf("flowmodel: no attribute %q in the schema", k)
			}
		}
		return tftypes.NewValue(t, items), nil
	}
	return tftypes.Value{}, fmt.Errorf("flowmodel: cannot build a %s", t)
}
