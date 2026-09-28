// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

// Command headers applies (-fix) or verifies the Apache-2.0 header on every Go
// file in the repository. The copyright holder is written in one place, the
// Holder constant, and is entity-neutral on purpose: the project's legal
// entity is expected to change, and no file should need editing when it does.
package main

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Holder is the copyright holder named in every header.
const Holder = "The flow-as-code Authors"

// Year is the year the headers carry.
const Year = "2026"

// Header is the exact text every Go file starts with.
var Header = fmt.Sprintf("// Copyright %s %s\n// SPDX-License-Identifier: Apache-2.0\n", Year, Holder)

var skip = map[string]bool{".git": true, "vendor": true, "dist": true, "node_modules": true}

func main() {
	fix := flag.Bool("fix", false, "write the header into files that lack it")
	flag.Parse()
	root := "."
	if flag.NArg() > 0 {
		root = flag.Arg(0)
	}
	missing, err := Check(root, *fix)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(missing) > 0 && !*fix {
		for _, m := range missing {
			fmt.Fprintf(os.Stderr, "missing license header: %s\n", m)
		}
		os.Exit(1)
	}
}

// Check returns the Go files under root that do not start with Header, and
// with fix set, prepends it to each.
func Check(root string, fix bool) ([]string, error) {
	var missing []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skip[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if strings.HasPrefix(string(b), Header) {
			return nil
		}
		missing = append(missing, p)
		if fix {
			return os.WriteFile(p, []byte(Header+"\n"+string(b)), 0o644)
		}
		return nil
	})
	return missing, err
}
