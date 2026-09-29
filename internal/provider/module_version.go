// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/connect"
	ctypes "github.com/aws/aws-sdk-go-v2/service/connect/types"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/connectapi"
)

// moduleVersion is flowascode_contact_flow_module_version: an immutable
// snapshot of a module's content (conformance/hcl/README.md, rule 27). It is
// keyed to the module's content_hash, so a change to the module replaces it.
// Connect refuses to delete a version an alias points at
// (InvalidRequestException, "Cannot delete version '1' tied to one alias",
// observed on the sandbox 2026-09-28), so an aliased version needs
// create_before_destroy: the alias moves to the new version before the old
// one goes.
type moduleVersion struct{ client *connectapi.Client }

// NewContactFlowModuleVersion is the resource factory.
func NewContactFlowModuleVersion() resource.Resource { return &moduleVersion{} }

var (
	_ resource.ResourceWithConfigure   = (*moduleVersion)(nil)
	_ resource.ResourceWithImportState = (*moduleVersion)(nil)
)

type moduleVersionModel struct {
	ID          types.String `tfsdk:"id"`
	InstanceID  types.String `tfsdk:"instance_id"`
	ModuleID    types.String `tfsdk:"contact_flow_module_id"`
	ContentHash types.String `tfsdk:"content_hash"`
	Description types.String `tfsdk:"description"`
	Version     types.Int64  `tfsdk:"version"`
	ARN         types.String `tfsdk:"arn"`
}

func (r *moduleVersion) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_contact_flow_module_version"
}

func replace() []planmodifier.String {
	return []planmodifier.String{stringplanmodifier.RequiresReplace()}
}

func (r *moduleVersion) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A published snapshot of a flow module, keyed to its content_hash. Connect does not delete a version an alias points at, so a version an alias uses needs lifecycle { create_before_destroy = true }: the replacement is created and the alias moved to it before the old version is destroyed. Import takes `instance_id:contact_flow_module_id:version` and records the module's current content_hash, since Connect returns no single version's content: the next plan is empty when the module still holds what that version snapshotted.",
		Attributes: map[string]schema.Attribute{
			"id":                     schema.StringAttribute{Computed: true, PlanModifiers: keep(), Description: "instance_id:contact_flow_module_id:version."},
			"instance_id":            schema.StringAttribute{Required: true, PlanModifiers: replace()},
			"contact_flow_module_id": schema.StringAttribute{Required: true, PlanModifiers: replace()},
			"content_hash": schema.StringAttribute{Required: true, PlanModifiers: replace(),
				Description: "The module's content_hash. A new hash replaces this version; the version is refused if the module's live content has a different one."},
			"description": schema.StringAttribute{Optional: true, PlanModifiers: replace()},
			"version": schema.Int64Attribute{Computed: true,
				PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()}},
			"arn": schema.StringAttribute{Computed: true, PlanModifiers: keep()},
		},
	}
}

func (r *moduleVersion) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	if c, ok := req.ProviderData.(*connectapi.Client); ok {
		r.client = c
	}
}

func (r *moduleVersion) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m moduleVersionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// A version snapshots whatever the module holds when it is created. When
	// that is not the content this configuration names, the snapshot would
	// be of something nobody reviewed, so it is refused.
	mod, err := r.client.Connect.DescribeContactFlowModule(ctx, &connect.DescribeContactFlowModuleInput{
		InstanceId: aws.String(m.InstanceID.ValueString()), ContactFlowModuleId: aws.String(m.ModuleID.ValueString())})
	if err != nil {
		resp.Diagnostics.AddError("Reading the module failed", err.Error())
		return
	}
	if live := hashOf(aws.ToString(mod.ContactFlowModule.Content)); live != m.ContentHash.ValueString() {
		resp.Diagnostics.AddError("The module changed under this version",
			fmt.Sprintf("The module's live content_hash is %s, not %s; apply the module first.", live, m.ContentHash.ValueString()))
		return
	}
	in := &connect.CreateContactFlowModuleVersionInput{
		InstanceId: aws.String(m.InstanceID.ValueString()), ContactFlowModuleId: aws.String(m.ModuleID.ValueString())}
	if !m.Description.IsNull() {
		in.Description = aws.String(m.Description.ValueString())
	}
	out, err := r.client.Connect.CreateContactFlowModuleVersion(ctx, in)
	if err != nil {
		resp.Diagnostics.AddError("CreateContactFlowModuleVersion failed", err.Error())
		return
	}
	v := aws.ToInt64(out.Version)
	m.Version = types.Int64Value(v)
	m.ARN = types.StringValue(aws.ToString(out.ContactFlowModuleArn))
	m.ID = types.StringValue(fmt.Sprintf("%s:%s:%d", m.InstanceID.ValueString(), m.ModuleID.ValueString(), v))
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

