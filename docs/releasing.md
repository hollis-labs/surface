# Releasing Surface

Surface has one version. A tag `vX.Y.Z` on this repository is, all at once:

- the registry an app outside this repository reads (`"ref": "vX.Y.Z"` in its `ui.lock.json`);
- the Go module version (`go get github.com/hollis-labs/surface@vX.Y.Z`);
- the version of every npm package: `@hollis-labs/surface-chat`, `-cards`, `-genui`, `-theme`, `-page` and `-ui`.

So "which Surface is this app on" has one answer. Releases are run by the release manager (the `hollis-release-manager` GitHub App, acting for an agent with the owner's standing permission) or by the owner.

## Steps

1. **Version and changelog, in a pull request.** `node scripts/version.mjs X.Y.Z` sets every package (and the
   page's pins on its siblings); add a `## [X.Y.Z] - YYYY-MM-DD` section to `CHANGELOG.md` reconciled with
   `git log --oneline <last tag>..HEAD`. The full suite passes, including `make pack-check`. Merge with a merge
   commit.
2. **Tag the merge commit.** On a clean `main`: `git tag -a vX.Y.Z -m "Surface vX.Y.Z"`, then
   `git push origin refs/tags/vX.Y.Z`. A pushed tag is never moved or reused. Check the registry answers at it:
   `curl -sf https://raw.githubusercontent.com/hollis-labs/surface/vX.Y.Z/registry/r/button.json`.
3. **Publish to npm.** The local `release-surface` workflow checks a release tag on main with successful CI, resolves its immutable commit, checks package versions, builds and pack-checks all six packages, then publishes in dependency order. `RELEASE_ACTORS` gates the dispatcher and re-run actor. Dry run is the default and requires no npm credential; it checks every package even when its version is already published.

   ```sh
   gh workflow run release-surface -R hollis-labs/surface -f ref=vX.Y.Z -f dry_run=true
   gh workflow run release-surface -R hollis-labs/surface -f ref=vX.Y.Z -f dry_run=false
   ```

   **Credential migration pending:** configure Surface's `npm-release` environment with `NPM_TOKEN` before a real publish. The workflow fails closed without it. GitHub secrets cannot be read back or copied from frontend-support. Keep frontend-support's existing publisher available until the new credential is installed and validated; use its existing release command if a release is needed before cutover. Never dispatch both publishers for one release.

4. **Verify.** `npm view @hollis-labs/surface-page version` answers `X.Y.Z` (a new name can 404 for a minute),
   and `go get github.com/hollis-labs/surface@vX.Y.Z` resolves from an empty module.

npm names and versions are permanent. A mistake is fixed with a new patch version, never by republishing.

The packages currently publish without npm provenance. Publisher relocation keeps that behavior; provenance can be enabled separately after credential migration.
