# @hollis-labs/surface-cards

Cards without any styling. A card is something an app shows the user outside a window: a
notice, a question, a request to approve, progress on work.

| File | What it is |
|---|---|
| `src/response.ts` | What a card reports (`CardOutcome`: answered or declined) and how it reads the status the server recorded (`classifyPriorResponse`, total over any string) |
| `src/engine/` | The lifecycle engine: rules as data decide where each card shows (inline, tray, toast, modal), with dedupe, supersession and held updates while a card is being edited. No React, no timers, no transport |
| `src/form.ts` | A form compiled from a JSON Schema subset, with validation and coercion |
| `src/registry.ts` | The table of renderers by kind and version; an unknown kind resolves to a miss, never to nothing |
| `src/react.ts` | `useAnswerable`: the state every answerable card shares |

The visuals are the `@surface` registry's `card-*` components, which take props only and
do not import this package. An app joins the two: see `web/src/cards` in Surface.

The server owns a card's state. The page never decides a card expired or closed; it draws
what the server recorded. In Surface the server side is `core/internal/cards`, reached by
modules through `Host.Cards()` and by the agent through `card_show`, `card_ask` and
`card_update`.

The engine, the outcome contract and the form subset are ported from `@hollis-labs/kit-cards`
(hollis-labs/frontend-support), with their tests.

Unpublished: Surface resolves it through an alias (`web/tsconfig.json`, `web/vite.config.ts`).
