// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package export

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/flowdoc"
	"github.com/flow-as-code/terraform-provider-flowascode/internal/jsonv"
)

// testdata/ts-oracle.json is what @flow-as-code/core's export.ts itself
// returns for the inputs in it, recorded by running the built
// packages/core/dist (generatedFrom names the commit) over them. Flow
// content is JSON text, so both sides parse the same bytes; documents are
// compared as serialize() wrote them, and errors by message. It covers every
// ARN shape parseConnectArn accepts or refuses, slugging of non-ASCII names,
// reverse-map collisions and warnings, and exportFlow and exportInstance
// edge cases the four conformance cases do not reach.

type oracle struct {
	ParseConnectArn []struct {
		Input  string          `json:"input"`
		Output json.RawMessage `json:"output"`
	} `json:"parseConnectArn"`
	ParseLambdaFunctionArn []struct {
		Input  string  `json:"input"`
		Output *string `json:"output"`
	} `json:"parseLambdaFunctionArn"`
	NormalizeArn []struct {
		Input  string `json:"input"`
		Output string `json:"output"`
	} `json:"normalizeArn"`
	SlugifyResourceName []struct {
		Input  string `json:"input"`
		Output string `json:"output"`
	} `json:"slugifyResourceName"`
	BuildReverseMap []struct {
		Inventory string      `json:"inventory"`
		ByArn     []oracleRef `json:"byArn"`
		Warnings  []string    `json:"warnings"`
	} `json:"buildReverseMap"`
	ReverseMapOfResourceMap []struct {
		Map      map[string]string `json:"map"`
		ByArn    []oracleRef       `json:"byArn"`
		Warnings []string          `json:"warnings"`
	} `json:"reverseMapOfResourceMap"`
	ExportFlow []struct {
		Content     string            `json:"content"`
		Inv         string            `json:"inv"`
		ResourceMap map[string]string `json:"resourceMap"`
		Options     json.RawMessage   `json:"options"`
		Result      oracleResult      `json:"result"`
	} `json:"exportFlow"`
	ExportInstance []struct {
		Spec    string          `json:"spec"`
		Options json.RawMessage `json:"options"`
		Calls   []string        `json:"calls"`
		Result  struct {
			Flows []struct {
				Arn        string `json:"arn"`
				ID         string `json:"id"`
				SourceName string `json:"sourceName"`
				Saved      bool   `json:"saved"`
				Doc        string `json:"doc"`
			} `json:"flows"`
			Warnings []string `json:"warnings"`
			Failures []struct {
				Arn         string   `json:"arn"`
				Name        string   `json:"name"`
				Reason      string   `json:"reason"`
				UnknownArns []string `json:"unknownArns"`
			} `json:"failures"`
			Error *oracleError `json:"error"`
		} `json:"result"`
	} `json:"exportInstance"`
}

type oracleRef struct {
	Arn   string `json:"arn"`
	Token string `json:"token"`
	Type  string `json:"type"`
	Name  string `json:"name"`
	Alias string `json:"alias"`
}

type oracleError struct {
	Name             string              `json:"name"`
	Message          string              `json:"message"`
	UnknownArns      []string            `json:"unknownArns"`
	InterpolatedArns []string            `json:"interpolatedArns"`
	Locations        map[string][]string `json:"locations"`
	Resource         string              `json:"resource"`
}

type oracleResult struct {
	Doc   *string      `json:"doc"`
	Refs  *string      `json:"refs"`
	Error *oracleError `json:"error"`
}

// knownDeviations are oracle cases whose documents differ for a reason
// outside this package; the test holds each to still differing, so an entry
// goes stale loudly when the cause is fixed. Their refs are still compared.
var knownDeviations = map[string]string{
	// Actions elements that are not objects: flowdoc.CanonicalAction writes
	// "Identifier": null and "Type": null where TypeScript's canonicalAction
	// leaves the undefined keys out, and flowdoc.AutoLayout skips a
	// non-string Identifier where TypeScript's autoLayout places it.
	"odd":    "non-object actions: flowdoc.CanonicalAction and flowdoc.AutoLayout",
	"spread": "non-object actions: flowdoc.CanonicalAction and flowdoc.AutoLayout",
}

func loadOracle(t *testing.T) oracle {
	t.Helper()
	b, err := os.ReadFile("testdata/ts-oracle.json")
	if err != nil {
		t.Fatal(err)
	}
	var o oracle
	if err := json.Unmarshal(b, &o); err != nil {
		t.Fatal(err)
	}
	return o
}

