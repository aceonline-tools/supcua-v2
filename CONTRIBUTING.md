# Contributing

## Commits

Use [Conventional Commits](https://www.conventionalcommits.org/). release-please derives the next version and changelog from them.

| Type | Effect |
|---|---|
| `feat:` | minor bump |
| `fix:`, `perf:`, `refactor:` | patch bump (listed in the changelog) |
| `docs:`, `test:`, `ci:`, `chore:` | hidden; no release on their own |
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

### Updater migration (MinIO → GitHub Releases)

The in-app updater now reads the latest GitHub Release (asset `supcua-v2.exe`); see the README section of the same name. Clients ≤ v1.0.28 still read MinIO `latest.txt`, so **only the first ("bridge") release containing the new updater must also be uploaded to MinIO, manually, once**; do not upload later releases. Keep the bridge exe and `latest.txt` hosted until support for ≤ v1.0.28 clients is intentionally ended. MinIO is not published by CI. After the release asset exists, run with the same credentials `release.sh` uses (`VERSION` has no `v`). Upload the exe **before** `latest.txt`:

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
