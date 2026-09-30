// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/connectapi"
)

// Conditions built by a dynamic block over a value known only at apply: the
// plan holds flowdoc and content unknown, and apply produces the flow with
// those conditions instead of failing with an inconsistent final plan.
func TestDynamicConditionsKnownOnlyAtApply(t *testing.T) {
	terraformBinary(t)
	fake := connectapi.NewFake()
	config := fmt.Sprintf(`
resource "terraform_data" "tiers" {
  input = [["gold"], ["silver"]]
}

resource "flowascode_contact_flow" "line" {
  instance_id = %q
  name        = "line"
  type        = "CONTACT_FLOW"

  action {
    id   = "check"
    next = "bye"
    compare {
      comparison_value = "$.Attributes.tier"
    }
    dynamic "condition" {
      for_each = terraform_data.tiers.output
      content {
        operator = "Equals"
        operands = condition.value
        next     = "bye"
      }
    }
    error {
      type = "NoMatchingCondition"
      next = "bye"
    }
  }

  action {
    id = "bye"
    disconnect_participant {}
  }
}
`, instance)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories(fake),
		Steps: []resource.TestStep{{
			Config: config,
			Check: resource.ComposeTestCheckFunc(
				resource.TestCheckResourceAttrWith("flowascode_contact_flow.line", "content", func(content string) error {
					for _, tier := range []string{`"gold"`, `"silver"`} {
						if !strings.Contains(content, tier) {
							return fmt.Errorf("content has no %s condition", tier)
						}
					}
					return nil
				}),
			),
		}},
	})
}
