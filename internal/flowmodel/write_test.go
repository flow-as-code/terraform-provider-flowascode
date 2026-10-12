// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package flowmodel

import (
	"math/big"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/flowdoc"
	"github.com/flow-as-code/terraform-provider-flowascode/internal/jsonv"
)

// Positions round the way the TypeScript writer's Math.round does.
func TestJSRoundIsMathRound(t *testing.T) {
	for v, want := range map[float64]float64{
		-40.5: -40, -7.5: -7, 40.5: 41, 2.4: 2, -2.6: -3, 0.49999999999999994: 0, -0.5: 0,
	} {
		if got := JSRound(v); got != want {
			t.Errorf("JSRound(%v) = %v, want %v", v, got, want)
		}
	}
}

func TestActionsFromDocRoundsANegativeHalfUp(t *testing.T) {
	v, err := jsonv.Decode([]byte(`{"content":{"Version":"2019-10-30","StartAction":"a","Actions":[{"Identifier":"a","Type":"DisconnectParticipant","Parameters":{},"Transitions":{}}]},"layout":{"a":{"x":-40.5,"y":-7.5}}}`))
	if err != nil {
		t.Fatal(err)
	}
	actions := ActionsFromDoc(v.(jsonv.Object))
	pos, _ := actions[0].(map[string]any)["position"].(map[string]any)
	x, _ := pos["x"].(*big.Float).Float64()
	y, _ := pos["y"].(*big.Float).Float64()
	if x != -40 || y != -7 {
		t.Fatalf("position (%v, %v), want (-40, -7)", x, y)
	}
}

// A list the catalog marks dynamic (FlowDoc 0.3, ADR 0009) stays a list
// attribute, and a JSONPath standing for the whole list is not accepted
// typed, so the action is written generic (conformance/hcl/README.md, rule
// 11) rather than as a list attribute holding a string.
func TestDynamicListStaysAListAndAJSONPathGoesGeneric(t *testing.T) {
	list := flowdoc.CatalogParameter{Key: "Steps", Attr: "steps", CatalogElement: flowdoc.CatalogElement{Kind: "list", Dynamic: true}}
	if _, ok := attribute(list.CatalogElement, "d").(schema.ListAttribute); !ok {
		t.Errorf("a dynamic list is %T", attribute(list.CatalogElement, "d"))
	}
	elems := []flowdoc.CatalogParameter{list}
	if !Accepts(jsonv.Object{{Key: "Steps", Value: []any{"a"}}}, elems) {
		t.Error("a static list was refused")
	}
	if Accepts(jsonv.Object{{Key: "Steps", Value: "$.Attributes.steps"}}, elems) {
		t.Error("a JSONPath in a list position was accepted typed")
	}
}

// A reference in key or item position (rule 11, since flow-as-code D06): a
// casefield token is written as its reference key and a system field id as
// itself; a token of another type, or a string that reads as a reference
// key, writes the action generic.
func TestReferenceKeysAndItemsAreWrittenAsKeys(t *testing.T) {
	action := func(fields jsonv.Object, response []any) jsonv.Object {
		return jsonv.Object{
			{Key: "Identifier", Value: "c"}, {Key: "Type", Value: "GetCase"},
			{Key: "Parameters", Value: jsonv.Object{
				{Key: "LinkContactToCase", Value: "false"}, {Key: "GetLastUpdatedCase", Value: "true"},
				{Key: "CaseRequestFields", Value: fields}, {Key: "CaseResponseFields", Value: response},
			}},
			{Key: "Transitions", Value: jsonv.Object{}},
		}
	}
	typed := actionValue(action(
		jsonv.Object{{Key: "${cdref:casefield:priority}", Value: "high"}, {Key: "status", Value: "open"}},
		[]any{"${cdref:casefield:priority}", "summary"}), nil, nil)
	block, ok := typed["get_case"].(map[string]any)
	if !ok {
		t.Fatalf("written %v", typed)
	}
	fields := block["case_request_fields"].(map[string]any)
	if fields["casefield:priority"] != "high" || fields["status"] != "open" || len(fields) != 2 {
		t.Errorf("case_request_fields %v", fields)
	}
	if items := block["case_response_fields"].([]any); items[0] != "casefield:priority" || items[1] != "summary" {
		t.Errorf("case_response_fields %v", items)
	}
	for _, fields := range []jsonv.Object{
		{{Key: "${cdref:queue:q}", Value: "x"}},
		{{Key: "casefield:priority", Value: "x"}},
		{{Key: "x-${cdref:casefield:p}", Value: "x"}},
	} {
		if got := actionValue(action(fields, []any{}), nil, nil); got["generic"] == nil {
			t.Errorf("%v written typed: %v", fields, got)
		}
	}
}
