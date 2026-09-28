// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"io/fs"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/conformance"
	"github.com/flow-as-code/terraform-provider-flowascode/internal/flowdoc"
	"github.com/flow-as-code/terraform-provider-flowascode/internal/flowmodel"
	"github.com/flow-as-code/terraform-provider-flowascode/internal/jsonv"
)

// The Go writer and reader are inverses: every vendored FlowDoc 0.2, written
// as action blocks (typed or generic), typed by the schema, decoded and read
// back, is its own view. This is what makes Read's reconstruction after
// drift or an import show the live flow faithfully.
func TestActionsFromDocReadsBackToTheDocument(t *testing.T) {
	ctx := context.Background()
	var resp resource.SchemaResponse
	(&flowResource{kind: "flow"}).Schema(ctx, resource.SchemaRequest{}, &resp)
	actionType, d := resp.Schema.TypeAtPath(ctx, path.Root("action"))
	if d.HasError() {
		t.Fatal(d)
	}
	tfType := actionType.TerraformType(ctx)
	n := 0
	err := fs.WalkDir(conformance.FS(), ".", func(p string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() || !strings.HasSuffix(p, ".json") {
			return err
		}
		b, _ := fs.ReadFile(conformance.FS(), p)
		v, err := jsonv.Decode(b)
		doc, ok := v.(jsonv.Object)
		if err != nil || !ok {
			return nil
		}
		if ver, _ := doc.Get("flowdoc"); ver != "0.2" {
			return nil
		}
		if _, ok := doc.Get("content"); !ok {
			return nil
		}
		n++
		t.Run(p, func(t *testing.T) {
			want := viewedFlowDoc(t, p)
			kind, _ := doc.Get("kind")
			actions := flowmodel.ActionsFromDoc(doc)
			tv, err := flowmodel.ToTerraform(actions, tfType)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := flowmodel.Decode(tv)
			if err != nil {
				t.Fatal(err)
			}
			name, _ := doc.Get("name")
			cfg := map[string]any{"name": name, "action": decoded}
			if kind == "flow" {
				cfg["type"], _ = doc.Get("connectType")
			} else {
				cv, _ := doc.Get("content")
				if s, ok := cv.(jsonv.Object).Get("Settings"); ok {
					cfg["settings"] = flowmodel.JSONEncode(s)
				}
			}
			if d, ok := doc.Get("description"); ok {
				cfg["description"] = d
			}
			cv, _ := doc.Get("content")
			if s, _ := cv.(jsonv.Object).Get("StartAction"); s != nil {
				cfg["start"] = s
			}
			res := flowmodel.FromConfig(cfg, kind.(string), flowmodel.PhasePlan)
			if len(res.Problems) > 0 || res.Doc == nil {
				t.Fatalf("read back refused: %+v", res.Problems)
			}
			if got := string(flowdoc.Serialize(res.Doc)); got != want {
				t.Errorf("read back differs\ngot:\n%s\nwant:\n%s", got, want)
			}
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n < 80 {
		t.Fatalf("only %d documents", n)
	}
}

// What the inverse cannot see, because the reader accepts both spellings: the
// writer uses a typed block wherever the catalog accepts the parameters, and
// the key form in a reference field.
func TestActionsFromDocWritesTypedBlocksAndKeys(t *testing.T) {
	v, err := jsonv.Decode(readVendored(t, "demo/appointment-line.flowdoc.json"))
	if err != nil {
		t.Fatal(err)
	}
	actions := flowmodel.ActionsFromDoc(v.(jsonv.Object))
	byID := map[string]map[string]any{}
	for _, a := range actions {
		m := a.(map[string]any)
		byID[m["id"].(string)] = m
	}
	hours, ok := byID["check-hours"]["check_hours_of_operation"].(map[string]any)
	if !ok {
		t.Fatalf("check-hours is not typed: %v", byID["check-hours"])
	}
	if got := hours["hours_of_operation_id"]; got != "hours:main-line" {
		t.Errorf("hours_of_operation_id = %v, want the key form", got)
	}
	if _, generic := byID["check-hours"]["generic"]; generic {
		t.Error("check-hours also has a generic block")
	}
}
