// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package lint

import "unicode"

// localeCompare reproduces String.prototype.localeCompare as engine.ts calls
// it: no locale argument, so the runtime's default collator. Under Node that
// is ICU's CLDR root collation (en-US tailors nothing), at tertiary
// strength, alternate=non-ignorable, caseFirst off, numeric off. It is NOT
// byte order: "a" < "B" < "b", "_" < "-" < "0" < "a", and a control
// character is ignored entirely.
//
// The table below is exact for ASCII. It was read off Node 26.8.1 with
// ICU 78.3 by sorting every ASCII character and grouping them with a
// base-sensitivity collator, and testdata/ts-oracle.json holds 4,000
// localeCompare results over ASCII strings that the tests replay.
//
// Limits. Lint compares doc names, rule ids, block Identifiers and messages,
// which in practice are ASCII; messages can quote authored values, which may
// not be. Outside ASCII this is an approximation, not ICU. A format or
// control character (Cc, Cf) and a combining mark (Mn, Me) are ignored, as
// ICU ignores the first two outright (ICU gives a mark a secondary weight,
// which is lost here). Every other character is placed in one of three
// bands by general category, in code point order within the band: space,
// punctuation and non-currency symbols (Z, P, S) between "~" and "$", where
// ICU puts emoji and most symbols; currency symbols (Sc) between "$" and
// "0"; everything else (letters, digits, other scripts) after "z". So an
// accented letter does not sort next to its base letter, "\u00a1" does not sort
// next to "!", a non-ASCII digit does not equal its ASCII digit, and ICU's
// expansions and contractions are not modeled. Two findings whose ordering
// keys differ only in such text can come out in a different order than the
// TypeScript engine gives them. The TypeScript side is itself locale
// dependent: a runtime whose default locale tailors ASCII (none of the
// common ones do) would order differently too.
//
// The comparison is the Unicode Collation Algorithm's, level by level: the
// sequences of primary weights first, then (ASCII has one secondary weight,
// so that level never decides) the sequences of tertiary weights, where a
// lowercase letter precedes its uppercase form.
// https://www.unicode.org/reports/tr10/#Comparison
func localeCompare(a, b string) int {
	pa, ta := collationElements(a)
	pb, tb := collationElements(b)
	if c := compareWeights(pa, pb); c != 0 {
		return c
	}
	return compareWeights(ta, tb)
}

// asciiPrimaryOrder lists the ASCII characters that carry a primary weight,
// ascending. A letter's two cases share one weight; everything else has its
// own.
const asciiPrimaryOrder = "\t\n\v\f\r _-,;:!?.'\"()[]{}@*/\\&#%`^+<=>|~$0123456789" +
	"aAbBcCdDeEfFgGhHiIjJkKlLmMnNoOpPqQrRsStTuUvVwWxXyYzZ"

// asciiPrimary is each ASCII character's primary weight; 0 means completely
// ignorable (U+0000..U+0008, U+000E..U+001F and U+007F).
var asciiPrimary = func() [128]int {
	var w [128]int
	next := 0
	for i := 0; i < len(asciiPrimaryOrder); i++ {
		c := asciiPrimaryOrder[i]
		isUpper := c >= 'A' && c <= 'Z'
		if !isUpper {
			next++
		}
		w[c] = next
	}
	return w
}()

// Primary weights are spaced by bandWidth, which exceeds every code point,
// so a band can sit between two adjacent ASCII weights and order its
// characters by code point.
const bandWidth = 1 << 21

var (
	symbolBand   = asciiPrimary['~'] * bandWidth
	currencyBand = asciiPrimary['$'] * bandWidth
	letterBand   = (asciiPrimary['z'] + 1) * bandWidth
)

func collationElements(s string) (primary, tertiary []int) {
	for _, r := range s {
		if r < 128 {
			p := asciiPrimary[r]
			if p == 0 {
				continue
			}
			primary = append(primary, p*bandWidth)
			if r >= 'A' && r <= 'Z' {
				tertiary = append(tertiary, 1)
			} else {
				tertiary = append(tertiary, 0)
			}
			continue
		}
		var band int
		switch {
		case unicode.In(r, unicode.Cc, unicode.Cf, unicode.Mn, unicode.Me):
			continue
		case unicode.Is(unicode.Sc, r):
			band = currencyBand
		case unicode.In(r, unicode.Z, unicode.P, unicode.S):
			band = symbolBand
		default:
			band = letterBand
		}
		primary = append(primary, band+1+int(r))
		tertiary = append(tertiary, 0)
	}
	return primary, tertiary
}

func compareWeights(a, b []int) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(a) < len(b):
		return -1
	case len(a) > len(b):
		return 1
	}
	return 0
}
