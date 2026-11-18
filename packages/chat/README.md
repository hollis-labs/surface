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

## The conversation wire (in progress)

`conversation.ts`, `conversation-reduce.ts` and `conversation-store.ts` bind the same reducer and
components to a durable conversation instead of one run at a time: the page attaches as a viewer or a
controller, reads one event stream from wherever it says, and submits turns when it holds the right to
type. `createConversationStore` offers what `createChatStore` offers, so a panel draws either.

| What it handles | How |
|---|---|
| First paint and reload | Attach, read the stream from 0 (history arrives without deltas), then follow it. No separate history read |
| The sender's own message | Drawn at once under its request id; the stream's copy takes the same item, so it is never shown twice |
| A client that takes no deltas | A finished part carries its whole content, so `stream: "committed"` draws the same messages |
| A dropped connection | Resumes after the last event seen |
| The agent's side replaced (409 `stale_attachment`) | Attaches again and keeps its cursor |
| The conversation reset (410 `conversation_closed`) | Moves to the new conversation |
| Another window typing | A refused send says so; `takeover()` takes the input |
| A turn the host refuses (422) | The message stays, with the reason under it |

It assumes nothing about which agent runs the turns, only the wire, at any base URL. The tests run it
against an in-memory host (`test/fake-host.ts`) that follows the wire's rules. Surface's panel still
uses the run store until the server side is mounted.
