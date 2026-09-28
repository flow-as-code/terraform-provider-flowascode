// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package export

import (
	"unicode/utf16"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/jsonv"
)

// JavaScript object semantics export.ts leans on without saying so. Private
// copies of internal/flowdoc's jsKeyOrder and jsOrdered, which that package
// does not export.

// isArrayIndex reports whether a property key is an array index, which
// ordinary objects enumerate first, ascending: the canonical decimal form of
// an integer from 0 to 2^32 - 2.
// https://tc39.es/ecma262/#sec-ordinaryownpropertykeys
func isArrayIndex(key string) bool {
	if key == "" || len(key) > 10 {
		return false
	}
	if key != "0" && key[0] == '0' {
		return false
	}
	n := uint64(0)
	for i := 0; i < len(key); i++ {
		c := key[i]
		if c < '0' || c > '9' {
			return false
		}
		n = n*10 + uint64(c-'0')
	}
	return n <= 4294967294
}

// jsKeyOrder is the order Object.keys, Object.entries and JSON.stringify give
// an object whose properties were created in the given order.
func jsKeyOrder(keys []string) []string {
	var indices, rest []string
	for _, k := range keys {
		if isArrayIndex(k) {
			indices = append(indices, k)
		} else {
			rest = append(rest, k)
		}
	}
	for i := 1; i < len(indices); i++ {
		for j := i; j > 0 && numericLess(indices[j], indices[j-1]); j-- {
			indices[j], indices[j-1] = indices[j-1], indices[j]
		}
	}
	return append(indices, rest...)
}

func numericLess(a, b string) bool {
	if len(a) != len(b) {
		return len(a) < len(b)
	}
	return a < b
}

// jsOrderedObject returns o's members in JavaScript's own-key order, values
// untouched.
func jsOrderedObject(o jsonv.Object) jsonv.Object {
	out := make(jsonv.Object, 0, len(o))
	for _, k := range jsKeyOrder(o.Keys()) {
		v, _ := o.Get(k)
		out = append(out, jsonv.Member{Key: k, Value: v})
	}
	return out
}

// jsOrdered is v with every object in JavaScript's own-key order, which is
// what JSON.stringify writes for a value JSON.parse produced.
func jsOrdered(v any) any {
	switch t := v.(type) {
	case []any:
		out := make([]any, len(t))
		for i, item := range t {
			out[i] = jsOrdered(item)
		}
		return out
	case jsonv.Object:
		out := jsOrderedObject(t)
		for i := range out {
			out[i].Value = jsOrdered(out[i].Value)
		}
		return out
	default:
		return v
	}
}

// stringify is JSON.stringify(v) for a value JSON.parse produced; absent
// (JavaScript undefined) is the string "undefined", which is what a template
// literal makes of JSON.stringify(undefined).
func stringify(v any, present bool) string {
	if !present {
		return "undefined"
	}
	return string(jsonv.Encode(jsOrdered(v), ""))
}

// spread is `{ ...value }` for a value that is not a plain object: an array
// or a string contributes its indices, anything else nothing. A string's
// elements are UTF-16 code units; a lone surrogate half, which a Go string
// cannot hold, becomes U+FFFD.
func spread(value any) jsonv.Object {
	out := jsonv.Object{}
	switch t := value.(type) {
	case jsonv.Object:
		out = append(out, jsOrderedObject(t)...)
	case []any:
		for i, item := range t {
			out = append(out, jsonv.Member{Key: itoa(i), Value: item})
		}
	case string:
		for i, unit := range utf16.Encode([]rune(t)) {
			out = append(out, jsonv.Member{Key: itoa(i), Value: string(utf16.Decode([]uint16{unit}))})
		}
	}
	return out
}

func itoa(i int) string {
	return jsonv.FormatNumber(float64(i))
}

// objectPrototypeKeys are the properties every plain object inherits from
// Object.prototype. Reading one that is not an own property returns the
// inherited value, which is never nullish, so `record[id] ?? fallback`
// never reaches the fallback for these, and JSON.stringify drops the
// function it stored. `__proto__` is also one: assigning to it sets the
// prototype and creates no own property at all.
// https://tc39.es/ecma262/#sec-properties-of-the-object-prototype-object
var objectPrototypeKeys = map[string]bool{
	"constructor":          true,
	"__defineGetter__":     true,
	"__defineSetter__":     true,
	"hasOwnProperty":       true,
	"__lookupGetter__":     true,
	"__lookupSetter__":     true,
	"isPrototypeOf":        true,
	"propertyIsEnumerable": true,
	"toString":             true,
	"valueOf":              true,
	"__proto__":            true,
	"toLocaleString":       true,
}

// propertyKey is ToPropertyKey for a JSON value used as a computed key:
// `record[action.Identifier]`.
func propertyKey(v any, present bool) string {
	if !present {
		return "undefined"
	}
	switch t := v.(type) {
	case nil:
		return "null"
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	case float64:
		return jsonv.FormatNumber(t)
	case []any:
		// Array.prototype.join: null and undefined elements are empty.
		s := ""
		for i, item := range t {
			if i > 0 {
				s += ","
			}
			if item != nil {
				s += propertyKey(item, true)
			}
		}
		return s
	default:
		return "[object Object]"
	}
}
