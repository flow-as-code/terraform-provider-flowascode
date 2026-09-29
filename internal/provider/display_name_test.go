// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/connect"
	ctypes "github.com/aws/aws-sdk-go-v2/service/connect/types"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	tfresource "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/connectapi"
)

func namedFlow(displayName string) string {
	line := ""
	if displayName != "" {
		line = fmt.Sprintf("  display_name = %q\n", displayName)
	}
	return fmt.Sprintf(`
resource "flowascode_contact_flow" "line" {
  instance_id = %q
  name        = "main-line"
%s  type        = "CONTACT_FLOW"

  action {
    id = "bye"
    disconnect_participant {}
  }
}
`, instance, line)
}

// display_name is what Connect calls the flow; name stays the slug. Renaming
// through display_name updates the flow in place, and a rename in the console
// shows as drift in display_name.
func TestDisplayNameNamesTheFlowInConnect(t *testing.T) {
	terraformBinary(t)
	fake := connectapi.NewFake()
	connectNameIs := func(want string) func(*terraform.State) error {
		return func(*terraform.State) error {
			f, _ := fake.Flow("flow-1")
			if got := aws.ToString(f.Name); got != want {
				return fmt.Errorf("Connect calls the flow %q, want %q", got, want)
			}
			return nil
		}
	}
	tfresource.UnitTest(t, tfresource.TestCase{
		ProtoV6ProviderFactories: factories(fake),
		Steps: []tfresource.TestStep{
			{Config: namedFlow("Main Line"), Check: tfresource.ComposeTestCheckFunc(
				connectNameIs("Main Line"),
				tfresource.TestCheckResourceAttr("flowascode_contact_flow.line", "name", "main-line"),
			)},
			{
				Config: namedFlow("Main Line (days)"),
				ConfigPlanChecks: tfresource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("flowascode_contact_flow.line", plancheck.ResourceActionUpdate),
				}},
				Check: connectNameIs("Main Line (days)"),
			},
			{
				// Without display_name, Connect's name is name again.
				Config: namedFlow(""),
				Check:  connectNameIs("main-line"),
			},
			{
				PreConfig: func() {
					if _, err := fake.UpdateContactFlowMetadata(context.Background(), &connect.UpdateContactFlowMetadataInput{
						InstanceId: aws.String(instance), ContactFlowId: aws.String("flow-1"), Name: aws.String("Renamed In Console"),
					}); err != nil {
						t.Fatal(err)
					}
				},
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
				Check:              tfresource.TestCheckResourceAttr("flowascode_contact_flow.line", "display_name", "Renamed In Console"),
			},
			{Config: namedFlow(""), Check: connectNameIs("main-line")},
		},
	})
}

// Importing a flow whose Connect name is not a slug takes the slug as name
// and keeps the Connect name as display_name, so the next plan renames
// nothing.
func TestImportKeepsANonSlugName(t *testing.T) {
	terraformBinary(t)
	fake := connectapi.NewFake()
	tfresource.UnitTest(t, tfresource.TestCase{
		ProtoV6ProviderFactories: factories(fake),
		Steps: []tfresource.TestStep{
			{
				PreConfig: func() {
					if _, err := fake.CreateContactFlow(context.Background(), &connect.CreateContactFlowInput{
						InstanceId: aws.String(instance), Name: aws.String("Main Line"), Type: ctypes.ContactFlowTypeContactFlow,
						Content: aws.String(`{"Version":"2019-10-30","StartAction":"bye","Actions":[{"Identifier":"bye","Type":"DisconnectParticipant","Parameters":{},"Transitions":{}}]}`),
					}); err != nil {
						t.Fatal(err)
					}
				},
				Config:             namedFlow("Main Line"),
				ResourceName:       "flowascode_contact_flow.line",
				ImportState:        true,
				ImportStateId:      instance + ":flow-1",
				ImportStatePersist: true,
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					a := states[0].Attributes
					if a["name"] != "main-line" || a["display_name"] != "Main Line" {
						return fmt.Errorf("imported name %q display_name %q", a["name"], a["display_name"])
					}
					return nil
				},
			},
			{
				// The first apply after an import sends the configured content
				// (state holds Connect's own text); the name is untouched.
				Config: namedFlow("Main Line"),
				Check: func(*terraform.State) error {
					f, _ := fake.Flow("flow-1")
					if got := aws.ToString(f.Name); got != "Main Line" {
						return fmt.Errorf("the apply renamed the flow to %q", got)
					}
					return nil
				},
			},
		},
	})
}

// A moved aws_connect_contact_flow whose name is not a slug keeps it as
// display_name.
func TestMoveStateKeepsANonSlugName(t *testing.T) {
	ctx := context.Background()
	r := &flowResource{kind: "flow"}
	var sr resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sr)
	resp := &resource.MoveStateResponse{TargetState: tfsdk.State{Schema: sr.Schema, Raw: tftypes.NewValue(sr.Schema.Type().TerraformType(ctx), nil)}}
	raw := strings.Replace(awsFlowState, `"name": "line"`, `"name": "Main Line"`, 1)
	if raw == awsFlowState {
		t.Fatal("the fixture's name was not replaced")
	}
	req := resource.MoveStateRequest{
		SourceTypeName: "aws_connect_contact_flow", SourceProviderAddress: "registry.terraform.io/hashicorp/aws",
		SourceRawState: &tfprotov6.RawState{JSON: []byte(raw)},
	}
	for _, m := range r.MoveState(ctx) {
		m.StateMover(ctx, req, resp)
	}
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	var name, display types.String
	resp.TargetState.GetAttribute(ctx, path.Root("name"), &name)
	resp.TargetState.GetAttribute(ctx, path.Root("display_name"), &display)
	if name.ValueString() != "main-line" || display.ValueString() != "Main Line" {
		t.Fatalf("moved name %q display_name %q", name.ValueString(), display.ValueString())
	}
}
