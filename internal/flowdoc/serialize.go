// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

// Package flowdoc is FlowDoc, the interchange format of flow-as-code, ported
// one to one from @flow-as-code/core so the provider's plan-time `flowdoc`
// attribute is byte-identical to what the TypeScript side writes. A document
// is held as ordered JSON (package jsonv), not as structs, because the bytes
// are the contract.
package flowdoc

import (
	"github.com/flow-as-code/terraform-provider-flowascode/internal/jsonv"
)

// Version is the FlowDoc format version this provider reads and writes.
const Version = "0.2"

// FlowLanguageVersion is the Amazon Connect Flow language version.
const FlowLanguageVersion = "2019-10-30"

// ordered emits keys in the given order, then any other key in code-unit
// order, dropping nothing: serialize.ts's ordered().
func ordered(v jsonv.Object, keys ...string) jsonv.Object {
	out := jsonv.Object{}
	seen := map[string]bool{}
	for _, k := range keys {
		seen[k] = true
		if val, ok := v.Get(k); ok {
			out = append(out, jsonv.Member{Key: k, Value: val})
		}
	}
	var rest jsonv.Object
	for _, m := range v {
		if !seen[m.Key] {
			rest = append(rest, m)
		}
	}
	sorted := jsonv.SortKeys(rest)
	if sorted != nil {
		// SortKeys on a nil Object returns a nil Object; only the top level
		// is reordered here, so restore the members' own values.
		for _, m := range sorted.(jsonv.Object) {
			val, _ := rest.Get(m.Key)
			out = append(out, jsonv.Member{Key: m.Key, Value: val})
		}
	}
	return out
}

func asObject(v any) jsonv.Object {
	o, _ := v.(jsonv.Object)
	return o
}

func canonicalTransitions(t jsonv.Object) jsonv.Object {
	out := append(jsonv.Object{}, t...)
	if errs, ok := t.Get("Errors"); ok {
		if list, ok := errs.([]any); ok {
			next := make([]any, len(list))
			for i, e := range list {
				next[i] = ordered(asObject(e), "ErrorType", "NextAction")
			}
			out.Set("Errors", next)
		}
	}
	if conds, ok := t.Get("Conditions"); ok {
		if list, ok := conds.([]any); ok {
			next := make([]any, len(list))
			for i, c := range list {
				// { NextAction: c.NextAction, Condition: ordered({ ...c.Condition }) }:
				// an absent NextAction is dropped (undefined), a null one kept,
				// and Condition is always an object, {} when there is none.
				co := asObject(c)
				element := jsonv.Object{}
				if nextAction, ok := co.Get("NextAction"); ok {
					element = append(element, jsonv.Member{Key: "NextAction", Value: nextAction})
				}
				condition, _ := co.Get("Condition")
				element = append(element, jsonv.Member{
					Key:   "Condition",
					Value: ordered(asObject(condition), "Operator", "Operands"),
				})
				next[i] = element
			}
			out.Set("Conditions", next)
		}
	}
	return ordered(out, "NextAction", "Errors", "Conditions")
}

// CanonicalAction is serialize.ts's canonicalAction: four keys in a fixed
// order, Parameters with every key sorted, Transitions canonical.
func CanonicalAction(a jsonv.Object) jsonv.Object {
	// An absent key is undefined in the TypeScript object literal, which
	// JSON.stringify leaves out.
	out := jsonv.Object{}
	for _, k := range []string{"Identifier", "Type"} {
		if v, ok := a.Get(k); ok {
			out = append(out, jsonv.Member{Key: k, Value: v})
		}
	}
	if params, ok := a.Get("Parameters"); ok {
		out = append(out, jsonv.Member{Key: "Parameters", Value: jsonv.SortKeys(params)})
	}
	trans, _ := a.Get("Transitions")
	return append(out, jsonv.Member{Key: "Transitions", Value: canonicalTransitions(asObject(trans))})
}

// Canonicalize is serialize.ts's canonicalize. Key order below is the byte
// order of the output.
func Canonicalize(doc jsonv.Object) jsonv.Object {
	get := func(k string) any { v, _ := doc.Get(k); return v }
	out := jsonv.Object{
		{Key: "flowdoc", Value: get("flowdoc")},
		{Key: "kind", Value: get("kind")},
		{Key: "name", Value: get("name")},
	}
	if d, ok := doc.Get("description"); ok {
		out = append(out, jsonv.Member{Key: "description", Value: d})
	}
	out = append(out, jsonv.Member{Key: "connectType", Value: get("connectType")})

	content := asObject(get("content"))
	c := jsonv.Object{}
	for _, m := range content {
		switch m.Key {
		case "Settings", "Metadata":
			c = append(c, jsonv.Member{Key: m.Key, Value: jsonv.SortKeys(m.Value)})
		case "Actions":
			list, _ := m.Value.([]any)
			actions := make([]any, len(list))
			for i, a := range list {
				actions[i] = CanonicalAction(asObject(a))
			}
			c = append(c, jsonv.Member{Key: "Actions", Value: actions})
		default:
			c = append(c, m)
		}
	}
	out = append(out, jsonv.Member{
		Key:   "content",
		Value: ordered(c, "Version", "StartAction", "Settings", "Metadata", "Actions"),
	})
	if l, ok := doc.Get("layout"); ok {
		out = append(out, jsonv.Member{Key: "layout", Value: jsonv.SortKeys(l)})
	}
	if r, ok := doc.Get("refs"); ok {
		list, _ := r.([]any)
		refs := make([]any, len(list))
		for i, e := range list {
			refs[i] = ordered(asObject(e), "token", "type", "name", "alias")
		}
		out = append(out, jsonv.Member{Key: "refs", Value: refs})
	}
	if m, ok := doc.Get("meta"); ok {
		out = append(out, jsonv.Member{Key: "meta", Value: jsonv.SortKeys(m)})
	}
	return out
}

// Serialize is serialize.ts's serialize: canonical JSON, two-space indent,
// newline terminated.
func Serialize(doc jsonv.Object) []byte {
	return append(jsonv.Encode(Canonicalize(doc), "  "), '\n')
}
