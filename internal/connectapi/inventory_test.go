// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package connectapi

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/connect"
	"github.com/aws/aws-sdk-go-v2/service/connect/types"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/export"
)

const (
	testInstanceID = "11111111-2222-3333-4444-555555555555"
	testInstance   = "arn:aws:connect:us-east-1:111122223333:instance/" + testInstanceID
)

// spy is the Fake with every list input recorded, and a cap on calls so a
// pagination bug that never stops fails the test instead of hanging it.
type spy struct {
	*Fake
	inputs []any
}

func (s *spy) note(in any) error {
	s.inputs = append(s.inputs, in)
	if len(s.inputs) > 500 {
		return errors.New("spy: more than 500 list calls; pagination is not ending")
	}
	return nil
}

func (s *spy) ListContactFlows(ctx context.Context, in *connect.ListContactFlowsInput, opts ...func(*connect.Options)) (*connect.ListContactFlowsOutput, error) {
	if err := s.note(in); err != nil {
		return nil, err
	}
	return s.Fake.ListContactFlows(ctx, in, opts...)
}

func (s *spy) ListContactFlowModules(ctx context.Context, in *connect.ListContactFlowModulesInput, opts ...func(*connect.Options)) (*connect.ListContactFlowModulesOutput, error) {
	if err := s.note(in); err != nil {
		return nil, err
	}
	return s.Fake.ListContactFlowModules(ctx, in, opts...)
}

func (s *spy) ListQueues(ctx context.Context, in *connect.ListQueuesInput, opts ...func(*connect.Options)) (*connect.ListQueuesOutput, error) {
	if err := s.note(in); err != nil {
		return nil, err
	}
	return s.Fake.ListQueues(ctx, in, opts...)
}

func (s *spy) ListHoursOfOperations(ctx context.Context, in *connect.ListHoursOfOperationsInput, opts ...func(*connect.Options)) (*connect.ListHoursOfOperationsOutput, error) {
	if err := s.note(in); err != nil {
		return nil, err
	}
	return s.Fake.ListHoursOfOperations(ctx, in, opts...)
}

func (s *spy) ListPrompts(ctx context.Context, in *connect.ListPromptsInput, opts ...func(*connect.Options)) (*connect.ListPromptsOutput, error) {
	if err := s.note(in); err != nil {
		return nil, err
	}
	return s.Fake.ListPrompts(ctx, in, opts...)
}

func (s *spy) ListLambdaFunctions(ctx context.Context, in *connect.ListLambdaFunctionsInput, opts ...func(*connect.Options)) (*connect.ListLambdaFunctionsOutput, error) {
	if err := s.note(in); err != nil {
		return nil, err
	}
	return s.Fake.ListLambdaFunctions(ctx, in, opts...)
}

func (s *spy) ListBots(ctx context.Context, in *connect.ListBotsInput, opts ...func(*connect.Options)) (*connect.ListBotsOutput, error) {
	if err := s.note(in); err != nil {
		return nil, err
	}
	return s.Fake.ListBots(ctx, in, opts...)
}

func (s *spy) ListViews(ctx context.Context, in *connect.ListViewsInput, opts ...func(*connect.Options)) (*connect.ListViewsOutput, error) {
	if err := s.note(in); err != nil {
		return nil, err
	}
	return s.Fake.ListViews(ctx, in, opts...)
}

// pageShape is one recorded list call: the page size asked for and the token
// sent.
type pageShape struct {
	MaxResults int32
	NextToken  string
}

func shapes[T any](s *spy, get func(T) (*int32, *string)) []pageShape {
	var out []pageShape
	for _, in := range s.inputs {
		if typed, ok := in.(T); ok {
			m, n := get(typed)
			out = append(out, pageShape{aws.ToInt32(m), aws.ToString(n)})
		}
	}
	return out
}

// wantShapes is the calls a list of n items takes at page size size: the
// first with no token, each after with the token the one before returned.
func wantShapes(n, size int) []pageShape {
	var out []pageShape
	for start := 0; ; start += size {
		token := ""
		if start > 0 {
			token = fmt.Sprintf("page-%d", start)
		}
		out = append(out, pageShape{int32(size), token})
		if start+size >= n {
			return out
		}
	}
}

func noLimit() *float64 { v := 0.0; return &v }

