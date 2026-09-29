## What changes

## How it was shown to fail

Every guarantee lands with a test that was seen to fail: name what you broke,
the test that went red, and that it passed again once restored.

- [ ] `go test ./...` passes
- [ ] `go generate ./...` leaves no diff (registry docs)
- [ ] `internal/conformance/` untouched, or re-synced with `scripts/sync-conformance.sh`
- [ ] CHANGELOG.md "Unreleased" updated for a user-visible change
