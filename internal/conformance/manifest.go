// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

// Package conformance is flow-as-code's conformance/ directory, vendored at
// the commit named in COMMIT and embedded, so every runner reads the exact
// fixtures the TypeScript side passes. scripts/sync-conformance.sh is the only
// thing that writes it.
package conformance

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"sort"
	"strings"
)

//go:embed all:data
var data embed.FS

//go:embed COMMIT
var commit string

// FS is the vendored conformance/ tree, rooted at its top.
func FS() fs.FS {
	sub, err := fs.Sub(data, "data")
	if err != nil {
		panic(err)
	}
	return sub
}

// Commit is the flow-as-code commit the tree was vendored from.
func Commit() string { return strings.TrimSpace(commit) }

// Manifest maps every file's slash path to the hex sha256 of its bytes.
type Manifest map[string]string

// Compute hashes every file under root.
func Compute(root fs.FS) (Manifest, error) {
	out := Manifest{}
	err := fs.WalkDir(root, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(root, p)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		out[p] = hex.EncodeToString(sum[:])
		return nil
	})
	return out, err
}

// JSON is the manifest as MANIFEST.json holds it: keys sorted, two-space
// indent, newline terminated.
func (m Manifest) JSON() []byte {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("{\n")
	for i, k := range keys {
		kb, _ := json.Marshal(k)
		b.WriteString("  " + string(kb) + ": \"" + m[k] + "\"")
		if i < len(keys)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("}\n")
	return []byte(b.String())
}
