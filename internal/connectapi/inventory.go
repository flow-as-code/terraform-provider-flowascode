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
//
// Added for FlowDoc 0.3, as export.ts adds them (flow-as-code tasks/D01):
//   ListTaskTemplates           https://docs.aws.amazon.com/connect/latest/APIReference/API_ListTaskTemplates.html
//   ListPhoneNumbersV2          https://docs.aws.amazon.com/connect/latest/APIReference/API_ListPhoneNumbersV2.html
//   ListIntegrationAssociations https://docs.aws.amazon.com/connect/latest/APIReference/API_ListIntegrationAssociations.html
//   Cases ListTemplates         https://docs.aws.amazon.com/connect/latest/APIReference/API_connect-cases_ListTemplates.html
//   Cases ListFields            https://docs.aws.amazon.com/connect/latest/APIReference/API_connect-cases_ListFields.html
//   Q in Connect ListAssistants https://docs.aws.amazon.com/amazon-q-connect/latest/APIReference/API_ListAssistants.html
// The first two are Connect's and always read. The Cases and Q in Connect
// lists are other services' clients, optional in InventoryOptions as `cases`
// and `qconnect` are on createConnectInventoryClient: without one the
// inventory names no Cases resource or no assistant, and such an ARN in flow
// content is reported as unknown.

import (
	"context"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/connect"
	"github.com/aws/aws-sdk-go-v2/service/connect/types"
	"github.com/aws/aws-sdk-go-v2/service/connectcases"
	"github.com/aws/aws-sdk-go-v2/service/qconnect"

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
	// smallListMaxResults is the maximum of ListTaskTemplates,
	// ListIntegrationAssociations, and the Cases and Q in Connect lists.
	smallListMaxResults int32 = 100
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
	// Cases is an Amazon Connect Cases client for the same account and
	// Region. Nil means the inventory reads no Cases domain, and a Cases
	// template or field ARN in flow content is reported as unknown.
	Cases CasesAPI
	// QConnect is an Amazon Q in Connect client for the same account and
	// Region. Nil means the inventory lists no assistants.
	QConnect QConnectAPI
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
		cases:      options.Cases,
		qconnect:   options.QConnect,
		instanceID: instanceID,
		maxResults: maxResults,
		limiter:    newRateLimiter(rps, options.Sleep, options.Now),
	}
}

// inventory is the adapter. One limiter serves the three clients: they are
// one account's quotas, read in one sitting.
type inventory struct {
	api        API
	cases      CasesAPI
	qconnect   QConnectAPI
	instanceID string
	maxResults int32
	limiter    *rateLimiter
}

var (
	_ export.ConnectInventoryClient = (*inventory)(nil)
	_ export.ModuleAliasLister      = (*inventory)(nil)
	_ export.TaskTemplateLister     = (*inventory)(nil)
	_ export.PhoneNumberLister      = (*inventory)(nil)
	_ export.CasesDomainDescriber   = (*inventory)(nil)
	_ export.AssistantLister        = (*inventory)(nil)
)

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

// ListTaskTemplates caps a page at 100 and documents NextToken as always
// null, so one page is the whole list; the loop costs nothing if that
// changes.
// https://docs.aws.amazon.com/connect/latest/APIReference/API_ListTaskTemplates.html
func (c *inventory) ListTaskTemplates(ctx context.Context) ([]export.ResourceSummary, error) {
	return paginate(ctx, c.limiter, func(ctx context.Context, nextToken *string) ([]export.ResourceSummary, *string, error) {
		out, err := c.api.ListTaskTemplates(ctx, &connect.ListTaskTemplatesInput{
			InstanceId: aws.String(c.instanceID),
			MaxResults: aws.Int32(min(c.maxResults, smallListMaxResults)),
			NextToken:  nextToken,
		})
		if err != nil {
			return nil, nil, err
		}
		items := make([]export.ResourceSummary, 0, len(out.TaskTemplates))
		for _, t := range out.TaskTemplates {
			items = append(items, summary(t.Arn, t.Id, t.Name))
		}
		return items, out.NextToken, nil
	})
}

// ListPhoneNumbers is ListPhoneNumbersV2, which takes the instance as
// InstanceId or as TargetArn (which may also be a traffic distribution
// group); with neither it lists the whole account. The response names the
// ARN PhoneNumberArn, not Arn.
// https://docs.aws.amazon.com/connect/latest/APIReference/API_ListPhoneNumbersV2.html
func (c *inventory) ListPhoneNumbers(ctx context.Context) ([]export.PhoneNumberSummary, error) {
	return paginate(ctx, c.limiter, func(ctx context.Context, nextToken *string) ([]export.PhoneNumberSummary, *string, error) {
		in := &connect.ListPhoneNumbersV2Input{MaxResults: aws.Int32(c.maxResults), NextToken: nextToken}
		if strings.HasPrefix(c.instanceID, "arn:") {
			in.TargetArn = aws.String(c.instanceID)
		} else {
			in.InstanceId = aws.String(c.instanceID)
		}
		out, err := c.api.ListPhoneNumbersV2(ctx, in)
		if err != nil {
			return nil, nil, err
		}
		items := make([]export.PhoneNumberSummary, 0, len(out.ListPhoneNumbersSummaryList))
		for _, n := range out.ListPhoneNumbersSummaryList {
			items = append(items, export.PhoneNumberSummary{
				Arn:         aws.ToString(n.PhoneNumberArn),
				ID:          aws.ToString(n.PhoneNumberId),
				Number:      aws.ToString(n.PhoneNumber),
				Description: n.PhoneNumberDescription,
				Type:        enum(n.PhoneNumberType),
			})
		}
		return items, out.NextToken, nil
	})
}

