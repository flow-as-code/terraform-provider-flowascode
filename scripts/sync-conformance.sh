#!/usr/bin/env bash
# Copyright 2026 The flow-as-code Authors
# SPDX-License-Identifier: Apache-2.0
#
# Vendors flow-as-code's conformance/ directory at one commit into
# internal/conformance/data, and records the commit (COMMIT) and a sha256 per
# file (MANIFEST.json), which manifest_test.go recomputes offline. The
# directory is never edited here: a fixture that looks wrong is fixed upstream
# and re-synced.
#
#   scripts/sync-conformance.sh <sha>                 from github.com/flow-as-code/flow-as-code
#   scripts/sync-conformance.sh <sha> --from <clone>  from a local clone (unpushed commits)
set -euo pipefail

sha="${1:?usage: sync-conformance.sh <sha> [--from <clone>]}"
from=""
if [ "${2:-}" = "--from" ]; then from="${3:?--from needs a path}"; fi
case "$sha" in
  *[!0-9a-f]* | "") echo "sha must be a full lowercase hex commit id" >&2; exit 1 ;;
esac
if [ "${#sha}" -ne 40 ]; then echo "sha must be the full 40-character commit id" >&2; exit 1; fi

here="$(cd "$(dirname "$0")/.." && pwd)"
dest="$here/internal/conformance/data"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

if [ -n "$from" ]; then
  git -C "$from" archive --format=tar "$sha" conformance | tar -x -C "$work"
else
  gh api "repos/flow-as-code/flow-as-code/tarball/$sha" > "$work/src.tgz"
  tar -xzf "$work/src.tgz" -C "$work" --strip-components=1 --wildcards '*/conformance/*'
fi

rm -rf "$dest"
mv "$work/conformance" "$dest"
printf '%s\n' "$sha" > "$here/internal/conformance/COMMIT"
go run "$here/internal/conformance/cmd/manifest" "$dest" > "$here/internal/conformance/MANIFEST.json"
echo "vendored conformance/ at $sha"
