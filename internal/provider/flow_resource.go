// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/connect"
	ctypes "github.com/aws/aws-sdk-go-v2/service/connect/types"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/connectapi"
	"github.com/flow-as-code/terraform-provider-flowascode/internal/export"
	"github.com/flow-as-code/terraform-provider-flowascode/internal/flowdoc"
	"github.com/flow-as-code/terraform-provider-flowascode/internal/flowmodel"
	"github.com/flow-as-code/terraform-provider-flowascode/internal/jsonv"
)

// flowResource is flowascode_contact_flow (kind "flow") and
// flowascode_contact_flow_module (kind "module"): one Amazon Connect flow or
// flow module, its actions written as HCL blocks by the contract in
// conformance/hcl/README.md. The two differ only in naming, a few attributes,
// and which Connect operations they call.
type flowResource struct {
	kind   string
	client *connectapi.Client
}

var (
	_ resource.Resource                   = (*flowResource)(nil)
	_ resource.ResourceWithConfigure      = (*flowResource)(nil)
	_ resource.ResourceWithValidateConfig = (*flowResource)(nil)
	_ resource.ResourceWithModifyPlan     = (*flowResource)(nil)
	_ resource.ResourceWithImportState    = (*flowResource)(nil)
)

// NewContactFlow is flowascode_contact_flow's factory.
func NewContactFlow() resource.Resource { return &flowResource{kind: "flow"} }

// NewContactFlowModule is flowascode_contact_flow_module's factory.
func NewContactFlowModule() resource.Resource { return &flowResource{kind: "module"} }

func (r *flowResource) module() bool { return r.kind == "module" }

// idAttr names the Connect id attribute.
func (r *flowResource) idAttr() string {
	if r.module() {
		return "contact_flow_module_id"
	}
	return "contact_flow_id"
}

func (r *flowResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	if r.module() {
		resp.TypeName = req.ProviderTypeName + "_contact_flow_module"
	} else {
		resp.TypeName = req.ProviderTypeName + "_contact_flow"
	}
}

func (r *flowResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	what := "flow"
	if r.module() {
		what = "flow module"
	}
	attrs := map[string]schema.Attribute{
		"id":       schema.StringAttribute{Computed: true, PlanModifiers: keep(), Description: "instance_id:" + r.idAttr() + "."},
		r.idAttr(): schema.StringAttribute{Computed: true, PlanModifiers: keep()},
		"arn":      schema.StringAttribute{Computed: true, PlanModifiers: keep()},
		"instance_id": schema.StringAttribute{Required: true, Description: "The Amazon Connect instance id.",
			PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"name": schema.StringAttribute{Required: true, Description: "The " + what + "'s name, a slug."},
		"display_name": schema.StringAttribute{Optional: true,
			Description: "The name Connect shows, when it is not name (an adopted \"Main Line\"): the document's displayName. Omitted means Connect's name is name. 1 to 127 characters with one that is not a space.",
			Validators: []validator.String{stringvalidator.LengthBetween(1, 127),
				stringvalidator.RegexMatches(regexp.MustCompile(`\S`), "must have a character that is not a space")}},
		"description": schema.StringAttribute{Optional: true},
		"state": schema.StringAttribute{Optional: true, Computed: true, PlanModifiers: keep(),
			Validators: []validator.String{stringvalidator.OneOf("ACTIVE", "ARCHIVED")}},
		"start": schema.StringAttribute{Optional: true, Description: "StartAction, when it is not the first action."},
		"refs":  schema.MapAttribute{Optional: true, ElementType: types.StringType, Description: "Reference key to the ARN it binds, as a Terraform expression."},
		"tags":  schema.MapAttribute{Optional: true, ElementType: types.StringType},
		"flowdoc": schema.StringAttribute{Computed: true,
			Description: "The FlowDoc the configuration reads to, canonical JSON with reference tokens in place."},
		"content": schema.StringAttribute{Computed: true,
			Description: "The Flow language content sent to Connect: the FlowDoc with every reference bound."},
		"content_hash": schema.StringAttribute{Computed: true, Description: "sha256 of content in canonical form without Metadata: what the flow does, not how it is laid out."},
	}
	if r.module() {
		attrs["settings"] = schema.StringAttribute{Optional: true, Description: "The module's Settings, as jsonencode({...}). Omitted means {}."}
		// Connect takes ExternalInvocationConfiguration only on
		// CreateContactFlowModule; no update operation carries it
		// (https://docs.aws.amazon.com/connect/latest/APIReference/API_CreateContactFlowModule.html),
		// so a change replaces the module. Computed, and read from Connect,
		// so an imported module records its live value instead of planning a
		// change it could never apply.
		attrs["external_invocation_enabled"] = schema.BoolAttribute{Optional: true, Computed: true,
			Description:   "Whether the module can be invoked outside a flow. Connect sets it only when the module is created, so a change replaces the module.",
			PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown(), boolplanmodifier.RequiresReplace()}}
		attrs["type"] = schema.StringAttribute{Optional: true, Description: "Not settable on a module; present so the provider can say so (MODULE_WITH_TYPE)."}
	} else {
		attrs["type"] = schema.StringAttribute{Required: true, Description: "The flow's ConnectType.",
			Validators:    []validator.String{stringvalidator.OneOf(connectTypes...)},
			PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}}
		attrs["settings"] = schema.StringAttribute{Optional: true, Description: "Not settable on a flow; present so the provider can say so (FLOW_WITH_SETTINGS)."}
	}
	resp.Schema = schema.Schema{
		Description: "An Amazon Connect " + what + " whose actions are HCL blocks. The contract is " +
			"flow-as-code's conformance/hcl/README.md; the plan shows the FlowDoc the configuration " +
			"reads to in `flowdoc`, and lint runs at plan time.",
		Attributes: attrs,
		Blocks: map[string]schema.Block{
			"lint": schema.SingleNestedBlock{
				Description: "Lint rules to skip. Hard rules cannot be skipped.",
				Attributes:  map[string]schema.Attribute{"disable": schema.ListAttribute{Optional: true, ElementType: types.StringType}},
			},
			"action": flowmodel.ActionBlock(),
		},
	}
}