// DescribeCasesDomain finds the instance's Cases domain as the IntegrationArn
// of its CASES_DOMAIN association, then lists the domain's templates and
// fields from the Cases service. Nil, with no call made, when the adapter has
// no Cases client; nil when the instance has no domain.
// https://docs.aws.amazon.com/connect/latest/APIReference/API_ListIntegrationAssociations.html
// https://docs.aws.amazon.com/connect/latest/APIReference/API_connect-cases_ListTemplates.html
// https://docs.aws.amazon.com/connect/latest/APIReference/API_connect-cases_ListFields.html
func (c *inventory) DescribeCasesDomain(ctx context.Context) (*export.CasesDomainInventory, error) {
	if c.cases == nil {
		return nil, nil
	}
	associations, err := paginate(ctx, c.limiter, func(ctx context.Context, nextToken *string) ([]types.IntegrationAssociationSummary, *string, error) {
		out, err := c.api.ListIntegrationAssociations(ctx, &connect.ListIntegrationAssociationsInput{
			InstanceId:      aws.String(c.instanceID),
			IntegrationType: types.IntegrationTypeCasesDomain,
			MaxResults:      aws.Int32(smallListMaxResults),
			NextToken:       nextToken,
		})
		if err != nil {
			return nil, nil, err
		}
		return out.IntegrationAssociationSummaryList, out.NextToken, nil
	})
	if err != nil {
		return nil, err
	}
	domainArn := ""
	for _, a := range associations {
		if arn := aws.ToString(a.IntegrationArn); arn != "" {
			if _, ok := export.ParseCasesArn(arn); ok {
				domainArn = arn
				break
			}
		}
	}
	if domainArn == "" {
		return nil, nil
	}
	parsed, _ := export.ParseCasesArn(domainArn)
	domainID := parsed.DomainID
	templates, err := paginate(ctx, c.limiter, func(ctx context.Context, nextToken *string) ([]export.ResourceSummary, *string, error) {
		out, err := c.cases.ListTemplates(ctx, &connectcases.ListTemplatesInput{
			DomainId: aws.String(domainID), MaxResults: aws.Int32(smallListMaxResults), NextToken: nextToken,
		})
		if err != nil {
			return nil, nil, err
		}
		items := make([]export.ResourceSummary, 0, len(out.Templates))
		for _, t := range out.Templates {
			items = append(items, export.ResourceSummary{Arn: aws.ToString(t.TemplateArn), ID: aws.String(aws.ToString(t.TemplateId)), Name: aws.ToString(t.Name)})
		}
		return items, out.NextToken, nil
	})
	if err != nil {
		return nil, err
	}
	fields, err := paginate(ctx, c.limiter, func(ctx context.Context, nextToken *string) ([]export.ResourceSummary, *string, error) {
		out, err := c.cases.ListFields(ctx, &connectcases.ListFieldsInput{
			DomainId: aws.String(domainID), MaxResults: aws.Int32(smallListMaxResults), NextToken: nextToken,
		})
		if err != nil {
			return nil, nil, err
		}
		items := make([]export.ResourceSummary, 0, len(out.Fields))
		for _, f := range out.Fields {
			items = append(items, export.ResourceSummary{Arn: aws.ToString(f.FieldArn), ID: aws.String(aws.ToString(f.FieldId)), Name: aws.ToString(f.Name)})
		}
		return items, out.NextToken, nil
	})
	if err != nil {
		return nil, err
	}
	return &export.CasesDomainInventory{DomainArn: domainArn, DomainID: domainID, Templates: templates, Fields: fields}, nil
}

// ListAssistants lists Amazon Q in Connect assistants from that service's
// own client; a page caps at 100. Nil, with no call made, when the adapter
// has no Q in Connect client. It is account-wide: every assistant the
// account has in the Region is named, associated with this instance or not,
// the stance ListViews takes with AWS-managed views.
// https://docs.aws.amazon.com/amazon-q-connect/latest/APIReference/API_ListAssistants.html
func (c *inventory) ListAssistants(ctx context.Context) ([]export.ResourceSummary, error) {
	if c.qconnect == nil {
		return nil, nil
	}
	return paginate(ctx, c.limiter, func(ctx context.Context, nextToken *string) ([]export.ResourceSummary, *string, error) {
		out, err := c.qconnect.ListAssistants(ctx, &qconnect.ListAssistantsInput{
			MaxResults: aws.Int32(smallListMaxResults), NextToken: nextToken,
		})
		if err != nil {
			return nil, nil, err
		}
		items := make([]export.ResourceSummary, 0, len(out.AssistantSummaries))
		for _, a := range out.AssistantSummaries {
			items = append(items, export.ResourceSummary{Arn: aws.ToString(a.AssistantArn), ID: aws.String(aws.ToString(a.AssistantId)), Name: aws.ToString(a.Name)})
		}
		return items, out.NextToken, nil
	})
}
