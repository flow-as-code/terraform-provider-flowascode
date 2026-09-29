// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package export

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/flowdoc"
	"github.com/flow-as-code/terraform-provider-flowascode/internal/jsonv"
)

// The cases of packages/core/src/export.test.ts that are not the SDK
// adapter or codegen, in its order.

const instance = "arn:aws:connect:us-east-1:111122223333:instance/11111111-2222-3333-4444-555555555555"

func TestParseConnectArn(t *testing.T) {
	got, ok := ParseConnectArn(instance + "/queue/q1")
	want := ConnectArn{
		Partition: "aws", Region: "us-east-1", Account: "111122223333",
		InstanceID:  "11111111-2222-3333-4444-555555555555",
		HasResource: true, ResourceType: "queue", ResourceID: "q1",
	}
	if !ok || got != want {
		t.Fatalf("got %+v", got)
	}
	// The two keywords that do not match their IAM resource-type names.
	if p, _ := ParseConnectArn(instance + "/flow-module/m1"); p.ResourceType != "flow-module" {
		t.Errorf("flow-module: %+v", p)
	}
	if p, _ := ParseConnectArn(instance + "/operating-hours/h1"); p.ResourceType != "operating-hours" {
		t.Errorf("operating-hours: %+v", p)
	}
	// A $SAVED or version qualifier is captured, not glued to the id.
	if p, _ := ParseConnectArn(instance + "/contact-flow/f1:$SAVED"); p.ResourceID != "f1" || p.Qualifier != "$SAVED" || !p.HasQualifier {
		t.Errorf("$SAVED: %+v", p)
	}
	// An AWS-managed view belongs to no instance.
	view, ok := ParseConnectArn("arn:aws:connect:us-east-1:aws:view/after-contact-work:1")
	if !ok || view != (ConnectArn{Partition: "aws", Region: "us-east-1", Account: "aws", HasResource: true,
		ResourceType: "view", ResourceID: "after-contact-work", HasQualifier: true, Qualifier: "1"}) {
		t.Errorf("view: %+v", view)
	}
	if _, ok := ParseConnectArn("arn:aws:connect:us-east-1:aws:view/"); ok {
		t.Error("an empty view name parsed")
	}
	if got := NormalizeArn("arn:aws:connect:us-east-1:aws:view/after-contact-work:1"); got != "arn:aws:connect:us-east-1:aws:view/after-contact-work" {
		t.Errorf("NormalizeArn view: %s", got)
	}
	if got := NormalizeArn(instance + "/contact-flow/f1:$SAVED"); got != instance+"/contact-flow/f1" {
		t.Errorf("NormalizeArn $SAVED: %s", got)
	}
}

func TestParseLambdaFunctionArn(t *testing.T) {
	for in, want := range map[string]string{
		"arn:aws:lambda:us-east-1:111122223333:function:appointment-lookup": "appointment-lookup",
		"arn:aws:lambda:us-east-1:111122223333:function:lookup:PROD":        "lookup",
	} {
		if got, ok := ParseLambdaFunctionArn(in); !ok || got != want {
			t.Errorf("%s: %q", in, got)
		}
	}
	if _, ok := ParseLambdaFunctionArn(instance + "/queue/q1"); ok {
		t.Error("a queue ARN parsed as Lambda")
	}
}

