// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

// Package connectapi is the seam between the provider and Amazon Connect:
// exactly the operations the resources and the export inventory adapter call,
// each with its API reference, so both can be tested against a fake.
package connectapi

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/connect"
	"github.com/aws/aws-sdk-go-v2/service/connect/types"
)

// Client is what a configured provider hands its resources and data sources.
type Client struct {
	// Connect is the SDK client, behind the API interface.
	Connect API
	// Region the client was configured for, used in diagnostics.
	Region string
}

// API is the subset of the Connect SDK client the provider uses.
type API interface {
	// https://docs.aws.amazon.com/connect/latest/APIReference/API_DescribeInstance.html
	DescribeInstance(ctx context.Context, in *connect.DescribeInstanceInput, opts ...func(*connect.Options)) (*connect.DescribeInstanceOutput, error)
	// https://docs.aws.amazon.com/connect/latest/APIReference/API_CreateContactFlow.html
	CreateContactFlow(ctx context.Context, in *connect.CreateContactFlowInput, opts ...func(*connect.Options)) (*connect.CreateContactFlowOutput, error)
	// https://docs.aws.amazon.com/connect/latest/APIReference/API_DescribeContactFlow.html
	DescribeContactFlow(ctx context.Context, in *connect.DescribeContactFlowInput, opts ...func(*connect.Options)) (*connect.DescribeContactFlowOutput, error)
	// https://docs.aws.amazon.com/connect/latest/APIReference/API_UpdateContactFlowContent.html
	UpdateContactFlowContent(ctx context.Context, in *connect.UpdateContactFlowContentInput, opts ...func(*connect.Options)) (*connect.UpdateContactFlowContentOutput, error)
	// https://docs.aws.amazon.com/connect/latest/APIReference/API_UpdateContactFlowMetadata.html
	UpdateContactFlowMetadata(ctx context.Context, in *connect.UpdateContactFlowMetadataInput, opts ...func(*connect.Options)) (*connect.UpdateContactFlowMetadataOutput, error)
	// https://docs.aws.amazon.com/connect/latest/APIReference/API_DeleteContactFlow.html
	DeleteContactFlow(ctx context.Context, in *connect.DeleteContactFlowInput, opts ...func(*connect.Options)) (*connect.DeleteContactFlowOutput, error)
	// https://docs.aws.amazon.com/connect/latest/APIReference/API_CreateContactFlowModule.html
	CreateContactFlowModule(ctx context.Context, in *connect.CreateContactFlowModuleInput, opts ...func(*connect.Options)) (*connect.CreateContactFlowModuleOutput, error)
	// https://docs.aws.amazon.com/connect/latest/APIReference/API_DescribeContactFlowModule.html
	DescribeContactFlowModule(ctx context.Context, in *connect.DescribeContactFlowModuleInput, opts ...func(*connect.Options)) (*connect.DescribeContactFlowModuleOutput, error)
	// https://docs.aws.amazon.com/connect/latest/APIReference/API_UpdateContactFlowModuleContent.html
	UpdateContactFlowModuleContent(ctx context.Context, in *connect.UpdateContactFlowModuleContentInput, opts ...func(*connect.Options)) (*connect.UpdateContactFlowModuleContentOutput, error)
	// https://docs.aws.amazon.com/connect/latest/APIReference/API_UpdateContactFlowModuleMetadata.html
	UpdateContactFlowModuleMetadata(ctx context.Context, in *connect.UpdateContactFlowModuleMetadataInput, opts ...func(*connect.Options)) (*connect.UpdateContactFlowModuleMetadataOutput, error)
	// https://docs.aws.amazon.com/connect/latest/APIReference/API_DeleteContactFlowModule.html
	DeleteContactFlowModule(ctx context.Context, in *connect.DeleteContactFlowModuleInput, opts ...func(*connect.Options)) (*connect.DeleteContactFlowModuleOutput, error)
	// https://docs.aws.amazon.com/connect/latest/APIReference/API_CreateContactFlowModuleVersion.html
	CreateContactFlowModuleVersion(ctx context.Context, in *connect.CreateContactFlowModuleVersionInput, opts ...func(*connect.Options)) (*connect.CreateContactFlowModuleVersionOutput, error)
	// https://docs.aws.amazon.com/connect/latest/APIReference/API_ListContactFlowModuleAliases.html
	ListContactFlowModuleAliases(ctx context.Context, in *connect.ListContactFlowModuleAliasesInput, opts ...func(*connect.Options)) (*connect.ListContactFlowModuleAliasesOutput, error)
	// https://docs.aws.amazon.com/connect/latest/APIReference/API_ListContactFlowModuleVersions.html
	ListContactFlowModuleVersions(ctx context.Context, in *connect.ListContactFlowModuleVersionsInput, opts ...func(*connect.Options)) (*connect.ListContactFlowModuleVersionsOutput, error)
	// https://docs.aws.amazon.com/connect/latest/APIReference/API_DeleteContactFlowModuleVersion.html
	DeleteContactFlowModuleVersion(ctx context.Context, in *connect.DeleteContactFlowModuleVersionInput, opts ...func(*connect.Options)) (*connect.DeleteContactFlowModuleVersionOutput, error)
	// https://docs.aws.amazon.com/connect/latest/APIReference/API_CreateContactFlowModuleAlias.html
	CreateContactFlowModuleAlias(ctx context.Context, in *connect.CreateContactFlowModuleAliasInput, opts ...func(*connect.Options)) (*connect.CreateContactFlowModuleAliasOutput, error)
	// https://docs.aws.amazon.com/connect/latest/APIReference/API_DescribeContactFlowModuleAlias.html
	DescribeContactFlowModuleAlias(ctx context.Context, in *connect.DescribeContactFlowModuleAliasInput, opts ...func(*connect.Options)) (*connect.DescribeContactFlowModuleAliasOutput, error)
	// https://docs.aws.amazon.com/connect/latest/APIReference/API_UpdateContactFlowModuleAlias.html
	UpdateContactFlowModuleAlias(ctx context.Context, in *connect.UpdateContactFlowModuleAliasInput, opts ...func(*connect.Options)) (*connect.UpdateContactFlowModuleAliasOutput, error)
	// https://docs.aws.amazon.com/connect/latest/APIReference/API_DeleteContactFlowModuleAlias.html
	DeleteContactFlowModuleAlias(ctx context.Context, in *connect.DeleteContactFlowModuleAliasInput, opts ...func(*connect.Options)) (*connect.DeleteContactFlowModuleAliasOutput, error)
	// https://docs.aws.amazon.com/connect/latest/APIReference/API_ListContactFlows.html
	ListContactFlows(ctx context.Context, in *connect.ListContactFlowsInput, opts ...func(*connect.Options)) (*connect.ListContactFlowsOutput, error)
	// https://docs.aws.amazon.com/connect/latest/APIReference/API_ListContactFlowModules.html
	ListContactFlowModules(ctx context.Context, in *connect.ListContactFlowModulesInput, opts ...func(*connect.Options)) (*connect.ListContactFlowModulesOutput, error)
	// https://docs.aws.amazon.com/connect/latest/APIReference/API_ListQueues.html
	ListQueues(ctx context.Context, in *connect.ListQueuesInput, opts ...func(*connect.Options)) (*connect.ListQueuesOutput, error)
	// https://docs.aws.amazon.com/connect/latest/APIReference/API_ListHoursOfOperations.html
	ListHoursOfOperations(ctx context.Context, in *connect.ListHoursOfOperationsInput, opts ...func(*connect.Options)) (*connect.ListHoursOfOperationsOutput, error)
	// https://docs.aws.amazon.com/connect/latest/APIReference/API_ListPrompts.html
	ListPrompts(ctx context.Context, in *connect.ListPromptsInput, opts ...func(*connect.Options)) (*connect.ListPromptsOutput, error)
	// https://docs.aws.amazon.com/connect/latest/APIReference/API_ListLambdaFunctions.html
	ListLambdaFunctions(ctx context.Context, in *connect.ListLambdaFunctionsInput, opts ...func(*connect.Options)) (*connect.ListLambdaFunctionsOutput, error)
	// https://docs.aws.amazon.com/connect/latest/APIReference/API_ListBots.html
	ListBots(ctx context.Context, in *connect.ListBotsInput, opts ...func(*connect.Options)) (*connect.ListBotsOutput, error)
	// https://docs.aws.amazon.com/connect/latest/APIReference/API_ListViews.html
	ListViews(ctx context.Context, in *connect.ListViewsInput, opts ...func(*connect.Options)) (*connect.ListViewsOutput, error)
	// https://docs.aws.amazon.com/connect/latest/APIReference/API_TagResource.html
	TagResource(ctx context.Context, in *connect.TagResourceInput, opts ...func(*connect.Options)) (*connect.TagResourceOutput, error)
	// https://docs.aws.amazon.com/connect/latest/APIReference/API_UntagResource.html
	UntagResource(ctx context.Context, in *connect.UntagResourceInput, opts ...func(*connect.Options)) (*connect.UntagResourceOutput, error)
}

