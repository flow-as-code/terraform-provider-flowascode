// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

// Package tools holds the generate step: the registry documentation under
// docs/ is written by tfplugindocs from the schema, templates/ and examples/,
// and CI fails when it is not current.
package tools

//go:generate go tool tfplugindocs generate --provider-dir .. --provider-name flowascode
