// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package connectapi

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/connect"
	"github.com/aws/aws-sdk-go-v2/service/connect/types"
)

// The List* operations the export inventory adapter reads. Flows and modules
// are the ones the fake holds; the other resources are lists a test sets.
// Every list pages the way Connect does: at most MaxResults items, an opaque
// NextToken while more remain, and a MaxResults outside the operation's
// documented range refused.

// placeholderAccount is AWS's documentation account id. The fake refuses any
// other account in an ARN it is handed, so a real one cannot leak into a
// fixture. "aws" is the account an AWS-managed view's ARN carries.
const placeholderAccount = "111122223333"

func mustBePlaceholder(arn string) {
	if arn == "" {
		return
	}
	parts := strings.SplitN(arn, ":", 6)
	if len(parts) < 6 || (parts[4] != "" && parts[4] != placeholderAccount && parts[4] != "aws") {
		panic(fmt.Sprintf("connectapi.Fake: %q is not an ARN in the documentation account %s", arn, placeholderAccount))
	}
}

func invalidParameter(format string, args ...any) error {
	return &types.InvalidParameterException{Message: aws.String(fmt.Sprintf(format, args...))}
}

// fakePage returns one page of items. The page is maxResults long, or limit
// (the operation's maximum) when maxResults is nil; the token is the offset
// of the next page, which callers must treat as opaque.
func fakePage[T any](op string, items []T, maxResults *int32, limit int32, nextToken *string) ([]T, *string, error) {
	size := limit
	if maxResults != nil {
		size = *maxResults
		if size < 1 || size > limit {
			return nil, nil, invalidParameter("%s: MaxResults %d is outside 1 to %d", op, size, limit)
		}
	}
	start := 0
	if nextToken != nil {
		n, err := strconv.Atoi(strings.TrimPrefix(*nextToken, "page-"))
		if err != nil || !strings.HasPrefix(*nextToken, "page-") || n < 0 || n > len(items) {
			return nil, nil, invalidParameter("%s: NextToken %q is not one this operation issued", op, *nextToken)
		}
		start = n
	}
	end := min(start+int(size), len(items))
	var next *string
	if end < len(items) {
		next = aws.String("page-" + strconv.Itoa(end))
	}
	return slices.Clone(items[start:end]), next, nil
}

// PutContactFlow stores a flow as given, id and ARN included, for a test
// that seeds an instance rather than creating flows through the provider.
func (f *Fake) PutContactFlow(flow types.ContactFlow) {
	mustBePlaceholder(aws.ToString(flow.Arn))
	f.mu.Lock()
	defer f.mu.Unlock()
	f.flows[aws.ToString(flow.Id)] = &flow
}

// MarkNeverPublished makes DescribeContactFlow on the bare id fail with
// ContactFlowNotPublishedException, so the flow reads only through its
// `:$SAVED` alias.
func (f *Fake) MarkNeverPublished(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.neverPublished == nil {
		f.neverPublished = map[string]bool{}
	}
	f.neverPublished[id] = true
}

// PutContactFlowModule stores a module as given, id and ARN included.
func (f *Fake) PutContactFlowModule(module types.ContactFlowModule) {
	mustBePlaceholder(aws.ToString(module.Arn))
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.modules == nil {
		f.modules = map[string]*fakeModule{}
	}
	f.modules[aws.ToString(module.Id)] = &fakeModule{
		module: module, versions: map[int64]string{}, aliases: map[string]*types.ContactFlowModuleAliasInfo{},
	}
}

