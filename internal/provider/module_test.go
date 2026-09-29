// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/connect"
	ctypes "github.com/aws/aws-sdk-go-v2/service/connect/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/connectapi"
)

const instance = "11111111-2222-3333-4444-555555555555"

// promotion is the pattern docs/06-terraform-provider.md describes: a module,
// a version keyed to its content_hash (replaced, create before destroy, when
// the module changes), an alias pointing at the version, and a flow invoking
// the module through the alias.
func promotion(greeting string) string {
	return fmt.Sprintf(`
resource "flowascode_contact_flow_module" "survey" {
  instance_id = %[1]q
  name        = "survey"

  action {
    id   = "ask"
    next = "end"
    message_participant {
      text = %[2]q
    }
    error {
      type = "NoMatchingError"
      next = "end"
    }
  }

  action {
    id = "end"
    end_flow_module_execution {}
  }
}

resource "flowascode_contact_flow_module_version" "survey" {
  instance_id            = %[1]q
  contact_flow_module_id = flowascode_contact_flow_module.survey.contact_flow_module_id
  content_hash           = flowascode_contact_flow_module.survey.content_hash

  lifecycle {
    create_before_destroy = true
  }
}

resource "flowascode_contact_flow_module_alias" "survey_prod" {
  instance_id                 = %[1]q
  contact_flow_module_id      = flowascode_contact_flow_module.survey.contact_flow_module_id
  name                        = "prod"
  contact_flow_module_version = flowascode_contact_flow_module_version.survey.version
}

resource "flowascode_contact_flow" "line" {
  instance_id = %[1]q
  name        = "line"
  type        = "CONTACT_FLOW"

  refs = {
    "module:survey@prod" = flowascode_contact_flow_module_alias.survey_prod.arn
  }

  action {
    id   = "invoke"
    next = "bye"
    invoke_flow_module {
      flow_module_id = "module:survey@prod"
    }
    error {
      type = "NoMatchingError"
      next = "bye"
    }
  }

  action {
    id = "bye"
    disconnect_participant {}
  }
}
`, instance, greeting)
}

func TestModulePromotionRepointsTheAlias(t *testing.T) {
	terraformBinary(t)
	fake := connectapi.NewFake()
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories(fake),
		Steps: []resource.TestStep{
			{
				Config: promotion("How did we do?"),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("flowascode_contact_flow_module_version.survey", tfjsonpath.New("version"), knownvalue.Int64Exact(1)),
					statecheck.ExpectKnownValue("flowascode_contact_flow_module_alias.survey_prod", tfjsonpath.New("contact_flow_module_version"), knownvalue.Int64Exact(1)),
					statecheck.ExpectKnownValue("flowascode_contact_flow.line", tfjsonpath.New("content"),
						knownvalue.StringRegexp(regexp.MustCompile(`"FlowModuleId":\s*"arn:aws:connect:[^"]+:prod"`))),
				},
			},
			{
				// The module changes: a new version is snapshotted first, the
				// alias moves to it, then the old version goes.
				Config: promotion("How did we do today?"),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("flowascode_contact_flow_module_version.survey", tfjsonpath.New("version"), knownvalue.Int64Exact(2)),
					statecheck.ExpectKnownValue("flowascode_contact_flow_module_alias.survey_prod", tfjsonpath.New("contact_flow_module_version"), knownvalue.Int64Exact(2)),
				},
			},
		},
	})
}

// Connect refuses to delete a version an alias points at, so a version
// replaced without create_before_destroy fails, and the error says what to set.
func TestModuleVersionWithoutCreateBeforeDestroyExplainsTheRefusal(t *testing.T) {
	terraformBinary(t)
	fake := connectapi.NewFake()
	withoutCBD := func(greeting string) string {
		return strings.Replace(promotion(greeting), "\n  lifecycle {\n    create_before_destroy = true\n  }\n", "", 1)
	}
	if withoutCBD("x") == promotion("x") {
		t.Fatal("the lifecycle block was not removed")
	}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories(fake),
		Steps: []resource.TestStep{
			{Config: withoutCBD("How did we do?")},
			{
				Config:      withoutCBD("How did we do today?"),
				ExpectError: regexp.MustCompile(`(?s)tied to one alias.*create_before_destroy = true`),
			},
		},
	})
}

func TestModuleVersionRefusesContentItWasNotKeyedTo(t *testing.T) {
	terraformBinary(t)
	fake := connectapi.NewFake()
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories(fake),
		Steps: []resource.TestStep{{
			Config: fmt.Sprintf(`
resource "flowascode_contact_flow_module_version" "stale" {
  instance_id            = %q
  contact_flow_module_id = "module-1"
  content_hash           = "0000000000000000000000000000000000000000000000000000000000000000"
}
`, instance),
			PreConfig: func() {
				_, _ = fake.CreateContactFlowModule(t.Context(), moduleInput())
			},
			ExpectError: regexp.MustCompile(`live content_hash is\s+[0-9a-f]{64},\s+not\s+0{64}`),
		}},
	})
}

func TestViewDataSourceFindsAViewByName(t *testing.T) {
	terraformBinary(t)
	fake := connectapi.NewFake()
	fake.SetViews([]ctypes.ViewSummary{
		{Id: aws.String("v-1"), Name: aws.String("after-contact-work"), Type: ctypes.ViewTypeAwsManaged,
			Arn: aws.String("arn:aws:connect:us-east-1:aws:view/after-contact-work")},
		{Id: aws.String("v-2"), Name: aws.String("intake"), Type: ctypes.ViewTypeCustomerManaged,
			Arn: aws.String("arn:aws:connect:us-east-1:111122223333:instance/i/view/v-2")},
	})
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories(fake),
		Steps: []resource.TestStep{{
			Config: fmt.Sprintf(`
data "flowascode_view" "acw" {
  instance_id = %q
  name        = "after-contact-work"
}
`, instance),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue("data.flowascode_view.acw", tfjsonpath.New("arn"),
					knownvalue.StringExact("arn:aws:connect:us-east-1:aws:view/after-contact-work")),
			},
		}},
	})
}

func moduleInput() *connect.CreateContactFlowModuleInput {
	return &connect.CreateContactFlowModuleInput{
		InstanceId: aws.String(instance), Name: aws.String("survey"),
		Content: aws.String(`{"Version":"2019-10-30","StartAction":"end","Settings":{},"Actions":[{"Identifier":"end","Type":"EndFlowModuleExecution","Parameters":{},"Transitions":{}}]}`),
	}
}
