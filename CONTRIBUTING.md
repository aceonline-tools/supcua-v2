# Contributing

## Commits

Use [Conventional Commits](https://www.conventionalcommits.org/). release-please derives the next version and changelog from them.

| Type | Effect |
|---|---|
| `feat:` | minor bump |
| `fix:`, `perf:` | patch bump |
| `refactor:`, `docs:`, `test:`, `ci:`, `chore:` | no bump on their own; `refactor` is listed in the changelog |
| `feat!:` / `fix!:` or a `BREAKING CHANGE:` footer | major bump |

PR titles should follow the same format (squash merges use the title as the commit message).

## CI

`.github/workflows/ci.yml` runs on every PR and push to `main` on `windows-latest`: `build`, `vet`, `test`, and `release-build`. `release-build` runs the exact release build (`.github/actions/release-build`: `rsrc` + `garble -literals build` on Go 1.25.0) and uploads `supcua-v2.exe` as a workflow artifact with its SHA-256.

## Releases (release-please)

1. Merge conventional commits into `main`.
2. release-please opens or updates a release PR (`chore(main): release X.Y.Z`) with the changelog.
3. Merging that PR creates tag `vX.Y.Z` and the GitHub Release.
4. The `build-and-attach` job in `.github/workflows/release-please.yml` builds the tag with `version.Version=X.Y.Z` (via ldflags; no file needs bumping) and attaches `supcua-v2.exe` and `supcua-v2.exe.sha256` to the release.

`release.sh` / `/release` is the legacy manual path. Do not use it for a version release-please also manages: both create the same tag and release.

### Recovery

If the build or upload failed for an existing tag: Actions → release-please → Run workflow on `main`, set `tag` to e.g. `v1.0.29`. It rebuilds that tag and uploads with `--clobber`. No new release is created.

### MinIO (manual)

The auto-updater still reads MinIO, which is not published by CI. After the release asset exists, run with the same credentials `release.sh` uses (`VERSION` has no `v`). Upload the exe **before** `latest.txt`:

```bash
ENDPOINT="https://dev.hrm.vangtatmining.com.vn/minio"; BUCKET="bucket-supcua-v2"; VERSION=1.0.29
gh release download "v$VERSION" --pattern supcua-v2.exe
aws --endpoint-url "$ENDPOINT" --region us-east-1 configure set default.s3.multipart_threshold 200MB
aws --endpoint-url "$ENDPOINT" --region us-east-1 s3 cp supcua-v2.exe "s3://$BUCKET/supcua-v2.exe"
echo "$VERSION" > latest.txt
aws --endpoint-url "$ENDPOINT" --region us-east-1 s3 cp latest.txt "s3://$BUCKET/latest.txt"
```

## Repository prerequisites

- Settings → Actions → General → Workflow permissions → enable **Allow GitHub Actions to create and approve pull requests** (the organization setting must permit it too).
- PRs created with `GITHUB_TOKEN` do not trigger CI. For the release PR, close and reopen it, or push an empty commit to its branch, to run checks (or switch release-please to a PAT/GitHub App token).
