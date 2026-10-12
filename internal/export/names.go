// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package export

import (
	"strings"

	"golang.org/x/text/unicode/norm"
)

// SlugifyResourceName is export.ts's slugifyResourceName: Connect resource
// names are free text, FlowDoc names are slugs. NFKD, combining marks
// U+0300 to U+036F removed, lowercased, every run of anything but [a-z0-9]
// collapsed to one hyphen, edge hyphens trimmed.
//
// The TypeScript lowercases with String.prototype.toLowerCase, which is full
// Unicode case mapping; this lowercases ASCII only. The two agree on the
// result: after the regex only [a-z0-9] survives, and the only characters
// whose lowercase mapping reaches ASCII from outside A-Z are U+0130 and
// U+212A, both of which NFKD has already decomposed to ASCII by then.
func SlugifyResourceName(name string) string {
	decomposed := norm.NFKD.String(name)
	var b strings.Builder
	pendingHyphen := false
	for _, r := range decomposed {
		if r >= 0x0300 && r <= 0x036f {
			continue // /[̀-ͯ]/g
		}
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if pendingHyphen {
				b.WriteByte('-')
				pendingHyphen = false
			}
			b.WriteRune(r)
			continue
		}
		// /[^a-z0-9]+/g -> "-", then /^-+|-+$/g -> "": a run of others
		// becomes one hyphen, and one at either edge is never written.
		if b.Len() > 0 {
			pendingHyphen = true
		}
	}
	return b.String()
}

// trimJS is String.prototype.trim: WhiteSpace and LineTerminator stripped
// from both ends, which is not Go's unicode.IsSpace (it takes U+0085 and
// leaves U+FEFF). internal/lint's jsTrim is the same set.
// https://tc39.es/ecma262/#sec-string.prototype.trim
func trimJS(s string) string {
	return strings.TrimFunc(s, func(r rune) bool {
		switch r {
		case '\t', '\n', '\v', '\f', '\r', ' ', 0x00a0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
			return true
		}
		return r >= 0x2000 && r <= 0x200a
	})
}
