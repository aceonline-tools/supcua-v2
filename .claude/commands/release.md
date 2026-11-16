Run the release process for supcua-v2 (release-please; GitHub Releases is the only update channel).

Steps:
1. Commit and push pending work to `main` (via PR) using Conventional Commits; check `git status` first.
2. Find the release PR: `gh pr list --repo aceonline-tools/supcua-v2 --search "chore(main): release" --state open`. If none exists, check the latest `release-please` run: `gh run list --workflow release-please.yml --limit 3`.
3. Review its changelog and version, confirm CI checks pass (`gh pr checks <n>`), then merge it (needs the user's go-ahead): `gh pr merge <n> --squash`.
4. Merging creates tag `vX.Y.Z` and the GitHub Release; the `build-and-attach` job builds on Windows and attaches `supcua-v2.exe` and `supcua-v2.exe.sha256`. Verify: `gh release view vX.Y.Z --repo aceonline-tools/supcua-v2 --json assets,isDraft,isPrerelease`. The asset must be named exactly `supcua-v2.exe`.
5. Recovery if the build or upload failed: `gh workflow run release-please.yml --repo aceonline-tools/supcua-v2 --ref main -f tag=vX.Y.Z` (rebuilds that tag, uploads with `--clobber`, no new release).
6. Report the release URL and asset list to the user.

`release.sh [version]` only builds locally; never use it to publish.
