#!/usr/bin/env bash
set -e

# Load local env vars if present
if [ -f .env ]; then
  export $(grep -v '^#' .env | xargs)
fi

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
MINIO_ENDPOINT="https://dev.hrm.vangtatmining.com.vn/minio"
MINIO_BUCKET="bucket-supcua-v2"

echo "==> Regenerating icon resource..."
rsrc -ico app.ico -manifest main.manifest -o rsrc.syso

echo "==> Building $EXE $TAG..."
GOROOT=$(go1.25.0 env GOROOT) PATH="$(go1.25.0 env GOROOT)/bin:$PATH" \
  GOGARBLE="github.com/yanmad27/supcua-v2" \
  garble -literals build -trimpath -ldflags="-H windowsgui -s -w -X github.com/yanmad27/supcua-v2/version.Version=$VERSION" -o "$EXE" .

echo "==> Tagging $TAG..."
git tag "$TAG"
git push origin "$TAG"

echo "==> Creating GitHub release and uploading $EXE..."
gh release create "$TAG" "$EXE" --title "$TAG" --notes "Release $TAG"

echo "==> Uploading to MinIO..."
aws --endpoint-url "$MINIO_ENDPOINT" --region us-east-1 configure set default.s3.multipart_threshold 200MB
echo "$VERSION" > latest.txt
aws --endpoint-url "$MINIO_ENDPOINT" --region us-east-1 s3 cp "$EXE" "s3://$MINIO_BUCKET/$EXE"
aws --endpoint-url "$MINIO_ENDPOINT" --region us-east-1 s3 cp latest.txt "s3://$MINIO_BUCKET/latest.txt"
rm latest.txt

echo "==> Done!"
gh release view "$TAG" --web