func TestOracleParseConnectArn(t *testing.T) {
	o := loadOracle(t)
	if len(o.ParseConnectArn) < 20 {
		t.Fatalf("only %d cases", len(o.ParseConnectArn))
	}
	for _, c := range o.ParseConnectArn {
		got, ok := ParseConnectArn(c.Input)
		var gotJSON any
		if ok {
			m := map[string]any{
				"partition":  got.Partition,
				"region":     got.Region,
				"account":    got.Account,
				"instanceId": got.InstanceID,
			}
			if got.HasResource {
				m["resourceType"] = got.ResourceType
				m["resourceId"] = got.ResourceID
			}
			if got.HasQualifier {
				m["qualifier"] = got.Qualifier
			}
			gotJSON = m
		}
		var want any
		if err := json.Unmarshal(c.Output, &want); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(gotJSON, want) {
			t.Errorf("ParseConnectArn(%q) = %v, want %v", c.Input, gotJSON, want)
		}
	}
}

func TestOracleLambdaAndNormalize(t *testing.T) {
	o := loadOracle(t)
	for _, c := range o.ParseLambdaFunctionArn {
		got, ok := ParseLambdaFunctionArn(c.Input)
		if ok != (c.Output != nil) || (ok && got != *c.Output) {
			t.Errorf("ParseLambdaFunctionArn(%q) = %q %v, want %v", c.Input, got, ok, c.Output)
		}
	}
	for _, c := range o.NormalizeArn {
		if got := NormalizeArn(c.Input); got != c.Output {
			t.Errorf("NormalizeArn(%q) = %q, want %q", c.Input, got, c.Output)
		}
	}
}

func TestOracleSlugify(t *testing.T) {
	for _, c := range loadOracle(t).SlugifyResourceName {
		if got := SlugifyResourceName(c.Input); got != c.Output {
			t.Errorf("SlugifyResourceName(%q) = %q, want %q", c.Input, got, c.Output)
		}
	}
}

func dumpReverseMap(m ReverseMap) []oracleRef {
	out := []oracleRef{}
	for arn, e := range m.ByArn {
		out = append(out, oracleRef{Arn: arn, Token: e.Token, Type: e.Type, Name: e.Name, Alias: e.Alias})
	}
	sort.Slice(out, func(i, j int) bool { return jsonv.LessUTF16(out[i].Arn, out[j].Arn) })
	return out
}

func TestOracleReverseMaps(t *testing.T) {
	o := loadOracle(t)
	for i, c := range o.BuildReverseMap {
		var inv InstanceInventory
		if err := json.Unmarshal([]byte(c.Inventory), &inv); err != nil {
			t.Fatal(err)
		}
		m := BuildReverseMap(inv)
		if got := dumpReverseMap(m); !reflect.DeepEqual(got, c.ByArn) {
			t.Errorf("case %d byArn:\n got %+v\nwant %+v", i, got, c.ByArn)
		}
		if !reflect.DeepEqual(m.Warnings, c.Warnings) {
			t.Errorf("case %d warnings:\n got %q\nwant %q", i, m.Warnings, c.Warnings)
		}
	}
	for i, c := range o.ReverseMapOfResourceMap {
		m := ReverseMapOfResourceMap(c.Map)
		if got := dumpReverseMap(m); !reflect.DeepEqual(got, c.ByArn) {
			t.Errorf("resource map %d byArn:\n got %+v\nwant %+v", i, got, c.ByArn)
		}
		if !reflect.DeepEqual(m.Warnings, c.Warnings) {
			t.Errorf("resource map %d warnings:\n got %q\nwant %q", i, m.Warnings, c.Warnings)
		}
	}
}

// flowOptions reads the TypeScript's ExportFlowOptions object.
func flowOptions(t *testing.T, raw json.RawMessage) ExportFlowOptions {
	t.Helper()
	v, err := jsonv.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	o := v.(jsonv.Object)
	str := func(k string) string { s, _ := o.Get(k); r, _ := s.(string); return r }
	opts := ExportFlowOptions{
		Name:        str("name"),
		ConnectType: str("connectType"),
		Kind:        str("kind"),
		Description: str("description"),
	}
	if g, ok := o.Get("generator"); ok {
		s := g.(string)
		opts.Generator = &s
	}
	if m, ok := o.Get("meta"); ok {
		opts.Meta = m.(jsonv.Object)
	}
	if im, ok := o.Get("includeMeta"); ok && im == false {
		opts.OmitMeta = true
	}
	return opts
}