func arnOf(kind, id string) string { return testInstance + "/" + kind + "/" + id }

// seeded is a fake with n of every resource, plus one agent queue that
// ListQueues must not return.
func seeded(n int) *spy {
	f := NewFake()
	var queues []types.QueueSummary
	var hours []types.HoursOfOperationSummary
	var prompts []types.PromptSummary
	var lambdas []string
	var bots []types.LexBotConfig
	for i := range n {
		id := fmt.Sprintf("%02d", i)
		f.PutContactFlow(types.ContactFlow{
			Arn: aws.String(arnOf("contact-flow", "f"+id)), Id: aws.String("f" + id), Name: aws.String("Flow " + id),
			Type: types.ContactFlowTypeContactFlow, State: types.ContactFlowStateActive, Status: types.ContactFlowStatusPublished,
			Content: aws.String("{}"),
		})
		f.PutContactFlowModule(types.ContactFlowModule{
			Arn: aws.String(arnOf("flow-module", "m"+id)), Id: aws.String("m" + id), Name: aws.String("Module " + id),
			State: types.ContactFlowModuleStateActive, Content: aws.String("{}"),
		})
		queues = append(queues, types.QueueSummary{
			Arn: aws.String(arnOf("queue", "q"+id)), Id: aws.String("q" + id), Name: aws.String("Queue " + id), QueueType: types.QueueTypeStandard,
		})
		hours = append(hours, types.HoursOfOperationSummary{
			Arn: aws.String(arnOf("operating-hours", "h"+id)), Id: aws.String("h" + id), Name: aws.String("Hours " + id),
		})
		prompts = append(prompts, types.PromptSummary{
			Arn: aws.String(arnOf("prompt", "p"+id)), Id: aws.String("p" + id), Name: aws.String("Prompt " + id),
		})
		lambdas = append(lambdas, "arn:aws:lambda:us-east-1:111122223333:function:fn-"+id)
		bots = append(bots,
			types.LexBotConfig{LexBot: &types.LexBot{Name: aws.String("V1Bot" + id), LexRegion: aws.String("us-east-1")}},
			types.LexBotConfig{LexV2Bot: &types.LexV2Bot{AliasArn: aws.String("arn:aws:lex:us-east-1:111122223333:bot-alias/BOT" + id + "/ALIAS")}},
		)
	}
	queues = append(queues, types.QueueSummary{
		Arn: aws.String(arnOf("queue", "agent")), Id: aws.String("agent"), Name: aws.String("agent"), QueueType: types.QueueTypeAgent,
	})
	var views []types.ViewSummary
	for i := range n {
		id := fmt.Sprintf("%02d", i)
		views = append(views, types.ViewSummary{
			Arn: aws.String(arnOf("view", "v"+id)), Id: aws.String("v" + id), Name: aws.String("View " + id),
			Type: types.ViewTypeCustomerManaged, Status: types.ViewStatusPublished,
		})
	}
	views = append(views, types.ViewSummary{
		Arn: aws.String("arn:aws:connect:us-east-1:aws:view/after-contact-work"), Id: aws.String("after-contact-work"),
		Name: aws.String("after-contact-work"), Type: types.ViewTypeAwsManaged, Status: types.ViewStatusPublished,
	})
	f.SetQueues(queues)
	f.SetHoursOfOperations(hours)
	f.SetPrompts(prompts)
	f.SetLambdaFunctions(lambdas)
	f.SetBots(bots)
	f.SetViews(views)
	return &spy{Fake: f}
}

