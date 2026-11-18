# @hollis-labs/surface-chat

A conversation with an agent host, without any styling. It is the behaviour half of Surface's chat; the visuals
are the registry's chat components (`conversation`, `message`, `markdown`, `prompt-input`, `tool`), and
`web/src/agent/AgentPanel.tsx` puts the two together.

Unpublished for now. Another app can take it as source (the page aliases `@hollis-labs/surface-chat` to `src/index.ts`); it
has no dependency but React, and React only for `src/react.ts`.

| File | What it is |
|---|---|
| `src/wire.ts` | The agent-host/1 routes (libs/agent/host `WIRE.md`) and a follower for a run's chatstream/v1 events that resumes a dropped stream from the last event seen |
| `src/reduce.ts` | The conversation as data, and what each event does to it. Pure functions |
| `src/store.ts` | The store: start, send, stop, answer an approval, new thread, reload, follow a turn started elsewhere |
| `src/react.ts` | `useChat(store, select)` and its shorthands; `useStickToBottom` |

## Why drawing stays cheap

- **The shape.** Items are held by id (`items`) with their order beside them (`ids`). A streamed token replaces one
  item and nothing else: `ids` keeps its identity unless an item is added or removed, and so does every other
  item. A list drawn from `ids`, with each row subscribed to its own item, redraws only the row that changed.
- **The composer owns its draft.** Typing changes component state in the composer and nothing in the store.
- **One notification per frame.** A burst of tokens between two frames is one redraw.

Measured on a live turn in the page (development build, `window.__chatRenders`, commits per component): typing 30
characters gave the composer 30 commits and the list and every row 0; a reply of 77 streamed tokens gave its own
row 73 commits, the list 2 (its two new items) and each of the four earlier messages 0.

## History policy

1. The thread is read once, by `start()`, and kept for the life of the page. Opening the panel again reads nothing.
2. A run this store started, or found active, is drawn from its event stream, never from the stored copy.
3. The thread is read again only when someone else changed it: a turn started outside this page
   (`turnElsewhere`, wired to the host's `agent.turn` event) or an explicit `reload()`. Never on a timer or on focus.
4. `newThread()` empties the store and reads nothing.

## Not here

- **Page tools.** The wire lets a page declare tools and run them; Surface's tools are all server tools, so turns
  declare none. Add a runner here if an app needs one.
- **Cards.** The panel draws each item through a table (`web/src/agent/items.tsx`): a tool with a card of its own
  goes to that card, every other tool to the plain row.

`npm test` runs the reducer and store tests (`node --test`).
