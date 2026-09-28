// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package schema

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// ECMA-262 patterns on Go's RE2.
//
// JSON Schema's `pattern` and `patternProperties` are ECMA-262 regular
// expressions. Ajv compiles each one as `new RegExp(pattern, "u")` (its
// unicodeRegExp option, on by default in Ajv 8), and searches rather than
// anchors, as the specification says. santhosh-tekuri/jsonschema compiles them
// with Go's regexp (RE2) unless given another engine. The two dialects agree
// on most of what a schema writes, but not all of it:
//
//   - RE2 has no lookaround. flowdoc-0.2.schema.json writes `^(?!aws:)` and
//     `^(?!aws:).+` (tag keys), which RE2 refuses to compile, so the default
//     engine cannot load the schema at all.
//   - `.` excludes \n, \r, U+2028 and U+2029 in ECMA-262; in RE2 only \n.
//   - `\s` is Unicode white space plus the line terminators in ECMA-262; in
//     RE2 it is ASCII [\t\n\f\r ].
//   - `[]` matches nothing and `[^]` matches anything in ECMA-262; RE2 reads
//     `[]...]` as a class containing `]`.
//   - `[[:alpha:]]` is a POSIX class in RE2 and a plain set of characters in
//     ECMA-262.
//
// compileECMA translates a pattern to RE2 where the semantics coincide and
// refuses it otherwise, so a schema that someday writes a construct this
// translation does not cover fails to compile here instead of validating
// differently from Ajv. The one lookaround it accepts is the shape the
// schema uses: `^(?!literal)` at the very start of a pattern with no
// top-level alternation, which is exactly "does not start with literal, and
// the rest matches from offset 0".

// ecmaSpace is ECMA-262's WhiteSpace and LineTerminator code points, the set
// `\s` matches, as the inside of an RE2 character class.
// https://tc39.es/ecma262/#sec-characterclassescape
const ecmaSpace = `\t\n\x{b}\f\r \x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}`

// ecmaDot is `.` without the s flag: any code point but a line terminator.
const ecmaDot = `[^\n\r\x{2028}\x{2029}]`

// quantifier is a braced quantifier, {n}, {n,} or {n,m}.
var quantifier = regexp.MustCompile(`^\{[0-9]+(,[0-9]*)?\}`)

// ecmaRegexp is a compiled pattern; String is the ECMA-262 source, which is
// what Ajv prints in a pattern error.
type ecmaRegexp struct {
	src string
	re  *regexp.Regexp
	// notPrefix, when set, is the literal a leading ^(?!...) refuses.
	notPrefix *string
}

func (r *ecmaRegexp) String() string { return r.src }

func (r *ecmaRegexp) MatchString(s string) bool {
	if r.notPrefix != nil && strings.HasPrefix(s, *r.notPrefix) {
		return false
	}
	return r.re.MatchString(s)
}

// compileECMA is the jsonschema.RegexpEngine this package validates with.
func compileECMA(src string) (jsonschema.Regexp, error) {
	body := src
	var notPrefix *string
	if strings.HasPrefix(body, "^(?!") {
		lit, rest, err := leadingNegativeLookahead(body[len("^(?!"):])
		if err != nil {
			return nil, fmt.Errorf("pattern %q: %w", src, err)
		}
		if hasTopLevelAlternation(rest) {
			return nil, fmt.Errorf("pattern %q: a leading negative lookahead followed by top-level alternation is not supported", src)
		}
		notPrefix = &lit
		body = "^" + rest
	}
	translated, err := translateECMA(body)
	if err != nil {
		return nil, fmt.Errorf("pattern %q: %w", src, err)
	}
	re, err := regexp.Compile(translated)
	if err != nil {
		return nil, fmt.Errorf("pattern %q: %w", src, err)
	}
	return &ecmaRegexp{src: src, re: re, notPrefix: notPrefix}, nil
}

