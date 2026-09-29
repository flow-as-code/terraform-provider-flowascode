// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package lint

import (
	"strconv"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/flowdoc"
)

// ErrorBranches is rules/error-branches.ts, the backstop for the builder's
// type-level enforcement: hand-edited FlowDocs, exported flows and studio
// edits. Only modeled, non-terminal actions are checked; which branches an
// action must wire comes from the catalog (requiredErrorsFor), as do the
// branches it may wire at all and the fewest conditions its type takes
// (minConditions), each of which the service refuses otherwise.
var ErrorBranches = Rule{
	ID:          "error-branches",
	Description: "Every non-terminal modeled action must wire the error branches the service requires, the catch-all for most, any branch a parameter it carries makes required, and the conditions its type cannot do without, and no error branch its type does not have.",
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
			var types []string
			for _, e := range listAt(a.transitions, "Errors") {
				if s, ok := field(e, "ErrorType").(string); ok {
					wired[s] = true
				}
				// error.ErrorType as the TypeScript loop reads it: a
				// non-string never equals a listed type, and prints as
				// JavaScript would in the message.
				types = append(types, jsString(field(e, "ErrorType")))
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
			listed := map[string]bool{}
			for _, e := range entry.Transitions.Errors {
				listed[e.Type] = true
			}
			for _, t := range types {
				if listed[t] {
					continue
				}
				ctx.Report(Report{
					Severity: SeverityError,
					BlockID:  blockID(a.id),
					Message:  a.typ + ` has no "` + t + `" branch.`,
				})
			}
			if n, ok := lengthAt(a.transitions, "Conditions"); ok && n < flowdoc.MinConditionsFor(a.typ) {
				least := flowdoc.MinConditionsFor(a.typ)
				plural := "s"
				if least == 1 {
					plural = ""
				}
				ctx.Report(Report{
					Severity: SeverityError,
					BlockID:  blockID(a.id),
					Message:  a.typ + " needs at least " + strconv.Itoa(least) + " condition" + plural + ".",
				})
			}
		}
	},
}