func (r *flowResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if c, ok := req.ProviderData.(*connectapi.Client); ok {
		r.client = c
	}
}

func (r *flowResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	res, err := read(req.Config.Raw, r.kind, flowmodel.PhaseValidate)
	if err != nil {
		resp.Diagnostics.AddError("Cannot read the configuration", err.Error())
		return
	}
	addProblems(&resp.Diagnostics, res.Problems)
}

func (r *flowResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return // destroy
	}
	p := plan(req.Config.Raw, r.kind, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	setPlanned(ctx, p, &resp.Plan, &resp.Diagnostics)
}

// flowAttrs are the scalar attributes CRUD reads from a plan or state.
type flowAttrs struct {
	ID          types.String
	ConnectID   types.String
	ARN         types.String
	InstanceID  types.String
	Name        types.String
	DisplayName types.String
	Type        types.String
	Description types.String
	State       types.String
	Tags        types.Map
	Content     types.String
	External    types.Bool
	Refs        types.Map
}

func (r *flowResource) getAttrs(ctx context.Context, src interface {
	GetAttribute(context.Context, path.Path, interface{}) diag.Diagnostics
}, diags *diag.Diagnostics) flowAttrs {
	var a flowAttrs
	targets := map[string]any{
		"id": &a.ID, r.idAttr(): &a.ConnectID, "arn": &a.ARN, "instance_id": &a.InstanceID,
		"name": &a.Name, "display_name": &a.DisplayName, "description": &a.Description, "state": &a.State,
		"tags": &a.Tags, "content": &a.Content, "refs": &a.Refs,
	}
	if r.module() {
		targets["external_invocation_enabled"] = &a.External
	} else {
		targets["type"] = &a.Type
	}
	for name, target := range targets {
		diags.Append(src.GetAttribute(ctx, path.Root(name), target)...)
	}
	return a
}

// live is a flow or module as Describe returns it.
type live struct {
	arn, name, description, state, content, typ string
	tags                                        map[string]string
	external                                    *bool
}