// SetQueues sets what ListQueues returns, agent queues included; ListQueues
// applies its QueueTypes filter.
func (f *Fake) SetQueues(queues []types.QueueSummary) {
	for _, q := range queues {
		mustBePlaceholder(aws.ToString(q.Arn))
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queues = queues
}

// SetHoursOfOperations sets what ListHoursOfOperations returns.
func (f *Fake) SetHoursOfOperations(hours []types.HoursOfOperationSummary) {
	for _, h := range hours {
		mustBePlaceholder(aws.ToString(h.Arn))
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hours = hours
}

// SetPrompts sets what ListPrompts returns.
func (f *Fake) SetPrompts(prompts []types.PromptSummary) {
	for _, p := range prompts {
		mustBePlaceholder(aws.ToString(p.Arn))
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prompts = prompts
}

// SetLambdaFunctions sets the function ARNs ListLambdaFunctions returns.
func (f *Fake) SetLambdaFunctions(arns []string) {
	for _, a := range arns {
		mustBePlaceholder(a)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lambdas = arns
}

// SetBots sets the bots ListBots returns. A config with LexBot set is a V1
// bot and one with LexV2Bot set a V2 bot; ListBots returns only the version
// asked for, as Connect does.
func (f *Fake) SetBots(bots []types.LexBotConfig) {
	for _, b := range bots {
		if b.LexV2Bot != nil {
			mustBePlaceholder(aws.ToString(b.LexV2Bot.AliasArn))
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bots = bots
}

// https://docs.aws.amazon.com/connect/latest/APIReference/API_ListContactFlows.html
func (f *Fake) ListContactFlows(_ context.Context, in *connect.ListContactFlowsInput, _ ...func(*connect.Options)) (*connect.ListContactFlowsOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("ListContactFlows")
	ids := make([]string, 0, len(f.flows))
	for id := range f.flows {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var all []types.ContactFlowSummary
	for _, id := range ids {
		c := f.flows[id]
		if len(in.ContactFlowTypes) > 0 && !slices.Contains(in.ContactFlowTypes, c.Type) {
			continue
		}
		all = append(all, types.ContactFlowSummary{
			Arn: c.Arn, Id: c.Id, Name: c.Name,
			ContactFlowType: c.Type, ContactFlowState: c.State, ContactFlowStatus: c.Status,
		})
	}
	page, next, err := fakePage("ListContactFlows", all, in.MaxResults, 1000, in.NextToken)
	if err != nil {
		return nil, err
	}
	return &connect.ListContactFlowsOutput{ContactFlowSummaryList: page, NextToken: next}, nil
}

// https://docs.aws.amazon.com/connect/latest/APIReference/API_ListContactFlowModules.html
func (f *Fake) ListContactFlowModules(_ context.Context, in *connect.ListContactFlowModulesInput, _ ...func(*connect.Options)) (*connect.ListContactFlowModulesOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("ListContactFlowModules")
	ids := make([]string, 0, len(f.modules))
	for id := range f.modules {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var all []types.ContactFlowModuleSummary
	for _, id := range ids {
		m := f.modules[id].module
		if in.ContactFlowModuleState != "" && m.State != in.ContactFlowModuleState {
			continue
		}
		all = append(all, types.ContactFlowModuleSummary{Arn: m.Arn, Id: m.Id, Name: m.Name, State: m.State})
	}
	page, next, err := fakePage("ListContactFlowModules", all, in.MaxResults, 1000, in.NextToken)
	if err != nil {
		return nil, err
	}
	return &connect.ListContactFlowModulesOutput{ContactFlowModulesSummaryList: page, NextToken: next}, nil
}

// https://docs.aws.amazon.com/connect/latest/APIReference/API_ListQueues.html
func (f *Fake) ListQueues(_ context.Context, in *connect.ListQueuesInput, _ ...func(*connect.Options)) (*connect.ListQueuesOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("ListQueues")
	var all []types.QueueSummary
	for _, q := range f.queues {
		if len(in.QueueTypes) == 0 || slices.Contains(in.QueueTypes, q.QueueType) {
			all = append(all, q)
		}
	}
	page, next, err := fakePage("ListQueues", all, in.MaxResults, 1000, in.NextToken)
	if err != nil {
		return nil, err
	}
	return &connect.ListQueuesOutput{QueueSummaryList: page, NextToken: next}, nil
}

// https://docs.aws.amazon.com/connect/latest/APIReference/API_ListHoursOfOperations.html
func (f *Fake) ListHoursOfOperations(_ context.Context, in *connect.ListHoursOfOperationsInput, _ ...func(*connect.Options)) (*connect.ListHoursOfOperationsOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("ListHoursOfOperations")
	page, next, err := fakePage("ListHoursOfOperations", f.hours, in.MaxResults, 1000, in.NextToken)
	if err != nil {
		return nil, err
	}
	return &connect.ListHoursOfOperationsOutput{HoursOfOperationSummaryList: page, NextToken: next}, nil
}

// https://docs.aws.amazon.com/connect/latest/APIReference/API_ListPrompts.html
func (f *Fake) ListPrompts(_ context.Context, in *connect.ListPromptsInput, _ ...func(*connect.Options)) (*connect.ListPromptsOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("ListPrompts")
	page, next, err := fakePage("ListPrompts", f.prompts, in.MaxResults, 1000, in.NextToken)
	if err != nil {
		return nil, err
	}
	return &connect.ListPromptsOutput{PromptSummaryList: page, NextToken: next}, nil
}

// MaxResults runs 1 to 25 here.
// https://docs.aws.amazon.com/connect/latest/APIReference/API_ListLambdaFunctions.html
func (f *Fake) ListLambdaFunctions(_ context.Context, in *connect.ListLambdaFunctionsInput, _ ...func(*connect.Options)) (*connect.ListLambdaFunctionsOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("ListLambdaFunctions")
	page, next, err := fakePage("ListLambdaFunctions", f.lambdas, in.MaxResults, 25, in.NextToken)
	if err != nil {
		return nil, err
	}
	return &connect.ListLambdaFunctionsOutput{LambdaFunctions: page, NextToken: next}, nil
}

// LexVersion is required, and MaxResults runs 1 to 25.
// https://docs.aws.amazon.com/connect/latest/APIReference/API_ListBots.html
func (f *Fake) ListBots(_ context.Context, in *connect.ListBotsInput, _ ...func(*connect.Options)) (*connect.ListBotsOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("ListBots")
	var all []types.LexBotConfig
	switch in.LexVersion {
	case types.LexVersionV1:
		for _, b := range f.bots {
			if b.LexBot != nil {
				all = append(all, b)
			}
		}
	case types.LexVersionV2:
		for _, b := range f.bots {
			if b.LexV2Bot != nil {
				all = append(all, b)
			}
		}
	default:
		return nil, invalidParameter("ListBots: LexVersion %q is not V1 or V2", in.LexVersion)
	}
	page, next, err := fakePage("ListBots", all, in.MaxResults, 25, in.NextToken)
	if err != nil {
		return nil, err
	}
	return &connect.ListBotsOutput{LexBots: page, NextToken: next}, nil
}
