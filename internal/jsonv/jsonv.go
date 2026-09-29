// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

// Package jsonv is JSON as JavaScript holds it: objects keep their key order,
// numbers are float64, and Encode writes exactly what JSON.stringify writes,
// so a FlowDoc the provider serializes is byte-identical to the one
// @flow-as-code/core serializes. encoding/json differs in three ways that
// matter here: it sorts or struct-orders keys, it escapes <, >, & and
// U+2028/U+2029, and it formats numbers its own way.
//
// Values are nil, bool, float64, string, []any and Object.
package jsonv

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
)

// Member is one key and its value.
type Member struct {
	Key   string
	Value any
}

// Object is a JSON object in key order.
type Object []Member

// Get returns the value under key.
func (o Object) Get(key string) (any, bool) {
	for _, m := range o {
		if m.Key == key {
			return m.Value, true
		}
	}
	return nil, false
}

// Set replaces the value under key in place, or appends it, as assigning a
// property does in JavaScript.
func (o *Object) Set(key string, value any) {
	for i := range *o {
		if (*o)[i].Key == key {
			(*o)[i].Value = value
			return
		}
	}
	*o = append(*o, Member{Key: key, Value: value})
}

// Delete removes key.
func (o *Object) Delete(key string) {
	out := (*o)[:0]
	for _, m := range *o {
		if m.Key != key {
			out = append(out, m)
		}
	}
	*o = out
}

// Keys in order.
func (o Object) Keys() []string {
	keys := make([]string, len(o))
	for i, m := range o {
		keys[i] = m.Key
	}
	return keys
}

// Decode parses one JSON value as JSON.parse does: a repeated key keeps its
// first position and its last value.
func Decode(b []byte) (any, error) {
	// JSON.parse keeps an unpaired surrogate escape; encoding/json would turn
	// it into U+FFFD, and a Go string (like a Terraform value) cannot hold
	// it. Refusing is the only faithful answer, and it is the contract's
	// LONE_SURROGATE.
	if esc, ok := loneSurrogate(b); ok {
		return nil, fmt.Errorf("jsonv: lone surrogate %s has no UTF-8 encoding", esc)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	v, err := decodeValue(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("jsonv: trailing data after the value")
	}
	return v, nil
}

func decodeValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			obj := Object{}
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, ok := kt.(string)
				if !ok {
					return nil, fmt.Errorf("jsonv: object key is not a string")
				}
				val, err := decodeValue(dec)
				if err != nil {
					return nil, err
				}
				obj.Set(key, val)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return obj, nil
		case '[':
			arr := []any{}
			for dec.More() {
				val, err := decodeValue(dec)
				if err != nil {
					return nil, err
				}
				arr = append(arr, val)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return arr, nil
		}
		return nil, fmt.Errorf("jsonv: unexpected %v", t)
	case json.Number:
		return strconv.ParseFloat(string(t), 64)
	default:
		return t, nil // nil, bool, string
	}
}

// Encode is JSON.stringify(v, null, indent); an empty indent is the compact
// form.
func Encode(v any, indent string) []byte {
	var b bytes.Buffer
	encode(&b, v, indent, 0)
	return b.Bytes()
}

func encode(b *bytes.Buffer, v any, indent string, depth int) {
	newline := func(d int) {
		if indent != "" {
			b.WriteByte('\n')
			b.WriteString(strings.Repeat(indent, d))
		}
	}
	switch t := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		if t {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case float64:
		b.WriteString(FormatNumber(t))
	case int:
		b.WriteString(FormatNumber(float64(t)))
	case string:
		writeString(b, t)
	case []any:
		if len(t) == 0 {
			b.WriteString("[]")
			return
		}
		b.WriteByte('[')
		for i, item := range t {
			if i > 0 {
				b.WriteByte(',')
			}
			newline(depth + 1)
			encode(b, item, indent, depth+1)
		}
		newline(depth)
		b.WriteByte(']')
	case Object:
		if len(t) == 0 {
			b.WriteString("{}")
			return
		}
		b.WriteByte('{')
		for i, m := range OwnKeyOrder(t) {
			if i > 0 {
				b.WriteByte(',')
			}
			newline(depth + 1)
			writeString(b, m.Key)
			b.WriteByte(':')
			if indent != "" {
				b.WriteByte(' ')
			}
			encode(b, m.Value, indent, depth+1)
		}
		newline(depth)
		b.WriteByte('}')
	default:
		panic(fmt.Sprintf("jsonv: cannot encode %T", v))
	}
}

// writeString quotes as JSON.stringify does: the two-character escapes for
// quote, backslash, backspace, form feed, newline, carriage return and tab,
// \u00xx (lowercase) for any other control character, everything else raw.
func writeString(b *bytes.Buffer, s string) {
	const hex = "0123456789abcdef"
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if c < 0x20 {
				b.WriteString(`\u00`)
				b.WriteByte(hex[c>>4])
				b.WriteByte(hex[c&0xf])
			} else {
				b.WriteByte(c)
			}
		}
	}
	b.WriteByte('"')
}