func checkError(t *testing.T, label string, err error, want *oracleError) {
	t.Helper()
	if err == nil {
		t.Errorf("%s: no error, want %q", label, want.Message)
		return
	}
	if err.Error() != want.Message {
		t.Errorf("%s: error\n got %q\nwant %q", label, err.Error(), want.Message)
	}
	var exportErr *ExportError
	isExport := errors.As(err, &exportErr)
	if isExport != (want.Name == "ExportError") {
		t.Errorf("%s: *ExportError %v, TypeScript %s", label, isExport, want.Name)
		return
	}
	if !isExport {
		return
	}
	if !reflect.DeepEqual(nonNil(exportErr.UnknownArns), nonNil(want.UnknownArns)) ||
		!reflect.DeepEqual(nonNil(exportErr.InterpolatedArns), nonNil(want.InterpolatedArns)) {
		t.Errorf("%s: arns %q %q, want %q %q", label, exportErr.UnknownArns, exportErr.InterpolatedArns,
			want.UnknownArns, want.InterpolatedArns)
	}
	if !reflect.DeepEqual(exportErr.Locations, want.Locations) {
		t.Errorf("%s: locations\n got %q\nwant %q", label, exportErr.Locations, want.Locations)
	}
	if exportErr.Resource != want.Resource {
		t.Errorf("%s: resource %q, want %q", label, exportErr.Resource, want.Resource)
	}
}

func nonNil(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	return s
}

func TestOracleExportFlow(t *testing.T) {
	o := loadOracle(t)
	if len(o.ExportFlow) < 20 {
		t.Fatalf("only %d cases", len(o.ExportFlow))
	}
	for i, c := range o.ExportFlow {
		var reverseMap ReverseMap
		if c.ResourceMap != nil {
			reverseMap = ReverseMapOfResourceMap(c.ResourceMap)
		} else {
			var inv InstanceInventory
			if err := json.Unmarshal([]byte(c.Inv), &inv); err != nil {
				t.Fatal(err)
			}
			reverseMap = BuildReverseMap(inv)
		}
		opts := flowOptions(t, c.Options)
		label := opts.Name + " #" + itoa(i)
		doc, err := ExportFlow(c.Content, reverseMap, opts)
		if c.Result.Error != nil {
			checkError(t, label, err, c.Result.Error)
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", label, err)
			continue
		}
		refs, _ := doc.Get("refs")
		if got := string(jsonv.Encode(refs, "")); got != *c.Result.Refs {
			t.Errorf("%s: refs\n got %s\nwant %s", label, got, *c.Result.Refs)
		}
		got := string(flowdoc.Serialize(doc))
		if reason, known := knownDeviations[opts.Name]; known {
			if got == *c.Result.Doc {
				t.Errorf("%s: matches TypeScript now; drop its knownDeviations entry (%s)", label, reason)
			}
			continue
		}
		if got != *c.Result.Doc {
			t.Errorf("%s:\n got %s\nwant %s", label, got, *c.Result.Doc)
		}
	}
}

// scriptedClient replays the oracle's exportInstance client: flows keyed by
// id, a draft flow refusing a describe without $SAVED.
type scriptedClient struct {
	inventory InstanceInventory
	flows     map[string]struct {
		Content string         `json:"content"`
		Name    *string        `json:"name"`
		Draft   bool           `json:"draft"`
		Extra   map[string]any `json:"extra"`
	}
	calls []string
}

func (c *scriptedClient) describe(id string) (DescribedContactFlowModule, error) {
	c.calls = append(c.calls, id)
	saved := strings.HasSuffix(id, ":$SAVED")
	base := strings.TrimSuffix(id, ":$SAVED")
	f, ok := c.flows[base]
	if !ok {
		return DescribedContactFlowModule{}, &apiError{"ResourceNotFoundException", "No fixture for " + id}
	}
	if f.Draft && !saved {
		return DescribedContactFlowModule{}, &apiError{"ContactFlowNotPublishedException", "Flow " + base + " has not been published."}
	}
	name := ""
	if f.Name != nil {
		name = *f.Name
	}
	d := DescribedContactFlowModule{DescribedContactFlow: DescribedContactFlow{ID: strings.Replace(id, ":$SAVED", "", 1), Name: name, Content: f.Content}}
	s := func(k string) *string {
		if v, ok := f.Extra[k].(string); ok {
			return &v
		}
		return nil
	}
	d.Description, d.Version, d.ContentSha256, d.Settings = s("description"), s("version"), s("contentSha256"), s("settings")
	if v, ok := f.Extra["externalInvocationEnabled"].(bool); ok {
		d.ExternalInvocationEnabled = &v
	}
	return d, nil
}

