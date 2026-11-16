#!/usr/bin/env bash
# Build-only: cross-compiles the spike for Windows (plain + garble). Never tags,
# pushes or uploads. Tools (garble v0.15.0, rsrc v0.10.2) come from TOOLS_BIN, a
# job-local/temporary GOBIN; nothing is installed globally.
set -euo pipefail
cd "$(dirname "$0")"

TOOLS_BIN="${TOOLS_BIN:-${TMPDIR:-/tmp}/supcua-tools/bin}"
LDFLAGS="-H windowsgui -s -w"
export GOOS=windows GOARCH=amd64 CGO_ENABLED=0

"$TOOLS_BIN/rsrc" -ico ../app.ico -manifest spike.manifest -o rsrc.syso

mkdir -p out/plain out/garble
go build -trimpath -ldflags "$LDFLAGS" -o out/plain/spike.exe .
GOGARBLE=spike "$TOOLS_BIN/garble" -literals build -trimpath -ldflags "$LDFLAGS" -o out/garble/spike.exe .
ls -l out/plain/spike.exe out/garble/spike.exe