func (r *flowResource) describe(ctx context.Context, instance, id string) (live, error) {
	if r.module() {
		out, err := r.client.Connect.DescribeContactFlowModule(ctx, &connect.DescribeContactFlowModuleInput{
			InstanceId: aws.String(instance), ContactFlowModuleId: aws.String(id)})
		if err != nil {
			return live{}, err
		}
		m := out.ContactFlowModule
		l := live{arn: aws.ToString(m.Arn), name: aws.ToString(m.Name), description: aws.ToString(m.Description),
			state: string(m.State), content: aws.ToString(m.Content), tags: m.Tags}
		if m.ExternalInvocationConfiguration != nil {
			l.external = aws.Bool(m.ExternalInvocationConfiguration.Enabled)
		}
		return l, nil
	}
	out, err := r.client.Connect.DescribeContactFlow(ctx, &connect.DescribeContactFlowInput{
		InstanceId: aws.String(instance), ContactFlowId: aws.String(id)})
	if err != nil {
		return live{}, err
	}
	f := out.ContactFlow
	return live{arn: aws.ToString(f.Arn), name: aws.ToString(f.Name), description: aws.ToString(f.Description),
		state: string(f.State), content: aws.ToString(f.Content), tags: f.Tags, typ: string(f.Type)}, nil
}

func (r *flowResource) create(ctx context.Context, a flowAttrs, content string, tags map[string]string) (string, string, error) {
	var description *string
	if !a.Description.IsNull() {
		description = aws.String(a.Description.ValueString())
	}
	if len(tags) == 0 {
		tags = nil
	}
	if r.module() {
		in := &connect.CreateContactFlowModuleInput{
			InstanceId: aws.String(a.InstanceID.ValueString()), Name: aws.String(a.connectName()),
			Content: aws.String(content), Description: description, Tags: tags,
		}
		if !a.External.IsNull() && !a.External.IsUnknown() {
			in.ExternalInvocationConfiguration = &ctypes.ExternalInvocationConfiguration{Enabled: a.External.ValueBool()}
		}
		out, err := r.client.Connect.CreateContactFlowModule(ctx, in)
		if err != nil {
			return "", "", fmt.Errorf("CreateContactFlowModule: %w", err)
		}
		return aws.ToString(out.Id), aws.ToString(out.Arn), nil
	}
	out, err := r.client.Connect.CreateContactFlow(ctx, &connect.CreateContactFlowInput{
		InstanceId: aws.String(a.InstanceID.ValueString()), Name: aws.String(a.connectName()),
		Type: ctypes.ContactFlowType(a.Type.ValueString()), Content: aws.String(content),
		Status: ctypes.ContactFlowStatusPublished, Description: description, Tags: tags,
	})
	if err != nil {
		return "", "", fmt.Errorf("CreateContactFlow: %w", err)
	}
	return aws.ToString(out.ContactFlowId), aws.ToString(out.ContactFlowArn), nil
}

func (r *flowResource) updateContent(ctx context.Context, instance, id, content string) error {
	if r.module() {
		_, err := r.client.Connect.UpdateContactFlowModuleContent(ctx, &connect.UpdateContactFlowModuleContentInput{
			InstanceId: aws.String(instance), ContactFlowModuleId: aws.String(id), Content: aws.String(content)})
		return wrap("UpdateContactFlowModuleContent", err)
	}
	_, err := r.client.Connect.UpdateContactFlowContent(ctx, &connect.UpdateContactFlowContentInput{
		InstanceId: aws.String(instance), ContactFlowId: aws.String(id), Content: aws.String(content)})
	return wrap("UpdateContactFlowContent", err)
}

func (r *flowResource) updateMetadata(ctx context.Context, instance, id, name, description, state string) error {
	if r.module() {
		_, err := r.client.Connect.UpdateContactFlowModuleMetadata(ctx, &connect.UpdateContactFlowModuleMetadataInput{
			InstanceId: aws.String(instance), ContactFlowModuleId: aws.String(id), Name: aws.String(name),
			Description: aws.String(description), State: ctypes.ContactFlowModuleState(state)})
		return wrap("UpdateContactFlowModuleMetadata", err)
	}
	_, err := r.client.Connect.UpdateContactFlowMetadata(ctx, &connect.UpdateContactFlowMetadataInput{
		InstanceId: aws.String(instance), ContactFlowId: aws.String(id), Name: aws.String(name),
		Description: aws.String(description), ContactFlowState: ctypes.ContactFlowState(state)})
	return wrap("UpdateContactFlowMetadata", err)
}

