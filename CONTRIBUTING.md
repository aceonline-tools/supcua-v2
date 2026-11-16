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

`release.sh` only builds locally; it does not tag or publish. GitHub Releases is the only update channel.

### Recovery

If the build or upload failed for an existing tag: Actions → release-please → Run workflow on `main`, set `tag` to e.g. `v1.0.29`. It rebuilds that tag and uploads with `--clobber`. No new release is created.

### Updating from v1.0.28 or older

Clients on v1.0.28 or older do not auto-update to the new version. Users download `supcua-v2.exe` once from https://github.com/aceonline-tools/supcua-v2/releases/latest; from then on the in-app updater reads GitHub Releases.

## Repository prerequisites

- Settings → Actions → General → Workflow permissions → enable **Allow GitHub Actions to create and approve pull requests** (the organization setting must permit it too).
- PRs created with `GITHUB_TOKEN` do not trigger CI. For the release PR, close and reopen it, or push an empty commit to its branch, to run checks (or switch release-please to a PAT/GitHub App token).
