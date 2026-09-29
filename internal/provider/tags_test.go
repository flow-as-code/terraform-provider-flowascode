// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/connect"
	ctypes "github.com/aws/aws-sdk-go-v2/service/connect/types"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/connectapi"
)

// A new tag with an empty value is sent: Connect allows empty values.
func TestSyncTagsSendsANewEmptyValue(t *testing.T) {
	ctx := context.Background()
	fake := connectapi.NewFake()
	out, err := fake.CreateContactFlow(ctx, &connect.CreateContactFlowInput{
		InstanceId: aws.String(instance), Name: aws.String("line"), Type: ctypes.ContactFlowTypeContactFlow,
		Content: aws.String(`{"Version":"2019-10-30","StartAction":"a","Actions":[]}`), Tags: map[string]string{"team": "a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := syncTags(ctx, fake, aws.ToString(out.ContactFlowArn), map[string]string{"team": "a"}, map[string]string{"team": "a", "env": ""}); err != nil {
		t.Fatal(err)
	}
	f, _ := fake.Flow(aws.ToString(out.ContactFlowId))
	if _, ok := f.Tags["env"]; !ok {
		t.Fatalf("live tags %v have no env", f.Tags)
	}
}

// A flow whose only tags are AWS-managed (aws:cloudformation:..., from a CDK
// deploy) plans no change to tags.
func TestAWSOnlyTagsLeaveNoDiff(t *testing.T) {
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
					if _, err := fake.TagResource(context.Background(), &connect.TagResourceInput{
						ResourceArn: f.Arn, Tags: map[string]string{"aws:cloudformation:stack-name": "s"},
					}); err != nil {
						t.Fatal(err)
					}
				},
				Config: greetingFlow(queue),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectEmptyPlan(),
				}},
			},
		},
	})
}

type deniedQueues struct{ *connectapi.Fake }

func (d deniedQueues) ListQueues(context.Context, *connect.ListQueuesInput, ...func(*connect.Options)) (*connect.ListQueuesOutput, error) {
	return nil, &ctypes.AccessDeniedException{Message: aws.String("not authorized to perform connect:ListQueues")}
}

type ignoreState struct{}

func (ignoreState) SetAttribute(context.Context, path.Path, interface{}) diag.Diagnostics { return nil }

// When the inventory cannot be listed, the warning says so, rather than only
// telling the user to bind ARNs by hand.
func TestReconstructSaysWhyTheInventoryFailed(t *testing.T) {
	r := &flowResource{kind: "flow", client: &connectapi.Client{Connect: deniedQueues{connectapi.NewFake()}, Region: "us-east-1"}}
	queue := "arn:aws:connect:us-east-1:111122223333:instance/" + instance + "/queue/front-desk"
	a := flowAttrs{InstanceID: types.StringValue(instance), Name: types.StringValue("line"),
		Type: types.StringValue("CONTACT_FLOW"), Refs: types.MapNull(types.StringType)}
	l := live{name: "line", typ: "CONTACT_FLOW", content: `{"Version":"2019-10-30","StartAction":"q","Actions":[` +
		`{"Identifier":"q","Type":"UpdateContactTargetQueue","Parameters":{"QueueId":"` + queue + `"},"Transitions":{"NextAction":"bye","Errors":[{"NextAction":"bye","ErrorType":"NoMatchingError"}],"Conditions":[]}},` +
		`{"Identifier":"bye","Type":"DisconnectParticipant","Parameters":{},"Transitions":{}}]}`}
	var diags diag.Diagnostics
	r.reconstruct(context.Background(), a, l, ignoreState{}, &diags)
	var detail string
	for _, d := range diags.Warnings() {
		detail += d.Detail()
	}
	if !strings.Contains(detail, "not authorized to perform connect:ListQueues") {
		t.Fatalf("warnings do not name the listing error: %q", detail)
	}
}