// IsNotFound reports whether err is Connect's ResourceNotFoundException,
// which a resource reads as "gone" rather than as a failure.
func IsNotFound(err error) bool {
	var nf *types.ResourceNotFoundException
	return errors.As(err, &nf)
}

// Detail is err's text with the problem list Connect attaches to
// InvalidContactFlowException and InvalidContactFlowModuleException. The
// exception's own message is often empty (observed 2026-09-29: "InvalidContact
// FlowException: " and nothing else), and the problems are what say which
// action is wrong and why.
// https://docs.aws.amazon.com/connect/latest/APIReference/API_CreateContactFlow.html
func Detail(err error) string {
	var flow *types.InvalidContactFlowException
	var module *types.InvalidContactFlowModuleException
	var problems []types.ProblemDetail
	switch {
	case errors.As(err, &flow):
		problems = flow.Problems
	case errors.As(err, &module):
		problems = module.Problems
	}
	if len(problems) == 0 {
		return err.Error()
	}
	var b strings.Builder
	b.WriteString(err.Error())
	b.WriteString("\n\nAmazon Connect refused the content:")
	for _, p := range problems {
		b.WriteString("\n  - ")
		b.WriteString(aws.ToString(p.Message))
	}
	return b.String()
}

var _ API = (*connect.Client)(nil)
