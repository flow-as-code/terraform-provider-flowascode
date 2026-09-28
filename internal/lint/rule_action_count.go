// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package lint

import (
	"strconv"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/flowdoc"
)

// ActionCount is rules/action-count.ts: "No more than 250 Actions per flow."
// https://docs.aws.amazon.com/connect/latest/devguide/flow-language-example.html
// The FlowDoc schema says maxItems 250, but nothing on the authoring path
// runs the schema, so this rule is the check on the FlowDoc path.
var ActionCount = Rule{
	ID:          "action-count",
	Description: "A flow holds no more than " + strconv.Itoa(flowdoc.MaxActionsPerFlow) + " actions.",
	Check: func(ctx RuleContext) {
		count := len(actionsOf(ctx.Doc))
		if count > flowdoc.MaxActionsPerFlow {
			ctx.Report(Report{
				Severity: SeverityError,
				Message:  "Flow has " + strconv.Itoa(count) + " actions; Connect allows at most " + strconv.Itoa(flowdoc.MaxActionsPerFlow) + ".",
			})
		}
	},
}
