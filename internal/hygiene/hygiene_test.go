// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

// Package hygiene holds the repository-wide rules that are not about any one
// package: license headers, no em-dashes, and SHA-pinned workflow actions.
package hygiene

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const root = "../.."

// header is tools/headers' Header, restated so this test has no build-time
// link to a main package; TestHeaderMatchesTheTool keeps the two equal.
const header = "// Copyright 2026 The flow-as-code Authors\n// SPDX-License-Identifier: Apache-2.0\n"

var skipDirs = map[string]bool{".git": true, "dist": true, "vendor": true, "node_modules": true}

func walk(t *testing.T, visit func(path string, body string)) {
	t.Helper()
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		visit(p, string(b))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestEveryGoFileCarriesTheHeader(t *testing.T) {
	n := 0
	walk(t, func(p, body string) {
		if !strings.HasSuffix(p, ".go") {
			return
		}
		n++
		if !strings.HasPrefix(body, header) {
			t.Errorf("%s: missing the Apache-2.0 header (go run ./tools/headers -fix)", p)
		}
	})
	if n < 5 {
		t.Fatalf("found only %d Go files; is the walk rooted right?", n)
	}
}

func TestHeaderMatchesTheTool(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(root, "tools", "headers", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, want := range []string{`const Holder = "The flow-as-code Authors"`, `const Year = "2026"`} {
		if !strings.Contains(src, want) {
			t.Errorf("tools/headers/main.go no longer declares %s; update this test's header too", want)
		}
	}
}

// Prose in this repository uses no em-dash (the project's writing rule).
func TestNoEmDash(t *testing.T) {
	walk(t, func(p, body string) {
		if strings.HasSuffix(p, ".sum") || strings.Contains(p, string(filepath.Separator)+"conformance"+string(filepath.Separator)) {
			return
		}
		if strings.ContainsRune(body, '\u2014') {
			t.Errorf("%s: contains an em-dash", p)
		}
	})
}

var usesLine = regexp.MustCompile(`^\s*-?\s*uses:\s*(\S+)(.*)$`)
var pinned = regexp.MustCompile(`^[^@\s]+@[0-9a-f]{40}$`)
var versionComment = regexp.MustCompile(`^\s+#\s*v\d+(\.\d+)*\S*\s*$`)

// Every action from outside this repository is pinned to a full commit SHA
// with its version in a trailing comment, which Dependabot keeps current.
func TestWorkflowActionsArePinned(t *testing.T) {
	n := 0
	walk(t, func(p, body string) {
		if !strings.Contains(p, filepath.Join(".github", "workflows")) || !strings.HasSuffix(p, ".yml") {
			return
		}
		for i, line := range strings.Split(body, "\n") {
			m := usesLine.FindStringSubmatch(line)
			if m == nil || strings.HasPrefix(m[1], "./") {
				continue
			}
			n++
			if !pinned.MatchString(m[1]) || !versionComment.MatchString(m[2]) {
				t.Errorf("%s:%d: %q is not pinned to a full SHA with a # vX.Y.Z comment", p, i+1, strings.TrimSpace(line))
			}
		}
	})
	if n == 0 {
		t.Fatal("no workflow actions found; is the walk rooted right?")
	}
}
