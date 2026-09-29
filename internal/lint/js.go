// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package lint

import (
	"strings"
	"unicode/utf16"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/jsonv"
)

// JavaScript semantics the TypeScript rules lean on without saying so. Each
// helper names the built-in it reproduces.

// undefinedValue is JavaScript's undefined where a rule can observe it: a
// transition target read from an element that has no NextAction. A jsonv
// value is never this type, so it cannot collide with a document's null.
type undefinedValue struct{}

var undefined = undefinedValue{}

// jsString is String(value), which a template literal applies to each
// substitution: undefined, null, booleans and numbers as JavaScript prints
// them, an array joined with "," (null and undefined elements empty), and
// any other object "[object Object]".
func jsString(v any) string {
	switch t := v.(type) {
	case undefinedValue:
		return "undefined"
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
		// Number.prototype.toString and JSON.stringify agree on every finite
		// number, and a parsed document holds no other kind.
		return jsonv.FormatNumber(t)
	case int:
		return jsonv.FormatNumber(float64(t))
	case []any:
		parts := make([]string, len(t))
		for i, item := range t {
			if item == nil {
				continue
			}
			if _, ok := item.(undefinedValue); ok {
				continue
			}
			parts[i] = jsString(item)
		}
		return strings.Join(parts, ",")
	default:
		return "[object Object]"
	}
}

// jsLength is String.prototype.length: UTF-16 code units, so a character
// outside the Basic Multilingual Plane counts two.
func jsLength(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}

// isJSSpace is the set String.prototype.trim strips and the regular
// expression class \s matches: WhiteSpace plus LineTerminator.
// https://tc39.es/ecma262/#sec-white-space
// https://tc39.es/ecma262/#sec-line-terminators
// Go's unicode.IsSpace differs on two: it takes U+0085 and leaves U+FEFF.
func isJSSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0x00a0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

// jsSpaceClass is isJSSpace as the body of a regexp character class, for
// translating a JavaScript \s. A test holds the two equal.
const jsSpaceClass = `\t\n\v\f\r \x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}`

// jsTrim is String.prototype.trim.
func jsTrim(s string) string { return strings.TrimFunc(s, isJSSpace) }

// isArrayIndex reports whether a property key is an array index, which an
// ordinary JavaScript object enumerates first, in ascending numeric order:
// the canonical decimal form of an integer from 0 to 2^32 - 2.
// https://tc39.es/ecma262/#sec-ordinaryownpropertykeys
// A private copy of internal/flowdoc's.
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

// entries is Object.entries(o): array-index keys first in ascending numeric
// order, then every other key in creation order. jsonv keeps creation order
// throughout, which differs exactly when a key is an array index.
func entries(o jsonv.Object) []jsonv.Member {
	var indices, rest []jsonv.Member
	for _, m := range o {
		if isArrayIndex(m.Key) {
			indices = append(indices, m)
		} else {
			rest = append(rest, m)
		}
	}
	// Insertion sort: array indices have no leading zeros, so shorter is
	// smaller and equal lengths compare bytewise.
	for i := 1; i < len(indices); i++ {
		for j := i; j > 0 && numericLess(indices[j].Key, indices[j-1].Key); j-- {
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

// lengthAt is `(o[key] ?? []).length` compared with a number: an array's
// length, a string's length in UTF-16 code units, and for anything else
// undefined, which no comparison holds for (ok false).
func lengthAt(o jsonv.Object, key string) (int, bool) {
	v, has := o.Get(key)
	if !has || v == nil {
		return 0, true
	}
	switch t := v.(type) {
	case []any:
		return len(t), true
	case string:
		return jsLength(t), true
	default:
		return 0, false
	}
}
