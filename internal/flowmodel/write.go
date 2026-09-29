// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package flowmodel

import (
	"encoding/json"
	"math"
	"math/big"
	"strings"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/flowdoc"
	"github.com/flow-as-code/terraform-provider-flowascode/internal/jsonv"
)

// ActionsFromDoc is a FlowDoc's actions as the `action` block values the
// schema holds (rules 9 to 15, the provider's half of the writer): a typed
// sub-block where the catalog's shape accepts the Parameters, generic
// otherwise; conditions and errors in order; a position only where it
// differs from the layout. Values are plain Go (string, *big.Float, bool,
// []any, map[string]any, nil), for ToTerraform. It is what Read writes back
// after drift or an import, so the plan shows the live flow as blocks.
func ActionsFromDoc(doc jsonv.Object) []any {
	cv, _ := doc.Get("content")
	content, _ := cv.(jsonv.Object)
	av, _ := content.Get("Actions")
	actions, _ := av.([]any)
	sv, _ := content.Get("StartAction")
	start, _ := sv.(string)
	auto := flowdoc.AutoLayout(actions, &start)
	var layout jsonv.Object
	if lv, ok := doc.Get("layout"); ok {
		layout, _ = lv.(jsonv.Object)
	}
	out := make([]any, 0, len(actions))
	for _, raw := range actions {
		a, _ := raw.(jsonv.Object)
		out = append(out, actionValue(a, layout, auto))
	}
	return out
}

func actionValue(a jsonv.Object, layout jsonv.Object, auto map[string]flowdoc.Point) map[string]any {
	idv, _ := a.Get("Identifier")
	id, _ := idv.(string)
	tv, _ := a.Get("Type")
	typ, _ := tv.(string)
	pv, _ := a.Get("Parameters")
	params, _ := pv.(jsonv.Object)
	out := map[string]any{"id": id}

	entry := flowdoc.ModeledEntry(typ)
	if entry != nil && Accepts(params, entry.Parameters) {
		block := map[string]any{}
		byKey := map[string]flowdoc.CatalogParameter{}
		for _, p := range entry.Parameters {
			byKey[p.Key] = p
		}
		for _, m := range params {
			p := byKey[m.Key]
			block[p.Attr] = attrValue(m.Value, p.CatalogElement)
		}
		out[entry.Block] = block
	} else {
		generic := map[string]any{"type": typ}
		if len(params) > 0 {
			generic["parameters"] = JSONEncode(params)
		}
		out["generic"] = generic
	}

	trv, _ := a.Get("Transitions")
	t, _ := trv.(jsonv.Object)
	if n, ok := t.Get("NextAction"); ok {
		if s, ok := n.(string); ok {
			out["next"] = s
		}
	}
	if cv, ok := t.Get("Conditions"); ok {
		list, _ := cv.([]any)
		conds := []any{}
		for _, c := range list {
			co, _ := c.(jsonv.Object)
			next, _ := co.Get("NextAction")
			condv, _ := co.Get("Condition")
			cond, _ := condv.(jsonv.Object)
			op, _ := cond.Get("Operator")
			ops, _ := cond.Get("Operands")
			operands := []any{}
			if list, ok := ops.([]any); ok {
				for _, o := range list {
					operands = append(operands, stringOf(o))
				}
			}
			conds = append(conds, map[string]any{"operator": op, "operands": operands, "next": next})
		}
		if len(conds) > 0 {
			out["condition"] = conds
		}
	}
	if ev, ok := t.Get("Errors"); ok {
		list, _ := ev.([]any)
		errs := []any{}
		for _, e := range list {
			eo, _ := e.(jsonv.Object)
			et, _ := eo.Get("ErrorType")
			next, _ := eo.Get("NextAction")
			errs = append(errs, map[string]any{"type": et, "next": next})
		}
		if len(errs) > 0 {
			out["error"] = errs
		}
	}
	if pv, ok := layout.Get(id); ok {
		if po, ok := pv.(jsonv.Object); ok {
			xv, _ := po.Get("x")
			yv, _ := po.Get("y")
			x, _ := xv.(float64)
			y, _ := yv.(float64)
			x, y = JSRound(x), JSRound(y)
			if p, ok := auto[id]; !ok || p.X != x || p.Y != y {
				out["position"] = map[string]any{"x": big.NewFloat(x), "y": big.NewFloat(y)}
			}
		}
	}
	return out
}

// stringOf is a Flow language operand as the provider holds it: a string;
// a number as JavaScript prints it.
func stringOf(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return jsonv.FormatNumber(t)
	}
	return string(jsonv.Encode(v, ""))
}

