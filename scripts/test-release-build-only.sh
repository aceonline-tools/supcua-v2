#!/usr/bin/env bash
# Proves `release.sh` builds the exe and makes no publish call:
# git, gh, aws and curl are shadowed by stubs that log and fail.
# Usage: scripts/test-release-build-only.sh [--plain]
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
mkdir -p "$TMP/stubs" "$TMP/out"
: > "$TMP/calls.log"
for tool in git gh aws curl; do
  printf '#!/bin/sh\necho "%s $*" >> "%s"\nexit 97\n' "$tool" "$TMP/calls.log" > "$TMP/stubs/$tool"
  chmod +x "$TMP/stubs/$tool"
done

fail() { echo "FAIL: $*" >&2; exit 1; }

cd "$ROOT"
PATH="$TMP/stubs:$PATH" bash release.sh 0.0.0-test "$@" --out "$TMP/out"
[ -s "$TMP/out/supcua-v2.exe" ] || fail "exe not produced"
[ ! -s "$TMP/calls.log" ] || fail "publish/VCS tools were called: $(cat "$TMP/calls.log")"

echo "PASS: release.sh built $(wc -c < "$TMP/out/supcua-v2.exe" | tr -d ' ') bytes, 0 stub calls"
