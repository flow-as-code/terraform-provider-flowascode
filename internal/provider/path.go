// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import "github.com/hashicorp/terraform-plugin-framework/path"

func pathOf(name string) path.Path { return path.Root(name) }
