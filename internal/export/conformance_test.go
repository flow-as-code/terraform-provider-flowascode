// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package export

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/conformance"
	"github.com/flow-as-code/terraform-provider-flowascode/internal/flowdoc"
	"github.com/flow-as-code/terraform-provider-flowascode/internal/jsonv"
)

// The conformance/export family, replayed as packages/core/src/export.test.ts
// replays it: conformance/export/<case>/inventory.json is the List* responses,
// flows/<id>.json the published DescribeContactFlow Content, and
// flows/<id>.saved.json a flow that has never been published. A case either
// has expected/<name>.flowdoc.json goldens, compared byte for byte through
// flowdoc.Serialize, or an expected-error.json listing the unknown and
// interpolated ARNs. The .flow.ts goldens are TypeScript only
// (conformance/README.md, "What a second implementation must pass").

// apiError stands in for the AWS SDK's smithy.APIError.
type apiError struct{ code, message string }

func (e *apiError) Error() string     { return e.message }
func (e *apiError) ErrorCode() string { return e.code }

// fixtureClient is export.test.ts's FixtureClient.
type fixtureClient struct {
	caseName      string
	inventory     InstanceInventory
	describeCalls []string
}

func readCase(t *testing.T, name string) []byte {
	t.Helper()
	b, err := fs.ReadFile(conformance.FS(), name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func loadInventory(t *testing.T, caseName string) InstanceInventory {
	t.Helper()
	var inv InstanceInventory
	if err := json.Unmarshal(readCase(t, "export/"+caseName+"/inventory.json"), &inv); err != nil {
		t.Fatal(err)
	}
	return inv
}

// readFixture reads from the vendored tree only; fixtures are never added or
// edited here (CONTRIBUTING.md).
func readFixture(name string) ([]byte, error) {
	return fs.ReadFile(conformance.FS(), name)
}

func newFixtureClient(t *testing.T, caseName string) *fixtureClient {
	return &fixtureClient{caseName: caseName, inventory: loadInventory(t, caseName)}
}

func (c *fixtureClient) content(id string) (string, error) {
	c.describeCalls = append(c.describeCalls, id)
	saved := strings.HasSuffix(id, ":$SAVED")
	base := strings.TrimSuffix(id, ":$SAVED")
	dir := "export/" + c.caseName + "/flows/"
	published, pubErr := readFixture(dir + base + ".json")
	draft, draftErr := readFixture(dir + base + ".saved.json")
	switch {
	case saved && draftErr == nil:
		return string(draft), nil
	case saved && pubErr == nil:
		return string(published), nil
	case !saved && pubErr == nil:
		return string(published), nil
	case !saved && draftErr == nil:
		return "", &apiError{"ContactFlowNotPublishedException", "Flow " + base + " has not been published."}
	}
	return "", &apiError{"ResourceNotFoundException", "No fixture for " + id}
}

func (c *fixtureClient) summaryOf(id string) ResourceSummary {
	for _, s := range c.inventory.ContactFlows {
		if s.ID != nil && *s.ID == id {
			return s.ResourceSummary
		}
	}
	for _, s := range c.inventory.ContactFlowModules {
		if s.ID != nil && *s.ID == id {
			return s.ResourceSummary
		}
	}
	return ResourceSummary{}
}

func (c *fixtureClient) ListContactFlows(_ context.Context, types []string) ([]ContactFlowSummary, error) {
	if types == nil {
		return c.inventory.ContactFlows, nil
	}
	var out []ContactFlowSummary
	for _, f := range c.inventory.ContactFlows {
		typ := ""
		if f.ContactFlowType != nil {
			typ = *f.ContactFlowType
		}
		for _, want := range types {
			if typ == want {
				out = append(out, f)
			}
		}
	}
	return out, nil
}

func (c *fixtureClient) DescribeContactFlow(_ context.Context, id string) (DescribedContactFlow, error) {
	content, err := c.content(id)
	if err != nil {
		return DescribedContactFlow{}, err
	}
	base := strings.Replace(id, ":$SAVED", "", 1)
	summary := c.summaryOf(base)
	status := "PUBLISHED"
	return DescribedContactFlow{Arn: summary.Arn, ID: base, Name: summary.Name, Status: &status, Content: content}, nil
}

func (c *fixtureClient) ListContactFlowModules(context.Context) ([]ContactFlowModuleSummary, error) {
	return c.inventory.ContactFlowModules, nil
}

func (c *fixtureClient) DescribeContactFlowModule(_ context.Context, id string) (DescribedContactFlowModule, error) {
	content, err := c.content(id)
	if err != nil {
		return DescribedContactFlowModule{}, err
	}
	base := strings.Replace(id, ":$SAVED", "", 1)
	summary := c.summaryOf(base)
	settings := "{}"
	return DescribedContactFlowModule{
		DescribedContactFlow: DescribedContactFlow{Arn: summary.Arn, ID: base, Name: summary.Name, Content: content},
		Settings:             &settings,
	}, nil
}

func (c *fixtureClient) ListQueues(context.Context) ([]ResourceSummary, error) {
	return c.inventory.Queues, nil
}
func (c *fixtureClient) ListHoursOfOperations(context.Context) ([]ResourceSummary, error) {
	return c.inventory.HoursOfOperations, nil
}
func (c *fixtureClient) ListPrompts(context.Context) ([]ResourceSummary, error) {
	return c.inventory.Prompts, nil
}
func (c *fixtureClient) ListLambdaFunctions(context.Context) ([]string, error) {
	return c.inventory.LambdaFunctions, nil
}
func (c *fixtureClient) ListBots(context.Context) ([]LexBotSummary, error) {
	return c.inventory.LexBots, nil
}
func (c *fixtureClient) ListViews(context.Context) ([]ViewSummary, error) {
	return c.inventory.Views, nil
}

func exportCases(t *testing.T) []string {
	t.Helper()
	entries, err := fs.ReadDir(conformance.FS(), "export")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	if len(out) < 4 {
		t.Fatalf("only %d export cases vendored", len(out))
	}
	return out
}

func strp(s string) *string { return &s }

// TestExportConformance runs every conformance/export case. The generator is
// "core@0.2", the value the goldens record (export.test.ts passes it too);
// the CLI's own meta.generator differs by design.
func TestExportConformance(t *testing.T) {
	for _, name := range exportCases(t) {
		t.Run(name, func(t *testing.T) {
			if b, err := fs.ReadFile(conformance.FS(), "export/"+name+"/expected-error.json"); err == nil {
				runErrorCase(t, name, b)
				return
			}
			runGoldenCase(t, name)
		})
	}
}

func runGoldenCase(t *testing.T, name string) {
	goldens, err := fs.Glob(conformance.FS(), "export/"+name+"/expected/*.flowdoc.json")
	if err != nil || len(goldens) == 0 {
		t.Fatalf("no goldens for %s (%v)", name, err)
	}
	client := newFixtureClient(t, name)
	result, err := ExportInstance(context.Background(), client, ExportInstanceOptions{Generator: strp("core@0.2")})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Failures) != 0 {
		t.Fatalf("failures: %+v", result.Failures)
	}
	var got, want []string
	for _, flow := range result.Flows {
		docName, _ := flow.Doc.Get("name")
		got = append(got, "export/"+name+"/expected/"+docName.(string)+".flowdoc.json")
	}
	want = append(want, goldens...)
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("exported %v, goldens %v", got, want)
	}
	for _, flow := range result.Flows {
		docName, _ := flow.Doc.Get("name")
		golden := readCase(t, "export/"+name+"/expected/"+docName.(string)+".flowdoc.json")
		serialized := flowdoc.Serialize(flow.Doc)
		if !bytes.Equal(serialized, golden) {
			t.Errorf("%s differs from its golden:\n%s\n---\n%s", docName, serialized, golden)
		}
		// An exported FlowDoc is an authored document: no literal ARN
		// anywhere, meta included.
		if bytes.Contains(serialized, []byte("arn:aws")) {
			t.Errorf("%s carries a literal ARN", docName)
		}
	}
}

