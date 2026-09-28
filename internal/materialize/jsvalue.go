// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package materialize

import (
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/flowdoc"
	"github.com/flow-as-code/terraform-provider-flowascode/internal/jsonv"
)

// materialize.ts and serialize.ts read documents that only assertFlowDoc has
// checked, which is shallow: layout, Metadata, Settings and the contents of
// Transitions can hold any JSON. What the TypeScript then does with them is
// JavaScript's property semantics, so this file models the small part of
// them those two files reach: undefined, own and inherited properties, the
// object spread, and ToNumber. A value is a jsonv value (nil, bool, float64,
// string, []any, jsonv.Object) or one of the sentinels below.

// undefinedValue is JavaScript's undefined where a value can hold it: a map
// lookup that found nothing, or a property that is not there. JSON.stringify
// omits it as an object member and writes it as null in an array; finish
// does the same.
type undefinedValue struct{}

// inheritedFunction is a function a JSON value inherits from a prototype,
// such as ({}).constructor or ({}).toString. Reading x or y from it gives
// undefined, and it is an object for Object.setPrototypeOf.
type inheritedFunction struct{}

// objectPrototype is Object.prototype itself, which ({}).__proto__ reads.
// Spreading it copies nothing: it has no own enumerable properties.
type objectPrototype struct{}

var undefined = undefinedValue{}

// objectPrototypeFunctions are the function-valued properties of
// Object.prototype (Object.getOwnPropertyNames(Object.prototype) less
// __proto__, an accessor), which every JSON value inherits.
var objectPrototypeFunctions = map[string]bool{
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
	"toLocaleString":       true,
}

// inherited is what reading key from a JSON value finds when the value has no
// own property of that name. Only Object.prototype's members are modeled;
// Array.prototype's and String.prototype's (map, slice and the rest) are not,
// and read as undefined here.
func inherited(key string) any {
	if key == "__proto__" {
		return objectPrototype{}
	}
	if objectPrototypeFunctions[key] {
		return inheritedFunction{}
	}
	return undefined
}

// get is the property read value[key] for a value that is neither null nor
// undefined, which JavaScript would throw on and every caller rules out.
func get(value any, key string) any {
	switch t := value.(type) {
	case jsonv.Object:
		if v, ok := t.Get(key); ok {
			return v
		}
	case []any:
		if i, ok := arrayIndex(key); ok && i < len(t) {
			return t[i]
		}
		if key == "length" {
			return float64(len(t))
		}
	case string:
		units := utf16.Encode([]rune(t))
		if i, ok := arrayIndex(key); ok && i < len(units) {
			return unitString(units[i])
		}
		if key == "length" {
			return float64(len(units))
		}
	case flowdoc.Point:
		switch key {
		case "x":
			return t.X
		case "y":
			return t.Y
		}
	case objectPrototype:
		if key == "__proto__" {
			return nil
		}
	}
	return inherited(key)
}

// nullish is `v === null || v === undefined`, what ?? tests.
func nullish(v any) bool {
	switch v.(type) {
	case nil, undefinedValue:
		return true
	}
	return false
}

// isRecord is `v !== null && typeof v === "object" && !Array.isArray(v)`.
func isRecord(v any) bool {
	switch v.(type) {
	case jsonv.Object, objectPrototype, flowdoc.Point:
		return true
	}
	return false
}

// isObject is whether v is an object (or function) for JavaScript, which is
// what Object.setPrototypeOf accepts; a primitive is ignored by the
// __proto__ setter.
func isObject(v any) bool {
	switch v.(type) {
	case jsonv.Object, []any, objectPrototype, inheritedFunction, flowdoc.Point:
		return true
	}
	return false
}

// spread is the object spread {...v}: v's own enumerable properties, copied
// in JavaScript's own-key order. null and undefined copy nothing, as do
// numbers, booleans and functions. An array copies its indices and a string
// its UTF-16 code units.
func spread(v any) jsonv.Object {
	out := jsonv.Object{}
	switch t := v.(type) {
	case jsonv.Object:
		for _, k := range jsKeyOrder(t.Keys()) {
			val, _ := t.Get(k)
			out = append(out, jsonv.Member{Key: k, Value: val})
		}
	case []any:
		for i, item := range t {
			out = append(out, jsonv.Member{Key: strconv.Itoa(i), Value: item})
		}
	case string:
		for i, u := range utf16.Encode([]rune(t)) {
			out = append(out, jsonv.Member{Key: strconv.Itoa(i), Value: unitString(u)})
		}
	case flowdoc.Point:
		out = jsonv.Object{{Key: "x", Value: t.X}, {Key: "y", Value: t.Y}}
	}
	return out
}

// unitString is a one-code-unit JavaScript string. A lone surrogate half is
// written in its three-byte generalized UTF-8 form, which Go can hold but
// jsonv.Encode writes raw where JSON.stringify writes a \udXXX escape.
func unitString(u uint16) string {
	if utf16.IsSurrogate(rune(u)) {
		return string([]byte{0xe0 | byte(u>>12), 0x80 | byte(u>>6)&0x3f, 0x80 | byte(u)&0x3f})
	}
	return string(rune(u))
}