// Every list follows NextToken to the end at the page size it was given,
// ListLambdaFunctions and ListBots at their fixed 25, ListViews at the
// smaller of the page size and 100.
func TestInventoryPaginatesEveryList(t *testing.T) {
	const n = 30
	s := seeded(n)
	client := NewInventoryWithOptions(s, testInstanceID, InventoryOptions{MaxResults: 4, RequestsPerSecond: noLimit()})
	inv, err := export.CollectInventory(context.Background(), client, export.CollectInventoryOptions{})
	if err != nil {
		t.Fatal(err)
	}

	counts := map[string]int{
		"contactFlows": len(inv.ContactFlows), "modules": len(inv.ContactFlowModules), "queues": len(inv.Queues),
		"hours": len(inv.HoursOfOperations), "prompts": len(inv.Prompts), "lambdas": len(inv.LambdaFunctions),
		"bots": len(inv.LexBots), "views": len(inv.Views),
	}
	want := map[string]int{
		"contactFlows": n, "modules": n, "queues": n, "hours": n, "prompts": n, "lambdas": n, "bots": 2 * n, "views": n + 1,
	}
	if !reflect.DeepEqual(counts, want) {
		t.Errorf("counts %v, want %v", counts, want)
	}
	for i, f := range inv.ContactFlows {
		if want := fmt.Sprintf("f%02d", i); aws.ToString(f.ID) != want {
			t.Fatalf("contactFlows[%d] is %s, want %s: pages out of order or repeated", i, aws.ToString(f.ID), want)
		}
	}
	for i, l := range inv.LambdaFunctions {
		if want := fmt.Sprintf("arn:aws:lambda:us-east-1:111122223333:function:fn-%02d", i); l != want {
			t.Fatalf("lambdaFunctions[%d] is %s, want %s", i, l, want)
		}
	}

	checks := []struct {
		op   string
		got  []pageShape
		want []pageShape
	}{
		{"ListContactFlows", shapes(s, func(in *connect.ListContactFlowsInput) (*int32, *string) { return in.MaxResults, in.NextToken }), wantShapes(n, 4)},
		{"ListContactFlowModules", shapes(s, func(in *connect.ListContactFlowModulesInput) (*int32, *string) { return in.MaxResults, in.NextToken }), wantShapes(n, 4)},
		{"ListQueues", shapes(s, func(in *connect.ListQueuesInput) (*int32, *string) { return in.MaxResults, in.NextToken }), wantShapes(n, 4)},
		{"ListHoursOfOperations", shapes(s, func(in *connect.ListHoursOfOperationsInput) (*int32, *string) { return in.MaxResults, in.NextToken }), wantShapes(n, 4)},
		{"ListPrompts", shapes(s, func(in *connect.ListPromptsInput) (*int32, *string) { return in.MaxResults, in.NextToken }), wantShapes(n, 4)},
		{"ListLambdaFunctions", shapes(s, func(in *connect.ListLambdaFunctionsInput) (*int32, *string) { return in.MaxResults, in.NextToken }), wantShapes(n, 25)},
		{"ListBots", shapes(s, func(in *connect.ListBotsInput) (*int32, *string) { return in.MaxResults, in.NextToken }), append(wantShapes(n, 25), wantShapes(n, 25)...)},
		{"ListViews", shapes(s, func(in *connect.ListViewsInput) (*int32, *string) { return in.MaxResults, in.NextToken }), wantShapes(n+1, 4)},
	}
	for _, c := range checks {
		if !reflect.DeepEqual(c.got, c.want) {
			t.Errorf("%s calls %v, want %v", c.op, c.got, c.want)
		}
	}
}

// The defaults are export.ts's: 1000 for the resource lists, 25 for Lambda
// and Lex, 100 for views; every list call names the instance.
func TestInventoryDefaultPageSizes(t *testing.T) {
	s := seeded(2)
	client := NewInventoryWithOptions(s, testInstanceID, InventoryOptions{RequestsPerSecond: noLimit()})
	if _, err := export.CollectInventory(context.Background(), client, export.CollectInventoryOptions{}); err != nil {
		t.Fatal(err)
	}
	got := map[string]int32{}
	for _, in := range s.inputs {
		var op string
		var instance *string
		var m *int32
		switch in := in.(type) {
		case *connect.ListContactFlowsInput:
			op, instance, m = "ListContactFlows", in.InstanceId, in.MaxResults
		case *connect.ListContactFlowModulesInput:
			op, instance, m = "ListContactFlowModules", in.InstanceId, in.MaxResults
		case *connect.ListQueuesInput:
			op, instance, m = "ListQueues", in.InstanceId, in.MaxResults
		case *connect.ListHoursOfOperationsInput:
			op, instance, m = "ListHoursOfOperations", in.InstanceId, in.MaxResults
		case *connect.ListPromptsInput:
			op, instance, m = "ListPrompts", in.InstanceId, in.MaxResults
		case *connect.ListLambdaFunctionsInput:
			op, instance, m = "ListLambdaFunctions", in.InstanceId, in.MaxResults
		case *connect.ListBotsInput:
			op, instance, m = "ListBots", in.InstanceId, in.MaxResults
		case *connect.ListViewsInput:
			op, instance, m = "ListViews", in.InstanceId, in.MaxResults
		}
		if aws.ToString(instance) != testInstanceID {
			t.Errorf("%s: InstanceId %q", op, aws.ToString(instance))
		}
		got[op] = aws.ToInt32(m)
	}
	want := map[string]int32{
		"ListContactFlows": 1000, "ListContactFlowModules": 1000, "ListQueues": 1000,
		"ListHoursOfOperations": 1000, "ListPrompts": 1000, "ListLambdaFunctions": 25,
		"ListBots": 25, "ListViews": 100,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("MaxResults %v, want %v", got, want)
	}
}

