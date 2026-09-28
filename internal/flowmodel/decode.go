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
