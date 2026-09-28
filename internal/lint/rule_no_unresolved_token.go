// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package lint

import (
	"regexp"
	"strings"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/flowdoc"
)

// cdrefPlaceholder is no-unresolved-token.ts's CDREF_PLACEHOLDER,
// /\$\{\s*cdref[^}]*\}/gi, translated rather than copied: JavaScript's \s is
// Unicode whitespace where Go's is ASCII, and the /i flag (without /u) folds
// only the five ASCII letters of "cdref" here, so the class and the letters
// are spelled out. Only placeholders that attempt to be a reference are this
// rule's business; "Your balance is ${amount}" is ordinary prompt text.
var cdrefPlaceholder = regexp.MustCompile(`\$\{[` + jsSpaceClass + `]*[cC][dD][rR][eE][fF][^}]*\}`)

// NoUnresolvedToken is rules/no-unresolved-token.ts, a hard rule. Every
// token must be well formed, occupy its entire field, and appear in the
// refs index; a token Connect never resolves deploys a flow that fails at
// runtime with an opaque error.
var NoUnresolvedToken = Rule{
	ID:          "no-unresolved-token",
	Description: "Every ${cdref:...} token must be well formed, stand alone, and appear in the refs index.",
	Hard:        true,
	Check: func(ctx RuleContext) {
		// new Set(refs.map(r => r.token)): only a string token can match.
		indexed := map[string]bool{}
		refs, _ := ctx.Doc.Get("refs")
		if list, ok := refs.([]any); ok {
			for _, r := range list {
				if t, ok := field(r, "token").(string); ok {
					indexed[t] = true
				}
			}
		}

		type entry struct {
			blockID *string
			path    string
			value   string
		}
		var all []entry
		for _, s := range documentStrings(ctx.Doc) {
			all = append(all, entry{path: s.Path, value: s.Value})
		}
		for _, as := range findingsForActions(ctx.Doc) {
			for _, s := range as.strings {
				all = append(all, entry{blockID: blockID(as.action.id), path: s.Path, value: s.Value})
			}
		}

		where := func(path string) string {
			switch {
			case strings.HasPrefix(path, "content.Metadata"):
				return ""
			case strings.HasPrefix(path, "Transitions"):
				return "Transition "
			}
			return "Parameter "
		}

		for _, e := range all {
			// placeholders[0]: the first match. A match is never empty, so ""
			// is placeholders.length === 0.
			token := cdrefPlaceholder.FindString(e.value)
			if token == "" {
				continue
			}
			// Connect requires reference fields to be fully static or a single
			// JSONPath identifier, so a token cannot be part of a longer string.
			if !flowdoc.TokenPattern.MatchString(e.value) {
				ctx.Report(Report{
					Severity: SeverityError,
					BlockID:  e.blockID,
					Message:  where(e.path) + `"` + e.path + `" embeds a token in a larger string ("` + e.value + `"). A token must be the entire value.`,
				})
				continue
			}
			if _, ok := flowdoc.ParseToken(token); !ok {
				ctx.Report(Report{
					Severity: SeverityError,
					BlockID:  e.blockID,
					Message:  where(e.path) + `"` + e.path + `" contains a malformed token "` + token + `".`,
				})
			} else if !indexed[token] {
				ctx.Report(Report{
					Severity: SeverityError,
					BlockID:  e.blockID,
					Message:  `Token "` + token + `" is missing from the refs index. Re-run synth to regenerate it.`,
				})
			}
		}
	},
}
