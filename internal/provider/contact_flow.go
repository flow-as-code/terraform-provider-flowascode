// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/connect"
	ctypes "github.com/aws/aws-sdk-go-v2/service/connect/types"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/connectapi"
	"github.com/flow-as-code/terraform-provider-flowascode/internal/flowdoc"
	"github.com/flow-as-code/terraform-provider-flowascode/internal/flowmodel"
	"github.com/flow-as-code/terraform-provider-flowascode/internal/jsonv"
	"github.com/flow-as-code/terraform-provider-flowascode/internal/lint"
	"github.com/flow-as-code/terraform-provider-flowascode/internal/materialize"
	fdschema "github.com/flow-as-code/terraform-provider-flowascode/internal/schema"
)

// contactFlow is flowascode_contact_flow: one Amazon Connect flow, its
// actions written as HCL blocks by the contract in
// conformance/hcl/README.md.
type contactFlow struct {
	client *connectapi.Client
}

var (
	_ resource.Resource                   = (*contactFlow)(nil)
	_ resource.ResourceWithConfigure      = (*contactFlow)(nil)
	_ resource.ResourceWithValidateConfig = (*contactFlow)(nil)
	_ resource.ResourceWithModifyPlan     = (*contactFlow)(nil)
	_ resource.ResourceWithImportState    = (*contactFlow)(nil)
)

// NewContactFlow is the resource factory.
func NewContactFlow() resource.Resource { return &contactFlow{} }

func (r *contactFlow) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_contact_flow"
}

// connectTypes are the ConnectType values a flow resource takes; MODULE is
// the module resource's.
var connectTypes = []string{
	"CONTACT_FLOW", "CUSTOMER_QUEUE", "CUSTOMER_HOLD", "CUSTOMER_WHISPER", "AGENT_HOLD",
	"AGENT_WHISPER", "OUTBOUND_WHISPER", "AGENT_TRANSFER", "QUEUE_TRANSFER",
}

func keep() []planmodifier.String {
	return []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
}

func (r *contactFlow) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "An Amazon Connect flow whose actions are HCL blocks. The contract is " +
			"flow-as-code's conformance/hcl/README.md; the plan shows the FlowDoc the configuration " +
			"reads to in `flowdoc`, and lint runs at plan time.",
		Attributes: map[string]schema.Attribute{
			"id":              schema.StringAttribute{Computed: true, PlanModifiers: keep(), Description: "instance_id:contact_flow_id."},
			"contact_flow_id": schema.StringAttribute{Computed: true, PlanModifiers: keep()},
			"arn":             schema.StringAttribute{Computed: true, PlanModifiers: keep()},
			"instance_id": schema.StringAttribute{Required: true, Description: "The Amazon Connect instance id.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"name": schema.StringAttribute{Required: true, Description: "The flow's name, a slug."},
			"type": schema.StringAttribute{Required: true, Description: "The flow's ConnectType.",
				Validators:    []validator.String{stringvalidator.OneOf(connectTypes...)},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"description": schema.StringAttribute{Optional: true},
			"state": schema.StringAttribute{Optional: true, Computed: true, PlanModifiers: keep(),
				Validators: []validator.String{stringvalidator.OneOf("ACTIVE", "ARCHIVED")}},
			"start":    schema.StringAttribute{Optional: true, Description: "StartAction, when it is not the first action."},
			"refs":     schema.MapAttribute{Optional: true, ElementType: types.StringType, Description: "Reference key to the ARN it binds, as a Terraform expression."},
			"tags":     schema.MapAttribute{Optional: true, ElementType: types.StringType},
			"settings": schema.StringAttribute{Optional: true, Description: "Not settable on a flow; present so the provider can say so (FLOW_WITH_SETTINGS)."},
			"flowdoc": schema.StringAttribute{Computed: true,
				Description: "The FlowDoc the configuration reads to, canonical JSON with reference tokens in place."},
			"content": schema.StringAttribute{Computed: true,
				Description: "The Flow language content sent to Connect: the FlowDoc with every reference bound."},
			"content_hash": schema.StringAttribute{Computed: true, Description: "sha256 of content."},
		},
		Blocks: map[string]schema.Block{
			"lint": schema.SingleNestedBlock{
				Description: "Lint rules to skip for this flow. Hard rules cannot be skipped.",
				Attributes:  map[string]schema.Attribute{"disable": schema.ListAttribute{Optional: true, ElementType: types.StringType}},
			},
			"action": flowmodel.ActionBlock(),
		},
	}
}

