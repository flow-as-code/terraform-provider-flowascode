// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/connect"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// The live acceptance lane (acceptance.yml). It needs TF_ACC=1,
// FLOWASCODE_ACC_INSTANCE_ARN (the sandbox instance's ARN, from a repository
// secret or the environment, never from a file) and AWS credentials for
// the account. Everything it creates is named with FLOWASCODE_ACC_PREFIX and
// tagged flowascode-acc, which TestAccSweep deletes.

type accEnv struct {
	instanceID, region, prefix string
	api                        *connect.Client
}

var accInstanceARN = regexp.MustCompile(`^arn:aws[a-z-]*:connect:([a-z0-9-]+):[0-9]{12}:instance/([0-9a-f-]{36})$`)

func accPreCheck(t *testing.T) accEnv {
	t.Helper()
	if os.Getenv("TF_ACC") == "" {
		t.Skip("set TF_ACC=1 to run acceptance tests")
	}
	m := accInstanceARN.FindStringSubmatch(os.Getenv("FLOWASCODE_ACC_INSTANCE_ARN"))
	if m == nil {
		t.Fatal("FLOWASCODE_ACC_INSTANCE_ARN must be a Connect instance ARN")
	}
	terraformBinary(t)
	prefix := os.Getenv("FLOWASCODE_ACC_PREFIX")
	if prefix == "" {
		b := make([]byte, 4)
		_, _ = rand.Read(b)
		prefix = "tfacc-local-" + hex.EncodeToString(b)
	}
	cfg, err := awsconfig.LoadDefaultConfig(context.Background(), awsconfig.WithRegion(m[1]))
	if err != nil {
		t.Fatal(err)
	}
	return accEnv{instanceID: m[2], region: m[1], prefix: prefix, api: connect.NewFromConfig(cfg)}
}

func accFactories() map[string]func() (tfprotov6.ProviderServer, error) {
	return map[string]func() (tfprotov6.ProviderServer, error){
		"flowascode": providerserver.NewProtocol6WithError(New("acc")()),
	}
}

func (e accEnv) providerBlock() string {
	return fmt.Sprintf("provider \"flowascode\" {\n  region = %q\n}\n", e.region)
}

func (e accEnv) flow(name, text string, tags string) string {
	return e.providerBlock() + fmt.Sprintf(`
resource "flowascode_contact_flow" "acc" {
  instance_id = %q
  name        = %q
  type        = "CONTACT_FLOW"
  description = "flowascode acceptance test"

  tags = {
    flowascode-acc = "true"
%s  }

  action {
    id   = "welcome"
    next = "bye"
    message_participant {
      text = %q
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
`, e.instanceID, name, tags, text)
}

// Create, update in place (content and tags), import, and destroy.
func TestAccContactFlowLifecycle(t *testing.T) {
	e := accPreCheck(t)
	name := e.prefix + "-line"
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: accFactories(),
		Steps: []resource.TestStep{
			{Config: e.flow(name, "Welcome.", "")},
			{
				Config: e.flow(name, "Welcome back.", "    team = \"cx\"\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("flowascode_contact_flow.acc", plancheck.ResourceActionUpdate),
				}},
			},
			{
				ResourceName:      "flowascode_contact_flow.acc",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// An edit made outside Terraform shows in the next plan, and apply restores
// the configuration.
func TestAccContactFlowDrift(t *testing.T) {
	e := accPreCheck(t)
	name := e.prefix + "-drift"
	var flowID string
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: accFactories(),
		Steps: []resource.TestStep{
			{
				Config: e.flow(name, "Welcome.", ""),
				Check: func(s *terraform.State) error {
					flowID = s.RootModule().Resources["flowascode_contact_flow.acc"].Primary.Attributes["contact_flow_id"]
					return nil
				},
			},
			{
				PreConfig: func() {
					out, err := e.api.DescribeContactFlow(context.Background(), &connect.DescribeContactFlowInput{
						InstanceId: aws.String(e.instanceID), ContactFlowId: aws.String(flowID)})
					if err != nil {
						t.Fatal(err)
					}
					edited := strings.Replace(aws.ToString(out.ContactFlow.Content), "Welcome.", "Edited outside Terraform.", 1)
					if _, err := e.api.UpdateContactFlowContent(context.Background(), &connect.UpdateContactFlowContentInput{
						InstanceId: aws.String(e.instanceID), ContactFlowId: aws.String(flowID), Content: aws.String(edited)}); err != nil {
						t.Fatal(err)
					}
				},
				Config: e.flow(name, "Welcome.", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("flowascode_contact_flow.acc", plancheck.ResourceActionUpdate),
				}},
			},
		},
	})
}

