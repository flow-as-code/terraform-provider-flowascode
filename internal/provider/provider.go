// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

// Package provider is the flowascode provider: its configuration block, which
// follows hashicorp/aws's vocabulary so an existing AWS setup carries over, and
// the resources and data sources it serves.
package provider

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/connect"
	awsbase "github.com/hashicorp/aws-sdk-go-base/v2"
	basediag "github.com/hashicorp/aws-sdk-go-base/v2/diag"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/connectapi"
)

// New returns the provider factory providerserver.Serve takes.
func New(version string) func() provider.Provider {
	return func() provider.Provider { return &flowascode{version: version} }
}

type flowascode struct {
	version string
	// client, when set, is used as is and the provider block is ignored: the
	// tests' way in, with a fake Connect.
	client *connectapi.Client
}

// NewWithClient is New with a Connect client supplied rather than configured,
// for tests.
func NewWithClient(version string, client *connectapi.Client) func() provider.Provider {
	return func() provider.Provider { return &flowascode{version: version, client: client} }
}

var _ provider.Provider = (*flowascode)(nil)

// config is the provider block. Attribute names and meanings are
// hashicorp/aws's, so a configuration that authenticates that provider
// authenticates this one:
// https://registry.terraform.io/providers/hashicorp/aws/latest/docs#argument-reference
type config struct {
	Region                    types.String      `tfsdk:"region"`
	Profile                   types.String      `tfsdk:"profile"`
	AccessKey                 types.String      `tfsdk:"access_key"`
	SecretKey                 types.String      `tfsdk:"secret_key"`
	Token                     types.String      `tfsdk:"token"`
	SharedConfigFiles         []types.String    `tfsdk:"shared_config_files"`
	SharedCredentialsFiles    []types.String    `tfsdk:"shared_credentials_files"`
	MaxRetries                types.Int64       `tfsdk:"max_retries"`
	RetryMode                 types.String      `tfsdk:"retry_mode"`
	HTTPProxy                 types.String      `tfsdk:"http_proxy"`
	HTTPSProxy                types.String      `tfsdk:"https_proxy"`
	NoProxy                   types.String      `tfsdk:"no_proxy"`
	UseFIPSEndpoint           types.Bool        `tfsdk:"use_fips_endpoint"`
	UseDualStackEndpoint      types.Bool        `tfsdk:"use_dualstack_endpoint"`
	SkipCredentialsValidation types.Bool        `tfsdk:"skip_credentials_validation"`
	SkipRequestingAccountID   types.Bool        `tfsdk:"skip_requesting_account_id"`
	AssumeRole                *assumeRoleConfig `tfsdk:"assume_role"`
	Endpoints                 *endpointsConfig  `tfsdk:"endpoints"`
}

type assumeRoleConfig struct {
	RoleARN     types.String `tfsdk:"role_arn"`
	SessionName types.String `tfsdk:"session_name"`
	ExternalID  types.String `tfsdk:"external_id"`
	Duration    types.String `tfsdk:"duration"`
	Policy      types.String `tfsdk:"policy"`
}

type endpointsConfig struct {
	Connect types.String `tfsdk:"connect"`
	STS     types.String `tfsdk:"sts"`
}

func (p *flowascode) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "flowascode"
	resp.Version = p.version
}

func (p *flowascode) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	str := func(description string) schema.StringAttribute {
		return schema.StringAttribute{Optional: true, Description: description}
	}
	boolean := func(description string) schema.BoolAttribute {
		return schema.BoolAttribute{Optional: true, Description: description}
	}
	files := func(description string) schema.ListAttribute {
		return schema.ListAttribute{Optional: true, ElementType: types.StringType, Description: description}
	}
	resp.Schema = schema.Schema{
		Description: "Amazon Connect contact flows authored as HCL action blocks. Authentication " +
			"follows hashicorp/aws: the same attributes, environment variables and shared files.",
		Attributes: map[string]schema.Attribute{
			"region":                      str("AWS Region of the Connect instances. Also AWS_REGION."),
			"profile":                     str("Named profile in the shared configuration files. Also AWS_PROFILE."),
			"access_key":                  schema.StringAttribute{Optional: true, Sensitive: true, Description: "Static access key. Prefer a profile, environment variables or a role."},
			"secret_key":                  schema.StringAttribute{Optional: true, Sensitive: true, Description: "Static secret key."},
			"token":                       schema.StringAttribute{Optional: true, Sensitive: true, Description: "Session token for temporary credentials."},
			"shared_config_files":         files("Shared configuration files, in place of ~/.aws/config."),
			"shared_credentials_files":    files("Shared credentials files, in place of ~/.aws/credentials."),
			"max_retries":                 schema.Int64Attribute{Optional: true, Description: "Most retries for a retryable API error. Default 25, as hashicorp/aws."},
			"retry_mode":                  str(`"standard" or "adaptive". Also AWS_RETRY_MODE.`),
			"http_proxy":                  str("Proxy for HTTP requests. Also HTTP_PROXY."),
			"https_proxy":                 str("Proxy for HTTPS requests. Also HTTPS_PROXY."),
			"no_proxy":                    str("Hosts that bypass the proxy. Also NO_PROXY."),
			"use_fips_endpoint":           boolean("Use FIPS endpoints."),
			"use_dualstack_endpoint":      boolean("Use dual-stack endpoints."),
			"skip_credentials_validation": boolean("Skip the STS GetCallerIdentity check at configuration."),
			"skip_requesting_account_id":  boolean("Skip the account id lookup at configuration."),
		},
		Blocks: map[string]schema.Block{
			"assume_role": schema.SingleNestedBlock{
				Description: "A role to assume before calling Connect.",
				Attributes: map[string]schema.Attribute{
					"role_arn":     str("ARN of the role to assume."),
					"session_name": str("Session name for the assumed role."),
					"external_id":  str("External id the role's trust policy requires."),
					"duration":     str(`Session duration, a Go duration such as "1h".`),
					"policy":       str("A session policy, as JSON."),
				},
			},
			"endpoints": schema.SingleNestedBlock{
				Description: "Custom service endpoints, for testing against a local stand-in.",
				Attributes: map[string]schema.Attribute{
					"connect": str("Amazon Connect endpoint URL."),
					"sts":     str("AWS STS endpoint URL."),
				},
			},
		},
	}
}