func (r *flowResource) delete(ctx context.Context, instance, id string) error {
	var err error
	if r.module() {
		_, err = r.client.Connect.DeleteContactFlowModule(ctx, &connect.DeleteContactFlowModuleInput{
			InstanceId: aws.String(instance), ContactFlowModuleId: aws.String(id)})
	} else {
		_, err = r.client.Connect.DeleteContactFlow(ctx, &connect.DeleteContactFlowInput{
			InstanceId: aws.String(instance), ContactFlowId: aws.String(id)})
	}
	if connectapi.IsNotFound(err) {
		return nil
	}
	return wrap("Delete", err)
}

func wrap(op string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", op, err)
}

func (r *flowResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	p := plan(req.Config.Raw, r.kind, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if !p.contentKnow {
		resp.Diagnostics.AddError("Unknown content at apply", "Every refs value must be known when the "+r.kind+" is created.")
		return
	}
	a := r.getAttrs(ctx, req.Plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	id, arn, err := r.create(ctx, a, p.content, tagsOf(ctx, a.Tags))
	if err != nil {
		resp.Diagnostics.AddError("Creating the "+r.kind+" failed", connectapi.Detail(err))
		return
	}
	resp.State.Raw = req.Plan.Raw
	state := "ACTIVE"
	if a.State.ValueString() == "ARCHIVED" {
		if err := r.updateMetadata(ctx, a.InstanceID.ValueString(), id, a.connectName(), a.Description.ValueString(), "ARCHIVED"); err != nil {
			resp.Diagnostics.AddError("Archiving the "+r.kind+" failed", err.Error())
		}
		state = "ARCHIVED"
	}
	if r.module() && a.External.IsUnknown() {
		// Not configured: record what Connect holds, which is disabled
		// unless Describe says otherwise.
		external := false
		if l, err := r.describe(ctx, a.InstanceID.ValueString(), id); err == nil && l.external != nil {
			external = *l.external
		}
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("external_invocation_enabled"), types.BoolValue(external))...)
	}
	for name, v := range map[string]string{
		"id": a.InstanceID.ValueString() + ":" + id, r.idAttr(): id, "arn": arn,
		"state": state, "flowdoc": p.flowdoc, "content": p.content, "content_hash": hashOf(p.content),
	} {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(name), types.StringValue(v))...)
	}
}

func (r *flowResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	a := r.getAttrs(ctx, req.State, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	l, err := r.describe(ctx, a.InstanceID.ValueString(), a.ConnectID.ValueString())
	if connectapi.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Reading the "+r.kind+" failed", err.Error())
		return
	}
	set := func(name string, v attrValue) {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(name), v)...)
	}
	set("arn", types.StringValue(l.arn))
	// name is the slug the configuration holds; Connect's name is
	// display_name when it differs. An import has no name yet, so it takes
	// the slug of Connect's, as export does.
	name := a.Name.ValueString()
	if a.Name.IsNull() || a.Name.IsUnknown() {
		name = export.SlugifyResourceName(l.name)
	}
	set("name", types.StringValue(name))
	if l.name != name {
		set("display_name", types.StringValue(l.name))
	} else if !a.DisplayName.IsNull() {
		set("display_name", types.StringNull())
	}
	if l.description != "" {
		set("description", types.StringValue(l.description))
	} else if !a.Description.IsNull() {
		set("description", types.StringNull())
	}
	set("state", types.StringValue(l.state))
	if !r.module() {
		set("type", types.StringValue(l.typ))
		a.Type = types.StringValue(l.typ)
	}
	if len(userTags(l.tags)) > 0 || !a.Tags.IsNull() {
		tags, d := types.MapValueFrom(ctx, types.StringType, userTags(l.tags))
		resp.Diagnostics.Append(d...)
		set("tags", tags)
	}
	if r.module() && l.external != nil {
		set("external_invocation_enabled", types.BoolValue(*l.external))
	}
	// Drift in the flow itself shows as a change to content: the plan
	// recomputes content from the configuration and Terraform sees the two
	// differ. Metadata is left out of the comparison, because Connect adds
	// its own there.
	if !sameContent(l.content, a.Content.ValueString()) {
		set("content", types.StringValue(l.content))
		set("content_hash", types.StringValue(hashOf(l.content)))
		// Show the live flow as blocks too, so the plan says what changed
		// rather than only that content did. After an import this is also
		// what -generate-config-out writes.
		r.reconstruct(ctx, a, l, &resp.State, &resp.Diagnostics)
	}
}

