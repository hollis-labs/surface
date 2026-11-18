/**
 * The page's attention hub (@hollis-labs/ade-core) and how it follows the server.
 *
 * The desk is one ADE surface, "desk". Its parts are the windows ({kind: "window", id}) and
 * the anchors views declare ({kind: "anchor", id: "<window>/<name>"}). The agent's
 * ade_point and ade_guide_start tools publish what to show; this file applies it to the hub,
 * and reports where a guide stands back to the server, which is how the agent sees it.
 */
import { type Anchor, createHub, type Guide, type GuideState, type Rect } from "@hollis-labs/ade-core"
import { deskState, viewById } from "../desk/store"
import { anchorElement } from "../sdk/anchors"
import { call, onEvent } from "../sdk/runtime"

export const hub = createHub()

/** What the server sends: see core/internal/ade. */
interface Point {
  window: string
  anchor?: string
  label?: string
}
interface ServerAttention {
  style: "highlight" | "spotlight" | "arrow"
  targets: Point[]
  from?: Point
  note?: string
  until?: number
}
interface ServerStep {
  id: string
  say: string
  point?: Point[]
  style?: "highlight" | "spotlight"
  await?: { kind: "open" | "focus"; view: string }
  hint?: string
}
interface ServerGuide {
  id: string
  title: string
  steps: ServerStep[]
}
interface Report {
  guide: string
  index: number
  status: string
}

const toAnchor = (p: Point): Anchor => ({
  surface: "desk",
  target: p.anchor ? { kind: "anchor", id: `${p.window}/${p.anchor}` } : { kind: "window", id: p.window },
  ...(p.label ? { label: p.label } : {}),
})

/** A rectangle on screen, or null when the element is not showing (a background tab). */
function rectOf(el: Element | null): Rect | null {
  if (!el) return null
  const r = el.getBoundingClientRect()
  return r.width && r.height ? { x: r.x, y: r.y, w: r.width, h: r.height } : null
}

const viewTitle = (view: string) => viewById(view)?.title ?? view

/** The server's guide as the hub's. A step that waits says so in words, in its await's args.text. */
function toGuide(g: ServerGuide): Guide {
  return {
    id: g.id,
    title: g.title,
    surface: "desk",
    steps: g.steps.map((s) => ({
      id: s.id,
      say: s.say,
      ...(s.point?.length ? { anchors: s.point.map(toAnchor) } : {}),
      ...(s.style ? { style: s.style } : {}),
      ...(s.hint ? { hint: s.hint } : {}),
      ...(s.await
        ? {
            await: {
              kind: "state" as const,
              check: s.await.kind === "open" ? "window_open" : "focused",
              args: {
                view: s.await.view,
                text:
                  s.await.kind === "open"
                    ? `Open ${viewTitle(s.await.view)}.`
                    : `Bring ${viewTitle(s.await.view)} to the front.`,
              },
            },
          }
        : {}),
    })),
  }
}

function applyAttention(a: ServerAttention | null) {
  if (!a) {
    hub.clearAttention("agent", "point")
    return
  }
  const ttl = a.until ? a.until - Date.now() : undefined
  if (ttl !== undefined && ttl <= 0) return
  hub.attend({
    actor: "agent",
    style: a.style,
    anchors: a.targets.map(toAnchor),
    ...(a.from ? { from: toAnchor(a.from) } : {}),
    ...(a.note ? { note: a.note } : {}),
    ...(ttl ? { ttl } : {}),
    slot: "point",
  })
}

function applyGuide(g: ServerGuide | null, report?: Report | null) {
  if (!g) {
    if (hub.guideState()) hub.guideDo("stop", "agent")
    return
  }
  const guide = toGuide(g)
  if (
    report &&
    report.guide === g.id &&
    report.index > 0 &&
    (report.status === "running" || report.status === "paused")
  ) {
    const state: GuideState = { guide, index: report.index, status: report.status, controller: "agent", history: [] }
    if (hub.restoreGuide(state)) return
  }
  try {
    hub.startGuide(guide, "agent")
  } catch (err) {
    console.warn("ade: the guide could not start", err)
  }
}

let started = false

/** Put the desk in front of the hub and follow the server. Once per page. */
export function startAde() {
  if (started) return
  started = true
  const desk = hub.register({
    info: { id: "desk", app: "surface", title: "Desk" },
    observe: () => ({
      actions: [],
      checks: [
        {
          name: "window_open",
          description: "A window of the view is open.",
          args_schema: { type: "object", properties: { view: { type: "string" } } },
        },
        {
          name: "focused",
          description: "A window of the view is in front.",
          args_schema: { type: "object", properties: { view: { type: "string" } } },
        },
      ],
      targets: deskState().wins.map((w) => ({ kind: "window", id: w.id, label: w.title })),
    }),
    act: () => ({ outcome: "rejected", code: "invalid", reason: "The desk is changed through the desk's own tools." }),
    resolve: (a) => {
      const t = a.target
      if (!t) return null
      if (t.kind === "window") return rectOf(document.querySelector(`[data-window="${CSS.escape(t.id)}"]`))
      if (t.kind === "anchor") {
        const cut = t.id.indexOf("/")
        return rectOf(anchorElement(t.id.slice(0, cut), t.id.slice(cut + 1)))
      }
      return null
    },
    bounds: () => rectOf(document.querySelector("[data-slot=app-frame-content]")),
    has: (t) =>
      t.kind === "window"
        ? deskState().wins.some((w) => w.id === t.id)
        : t.kind === "anchor" && !!anchorElement(t.id.split("/")[0] ?? "", t.id.slice(t.id.indexOf("/") + 1)),
    check: (name, args) => {
      const s = deskState()
      const view = String(args.view ?? "")
      if (name === "window_open") return s.wins.some((w) => w.view === view)
      if (name === "focused") return s.wins.find((w) => w.id === s.focus)?.view === view
      return false
    },
  })
  // A window opened, closed or came forward: a guide step may now be met.
  onEvent("desk", () => setTimeout(() => desk.changed(), 0))

  onEvent("ade.attention", (ev) => applyAttention((ev.data as { attention: ServerAttention | null }).attention))
  onEvent("ade.guide", (ev) => applyGuide((ev.data as { guide: ServerGuide | null }).guide))

  // Where the guide stands goes back to the server, for ade_guide_status.
  hub.subscribe((e) => {
    if (e.type !== "guide" || !e.state) return
    const g = e.state
    void call("POST", "/api/ade/guide", {
      guide: g.guide.id,
      index: g.index,
      step: g.guide.steps[g.index]?.id ?? "",
      status: g.status,
      ...(g.stalled ? { stalled: g.stalled.reason } : {}),
    }).catch(() => {})
  })

  void call<{ attention: ServerAttention | null; guide: ServerGuide | null; report: Report | null }>("GET", "/api/ade")
    .then((s) => {
      applyAttention(s.attention)
      applyGuide(s.guide, s.report)
    })
    .catch(() => {})
}

/** The owner dismissed what the agent pointed at. */
export const dismissAttention = () => {
  hub.clearAttention("agent", "point")
  void call("DELETE", "/api/ade/attention").catch(() => {})
}
