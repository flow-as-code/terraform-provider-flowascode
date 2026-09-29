// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package connectapi

// The export inventory adapter: export.ConnectInventoryClient over the AWS SDK
// for Go v2 Connect client. It ports createConnectInventoryClient from
// packages/core/src/export.ts in the flow-as-code repository: pagination, the
// 2 rps throttle budget, and the response-shape differences between the list
// and describe operations all live here, so package export never sees the SDK.
//
// Operations used, one per interface method:
//   ListContactFlows           https://docs.aws.amazon.com/connect/latest/APIReference/API_ListContactFlows.html
//   DescribeContactFlow        https://docs.aws.amazon.com/connect/latest/APIReference/API_DescribeContactFlow.html
//   ListContactFlowModules     https://docs.aws.amazon.com/connect/latest/APIReference/API_ListContactFlowModules.html
//   DescribeContactFlowModule  https://docs.aws.amazon.com/connect/latest/APIReference/API_DescribeContactFlowModule.html
//   ListQueues                 https://docs.aws.amazon.com/connect/latest/APIReference/API_ListQueues.html
//   ListHoursOfOperations      https://docs.aws.amazon.com/connect/latest/APIReference/API_ListHoursOfOperations.html
//   ListPrompts                https://docs.aws.amazon.com/connect/latest/APIReference/API_ListPrompts.html
//   ListLambdaFunctions        https://docs.aws.amazon.com/connect/latest/APIReference/API_ListLambdaFunctions.html
//   ListBots                   https://docs.aws.amazon.com/connect/latest/APIReference/API_ListBots.html
//   ListViews                  https://docs.aws.amazon.com/connect/latest/APIReference/API_ListViews.html

import (
	"context"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/connect"
	"github.com/aws/aws-sdk-go-v2/service/connect/types"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/export"
)

const (
	// defaultMaxResults is the page size for the Connect resource lists, whose
	// maximum is 1000 (export.ts: options.maxResults ?? 1000).
	defaultMaxResults int32 = 1000
	// lambdaAndBotMaxResults is ListLambdaFunctions' and ListBots' maximum,
	// 25 rather than 1000.
	lambdaAndBotMaxResults int32 = 25
	// viewsMaxResults is ListViews' maximum, smaller than the other lists'.
	viewsMaxResults int32 = 100
	// aliasesMaxResults is ListContactFlowModuleAliases' maximum, also 100:
	// the sandbox refused 1000 on 2026-09-29 ("MaxResults must have a length
	// less than or equal to 100").
	aliasesMaxResults int32 = 100
	// defaultRequestsPerSecond is Amazon Connect's default RateLimit for every
	// operation outside the exceptions table, per account per Region and
	// shared across users and instances. None of the operations here is in
	// that table.
	// https://docs.aws.amazon.com/connect/latest/adminguide/amazon-connect-service-limits.html#connect-api-quotas
	defaultRequestsPerSecond = 2.0
)

// InventoryOptions is export.ts's ConnectClientOptions beyond the client and
// the instance id, with RateLimiterOptions from aws.ts folded in.
type InventoryOptions struct {
	// MaxResults is the page size for the Connect resource lists. Zero means
	// 1000, their maximum. ListLambdaFunctions and ListBots always ask for 25,
	// and ListViews for at most 100, their own maxima.
	MaxResults int32
	// RequestsPerSecond spaces every request, list and describe alike. Nil
	// means 2; zero or less turns the limiter off (TypeScript: rps > 0).
	RequestsPerSecond *float64
	// Sleep waits d or until ctx is done. Injected for tests; nil sleeps on a
	// timer.
	Sleep func(ctx context.Context, d time.Duration) error
	// Now is the clock. Injected for tests; nil is time.Now.
	Now func() time.Time
}

// NewInventory returns an export.ConnectInventoryClient over api for one
// instance, with the defaults export.ts uses: 1000-item pages and 2 requests
// per second. instanceID is an instance id or instance ARN; every operation
// accepts both.
func NewInventory(api API, instanceID string) export.ConnectInventoryClient {
	return NewInventoryWithOptions(api, instanceID, InventoryOptions{})
}

