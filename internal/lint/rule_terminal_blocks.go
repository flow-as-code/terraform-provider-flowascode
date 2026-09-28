// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package lint

import "github.com/flow-as-code/terraform-provider-flowascode/internal/flowdoc"

// endsFlow is terminal-blocks.ts's endsFlow: a terminal type with its empty
// Transitions, or an action that holds the participant (the catalog's
// waits) with no NextAction key. A present null NextAction is not absent.
func endsFlow(a action) bool {
	if isTerminal(a) {
		return flowdoc.IsTerminalAction(a.typ)
	}
	_, hasNext := a.transitions.Get("NextAction")
	return flowdoc.HoldsParticipant(a.typ) && !hasNext
}

// TerminalBlocks is rules/terminal-blocks.ts. A flow that cannot terminate
// strands the contact; an action with empty Transitions that is not a
// terminal type is an unintended dead end.
var TerminalBlocks = Rule{
	ID:          "terminal-blocks",
	Description: "A flow must reach a terminal action, and only terminal action types may have empty transitions.",
	Check: func(ctx RuleContext) {
		byID := actionsByID(ctx.Doc)
		reachesTerminal := false
		for id := range reachable(ctx.Doc) {
			if a, ok := byID[id]; ok && endsFlow(a) {
				reachesTerminal = true
				break
			}
		}
		if !reachesTerminal {
			ctx.Report(Report{
				Severity: SeverityError,
				Message:  "No terminal action is reachable from StartAction; the flow cannot end.",
			})
		}

		for _, a := range actionsOf(ctx.Doc) {
			if isTerminal(a) && !flowdoc.IsTerminalAction(a.typ) {
				ctx.Report(Report{
					Severity: SeverityWarning,
					BlockID:  blockID(a.id),
					Message:  a.typ + " has no transitions but is not a terminal action type, so the flow dead-ends here.",
				})
			}
		}
	},
}
