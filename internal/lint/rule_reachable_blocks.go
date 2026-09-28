// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package lint

// ReachableBlocks is rules/reachable-blocks.ts. Unreachable actions are kept,
// never dropped, but are almost always an editing mistake (a warning);
// dangling targets are an error, because Connect rejects the flow. A target
// that is not a string never names an action and is printed as JavaScript's
// String() prints it: "null", "undefined", "7", "[object Object]".
var ReachableBlocks = Rule{
	ID:          "reachable-blocks",
	Description: "Every action should be reachable from StartAction, and every transition target must exist.",
	Check: func(ctx RuleContext) {
		byID := actionsByID(ctx.Doc)

		if start := startAction(ctx.Doc); !hasAction(byID, start) {
			ctx.Report(Report{
				Severity: SeverityError,
				Message:  `StartAction "` + start + `" does not match any action.`,
			})
		}

		for _, a := range actionsOf(ctx.Doc) {
			for _, target := range transitionTargets(a) {
				if !hasAction(byID, target) {
					ctx.Report(Report{
						Severity: SeverityError,
						BlockID:  blockID(a.id),
						Message:  `Transition points at "` + jsString(target) + `", which does not exist.`,
					})
				}
			}
		}

		live := reachable(ctx.Doc)
		for _, a := range actionsOf(ctx.Doc) {
			if !live[a.id] {
				ctx.Report(Report{
					Severity: SeverityWarning,
					BlockID:  blockID(a.id),
					Message:  "Action is not reachable from StartAction.",
				})
			}
		}
	},
}

// hasAction is byId.has(target): only a string can be a key.
func hasAction(byID map[string]action, target any) bool {
	id, ok := target.(string)
	if !ok {
		return false
	}
	_, has := byID[id]
	return has
}
