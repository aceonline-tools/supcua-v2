#!/usr/bin/env bash
# Local build-only helper. Releases are made by release-please (see CONTRIBUTING.md):
# merge the release PR and CI builds and attaches supcua-v2.exe to the GitHub Release.
# Usage: ./release.sh [version] [--plain] [--out DIR]   (version without leading v; default "dev")
# Never tags, pushes or uploads; the build itself lives in scripts/build.sh.
set -e

cd "$(dirname "$0")"
VERSION=dev
if [ $# -gt 0 ] && [[ "$1" != -* ]]; then
  VERSION="$1"
  shift
fi
exec bash scripts/build.sh --version "$VERSION" "$@"