func (r *contactFlow) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if c, ok := req.ProviderData.(*connectapi.Client); ok {
		r.client = c
	}
}

// read decodes a whole configuration or plan and reads it to a FlowDoc.
func read(raw tftypes.Value, kind string, phase flowmodel.Phase) (flowmodel.Result, error) {
	v, err := flowmodel.Decode(raw)
	if err != nil {
		return flowmodel.Result{}, err
	}
	m, _ := v.(map[string]any)
	return flowmodel.FromConfig(m, kind, phase), nil
}

func addProblems(diags *diag.Diagnostics, problems []flowmodel.Problem) {
	for _, p := range problems {
		diags.AddAttributeError(p.Attr, p.Summary(), p.Detail)
	}
}

func (r *contactFlow) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	res, err := read(req.Config.Raw, "flow", flowmodel.PhaseValidate)
	if err != nil {
		resp.Diagnostics.AddError("Cannot read the configuration", err.Error())
		return
	}
	addProblems(&resp.Diagnostics, res.Problems)
}

// planned is what ModifyPlan computes from a configuration: the FlowDoc, and
// the content once every reference is bound to a known value.
type planned struct {
	flowdoc     string
	content     string
	contentKnow bool
	complete    bool
}

// plan reads a configuration at plan time and reports what lint, the schema
// and the refs bindings say about it.
func plan(raw tftypes.Value, kind string, diags *diag.Diagnostics) planned {
	res, err := read(raw, kind, flowmodel.PhasePlan)
	if err != nil {
		diags.AddError("Cannot read the configuration", err.Error())
		return planned{}
	}
	// Problems found in the validate walk stop the run before a plan; here
	// the only new ones are expressions in reference fields.
	for _, p := range res.Problems {
		if p.Code == "REF_EXPRESSION_REFUSED" {
			diags.AddAttributeError(p.Attr, p.Summary(), p.Detail)
		}
	}
	if res.Doc == nil || diags.HasError() {
		return planned{}
	}
	doc := res.Doc
	violations, err := fdschema.Validate(doc)
	if err != nil {
		diags.AddError("Cannot validate the FlowDoc", err.Error())
		return planned{}
	}
	for _, v := range violations {
		diags.AddError("FlowDoc schema", fmt.Sprintf("%s %s", pointerOrRoot(v.InstancePath), v.Message))
	}
	findings, err := lint.Lint([]any{doc}, lint.Options{Disable: res.LintDisable})
	if err != nil {
		diags.AddError("Cannot lint the FlowDoc", err.Error())
		return planned{}
	}
	hard := map[string]bool{}
	for _, rule := range lint.AllRules() {
		hard[rule.ID] = rule.Hard
	}
	for _, f := range findings {
		detail := f.Message
		if f.BlockID != nil {
			detail = fmt.Sprintf("action %q: %s", *f.BlockID, f.Message)
		}
		// Settled decision 8: a hard rule fails the plan; every other finding
		// is a warning, whatever its severity, since the lint panel and
		// flow-cli lint already say it.
		if hard[f.Rule] {
			diags.AddError(f.Rule, detail)
		} else {
			diags.AddWarning(f.Rule, detail)
		}
	}

	// Every reference the flow uses needs a binding (rule 6); a binding no
	// action uses is a warning (rule 22).
	bindings := map[string]string{}
	known := true
	used := map[string]bool{}
	for _, e := range flowdoc.CollectRefs(doc) {
		key := flowdoc.RefKey(e)
		used[key] = true
		switch v := res.Refs[key].(type) {
		case string:
			bindings[key] = v
		case flowmodel.Unknown:
			known = false
		default:
			diags.AddAttributeError(path.Root("refs").AtMapKey(key), "unbound reference",
				fmt.Sprintf("No terraform address for %s. Bind it in refs: %q = <expression>.", e.Token, key))
		}
	}
	for key := range res.Refs {
		if !used[key] {
			diags.AddAttributeWarning(path.Root("refs").AtMapKey(key), "unused reference",
				fmt.Sprintf("refs[%q] is referenced by no action.", key))
		}
	}
	out := planned{flowdoc: string(flowdoc.Serialize(doc)), complete: true}
	if diags.HasError() || !known {
		return out
	}
	content, err := contentOf(doc, bindings)
	if err != nil {
		diags.AddError("Cannot materialize the flow", err.Error())
		return out
	}
	out.content, out.contentKnow = content, true
	return out
}

