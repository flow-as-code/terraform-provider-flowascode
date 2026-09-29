// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/connectapi"
)

func greetingFlow(queueARN string) string {
	return fmt.Sprintf(`
resource "flowascode_contact_flow" "line" {
  instance_id = %[1]q
  name        = "line"
  type        = "CONTACT_FLOW"

  refs = {
    "queue:front-desk" = %[2]q
  }

  action {
    id   = "welcome"
    next = "to-queue"
    message_participant {
      text = "Welcome."
    }
    error {
      type = "NoMatchingError"
      next = "bye"
    }
  }

  action {
    id   = "to-queue"
    next = "bye"
    update_contact_target_queue {
      queue_id = "queue:front-desk"
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
`, instance, queueARN)
}

// A console edit to the live flow shows in the plan as the blocks that
// changed, read back through the refs bindings, and the next apply puts the
// configuration back.
func TestDriftShowsAsBlocksAndIsCorrected(t *testing.T) {
	terraformBinary(t)
	fake := connectapi.NewFake()
	queue := "arn:aws:connect:us-east-1:111122223333:instance/" + instance + "/queue/front-desk"
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories(fake),
		Steps: []resource.TestStep{
			{Config: greetingFlow(queue)},
			{
				PreConfig: func() {
					f, _ := fake.Flow("flow-1")
					fake.SetContent("flow-1", strings.Replace(*f.Content, "Welcome.", "Edited in the console.", 1))
				},
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("flowascode_contact_flow.line", "action.0.message_participant.text", "Edited in the console."),
					resource.TestCheckResourceAttr("flowascode_contact_flow.line", "action.1.update_contact_target_queue.queue_id", "queue:front-desk"),
				),
			},
			{
				Config: greetingFlow(queue),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("flowascode_contact_flow.line", plancheck.ResourceActionUpdate),
				}},
			},
		},
	})
}

// Moving blocks in the console changes only the content's Metadata, which is
// layout, not behavior: the next plan is empty and content_hash, which a
// module version is keyed to, does not move.
func TestLayoutIsNotDrift(t *testing.T) {
	terraformBinary(t)
	fake := connectapi.NewFake()
	queue := "arn:aws:connect:us-east-1:111122223333:instance/" + instance + "/queue/front-desk"
	var hash string
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories(fake),
		Steps: []resource.TestStep{
			{
				Config: greetingFlow(queue),
				Check: func(s *terraform.State) error {
					hash = s.RootModule().Resources["flowascode_contact_flow.line"].Primary.Attributes["content_hash"]
					return nil
				},
			},
			{
				PreConfig: func() {
					f, _ := fake.Flow("flow-1")
					var content map[string]any
					if err := json.Unmarshal([]byte(*f.Content), &content); err != nil {
						t.Fatal(err)
					}
					content["Metadata"] = map[string]any{"ActionMetadata": map[string]any{
						"welcome": map[string]any{"position": map[string]any{"x": 999, "y": 777}},
					}}
					moved, _ := json.Marshal(content)
					if string(moved) == *f.Content {
						t.Fatal("the Metadata edit changed nothing")
					}
					fake.SetContent("flow-1", string(moved))
				},
				Config: greetingFlow(queue),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectEmptyPlan(),
				}},
				Check: func(s *terraform.State) error {
					if got := s.RootModule().Resources["flowascode_contact_flow.line"].Primary.Attributes["content_hash"]; hash == "" || got != hash {
						return fmt.Errorf("content_hash moved from %q to %q", hash, got)
					}
					return nil
				},
			},
		},
	})
}
