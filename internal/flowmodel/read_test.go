// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package flowmodel

import (
	"testing"
)

func compareAction(condition any) map[string]any {
	return map[string]any{
		"id":        "check",
		"compare":   map[string]any{"comparison_value": "$.Attributes.tier"},
		"condition": condition,
		"error":     []any{map[string]any{"type": "NoMatchingCondition", "next": "bye"}},
	}
}

var bye = map[string]any{"id": "bye", "disconnect_participant": map[string]any{}}

// A value unknown until apply makes the document incomplete: never an
// error, and never read as if the value were empty.
func TestUnknownValuesMakeTheDocumentIncomplete(t *testing.T) {
	for _, c := range []struct {
		name string
		cfg  map[string]any
	}{
		{"an unknown condition list", map[string]any{"name": "line", "type": "CONTACT_FLOW",
			"action": []any{compareAction(Unknown{}), bye}}},
		{"an unknown condition", map[string]any{"name": "line", "type": "CONTACT_FLOW",
			"action": []any{compareAction([]any{Unknown{}}), bye}}},
		{"unknown operands", map[string]any{"name": "line", "type": "CONTACT_FLOW",
			"action": []any{compareAction([]any{map[string]any{"operator": "Equals", "operands": Unknown{}, "next": "bye"}}), bye}}},
		{"an unknown error list", map[string]any{"name": "line", "type": "CONTACT_FLOW",
			"action": []any{map[string]any{"id": "hi", "next": "bye", "message_participant": map[string]any{"text": "Hi."}, "error": Unknown{}}, bye}}},
		{"an unknown position", map[string]any{"name": "line", "type": "CONTACT_FLOW",
			"action": []any{map[string]any{"id": "bye", "disconnect_participant": map[string]any{}, "position": Unknown{}}}}},
		{"an unknown action list, with start", map[string]any{"name": "line", "type": "CONTACT_FLOW", "start": "bye",
			"action": Unknown{}}},
		{"an unknown action id, with start", map[string]any{"name": "line", "type": "CONTACT_FLOW", "start": "bye",
			"action": []any{map[string]any{"id": Unknown{}, "disconnect_participant": map[string]any{}}}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			for _, phase := range []Phase{PhaseValidate, PhasePlan} {
				res := FromConfig(c.cfg, "flow", phase)
				if len(res.Problems) > 0 {
					t.Errorf("phase %v: problems %v", phase, res.Problems)
				}
				if !res.Incomplete {
					t.Errorf("phase %v: not incomplete, doc %v", phase, res.Doc)
				}
			}
		})
	}
}

// With every id known, a start naming no action is still refused.
func TestStartNamingNoActionIsStillRefused(t *testing.T) {
	res := FromConfig(map[string]any{"name": "line", "type": "CONTACT_FLOW", "start": "nowhere",
		"action": []any{bye}}, "flow", PhaseValidate)
	if len(res.Problems) != 1 || res.Problems[0].Code != "START_UNKNOWN" {
		t.Fatalf("problems %v, want one START_UNKNOWN", res.Problems)
	}
}
