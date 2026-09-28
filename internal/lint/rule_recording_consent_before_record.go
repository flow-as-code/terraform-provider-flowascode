// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package lint

import (
	"github.com/flow-as-code/terraform-provider-flowascode/internal/flowdoc"
	"github.com/flow-as-code/terraform-provider-flowascode/internal/jsonv"
)

// enablesRecording is recording-consent-before-record.ts's
// enablesRecording: the catalog's recordingEnabler list is present and
// non-empty ("An empty list disables recording").
func enablesRecording(a action) bool {
	p, ok := flowdoc.RecordingEnablerPath(a.typ)
	if !ok {
		return false
	}
	hits, _ := flowdoc.ReadPath(a.parameters, p)
	for _, hit := range hits {
		if list, ok := hit.Value.([]any); ok && len(list) > 0 {
			return true
		}
	}
	return false
}

// announces is recording-consent-before-record.ts's announces: some
// catalog announce path holds a string that is not blank once trimmed as
// String.prototype.trim trims. Content counts, not the key: the studio
// writes Text: "" until the author types.
// https://docs.aws.amazon.com/connect/latest/devguide/participant-actions-messageparticipant.html
// https://docs.aws.amazon.com/connect/latest/devguide/participant-actions-getparticipantinput.html
func announces(a action) bool {
	for _, p := range flowdoc.AnnouncePaths(a.typ) {
		hits, _ := flowdoc.ReadPath(a.parameters, p)
		for _, hit := range hits {
			if s, ok := hit.Value.(string); ok && jsTrim(s) != "" {
				return true
			}
		}
	}
	return false
}

// pathsWithoutAnnouncement is recording-consent-before-record.ts's function
// of that name: whether targetID is reachable from StartAction without
// passing an action that announces. Reaching the target is checked before
// the announcement, so a recording StartAction is reported even when it
// announces.
func pathsWithoutAnnouncement(doc jsonv.Object, targetID string) bool {
	byID := actionsByID(doc)
	seen := map[string]bool{}
	queue := []any{startAction(doc)}
	for len(queue) > 0 {
		next := queue[0]
		queue = queue[1:]
		id, isString := next.(string)
		if !isString {
			continue
		}
		if id == targetID {
			return true
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		a, ok := byID[id]
		if !ok {
			continue
		}
		// An announcement on this path satisfies the rule, so stop exploring it.
		if announces(a) {
			continue
		}
		queue = append(queue, transitionTargets(a)...)
	}
	return false
}

// RecordingConsentBeforeRecord is rules/recording-consent-before-record.ts:
// every path reaching an action that enables recording passes through an
// action that plays the participant something first.
var RecordingConsentBeforeRecord = Rule{
	ID:          "recording-consent-before-record",
	Description: "Recording must not start before the participant has been played a message.",
	Check: func(ctx RuleContext) {
		for _, a := range actionsOf(ctx.Doc) {
			if !enablesRecording(a) {
				continue
			}
			if pathsWithoutAnnouncement(ctx.Doc, a.id) {
				ctx.Report(Report{
					Severity: SeverityError,
					BlockID:  blockID(a.id),
					Message:  "Recording is enabled on a path that plays no message first. Announce recording before starting it.",
				})
			}
		}
	},
}
