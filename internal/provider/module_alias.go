// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/connect"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/connectapi"
)

// moduleAlias is flowascode_contact_flow_module_alias: a named pointer at a
// module version, which a flow binds a module:<name>@<alias> key to (rule
// 27). Pointing it at another version is an update in place.
type moduleAlias struct{ client *connectapi.Client }

// NewContactFlowModuleAlias is the resource factory.
func NewContactFlowModuleAlias() resource.Resource { return &moduleAlias{} }

var (
	_ resource.ResourceWithConfigure   = (*moduleAlias)(nil)
	_ resource.ResourceWithImportState = (*moduleAlias)(nil)
)

type moduleAliasModel struct {
	ID          types.String `tfsdk:"id"`
	InstanceID  types.String `tfsdk:"instance_id"`
	ModuleID    types.String `tfsdk:"contact_flow_module_id"`
	Name        types.String `tfsdk:"name"`
	Version     types.Int64  `tfsdk:"contact_flow_module_version"`
	Description types.String `tfsdk:"description"`
	AliasID     types.String `tfsdk:"alias_id"`
	ARN         types.String `tfsdk:"arn"`
}

func (r *moduleAlias) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_contact_flow_module_alias"
}

func (r *moduleAlias) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A named alias of a flow module version, the ARN a flow invokes the module through.",
		Attributes: map[string]schema.Attribute{
			"id":                          schema.StringAttribute{Computed: true, PlanModifiers: keep(), Description: "instance_id:contact_flow_module_id:alias_id."},
			"instance_id":                 schema.StringAttribute{Required: true, PlanModifiers: replace()},
			"contact_flow_module_id":      schema.StringAttribute{Required: true, PlanModifiers: replace()},
			"name":                        schema.StringAttribute{Required: true, PlanModifiers: replace(), Description: "The alias, as a module:<name>@<alias> key spells it."},
			"contact_flow_module_version": schema.Int64Attribute{Required: true, Description: "The version the alias points at."},
			"description":                 schema.StringAttribute{Optional: true},
			"alias_id":                    schema.StringAttribute{Computed: true, PlanModifiers: keep()},
			"arn":                         schema.StringAttribute{Computed: true, PlanModifiers: keep()},
		},
	}
}

func (r *moduleAlias) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	if c, ok := req.ProviderData.(*connectapi.Client); ok {
		r.client = c
	}
}

func (r *moduleAlias) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m moduleAliasModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	in := &connect.CreateContactFlowModuleAliasInput{
		InstanceId: aws.String(m.InstanceID.ValueString()), ContactFlowModuleId: aws.String(m.ModuleID.ValueString()),
		AliasName: aws.String(m.Name.ValueString()), ContactFlowModuleVersion: aws.Int64(m.Version.ValueInt64())}
	if !m.Description.IsNull() {
		in.Description = aws.String(m.Description.ValueString())
	}
	out, err := r.client.Connect.CreateContactFlowModuleAlias(ctx, in)
	if err != nil {
		resp.Diagnostics.AddError("CreateContactFlowModuleAlias failed", err.Error())
		return
	}
	m.AliasID = types.StringValue(aws.ToString(out.Id))
	m.ARN = types.StringValue(aws.ToString(out.ContactFlowModuleArn))
	m.ID = types.StringValue(fmt.Sprintf("%s:%s:%s", m.InstanceID.ValueString(), m.ModuleID.ValueString(), m.AliasID.ValueString()))
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *moduleAlias) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m moduleAliasModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	out, err := r.client.Connect.DescribeContactFlowModuleAlias(ctx, &connect.DescribeContactFlowModuleAliasInput{
		InstanceId: aws.String(m.InstanceID.ValueString()), ContactFlowModuleId: aws.String(m.ModuleID.ValueString()),
		AliasId: aws.String(m.AliasID.ValueString())})
	if connectapi.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("DescribeContactFlowModuleAlias failed", err.Error())
		return
	}
	a := out.ContactFlowModuleAlias
	m.Name = types.StringValue(aws.ToString(a.Name))
	m.Version = types.Int64Value(aws.ToInt64(a.Version))
	if a.Description != nil && *a.Description != "" {
		m.Description = types.StringValue(*a.Description)
	} else {
		m.Description = types.StringNull()
	}
	if a.ContactFlowModuleArn != nil {
		m.ARN = types.StringValue(*a.ContactFlowModuleArn)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *moduleAlias) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m, old moduleAliasModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &old)...)
	if resp.Diagnostics.HasError() {
		return
	}
	in := &connect.UpdateContactFlowModuleAliasInput{
		InstanceId: aws.String(old.InstanceID.ValueString()), ContactFlowModuleId: aws.String(old.ModuleID.ValueString()),
		AliasId: aws.String(old.AliasID.ValueString()), ContactFlowModuleVersion: aws.Int64(m.Version.ValueInt64()),
		Description: aws.String(m.Description.ValueString())}
	if _, err := r.client.Connect.UpdateContactFlowModuleAlias(ctx, in); err != nil {
		resp.Diagnostics.AddError("UpdateContactFlowModuleAlias failed", err.Error())
		return
	}
	m.ID, m.AliasID, m.ARN = old.ID, old.AliasID, old.ARN
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *moduleAlias) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m moduleAliasModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	_, err := r.client.Connect.DeleteContactFlowModuleAlias(ctx, &connect.DeleteContactFlowModuleAliasInput{
		InstanceId: aws.String(m.InstanceID.ValueString()), ContactFlowModuleId: aws.String(m.ModuleID.ValueString()),
		AliasId: aws.String(m.AliasID.ValueString())})
	if err != nil && !connectapi.IsNotFound(err) {
		resp.Diagnostics.AddError("DeleteContactFlowModuleAlias failed", err.Error())
	}
}

func (r *moduleAlias) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, ":")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		resp.Diagnostics.AddError("Invalid import id", fmt.Sprintf("Expected instance_id:contact_flow_module_id:alias_id, got %q.", req.ID))
		return
	}
	for name, v := range map[string]string{"id": req.ID, "instance_id": parts[0], "contact_flow_module_id": parts[1], "alias_id": parts[2]} {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(name), v)...)
	}
}
