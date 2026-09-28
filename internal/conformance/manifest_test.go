// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package conformance

import (
	"bytes"
	"os"
	"regexp"
	"testing"
)

// The vendored tree is exactly what the sync wrote: no file edited, added or
// removed since. Offline; the drift against upstream main is the canary's job.
func TestVendoredTreeMatchesItsManifest(t *testing.T) {
	want, err := os.ReadFile("MANIFEST.json")
	if err != nil {
		t.Fatal(err)
	}
	got, err := Compute(FS())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < 100 {
		t.Fatalf("only %d vendored files; is data/ populated?", len(got))
	}
	if !bytes.Equal(got.JSON(), want) {
		t.Fatal("internal/conformance/data differs from MANIFEST.json; never edit it here, re-run scripts/sync-conformance.sh")
	}
}

func TestCommitIsAFullSHA(t *testing.T) {
	if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(Commit()) {
		t.Fatalf("COMMIT is %q", Commit())
	}
}
