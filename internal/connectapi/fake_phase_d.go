// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package connectapi

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/connect"
	"github.com/aws/aws-sdk-go-v2/service/connect/types"
	"github.com/aws/aws-sdk-go-v2/service/connectcases"
	casestypes "github.com/aws/aws-sdk-go-v2/service/connectcases/types"
	"github.com/aws/aws-sdk-go-v2/service/qconnect"
	qtypes "github.com/aws/aws-sdk-go-v2/service/qconnect/types"
)

// The FlowDoc 0.3 listings: the two Connect lists and the integration
// association that names the Cases domain on the Fake, and fakes of the
// Cases and Amazon Q in Connect clients. Each pages as its service does,
// with the documented MaxResults range refused outside it.

// SetTaskTemplates sets what ListTaskTemplates returns.
func (f *Fake) SetTaskTemplates(templates []types.TaskTemplateMetadata) {
	for _, t := range templates {
		mustBePlaceholder(aws.ToString(t.Arn))
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.taskTemplates = templates
}

// SetPhoneNumbers sets what ListPhoneNumbersV2 returns.
func (f *Fake) SetPhoneNumbers(numbers []types.ListPhoneNumbersSummary) {
	for _, n := range numbers {
		mustBePlaceholder(aws.ToString(n.PhoneNumberArn))
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.phoneNumbers = numbers
}

// SetIntegrationAssociations sets what ListIntegrationAssociations returns,
// before its IntegrationType filter.
func (f *Fake) SetIntegrationAssociations(associations []types.IntegrationAssociationSummary) {
	for _, a := range associations {
		mustBePlaceholder(aws.ToString(a.IntegrationArn))
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.integrations = associations
}

// ListTaskTemplates caps a page at 100.
// https://docs.aws.amazon.com/connect/latest/APIReference/API_ListTaskTemplates.html
func (f *Fake) ListTaskTemplates(_ context.Context, in *connect.ListTaskTemplatesInput, _ ...func(*connect.Options)) (*connect.ListTaskTemplatesOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("ListTaskTemplates")
	page, next, err := fakePage("ListTaskTemplates", f.taskTemplates, in.MaxResults, 100, in.NextToken)
	if err != nil {
		return nil, err
	}
	return &connect.ListTaskTemplatesOutput{TaskTemplates: page, NextToken: next}, nil
}

// ListPhoneNumbersV2 takes the instance as InstanceId or TargetArn; the
// fake holds one instance's numbers and lists them for either, and caps a
// page at 1000.
// https://docs.aws.amazon.com/connect/latest/APIReference/API_ListPhoneNumbersV2.html
func (f *Fake) ListPhoneNumbersV2(_ context.Context, in *connect.ListPhoneNumbersV2Input, _ ...func(*connect.Options)) (*connect.ListPhoneNumbersV2Output, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("ListPhoneNumbersV2")
	page, next, err := fakePage("ListPhoneNumbersV2", f.phoneNumbers, in.MaxResults, 1000, in.NextToken)
	if err != nil {
		return nil, err
	}
	return &connect.ListPhoneNumbersV2Output{ListPhoneNumbersSummaryList: page, NextToken: next}, nil
}

// ListIntegrationAssociations filters by IntegrationType and caps a page at
// 100.
// https://docs.aws.amazon.com/connect/latest/APIReference/API_ListIntegrationAssociations.html
func (f *Fake) ListIntegrationAssociations(_ context.Context, in *connect.ListIntegrationAssociationsInput, _ ...func(*connect.Options)) (*connect.ListIntegrationAssociationsOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("ListIntegrationAssociations")
	var all []types.IntegrationAssociationSummary
	for _, a := range f.integrations {
		if in.IntegrationType == "" || a.IntegrationType == in.IntegrationType {
			all = append(all, a)
		}
	}
	page, next, err := fakePage("ListIntegrationAssociations", all, in.MaxResults, 100, in.NextToken)
	if err != nil {
		return nil, err
	}
	return &connect.ListIntegrationAssociationsOutput{IntegrationAssociationSummaryList: page, NextToken: next}, nil
}

// FakeCases is an in-memory Cases client: one domain's templates and fields,
// listed only for that domain id.
type FakeCases struct {
	DomainID  string
	Templates []casestypes.TemplateSummary
	Fields    []casestypes.FieldSummary
	// Calls records every operation, in order.
	Calls []string
}

var _ CasesAPI = (*FakeCases)(nil)

// ListTemplates caps a page at 100.
// https://docs.aws.amazon.com/connect/latest/APIReference/API_connect-cases_ListTemplates.html
func (c *FakeCases) ListTemplates(_ context.Context, in *connectcases.ListTemplatesInput, _ ...func(*connectcases.Options)) (*connectcases.ListTemplatesOutput, error) {
	c.Calls = append(c.Calls, "ListTemplates")
	if aws.ToString(in.DomainId) != c.DomainID {
		return nil, &casestypes.ResourceNotFoundException{Message: aws.String("domain " + aws.ToString(in.DomainId) + " not found")}
	}
	page, next, err := fakePage("ListTemplates", c.Templates, in.MaxResults, 100, in.NextToken)
	if err != nil {
		return nil, err
	}
	return &connectcases.ListTemplatesOutput{Templates: page, NextToken: next}, nil
}

// ListFields caps a page at 100.
// https://docs.aws.amazon.com/connect/latest/APIReference/API_connect-cases_ListFields.html
func (c *FakeCases) ListFields(_ context.Context, in *connectcases.ListFieldsInput, _ ...func(*connectcases.Options)) (*connectcases.ListFieldsOutput, error) {
	c.Calls = append(c.Calls, "ListFields")
	if aws.ToString(in.DomainId) != c.DomainID {
		return nil, &casestypes.ResourceNotFoundException{Message: aws.String("domain " + aws.ToString(in.DomainId) + " not found")}
	}
	page, next, err := fakePage("ListFields", c.Fields, in.MaxResults, 100, in.NextToken)
	if err != nil {
		return nil, err
	}
	return &connectcases.ListFieldsOutput{Fields: page, NextToken: next}, nil
}

// FakeQConnect is an in-memory Amazon Q in Connect client: the account's
// assistants in the Region.
type FakeQConnect struct {
	Assistants []qtypes.AssistantSummary
	// Calls records every operation, in order.
	Calls []string
}

var _ QConnectAPI = (*FakeQConnect)(nil)

// ListAssistants caps a page at 100.
// https://docs.aws.amazon.com/amazon-q-connect/latest/APIReference/API_ListAssistants.html
func (q *FakeQConnect) ListAssistants(_ context.Context, in *qconnect.ListAssistantsInput, _ ...func(*qconnect.Options)) (*qconnect.ListAssistantsOutput, error) {
	q.Calls = append(q.Calls, "ListAssistants")
	page, next, err := fakePage("ListAssistants", q.Assistants, in.MaxResults, 100, in.NextToken)
	if err != nil {
		return nil, err
	}
	return &qconnect.ListAssistantsOutput{AssistantSummaries: page, NextToken: next}, nil
}
