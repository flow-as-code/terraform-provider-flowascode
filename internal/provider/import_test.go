// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	ctypes "github.com/aws/aws-sdk-go-v2/service/connect/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/connectapi"
)

// An imported flow reads back to the configuration that made it: its actions
// as blocks, and its refs bindings recovered from the instance's inventory,
// since an import starts with none (the queue here is named front-desk, so
// its key is queue:front-desk).
func TestImportRecoversBlocksAndBindings(t *testing.T) {
	terraformBinary(t)
	fake := connectapi.NewFake()
	queue := "arn:aws:connect:us-east-1:111122223333:instance/" + instance + "/queue/front-desk"
	fake.SetQueues([]ctypes.QueueSummary{{Id: aws.String("front-desk"), Name: aws.String("front-desk"), Arn: aws.String(queue), QueueType: ctypes.QueueTypeStandard}})
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories(fake),
		Steps: []resource.TestStep{
			{Config: greetingFlow(queue)},
			{
				ResourceName:      "flowascode_contact_flow.line",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}