// FormatNumber is Number.prototype.toString for a finite number, and "null"
// for NaN and the infinities as JSON.stringify writes them.
// https://tc39.es/ecma262/#sec-numeric-types-number-tostring
func FormatNumber(x float64) string {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return "null"
	}
	if x == 0 {
		return "0"
	}
	sign := ""
	if x < 0 {
		sign = "-"
		x = -x
	}
	// Shortest round-trip digits and the exponent of the first one.
	e := strconv.FormatFloat(x, 'e', -1, 64)
	mant, expPart, _ := strings.Cut(e, "e")
	digits := strings.Replace(mant, ".", "", 1)
	exp, _ := strconv.Atoi(expPart)
	k := len(digits)
	n := exp + 1
	switch {
	case k <= n && n <= 21:
		return sign + digits + strings.Repeat("0", n-k)
	case 0 < n && n <= 21:
		return sign + digits[:n] + "." + digits[n:]
	case -6 < n && n <= 0:
		return sign + "0." + strings.Repeat("0", -n) + digits
	}
	expSign := "+"
	if n-1 < 0 {
		expSign = "-"
	}
	abs := n - 1
	if abs < 0 {
		abs = -abs
	}
	if k == 1 {
		return sign + digits + "e" + expSign + strconv.Itoa(abs)
	}
	return sign + digits[:1] + "." + digits[1:] + "e" + expSign + strconv.Itoa(abs)
}

// SortKeys returns v with every object's keys sorted by UTF-16 code unit, as
// @flow-as-code/core's sortKeys compares with < on strings. Arrays keep their
// order.
func SortKeys(v any) any {
	switch t := v.(type) {
	case []any:
		out := make([]any, len(t))
		for i, item := range t {
			out[i] = SortKeys(item)
		}
		return out
	case Object:
		out := make(Object, len(t))
		for i, m := range t {
			out[i] = Member{Key: m.Key, Value: SortKeys(m.Value)}
		}
		sort.SliceStable(out, func(i, j int) bool { return LessUTF16(out[i].Key, out[j].Key) })
		// Object.fromEntries builds the result, so it has JavaScript's own-key
		// order: array-index keys first whatever the sort said.
		return OwnKeyOrder(out)
	default:
		return v
	}
}

// LessUTF16 is JavaScript's a < b on strings: code unit order, which differs
// from byte order only for characters above U+FFFF against U+E000..U+FFFF.
func LessUTF16(a, b string) bool {
	ua, ub := utf16Units(a), utf16Units(b)
	for i := 0; i < len(ua) && i < len(ub); i++ {
		if ua[i] != ub[i] {
			return ua[i] < ub[i]
		}
	}
	return len(ua) < len(ub)
}

func utf16Units(s string) []uint16 {
	out := make([]uint16, 0, len(s))
	for _, r := range s {
		if r >= 0x10000 {
			r -= 0x10000
			out = append(out, uint16(0xd800+(r>>10)), uint16(0xdc00+(r&0x3ff)))
		} else {
			out = append(out, uint16(r))
		}
	}
	return out
}

// OwnKeyOrder is the order JavaScript enumerates an object's own string keys
// (Object.keys, JSON.stringify): every array-index key (a canonical decimal
// integer below 2^32 - 1) in ascending numeric order, then the rest in
// insertion order.
// https://tc39.es/ecma262/#sec-ordinaryownpropertykeys
func OwnKeyOrder(o Object) Object {
	var indices, rest Object
	for _, m := range o {
		if isArrayIndex(m.Key) {
			indices = append(indices, m)
		} else {
			rest = append(rest, m)
		}
	}
	if len(indices) == 0 {
		return o
	}
	sort.SliceStable(indices, func(i, j int) bool {
		a, b := indices[i].Key, indices[j].Key
		if len(a) != len(b) {
			return len(a) < len(b)
		}
		return a < b
	})
	return append(indices, rest...)
}

func isArrayIndex(k string) bool {
	if k == "" || len(k) > 10 || (len(k) > 1 && k[0] == '0') {
		return false
	}
	for i := 0; i < len(k); i++ {
		if k[i] < '0' || k[i] > '9' {
			return false
		}
	}
	n, err := strconv.ParseUint(k, 10, 64)
	return err == nil && n < 4294967295
}

// loneSurrogate finds a \uXXXX escape of an unpaired UTF-16 surrogate. An
// escaped backslash is skipped, so \\ud800 is text, not an escape.
func loneSurrogate(b []byte) (string, bool) {
	unit := func(i int) (uint64, bool) {
		if i+6 > len(b) || b[i] != '\\' || b[i+1] != 'u' {
			return 0, false
		}
		u, err := strconv.ParseUint(string(b[i+2:i+6]), 16, 16)
		return u, err == nil
	}
	for i := 0; i < len(b); i++ {
		if b[i] != '\\' {
			continue
		}
		u, ok := unit(i)
		if !ok {
			i++ // the escaped character
			continue
		}
		switch {
		case u >= 0xDC00 && u <= 0xDFFF:
			return string(b[i : i+6]), true
		case u >= 0xD800 && u <= 0xDBFF:
			if lo, ok := unit(i + 6); !ok || lo < 0xDC00 || lo > 0xDFFF {
				return string(b[i : i+6]), true
			}
			i += 11
		default:
			i += 5
		}
	}
	return "", false
}