// arrayIndex parses a canonical array index (no sign, no leading zero, below
// 2^32 - 1).
func arrayIndex(key string) (int, bool) {
	if !isArrayIndex(key) {
		return 0, false
	}
	n, err := strconv.Atoi(key)
	return n, err == nil
}

// isArrayIndex reports whether a property key is an array index, which
// ordinary objects enumerate first, ascending: the canonical decimal form of
// an integer from 0 to 2^32 - 2. A private copy of internal/flowdoc's.
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

// jsKeyOrder is the order Object.keys and JSON.stringify give an object whose
// properties were created in the given order: array indices ascending, then
// every other key in creation order.
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

// finish is the value as JSON.stringify sees it: every object's members in
// JavaScript's own-key order, an undefined member dropped, an undefined array
// element written as null, and a Point as {x, y}. Every object the
// TypeScript builds is an ordinary object, so its enumeration order is
// jsKeyOrder over the order the Go code created its keys in.
func finish(v any) any {
	switch t := v.(type) {
	case []any:
		out := make([]any, len(t))
		for i, item := range t {
			if _, isUndefined := item.(undefinedValue); isUndefined {
				out[i] = nil
				continue
			}
			out[i] = finish(item)
		}
		return out
	case jsonv.Object:
		out := jsonv.Object{}
		for _, k := range jsKeyOrder(t.Keys()) {
			val, _ := t.Get(k)
			if _, isUndefined := val.(undefinedValue); isUndefined {
				continue
			}
			out = append(out, jsonv.Member{Key: k, Value: finish(val)})
		}
		return out
	case flowdoc.Point:
		return jsonv.Object{{Key: "x", Value: t.X}, {Key: "y", Value: t.Y}}
	default:
		return v
	}
}

// toNumber is ECMAScript's ToNumber over the values a layout position can
// hold. https://tc39.es/ecma262/#sec-tonumber
func toNumber(v any) float64 {
	switch t := v.(type) {
	case nil:
		return 0
	case bool:
		if t {
			return 1
		}
		return 0
	case float64:
		return t
	case string:
		return stringToNumber(t)
	case []any:
		// ToPrimitive on an array is its join(",").
		return stringToNumber(arrayToString(t))
	default:
		// undefined, a plain object ("[object Object]") and a function.
		return math.NaN()
	}
}

// toJSString is ToString for the values an array can hold.
func toJSString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case float64:
		switch {
		case math.IsNaN(t):
			return "NaN"
		case math.IsInf(t, 1):
			return "Infinity"
		case math.IsInf(t, -1):
			return "-Infinity"
		}
		return jsonv.FormatNumber(t)
	case []any:
		return arrayToString(t)
	case nil:
		return "null"
	default:
		return "[object Object]"
	}
}

// arrayToString is Array.prototype.join(","): null and undefined elements
// are written as the empty string.
func arrayToString(a []any) string {
	parts := make([]string, len(a))
	for i, item := range a {
		if nullish(item) {
			continue
		}
		parts[i] = toJSString(item)
	}
	return strings.Join(parts, ",")
}

var (
	decimalLiteral = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$`)
	infinity       = regexp.MustCompile(`^[+-]?Infinity$`)
	radixLiteral   = regexp.MustCompile(`^0(?:[xX][0-9a-fA-F]+|[oO][0-7]+|[bB][01]+)$`)
)

// isJSWhitespace is ECMAScript's WhiteSpace and LineTerminator, what
// String.prototype.trim and StringToNumber strip.
func isJSWhitespace(r rune) bool {
	switch r {
	case '\t', '\v', '\f', ' ', 0xa0, 0xfeff, '\n', '\r', 0x2028, 0x2029:
		return true
	}
	return unicode.Is(unicode.Zs, r)
}

// stringToNumber is ECMAScript's StringToNumber.
// https://tc39.es/ecma262/#sec-stringtonumber
func stringToNumber(s string) float64 {
	s = strings.TrimFunc(s, isJSWhitespace)
	switch {
	case s == "":
		return 0
	case infinity.MatchString(s):
		if s[0] == '-' {
			return math.Inf(-1)
		}
		return math.Inf(1)
	case radixLiteral.MatchString(s):
		base := map[byte]int{'x': 16, 'X': 16, 'o': 8, 'O': 8, 'b': 2, 'B': 2}[s[1]]
		n, _ := new(big.Int).SetString(s[2:], base)
		f, _ := new(big.Float).SetInt(n).Float64()
		return f
	case decimalLiteral.MatchString(s):
		// ParseFloat rounds to nearest, as StringToNumber does, and returns
		// the infinity or zero with ErrRange past float64's range, which is
		// also JavaScript's answer.
		f, _ := strconv.ParseFloat(s, 64)
		return f
	}
	return math.NaN()
}