// leadingNegativeLookahead reads a literal up to the `)` that closes the
// lookahead and returns it with the pattern that follows.
func leadingNegativeLookahead(s string) (lit, rest string, err error) {
	var b strings.Builder
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == ')':
			return b.String(), s[i+1:], nil
		case c == '\\':
			if i+1 >= len(s) || !isSyntaxChar(s[i+1]) {
				return "", "", fmt.Errorf("only a literal is supported inside a leading negative lookahead")
			}
			b.WriteByte(s[i+1])
			i += 2
		case isSyntaxChar(c):
			return "", "", fmt.Errorf("only a literal is supported inside a leading negative lookahead")
		default:
			_, n := utf8.DecodeRuneInString(s[i:])
			b.WriteString(s[i : i+n])
			i += n
		}
	}
	return "", "", fmt.Errorf("unterminated negative lookahead")
}

// hasTopLevelAlternation reports a `|` outside every group and class.
func hasTopLevelAlternation(s string) bool {
	depth, inClass := 0, false
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '\\':
			i++
		case inClass:
			if c == ']' {
				inClass = false
			}
		case c == '[':
			inClass = true
		case c == '(':
			depth++
		case c == ')':
			depth--
		case c == '|' && depth == 0:
			return true
		}
	}
	return false
}

// isSyntaxChar is ECMA-262's SyntaxCharacter plus `/`, the characters a
// u-mode pattern may escape to mean themselves.
func isSyntaxChar(c byte) bool {
	return strings.IndexByte(`^$\.*+?()[]{}|/`, c) >= 0
}

// translateECMA rewrites a u-mode ECMA-262 pattern as RE2 syntax with the same
// meaning, or refuses it.
func translateECMA(s string) (string, error) {
	var out strings.Builder
	for i := 0; i < len(s); {
		c := s[i]
		switch c {
		case '.':
			out.WriteString(ecmaDot)
			i++
		case '\\':
			n, err := translateEscape(&out, s, i, false)
			if err != nil {
				return "", err
			}
			i += n
		case '[':
			n, err := translateClass(&out, s, i)
			if err != nil {
				return "", err
			}
			i += n
		case '(':
			if strings.HasPrefix(s[i:], "(?") {
				switch {
				case strings.HasPrefix(s[i:], "(?:"):
					out.WriteString("(?:")
					i += 3
				case strings.HasPrefix(s[i:], "(?<") && !strings.HasPrefix(s[i:], "(?<=") && !strings.HasPrefix(s[i:], "(?<!"):
					out.WriteString("(?P<")
					i += 3
				default:
					return "", fmt.Errorf("group %q is not supported (RE2 has no lookaround or inline flags)", s[i:min(i+4, len(s))])
				}
				continue
			}
			out.WriteByte('(')
			i++
		case '{':
			m := quantifier.FindString(s[i:])
			if m == "" {
				return "", fmt.Errorf("a lone '{' is a syntax error in a u-mode pattern")
			}
			out.WriteString(m)
			i += len(m)
		case '}', ']':
			return "", fmt.Errorf("a lone %q is a syntax error in a u-mode pattern", c)
		default:
			_, n := utf8.DecodeRuneInString(s[i:])
			out.WriteString(s[i : i+n])
			i += n
		}
	}
	return out.String(), nil
}

// translateClass rewrites the class starting at s[i] and returns its length.
func translateClass(out *strings.Builder, s string, i int) (int, error) {
	start := i
	i++
	negated := i < len(s) && s[i] == '^'
	if negated {
		i++
	}
	if i < len(s) && s[i] == ']' {
		// ECMA-262: [] matches nothing, [^] matches any code point.
		if negated {
			out.WriteString(`[\x{0}-\x{10FFFF}]`)
		} else {
			out.WriteString(`[^\x{0}-\x{10FFFF}]`)
		}
		return i + 1 - start, nil
	}
	var body strings.Builder
	for i < len(s) {
		c := s[i]
		switch c {
		case ']':
			out.WriteByte('[')
			if negated {
				out.WriteByte('^')
			}
			out.WriteString(body.String())
			out.WriteByte(']')
			return i + 1 - start, nil
		case '\\':
			n, err := translateEscape(&body, s, i, true)
			if err != nil {
				return 0, err
			}
			i += n
		case '[':
			// Literal in ECMA-262; escaped so RE2 cannot read [:name:].
			body.WriteString(`\[`)
			i++
		default:
			_, n := utf8.DecodeRuneInString(s[i:])
			body.WriteString(s[i : i+n])
			i += n
		}
	}
	return 0, fmt.Errorf("unterminated character class")
}

