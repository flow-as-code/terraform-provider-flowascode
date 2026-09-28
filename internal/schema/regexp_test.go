// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package schema

import (
	"strings"
	"testing"
)

// Patterns and strings where ECMA-262 (u flag, as Ajv compiles) and RE2
// disagree, with ECMA-262's answer. The synthetic oracle cases hold several of
// these to Ajv itself; this table reaches the rest of the translation.
func TestECMAPatternSemantics(t *testing.T) {
	for _, tc := range []struct {
		pattern string
		in      string
		want    bool
	}{
		{`^(?!aws:)`, "aws:connect:x", false},
		{`^(?!aws:)`, "aws", true},
		{`^(?!aws:)`, "", true},
		{`^(?!aws:)`, "x-aws:", true},
		{`^(?!aws:).+`, "", false},
		{`^(?!aws:).+`, "\r", false},
		{`^(?!aws:).+`, "a\r", true},
		{`^(?!aws:).+`, "\u2028x", false},
		{`^(?!a\.b)x`, "x", true},
		{`^(?!a\.b)`, "a.bc", false},
		{`^.$`, "\u2029", false},
		{`^.$`, "\U0001F600", true},
		{`^[.]$`, "\n", false},
		{`^\s+$`, "\u00a0\u1680\u2000\u200a\u202f\u205f\u3000\ufeff\v\f", true},
		{`^\s$`, "\u200b", false},
		{`^\S$`, "\u00a0", false},
		{`^[\s]$`, "\u3000", true},
		{`^[a\s]$`, "a", true},
		{`[]`, "", false},
		{`[]`, "]", false},
		{`^[^]$`, "\n", true},
		{`^[[:a]$`, "[", true},
		{`^[[:a]$`, "b", false},
		{`^[\b]$`, "\b", true},
		{`^\bx\b$`, "x", true},
		{`^\0$`, "\x00", true},
		{`^\v$`, "\v", true},
		{`^\u0041\u{42}\x43$`, "ABC", true},
		{`^\uD83D\uDE00$`, "\U0001F600", true},
		{`^(?:ab)+$`, "abab", true},
		{`^(?<n>a)$`, "a", true},
		{`^a{2,}$`, "aaa", true},
		{`^a{2}$`, "aaa", false},
		{`^[a\-z]$`, "-", true},
		{`^\d\w\D\W$`, "1a!!", true},
		{`^\/$`, "/", true},
		{`b`, "abc", true},
		{`^b`, "abc", false},
		{`c$`, "abc\n", false},
		{`^\$\.[A-Za-z0-9_$.\[\]'-]+$`, "$.Attributes['x']", true},
		{`^[^%:(\\/)=$,;\[\]{}]+$`, `a\b`, false},
	} {
		re, err := compileECMA(tc.pattern)
		if err != nil {
			t.Errorf("%s: %v", tc.pattern, err)
			continue
		}
		if got := re.MatchString(tc.in); got != tc.want {
			t.Errorf("%s on %q: got %v, want %v", tc.pattern, tc.in, got, tc.want)
		}
		if re.String() != tc.pattern {
			t.Errorf("String() = %q, want the source %q", re.String(), tc.pattern)
		}
	}
}

// A pattern this translation cannot carry to RE2 with the same meaning is
// refused, so the schema fails to compile instead of validating differently
// from Ajv. The first list Ajv compiles and this does not: a schema using
// one is a porting task, not a silent divergence. The second is refused by
// `new RegExp(p, "u")` too (checked with node 26), so Ajv fails the same way.
func TestECMAPatternRefusals(t *testing.T) {
	ajvCompiles := []string{
		`(?=a)`, `a(?!b)`, `(?<=a)b`, `(?<!a)b`, // lookaround beyond a leading ^(?!literal)
		`^(?!a|b)`, `^(?!a+)`, `^(?!a)b|c`,
		`(a)\1`,          // backreference
		`\p{L}`, `\P{L}`, // property escapes
		`(?s:.)`,      // modifiers
		`\cJ`, `[\S]`, // control escape, \S inside a class
		`\uD800`, `\u{D800}`, // lone surrogates: Go strings cannot hold one
	}
	ajvRefuses := []string{
		`\k<n>`, `(?i)a`, `^(?!a`, `^(?!a\:b)`, `\z`, `\A`, `\q`, `\-`,
		`a{`, `}`, `]`, `{1}`, `\01`, `[\B]`, `\x4`, `\u12`, `\u{110000}`, `[abc`, `a\`, `a{2,1}`, `^[]a]$`,
	}
	for _, p := range append(ajvCompiles, ajvRefuses...) {
		if _, err := compileECMA(p); err == nil {
			t.Errorf("%s: compiled, want a refusal", p)
		} else if !strings.Contains(err.Error(), "pattern") {
			t.Errorf("%s: error %q does not name the pattern", p, err)
		}
	}
}
