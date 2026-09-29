// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package jsonv

import (
	"strings"
	"testing"
)

// Each want is what node's String(x) prints.
func TestFormatNumberIsNumberToString(t *testing.T) {
	cases := map[float64]string{
		0: "0", 1: "1", -1: "-1", 20: "20", 466.8238220214844: "466.8238220214844",
		0.1: "0.1", 1e21: "1e+21", 1e20: "100000000000000000000", 123e-20: "1.23e-18",
		0.000001: "0.000001", 0.0000001: "1e-7", 1.5e-7: "1.5e-7", 26.44894027709961: "26.44894027709961",
		9007199254740993: "9007199254740992", 1.7976931348623157e308: "1.7976931348623157e+308",
		-0.5: "-0.5", 5e-324: "5e-324", 123456789012345680000: "123456789012345680000",
	}
	for x, want := range cases {
		if got := FormatNumber(x); got != want {
			t.Errorf("FormatNumber(%v) = %q, want %q", x, got, want)
		}
	}
}

func TestEncodeIsJSONStringify(t *testing.T) {
	v, err := Decode([]byte(`{"b":[1,{"x":[]}],"a":{},"s":"q\"\\\n\t\u0001\u007f<>& é","a":2}`))
	if err != nil {
		t.Fatal(err)
	}
	// JSON.parse keeps a repeated key's first position and last value.
	want := "{\n  \"b\": [\n    1,\n    {\n      \"x\": []\n    }\n  ],\n  \"a\": 2,\n  \"s\": \"q\\\"\\\\\\n\\t\\u0001\u007f<>& é\"\n}"
	if got := string(Encode(v, "  ")); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	if got := string(Encode(v, "")); got != `{"b":[1,{"x":[]}],"a":2,"s":"q\"\\\n\t\u0001`+"\u007f<>& é"+`"}` {
		t.Errorf("compact: %s", got)
	}
}

func TestSortKeysComparesCodeUnits(t *testing.T) {
	// U+FF61 sorts after U+1F600 in UTF-16 (0xff61 > 0xd83d) but before it in bytes.
	v := SortKeys(Object{{Key: "\U0001F600", Value: 1.0}, {Key: "｡", Value: 2.0}, {Key: "a", Value: 3.0}})
	keys := v.(Object).Keys()
	if keys[0] != "a" || keys[1] != "\U0001F600" || keys[2] != "｡" {
		t.Errorf("order %q", keys)
	}
}

// node: JSON.stringify(JSON.parse('{"b":1,"10":2,"9":3,"01":4,"4294967295":5,"4294967294":6}'))
func TestEncodeUsesJavaScriptOwnKeyOrder(t *testing.T) {
	v, err := Decode([]byte(`{"b":1,"10":2,"9":3,"01":4,"4294967295":5,"4294967294":6}`))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"9":3,"10":2,"4294967294":6,"b":1,"01":4,"4294967295":5}`
	if got := string(Encode(v, "")); got != want {
		t.Errorf("got %s, want %s", got, want)
	}
	if got := string(Encode(SortKeys(v), "")); got != `{"9":3,"10":2,"4294967294":6,"01":4,"4294967295":5,"b":1}` {
		t.Errorf("sorted: %s", got)
	}
}

// JSON.parse keeps an unpaired surrogate, which no Go string can hold, so
// Decode refuses it rather than write U+FFFD in its place.
func TestDecodeRefusesALoneSurrogate(t *testing.T) {
	for _, in := range []string{`["\ud800"]`, `["\udc00x"]`, `["\ud83dA"]`, `{"\uDBFF":1}`, `["\ude00\ud83d"]`} {
		if _, err := Decode([]byte(in)); err == nil || !strings.Contains(err.Error(), "lone surrogate") {
			t.Errorf("Decode(%s) = %v, want a lone surrogate error", in, err)
		}
	}
	for _, in := range []string{`["😀"]`, `["\\ud800"]`, `["é"]`, `["\\\\"]`, `["😀"]`} {
		if _, err := Decode([]byte(in)); err != nil {
			t.Errorf("Decode(%s): %v", in, err)
		}
	}
}