func (c *scriptedClient) ListContactFlows(context.Context, []string) ([]ContactFlowSummary, error) {
	return c.inventory.ContactFlows, nil
}
func (c *scriptedClient) DescribeContactFlow(_ context.Context, id string) (DescribedContactFlow, error) {
	d, err := c.describe(id)
	return d.DescribedContactFlow, err
}
func (c *scriptedClient) ListContactFlowModules(context.Context) ([]ContactFlowModuleSummary, error) {
	return c.inventory.ContactFlowModules, nil
}
func (c *scriptedClient) DescribeContactFlowModule(_ context.Context, id string) (DescribedContactFlowModule, error) {
	return c.describe(id)
}
func (c *scriptedClient) ListQueues(context.Context) ([]ResourceSummary, error) {
	return c.inventory.Queues, nil
}
func (c *scriptedClient) ListHoursOfOperations(context.Context) ([]ResourceSummary, error) {
	return c.inventory.HoursOfOperations, nil
}
func (c *scriptedClient) ListPrompts(context.Context) ([]ResourceSummary, error) {
	return c.inventory.Prompts, nil
}
func (c *scriptedClient) ListLambdaFunctions(context.Context) ([]string, error) {
	return c.inventory.LambdaFunctions, nil
}
func (c *scriptedClient) ListBots(context.Context) ([]LexBotSummary, error) {
	return c.inventory.LexBots, nil
}
func (c *scriptedClient) ListViews(context.Context) ([]ViewSummary, error) {
	return c.inventory.Views, nil
}

func TestOracleExportInstance(t *testing.T) {
	o := loadOracle(t)
	if len(o.ExportInstance) < 4 {
		t.Fatalf("only %d cases", len(o.ExportInstance))
	}
	for i, c := range o.ExportInstance {
		client := &scriptedClient{}
		var spec struct {
			Inventory json.RawMessage `json:"inventory"`
			Flows     json.RawMessage `json:"flows"`
		}
		if err := json.Unmarshal([]byte(c.Spec), &spec); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(spec.Inventory, &client.inventory); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(spec.Flows, &client.flows); err != nil {
			t.Fatal(err)
		}
		var raw struct {
			OnError        string  `json:"onError"`
			SavedFallback  *bool   `json:"savedFallback"`
			Generator      *string `json:"generator"`
			IncludeModules *bool   `json:"includeModules"`
		}
		if err := json.Unmarshal(c.Options, &raw); err != nil {
			t.Fatal(err)
		}
		opts := ExportInstanceOptions{
			CollectFailures: raw.OnError == "collect",
			NoSavedFallback: raw.SavedFallback != nil && !*raw.SavedFallback,
			Generator:       raw.Generator,
		}
		opts.ExcludeModules = raw.IncludeModules != nil && !*raw.IncludeModules
		label := "instance #" + itoa(i) + " " + string(c.Options)

		result, err := ExportInstance(context.Background(), client, opts)
		if !reflect.DeepEqual(client.calls, c.Calls) {
			t.Errorf("%s: describe calls\n got %q\nwant %q", label, client.calls, c.Calls)
		}
		if c.Result.Error != nil {
			checkError(t, label, err, c.Result.Error)
			continue
		}
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		if !reflect.DeepEqual(result.Warnings, c.Result.Warnings) {
			t.Errorf("%s: warnings\n got %q\nwant %q", label, result.Warnings, c.Result.Warnings)
		}
		if len(result.Failures) != len(c.Result.Failures) {
			t.Fatalf("%s: %d failures, want %d: %+v", label, len(result.Failures), len(c.Result.Failures), result.Failures)
		}
		for j, f := range c.Result.Failures {
			g := result.Failures[j]
			if g.Arn != f.Arn || g.Name != f.Name || g.Reason != f.Reason || !reflect.DeepEqual(g.UnknownArns, f.UnknownArns) {
				t.Errorf("%s: failure %d\n got %+v\nwant %+v", label, j, g, f)
			}
		}
		if len(result.Flows) != len(c.Result.Flows) {
			t.Fatalf("%s: %d flows, want %d", label, len(result.Flows), len(c.Result.Flows))
		}
		for j, f := range c.Result.Flows {
			g := result.Flows[j]
			if g.Arn != f.Arn || g.ID != f.ID || g.SourceName != f.SourceName || g.Saved != f.Saved {
				t.Errorf("%s: flow %d\n got %s %s %s %v\nwant %+v", label, j, g.Arn, g.ID, g.SourceName, g.Saved, f)
			}
			if got := flowdoc.Serialize(g.Doc); !bytes.Equal(got, []byte(f.Doc)) {
				t.Errorf("%s: flow %d doc\n got %s\nwant %s", label, j, got, f.Doc)
			}
		}
	}
}