// reconstruct writes the live flow back into state as the configuration
// would hold it: action blocks, start, refs, settings and flowdoc. ARNs are
// mapped back to reference keys through the refs bindings already in state;
// an ARN nothing binds leaves the blocks as they were, with a warning, since
// the content change already shows the drift.
func (r *flowResource) reconstruct(ctx context.Context, a flowAttrs, l live, state interface {
	SetAttribute(context.Context, path.Path, interface{}) diag.Diagnostics
}, diags *diag.Diagnostics) {
	bound := map[string]string{} // token -> ARN
	for key, arn := range tagsOf(ctx, a.Refs) {
		bound["${cdref:"+key+"}"] = arn
	}
	name := a.Name.ValueString()
	if a.Name.IsNull() || a.Name.IsUnknown() {
		name = export.SlugifyResourceName(l.name)
	}
	connectType := "MODULE"
	if !r.module() {
		connectType = a.Type.ValueString()
	}
	options := export.ExportFlowOptions{
		Name: name, DisplayName: l.name, ConnectType: connectType, Kind: r.kind, Description: l.description, OmitMeta: true,
	}
	reverse := export.ReverseMapOfResourceMap(bound)
	doc, err := export.ExportFlow(l.content, reverse, options)
	var unknown *export.ExportError
	if errors.As(err, &unknown) && len(unknown.UnknownArns) > 0 {
		// An ARN no binding in state names (every one, after an import): list
		// the instance once and map through its inventory, the bindings in
		// state taking precedence so a key the user chose stays theirs.
		inv, ierr := export.CollectInventory(ctx, connectapi.NewInventory(r.client.Connect, a.InstanceID.ValueString()), export.CollectInventoryOptions{})
		if ierr != nil {
			diags.AddWarning("The live "+r.kind+" is not shown as blocks",
				fmt.Sprintf("%s, and listing the instance to name them failed: %s. Allow the provider's Connect List operations, or bind each ARN in refs.", err.Error(), ierr.Error()))
			return
		}
		{
			reverse = export.BuildReverseMap(inv)
			for arn, entry := range export.ReverseMapOfResourceMap(bound).ByArn {
				reverse.ByArn[arn] = entry
			}
			for arn, entry := range reverse.ByArn {
				if _, ok := bound[entry.Token]; !ok {
					bound[entry.Token] = arn
				}
			}
			doc, err = export.ExportFlow(l.content, reverse, options)
		}
	}
	if err != nil {
		diags.AddWarning("The live "+r.kind+" is not shown as blocks",
			fmt.Sprintf("%s. Bind each ARN in refs to see the live actions in the plan.", err.Error()))
		return
	}
	var sr resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sr)
	actionType, d := sr.Schema.TypeAtPath(ctx, path.Root("action"))
	diags.Append(d...)
	if diags.HasError() {
		return
	}
	tv, err := flowmodel.ToTerraform(flowmodel.ActionsFromDoc(doc), actionType.TerraformType(ctx))
	if err != nil {
		diags.AddError("Cannot write the live "+r.kind+" as blocks", err.Error())
		return
	}
	actions, err := actionType.ValueFromTerraform(ctx, tv)
	if err != nil {
		diags.AddError("Cannot write the live "+r.kind+" as blocks", err.Error())
		return
	}
	diags.Append(state.SetAttribute(ctx, path.Root("action"), actions)...)
	diags.Append(state.SetAttribute(ctx, path.Root("flowdoc"), types.StringValue(string(flowdoc.Serialize(doc))))...)

	cv, _ := doc.Get("content")
	content, _ := cv.(jsonv.Object)
	start, _ := content.Get("StartAction")
	av, _ := content.Get("Actions")
	list, _ := av.([]any)
	first := ""
	if len(list) > 0 {
		f, _ := list[0].(jsonv.Object).Get("Identifier")
		first, _ = f.(string)
	}
	if s, _ := start.(string); s != first {
		diags.Append(state.SetAttribute(ctx, path.Root("start"), types.StringValue(s))...)
	} else {
		diags.Append(state.SetAttribute(ctx, path.Root("start"), types.StringNull())...)
	}
	if r.module() {
		if s, ok := content.Get("Settings"); ok {
			if o, _ := s.(jsonv.Object); len(o) > 0 {
				diags.Append(state.SetAttribute(ctx, path.Root("settings"), types.StringValue(flowmodel.JSONEncode(o)))...)
			}
		}
	}
	refs := map[string]string{}
	for _, e := range flowdoc.CollectRefs(content) {
		if arn, ok := bound[e.Token]; ok {
			refs[flowdoc.RefKey(e)] = arn
		} else if e.Type == "view" && e.Alias != "" {
			// The inventory binds a view by its unversioned ARN; the
			// document keys it with the version the flow holds, which
			// qualifyView adds back at plan time.
			if arn, ok := bound["${cdref:view:"+e.Name+"}"]; ok {
				refs[flowdoc.RefKey(e)] = arn
			}
		} else if e.Type == "module" && e.Alias != "" {
			// The inventory binds a module by its bare ARN; the flow invokes
			// it through a qualifier, so the binding is the ARN the flow
			// holds.
			if arn, ok := bound["${cdref:module:"+e.Name+"}"]; ok {
				refs[flowdoc.RefKey(e)] = arn + ":" + e.Alias
			}
		}
	}
	if len(refs) > 0 || !a.Refs.IsNull() {
		m, d := types.MapValueFrom(ctx, types.StringType, refs)
		diags.Append(d...)
		diags.Append(state.SetAttribute(ctx, path.Root("refs"), m)...)
	}
}