// Read finds the version among the module's versions: a version is
// immutable, so all that can change is that it, or its module, is gone, and
// then it leaves state so the next plan creates it again.
// https://docs.aws.amazon.com/connect/latest/APIReference/API_ListContactFlowModuleVersions.html
func (r *moduleVersion) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m moduleVersionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	pages := connect.NewListContactFlowModuleVersionsPaginator(r.client.Connect, &connect.ListContactFlowModuleVersionsInput{
		InstanceId: aws.String(m.InstanceID.ValueString()), ContactFlowModuleId: aws.String(m.ModuleID.ValueString())})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if connectapi.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		if err != nil {
			resp.Diagnostics.AddError("ListContactFlowModuleVersions failed", err.Error())
			return
		}
		for _, v := range page.ContactFlowModuleVersionSummaryList {
			if aws.ToInt64(v.Version) != m.Version.ValueInt64() {
				continue
			}
			if v.Arn != nil {
				m.ARN = types.StringValue(*v.Arn)
			}
			if d := aws.ToString(v.VersionDescription); d != "" {
				m.Description = types.StringValue(d)
			}
			resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
			return
		}
	}
	resp.State.RemoveResource(ctx)
}

// Update is never called: every argument replaces.
func (r *moduleVersion) Update(context.Context, resource.UpdateRequest, *resource.UpdateResponse) {}

func (r *moduleVersion) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m moduleVersionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	_, err := r.client.Connect.DeleteContactFlowModuleVersion(ctx, &connect.DeleteContactFlowModuleVersionInput{
		InstanceId: aws.String(m.InstanceID.ValueString()), ContactFlowModuleId: aws.String(m.ModuleID.ValueString()),
		ContactFlowModuleVersion: aws.Int64(m.Version.ValueInt64())})
	if err != nil && !connectapi.IsNotFound(err) {
		detail := err.Error()
		var ir *ctypes.InvalidRequestException
		if errors.As(err, &ir) && strings.Contains(aws.ToString(ir.Message), "alias") {
			detail += "\n\nConnect does not delete a version an alias points at. Set lifecycle { create_before_destroy = true } on this resource, so a replacement version is created and the alias moved to it before this one is destroyed."
		}
		resp.Diagnostics.AddError("DeleteContactFlowModuleVersion failed", detail)
	}
}

func (r *moduleVersion) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, ":")
	v, err := strconv.ParseInt(parts[len(parts)-1], 10, 64)
	if len(parts) != 3 || err != nil {
		resp.Diagnostics.AddError("Invalid import id", fmt.Sprintf("Expected instance_id:contact_flow_module_id:version, got %q.", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("instance_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("contact_flow_module_id"), parts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("version"), v)...)
	// Connect has no call that returns one version's content, so an imported
	// version takes the module's current content_hash: it plans no change
	// when the module still holds what the version snapshotted, and a
	// replacement when it has moved on, which is what a new version is for.
	mod, err := r.client.Connect.DescribeContactFlowModule(ctx, &connect.DescribeContactFlowModuleInput{
		InstanceId: aws.String(parts[0]), ContactFlowModuleId: aws.String(parts[1])})
	if err != nil {
		resp.Diagnostics.AddError("Reading the module failed", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("content_hash"), hashOf(aws.ToString(mod.ContactFlowModule.Content)))...)
}
