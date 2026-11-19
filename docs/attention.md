# Attention

What needs the owner, or has not been seen by them, is kept in one place with one vocabulary
(`core/internal/attend`). The page, a notification, a "jump to what needs me" key and the agent all read the same
list, so "waiting for you" means the same thing wherever it comes from.

| State | Means | Cleared when |
|---|---|---|
| `needs_you` | an open card waits on the owner's answer: `confirm`, `prompt`, `choice`, `form`, `approval`, `plan`, or any blocking card | the card is answered or closed. Looking at it does not clear it. |
| `failed` | an `error` card the owner has not dealt with | the card is closed, or the owner marks it seen |
| `unseen` | a window a delegate opened while the owner was in another window; a question that **expired before anyone answered**; the agent's answer, finished and not yet read (subject `agent`); or a delegate's change held while the owner works in a window (subject `held:<window>`, cleared when it applies or is dropped, not by looking) | the owner brings the window in front, or marks it seen |
| `working` | the agent is on the owner's message (subject `agent`): nothing to do but know | the run ends: finished becomes `unseen`, failed becomes `failed`, canceled clears it |

The list is ordered with `needs_you` first, then `failed`, then `unseen`, then `working`, and the oldest first within
each.

## Rules

- **Items come from what happens, never from a claim.** They are derived from the bus (`desk.opened`,
  `desk.focus`, `desk.changed`, `desk.held`, `cards.changed`, `agent.run.started`, `agent.run.ended`). There is no tool for an agent to raise an item: an agent that
  wants an answer shows a card.
- **Only the owner sees.** A window counts as seen when the owner brings it in front. The agent bringing it in
  front does not count. `POST /api/attend/seen` refuses a delegate.
- **The owner's own actions raise nothing.** A window the owner opens, or one the agent opens in front of them,
  is not news.
- **Lapsing is not declining.** A question that expired unanswered stays as `unseen`, so the owner learns it
  happened, instead of the silence being read as "no" (after ai-bwrap's "could not be asked").
- An item goes when its window closes. Items survive a restart.

## Reading it

- The agent's answer (subject `agent`) is marked seen by the page when it waits while the agent panel is open and
  the page has the focus: as it finishes in front of the owner, or when they open the panel or come back afterwards.

- The page: the bar's badge counts the items (highlighted while one needs an answer); a click or cmd+. brings the first
  one in front (its window; the Cards window, or the agent panel for the agent's own card). A notice about the window
  already in front does not pop up.

- `GET /api/attend` returns `{items, counts}`. `attend.changed` on the bus carries the same whenever it changes.
- The agent's tool is `attend_list`. Use it before asking the owner something new, so as not to pile on.

## Not there yet


## The agent's runs on the bus

Either engine announces its runs, and both are watched through what Surface hands them
(`core/internal/agent/runs.go`): the host engine through its run store, the conversation wire through its runner.
- `agent.run.started {run_id}`
- `agent.run.ended {run_id, status, reason}`, where `status` is `finished`, `canceled` or `failed`. A stream that
  stops without a terminal event counts as `failed`.

The context trace keeps each end as a `run_end` signal on the turn.