func pointerOrRoot(p string) string {
	if p == "" {
		return "(document)"
	}
	return p
}

// contentOf is the Flow language content Connect receives: every token bound
// to its ARN and the layout projected into Metadata.
func contentOf(doc jsonv.Object, bindings map[string]string) (string, error) {
	content, err := materialize.MaterializeWithMap(doc, bindings)
	if err != nil {
		return "", err
	}
	b, err := materialize.SerializeContent(content)
	return string(b), err
}

func hashOf(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func (r *contactFlow) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return // destroy
	}
	p := plan(req.Config.Raw, "flow", &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	setPlanned(ctx, p, &resp.Plan, &resp.Diagnostics)
}

func setPlanned(ctx context.Context, p planned, plan interface {
	SetAttribute(context.Context, path.Path, interface{}) diag.Diagnostics
}, diags *diag.Diagnostics) {
	if p.complete {
		diags.Append(plan.SetAttribute(ctx, path.Root("flowdoc"), types.StringValue(p.flowdoc))...)
	} else {
		diags.Append(plan.SetAttribute(ctx, path.Root("flowdoc"), types.StringUnknown())...)
	}
	if p.contentKnow {
		diags.Append(plan.SetAttribute(ctx, path.Root("content"), types.StringValue(p.content))...)
		diags.Append(plan.SetAttribute(ctx, path.Root("content_hash"), types.StringValue(hashOf(p.content)))...)
	} else {
		diags.Append(plan.SetAttribute(ctx, path.Root("content"), types.StringUnknown())...)
		diags.Append(plan.SetAttribute(ctx, path.Root("content_hash"), types.StringUnknown())...)
	}
}

// flowAttrs are the scalar attributes CRUD reads from a plan or state.
type flowAttrs struct {
	ID          types.String `tfsdk:"id"`
	FlowID      types.String `tfsdk:"contact_flow_id"`
	ARN         types.String `tfsdk:"arn"`
	InstanceID  types.String `tfsdk:"instance_id"`
	Name        types.String `tfsdk:"name"`
	Type        types.String `tfsdk:"type"`
	Description types.String `tfsdk:"description"`
	State       types.String `tfsdk:"state"`
	Tags        types.Map    `tfsdk:"tags"`
	Content     types.String `tfsdk:"content"`
}

func getAttrs(ctx context.Context, src interface {
	GetAttribute(context.Context, path.Path, interface{}) diag.Diagnostics
}, diags *diag.Diagnostics) flowAttrs {
	var a flowAttrs
	for name, target := range map[string]any{
		"id": &a.ID, "contact_flow_id": &a.FlowID, "arn": &a.ARN, "instance_id": &a.InstanceID,
		"name": &a.Name, "type": &a.Type, "description": &a.Description, "state": &a.State,
		"tags": &a.Tags, "content": &a.Content,
	} {
		diags.Append(src.GetAttribute(ctx, path.Root(name), target)...)
	}
	return a
}

func tagsOf(ctx context.Context, m types.Map) map[string]string {
	out := map[string]string{}
	if m.IsNull() || m.IsUnknown() {
		return out
	}
	_ = m.ElementsAs(ctx, &out, false)
	return out
}