func runErrorCase(t *testing.T, name string, expectedJSON []byte) {
	var expected struct {
		UnknownArns      []string `json:"unknownArns"`
		InterpolatedArns []string `json:"interpolatedArns"`
	}
	if err := json.Unmarshal(expectedJSON, &expected); err != nil {
		t.Fatal(err)
	}

	// Aborts on an unknown ARN by default, listing every one at once.
	client := newFixtureClient(t, name)
	_, err := ExportInstance(context.Background(), client, ExportInstanceOptions{})
	var exportErr *ExportError
	if !errors.As(err, &exportErr) {
		t.Fatalf("got %v, want an *ExportError", err)
	}
	if !reflect.DeepEqual(exportErr.UnknownArns, expected.UnknownArns) {
		t.Errorf("unknownArns %q, want %q", exportErr.UnknownArns, expected.UnknownArns)
	}
	if !reflect.DeepEqual(exportErr.InterpolatedArns, expected.InterpolatedArns) {
		t.Errorf("interpolatedArns %q, want %q", exportErr.InterpolatedArns, expected.InterpolatedArns)
	}
	for _, arn := range append(append([]string{}, expected.UnknownArns...), expected.InterpolatedArns...) {
		if !strings.Contains(exportErr.Error(), arn) {
			t.Errorf("message does not name %s: %s", arn, exportErr.Error())
		}
		if len(exportErr.Locations[arn]) == 0 {
			t.Errorf("no location for %s", arn)
		}
	}
	if exportErr.Resource == "" || !strings.HasPrefix(exportErr.Error(), `Cannot export "`+exportErr.Resource+`": `) {
		t.Errorf("message does not name the flow: %q", exportErr.Error())
	}

	// Collected instead of aborting when asked to.
	client = newFixtureClient(t, name)
	result, err := ExportInstance(context.Background(), client, ExportInstanceOptions{CollectFailures: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Flows) != 0 || len(result.Failures) != 1 {
		t.Fatalf("flows %d, failures %d", len(result.Flows), len(result.Failures))
	}
	if !reflect.DeepEqual(result.Failures[0].UnknownArns, expected.UnknownArns) {
		t.Errorf("failure unknownArns %q", result.Failures[0].UnknownArns)
	}
}

// The demo instance's appointment-line export is the demo FlowDoc apart from
// meta (export.test.ts, "matches the demo FlowDoc apart from meta").
func TestExportMatchesDemoApartFromMeta(t *testing.T) {
	result, err := ExportInstance(context.Background(), newFixtureClient(t, "demo-instance"), ExportInstanceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var doc jsonv.Object
	for _, f := range result.Flows {
		if n, _ := f.Doc.Get("name"); n == "appointment-line" {
			doc = append(jsonv.Object{}, f.Doc...)
		}
	}
	if doc == nil {
		t.Fatal("appointment-line not exported")
	}
	doc.Delete("meta")
	demoValue, err := jsonv.Decode(readCase(t, "demo/appointment-line.flowdoc.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := flowdoc.Serialize(doc), flowdoc.Serialize(demoValue.(jsonv.Object)); !bytes.Equal(got, want) {
		t.Fatalf("export differs from the demo:\n%s\n---\n%s", got, want)
	}
}
