// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

func TestSchemaIsValid(t *testing.T) {
	server, err := providerserver.NewProtocol6WithError(New("test")())()
	if err != nil {
		t.Fatal(err)
	}
	resp, err := server.GetProviderSchema(context.Background(), &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range resp.Diagnostics {
		t.Errorf("%s: %s", d.Summary, d.Detail)
	}
	if resp.Provider == nil || resp.Provider.Block == nil {
		t.Fatal("no provider schema")
	}
	for _, name := range []string{"region", "profile", "access_key", "max_retries", "retry_mode"} {
		found := false
		for _, a := range resp.Provider.Block.Attributes {
			if a.Name == name {
				found = true
			}
		}
		if !found {
			t.Errorf("provider schema has no %q", name)
		}
	}
}

func TestBaseConfigMapsTheProviderBlock(t *testing.T) {
	c := config{
		Region:     types.StringValue("us-west-2"),
		Profile:    types.StringValue("sandbox"),
		MaxRetries: types.Int64Value(3),
		RetryMode:  types.StringValue("adaptive"),
		HTTPSProxy: types.StringValue("http://proxy.example:3128"),
		SharedConfigFiles: []types.String{
			types.StringValue("/etc/aws/config"),
		},
		AssumeRole: &assumeRoleConfig{
			RoleARN:     types.StringValue("arn:aws:iam::111122223333:role/deployer"),
			SessionName: types.StringValue("flowascode"),
			Duration:    types.StringValue("30m"),
		},
		Endpoints: &endpointsConfig{STS: types.StringValue("http://localhost:4566")},
	}
	base, diags := baseConfig(c, "1.2.3")
	if diags.HasError() {
		t.Fatal(diags)
	}
	if base.Region != "us-west-2" || base.Profile != "sandbox" || base.MaxRetries != 3 {
		t.Errorf("region/profile/retries: %+v", base)
	}
	if base.RetryMode != aws.RetryModeAdaptive {
		t.Errorf("retry mode %q", base.RetryMode)
	}
	if base.HTTPSProxy == nil || *base.HTTPSProxy != "http://proxy.example:3128" || base.HTTPProxy != nil {
		t.Errorf("proxies: %v %v", base.HTTPProxy, base.HTTPSProxy)
	}
	if len(base.SharedConfigFiles) != 1 || base.StsEndpoint != "http://localhost:4566" {
		t.Errorf("files/endpoint: %+v", base)
	}
	if len(base.AssumeRole) != 1 || base.AssumeRole[0].Duration != 30*time.Minute ||
		base.AssumeRole[0].SessionName != "flowascode" {
		t.Errorf("assume role: %+v", base.AssumeRole)
	}
	if base.UserAgent[0].Version != "1.2.3" {
		t.Errorf("user agent: %+v", base.UserAgent)
	}
}

func TestBaseConfigDefaultsRetriesAsHashicorpAWS(t *testing.T) {
	base, diags := baseConfig(config{}, "dev")
	if diags.HasError() || base.MaxRetries != 25 || len(base.AssumeRole) != 0 {
		t.Errorf("defaults: %+v %v", base, diags)
	}
}

func TestBaseConfigRefusesAnUnknownRetryModeAndABadDuration(t *testing.T) {
	_, diags := baseConfig(config{
		RetryMode:  types.StringValue("eager"),
		AssumeRole: &assumeRoleConfig{RoleARN: types.StringValue("r"), Duration: types.StringValue("soon")},
	}, "dev")
	if diags.ErrorsCount() != 2 {
		t.Errorf("want 2 errors, got %v", diags)
	}
}
