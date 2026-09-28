// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package flowdoc

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/jsonv"
)

// MaxActionsPerFlow is flowdoc.ts's MAX_ACTIONS_PER_FLOW: a single flow holds
// no more than 250 Actions.
const MaxActionsPerFlow = 250

// MaxIdentifierLength is flowdoc.ts's MAX_IDENTIFIER_LENGTH, counted in UTF-16
// code units as JavaScript's String length is.
const MaxIdentifierLength = 50

// ForbiddenIdentifierChars is flowdoc.ts's FORBIDDEN_IDENTIFIER_CHARS, the
// characters Connect rejects in an Identifier, in the same order.
// https://docs.aws.amazon.com/connect/latest/devguide/flow-language-actions.html
var ForbiddenIdentifierChars = []string{
	"%", ":", "(", "\\", "/", ")", "=", "$", ",", ";", "[", "]", "{", "}",
}

// ForbiddenIdentifiers is flowdoc.ts's FORBIDDEN_IDENTIFIERS, the Identifier
// values Connect rejects outright (prototype pollution names).
var ForbiddenIdentifiers = []string{
	"__proto__",
	"constructor",
	"__defineGetter__",
	"__defineSetter__",
	"toString",
	"hasOwnProperty",
	"isPrototypeOf",
	"propertyIsEnumerable",
	"toLocaleString",
	"valueOf",
}

// SlugPattern is flowdoc.ts's SLUG_PATTERN: lowercase, digits, single hyphens.
var SlugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// jsLength is JavaScript's String.prototype.length: UTF-16 code units. A
// byte that is not valid UTF-8 ranges as U+FFFD, one unit.
func jsLength(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}

// IsValidIdentifier is flowdoc.ts's isValidIdentifier.
func IsValidIdentifier(id string) bool {
	if l := jsLength(id); l == 0 || l > MaxIdentifierLength {
		return false
	}
	for _, f := range ForbiddenIdentifiers {
		if id == f {
			return false
		}
	}
	for _, c := range ForbiddenIdentifierChars {
		if strings.Contains(id, c) {
			return false
		}
	}
	return true
}

// InvalidFlowDocError is flowdoc.ts's InvalidFlowDocError: a public entry
// point was handed something that is not a usable FlowDoc.
type InvalidFlowDocError struct{ Message string }

func (e *InvalidFlowDocError) Error() string { return e.Message }

func invalid(msg string) error { return &InvalidFlowDocError{Message: msg} }

// typeName is flowdoc.ts's typeName. present is false for a missing key,
// which JavaScript reads as undefined. An Object reads "a object", as
// `a ${typeof value}` writes it.
func typeName(value any, present bool) string {
	if !present {
		return "undefined"
	}
	switch t := value.(type) {
	case nil:
		return "null"
	case []any:
		return "an array"
	case string:
		return "a string (" + string(jsonv.Encode(t, "")) + ")"
	case bool:
		return "a boolean"
	case float64, int:
		return "a number"
	default:
		return "a object"
	}
}

// AssertFlowDoc is flowdoc.ts's assertFlowDoc: the shallow structural check
// every public entry point runs, with the same messages byte for byte. It
// returns the value as an Object when it passes.
func AssertFlowDoc(value any, context string) (jsonv.Object, error) {
	doc, ok := value.(jsonv.Object)
	if !ok {
		return nil, invalid(context + " expects a FlowDoc object, got " + typeName(value, true) + ".")
	}
	for _, key := range []string{"name", "kind", "connectType"} {
		v, has := doc.Get(key)
		if _, isString := v.(string); !has || !isString {
			return nil, invalid(context + ` expects a FlowDoc with a string "` + key + `", got ` + typeName(v, has) + ".")
		}
	}
	contentValue, has := doc.Get("content")
	content, ok := contentValue.(jsonv.Object)
	if !has || !ok {
		return nil, invalid(context + ` expects a FlowDoc with an object "content", got ` + typeName(contentValue, has) + ".")
	}
	start, has := content.Get("StartAction")
	if _, isString := start.(string); !has || !isString {
		return nil, invalid(context + ` expects a FlowDoc with a string "content.StartAction", got ` + typeName(start, has) + ".")
	}
	actionsValue, has := content.Get("Actions")
	actions, ok := actionsValue.([]any)
	if !has || !ok {
		return nil, invalid(context + ` expects a FlowDoc with an array "content.Actions", got ` + typeName(actionsValue, has) + ".")
	}
	for i, a := range actions {
		at := context + ": FlowDoc content.Actions[" + strconv.Itoa(i) + "]"
		action, ok := a.(jsonv.Object)
		if !ok {
			return nil, invalid(at + " must be an action object, got " + typeName(a, true) + ".")
		}
		for _, key := range []string{"Identifier", "Type"} {
			v, has := action.Get(key)
			if _, isString := v.(string); !has || !isString {
				return nil, invalid(at + ` must have a string "` + key + `", got ` + typeName(v, has) + ".")
			}
		}
		for _, key := range []string{"Parameters", "Transitions"} {
			v, has := action.Get(key)
			if _, isObject := v.(jsonv.Object); !has || !isObject {
				id, _ := action.Get("Identifier")
				return nil, invalid(at + ` ("` + id.(string) + `") must have an object "` + key + `", got ` + typeName(v, has) + ".")
			}
		}
	}
	return doc, nil
}

// isArrayIndex reports whether a property key is an array index, which
// JavaScript's ordinary objects enumerate first, in ascending numeric order:
// the canonical decimal form of an integer from 0 to 2^32 - 2.
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
// an object whose properties were created in the given order: array indices
// ascending, then every other key in creation order.
func jsKeyOrder(keys []string) []string {
	var indices, rest []string
	for _, k := range keys {
		if isArrayIndex(k) {
			indices = append(indices, k)
		} else {
			rest = append(rest, k)
		}
	}
	// Insertion sort by numeric value; array indices have no leading zeros,
	// so length then bytes is numeric order.
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

// jsOrdered returns v with every object's members in JavaScript's own-key
// order (jsKeyOrder), which is what JSON.parse followed by JSON.stringify
// writes. jsonv keeps creation order, which differs only when a key is an
// array index.
func jsOrdered(v any) any {
	switch t := v.(type) {
	case []any:
		out := make([]any, len(t))
		for i, item := range t {
			out[i] = jsOrdered(item)
		}
		return out
	case jsonv.Object:
		out := make(jsonv.Object, 0, len(t))
		for _, k := range jsKeyOrder(t.Keys()) {
			val, _ := t.Get(k)
			out = append(out, jsonv.Member{Key: k, Value: jsOrdered(val)})
		}
		return out
	default:
		return v
	}
}