func TestSlugifyResourceName(t *testing.T) {
	for in, want := range map[string]string{
		"Main Line":          "main-line",
		"  Front_Desk (US) ": "front-desk-us",
		"Café":               "cafe",
		"Ｆｕｌｌ":               "full", // fullwidth, NFKD to ASCII
		"--a--":              "a",
	} {
		if got := SlugifyResourceName(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

func TestBuildReverseMap(t *testing.T) {
	inv := loadInventory(t, "demo-instance")
	m := BuildReverseMap(inv)
	for arn, token := range map[string]string{
		instance + "/queue/aaaa1111-0000-4000-8000-000000000001":            "${cdref:queue:appointments}",
		instance + "/operating-hours/bbbb2222-0000-4000-8000-000000000001":  "${cdref:hours:main-line}",
		instance + "/contact-flow/cccc3333-0000-4000-8000-000000000001":     "${cdref:flow:appointment-line}",
		instance + "/flow-module/eeee5555-0000-4000-8000-000000000001":      "${cdref:module:recording-consent}",
		instance + "/prompt/dddd4444-0000-4000-8000-000000000001":           "${cdref:prompt:consent-notice}",
		"arn:aws:lambda:us-east-1:111122223333:function:appointment-lookup": "${cdref:lambda:appointment-lookup}",
	} {
		if e, ok := LookupArn(m, arn); !ok || e.Token != token {
			t.Errorf("%s: %+v", arn, e)
		}
	}
	// A reference that carries a $SAVED or version qualifier resolves.
	if e, _ := LookupArn(m, instance+"/contact-flow/cccc3333-0000-4000-8000-000000000001:$SAVED"); e.Name != "appointment-line" {
		t.Errorf("$SAVED lookup: %+v", e)
	}
	// A Lex V1 bot is warned about rather than silently unmapped.
	if !strings.Contains(strings.Join(m.Warnings, "\n"), "LegacyBot") {
		t.Errorf("no LegacyBot warning: %q", m.Warnings)
	}

	// Deterministic rename when two names slug the same.
	inv.Queues = []ResourceSummary{
		{Arn: instance + "/queue/q1", Name: "Front Desk"},
		{Arn: instance + "/queue/q2", Name: "front_desk"},
	}
	collided := BuildReverseMap(inv)
	a, _ := LookupArn(collided, instance+"/queue/q1")
	b, _ := LookupArn(collided, instance+"/queue/q2")
	if a.Name != "front-desk" || b.Name != "front-desk-2" {
		t.Errorf("rename: %s %s", a.Name, b.Name)
	}
	if !strings.Contains(strings.Join(collided.Warnings, "\n"), `slug to "front-desk"`) {
		t.Errorf("no collision warning: %q", collided.Warnings)
	}

	reverse := ReverseMapOfResourceMap(map[string]string{"${cdref:queue:appointments}": instance + "/queue/q1"})
	if e, _ := LookupArn(reverse, instance+"/queue/q1"); e.Token != "${cdref:queue:appointments}" {
		t.Errorf("resource map: %+v", e)
	}
}

func demoMap(t *testing.T) ReverseMap { return BuildReverseMap(loadInventory(t, "demo-instance")) }

func flowFixture(t *testing.T, caseName, file string) string {
	return string(readCase(t, "export/"+caseName+"/flows/"+file))
}

func TestExportFlowLiftsMetadata(t *testing.T) {
	content := flowFixture(t, "demo-instance", "eeee5555-0000-4000-8000-000000000001.json")
	doc, err := ExportFlow(content, demoMap(t), ExportFlowOptions{Name: "recording-consent", ConnectType: "MODULE"})
	if err != nil {
		t.Fatal(err)
	}
	// The console fixture writes lowercase `position`; both spellings lift.
	layout, _ := doc.Get("layout")
	if got := string(jsonv.Encode(layout, "")); got != `{"done":{"x":420,"y":40},"notify":{"x":160,"y":40}}` {
		t.Errorf("layout %s", got)
	}
	c, _ := doc.Get("content")
	meta, _ := c.(jsonv.Object).Get("Metadata")
	if got := string(jsonv.Encode(meta, "")); got != `{"ActionMetadata":{"notify":{"useDynamic":false}}}` {
		t.Errorf("Metadata %s", got)
	}
	if kind, _ := doc.Get("kind"); kind != "module" {
		t.Errorf("kind %v", kind)
	}
}

func TestExportFlowAutoLayoutAndName(t *testing.T) {
	content := flowFixture(t, "demo-instance", "cccc3333-0000-4000-8000-000000000002.saved.json")
	doc, err := ExportFlow(content, demoMap(t), ExportFlowOptions{Name: "draft-line", ConnectType: "CONTACT_FLOW"})
	if err != nil {
		t.Fatal(err)
	}
	layout, _ := doc.Get("layout")
	if keys := layout.(jsonv.Object).Keys(); !reflect.DeepEqual(keys, []string{"greet", "hang-up"}) {
		t.Errorf("layout keys %q", keys)
	}
	c, _ := doc.Get("content")
	if _, has := c.(jsonv.Object).Get("Metadata"); has {
		t.Error("Metadata present")
	}
	_, err = ExportFlow(content, demoMap(t), ExportFlowOptions{Name: "Draft Line", ConnectType: "CONTACT_FLOW"})
	if err == nil || !strings.Contains(err.Error(), "not a valid FlowDoc name") {
		t.Errorf("non-slug name: %v", err)
	}
}

func TestExportFlowUnknownArns(t *testing.T) {
	content := flowFixture(t, "unknown-arns", "cccc3333-0000-4000-8000-000000000009.json")
	_, err := ExportFlow(content, BuildReverseMap(loadInventory(t, "unknown-arns")),
		ExportFlowOptions{Name: "stale-refs", ConnectType: "CONTACT_FLOW"})
	var e *ExportError
	if !errors.As(err, &e) {
		t.Fatalf("got %v", err)
	}
	if got := e.Locations[e.UnknownArns[0]]; !reflect.DeepEqual(got, []string{"Actions[0].Parameters.HoursOfOperationId"}) {
		t.Errorf("first location %q", got)
	}
	// An AWS-managed ARN with `aws` for its account is a reference, never
	// prose.
	view := "arn:aws:connect:us-east-1:aws:view/after-contact-work:1"
	if got := e.Locations[view]; !reflect.DeepEqual(got, []string{"Actions[3].Parameters.ViewResource.Id"}) {
		t.Errorf("view location %q", got)
	}
	for _, arn := range e.InterpolatedArns {
		if arn == view {
			t.Error("view counted as interpolated")
		}
	}
	if e.Resource != "stale-refs" || !strings.HasPrefix(e.Error(), `Cannot export "stale-refs": 4 ARN(s) not found`) {
		t.Errorf("message %q", e.Error())
	}
}

func TestExportFlowOmittedParameters(t *testing.T) {
	raw := flowFixture(t, "omitted-parameters", "cccc3333-0000-4000-8000-000000000011.json")
	// The fixture records the case, so it cannot silently stop being tested.
	v, _ := jsonv.Decode([]byte(raw))
	actions, _ := v.(jsonv.Object).Get("Actions")
	var bare []any
	for _, a := range actions.([]any) {
		if _, has := a.(jsonv.Object).Get("Parameters"); !has {
			typ, _ := a.(jsonv.Object).Get("Type")
			bare = append(bare, typ)
		}
	}
	if !reflect.DeepEqual(bare, []any{"TransferContactToQueue"}) {
		t.Fatalf("bare actions %v", bare)
	}
	before := jsonv.Encode(v, "")
	doc, err := ExportFlow(v, BuildReverseMap(loadInventory(t, "omitted-parameters")),
		ExportFlowOptions{Name: "default-queue-transfer", ConnectType: "QUEUE_TRANSFER"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, jsonv.Encode(v, "")) {
		t.Error("ExportFlow mutated its input")
	}
	c, _ := doc.Get("content")
	out, _ := c.(jsonv.Object).Get("Actions")
	for _, a := range out.([]any) {
		p, hasP := a.(jsonv.Object).Get("Parameters")
		_, hasT := a.(jsonv.Object).Get("Transitions")
		if !hasP || !hasT {
			t.Errorf("action without Parameters or Transitions: %s", jsonv.Encode(a, ""))
		}
		if typ, _ := a.(jsonv.Object).Get("Type"); typ == "TransferContactToQueue" && string(jsonv.Encode(p, "")) != "{}" {
			t.Errorf("Parameters %s", jsonv.Encode(p, ""))
		}
	}
	// Same document from the text, the bytes, and the decoded value.
	fromText, _ := ExportFlow(raw, BuildReverseMap(loadInventory(t, "omitted-parameters")),
		ExportFlowOptions{Name: "default-queue-transfer", ConnectType: "QUEUE_TRANSFER"})
	fromBytes, _ := ExportFlow([]byte(raw), BuildReverseMap(loadInventory(t, "omitted-parameters")),
		ExportFlowOptions{Name: "default-queue-transfer", ConnectType: "QUEUE_TRANSFER"})
	if !bytes.Equal(flowdoc.Serialize(doc), flowdoc.Serialize(fromText)) || !bytes.Equal(flowdoc.Serialize(doc), flowdoc.Serialize(fromBytes)) {
		t.Error("input forms disagree")
	}
}

func TestExportFlowViewVersion(t *testing.T) {
	m := BuildReverseMap(loadInventory(t, "managed-view"))
	raw, err := readFixture("export/managed-view/flows/cccc3333-0000-4000-8000-000000000021.json")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := ExportFlow(string(raw), m, ExportFlowOptions{Name: "acw", ConnectType: "CONTACT_FLOW"})
	if err != nil {
		t.Fatal(err)
	}
	refs, _ := doc.Get("refs")
	if got := string(jsonv.Encode(refs, "")); got != `[{"token":"${cdref:view:after-contact-work@1}","type":"view","name":"after-contact-work","alias":"1"}]` {
		t.Errorf("refs %s", got)
	}
	// A version that is not a slug stays an unknown ARN rather than losing
	// the version.
	latest := strings.Replace(string(raw), "view/after-contact-work:1", "view/after-contact-work:$LATEST", 1)
	_, err = ExportFlow(latest, m, ExportFlowOptions{Name: "acw", ConnectType: "CONTACT_FLOW"})
	var e *ExportError
	if !errors.As(err, &e) {
		t.Fatalf("got %v", err)
	}
}

func TestExportFlowValidation(t *testing.T) {
	m := ReverseMap{}
	for content, want := range map[string]string{
		`[]`:            "Cannot export: flow content is not a JSON object.",
		`{"Version":1}`: `Cannot export: flow content Version is 1, expected "2019-10-30".`,
		`{}`:            `Cannot export: flow content Version is undefined, expected "2019-10-30".`,
		`{"Version":"2019-10-30","StartAction":7}`:                    "Cannot export: flow content has no StartAction.",
		`{"Version":"2019-10-30","StartAction":"a","Actions":[]}`:     "Cannot export: flow content has no Actions.",
		`{"Version":"2019-10-30","StartAction":"a","Actions":[null]}`: "Cannot read properties of null (reading 'Parameters')",
	} {
		_, err := ExportFlow(content, m, ExportFlowOptions{Name: "x", ConnectType: "CONTACT_FLOW"})
		if err == nil || err.Error() != want {
			t.Errorf("%s: %v, want %q", content, err, want)
		}
	}
}

func TestExportInstanceDemo(t *testing.T) {
	client := newFixtureClient(t, "demo-instance")
	result, err := ExportInstance(context.Background(), client, ExportInstanceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	var draftSaved bool
	for _, f := range result.Flows {
		n, _ := f.Doc.Get("name")
		names = append(names, n.(string))
		if n == "draft-line" {
			draftSaved = f.Saved
		}
		meta, _ := f.Doc.Get("meta")
		source, _ := meta.(jsonv.Object).Get("source")
		if id, _ := source.(jsonv.Object).Get("instanceId"); id != "11111111-2222-3333-4444-555555555555" {
			t.Errorf("%s instanceId %v", n, id)
		}
	}
	if !reflect.DeepEqual(names, []string{"appointment-line", "draft-line", "recording-consent"}) {
		t.Errorf("names %q", names)
	}
	// A never-published flow is read through the $SAVED alias.
	if !contains(client.describeCalls, "cccc3333-0000-4000-8000-000000000002:$SAVED") || !draftSaved {
		t.Errorf("no $SAVED fallback: %q", client.describeCalls)
	}
	warnings := strings.Join(result.Warnings, "\n")
	if !strings.Contains(warnings, "CAMPAIGN") {
		t.Error("no CAMPAIGN warning")
	}
	// Connect returns the separate Settings field and
	// ExternalInvocationConfiguration on every module; neither is worth a
	// warning until it holds a value.
	if strings.Contains(warnings, "external invocation") {
		t.Error("a module whose extra fields say nothing was warned about")
	}

	_, err = ExportInstance(context.Background(), newFixtureClient(t, "demo-instance"), ExportInstanceOptions{NoSavedFallback: true})
	if err == nil || !strings.Contains(err.Error(), "has not been published") {
		t.Errorf("savedFallback off: %v", err)
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func TestIsNotPublishedUnwraps(t *testing.T) {
	err := fmt.Errorf("describe: %w", &apiError{"ContactFlowNotPublishedException", "x"})
	if !isNotPublished(err) {
		t.Error("wrapped not-published error missed")
	}
	if isNotPublished(errors.New("ContactFlowNotPublishedException")) {
		t.Error("a message is not an error code")
	}
}

// recordingClient notes which list operations ran.
type recordingClient struct {
	fixtureClient
	listed []string
	fail   string
}

func (c *recordingClient) note(op string) error {
	c.listed = append(c.listed, op)
	if op == c.fail {
		return errors.New(op + " failed")
	}
	return nil
}

func (c *recordingClient) ListContactFlows(ctx context.Context, types []string) ([]ContactFlowSummary, error) {
	if err := c.note("flows:" + strings.Join(types, ",")); err != nil {
		return nil, err
	}
	return c.fixtureClient.ListContactFlows(ctx, types)
}
func (c *recordingClient) ListContactFlowModules(ctx context.Context) ([]ContactFlowModuleSummary, error) {
	if err := c.note("modules"); err != nil {
		return nil, err
	}
	return c.fixtureClient.ListContactFlowModules(ctx)
}
func (c *recordingClient) ListViews(ctx context.Context) ([]ViewSummary, error) {
	if err := c.note("views"); err != nil {
		return nil, err
	}
	return c.fixtureClient.ListViews(ctx)
}

func TestCollectInventory(t *testing.T) {
	client := &recordingClient{fixtureClient: *newFixtureClient(t, "demo-instance")}
	inv, err := CollectInventory(context.Background(), client, CollectInventoryOptions{FlowTypes: []string{"CAMPAIGN"}, ExcludeModules: true})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(client.listed, []string{"flows:CAMPAIGN", "views"}) {
		t.Errorf("listed %q", client.listed)
	}
	if len(inv.ContactFlows) != 1 || inv.ContactFlowModules == nil || len(inv.ContactFlowModules) != 0 || len(inv.Queues) != 2 {
		t.Errorf("inventory %+v", inv)
	}
	client = &recordingClient{fixtureClient: *newFixtureClient(t, "demo-instance"), fail: "views"}
	if _, err := ExportInstance(context.Background(), client, ExportInstanceOptions{}); err == nil || err.Error() != "views failed" {
		t.Errorf("list failure: %v", err)
	}
	// A supplied inventory is not listed again.
	client = &recordingClient{fixtureClient: *newFixtureClient(t, "demo-instance")}
	inventory := client.inventory
	if _, err := ExportInstance(context.Background(), client, ExportInstanceOptions{Inventory: &inventory}); err != nil || len(client.listed) != 0 {
		t.Errorf("listed %q, err %v", client.listed, err)
	}
}
