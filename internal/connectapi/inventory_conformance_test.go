// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package connectapi

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

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/connect/types"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/conformance"
	"github.com/flow-as-code/terraform-provider-flowascode/internal/export"
	"github.com/flow-as-code/terraform-provider-flowascode/internal/flowdoc"
)

// The conformance/export cases, replayed through the SDK seam instead of a
// fixture client: each case's inventory.json and flows/ seed the Fake, and
// the adapter lists and describes them the way it would a live instance.
// That the result is the recorded inventory, and that ExportInstance over it
// reproduces the recorded goldens, is what the adapter owes package export.

func readConformance(t *testing.T, name string) []byte {
	t.Helper()
	b, err := fs.ReadFile(conformance.FS(), name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// seedFromCase builds a Fake holding what a case's inventory.json lists. A
// flow with only a flows/<id>.saved.json is marked never published.
func seedFromCase(t *testing.T, name string) (*Fake, export.InstanceInventory) {
	t.Helper()
	var inv export.InstanceInventory
	if err := json.Unmarshal(readConformance(t, "export/"+name+"/inventory.json"), &inv); err != nil {
		t.Fatal(err)
	}
	content := func(id string) (string, bool) {
		dir := "export/" + name + "/flows/"
		if b, err := fs.ReadFile(conformance.FS(), dir+id+".json"); err == nil {
			return string(b), true
		}
		if b, err := fs.ReadFile(conformance.FS(), dir+id+".saved.json"); err == nil {
			return string(b), false
		}
		return "", true
	}

	f := NewFake()
	for _, s := range inv.ContactFlows {
		id := aws.ToString(s.ID)
		body, published := content(id)
		f.PutContactFlow(types.ContactFlow{
			Arn: aws.String(s.Arn), Id: s.ID, Name: aws.String(s.Name),
			Type:    types.ContactFlowType(aws.ToString(s.ContactFlowType)),
			State:   types.ContactFlowState(aws.ToString(s.ContactFlowState)),
			Status:  types.ContactFlowStatus(aws.ToString(s.ContactFlowStatus)),
			Content: aws.String(body),
		})
		if !published {
			f.MarkNeverPublished(id)
		}
	}
	for _, s := range inv.ContactFlowModules {
		body, _ := content(aws.ToString(s.ID))
		f.PutContactFlowModule(types.ContactFlowModule{
			Arn: aws.String(s.Arn), Id: s.ID, Name: aws.String(s.Name),
			State: types.ContactFlowModuleState(aws.ToString(s.State)), Content: aws.String(body),
		})
	}
	for _, a := range inv.ModuleAliases {
		f.PutContactFlowModuleAlias(a.ModuleArn[strings.LastIndex(a.ModuleArn, "/")+1:], a.AliasID, a.Name)
	}
	var queues []types.QueueSummary
	for _, s := range inv.Queues {
		queues = append(queues, types.QueueSummary{Arn: aws.String(s.Arn), Id: s.ID, Name: aws.String(s.Name), QueueType: types.QueueTypeStandard})
	}
	f.SetQueues(queues)
	var hours []types.HoursOfOperationSummary
	for _, s := range inv.HoursOfOperations {
		hours = append(hours, types.HoursOfOperationSummary{Arn: aws.String(s.Arn), Id: s.ID, Name: aws.String(s.Name)})
	}
	f.SetHoursOfOperations(hours)
	var prompts []types.PromptSummary
	for _, s := range inv.Prompts {
		prompts = append(prompts, types.PromptSummary{Arn: aws.String(s.Arn), Id: s.ID, Name: aws.String(s.Name)})
	}
	f.SetPrompts(prompts)
	f.SetLambdaFunctions(inv.LambdaFunctions)
	var bots []types.LexBotConfig
	for _, b := range inv.LexBots {
		if b.LexVersion == "V2" {
			bots = append(bots, types.LexBotConfig{LexV2Bot: &types.LexV2Bot{AliasArn: b.AliasArn}})
		} else {
			bots = append(bots, types.LexBotConfig{LexBot: &types.LexBot{Name: b.Name, LexRegion: b.LexRegion}})
		}
	}
	f.SetBots(bots)
	var views []types.ViewSummary
	for _, v := range inv.Views {
		views = append(views, types.ViewSummary{
			Arn: aws.String(v.Arn), Id: aws.String(v.ID), Name: aws.String(v.Name),
			Type: types.ViewType(v.Type), Status: types.ViewStatus(aws.ToString(v.Status)),
		})
	}
	f.SetViews(views)
	return f, inv
}

func exportCaseNames(t *testing.T) []string {
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

// Collecting over the adapter gives back the recorded inventory, but for the
// order of lexBots: the adapter lists V1 before V2, as export.ts's does, and
// a fixture may record them the other way round.
func TestCollectInventoryOverAdapterIsTheRecordedInventory(t *testing.T) {
	for _, name := range exportCaseNames(t) {
		t.Run(name, func(t *testing.T) {
			f, want := seedFromCase(t, name)
			sort.SliceStable(want.LexBots, func(i, j int) bool { return want.LexBots[i].LexVersion < want.LexBots[j].LexVersion })
			client := NewInventoryWithOptions(&spy{Fake: f}, testInstanceID, InventoryOptions{MaxResults: 1, RequestsPerSecond: noLimit()})
			got, err := export.CollectInventory(context.Background(), client, export.CollectInventoryOptions{})
			if err != nil {
				t.Fatal(err)
			}
			gotJSON, _ := json.MarshalIndent(got, "", "  ")
			wantJSON, _ := json.MarshalIndent(want, "", "  ")
			if !bytes.Equal(gotJSON, wantJSON) {
				t.Errorf("inventory differs:\n%s\n---\n%s", gotJSON, wantJSON)
			}
		})
	}
}

// ExportInstance over the adapter reproduces each case's goldens byte for
// byte, reading a never-published flow through `:$SAVED`, or fails with the
// recorded unknown ARNs.
func TestExportInstanceOverAdapterMatchesGoldens(t *testing.T) {
	for _, name := range exportCaseNames(t) {
		t.Run(name, func(t *testing.T) {
			f, _ := seedFromCase(t, name)
			client := NewInventoryWithOptions(&spy{Fake: f}, testInstanceID, InventoryOptions{RequestsPerSecond: noLimit()})
			ctx := context.Background()

			if b, err := fs.ReadFile(conformance.FS(), "export/"+name+"/expected-error.json"); err == nil {
				var expected struct {
					UnknownArns []string `json:"unknownArns"`
				}
				if err := json.Unmarshal(b, &expected); err != nil {
					t.Fatal(err)
				}
				_, err := export.ExportInstance(ctx, client, export.ExportInstanceOptions{})
				var exportErr *export.ExportError
				if !errors.As(err, &exportErr) {
					t.Fatalf("got %v, want an *export.ExportError", err)
				}
				if !reflect.DeepEqual(exportErr.UnknownArns, expected.UnknownArns) {
					t.Errorf("unknownArns %q, want %q", exportErr.UnknownArns, expected.UnknownArns)
				}
				return
			}

			result, err := export.ExportInstance(ctx, client, export.ExportInstanceOptions{Generator: aws.String("core@0.2")})
			if err != nil {
				t.Fatal(err)
			}
			goldens, _ := fs.Glob(conformance.FS(), "export/"+name+"/expected/*.flowdoc.json")
			if len(result.Flows) != len(goldens) {
				t.Fatalf("exported %d flows, %d goldens", len(result.Flows), len(goldens))
			}
			for _, flow := range result.Flows {
				docName, _ := flow.Doc.Get("name")
				golden := readConformance(t, "export/"+name+"/expected/"+docName.(string)+".flowdoc.json")
				if got := flowdoc.Serialize(flow.Doc); !bytes.Equal(got, golden) {
					t.Errorf("%s differs from its golden:\n%s\n---\n%s", docName, got, golden)
				}
				if _, err := fs.Stat(conformance.FS(), "export/"+name+"/flows/"+flow.ID+".saved.json"); (err == nil) != flow.Saved {
					t.Errorf("%s: Saved is %v", docName, flow.Saved)
				}
			}
		})
	}
}
