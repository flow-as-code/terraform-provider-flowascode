// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

// Package connectapi is the seam between the provider and Amazon Connect:
// exactly the operations the resources call, each with its API reference, so
// the resources can be tested against a fake.
package connectapi

import (
	"context"
	"errors"

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

var _ API = (*connect.Client)(nil)
