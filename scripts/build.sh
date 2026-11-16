#!/usr/bin/env bash
# Single build path for supcua-v2.exe, shared by release.sh, CI and the
# release-build action: frontend -> ui/webview/assets/dist -> rsrc -> go/garble.
# Never tags, pushes or uploads, and never calls git.
#
# Usage: scripts/build.sh [--plain] [--version X] [--out DIR]
#   --plain      plain `go build` instead of `garble -literals build`
#   --version X  value for version.Version (default: dev)
#   --out DIR    output directory (default: repository root)
# Env: BUILD_ROOT overrides the repository root (used by the composite action).
set -euo pipefail

MODULE="github.com/yanmad27/supcua-v2"
EXE="supcua-v2.exe"

die() { echo "build.sh: $*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "$1 not found in PATH${2:+ ($2)}"; }

PLAIN=0
VERSION=dev
OUT=
while [ $# -gt 0 ]; do
  case "$1" in
    --plain) PLAIN=1; shift ;;
    --version) [ $# -ge 2 ] || die "--version needs a value"; VERSION="$2"; shift 2 ;;
    --out) [ $# -ge 2 ] || die "--out needs a value"; OUT="$2"; shift 2 ;;
    -h|--help) sed -n '2,10p' "$0"; exit 0 ;;
    *) die "unknown argument: $1" ;;
  esac
done
[[ "$VERSION" =~ ^[0-9A-Za-z._+-]+$ ]] || die "invalid version: $VERSION"

ROOT="${BUILD_ROOT:-$(cd "$(dirname "$0")/.." && pwd)}"
[ -n "$OUT" ] || OUT="$ROOT"
mkdir -p "$OUT"
OUT="$(cd "$OUT" && pwd)"
cd "$ROOT"

if command -v go1.25.0 >/dev/null 2>&1; then
  GOROOT="$(go1.25.0 env GOROOT)"
  export GOROOT
  PATH="$GOROOT/bin:$PATH"
fi

need go
need rsrc
[ "$PLAIN" = 1 ] || need garble "go install mvdan.cc/garble@v0.15.0"

DIST="ui/webview/assets/dist"
BACKUP="$(mktemp -d)"
SYSO_CREATED=0
[ -e rsrc.syso ] || SYSO_CREATED=1
cp -Rp "$DIST/." "$BACKUP/"
cleanup() {
  rm -rf "$DIST"
  mkdir -p "$DIST"
  cp -Rp "$BACKUP/." "$DIST/"
  rm -rf "$BACKUP"
  [ "$SYSO_CREATED" = 0 ] || rm -f rsrc.syso
}
trap cleanup EXIT

if [ -d frontend ]; then
  need node
  need npm
  echo "==> Building frontend..."
  (cd frontend && npm ci --ignore-scripts && npm run build)
  [ -f frontend/dist/index.html ] || die "frontend build produced no dist/index.html"
  rm -rf "$DIST"
  mkdir -p "$DIST"
  cp -R frontend/dist/. "$DIST/"
  rm -f "$DIST/.gitkeep"
else
  echo "==> No frontend/ directory; building with the placeholder UI"
fi

echo "==> Generating icon resource..."
rsrc -ico app.ico -manifest main.manifest -o rsrc.syso

export GOOS=windows GOARCH=amd64 GOGARBLE="$MODULE"
LDFLAGS="-H windowsgui -s -w -X $MODULE/version.Version=$VERSION"

echo "==> Building $EXE (version $VERSION, $([ "$PLAIN" = 1 ] && echo plain || echo garble)) with $(go version)"
if [ "$PLAIN" = 1 ]; then
  go build -buildvcs=false -trimpath -ldflags="$LDFLAGS" -o "$OUT/$EXE" .
else
  garble -literals build -buildvcs=false -trimpath -ldflags="$LDFLAGS" -o "$OUT/$EXE" .
fi

if command -v sha256sum >/dev/null 2>&1; then HASH="$(sha256sum "$OUT/$EXE")"; else HASH="$(shasum -a 256 "$OUT/$EXE")"; fi
echo "==> Built $OUT/$EXE ($(wc -c < "$OUT/$EXE" | tr -d ' ') bytes)"
echo "sha256 ${HASH%% *}"