func (p *flowascode) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	if p.client != nil {
		resp.ResourceData = p.client
		resp.DataSourceData = p.client
		return
	}
	var c config
	resp.Diagnostics.Append(req.Config.Get(ctx, &c)...)
	if resp.Diagnostics.HasError() {
		return
	}
	base, diags := baseConfig(c, p.version)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	_, awsConfig, baseDiags := awsbase.GetAwsConfig(ctx, base)
	resp.Diagnostics.Append(fromBase(baseDiags)...)
	if resp.Diagnostics.HasError() {
		return
	}
	endpoint := ""
	if c.Endpoints != nil {
		endpoint = c.Endpoints.Connect.ValueString()
	}
	client := &connectapi.Client{
		Connect: connect.NewFromConfig(awsConfig, func(o *connect.Options) {
			if endpoint != "" {
				o.BaseEndpoint = aws.String(endpoint)
			}
		}),
		Region: awsConfig.Region,
	}
	resp.ResourceData = client
	resp.DataSourceData = client
}

// baseConfig maps the provider block onto aws-sdk-go-base's configuration.
func baseConfig(c config, version string) (*awsbase.Config, diag.Diagnostics) {
	var diags diag.Diagnostics
	base := &awsbase.Config{
		AccessKey:               c.AccessKey.ValueString(),
		SecretKey:               c.SecretKey.ValueString(),
		Token:                   c.Token.ValueString(),
		Profile:                 c.Profile.ValueString(),
		Region:                  c.Region.ValueString(),
		MaxRetries:              25,
		NoProxy:                 c.NoProxy.ValueString(),
		HTTPProxyMode:           awsbase.HTTPProxyModeSeparate,
		SkipCredsValidation:     c.SkipCredentialsValidation.ValueBool(),
		SkipRequestingAccountId: c.SkipRequestingAccountID.ValueBool(),
		UseFIPSEndpoint:         c.UseFIPSEndpoint.ValueBool(),
		UseDualStackEndpoint:    c.UseDualStackEndpoint.ValueBool(),
		CallerName:              "flow-as-code/flowascode provider",
		CallerDocumentationURL:  "https://registry.terraform.io/providers/flow-as-code/flowascode/latest/docs",
		UserAgent: awsbase.UserAgentProducts{
			{Name: "terraform-provider-flowascode", Version: version},
		},
	}
	if !c.MaxRetries.IsNull() {
		base.MaxRetries = int(c.MaxRetries.ValueInt64())
	}
	switch mode := c.RetryMode.ValueString(); mode {
	case "":
	case "standard":
		base.RetryMode = aws.RetryModeStandard
	case "adaptive":
		base.RetryMode = aws.RetryModeAdaptive
	default:
		diags.AddAttributeError(pathOf("retry_mode"), "Invalid retry_mode",
			fmt.Sprintf(`retry_mode must be "standard" or "adaptive", got %q.`, mode))
	}
	if v := c.HTTPProxy.ValueString(); v != "" {
		base.HTTPProxy = aws.String(v)
	}
	if v := c.HTTPSProxy.ValueString(); v != "" {
		base.HTTPSProxy = aws.String(v)
	}
	for _, f := range c.SharedConfigFiles {
		base.SharedConfigFiles = append(base.SharedConfigFiles, f.ValueString())
	}
	for _, f := range c.SharedCredentialsFiles {
		base.SharedCredentialsFiles = append(base.SharedCredentialsFiles, f.ValueString())
	}
	if c.Endpoints != nil {
		base.StsEndpoint = c.Endpoints.STS.ValueString()
	}
	if r := c.AssumeRole; r != nil && r.RoleARN.ValueString() != "" {
		role := awsbase.AssumeRole{
			RoleARN:     r.RoleARN.ValueString(),
			SessionName: r.SessionName.ValueString(),
			ExternalID:  r.ExternalID.ValueString(),
			Policy:      r.Policy.ValueString(),
		}
		if d := r.Duration.ValueString(); d != "" {
			parsed, err := time.ParseDuration(d)
			if err != nil {
				diags.AddAttributeError(pathOf("assume_role").AtName("duration"), "Invalid duration", err.Error())
			}
			role.Duration = parsed
		}
		base.AssumeRole = []awsbase.AssumeRole{role}
	}
	return base, diags
}

// fromBase converts aws-sdk-go-base diagnostics to the framework's.
func fromBase(in basediag.Diagnostics) diag.Diagnostics {
	var out diag.Diagnostics
	for _, d := range in {
		if d.Severity() == basediag.SeverityError {
			out.AddError(d.Summary(), d.Detail())
		} else {
			out.AddWarning(d.Summary(), d.Detail())
		}
	}
	return out
}

func (p *flowascode) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{NewContactFlow}
}

func (p *flowascode) DataSources(_ context.Context) []func() datasource.DataSource {
	return nil
}
