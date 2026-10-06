// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/connectapi"
)

// Through 0.1.2 the catalog gave GetParticipantInput's MaximumLength the
// kind integer, so maximum_length was a Number attribute and a state wrote
// it as a JSON number. flow-as-code 78b0a87 (C04) records the kind the
// console writes, integerString, so the attribute is a String, and the
// provider has no state upgraders and keeps schema version 0. The two tests
// here are what that costs a 0.1.x state: nothing at read, one in-place
// update at plan.

// storedInputFlow is a flow with the stored-input form of
// GetParticipantInput, maximum_length written as a number literal the way
// the hcl/roundtrip/stored-input golden writes it.
func storedInputFlow() string {
	return fmt.Sprintf(`
resource "flowascode_contact_flow" "ask" {
  instance_id = %q
  name        = "ask"
  type        = "CONTACT_FLOW"

  action {
    id   = "ask-postcode"
    next = "bye"
    get_participant_input {
      input_time_limit_seconds = 10
      input_validation = {
        custom_validation = {
          maximum_length = 5
        }
      }
      store_input = "True"
      text        = "Enter the five digit postcode of the address, then press pound."
    }
    error {
      type = "NoMatchingError"
      next = "bye"
    }
  }

  action {
    id = "bye"
    disconnect_participant {}
  }
}
`, instance)
}

// A 0.1.x state holding maximum_length as a JSON number reads under the
// String schema: Terraform hands the provider the raw state at the same
// schema version, the framework decodes it against the current schema, and
// tftypes reads a JSON number into a String as its digits, so no upgrader
// is needed. Offline: the protocol server is called directly.
func TestUpgradeStateReadsANumberMaximumLengthAsAString(t *testing.T) {
	ctx := context.Background()
	client := &connectapi.Client{Connect: connectapi.NewFake(), Region: "us-east-1"}
	server := providerserver.NewProtocol6(NewWithClient("test", client)())()
	schemas, err := server.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	flowType := schemas.ResourceSchemas["flowascode_contact_flow"].ValueType()
	raw := `{"id":"` + instance + `:flow-1","contact_flow_id":"flow-1","instance_id":"` + instance + `","name":"ask","type":"CONTACT_FLOW",` +
		`"action":[{"id":"ask-postcode","next":"bye","get_participant_input":{"input_time_limit_seconds":"10",` +
		`"input_validation":{"custom_validation":{"maximum_length":5}},"store_input":"True","text":"Enter."},` +
		`"error":[{"type":"NoMatchingError","next":"bye"}]},{"id":"bye","disconnect_participant":{}}]}`
	resp, err := server.UpgradeResourceState(ctx, &tfprotov6.UpgradeResourceStateRequest{
		TypeName: "flowascode_contact_flow", Version: 0, RawState: &tfprotov6.RawState{JSON: []byte(raw)},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range resp.Diagnostics {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			t.Fatalf("%s: %s", d.Summary, d.Detail)
		}
	}
	state, err := resp.UpgradedState.Unmarshal(flowType)
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]tftypes.Value
	var actions []tftypes.Value
	var first, input, validation, custom map[string]tftypes.Value
	var maximumLength string
	for _, step := range []struct {
		from tftypes.Value
		to   any
	}{
		{state, &top},
	} {
		if err := step.from.As(step.to); err != nil {
			t.Fatal(err)
		}
	}
	if err := top["action"].As(&actions); err != nil {
		t.Fatal(err)
	}
	if err := actions[0].As(&first); err != nil {
		t.Fatal(err)
	}
	if err := first["get_participant_input"].As(&input); err != nil {
		t.Fatal(err)
	}
	if err := input["input_validation"].As(&validation); err != nil {
		t.Fatal(err)
	}
	if err := validation["custom_validation"].As(&custom); err != nil {
		t.Fatal(err)
	}
	if err := custom["maximum_length"].As(&maximumLength); err != nil {
		t.Fatalf("maximum_length %s: %v", custom["maximum_length"], err)
	}
	if maximumLength != "5" {
		t.Errorf("maximum_length read as %q, want \"5\"", maximumLength)
	}
}

// A plan over a 0.1.x state (maximum_length a number, and the content and
// flowdoc it deployed holding MaximumLength as a JSON number) is one
// in-place update, not a replacement or a refusal: the block reads equal to
// the configuration, and content, content_hash and flowdoc change to the
// string form the catalog now records, which the next apply sends. The
// state is one this version applied, edited into the 0.1.x shape, with the
// fake holding the 0.1.x content as the live flow.
func TestPlanOverAZeroOneStateIsAnInPlaceUpdate(t *testing.T) {
	fake := connectapi.NewFake()
	config := storedInputFlow()
	applied := applyFiles(t, fake, map[string]string{"main.tf": config})

	var state map[string]any
	if err := json.Unmarshal([]byte(applied), &state); err != nil {
		t.Fatal(err)
	}
	resources := state["resources"].([]any)
	attrs := resources[0].(map[string]any)["instances"].([]any)[0].(map[string]any)["attributes"].(map[string]any)
	custom := attrs["action"].([]any)[0].(map[string]any)["get_participant_input"].(map[string]any)["input_validation"].(map[string]any)["custom_validation"].(map[string]any)
	if custom["maximum_length"] != "5" {
		t.Fatalf("this version wrote maximum_length as %#v", custom["maximum_length"])
	}
	custom["maximum_length"] = float64(5)
	numberForm := regexp.MustCompile(`"MaximumLength":(\s*)"5"`)
	content := attrs["content"].(string)
	if !numberForm.MatchString(content) {
		t.Fatalf("content holds no MaximumLength string: %s", content)
	}
	content = numberForm.ReplaceAllString(content, `"MaximumLength":${1}5`)
	attrs["content"] = content
	attrs["content_hash"] = hashOf(content)
	attrs["flowdoc"] = numberForm.ReplaceAllString(attrs["flowdoc"].(string), `"MaximumLength":${1}5`)
	edited, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	fake.SetContent(attrs["contact_flow_id"].(string), content)

	p, failed := planFilesWith(t, fake, map[string]string{"main.tf": config, "terraform.tfstate": string(edited)}, "flowascode_contact_flow.ask")
	if failed != "" {
		t.Fatalf("plan failed:\n%s", failed)
	}
	if want := []string{"update"}; !reflect.DeepEqual(p.Actions, want) {
		t.Fatalf("planned %v, want %v", p.Actions, want)
	}
	for _, attr := range []string{"content", "flowdoc"} {
		if s, _ := p.After[attr].(string); !strings.Contains(s, `"MaximumLength":"5"`) && !strings.Contains(s, `"MaximumLength": "5"`) {
			t.Errorf("planned %s does not hold MaximumLength as a string: %s", attr, s)
		}
	}
	block := p.After["action"].([]any)[0].(map[string]any)["get_participant_input"].(map[string]any)["input_validation"].(map[string]any)["custom_validation"].(map[string]any)
	if block["maximum_length"] != "5" {
		t.Errorf("planned maximum_length %#v, want \"5\"", block["maximum_length"])
	}
}
