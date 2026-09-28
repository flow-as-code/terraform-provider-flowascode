// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

// Package connectapi is the seam between the provider and Amazon Connect:
// exactly the operations the resources call, each with its API reference, so
// the resources can be tested against a fake that replays recorded inventories.
package connectapi

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/connect"
)

// Client is what a configured provider hands its resources and data sources.
type Client struct {
	// Connect is the SDK client, behind the API interface.
	Connect API
	// Region the client was configured for, used in diagnostics.
	Region string
}

// API is the subset of the Connect SDK client the provider uses. It grows
// with the resources (task B04f); every method carries its API reference URL
// where it is added.
type API interface {
	// https://docs.aws.amazon.com/connect/latest/APIReference/API_DescribeInstance.html
	DescribeInstance(ctx context.Context, in *connect.DescribeInstanceInput, opts ...func(*connect.Options)) (*connect.DescribeInstanceOutput, error)
}
