// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/connect"
	ctypes "github.com/aws/aws-sdk-go-v2/service/connect/types"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/connectapi"
)

// view is data.flowascode_view: an AWS-managed or customer-managed view by
// name, for a view:<name> reference key to bind to (rule 27).
type view struct{ client *connectapi.Client }

// NewView is the data source factory.
func NewView() datasource.DataSource { return &view{} }

var _ datasource.DataSourceWithConfigure = (*view)(nil)

type viewModel struct {
	InstanceID types.String `tfsdk:"instance_id"`
	Name       types.String `tfsdk:"name"`
	Type       types.String `tfsdk:"type"`
	ID         types.String `tfsdk:"id"`
	ARN        types.String `tfsdk:"arn"`
}

func (d *view) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_view"
}

func (d *view) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A view in an Amazon Connect instance, found by name. " +
			"https://docs.aws.amazon.com/connect/latest/APIReference/API_ListViews.html",
		Attributes: map[string]schema.Attribute{
			"instance_id": schema.StringAttribute{Required: true},
			"name":        schema.StringAttribute{Required: true},
			"type": schema.StringAttribute{Optional: true, Description: "AWS_MANAGED or CUSTOMER_MANAGED; both are searched when omitted.",
				Validators: []validator.String{stringvalidator.OneOf("AWS_MANAGED", "CUSTOMER_MANAGED")}},
			"id": schema.StringAttribute{Computed: true},
			"arn": schema.StringAttribute{Computed: true,
				Description: "The view's ARN as ListViews returns it, without a version. Bind it to a `view:<name>@<version>` key: the flow receives this ARN with `:<version>` added, the form the console writes."},
		},
	}
}

func (d *view) Configure(_ context.Context, req datasource.ConfigureRequest, _ *datasource.ConfigureResponse) {
	if c, ok := req.ProviderData.(*connectapi.Client); ok {
		d.client = c
	}
}

func (d *view) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m viewModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	types_ := []ctypes.ViewType{ctypes.ViewTypeAwsManaged, ctypes.ViewTypeCustomerManaged}
	if !m.Type.IsNull() {
		types_ = []ctypes.ViewType{ctypes.ViewType(m.Type.ValueString())}
	}
	var found []ctypes.ViewSummary
	for _, t := range types_ {
		var token *string
		for {
			out, err := d.client.Connect.ListViews(ctx, &connect.ListViewsInput{
				InstanceId: aws.String(m.InstanceID.ValueString()), Type: t, NextToken: token, MaxResults: aws.Int32(100)})
			if err != nil {
				resp.Diagnostics.AddError("ListViews failed", err.Error())
				return
			}
			for _, v := range out.ViewsSummaryList {
				if aws.ToString(v.Name) == m.Name.ValueString() {
					found = append(found, v)
				}
			}
			if out.NextToken == nil {
				break
			}
			token = out.NextToken
		}
	}
	switch len(found) {
	case 0:
		resp.Diagnostics.AddError("No such view", fmt.Sprintf("No view named %q in the instance.", m.Name.ValueString()))
		return
	case 1:
	default:
		resp.Diagnostics.AddError("Ambiguous view", fmt.Sprintf("%d views are named %q; set type.", len(found), m.Name.ValueString()))
		return
	}
	m.ID = types.StringValue(aws.ToString(found[0].Id))
	m.ARN = types.StringValue(aws.ToString(found[0].Arn))
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
