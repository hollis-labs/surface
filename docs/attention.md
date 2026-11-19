# Attention

What needs the owner, or has not been seen by them, is kept in one place with one vocabulary
(`core/internal/attend`). The page, a notification, a "jump to what needs me" key and the agent all read the same
list, so "waiting for you" means the same thing wherever it comes from.

| State | Means | Cleared when |
|---|---|---|
| `needs_you` | an open card waits on the owner's answer: `confirm`, `prompt`, `choice`, `form`, `approval`, `plan`, or any blocking card | the card is answered or closed. Looking at it does not clear it. |
| `failed` | an `error` card the owner has not dealt with | the card is closed, or the owner marks it seen |
| `unseen` | a window a delegate opened while the owner was in another window; or a question that **expired before anyone answered** | the owner brings the window in front, or marks it seen |

The list is ordered with `needs_you` first, then `failed`, then `unseen`, and the oldest first within each.

## Rules

- **Items come from what happens, never from a claim.** They are derived from the bus (`desk.opened`,
  `desk.focus`, `desk.changed`, `cards.changed`). There is no tool for an agent to raise an item: an agent that
  wants an answer shows a card.
- **Only the owner sees.** A window counts as seen when the owner brings it in front. The agent bringing it in
  front does not count. `POST /api/attend/seen` refuses a delegate.
- **The owner's own actions raise nothing.** A window the owner opens, or one the agent opens in front of them,
  is not news.
- **Lapsing is not declining.** A question that expired unanswered stays as `unseen`, so the owner learns it
  happened, instead of the silence being read as "no" (after ai-bwrap's "could not be asked").
- An item goes when its window closes. Items survive a restart.

## Reading it

- `GET /api/attend` returns `{items, counts}`. `attend.changed` on the bus carries the same whenever it changes.
- The agent's tool is `attend_list`. Use it before asking the owner something new, so as not to pile on.

## Not there yet

- **The agent's own runs** (working, done but not yet read, failed). These need the engine and the conversation
  wire to say when a run ends.
- **The page.** A badge, the jump key, and notifications suppressed for what is already in front.
- **The panel.** Seen should mean visible *and* focused, which the page will report for the agent panel too.
