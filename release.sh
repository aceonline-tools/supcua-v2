#!/usr/bin/env bash
set -e

cd "$(dirname "$0")"

# --build-only is parsed first: it builds locally and never reaches git, gh, aws or .env.
if [ "$1" = "--build-only" ]; then
  shift
  BUILD_ARGS=()
  while [ $# -gt 0 ]; do
    case "$1" in
      --plain) BUILD_ARGS+=(--plain); shift ;;
      --version|--out)
        [ $# -ge 2 ] || { echo "release.sh: $1 needs a value" >&2; exit 2; }
        BUILD_ARGS+=("$1" "$2"); shift 2 ;;
      *) echo "release.sh: --build-only does not accept '$1' (only --version X, --out DIR, --plain)" >&2; exit 2 ;;
    esac
  done
  exec bash scripts/build.sh "${BUILD_ARGS[@]}"
fi

case "$1" in
  -*) echo "release.sh: unknown option '$1' (use --build-only first)" >&2; exit 2 ;;
esac

# Load local env vars if present
if [ -f .env ]; then
  # shellcheck disable=SC2046
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

echo "==> Building $EXE $TAG..."
bash scripts/build.sh --version "$VERSION" --out .

echo "==> Tagging $TAG..."
git tag "$TAG"
git push origin main
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