func (r *contactFlow) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	p := plan(req.Config.Raw, "flow", &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if !p.contentKnow {
		resp.Diagnostics.AddError("Unknown content at apply", "Every refs value must be known when the flow is created.")
		return
	}
	a := getAttrs(ctx, req.Plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	in := &connect.CreateContactFlowInput{
		InstanceId: aws.String(a.InstanceID.ValueString()),
		Name:       aws.String(a.Name.ValueString()),
		Type:       ctypes.ContactFlowType(a.Type.ValueString()),
		Content:    aws.String(p.content),
		Status:     ctypes.ContactFlowStatusPublished,
	}
	if !a.Description.IsNull() {
		in.Description = aws.String(a.Description.ValueString())
	}
	if tags := tagsOf(ctx, a.Tags); len(tags) > 0 {
		in.Tags = tags
	}
	out, err := r.client.Connect.CreateContactFlow(ctx, in)
	if err != nil {
		resp.Diagnostics.AddError("CreateContactFlow failed", err.Error())
		return
	}
	flowID := aws.ToString(out.ContactFlowId)
	resp.State.Raw = req.Plan.Raw
	state := "ACTIVE"
	if a.State.ValueString() == "ARCHIVED" {
		if _, err := r.client.Connect.UpdateContactFlowMetadata(ctx, &connect.UpdateContactFlowMetadataInput{
			InstanceId: in.InstanceId, ContactFlowId: out.ContactFlowId, ContactFlowState: ctypes.ContactFlowStateArchived,
		}); err != nil {
			resp.Diagnostics.AddError("UpdateContactFlowMetadata failed", err.Error())
		}
		state = "ARCHIVED"
	}
	for name, v := range map[string]string{
		"id": a.InstanceID.ValueString() + ":" + flowID, "contact_flow_id": flowID, "arn": aws.ToString(out.ContactFlowArn),
		"state": state, "flowdoc": p.flowdoc, "content": p.content, "content_hash": hashOf(p.content),
	} {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(name), types.StringValue(v))...)
	}
}

func (r *contactFlow) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	a := getAttrs(ctx, req.State, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	out, err := r.client.Connect.DescribeContactFlow(ctx, &connect.DescribeContactFlowInput{
		InstanceId: aws.String(a.InstanceID.ValueString()), ContactFlowId: aws.String(a.FlowID.ValueString()),
	})
	if connectapi.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("DescribeContactFlow failed", err.Error())
		return
	}
	f := out.ContactFlow
	set := func(name string, v attrValue) {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(name), v)...)
	}
	set("arn", types.StringValue(aws.ToString(f.Arn)))
	set("name", types.StringValue(aws.ToString(f.Name)))
	if f.Description != nil && *f.Description != "" {
		set("description", types.StringValue(*f.Description))
	} else if !a.Description.IsNull() {
		set("description", types.StringNull())
	}
	set("state", types.StringValue(string(f.State)))
	if len(f.Tags) > 0 || !a.Tags.IsNull() {
		tags, d := types.MapValueFrom(ctx, types.StringType, userTags(f.Tags))
		resp.Diagnostics.Append(d...)
		set("tags", tags)
	}
	// Drift in the flow itself shows as a change to content: the plan
	// recomputes content from the configuration and Terraform sees the two
	// differ. Metadata is left out of the comparison, because Connect adds
	// its own there.
	live := aws.ToString(f.Content)
	if !sameContent(live, a.Content.ValueString()) {
		set("content", types.StringValue(live))
		set("content_hash", types.StringValue(hashOf(live)))
	}
}

type attrValue = interface{}

// userTags drops the tags AWS manages itself (aws: prefix).
func userTags(tags map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range tags {
		if !strings.HasPrefix(k, "aws:") {
			out[k] = v
		}
	}
	return out
}

// sameContent compares two Flow language documents without Metadata, in
// canonical form.
func sameContent(a, b string) bool {
	norm := func(s string) string {
		v, err := jsonv.Decode([]byte(s))
		o, ok := v.(jsonv.Object)
		if err != nil || !ok {
			return s
		}
		o.Delete("Metadata")
		c, err := materialize.SerializeContent(o)
		if err != nil {
			return s
		}
		return string(c)
	}
	return norm(a) == norm(b)
}

