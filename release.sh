#!/usr/bin/env bash
set -e

VERSION=${1:?"Usage: ./release.sh <version>  e.g. ./release.sh 1.0.1"}
TAG="v$VERSION"
EXE="supcua-v2.exe"

echo "==> Building $EXE $TAG..."
go build -ldflags="-H windowsgui -X github.com/yanmad27/supcua-v2/version.Version=$VERSION" -o "$EXE" .

echo "==> Tagging $TAG..."
git tag "$TAG"
git push origin "$TAG"

echo "==> Creating GitHub release and uploading $EXE..."
gh release create "$TAG" "$EXE" --title "$TAG" --notes "Release $TAG"

echo "==> Done!"
gh release view "$TAG" --web
