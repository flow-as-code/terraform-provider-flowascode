// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"fmt"
	"regexp"
	"testing"

	tfresource "github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/connectapi"
)

// Connect refuses content with InvalidContactFlowException, whose message is
// often empty and whose problems say what is wrong (sandbox, 2026-09-29). The
// apply error has to carry them, or the author sees "InvalidContactFlowException: "
// and nothing else. A transfer without its QueueAtCapacity branch is the
// refusal the fake models; lint warns about it at plan time (error-branches
// is not a hard rule) and the service refuses it at apply.
func TestConnectRefusalNamesTheProblem(t *testing.T) {
	terraformBinary(t)
	config := fmt.Sprintf(`
resource "flowascode_contact_flow" "line" {
  instance_id = %q
  name        = "line"
  type        = "CONTACT_FLOW"

  action {
    id   = "set-queue"
    next = "transfer"
    update_contact_target_queue {
      queue_id = "$.Attributes.queueArn"
    }
    error {
      type = "NoMatchingError"
      next = "bye"
    }
  }

  action {
    id   = "transfer"
    next = "bye"
    transfer_contact_to_queue {}
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
	tfresource.UnitTest(t, tfresource.TestCase{
		ProtoV6ProviderFactories: factories(connectapi.NewFake()),
		Steps: []tfresource.TestStep{{
			Config:      config,
			ExpectError: regexp.MustCompile(`(?s)Amazon Connect refused the content:.*Action is missing required error\. Error: QueueAtCapacity, Path:\s+Actions\[1\]`),
		}},
	})
}