// A hard lint rule fails the plan, so nothing reaches Connect.
func TestAccLintRefusesAHardRule(t *testing.T) {
	e := accPreCheck(t)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: accFactories(),
		Steps: []resource.TestStep{{
			Config:      e.flow(e.prefix+"-lint", "Call arn:aws:connect:us-east-1:111122223333:instance/x/queue/y", ""),
			ExpectError: regexp.MustCompile(`no-literal-arn`),
		}},
	})
}

// A module, a version keyed to its content_hash, an alias, and an edit that
// moves the alias to a new version.
func TestAccModuleVersionAndAlias(t *testing.T) {
	e := accPreCheck(t)
	cfg := func(text string) string {
		return e.providerBlock() + fmt.Sprintf(`
resource "flowascode_contact_flow_module" "acc" {
  instance_id = %[1]q
  name        = %[2]q

  tags = {
    flowascode-acc = "true"
  }

  action {
    id   = "say"
    next = "end"
    message_participant {
      text = %[3]q
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

resource "flowascode_contact_flow_module_version" "acc" {
  instance_id            = %[1]q
  contact_flow_module_id = flowascode_contact_flow_module.acc.contact_flow_module_id
  content_hash           = flowascode_contact_flow_module.acc.content_hash

  lifecycle {
    create_before_destroy = true
  }
}

resource "flowascode_contact_flow_module_alias" "acc" {
  instance_id                 = %[1]q
  contact_flow_module_id      = flowascode_contact_flow_module.acc.contact_flow_module_id
  name                        = "prod"
  contact_flow_module_version = flowascode_contact_flow_module_version.acc.version
}

resource "flowascode_contact_flow" "acc" {
  instance_id = %[1]q
  name        = %[4]q
  type        = "CONTACT_FLOW"

  refs = {
    "module:%[2]s@prod" = flowascode_contact_flow_module_alias.acc.arn
  }

  tags = {
    flowascode-acc = "true"
  }

  action {
    id   = "invoke"
    next = "bye"
    invoke_flow_module {
      flow_module_id = "module:%[2]s@prod"
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
`, e.instanceID, e.prefix+"-module", text, e.prefix+"-invokes-alias")
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: accFactories(),
		Steps: []resource.TestStep{
			{Config: cfg("First.")},
			{Config: cfg("Second."), Check: resource.ComposeTestCheckFunc(
				resource.TestCheckResourceAttr("flowascode_contact_flow_module_alias.acc", "contact_flow_module_version", "2"),
				// The flow invokes the alias through <module ARN>:<alias id>,
				// the only qualifier Connect runs as the alias.
				func(s *terraform.State) error {
					res := s.RootModule().Resources
					alias := res["flowascode_contact_flow_module_alias.acc"].Primary.Attributes
					module := res["flowascode_contact_flow_module.acc"].Primary.Attributes["arn"]
					if want := module + ":" + alias["alias_id"]; alias["arn"] != want {
						return fmt.Errorf("alias arn is not the module ARN qualified by the alias id")
					}
					if !strings.Contains(res["flowascode_contact_flow.acc"].Primary.Attributes["content"], fmt.Sprintf("%q", alias["arn"])) {
						return fmt.Errorf("the flow does not invoke the alias ARN")
					}
					return nil
				},
			)},
			// Import reads the flow's invocation, <module ARN>:<alias id>, back
			// as module:<name>@prod through the instance's alias listing.
			{
				Config:            cfg("Second."),
				ResourceName:      "flowascode_contact_flow.acc",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// awsProvider is hashicorp/aws, pinned, for the resources a flow refers to
// and for the resource a moved block starts from.
var awsProvider = map[string]resource.ExternalProvider{
	"aws": {Source: "hashicorp/aws", VersionConstraint: "6.66.0"},
}

// References bound in refs to resources hashicorp/aws creates reach Connect
// as those resources' ARNs, and import reads them back as the same keys
// and bindings through the instance's inventory.
func TestAccReferencesResolveToLiveResources(t *testing.T) {
	e := accPreCheck(t)
	hours, queue := e.prefix+"-hours", e.prefix+"-queue"
	cfg := e.providerBlock() + fmt.Sprintf(`
provider "aws" {
  region = %[2]q
}

resource "aws_connect_hours_of_operation" "acc" {
  instance_id = %[1]q
  name        = %[3]q
  time_zone   = "UTC"
  tags        = { flowascode-acc = "true" }

  config {
    day = "MONDAY"
    start_time {
      hours   = 9
      minutes = 0
    }
    end_time {
      hours   = 17
      minutes = 0
    }
  }
}

resource "aws_connect_queue" "acc" {
  instance_id           = %[1]q
  name                  = %[4]q
  hours_of_operation_id = aws_connect_hours_of_operation.acc.hours_of_operation_id
  tags                  = { flowascode-acc = "true" }
}

resource "flowascode_contact_flow" "acc" {
  instance_id = %[1]q
  name        = %[5]q
  type        = "CONTACT_FLOW"

  refs = {
    "hours:%[3]s" = aws_connect_hours_of_operation.acc.arn
    "queue:%[4]s" = aws_connect_queue.acc.arn
  }

  tags = {
    flowascode-acc = "true"
  }

  action {
    id   = "check-hours"
    next = "bye"
    check_hours_of_operation {
      hours_of_operation_id = "hours:%[3]s"
    }
    condition {
      operator = "Equals"
      operands = ["True"]
      next     = "set-queue"
    }
    condition {
      operator = "Equals"
      operands = ["False"]
      next     = "bye"
    }
    error {
      type = "NoMatchingError"
      next = "bye"
    }
  }

  action {
    id   = "set-queue"
    next = "bye"
    update_contact_target_queue {
      queue_id = "queue:%[4]s"
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
`, e.instanceID, e.region, hours, queue, e.prefix+"-refs")
	contentHolds := func(s *terraform.State) error {
		res := s.RootModule().Resources
		content := res["flowascode_contact_flow.acc"].Primary.Attributes["content"]
		for _, addr := range []string{"aws_connect_hours_of_operation.acc", "aws_connect_queue.acc"} {
			arn := res[addr].Primary.Attributes["arn"]
			if arn == "" || !strings.Contains(content, fmt.Sprintf("%q", arn)) {
				return fmt.Errorf("content does not hold %s's ARN", addr)
			}
		}
		if strings.Contains(content, "${cdref:") {
			return fmt.Errorf("content still holds a token")
		}
		return nil
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: accFactories(),
		ExternalProviders:        awsProvider,
		Steps: []resource.TestStep{
			{Config: cfg, Check: contentHolds},
			{
				ResourceName:      "flowascode_contact_flow.acc",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// A flow hashicorp/aws manages moves to flowascode_contact_flow in place: the
// same flow in Connect, and no change left after the first apply.
func TestAccMovedFromAWSProvider(t *testing.T) {
	e := accPreCheck(t)
	name := e.prefix + "-moved"
	awsFlow := e.providerBlock() + fmt.Sprintf(`
provider "aws" {
  region = %[2]q
}

resource "aws_connect_contact_flow" "acc" {
  instance_id = %[1]q
  name        = %[3]q
  type        = "CONTACT_FLOW"
  description = "flowascode acceptance test"
  tags        = { flowascode-acc = "true" }
  content = jsonencode({
    Version     = "2019-10-30"
    StartAction = "welcome"
    Actions = [
      {
        Identifier = "welcome"
        Type       = "MessageParticipant"
        Parameters = { Text = "Welcome." }
        Transitions = {
          NextAction = "bye"
          Errors     = [{ NextAction = "bye", ErrorType = "NoMatchingError" }]
        }
      },
      {
        Identifier  = "bye"
        Type        = "DisconnectParticipant"
        Parameters  = {}
        Transitions = {}
      },
    ]
  })
}
`, e.instanceID, e.region, name)
	moved := e.flow(name, "Welcome.", "") + fmt.Sprintf(`
provider "aws" {
  region = %q
}

moved {
  from = aws_connect_contact_flow.acc
  to   = flowascode_contact_flow.acc
}
`, e.region)
	var flowID string
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: accFactories(),
		ExternalProviders:        awsProvider,
		Steps: []resource.TestStep{
			{
				Config: awsFlow,
				Check: func(s *terraform.State) error {
					flowID = s.RootModule().Resources["aws_connect_contact_flow.acc"].Primary.Attributes["contact_flow_id"]
					return nil
				},
			},
			{
				Config: moved,
				Check: func(s *terraform.State) error {
					got := s.RootModule().Resources["flowascode_contact_flow.acc"].Primary.Attributes["contact_flow_id"]
					if flowID == "" || got != flowID {
						return fmt.Errorf("the move recreated the flow: %q became %q", flowID, got)
					}
					return nil
				},
			},
		},
	})
}

// TestAccSweep deletes every flow, module, queue and hours of operation this
// lane created: names starting tfacc-. It runs before and after the lane
// (acceptance.yml). Flows go first, since a flow may refer to a queue.
func TestAccSweep(t *testing.T) {
	e := accPreCheck(t)
	ctx := context.Background()
	flows := connect.NewListContactFlowsPaginator(e.api, &connect.ListContactFlowsInput{InstanceId: aws.String(e.instanceID)})
	for flows.HasMorePages() {
		page, err := flows.NextPage(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range page.ContactFlowSummaryList {
			if strings.HasPrefix(aws.ToString(f.Name), "tfacc-") {
				t.Logf("deleting flow %s", aws.ToString(f.Name))
				if _, err := e.api.DeleteContactFlow(ctx, &connect.DeleteContactFlowInput{InstanceId: aws.String(e.instanceID), ContactFlowId: f.Id}); err != nil {
					t.Errorf("deleting %s: %v", aws.ToString(f.Name), err)
				}
			}
		}
	}
	modules := connect.NewListContactFlowModulesPaginator(e.api, &connect.ListContactFlowModulesInput{InstanceId: aws.String(e.instanceID)})
	for modules.HasMorePages() {
		page, err := modules.NextPage(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range page.ContactFlowModulesSummaryList {
			if strings.HasPrefix(aws.ToString(m.Name), "tfacc-") {
				t.Logf("deleting module %s", aws.ToString(m.Name))
				if _, err := e.api.DeleteContactFlowModule(ctx, &connect.DeleteContactFlowModuleInput{InstanceId: aws.String(e.instanceID), ContactFlowModuleId: m.Id}); err != nil {
					t.Errorf("deleting %s: %v", aws.ToString(m.Name), err)
				}
			}
		}
	}
	// https://docs.aws.amazon.com/connect/latest/APIReference/API_DeleteQueue.html
	queues := connect.NewListQueuesPaginator(e.api, &connect.ListQueuesInput{InstanceId: aws.String(e.instanceID)})
	for queues.HasMorePages() {
		page, err := queues.NextPage(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, q := range page.QueueSummaryList {
			if strings.HasPrefix(aws.ToString(q.Name), "tfacc-") {
				t.Logf("deleting queue %s", aws.ToString(q.Name))
				if _, err := e.api.DeleteQueue(ctx, &connect.DeleteQueueInput{InstanceId: aws.String(e.instanceID), QueueId: q.Id}); err != nil {
					t.Errorf("deleting %s: %v", aws.ToString(q.Name), err)
				}
			}
		}
	}
	// https://docs.aws.amazon.com/connect/latest/APIReference/API_DeleteHoursOfOperation.html
	hours := connect.NewListHoursOfOperationsPaginator(e.api, &connect.ListHoursOfOperationsInput{InstanceId: aws.String(e.instanceID)})
	for hours.HasMorePages() {
		page, err := hours.NextPage(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, h := range page.HoursOfOperationSummaryList {
			if strings.HasPrefix(aws.ToString(h.Name), "tfacc-") {
				t.Logf("deleting hours of operation %s", aws.ToString(h.Name))
				if _, err := e.api.DeleteHoursOfOperation(ctx, &connect.DeleteHoursOfOperationInput{InstanceId: aws.String(e.instanceID), HoursOfOperationId: h.Id}); err != nil {
					t.Errorf("deleting %s: %v", aws.ToString(h.Name), err)
				}
			}
		}
	}
}
