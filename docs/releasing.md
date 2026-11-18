# Releasing Surface

Surface has one version. A tag `vX.Y.Z` on this repository is, all at once:

- the registry an app outside this repository reads (`"ref": "vX.Y.Z"` in its `ui.lock.json`);
- the Go module version (`go get github.com/hollis-labs/surface@vX.Y.Z`);
- the version of every npm package: `@hollis-labs/surface-chat`, `-cards`, `-genui`, `-page` and `-ui`.

So "which Surface is this app on" has one answer. Tags and releases are the owner's decision.

## Steps

1. **Version and changelog, in a pull request.** `node scripts/version.mjs X.Y.Z` sets every package (and the
   page's pins on its siblings); add a `## [X.Y.Z] - YYYY-MM-DD` section to `CHANGELOG.md` reconciled with
   `git log --oneline <last tag>..HEAD`. The full suite passes, including `make pack-check`. Merge with a merge
   commit.
2. **Tag the merge commit.** On a clean `main`: `git tag -a vX.Y.Z -m "Surface vX.Y.Z"`, then
   `git push origin refs/tags/vX.Y.Z`. A pushed tag is never moved or reused. Check the registry answers at it:
   `curl -sf https://raw.githubusercontent.com/hollis-labs/surface/vX.Y.Z/registry/r/button.json`.
3. **Publish to npm.** Publishing runs only in `hollis-labs/frontend-support`, whose `npm-release` environment
   holds the portfolio's one npm token. Its `release-surface` workflow checks out this repository at the tag,
   checks the versions match it, builds, and publishes the packages in dependency order, skipping any version
   already on npm:

   ```sh
   gh workflow run release-surface -R hollis-labs/frontend-support -f ref=vX.Y.Z -f dry_run=false
   ```

   The run waits for the owner to approve the environment. Dry run is the default.
4. **Verify.** `npm view @hollis-labs/surface-page version` answers `X.Y.Z` (a new name can 404 for a minute),
   and `go get github.com/hollis-labs/surface@vX.Y.Z` resolves from an empty module.

npm names and versions are permanent. A mistake is fixed with a new patch version, never by republishing.

The packages are published without npm provenance: provenance requires the package's repository to be the
repository whose workflow publishes, and the token lives in frontend-support while the source is here.