// translateEscape rewrites the escape at s[i] and returns its length.
func translateEscape(out *strings.Builder, s string, i int, inClass bool) (int, error) {
	if i+1 >= len(s) {
		return 0, fmt.Errorf("a pattern may not end in '\\'")
	}
	c := s[i+1]
	switch {
	case strings.IndexByte("dDwW", c) >= 0:
		// ASCII in both dialects without the i flag.
		out.WriteString(s[i : i+2])
	case c == 'b' || c == 'B':
		if inClass {
			if c == 'B' {
				return 0, fmt.Errorf(`\B inside a class is a syntax error in a u-mode pattern`)
			}
			out.WriteString(`\x{8}`) // [\b] is backspace
		} else {
			out.WriteString(s[i : i+2])
		}
	case c == 's':
		if inClass {
			out.WriteString(ecmaSpace)
		} else {
			out.WriteString("[" + ecmaSpace + "]")
		}
	case c == 'S':
		if inClass {
			return 0, fmt.Errorf(`\S inside a class is not supported`)
		}
		out.WriteString("[^" + ecmaSpace + "]")
	case strings.IndexByte("tnrf", c) >= 0:
		out.WriteString(s[i : i+2])
	case c == 'v':
		out.WriteString(`\x{b}`)
	case c == '0':
		if i+2 < len(s) && s[i+2] >= '0' && s[i+2] <= '9' {
			return 0, fmt.Errorf("an octal escape is a syntax error in a u-mode pattern")
		}
		out.WriteString(`\x{0}`)
	case c == '-' && inClass:
		out.WriteString(`\-`)
	case isSyntaxChar(c):
		if c == '/' {
			out.WriteByte('/')
		} else {
			out.WriteString(s[i : i+2])
		}
	case c == 'x':
		if i+4 > len(s) || !isHex(s[i+2:i+4]) {
			return 0, fmt.Errorf(`\x needs two hex digits`)
		}
		fmt.Fprintf(out, `\x{%s}`, s[i+2:i+4])
		return 4, nil
	case c == 'u':
		return translateUnicodeEscape(out, s, i)
	default:
		return 0, fmt.Errorf(`escape \%c is not supported`, c)
	}
	return 2, nil
}

// translateUnicodeEscape handles \uXXXX (joining a surrogate pair) and
// \u{X...}.
func translateUnicodeEscape(out *strings.Builder, s string, i int) (int, error) {
	if strings.HasPrefix(s[i:], `\u{`) {
		end := strings.IndexByte(s[i:], '}')
		if end < 0 || !isHex(s[i+3:i+end]) {
			return 0, fmt.Errorf(`malformed \u{...} escape`)
		}
		cp, err := strconv.ParseUint(s[i+3:i+end], 16, 32)
		if err != nil || cp > 0x10FFFF {
			return 0, fmt.Errorf(`\u{...} escape out of range`)
		}
		if cp >= 0xD800 && cp <= 0xDFFF {
			return 0, fmt.Errorf("a lone surrogate cannot be matched by Go strings")
		}
		fmt.Fprintf(out, `\x{%x}`, cp)
		return end + 1, nil
	}
	if i+6 > len(s) || !isHex(s[i+2:i+6]) {
		return 0, fmt.Errorf(`\u needs four hex digits`)
	}
	hi, _ := strconv.ParseUint(s[i+2:i+6], 16, 32)
	if hi >= 0xD800 && hi <= 0xDBFF && i+12 <= len(s) && s[i+6:i+8] == `\u` && isHex(s[i+8:i+12]) {
		lo, _ := strconv.ParseUint(s[i+8:i+12], 16, 32)
		if lo >= 0xDC00 && lo <= 0xDFFF {
			fmt.Fprintf(out, `\x{%x}`, 0x10000+(hi-0xD800)<<10+(lo-0xDC00))
			return 12, nil
		}
	}
	if hi >= 0xD800 && hi <= 0xDFFF {
		return 0, fmt.Errorf("a lone surrogate cannot be matched by Go strings")
	}
	fmt.Fprintf(out, `\x{%x}`, hi)
	return 6, nil
}

func isHex(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}
