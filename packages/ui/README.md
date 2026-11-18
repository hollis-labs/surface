# @hollis-labs/surface-ui

The `ui` tool for apps built on the [Surface](https://github.com/hollis-labs/surface) registry. It copies
components into the app's source, records them in `ui.lock.json`, and fails when a copy differs from the
registry: a look is changed in the registry and synced, never edited in the app.

```sh
npx surface-ui init --ref v0.1.0          # ui.lock.json naming the registry on GitHub at a tag
npx surface-ui add button dialog          # install items and what they depend on
npx surface-ui add --for <package>        # the items an installed package draws with
npx surface-ui sync                       # reinstall everything locked (after moving ref)
npx surface-ui verify                     # CI: an edited, stale, missing or stray file fails
npx surface-ui status
npx surface-ui report <app dir...>        # drift across several apps
```

Options: `--app <dir>` (default the current directory), `--registry <dir or URL template>`. The registry and
its rules are in the repository's `registry/README.md`.
