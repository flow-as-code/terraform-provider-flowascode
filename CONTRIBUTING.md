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

The acceptance lane (`TF_ACC=1`) runs against a live Amazon Connect sandbox and
gates every pull request; a fork's pull request cannot reach it, so a
maintainer re-runs it from a branch in this repository.
