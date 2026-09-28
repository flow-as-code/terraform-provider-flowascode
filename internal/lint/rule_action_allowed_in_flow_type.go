// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package lint

import (
	"strings"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/flowdoc"
)

// ActionAllowedInFlowType is rules/action-allowed-in-flow-type.ts. Almost
// every Connect action documents a Restrictions section naming the flow
// types it is legal in; deploying a flow that violates one fails at create
// time with a message that does not say which block is at fault.
//
// The TypeScript indexes FLOW_TYPE_RESTRICTIONS, a plain object, with the
// action's Type, so a Type naming an Object.prototype property
// ("constructor", "toString") throws a TypeError there. The catalog lookup
// here has no such entries and reports nothing for them.
var ActionAllowedInFlowType = Rule{
	ID:          "action-allowed-in-flow-type",
	Description: "Each action must be legal in the flow type that contains it.",
	Check: func(ctx RuleContext) {
		connectType := str(ctx.Doc, "connectType")
		for _, a := range actionsOf(ctx.Doc) {
			allowed, ok := flowdoc.FlowTypeRestrictions(a.typ)
			if !ok {
				continue
			}
			if !contains(allowed, connectType) {
				ctx.Report(Report{
					Severity: SeverityError,
					BlockID:  blockID(a.id),
					Message:  a.typ + " is not allowed in a " + connectType + " flow. Allowed: " + strings.Join(allowed, ", ") + ".",
				})
			}
		}
	},
}

func contains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}
