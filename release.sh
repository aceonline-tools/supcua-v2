#!/usr/bin/env bash
set -e

if [ -n "$1" ]; then
  VERSION="$1"
else
  LAST_TAG=$(git tag --sort=-v:refname | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | head -1)
  if [ -z "$LAST_TAG" ]; then
    LAST_TAG="v0.0.0"
  fi
  # Strip leading 'v' and increment patch
  IFS='.' read -r MAJOR MINOR PATCH <<< "${LAST_TAG#v}"
  PATCH=$((PATCH + 1))
  VERSION="$MAJOR.$MINOR.$PATCH"
  echo "==> Auto version: $LAST_TAG -> v$VERSION"
fi
TAG="v$VERSION"
EXE="supcua-v2.exe"

echo "==> Regenerating icon resource..."
rsrc -ico app.ico -manifest main.manifest -o rsrc.syso

echo "==> Building $EXE $TAG..."
go build -ldflags="-H windowsgui -X github.com/yanmad27/supcua-v2/version.Version=$VERSION" -o "$EXE" .

echo "==> Tagging $TAG..."
git tag "$TAG"
git push origin "$TAG"

echo "==> Creating GitHub release and uploading $EXE..."
gh release create "$TAG" "$EXE" --title "$TAG" --notes "Release $TAG"

echo "==> Done!"
gh release view "$TAG" --web
