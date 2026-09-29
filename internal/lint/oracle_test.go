// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package lint

import (
	"encoding/json"
	"os"
	"sort"
	"testing"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/jsonv"
)

// testdata/ts-oracle.json is @flow-as-code/core's own output, recorded by
// running packages/core/dist (built from flow-as-code e414239; lint is
// unchanged since) under Node 26.8.1, ICU 78.3, locale en-US:
//
//   - fixtures: toJson, toText and hasBlockingFindings of lint over every
//     conformance/lint fixture and the demo, every rule, not only the
//     fixture's own, so message bytes and the full sort are held;
//   - cases: hand-built sets aimed at the sort, stable ties, JavaScript key
//     order, odd transition values, JavaScript whitespace, UTF-16 lengths,
//     module walks, names and the engine options, with the error message
//     where lint throws;
//   - collation: 4,000 localeCompare signs over ASCII strings and a pool
//     sorted with it.
//
// Inputs are JSON text, so both sides parse the same bytes. testdata/lint-oracle.mjs
// regenerates the file.
type oracle struct {
	Fixtures []struct {
		File     string `json:"file"`
		JSON     string `json:"json"`
		Text     string `json:"text"`
		Blocking bool   `json:"blocking"`
		Error    string `json:"error"`
	} `json:"fixtures"`
	Cases []struct {
		Name     string   `json:"name"`
		Input    string   `json:"input"`
		Disable  []string `json:"disable"`
		Rules    []string `json:"rules"`
		JSON     string   `json:"json"`
		Text     string   `json:"text"`
		Blocking bool     `json:"blocking"`
		Error    string   `json:"error"`
	} `json:"cases"`
	Collation struct {
		Pairs  [][3]any `json:"pairs"`
		Pool   []string `json:"pool"`
		Sorted []string `json:"sorted"`
	} `json:"collation"`
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

func TestOracleFixtures(t *testing.T) {
	o := loadOracle(t)
	if len(o.Fixtures) != 73 {
		t.Fatalf("oracle holds %d fixtures, want the 72 lint fixtures and the demo", len(o.Fixtures))
	}
	for _, fx := range o.Fixtures {
		parsed := readJSON(t, fx.File)
		docs := docsOf(parsed)
		if fx.File == "demo/appointment-line.flowdoc.json" {
			docs = []any{parsed}
		}
		found, err := Lint(docs, Options{})
		if err != nil {
			t.Errorf("%s: %v", fx.File, err)
			continue
		}
		if got := ToJSON(found); got != fx.JSON {
			t.Errorf("%s: toJson\n got %s\nwant %s", fx.File, got, fx.JSON)
		}
		if got := ToText(found); got != fx.Text {
			t.Errorf("%s: toText\n got %s\nwant %s", fx.File, got, fx.Text)
		}
		if got := HasBlockingFindings(found, nil); got != fx.Blocking {
			t.Errorf("%s: hasBlockingFindings %v, want %v", fx.File, got, fx.Blocking)
		}
	}
}

func TestOracleCases(t *testing.T) {
	o := loadOracle(t)
	if len(o.Cases) < 20 {
		t.Fatalf("oracle holds %d cases", len(o.Cases))
	}
	for _, c := range o.Cases {
		t.Run(c.Name, func(t *testing.T) {
			v, err := jsonv.Decode([]byte(c.Input))
			if err != nil {
				t.Fatal(err)
			}
			opts := Options{Disable: c.Disable}
			if c.Rules != nil {
				// allRules.filter(r => rules.includes(r.id)): registry order.
				opts.Rules = []Rule{}
				for _, r := range AllRules() {
					if contains(c.Rules, r.ID) {
						opts.Rules = append(opts.Rules, r)
					}
				}
			}
			found, err := Lint(v.([]any), opts)
			if c.Error != "" {
				if err == nil || err.Error() != c.Error {
					t.Fatalf("error %v, want %q", err, c.Error)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := ToJSON(found); got != c.JSON {
				t.Errorf("toJson\n got %s\nwant %s", got, c.JSON)
			}
			if got := ToText(found); got != c.Text {
				t.Errorf("toText\n got %s\nwant %s", got, c.Text)
			}
			if got := HasBlockingFindings(found, nil); got != c.Blocking {
				t.Errorf("hasBlockingFindings %v, want %v", got, c.Blocking)
			}
		})
	}
}

// The comparator fixture: localeCompare over ASCII, sign for sign, and a
// stable sort with it reproducing Array.prototype.sort's order.
func TestOracleCollation(t *testing.T) {
	o := loadOracle(t)
	if len(o.Collation.Pairs) != 4000 {
		t.Fatalf("oracle holds %d pairs", len(o.Collation.Pairs))
	}
	bytewise := 0
	for _, p := range o.Collation.Pairs {
		a, b, want := p[0].(string), p[1].(string), int(p[2].(float64))
		if got := localeCompare(a, b); got != want {
			t.Errorf("localeCompare(%q, %q) = %d, want %d", a, b, got, want)
		}
		if sign(stringsCompare(a, b)) != want {
			bytewise++
		}
	}
	// The fixture only proves something if byte order would fail it.
	if bytewise < 1000 {
		t.Errorf("only %d pairs disagree with byte order", bytewise)
	}
	pool := append([]string(nil), o.Collation.Pool...)
	sort.SliceStable(pool, func(i, j int) bool { return localeCompare(pool[i], pool[j]) < 0 })
	for i := range pool {
		if pool[i] != o.Collation.Sorted[i] {
			t.Fatalf("sorted pool differs at %d: %q, want %q", i, pool[i], o.Collation.Sorted[i])
		}
	}
}

func stringsCompare(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	}
	return 0
}
