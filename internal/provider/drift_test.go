// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"

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
