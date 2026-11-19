# Desk rules

These are the rules the desk keeps, so that working beside an agent feels like working beside a considerate
colleague, not one who keeps rearranging your desk. Each rule says who it protects and what enforces it. A rule
moves from **planned** to **kept** only when a test holds it.

The agent, the palette, the CLI and outside clients drive the same desk (architecture.md, "The desk is the
server's"). So these rules live in the server (`core/internal/desk`), not in the page. That way no actor gets
round them by using another door.

## Who is acting

Every change to the desk is made by an **actor** (`module.Actor`, carried on the request's context):

| Actor | Who | Set where |
|---|---|---|
| `owner` | the person at the app's own page | the HTTP guard, for a request that carries the page's `Origin` |
| `agent` | the embedded agent | every tool the engine runs (`agent.bind`) |
| `reflex` | a reflex's `run_command` or `call_tool` | the reflex doer |
| `outside` | MCP, `/api/tools`, the CLI: anything without an `Origin` | the HTTP guard |
| `""` | a module acting on its own (a timer, a background job) | nothing; treated as the owner |

The agent, a reflex and an outside client are **delegates**: they act for the owner, not as the owner. The rules
below hold delegates to them. A request body can never name its actor (`desk.Action.Actor` is `json:"-"`).

## The rules

| # | Rule | State |
|---|---|---|
| 1 | Every window records who opened it (`opened_by`), the owner's window in front when it opened (`opener`), what it is for (`reason`) and how long a delegate means it to stay (`retention`: `transient` or `deliverable`). | kept |
| 2 | A delegate never takes the focus from a window holding the owner's unsaved work. An open goes to the background instead, and a focus is refused (`ErrOwnerBusy`). | kept, for unsaved work; planned, for typing in the agent panel or a field that is not yet dirty |
| 3 | When the window in front closes, the focus goes back to the newest window the owner had in front. The fallbacks, in order: the closed window's `opener`, the plain trail, the newest window. Where the agent last looked does not count. | kept |
| 4 | A window becomes the owner's (`kept`) when they open it, make unsaved changes in it, move it, resize it, maximize it or keep it (the `keep` action). Moving it covers float, rect, first, tab, swap and a drag. For a drag, only the window the owner grabbed counts (the `tree` action's `id`), not the windows that shifted to make room. A delegate doing any of these takes nothing over. The windows of a layout are the owner's, whoever arranged them. | kept on the server; the page still has to send `id` with `tree` and offer Keep |
| 5 | A delegate closes only windows a delegate opened and the owner has not kept (`ErrNotYours`). Windows from before actors were recorded count as the owner's. | kept |
| 6 | A `transient` window closes when the delegate is done with it (`release`, the tool `desk_release`), unless the owner kept it or has unsaved work in it, and the focus returns by rule 3. A `deliverable` window stays. | kept for `release`; planned: closing by itself when its task or card resolves |
| 7 | Draft-aware: nothing is typed into, or replaces, a window holding the owner's unsent input. It is queued with a mark instead. | planned |
| 8 | Nothing reflows under the cursor during a drag, and a list never reorders under the cursor; only new arrivals move. | planned (page) |
| 9 | No notification fires for what is already in front, and bursts are coalesced. | planned |
| 10 | There is one way the focus is restored: `returnTo` in `desk.go`. Nothing else picks where the focus goes after a close. | kept |
| 11 | An agent window that lands while the owner is away counts as unseen until it has been visible and focused. | planned (attention model) |
| 12 | Esc closes the topmost layer, in a fixed order: overlay, then card, then transient window. | planned (page) |
| 13 | A delegate's window opens beside the owner's window it came from, sharing that window's space: to its right when it is wide, below it when it is narrow (judged on a 16:10 desk). It is placed at the end of the tiling only when that window floats or is gone. | kept |

### Why the owner's trail, not the opener, comes first in rule 3

Take a desk where the owner was in Notes, the agent opened Browser, the owner went to Scratch, then came back
to Browser and closed it. The owner expects to land in Scratch, where they last were, not in Notes, where
Browser was opened from. The plain case still lands where the owner expects: they were in Notes, the agent
opened Browser, and the owner closed it, so Notes is both the newest owner's window and the opener. The opener
is what is left when every window the owner had in front is gone.

## For the agent

`desk_windows` shows `opened_by`, `opener`, `reason`, `retention` and `kept` for every window. `desk_open` takes
`reason` and `retention`. `desk_close` refuses the owner's windows with a message telling the agent to ask
instead. The agent should:

- give a reason for every window it opens;
- open `transient` for a window that serves a step, and `deliverable` for a result;
- close its own transient windows when it is done with them;
- never work around a refusal.

## Open questions

- **The CLI is `outside`.** The owner typing `desk close notes-1` in a shell is refused like an agent would be.
  The fix is a caller identity the owner can prove, which comes with sign-in. Basalisk has sign-in; Surface
  does not have it yet.
- **Ratio.** Dragging the split between the first window and the rest (`ratio`) is not tied to one window, so
  it takes nothing over.
