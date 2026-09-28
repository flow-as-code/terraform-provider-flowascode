// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package materialize

import (
	"sort"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/jsonv"
)

// ordered is serialize.ts's ordered(value, keys): the given keys first, then
// every other own key of value sorted with Array.prototype.sort's default
// comparison (UTF-16 code unit order, jsonv.LessUTF16). The TypeScript skips
// an undefined value; here it is kept and finish drops it, which writes the
// same bytes.
// It is a private copy rather than internal/flowdoc's because the TypeScript
// reads value's own keys however value is shaped (an array's indices, a
// string's code units), which the caller's spread already gives here.
func ordered(value jsonv.Object, keys ...string) jsonv.Object {
	out := jsonv.Object{}
	fixed := map[string]bool{}
	for _, k := range keys {
		fixed[k] = true
		if v, ok := value.Get(k); ok {
			out = append(out, jsonv.Member{Key: k, Value: v})
		}
	}
	rest := jsKeyOrder(value.Keys())
	sort.SliceStable(rest, func(i, j int) bool { return jsonv.LessUTF16(rest[i], rest[j]) })
	for _, k := range rest {
		if !fixed[k] {
			v, _ := value.Get(k)
			out = append(out, jsonv.Member{Key: k, Value: v})
		}
	}
	return out
}

func isUndefined(v any) bool {
	_, ok := v.(undefinedValue)
	return ok
}

// sortKeys is serialize.ts's sortKeys: every object's keys sorted with <
// (jsonv.LessUTF16), arrays in order. finish later puts array-index keys
// first, as Object.fromEntries does.
func sortKeys(v any) any {
	if isUndefined(v) {
		return v
	}
	return jsonv.SortKeys(v)
}

// mapList is `value.map(fn)` for the property read `<expr>` of an object:
// undefined and null throw the read error, any other non-array throws
// "<expr>.map is not a function".
func mapList(value any, expr string, fn func(any) (any, error)) ([]any, error) {
	switch t := value.(type) {
	case undefinedValue:
		return nil, readError("undefined", "map")
	case nil:
		return nil, readError("null", "map")
	case []any:
		out := make([]any, len(t))
		for i, item := range t {
			v, err := fn(item)
			if err != nil {
				return nil, err
			}
			out[i] = v
		}
		return out, nil
	}
	return nil, &TypeError{Message: expr + ".map is not a function"}
}

// canonicalTransitions is serialize.ts's canonicalTransitions: Errors and
// Conditions elements put in a fixed key order too, so two authorings of the
// same flow serialize to the same bytes. It is ported here rather than
// reused from internal/flowdoc's CanonicalAction, which is lenient where the
// TypeScript throws and omits a Conditions element's Condition (or a null
// NextAction) where the TypeScript writes {} (or null).
func canonicalTransitions(t any) (jsonv.Object, error) {
	canonical := spread(t)
	switch t.(type) {
	case undefinedValue:
		return nil, readError("undefined", "Errors")
	case nil:
		return nil, readError("null", "Errors")
	}
	if errs := get(t, "Errors"); !isUndefined(errs) {
		list, err := mapList(errs, "t.Errors", func(e any) (any, error) {
			return ordered(spread(e), "ErrorType", "NextAction"), nil
		})
		if err != nil {
			return nil, err
		}
		canonical.Set("Errors", list)
	}
	if conds := get(t, "Conditions"); !isUndefined(conds) {
		list, err := mapList(conds, "t.Conditions", func(c any) (any, error) {
			if c == nil {
				return nil, readError("null", "NextAction")
			}
			return ordered(jsonv.Object{
				{Key: "NextAction", Value: get(c, "NextAction")},
				{Key: "Condition", Value: ordered(spread(get(c, "Condition")), "Operator", "Operands")},
			}, "NextAction", "Condition"), nil
		})
		if err != nil {
			return nil, err
		}
		canonical.Set("Conditions", list)
	}
	return ordered(canonical, "NextAction", "Errors", "Conditions"), nil
}

// canonicalAction is serialize.ts's canonicalAction: four keys in a fixed
// order, Parameters with every key sorted, Transitions canonical.
func canonicalAction(a any) (any, error) {
	if a == nil {
		return nil, readError("null", "Identifier")
	}
	transitions, err := canonicalTransitions(get(a, "Transitions"))
	if err != nil {
		return nil, err
	}
	return jsonv.Object{
		{Key: "Identifier", Value: get(a, "Identifier")},
		{Key: "Type", Value: get(a, "Type")},
		{Key: "Parameters", Value: sortKeys(get(a, "Parameters"))},
		{Key: "Transitions", Value: transitions},
	}, nil
}

// SerializeContent is materialize.ts's serializeContent: the canonical JSON
// text of deployable content, two-space indented, newline terminated.
// Version, StartAction, Settings, Metadata and Actions in that order (any
// other top-level key is dropped); Metadata leads with EntryPointPosition
// then ActionMetadata, matching the Flow language example; everything nested
// is key-sorted. The bytes are the TypeScript's for the same content, and so
// is the refusal of a shape it cannot serialize: a *TypeError with V8's
// message, where the TypeScript throws one.
func SerializeContent(content jsonv.Object) ([]byte, error) {
	top := jsonv.Object{
		{Key: "Version", Value: member(content, "Version")},
		{Key: "StartAction", Value: member(content, "StartAction")},
	}
	if settings, ok := content.Get("Settings"); ok {
		top = append(top, jsonv.Member{Key: "Settings", Value: sortKeys(settings)})
	}
	if metadata, ok := content.Get("Metadata"); ok {
		if metadata == nil {
			return nil, readError("null", "EntryPointPosition")
		}
		top = append(top, jsonv.Member{
			Key:   "Metadata",
			Value: ordered(spread(sortKeys(metadata)), "EntryPointPosition", "ActionMetadata"),
		})
	}
	actions, err := mapList(member(content, "Actions"), "content.Actions", canonicalAction)
	if err != nil {
		return nil, err
	}
	top = append(top, jsonv.Member{Key: "Actions", Value: actions})
	canonical := ordered(top, "Version", "StartAction", "Settings", "Metadata", "Actions")
	return append(jsonv.Encode(finish(canonical), "  "), '\n'), nil
}
