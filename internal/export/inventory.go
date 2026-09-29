// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package export

import (
	"context"
	"strings"
)

// ResourceSummary is export.ts's ResourceSummary: one named Connect resource,
// as the List* operations return it. The JSON tags are the TypeScript's field
// names, which is the shape of conformance/export/<case>/inventory.json.
type ResourceSummary struct {
	Arn  string  `json:"arn"`
	ID   *string `json:"id,omitempty"`
	Name string  `json:"name"`
}

// ContactFlowSummary is export.ts's ContactFlowSummary. ListContactFlows names
// the type field ContactFlowType, not Type.
type ContactFlowSummary struct {
	ResourceSummary
	ContactFlowType   *string `json:"contactFlowType,omitempty"`
	ContactFlowState  *string `json:"contactFlowState,omitempty"`
	ContactFlowStatus *string `json:"contactFlowStatus,omitempty"`
}

// ContactFlowModuleSummary is export.ts's ContactFlowModuleSummary.
type ContactFlowModuleSummary struct {
	ResourceSummary
	State *string `json:"state,omitempty"`
}

// LexBotSummary is export.ts's LexBotSummary. ListBots returns V1 and V2 bots
// in one list and they are not symmetric: a V2 bot yields an alias ARN and no
// name, a V1 bot a name and region and no ARN.
// https://docs.aws.amazon.com/connect/latest/APIReference/API_ListBots.html
type LexBotSummary struct {
	// LexVersion is "V1" or "V2".
	LexVersion string  `json:"lexVersion"`
	Name       *string `json:"name,omitempty"`
	LexRegion  *string `json:"lexRegion,omitempty"`
	AliasArn   *string `json:"aliasArn,omitempty"`
}

// ViewSummary is export.ts's ViewSummary: one view as ListViews returns it,
// AWS_MANAGED views beside the instance's own.
// https://docs.aws.amazon.com/connect/latest/APIReference/API_ListViews.html
type ViewSummary struct {
	Arn  string `json:"arn"`
	ID   string `json:"id"`
	Name string `json:"name"`
	// Type is "CUSTOMER_MANAGED" or "AWS_MANAGED".
	Type   string  `json:"type"`
	Status *string `json:"status,omitempty"`
}

// InstanceInventory is export.ts's InstanceInventory.
type InstanceInventory struct {
	ContactFlows       []ContactFlowSummary       `json:"contactFlows"`
	ContactFlowModules []ContactFlowModuleSummary `json:"contactFlowModules"`
	Queues             []ResourceSummary          `json:"queues"`
	HoursOfOperations  []ResourceSummary          `json:"hoursOfOperations"`
	Prompts            []ResourceSummary          `json:"prompts"`
	// LambdaFunctions are bare Lambda function ARNs, which is all
	// ListLambdaFunctions returns.
	LambdaFunctions []string        `json:"lambdaFunctions"`
	LexBots         []LexBotSummary `json:"lexBots"`
	Views           []ViewSummary   `json:"views"`
	// ModuleAliases are each module's aliases, when the client lists them:
	// a flow invokes an alias as <module ARN>:<alias id>, and the reverse map
	// needs the id to read that back as module:<name>@<alias name>.
	ModuleAliases []ModuleAliasSummary `json:"moduleAliases,omitempty"`
}

// ModuleAliasSummary is export.ts's ModuleAliasSummary.
type ModuleAliasSummary struct {
	ModuleArn string `json:"moduleArn"`
	AliasID   string `json:"aliasId"`
	Name      string `json:"name"`
}

// ModuleAlias is one alias ListContactFlowModuleAliases returns.
type ModuleAlias struct {
	AliasID string
	Name    string
}

// ModuleAliasLister is export.ts's optional
// ConnectInventoryClient.listContactFlowModuleAliases: a client that lists a
// module's aliases implements it, and one that does not still collects.
// https://docs.aws.amazon.com/connect/latest/APIReference/API_ListContactFlowModuleAliases.html
type ModuleAliasLister interface {
	ListContactFlowModuleAliases(ctx context.Context, contactFlowModuleID string) ([]ModuleAlias, error)
}

// DescribedContactFlow is export.ts's DescribedContactFlow: the operation that
// carries the Flow language. DescribeContactFlow names the type fields
// Type/State/Status, not ContactFlow*.
type DescribedContactFlow struct {
	Arn    string
	ID     string
	Name   string
	Type   *string
	State  *string
	Status *string
	// Version is the flow version, as a string.
	Version *string
	// Description is carried into FlowDoc.description.
	Description *string
	// Content is the Flow language JSON, as a string.
	Content       string
	ContentSha256 *string
}

