# @surface/genui

Generated UI without any styling. Something other than the app's own code (the agent, a
module, an outside client) asks to show a small interface. It names one of three reviewed
patterns and gives parameters; it cannot send components, markup, code or addresses.

| File | What it is |
|---|---|
| `contract/` | The GenUI contract, copied from Substrate (`provenance.json` records the source commit and hashes): the JSON Schema, the catalog and the shared fixtures |
| `src/decode.ts` | Strict JSON decoding before any number is converted: duplicate keys, fractions, exponents, unsafe integers and bad Unicode are refused |
| `src/contract.ts` | Parsing and validating surfaces, calls, outcomes and responses against the schema; `draftErrors` for a form's values |
| `src/controller.ts` | `GenuiController`: takes one surface through an action, a confirmation and a read-only recovery. An uncertain result is never retried and never treated as success |
| `src/validators.js`, `src/generated.ts` | Generated from the schema. Not edited |

The visuals are the `@surface` registry's `genui-surface` component, which takes props only.
An app joins the two and supplies the transport to its host: see `web/src/genui` in Surface.

The host decides everything: what a pattern's parameters become, whether an action is
allowed, what it does, and what came of it. In Surface the host is `core/internal/genui`,
on Substrate's `genui/gateway`.

Ported from `@hollis-labs/genui` (hollis-labs/frontend-support), keeping the logic and its
contract tests and leaving the rendering behind. To refresh the contract, copy `genui/schema`
and the fixtures from a reviewed Substrate commit, regenerate the validators and types there,
and update `provenance.json`.

Unpublished: Surface resolves it through an alias (`web/tsconfig.json`, `web/vite.config.ts`).
