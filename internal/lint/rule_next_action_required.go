// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package lint

import (
	"strings"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/flowdoc"
	"github.com/flow-as-code/terraform-provider-flowascode/internal/jsonv"
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
// branch. The fifteen non-terminal modeled types rule 38 lists as unprobed
// are checked from the catalog alone; that the service refuses them without
// a NextAction is assumed. Only presence is checked, as the TypeScript's
// `!== undefined` does, so a present null counts. Terminal types, "none"
// types and unmodeled actions are not checked; a NextAction on a terminal
// type, which the service refuses, is left unchecked, as upstream.
//
// For a "mirrors:*" type whose mirrored branch is wired to a string target,
// the message names that target (nextActionHint), as the TypeScript does.
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
				Message:  a.typ + " has no NextAction; Connect refuses the action without one." + nextActionHint(a, rule),
			})
		}
	},
}

// nextActionHint is next-action-required.ts's hint: the mirrored branch's
// target, in words, or "" when there is none to copy. The first Errors
// element with the mirrored ErrorType, or the first Conditions element whose
// Condition.Operands[0] is the mirrored operand, supplies it, and only a
// string target counts.
func nextActionHint(a action, rule string) string {
	const errPrefix, condPrefix = "mirrors:error:", "mirrors:condition:"
	switch {
	case strings.HasPrefix(rule, errPrefix):
		typ := strings.TrimPrefix(rule, errPrefix)
		for _, e := range listAt(a.transitions, "Errors") {
			if t, ok := field(e, "ErrorType").(string); !ok || t != typ {
				continue
			}
			if target, ok := field(e, "NextAction").(string); ok {
				return ` Set it to "` + target + `", the ` + typ + ` branch's target, as the builder does.`
			}
			return ""
		}
	case strings.HasPrefix(rule, condPrefix):
		operand := strings.TrimPrefix(rule, condPrefix)
		for _, c := range listAt(a.transitions, "Conditions") {
			cond, ok := field(c, "Condition").(jsonv.Object)
			if !ok {
				continue
			}
			v, _ := cond.Get("Operands")
			operands, _ := v.([]any)
			if len(operands) == 0 {
				continue
			}
			if first, ok := operands[0].(string); !ok || first != operand {
				continue
			}
			if target, ok := field(c, "NextAction").(string); ok {
				return ` Set it to "` + target + `", the ` + operand + ` condition's target, as the builder does.`
			}
			return ""
		}
	}
	return ""
}
