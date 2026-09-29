# Contributing

## Ground rules

- Every guarantee lands with a test that has been shown to fail: break the
  code the test guards, watch the test go red, restore it, and say so in the
  pull request.
- `internal/conformance/` is vendored from flow-as-code and never edited here.
  A fixture that looks wrong is fixed upstream and re-synced.
- Every Go file starts with the Apache-2.0 header; `make headers` applies it.
  The copyright holder lives in one constant in `tools/headers/main.go`.
- Every third-party action in `.github/workflows/` is pinned to a full commit
  SHA with its version in a trailing comment.
- Prose uses no em-dashes.
- Conventional commits, small and self-contained, docs in the same commit.

## Running the tests

```
go test ./...
```

The conformance and plan tests drive a real CLI with the provider served in
process: they use `tofu` from `PATH` (or `terraform` when there is no
`tofu`), or the binary `TF_ACC_TERRAFORM_PATH` names.

The acceptance lane (`TF_ACC=1`) runs against a live Amazon Connect sandbox and
gates every pull request; a fork's pull request cannot reach it, so a
maintainer re-runs it from a branch in this repository.

## Using a local build

Build and install the binary, then point Terraform or OpenTofu at it with a
development override in `~/.terraformrc` or `~/.tofurc`:

```
go install .
```

```hcl
provider_installation {
  dev_overrides {
    "flow-as-code/flowascode" = "/Users/you/go/bin"
  }
  direct {}
}
```

With an override in place, skip `init` for this provider and run `plan` and
`apply` directly; the CLI prints a warning naming the override.
