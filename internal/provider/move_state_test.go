// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// hashicorp/aws's aws_connect_contact_flow state, schema version 0, as
// `terraform state pull` shows it (placeholder ids).
const awsFlowState = `{
  "id": "11111111-2222-3333-4444-555555555555:aaaaaaaa-0000-4000-8000-000000000001",
  "arn": "arn:aws:connect:us-east-1:111122223333:instance/11111111-2222-3333-4444-555555555555/contact-flow/aaaaaaaa-0000-4000-8000-000000000001",
  "contact_flow_id": "aaaaaaaa-0000-4000-8000-000000000001",
  "content": "{\"Version\":\"2019-10-30\",\"StartAction\":\"bye\",\"Actions\":[{\"Identifier\":\"bye\",\"Type\":\"DisconnectParticipant\",\"Parameters\":{},\"Transitions\":{}}]}",
  "content_hash": null,
  "description": "",
  "filename": null,
  "instance_id": "11111111-2222-3333-4444-555555555555",
  "name": "line",
  "tags": {"team": "cx"},
  "tags_all": {"team": "cx"},
  "type": "CONTACT_FLOW"
}`

func moveInto(t *testing.T, r *flowResource, sourceType, provider string, version int64) (*resource.MoveStateResponse, tfsdk.State) {
	t.Helper()
	ctx := context.Background()
	var sr resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sr)
	target := tfsdk.State{Schema: sr.Schema, Raw: tftypes.NewValue(sr.Schema.Type().TerraformType(ctx), nil)}
	resp := &resource.MoveStateResponse{TargetState: target}
	req := resource.MoveStateRequest{
		SourceTypeName: sourceType, SourceProviderAddress: provider, SourceSchemaVersion: version,
		SourceRawState: &tfprotov6.RawState{JSON: []byte(awsFlowState)},
	}
	for _, m := range r.MoveState(ctx) {
		m.StateMover(ctx, req, resp)
	}
	return resp, resp.TargetState
}

func TestMoveStateFromHashicorpAws(t *testing.T) {
	ctx := context.Background()
	resp, state := moveInto(t, &flowResource{kind: "flow"}, "aws_connect_contact_flow", "registry.terraform.io/hashicorp/aws", 0)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	for name, want := range map[string]string{
		"id":              "11111111-2222-3333-4444-555555555555:aaaaaaaa-0000-4000-8000-000000000001",
		"contact_flow_id": "aaaaaaaa-0000-4000-8000-000000000001",
		"instance_id":     "11111111-2222-3333-4444-555555555555",
		"name":            "line",
		"type":            "CONTACT_FLOW",
	} {
		var got types.String
		resp.Diagnostics.Append(state.GetAttribute(ctx, path.Root(name), &got)...)
		if got.ValueString() != want {
			t.Errorf("%s = %q, want %q", name, got.ValueString(), want)
		}
	}
	var desc types.String
	state.GetAttribute(ctx, path.Root("description"), &desc)
	if !desc.IsNull() {
		t.Errorf("an empty description moves as null, got %q", desc.ValueString())
	}
	var hash types.String
	state.GetAttribute(ctx, path.Root("content_hash"), &hash)
	if len(hash.ValueString()) != 64 {
		t.Errorf("content_hash = %q", hash.ValueString())
	}
	// OpenTofu's registry address is accepted too, and so is the bare type
	// OpenTofu 1.10 sends (opentofu/opentofu#4352), which the live moved
	// test showed on 1.10.10 on 2026-09-28.
	for _, addr := range []string{"registry.opentofu.org/hashicorp/aws", "aws"} {
		if resp, _ := moveInto(t, &flowResource{kind: "flow"}, "aws_connect_contact_flow", addr, 0); resp.TargetState.Raw.IsNull() {
			t.Errorf("a move from %q was not taken", addr)
		}
	}
}

func TestMoveStateTakesOnlyWhatItKnows(t *testing.T) {
	for _, c := range []struct {
		kind, source, provider string
		version                int64
	}{
		{"flow", "aws_connect_queue", "registry.terraform.io/hashicorp/aws", 0},
		{"flow", "aws_connect_contact_flow", "registry.terraform.io/example/aws", 0},
		{"flow", "aws_connect_contact_flow", "registry.terraform.io/nothashicorp/aws", 0},
		{"flow", "aws_connect_contact_flow", "awscc", 0},
		{"flow", "aws_connect_contact_flow", "registry.terraform.io/hashicorp/aws", 1},
		{"module", "aws_connect_contact_flow", "registry.terraform.io/hashicorp/aws", 0},
	} {
		resp, _ := moveInto(t, &flowResource{kind: c.kind}, c.source, c.provider, c.version)
		if !resp.TargetState.Raw.IsNull() {
			t.Errorf("%+v was taken", c)
		}
	}
}