func (r *flowResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	p := plan(req.Config.Raw, r.kind, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	a := r.getAttrs(ctx, req.Plan, &resp.Diagnostics)
	old := r.getAttrs(ctx, req.State, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	instance, id := old.InstanceID.ValueString(), old.ConnectID.ValueString()
	// The exact bytes, not sameContent: a position-only change differs only
	// in Metadata, and the plan says it will be sent. Both sides are the
	// provider's own serialization, so an unchanged flow sends nothing.
	if p.content != old.Content.ValueString() {
		if err := r.updateContent(ctx, instance, id, p.content); err != nil {
			resp.Diagnostics.AddError("Updating the "+r.kind+" failed", connectapi.Detail(err))
			return
		}
	}
	state := old.State.ValueString()
	if !a.State.IsUnknown() && !a.State.IsNull() {
		state = a.State.ValueString()
	}
	if state == "" {
		state = "ACTIVE"
	}
	if a.connectName() != old.connectName() || !a.Description.Equal(old.Description) || state != old.State.ValueString() {
		if err := r.updateMetadata(ctx, instance, id, a.connectName(), a.Description.ValueString(), state); err != nil {
			resp.Diagnostics.AddError("Updating the "+r.kind+" failed", connectapi.Detail(err))
			return
		}
	}
	if err := syncTags(ctx, r.client.Connect, old.ARN.ValueString(), tagsOf(ctx, old.Tags), tagsOf(ctx, a.Tags)); err != nil {
		resp.Diagnostics.AddError("Updating tags failed", err.Error())
		return
	}
	resp.State.Raw = req.Plan.Raw
	for name, v := range map[string]string{
		"id": old.ID.ValueString(), r.idAttr(): id, "arn": old.ARN.ValueString(),
		"state": state, "flowdoc": p.flowdoc, "content": p.content, "content_hash": hashOf(p.content),
	} {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(name), types.StringValue(v))...)
	}
}

func (r *flowResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	a := r.getAttrs(ctx, req.State, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.delete(ctx, a.InstanceID.ValueString(), a.ConnectID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Deleting the "+r.kind+" failed", err.Error())
	}
}

func (r *flowResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	instance, id, ok := strings.Cut(req.ID, ":")
	if !ok || instance == "" || id == "" {
		resp.Diagnostics.AddError("Invalid import id", fmt.Sprintf("Expected instance_id:%s, got %q.", r.idAttr(), req.ID))
		return
	}
	for name, v := range map[string]string{"id": req.ID, "instance_id": instance, r.idAttr(): id} {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(name), types.StringValue(v))...)
	}
}

// connectName is the name Connect holds for the resource: display_name when
// set, name otherwise (the document's connectName).
func (a flowAttrs) connectName() string {
	if !a.DisplayName.IsNull() && !a.DisplayName.IsUnknown() {
		return a.DisplayName.ValueString()
	}
	return a.Name.ValueString()
}
