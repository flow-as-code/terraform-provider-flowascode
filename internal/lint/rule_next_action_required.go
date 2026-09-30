// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package lint

import (
	"strings"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/flowdoc"
)

// NextActionRequired is rules/next-action-required.ts. Connect refuses a
// non-terminal action without Transitions.NextAction ("Action is missing
// required property. Path: Actions[N].Transitions.NextAction",
// InvalidContactFlowException), observed on every non-terminal modeled type
// probed on 2026-09-30 except MessageParticipantIteratively, which the
// service accepts either way (conformance/flow-language/actions.md, rule 38).
// https://docs.aws.amazon.com/connect/latest/APIReference/API_CreateContactFlow.html
//
// Which types need it comes from the catalog's next rule: "required", or
// "mirrors:*" for a type whose builder writes NextAction as a copy of a
// branch. Only presence is checked, as the TypeScript's `!== undefined`
// does, so a present null counts. Terminal types, "none" types and
// unmodeled actions are not checked.
var NextActionRequired = Rule{
	ID:          "next-action-required",
	Description: "Every non-terminal modeled action whose type the service refuses without a NextAction must carry one.",
	Check: func(ctx RuleContext) {
		for _, a := range actionsOf(ctx.Doc) {
			entry := flowdoc.ModeledEntry(a.typ)
			if entry == nil || entry.Terminal {
				continue
			}
			rule := entry.Transitions.Next
			if rule != "required" && !strings.HasPrefix(rule, "mirrors:") {
				continue
			}
			if _, has := a.transitions.Get("NextAction"); has {
				continue
			}
			ctx.Report(Report{
				Severity: SeverityError,
				BlockID:  blockID(a.id),
				Message:  a.typ + " has no NextAction; Connect refuses the action without one.",
			})
		}
	},
}