// ListBots is two passes because LexVersion is required: V1 then V2, each bot
// tagged with the pass it came from, a V1 bot carrying name and region and a
// V2 bot its alias ARN.
func TestInventoryListBotsTwoPasses(t *testing.T) {
	f := NewFake()
	alias := "arn:aws:lex:us-east-1:111122223333:bot-alias/ABCDEF1234/TSTALIASID"
	f.SetBots([]types.LexBotConfig{
		{LexV2Bot: &types.LexV2Bot{AliasArn: aws.String(alias)}},
		{LexBot: &types.LexBot{Name: aws.String("LegacyBot"), LexRegion: aws.String("us-east-1")}},
	})
	s := &spy{Fake: f}
	bots, err := NewInventoryWithOptions(s, testInstanceID, InventoryOptions{RequestsPerSecond: noLimit()}).ListBots(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []export.LexBotSummary{
		{LexVersion: "V1", Name: aws.String("LegacyBot"), LexRegion: aws.String("us-east-1")},
		{LexVersion: "V2", AliasArn: aws.String(alias)},
	}
	if !reflect.DeepEqual(bots, want) {
		t.Errorf("bots %s, want %s", show(bots), show(want))
	}
	var versions []types.LexVersion
	for _, in := range s.inputs {
		versions = append(versions, in.(*connect.ListBotsInput).LexVersion)
	}
	if !reflect.DeepEqual(versions, []types.LexVersion{types.LexVersionV1, types.LexVersionV2}) {
		t.Errorf("ListBots passes %v, want [V1 V2]", versions)
	}
}

func show(bots []export.LexBotSummary) string {
	out := "["
	for _, b := range bots {
		out += fmt.Sprintf("{%s name=%s region=%s alias=%s}", b.LexVersion, aws.ToString(b.Name), aws.ToString(b.LexRegion), aws.ToString(b.AliasArn))
	}
	return out + "]"
}

// Queues are STANDARD only; the fake holds an agent queue that must not come
// back.
func TestInventoryQueuesAreStandardOnly(t *testing.T) {
	s := seeded(1)
	queues, err := NewInventoryWithOptions(s, testInstanceID, InventoryOptions{RequestsPerSecond: noLimit()}).ListQueues(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(queues) != 1 || queues[0].Name != "Queue 00" {
		t.Errorf("queues %+v, want only the standard queue", queues)
	}
}

// ListViews asks for no Type, so AWS_MANAGED views arrive beside the
// instance's own; a view with no Type reads as CUSTOMER_MANAGED, and one with
// no Status carries none.
func TestInventoryViewsBothTypes(t *testing.T) {
	f := NewFake()
	f.SetViews([]types.ViewSummary{
		{Arn: aws.String("arn:aws:connect:us-east-1:aws:view/after-contact-work"), Id: aws.String("after-contact-work"),
			Name: aws.String("after-contact-work"), Type: types.ViewTypeAwsManaged, Status: types.ViewStatusPublished},
		{Arn: aws.String(arnOf("view", "v1")), Id: aws.String("v1"), Name: aws.String("Mine")},
	})
	views, err := NewInventoryWithOptions(f, testInstanceID, InventoryOptions{RequestsPerSecond: noLimit()}).ListViews(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []export.ViewSummary{
		{Arn: "arn:aws:connect:us-east-1:aws:view/after-contact-work", ID: "after-contact-work", Name: "after-contact-work",
			Type: "AWS_MANAGED", Status: aws.String("PUBLISHED")},
		{Arn: arnOf("view", "v1"), ID: "v1", Name: "Mine", Type: "CUSTOMER_MANAGED"},
	}
	if !reflect.DeepEqual(views, want) {
		t.Errorf("views %+v, want %+v", views, want)
	}
}

// An empty list is [] and not nil, so it serializes as the TypeScript's does.
func TestInventoryEmptyListsAreEmptyNotNil(t *testing.T) {
	inv, err := export.CollectInventory(context.Background(),
		NewInventoryWithOptions(NewFake(), testInstanceID, InventoryOptions{RequestsPerSecond: noLimit()}),
		export.CollectInventoryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if inv.ContactFlows == nil || inv.ContactFlowModules == nil || inv.Queues == nil || inv.HoursOfOperations == nil ||
		inv.Prompts == nil || inv.LambdaFunctions == nil || inv.LexBots == nil || inv.Views == nil {
		t.Errorf("a nil list in %+v", inv)
	}
}

// Describe maps the SDK's fields as export.ts's described does, and passes a
// `:$SAVED` id through so a never-published flow is readable.
func TestInventoryDescribe(t *testing.T) {
	f := NewFake()
	f.PutContactFlow(types.ContactFlow{
		Arn: aws.String(arnOf("contact-flow", "f1")), Id: aws.String("f1"), Name: aws.String("Main"),
		Type: types.ContactFlowTypeContactFlow, State: types.ContactFlowStateActive, Status: types.ContactFlowStatusPublished,
		Version: aws.Int64(7), Description: aws.String("the main line"), Content: aws.String(`{"Actions":[]}`),
		FlowContentSha256: aws.String("abc"),
	})
	f.PutContactFlow(types.ContactFlow{
		Arn: aws.String(arnOf("contact-flow", "f2")), Id: aws.String("f2"), Name: aws.String("Draft"),
		Status: types.ContactFlowStatusSaved, Content: aws.String("{}"),
	})
	f.MarkNeverPublished("f2")
	f.PutContactFlowModule(types.ContactFlowModule{
		Arn: aws.String(arnOf("flow-module", "m1")), Id: aws.String("m1"), Name: aws.String("Mod"),
		State: types.ContactFlowModuleStateActive, Status: types.ContactFlowModuleStatusPublished,
		Content: aws.String("{}"), FlowModuleContentSha256: aws.String("def"), Settings: aws.String("{}"),
		ExternalInvocationConfiguration: &types.ExternalInvocationConfiguration{Enabled: false},
	})
	client := NewInventoryWithOptions(f, testInstanceID, InventoryOptions{RequestsPerSecond: noLimit()})
	ctx := context.Background()

	flow, err := client.DescribeContactFlow(ctx, "f1")
	if err != nil {
		t.Fatal(err)
	}
	wantFlow := export.DescribedContactFlow{
		Arn: arnOf("contact-flow", "f1"), ID: "f1", Name: "Main", Type: aws.String("CONTACT_FLOW"), State: aws.String("ACTIVE"),
		Status: aws.String("PUBLISHED"), Version: aws.String("7"), Description: aws.String("the main line"),
		Content: `{"Actions":[]}`, ContentSha256: aws.String("abc"),
	}
	if !reflect.DeepEqual(flow, wantFlow) {
		t.Errorf("flow %+v, want %+v", flow, wantFlow)
	}

	_, err = client.DescribeContactFlow(ctx, "f2")
	var coded interface{ ErrorCode() string }
	if !errors.As(err, &coded) || coded.ErrorCode() != "ContactFlowNotPublishedException" {
		t.Fatalf("never-published describe: %v, want ContactFlowNotPublishedException", err)
	}
	draft, err := client.DescribeContactFlow(ctx, "f2:$SAVED")
	if err != nil || draft.ID != "f2" || draft.Status == nil || *draft.Status != "SAVED" || draft.Type != nil || draft.Version != nil {
		t.Errorf("$SAVED describe: %+v, %v", draft, err)
	}

	module, err := client.DescribeContactFlowModule(ctx, "m1")
	if err != nil {
		t.Fatal(err)
	}
	wantModule := export.DescribedContactFlowModule{
		DescribedContactFlow: export.DescribedContactFlow{
			Arn: arnOf("flow-module", "m1"), ID: "m1", Name: "Mod", State: aws.String("ACTIVE"), Status: aws.String("PUBLISHED"),
			Content: "{}", ContentSha256: aws.String("def"),
		},
		Settings: aws.String("{}"), ExternalInvocationEnabled: aws.Bool(false),
	}
	if !reflect.DeepEqual(module, wantModule) {
		t.Errorf("module %+v, want %+v", module, wantModule)
	}
}

// fakeClock advances only when the limiter sleeps, and records each sleep.
type fakeClock struct {
	now    time.Time
	sleeps []time.Duration
}

func (c *fakeClock) Now() time.Time { return c.now }
func (c *fakeClock) Sleep(_ context.Context, d time.Duration) error {
	c.sleeps = append(c.sleeps, d)
	c.now = c.now.Add(d)
	return nil
}

// Every request, list page and describe alike, goes through one limiter: the
// first at once, each after it ceil(1000/rps) milliseconds after the last.
func TestInventoryRateLimit(t *testing.T) {
	for _, c := range []struct {
		name string
		rps  *float64
		gap  time.Duration
	}{
		{"default is 2 rps", nil, 500 * time.Millisecond},
		{"3 rps rounds up", aws.Float64(3), 334 * time.Millisecond},
		{"zero turns it off", aws.Float64(0), 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := seeded(5)
			clock := &fakeClock{now: time.Unix(1_700_000_000, 0)}
			client := NewInventoryWithOptions(s, testInstanceID, InventoryOptions{
				MaxResults: 2, RequestsPerSecond: c.rps, Sleep: clock.Sleep, Now: clock.Now,
			})
			ctx := context.Background()
			if _, err := client.ListQueues(ctx); err != nil { // 3 pages
				t.Fatal(err)
			}
			if _, err := client.DescribeContactFlow(ctx, "f00"); err != nil {
				t.Fatal(err)
			}
			if _, err := client.DescribeContactFlowModule(ctx, "m00"); err != nil {
				t.Fatal(err)
			}
			var want []time.Duration
			if c.gap > 0 {
				want = []time.Duration{c.gap, c.gap, c.gap, c.gap}
			}
			if !reflect.DeepEqual(clock.sleeps, want) {
				t.Errorf("sleeps %v, want %v", clock.sleeps, want)
			}
		})
	}
}

// A wait the context cuts short fails the call without sending it.
func TestInventoryRateLimitHonorsContext(t *testing.T) {
	f := NewFake()
	client := NewInventoryWithOptions(f, testInstanceID, InventoryOptions{RequestsPerSecond: aws.Float64(0.001)})
	if _, err := client.ListPrompts(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := client.ListPrompts(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want the deadline", err)
	}
	if len(f.Calls) != 1 {
		t.Errorf("calls %v: the throttled request was sent", f.Calls)
	}
}

// ContactFlowTypes goes to Connect only when the caller filters; nil lists
// every type.
func TestInventoryListContactFlowsFilter(t *testing.T) {
	f := NewFake()
	for _, c := range []struct {
		id  string
		typ types.ContactFlowType
	}{{"f1", types.ContactFlowTypeContactFlow}, {"f2", types.ContactFlowTypeCampaign}} {
		f.PutContactFlow(types.ContactFlow{Arn: aws.String(arnOf("contact-flow", c.id)), Id: aws.String(c.id), Name: aws.String(c.id), Type: c.typ})
	}
	s := &spy{Fake: f}
	client := NewInventoryWithOptions(s, testInstanceID, InventoryOptions{RequestsPerSecond: noLimit()})
	all, err := client.ListContactFlows(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	some, err := client.ListContactFlows(context.Background(), []string{"CAMPAIGN"})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 || len(some) != 1 || aws.ToString(some[0].ID) != "f2" || aws.ToString(some[0].ContactFlowType) != "CAMPAIGN" {
		t.Errorf("all %+v, filtered %+v", all, some)
	}
	if first := s.inputs[0].(*connect.ListContactFlowsInput); first.ContactFlowTypes != nil {
		t.Errorf("unfiltered call sent ContactFlowTypes %v", first.ContactFlowTypes)
	}
}
