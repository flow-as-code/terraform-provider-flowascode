// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.ResourceWithMoveState = (*flowResource)(nil)

// MoveState takes over an aws_connect_contact_flow (for a flow) or an
// aws_connect_contact_flow_module (for a module) from hashicorp/aws in a
// `moved` block, which needs Terraform 1.8 or OpenTofu 1.10.
// https://developer.hashicorp.com/terraform/plugin/framework/resources/state-move
//
// The move is pure: it copies the identity and the live attributes, and
// leaves the actions for the next plan to write from the configuration. The
// content is the live content, so a configuration that describes the same
// flow updates nothing in Connect.
func (r *flowResource) MoveState(context.Context) []resource.StateMover {
	source := "aws_connect_contact_flow"
	if r.module() {
		source = "aws_connect_contact_flow_module"
	}
	return []resource.StateMover{{
		StateMover: func(ctx context.Context, req resource.MoveStateRequest, resp *resource.MoveStateResponse) {
			if req.SourceTypeName != source || !strings.HasSuffix(req.SourceProviderAddress, "hashicorp/aws") ||
				req.SourceSchemaVersion != 0 || req.SourceRawState == nil {
				return
			}
			var old map[string]any
			if err := json.Unmarshal(req.SourceRawState.JSON, &old); err != nil {
				resp.Diagnostics.AddError("Cannot read the "+source+" state", err.Error())
				return
			}
			str := func(k string) types.String {
				if s, ok := old[k].(string); ok && s != "" {
					return types.StringValue(s)
				}
				return types.StringNull()
			}
			set := func(name string, v any) {
				resp.Diagnostics.Append(resp.TargetState.SetAttribute(ctx, path.Root(name), v)...)
			}
			set("id", str("id"))
			set(r.idAttr(), str(r.idAttr()))
			set("arn", str("arn"))
			set("instance_id", str("instance_id"))
			set("name", str("name"))
			set("description", str("description"))
			if !r.module() {
				set("type", str("type"))
			}
			if content, ok := old["content"].(string); ok && content != "" {
				set("content", types.StringValue(content))
				set("content_hash", types.StringValue(hashOf(content)))
			}
			if tags, ok := old["tags"].(map[string]any); ok && len(tags) > 0 {
				m := map[string]string{}
				for k, v := range tags {
					if s, ok := v.(string); ok {
						m[k] = s
					}
				}
				tv, d := types.MapValueFrom(ctx, types.StringType, m)
				resp.Diagnostics.Append(d...)
				set("tags", tv)
			}
		},
	}}
}