// NewInventoryWithOptions is NewInventory with the page size and the limiter
// set explicitly.
func NewInventoryWithOptions(api API, instanceID string, options InventoryOptions) export.ConnectInventoryClient {
	maxResults := options.MaxResults
	if maxResults == 0 {
		maxResults = defaultMaxResults
	}
	rps := defaultRequestsPerSecond
	if options.RequestsPerSecond != nil {
		rps = *options.RequestsPerSecond
	}
	return &inventory{
		api:        api,
		instanceID: instanceID,
		maxResults: maxResults,
		limiter:    newRateLimiter(rps, options.Sleep, options.Now),
	}
}

type inventory struct {
	api        API
	instanceID string
	maxResults int32
	limiter    *rateLimiter
}

var _ export.ConnectInventoryClient = (*inventory)(nil)

// rateLimiter is aws.ts's createRateLimiter: calls are spaced at least
// ceil(1000/rps) milliseconds apart, the first one not at all. The mutex is
// held across the wait, so concurrent callers queue on one budget rather than
// each pacing itself and jointly exceeding the account limit.
type rateLimiter struct {
	mu        sync.Mutex
	interval  time.Duration
	nextAllow time.Time
	sleep     func(context.Context, time.Duration) error
	now       func() time.Time
}

func newRateLimiter(rps float64, sleep func(context.Context, time.Duration) error, now func() time.Time) *rateLimiter {
	var interval time.Duration
	if rps > 0 {
		interval = time.Duration(math.Ceil(1000/rps)) * time.Millisecond
	}
	if sleep == nil {
		sleep = sleepContext
	}
	if now == nil {
		now = time.Now
	}
	return &rateLimiter{interval: interval, sleep: sleep, now: now}
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// wait blocks until the next request may go. A wait cut short by ctx leaves
// the budget where it was, as a rejected sleep does in aws.ts.
func (l *rateLimiter) wait(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if d := l.nextAllow.Sub(l.now()); d > 0 {
		if err := l.sleep(ctx, d); err != nil {
			return err
		}
	}
	l.nextAllow = l.now().Add(l.interval)
	return nil
}

// paginate is export.ts's paginate: every list operation here pages the same
// way, an opaque NextToken, and an empty token ends the list as a missing one
// does. Each page is one request through the limiter. The result is never nil,
// so an empty list serializes as [] the way the TypeScript's does.
func paginate[T any](ctx context.Context, l *rateLimiter, page func(ctx context.Context, nextToken *string) ([]T, *string, error)) ([]T, error) {
	out := []T{}
	var nextToken *string
	for {
		if err := l.wait(ctx); err != nil {
			return nil, err
		}
		items, next, err := page(ctx, nextToken)
		if err != nil {
			return nil, err
		}
		out = append(out, items...)
		if next == nil || *next == "" {
			return out, nil
		}
		nextToken = next
	}
}

// summary is export.ts's summary: arn and name default to "", id is kept
// only when present.
func summary(arn, id, name *string) export.ResourceSummary {
	return export.ResourceSummary{Arn: aws.ToString(arn), ID: id, Name: aws.ToString(name)}
}

// enum is an SDK enum as the TypeScript sees it: the empty string is a field
// the response left out.
func enum[E ~string](e E) *string {
	if e == "" {
		return nil
	}
	s := string(e)
	return &s
}

// described is export.ts's described, over the fields ContactFlow and
// ContactFlowModule share.
func described(arn, id, name *string, typ, state, status string, version *int64, description, content, sha *string) export.DescribedContactFlow {
	d := export.DescribedContactFlow{
		Arn:           aws.ToString(arn),
		ID:            aws.ToString(id),
		Name:          aws.ToString(name),
		Type:          enum(typ),
		State:         enum(state),
		Status:        enum(status),
		Description:   description,
		Content:       aws.ToString(content),
		ContentSha256: sha,
	}
	if version != nil {
		v := strconv.FormatInt(*version, 10)
		d.Version = &v
	}
	return d
}

// https://docs.aws.amazon.com/connect/latest/APIReference/API_ListContactFlows.html
func (c *inventory) ListContactFlows(ctx context.Context, contactFlowTypes []string) ([]export.ContactFlowSummary, error) {
	var filter []types.ContactFlowType
	if contactFlowTypes != nil {
		filter = make([]types.ContactFlowType, 0, len(contactFlowTypes))
		for _, t := range contactFlowTypes {
			filter = append(filter, types.ContactFlowType(t))
		}
	}
	return paginate(ctx, c.limiter, func(ctx context.Context, nextToken *string) ([]export.ContactFlowSummary, *string, error) {
		out, err := c.api.ListContactFlows(ctx, &connect.ListContactFlowsInput{
			InstanceId:       aws.String(c.instanceID),
			MaxResults:       aws.Int32(c.maxResults),
			NextToken:        nextToken,
			ContactFlowTypes: filter,
		})
		if err != nil {
			return nil, nil, err
		}
		items := make([]export.ContactFlowSummary, 0, len(out.ContactFlowSummaryList))
		for _, s := range out.ContactFlowSummaryList {
			items = append(items, export.ContactFlowSummary{
				ResourceSummary:   summary(s.Arn, s.Id, s.Name),
				ContactFlowType:   enum(s.ContactFlowType),
				ContactFlowState:  enum(s.ContactFlowState),
				ContactFlowStatus: enum(s.ContactFlowStatus),
			})
		}
		return items, out.NextToken, nil
	})
}

// DescribeContactFlow passes contactFlowID through untouched, so the `:$SAVED`
// alias export.ExportInstance appends after ContactFlowNotPublishedException
// reaches Connect, and the SDK's error comes back unwrapped for it to see.
// https://docs.aws.amazon.com/connect/latest/APIReference/API_DescribeContactFlow.html
func (c *inventory) DescribeContactFlow(ctx context.Context, contactFlowID string) (export.DescribedContactFlow, error) {
	if err := c.limiter.wait(ctx); err != nil {
		return export.DescribedContactFlow{}, err
	}
	out, err := c.api.DescribeContactFlow(ctx, &connect.DescribeContactFlowInput{
		InstanceId:    aws.String(c.instanceID),
		ContactFlowId: aws.String(contactFlowID),
	})
	if err != nil {
		return export.DescribedContactFlow{}, err
	}
	f := out.ContactFlow
	if f == nil {
		f = &types.ContactFlow{}
	}
	return described(f.Arn, f.Id, f.Name, string(f.Type), string(f.State), string(f.Status),
		f.Version, f.Description, f.Content, f.FlowContentSha256), nil
}

// The list field is ContactFlowModulesSummaryList, plural Modules.
// https://docs.aws.amazon.com/connect/latest/APIReference/API_ListContactFlowModules.html
func (c *inventory) ListContactFlowModules(ctx context.Context) ([]export.ContactFlowModuleSummary, error) {
	return paginate(ctx, c.limiter, func(ctx context.Context, nextToken *string) ([]export.ContactFlowModuleSummary, *string, error) {
		out, err := c.api.ListContactFlowModules(ctx, &connect.ListContactFlowModulesInput{
			InstanceId: aws.String(c.instanceID),
			MaxResults: aws.Int32(c.maxResults),
			NextToken:  nextToken,
		})
		if err != nil {
			return nil, nil, err
		}
		items := make([]export.ContactFlowModuleSummary, 0, len(out.ContactFlowModulesSummaryList))
		for _, s := range out.ContactFlowModulesSummaryList {
			items = append(items, export.ContactFlowModuleSummary{
				ResourceSummary: summary(s.Arn, s.Id, s.Name),
				State:           enum(s.State),
			})
		}
		return items, out.NextToken, nil
	})
}

// Settings and ExternalInvocationConfiguration have no ContactFlow analogue;
// export warns on either.
// https://docs.aws.amazon.com/connect/latest/APIReference/API_DescribeContactFlowModule.html
func (c *inventory) DescribeContactFlowModule(ctx context.Context, contactFlowModuleID string) (export.DescribedContactFlowModule, error) {
	if err := c.limiter.wait(ctx); err != nil {
		return export.DescribedContactFlowModule{}, err
	}
	out, err := c.api.DescribeContactFlowModule(ctx, &connect.DescribeContactFlowModuleInput{
		InstanceId:          aws.String(c.instanceID),
		ContactFlowModuleId: aws.String(contactFlowModuleID),
	})
	if err != nil {
		return export.DescribedContactFlowModule{}, err
	}
	m := out.ContactFlowModule
	if m == nil {
		m = &types.ContactFlowModule{}
	}
	d := export.DescribedContactFlowModule{
		// A module has no type; ContactFlowModule carries no Type field.
		DescribedContactFlow: described(m.Arn, m.Id, m.Name, "", string(m.State), string(m.Status),
			m.Version, m.Description, m.Content, m.FlowModuleContentSha256),
		Settings: m.Settings,
	}
	if m.ExternalInvocationConfiguration != nil {
		enabled := m.ExternalInvocationConfiguration.Enabled
		d.ExternalInvocationEnabled = &enabled
	}
	return d, nil
}

// QueueTypes is deliberate: without it agent queues come back too, which are
// per-user and can truncate the page past 1000 agents.
// https://docs.aws.amazon.com/connect/latest/APIReference/API_ListQueues.html
func (c *inventory) ListQueues(ctx context.Context) ([]export.ResourceSummary, error) {
	return paginate(ctx, c.limiter, func(ctx context.Context, nextToken *string) ([]export.ResourceSummary, *string, error) {
		out, err := c.api.ListQueues(ctx, &connect.ListQueuesInput{
			InstanceId: aws.String(c.instanceID),
			QueueTypes: []types.QueueType{types.QueueTypeStandard},
			MaxResults: aws.Int32(c.maxResults),
			NextToken:  nextToken,
		})
		if err != nil {
			return nil, nil, err
		}
		items := make([]export.ResourceSummary, 0, len(out.QueueSummaryList))
		for _, s := range out.QueueSummaryList {
			items = append(items, summary(s.Arn, s.Id, s.Name))
		}
		return items, out.NextToken, nil
	})
}

// https://docs.aws.amazon.com/connect/latest/APIReference/API_ListHoursOfOperations.html
func (c *inventory) ListHoursOfOperations(ctx context.Context) ([]export.ResourceSummary, error) {
	return paginate(ctx, c.limiter, func(ctx context.Context, nextToken *string) ([]export.ResourceSummary, *string, error) {
		out, err := c.api.ListHoursOfOperations(ctx, &connect.ListHoursOfOperationsInput{
			InstanceId: aws.String(c.instanceID),
			MaxResults: aws.Int32(c.maxResults),
			NextToken:  nextToken,
		})
		if err != nil {
			return nil, nil, err
		}
		items := make([]export.ResourceSummary, 0, len(out.HoursOfOperationSummaryList))
		for _, s := range out.HoursOfOperationSummaryList {
			items = append(items, summary(s.Arn, s.Id, s.Name))
		}
		return items, out.NextToken, nil
	})
}

// https://docs.aws.amazon.com/connect/latest/APIReference/API_ListPrompts.html
func (c *inventory) ListPrompts(ctx context.Context) ([]export.ResourceSummary, error) {
	return paginate(ctx, c.limiter, func(ctx context.Context, nextToken *string) ([]export.ResourceSummary, *string, error) {
		out, err := c.api.ListPrompts(ctx, &connect.ListPromptsInput{
			InstanceId: aws.String(c.instanceID),
			MaxResults: aws.Int32(c.maxResults),
			NextToken:  nextToken,
		})
		if err != nil {
			return nil, nil, err
		}
		items := make([]export.ResourceSummary, 0, len(out.PromptSummaryList))
		for _, s := range out.PromptSummaryList {
			items = append(items, summary(s.Arn, s.Id, s.Name))
		}
		return items, out.NextToken, nil
	})
}

// MaxResults maxes out at 25 here, not 1000, and the response is a bare list
// of Lambda ARNs with no ids or names.
// https://docs.aws.amazon.com/connect/latest/APIReference/API_ListLambdaFunctions.html
func (c *inventory) ListLambdaFunctions(ctx context.Context) ([]string, error) {
	return paginate(ctx, c.limiter, func(ctx context.Context, nextToken *string) ([]string, *string, error) {
		out, err := c.api.ListLambdaFunctions(ctx, &connect.ListLambdaFunctionsInput{
			InstanceId: aws.String(c.instanceID),
			MaxResults: aws.Int32(lambdaAndBotMaxResults),
			NextToken:  nextToken,
		})
		if err != nil {
			return nil, nil, err
		}
		return out.LambdaFunctions, out.NextToken, nil
	})
}

// LexVersion is required, so a full inventory is two paginated passes, V1
// then V2, each tagged with the version it was listed under. A V1 bot yields
// a name and region, a V2 bot an alias ARN.
// https://docs.aws.amazon.com/connect/latest/APIReference/API_ListBots.html
func (c *inventory) ListBots(ctx context.Context) ([]export.LexBotSummary, error) {
	out := []export.LexBotSummary{}
	for _, lexVersion := range []types.LexVersion{types.LexVersionV1, types.LexVersionV2} {
		configs, err := paginate(ctx, c.limiter, func(ctx context.Context, nextToken *string) ([]types.LexBotConfig, *string, error) {
			page, err := c.api.ListBots(ctx, &connect.ListBotsInput{
				InstanceId: aws.String(c.instanceID),
				LexVersion: lexVersion,
				MaxResults: aws.Int32(lambdaAndBotMaxResults),
				NextToken:  nextToken,
			})
			if err != nil {
				return nil, nil, err
			}
			return page.LexBots, page.NextToken, nil
		})
		if err != nil {
			return nil, err
		}
		for _, config := range configs {
			bot := export.LexBotSummary{LexVersion: string(lexVersion)}
			if config.LexBot != nil {
				bot.Name = config.LexBot.Name
				bot.LexRegion = config.LexBot.LexRegion
			}
			if config.LexV2Bot != nil {
				bot.AliasArn = config.LexV2Bot.AliasArn
			}
			out = append(out, bot)
		}
	}
	return out, nil
}

// ListViews caps a page at 100 and, with no Type filter, returns AWS_MANAGED
// views beside the instance's CUSTOMER_MANAGED ones in one listing. That is
// what the reverse map needs: the stock flows show AWS-managed views. A view
// with no Type is taken as CUSTOMER_MANAGED.
// https://docs.aws.amazon.com/connect/latest/APIReference/API_ListViews.html
func (c *inventory) ListViews(ctx context.Context) ([]export.ViewSummary, error) {
	return paginate(ctx, c.limiter, func(ctx context.Context, nextToken *string) ([]export.ViewSummary, *string, error) {
		out, err := c.api.ListViews(ctx, &connect.ListViewsInput{
			InstanceId: aws.String(c.instanceID),
			MaxResults: aws.Int32(min(c.maxResults, viewsMaxResults)),
			NextToken:  nextToken,
		})
		if err != nil {
			return nil, nil, err
		}
		items := make([]export.ViewSummary, 0, len(out.ViewsSummaryList))
		for _, v := range out.ViewsSummaryList {
			typ := string(v.Type)
			if typ == "" {
				typ = string(types.ViewTypeCustomerManaged)
			}
			items = append(items, export.ViewSummary{
				Arn:    aws.ToString(v.Arn),
				ID:     aws.ToString(v.Id),
				Name:   aws.ToString(v.Name),
				Type:   typ,
				Status: enum(v.Status),
			})
		}
		return items, out.NextToken, nil
	})
}

// ListContactFlowModuleAliases lists one module's aliases; it makes the
// inventory an export.ModuleAliasLister.
// https://docs.aws.amazon.com/connect/latest/APIReference/API_ListContactFlowModuleAliases.html
func (c *inventory) ListContactFlowModuleAliases(ctx context.Context, moduleID string) ([]export.ModuleAlias, error) {
	return paginate(ctx, c.limiter, func(ctx context.Context, nextToken *string) ([]export.ModuleAlias, *string, error) {
		out, err := c.api.ListContactFlowModuleAliases(ctx, &connect.ListContactFlowModuleAliasesInput{
			InstanceId: aws.String(c.instanceID), ContactFlowModuleId: aws.String(moduleID),
			MaxResults: aws.Int32(min(c.maxResults, aliasesMaxResults)), NextToken: nextToken,
		})
		if err != nil {
			return nil, nil, err
		}
		items := make([]export.ModuleAlias, 0, len(out.ContactFlowModuleAliasSummaryList))
		for _, a := range out.ContactFlowModuleAliasSummaryList {
			if a.AliasId == nil || a.AliasName == nil {
				continue
			}
			items = append(items, export.ModuleAlias{AliasID: *a.AliasId, Name: *a.AliasName})
		}
		return items, out.NextToken, nil
	})
}
