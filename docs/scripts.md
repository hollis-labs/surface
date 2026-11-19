# Scripts: one walk-through, three uses

A **script** is an ADE guide with two additions per step:
- **`do`**: look-only tool calls that drive the desk;
- **`expect`**: what the desk should show after the step.

Every desk action is a tool, so the same script runs three ways (`core/internal/script`):

| Mode | Who moves it on | What it is for |
|---|---|---|
| `guide` | The owner (Next, or doing what a step awaits). Each step's `do` runs when it comes up. | Onboarding, on the owner's own desk |
| `demo` | The server, step after step. Each step is held long enough to read, then pointed at with its words beside it. | A recording (`scripts/record-demo.mjs`) or a live demo |
| `test` | The server, with no page. Each step's `do` runs, then its `await` and `expect` are checked. The desk is put back as it was afterwards. | A regression check, from the CLI or in CI |

Writing a demo therefore also writes a tutorial and a test.

## A step

```json
{
  "say": "Scratch is for quick thoughts you will sort later.",
  "do": [{"tool": "desk_open", "input": {"view": "scratch"}}],
  "point": [{"view": "scratch"}],
  "style": "spotlight",
  "await": {"kind": "focus", "view": "scratch"},
  "expect": {"focused": "scratch", "open": ["notes", "scratch"], "closed": ["browser"],
             "mode": "", "layout": "",
             "tool": {"tool": "desk_windows"}, "contains": "Scratch"},
  "hold_ms": 4000,
  "zoom": true
}
```

| Field | Means |
|---|---|
| `say` | What the step says (required). |
| `do` | Tool calls made before the step shows. Look-only tools only (`read` or `act`), run as the agent, so the desk rules hold them as they hold the agent. A script never writes for the owner, and never runs scripts. |
| `point`, `style`, `hint` | As in an ADE guide (`ade_point` targets: `window` or `view`, `anchor`, `label`). |
| `await` | As in an ADE guide: `ack`, `open` or `focus` a view. In a test, the script's own `do` must have brought it about. |
| `expect` | Checked in a test: the view in front, views open or closed, mode, layout, and a look-only tool whose answer contains a text. |
| `hold_ms` | How long a demo shows the step. By default it is long enough to read what the step says: 1.5 s plus 45 ms a character, at most 9 s. |
| `zoom` | Asks the recorder or the page to move the camera in on what the step points at. |

`docs/scripts/tour.json` is a whole script.

## Running one

| | |
|---|---|
| Tools | `script_save` (write), `script_list`, `script_get`, `script_run {name, mode}`, `script_stop`, `script_delete` (confirm) |
| HTTP | `GET /api/scripts`, `GET /api/scripts/{name}`, `POST /api/scripts/{name}/run {mode}`, `POST /api/scripts/stop` |
| CLI | `./bin/desk call script_run '{"name":"tour","mode":"test"}'` prints the report |
| Events | `script.started {script, mode, steps}`, `script.step {script, index, of, say, point, zoom, hold_ms}` (demo), `script.done` (the report) |

Test mode puts the desk back by capturing it before and arranging it after. Run it against a disposable data
directory (`DESK_DATA_DIR`) when the owner's own desk should not even flicker.

## Recording

```sh
make desk
node scripts/record-demo.mjs --script docs/scripts/tour.json [--app desk] [--out dir] [--width 1440 --height 900]
```

The recorder needs no extra dependency. It drives headless Chrome over the DevTools protocol, as `ui-shots`
does:
1. It starts the app with a throwaway data directory.
2. It saves the script.
3. It records the page with the DevTools screencast while the demo runs, until `script.done`.

The frames keep their timing (`frames.ffconcat`). With ffmpeg on the PATH they become an MP4. `steps.json` holds
when each step showed, for captions or edits.

## Not there yet

- **The ghost cursor and camera zoom.** The page should draw a labelled agent cursor gliding to each step's
  target, and scale the desk toward it when `zoom` is set. The server already says what and when in
  `script.step`.
- **Guide mode points only at what is open when the guide starts.** ADE guides resolve their targets at start,
  so the first step's `do` runs first. A later step that points at a window its own `do` opens should use
  `await` instead.
- **Playwright** is not used yet. If recordings need what the screencast lacks (real cursor events, audio), the
  recorder is one file to swap.