// attrValue is one parameter or field as its attribute holds it (rule 11).
func attrValue(v any, e flowdoc.CatalogElement) any {
	switch e.Kind {
	case "ref":
		s, _ := v.(string)
		if entry, ok := flowdoc.ParseToken(s); ok {
			return flowdoc.RefKey(entry)
		}
		return s
	case "integer":
		f, _ := v.(float64)
		return big.NewFloat(f)
	case "json":
		return JSONEncode(v)
	case "object":
		o, _ := v.(jsonv.Object)
		out := map[string]any{}
		byKey := map[string]flowdoc.CatalogParameter{}
		for _, f := range e.Fields {
			byKey[f.Key] = f
		}
		for _, m := range o {
			f := byKey[m.Key]
			out[f.Attr] = attrValue(m.Value, f.CatalogElement)
		}
		return out
	case "list":
		list, _ := v.([]any)
		out := make([]any, len(list))
		for i, item := range list {
			of := flowdoc.CatalogElement{Kind: "string"}
			if e.Of != nil {
				of = *e.Of
			}
			out[i] = attrValue(item, of)
		}
		return out
	case "map":
		o, _ := v.(jsonv.Object)
		out := map[string]any{}
		for _, m := range o {
			of := flowdoc.CatalogElement{Kind: "string"}
			if e.Of != nil {
				of = *e.Of
			}
			out[m.Key] = attrValue(m.Value, of)
		}
		return out
	default:
		return v
	}
}

// Accepts is the TypeScript writer's accepts (packages/hcl/src/write.ts,
// rule 10): whether the catalog's shape takes these Parameters, so the
// action is written typed rather than generic.
func Accepts(params jsonv.Object, elems []flowdoc.CatalogParameter) bool {
	byKey := map[string]flowdoc.CatalogParameter{}
	for _, p := range elems {
		byKey[p.Key] = p
	}
	for _, m := range params {
		e, ok := byKey[m.Key]
		if !ok || !acceptsValue(m.Value, e.CatalogElement) {
			return false
		}
	}
	for _, e := range elems {
		if _, ok := params.Get(e.Key); e.Required && !ok {
			return false
		}
	}
	return true
}

func acceptsValue(v any, e flowdoc.CatalogElement) bool {
	switch e.Kind {
	case "object":
		o, ok := v.(jsonv.Object)
		return ok && Accepts(o, e.Fields)
	case "list":
		list, ok := v.([]any)
		if !ok {
			return false
		}
		if e.Of == nil {
			return true
		}
		for _, x := range list {
			if !acceptsValue(x, *e.Of) {
				return false
			}
		}
		return true
	case "map":
		o, ok := v.(jsonv.Object)
		if !ok {
			return false
		}
		for _, m := range o {
			if e.Of == nil {
				if _, ok := m.Value.(string); !ok {
					return false
				}
			} else if !acceptsValue(m.Value, *e.Of) {
				return false
			}
		}
		return true
	case "integer":
		_, ok := v.(float64)
		return ok
	case "json":
		_, ok := v.(jsonv.Object)
		return ok
	case "ref":
		s, ok := v.(string)
		if !ok {
			return false
		}
		if strings.HasPrefix(s, "$.") {
			return true
		}
		entry, ok := flowdoc.ParseToken(s)
		return ok && entry.Type == e.Ref
	default:
		_, ok := v.(string)
		return ok
	}
}

// JSONEncode is what Terraform's jsonencode writes for a value: compact, keys
// sorted, and <, >, & escaped as encoding/json escapes them, so a value Read
// writes back equals the one the configuration evaluates to.
// https://developer.hashicorp.com/terraform/language/functions/jsonencode
func JSONEncode(v any) string {
	b, _ := json.Marshal(plain(v))
	return string(b)
}

// plain turns jsonv values into what encoding/json sorts and writes.
func plain(v any) any {
	switch t := v.(type) {
	case jsonv.Object:
		m := make(map[string]any, len(t))
		for _, x := range t {
			m[x.Key] = plain(x.Value)
		}
		return m
	case []any:
		out := make([]any, len(t))
		for i, x := range t {
			out[i] = plain(x)
		}
		return out
	case float64:
		return json.Number(jsonv.FormatNumber(t))
	default:
		return v
	}
}

// JSRound is JavaScript's Math.round, which the TypeScript writer uses: half
// rounds toward +Infinity (-40.5 to -40), where math.Round rounds away from
// zero (-41). The fraction v-floor(v) is exact, so no addition can round
// 0.49999999999999994 up the way floor(v+0.5) does.
func JSRound(v float64) float64 {
	r := math.Floor(v)
	if v-r >= 0.5 {
		r++
	}
	return r
}
