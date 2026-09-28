// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package lint

import "github.com/flow-as-code/terraform-provider-flowascode/internal/flowdoc"

// UniqueNames is rules/unique-names.ts. Identifiers are unique within a
// flow and document names unique within a set, because both are keys to the
// emitters; the document-name half reads ctx.All.
var UniqueNames = Rule{
	ID:          "unique-names",
	Description: "Action Identifiers are unique within a flow, document names unique within a set, and both are valid.",
	Check: func(ctx RuleContext) {
		seen := map[string]bool{}
		for _, a := range actionsOf(ctx.Doc) {
			if seen[a.id] {
				ctx.Report(Report{
					Severity: SeverityError,
					BlockID:  blockID(a.id),
					Message:  `Duplicate Identifier "` + a.id + `".`,
				})
			}
			seen[a.id] = true

			if !flowdoc.IsValidIdentifier(a.id) {
				ctx.Report(Report{
					Severity: SeverityError,
					BlockID:  blockID(a.id),
					Message:  `Identifier "` + a.id + `" is not valid: at most 50 characters, and none of % : ( \ / ) = $ , ; [ ] { }.`,
				})
			}
		}

		name := docName(ctx.Doc)
		same := 0
		for _, d := range ctx.All {
			if docName(d) == name {
				same++
			}
		}
		if same > 1 {
			ctx.Report(Report{
				Severity: SeverityError,
				Message:  `Duplicate document name "` + name + `" in this flow set.`,
			})
		}
	},
}
