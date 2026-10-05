// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package lint

import (
	"strings"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/flowdoc"
)

// ChannelRestrictedAction is rules/channel-restricted-action.ts. Two modeled
// actions are restricted by channel rather than by flow type: Wait "is
// supported only by the chat channel" and ShowView "is only supported on
// the chat channel" (the catalog's channels, from each action's page). A
// contact flow's channel is decided by the contact that reaches it, not by
// the document, and FlowDoc records no channel, so the finding is a warning:
// an inbound flow may serve chat alone, and the document cannot say so. The
// message names the channels the page allows and the page itself.
//
// Added 2026-10-05 (flow-as-code tasks/C03). The severity rises to error
// only on a create the service refuses, recorded in actions.md rule 37.
var ChannelRestrictedAction = Rule{
	ID:          "channel-restricted-action",
	Description: "An action whose page restricts it to some channels is reported, since a document does not record which channels its flow serves.",
	Check: func(ctx RuleContext) {
		kind := str(ctx.Doc, "kind")
		for _, a := range actionsOf(ctx.Doc) {
			channels, ok := flowdoc.ChannelRestriction(a.typ)
			if !ok {
				continue
			}
			names := make([]string, len(channels))
			for i, c := range channels {
				names[i] = string(c)
			}
			plural := ""
			if len(channels) > 1 {
				plural = "s"
			}
			page := ""
			if e := flowdoc.ModeledEntry(a.typ); e != nil {
				page = e.Doc
			}
			ctx.Report(Report{
				Severity: SeverityWarning,
				BlockID:  blockID(a.id),
				Message: a.typ + " is supported only on the " + strings.Join(names, ", ") + " channel" + plural +
					", and the document does not record which channels this " + kind + " serves. " + page,
			})
		}
	},
}
