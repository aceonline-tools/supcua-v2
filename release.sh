#!/usr/bin/env bash
# Local build-only helper. Releases are made by release-please (see CONTRIBUTING.md):
# merge the release PR and CI builds and attaches supcua-v2.exe to the GitHub Release.
# Usage: ./release.sh [version]   (version without leading v; default "dev")
set -e

VERSION="${1:-dev}"
EXE="supcua-v2.exe"

echo "==> Regenerating icon resource..."
rsrc -ico app.ico -manifest main.manifest -o rsrc.syso

echo "==> Building $EXE ($VERSION)..."
GOROOT=$(go1.25.0 env GOROOT) PATH="$(go1.25.0 env GOROOT)/bin:$PATH" \
  GOGARBLE="github.com/yanmad27/supcua-v2" \
  garble -literals build -trimpath -ldflags="-H windowsgui -s -w -X github.com/yanmad27/supcua-v2/version.Version=$VERSION" -o "$EXE" .

echo "==> Done: $EXE"