// DescribedContactFlowModule is export.ts's DescribedContactFlowModule. Two
// fields a ContactFlow has no analogue for; FlowDoc models neither, so export
// warns rather than drops them silently.
// https://docs.aws.amazon.com/connect/latest/APIReference/API_DescribeContactFlowModule.html
type DescribedContactFlowModule struct {
	DescribedContactFlow
	Settings                  *string
	ExternalInvocationEnabled *bool
}

// ConnectInventoryClient is export.ts's ConnectInventoryClient: the narrow
// seam every export path runs through. Implementations page and rate limit;
// this interface deals in complete lists.
//
// A describe of a flow that has never been published must fail with an error
// whose ErrorCode() is "ContactFlowNotPublishedException", as the AWS SDK's
// smithy.APIError does, for the $SAVED fallback to engage.
type ConnectInventoryClient interface {
	// ListContactFlows filters by ContactFlowTypes; nil means every type
	// (TypeScript: undefined).
	ListContactFlows(ctx context.Context, contactFlowTypes []string) ([]ContactFlowSummary, error)
	DescribeContactFlow(ctx context.Context, contactFlowID string) (DescribedContactFlow, error)
	ListContactFlowModules(ctx context.Context) ([]ContactFlowModuleSummary, error)
	DescribeContactFlowModule(ctx context.Context, contactFlowModuleID string) (DescribedContactFlowModule, error)
	ListQueues(ctx context.Context) ([]ResourceSummary, error)
	ListHoursOfOperations(ctx context.Context) ([]ResourceSummary, error)
	ListPrompts(ctx context.Context) ([]ResourceSummary, error)
	ListLambdaFunctions(ctx context.Context) ([]string, error)
	ListBots(ctx context.Context) ([]LexBotSummary, error)
	ListViews(ctx context.Context) ([]ViewSummary, error)
}

// CollectInventoryOptions is export.ts's CollectInventoryOptions.
type CollectInventoryOptions struct {
	// FlowTypes is the ContactFlowTypes filter for ListContactFlows; nil means
	// every type.
	// https://docs.aws.amazon.com/connect/latest/APIReference/API_ListContactFlows.html
	FlowTypes []string
	// ExcludeModules skips ListContactFlowModules and
	// DescribeContactFlowModule (TypeScript: includeModules === false).
	ExcludeModules bool
}

// CollectInventory is export.ts's collectInventory: the list operations
// assembled into one inventory. The TypeScript runs them concurrently with
// Promise.all; this runs them in the same order one after another, so on
// failure it returns the first error in that order rather than the first to
// happen.
func CollectInventory(ctx context.Context, client ConnectInventoryClient, options CollectInventoryOptions) (InstanceInventory, error) {
	var inv InstanceInventory
	var err error
	if inv.ContactFlows, err = client.ListContactFlows(ctx, options.FlowTypes); err != nil {
		return InstanceInventory{}, err
	}
	if options.ExcludeModules {
		inv.ContactFlowModules = []ContactFlowModuleSummary{}
	} else if inv.ContactFlowModules, err = client.ListContactFlowModules(ctx); err != nil {
		return InstanceInventory{}, err
	}
	if inv.Queues, err = client.ListQueues(ctx); err != nil {
		return InstanceInventory{}, err
	}
	if inv.HoursOfOperations, err = client.ListHoursOfOperations(ctx); err != nil {
		return InstanceInventory{}, err
	}
	if inv.Prompts, err = client.ListPrompts(ctx); err != nil {
		return InstanceInventory{}, err
	}
	if inv.LambdaFunctions, err = client.ListLambdaFunctions(ctx); err != nil {
		return InstanceInventory{}, err
	}
	if inv.LexBots, err = client.ListBots(ctx); err != nil {
		return InstanceInventory{}, err
	}
	if inv.Views, err = client.ListViews(ctx); err != nil {
		return InstanceInventory{}, err
	}
	if lister, ok := client.(ModuleAliasLister); ok {
		for _, m := range inv.ContactFlowModules {
			id := m.Arn[strings.LastIndex(m.Arn, "/")+1:]
			if m.ID != nil {
				id = *m.ID
			}
			aliases, err := lister.ListContactFlowModuleAliases(ctx, id)
			if err != nil {
				return InstanceInventory{}, err
			}
			for _, a := range aliases {
				inv.ModuleAliases = append(inv.ModuleAliases, ModuleAliasSummary{ModuleArn: m.Arn, AliasID: a.AliasID, Name: a.Name})
			}
		}
	}
	return inv, nil
}