func (r *contactFlow) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	p := plan(req.Config.Raw, "flow", &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	a := getAttrs(ctx, req.Plan, &resp.Diagnostics)
	old := getAttrs(ctx, req.State, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	instance, flow := aws.String(old.InstanceID.ValueString()), aws.String(old.FlowID.ValueString())
	if !sameContent(p.content, old.Content.ValueString()) {
		if _, err := r.client.Connect.UpdateContactFlowContent(ctx, &connect.UpdateContactFlowContentInput{
			InstanceId: instance, ContactFlowId: flow, Content: aws.String(p.content),
		}); err != nil {
			resp.Diagnostics.AddError("UpdateContactFlowContent failed", err.Error())
			return
		}
	}
	state := old.State.ValueString()
	if !a.State.IsUnknown() && !a.State.IsNull() {
		state = a.State.ValueString()
	}
	if a.Name != old.Name || !a.Description.Equal(old.Description) || state != old.State.ValueString() {
		in := &connect.UpdateContactFlowMetadataInput{InstanceId: instance, ContactFlowId: flow, Name: aws.String(a.Name.ValueString())}
		if !a.Description.IsNull() {
			in.Description = aws.String(a.Description.ValueString())
		} else {
			in.Description = aws.String("")
		}
		if state != "" {
			in.ContactFlowState = ctypes.ContactFlowState(state)
		}
		if _, err := r.client.Connect.UpdateContactFlowMetadata(ctx, in); err != nil {
			resp.Diagnostics.AddError("UpdateContactFlowMetadata failed", err.Error())
			return
		}
	}
	if err := syncTags(ctx, r.client.Connect, old.ARN.ValueString(), tagsOf(ctx, old.Tags), tagsOf(ctx, a.Tags)); err != nil {
		resp.Diagnostics.AddError("Updating tags failed", err.Error())
		return
	}
	resp.State.Raw = req.Plan.Raw
	if state == "" {
		state = "ACTIVE"
	}
	for name, v := range map[string]string{
		"id": old.ID.ValueString(), "contact_flow_id": old.FlowID.ValueString(), "arn": old.ARN.ValueString(),
		"state": state, "flowdoc": p.flowdoc, "content": p.content, "content_hash": hashOf(p.content),
	} {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(name), types.StringValue(v))...)
	}
}

// syncTags applies the difference between two tag maps.
func syncTags(ctx context.Context, api connectapi.API, arn string, before, after map[string]string) error {
	var remove []string
	for k := range before {
		if _, ok := after[k]; !ok {
			remove = append(remove, k)
		}
	}
	sort.Strings(remove)
	add := map[string]string{}
	for k, v := range after {
		if before[k] != v {
			add[k] = v
		}
	}
	if len(remove) > 0 {
		if _, err := api.UntagResource(ctx, &connect.UntagResourceInput{ResourceArn: aws.String(arn), TagKeys: remove}); err != nil {
			return err
		}
	}
	if len(add) > 0 {
		if _, err := api.TagResource(ctx, &connect.TagResourceInput{ResourceArn: aws.String(arn), Tags: add}); err != nil {
			return err
		}
	}
	return nil
}

func (r *contactFlow) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	a := getAttrs(ctx, req.State, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	_, err := r.client.Connect.DeleteContactFlow(ctx, &connect.DeleteContactFlowInput{
		InstanceId: aws.String(a.InstanceID.ValueString()), ContactFlowId: aws.String(a.FlowID.ValueString()),
	})
	if err != nil && !connectapi.IsNotFound(err) {
		resp.Diagnostics.AddError("DeleteContactFlow failed", err.Error())
	}
}

func (r *contactFlow) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	instance, flow, ok := strings.Cut(req.ID, ":")
	if !ok || instance == "" || flow == "" {
		resp.Diagnostics.AddError("Invalid import id", fmt.Sprintf("Expected instance_id:contact_flow_id, got %q.", req.ID))
		return
	}
	for name, v := range map[string]string{"id": req.ID, "instance_id": instance, "contact_flow_id": flow} {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(name), types.StringValue(v))...)
	}
}
