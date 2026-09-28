// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package lint

import "github.com/flow-as-code/terraform-provider-flowascode/internal/flowdoc"

// ErrorBranches is rules/error-branches.ts, the backstop for the builder's
// type-level enforcement: hand-edited FlowDocs, exported flows and studio
// edits. Only modeled, non-terminal actions are checked; which branches an
// action must wire comes from the catalog (requiredErrorsFor).
var ErrorBranches = Rule{
	ID:          "error-branches",
	Description: "Every non-terminal modeled action must wire the error branches its page requires, the catch-all for most, and any branch a parameter it carries makes required.",
	Check: func(ctx RuleContext) {
		for _, a := range actionsOf(ctx.Doc) {
			if isTerminal(a) {
				continue
			}
			entry := flowdoc.ModeledEntry(a.typ)
			if entry == nil || entry.Terminal {
				continue
			}
			// new Set(Errors.map(e => e.ErrorType)): only a string can equal
			// an expected type.
			wired := map[string]bool{}
			for _, e := range listAt(a.transitions, "Errors") {
				if s, ok := field(e, "ErrorType").(string); ok {
					wired[s] = true
				}
			}
			for _, expected := range flowdoc.RequiredErrorsFor(a.typ, a.parameters) {
				if wired[expected] {
					continue
				}
				ctx.Report(Report{
					Severity: SeverityError,
					BlockID:  blockID(a.id),
					Message:  a.typ + ` does not wire its "` + expected + `" branch.`,
				})
			}
		}
	},
}
