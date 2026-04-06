Run the release process for supcua-v2.

Steps:
1. Run the release script immediately (no version prompt, no confirmation): `export PATH="$PATH:/c/Program Files/GitHub CLI" && bash release.sh`
2. Show the result to the user

The release script will:
- Auto-increment the patch version from the latest git tag
- Build supcua-v2.exe with the version embedded
- Create and push a git tag v<version>
- Create a GitHub release with the exe uploaded
- Open the release in the browser
