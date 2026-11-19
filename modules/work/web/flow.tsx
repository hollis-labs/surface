/**
 * Flow: the items as nodes and their links as edges, for plot lines, branching scenes and
 * dependencies. The view's relations (rel) are the edges; each relation has its own line,
 * named in the bar. Drag a node to place it (kept in the view's layout, like the canvas); drag
 * from the dot on a selected node to another node to link them with the chosen relation (the
 * link is kept on the source item); select an edge and press Delete to take the link out.
 * Lanes can group the nodes by kind, by stage or by parent. Tidy lays it all out.
 *
 * Basalisk drew this with its Diagram's renderer and router; a module here cannot import
 * another module's code, so this is a small renderer of its own: boxes, curved edges, lanes.
 */
import { Icon } from "@hollis-labs/surface-page/sdk"
import { type PointerEvent as RPointerEvent, useCallback, useEffect, useId, useMemo, useRef, useState } from "react"
import { Button } from "@/components/ui/button"
import { Toggle } from "@/components/ui/toggle"
import type { ViewArgs, WorkCard, WorkView } from "./api"
import { Bar, KindChips, Pick } from "./parts"
import { label, message } from "./util"

/** The relations a flow can draw, the line each gets and what it is called. */
const RELS: { rel: string; name: string; line: string; head: string; dash?: string }[] = [
  { rel: "leads-to", name: "leads to", line: "stroke-foreground", head: "fill-foreground" },
  { rel: "depends-on", name: "depends on", line: "stroke-warning", head: "fill-warning", dash: "6 4" },
  { rel: "appears-in", name: "appears in", line: "stroke-info", head: "fill-info", dash: "2 4" },
  { rel: "uses", name: "uses", line: "stroke-chart-3", head: "fill-chart-3", dash: "10 4 2 4" },
  { rel: "relates", name: "relates", line: "stroke-muted-foreground", head: "fill-muted-foreground", dash: "1 3" },
]
const DEFAULT_RELS = ["leads-to", "depends-on", "appears-in"]
const W = 184
const H = 52
const GAP_X = 70
const GAP_Y = 26
const LANE_PAD = 18

type Lanes = "" | "kind" | "stage" | "parent"
type Dir = "LR" | "TB"
interface FNode {
  card: WorkCard
  x: number
  y: number
  group?: string
}
interface FEdge {
  id: string
  from: string
  to: string
  rel: string
}
const edgeId = (from: string, rel: string, to: string) => `${from}>${rel}>${to}`

/** A layered layout: each node's rank is its longest path from a source; lanes stay together. */
function tidy(ids: string[], edges: FEdge[], groupOf: (id: string) => string | undefined, dir: Dir) {
  const rank = new Map(ids.map((id) => [id, 0]))
  for (let pass = 0; pass < ids.length; pass++) {
    let moved = false
    for (const e of edges) {
      const r = (rank.get(e.from) ?? 0) + 1
      if (rank.has(e.to) && r > (rank.get(e.to) ?? 0) && r < ids.length) {
        rank.set(e.to, r)
        moved = true
      }
    }
    if (!moved) break
  }
  const groups = [...new Set(ids.map((id) => groupOf(id) ?? ""))]
  const out: Record<string, { x: number; y: number }> = {}
  // Each lane is a band; inside it, a column (or row) a rank.
  let offset = 0
  for (const g of groups) {
    const inLane = ids.filter((id) => (groupOf(id) ?? "") === g)
    const byRank = new Map<number, string[]>()
    for (const id of inLane) byRank.set(rank.get(id) ?? 0, [...(byRank.get(rank.get(id) ?? 0) ?? []), id])
    let depth = 0
    for (const [r, list] of byRank) {
      list.forEach((id, i) => {
        const across = offset + i * (dir === "LR" ? H + GAP_Y : W + GAP_X)
        const along = r * (dir === "LR" ? W + GAP_X : H + GAP_Y * 2)
        out[id] = dir === "LR" ? { x: along, y: across } : { x: across, y: along }
      })
      depth = Math.max(depth, list.length)
    }
    offset += depth * (dir === "LR" ? H + GAP_Y : W + GAP_X) + (g ? LANE_PAD * 3 : 0)
  }
  return out
}

/** A curve from one box to another, leaving and entering on the facing sides. */
function curve(a: FNode, b: FNode) {
  const ax = a.x + W / 2
  const ay = a.y + H / 2
  const bx = b.x + W / 2
  const by = b.y + H / 2
  if (Math.abs(bx - ax) >= Math.abs(by - ay)) {
    const s = bx >= ax ? 1 : -1
    const x1 = ax + (s * W) / 2
    const x2 = bx - (s * W) / 2
    const c = Math.max(30, Math.abs(x2 - x1) / 2) * s
    return { d: `M${x1},${ay} C${x1 + c},${ay} ${x2 - c},${by} ${x2},${by}`, mx: (x1 + x2) / 2, my: (ay + by) / 2 }
  }
  const s = by >= ay ? 1 : -1
  const y1 = ay + (s * H) / 2
  const y2 = by - (s * H) / 2
  const c = Math.max(30, Math.abs(y2 - y1) / 2) * s
  return { d: `M${ax},${y1} C${ax},${y1 + c} ${bx},${y2 - c} ${bx},${y2}`, mx: (ax + bx) / 2, my: (y1 + y2) / 2 }
}

const clip = (s: string, n: number) => (s.length > n ? `${s.slice(0, n - 1)}…` : s)

export function Flow({ w, ws, space, view, picked, pick, changed, setError }: ViewArgs & { view: WorkView }) {
  const uid = `f${useId().replace(/[^a-zA-Z0-9]/g, "")}`
  const kinds = useMemo(() => new Map(space.kinds.map((k) => [k.kind, k])), [space.kinds])
  const shownKinds = useMemo(
    () => (view.kinds.length ? view.kinds : space.kinds.map((k) => k.kind)),
    [view.kinds, space.kinds],
  )
  const rels = useMemo(() => (view.rel?.length ? view.rel : DEFAULT_RELS), [view.rel])
  const lanes = (["kind", "stage", "parent"].includes(view.group_by ?? "") ? view.group_by : "") as Lanes
  const [newRel, setNewRel] = useState(rels[0] ?? "leads-to")
  const [moved, setMoved] = useState<Record<string, { x: number; y: number }>>({})
  const [sel, setSel] = useState<{ type: "node" | "edge"; id: string } | null>(null)
  const [wire, setWire] = useState<{ from: string; x: number; y: number } | null>(null)
  const [vp, setVp] = useState({ x: 40, y: 30, k: 1 })
  const svg = useRef<SVGSVGElement>(null)
  const root = useRef<HTMLDivElement>(null)
  const drag = useRef<{
    kind: "pan" | "node"
    id?: string
    sx: number
    sy: number
    ox: number
    oy: number
    moved: boolean
  } | null>(null)

  // Items with a link of a drawn relation (either end); the others only when asked for.
  const showUnlinked = (view.filter as { unlinked?: boolean } | undefined)?.unlinked === true
  const { items, unlinked } = useMemo(() => {
    const all = space.items.filter((c) => !c.cut && shownKinds.includes(c.kind))
    const ids = new Set(all.map((c) => c.id))
    const linked = new Set<string>()
    for (const c of all)
      for (const l of c.links)
        if (rels.includes(l.rel) && ids.has(l.to)) {
          linked.add(c.id)
          linked.add(l.to)
        }
    return {
      items: showUnlinked ? all : all.filter((c) => linked.has(c.id)),
      unlinked: all.filter((c) => !linked.has(c.id)).length,
    }
  }, [space.items, shownKinds, rels, showUnlinked])
  const saved = useMemo(() => {
    const cards = (view.layout as { cards?: Record<string, { x: number; y: number }> } | undefined)?.cards
    return cards && typeof cards === "object" ? cards : {}
  }, [view.layout])

  const groupOf = useCallback(
    (c: WorkCard | undefined): string | undefined =>
      !c
        ? undefined
        : lanes === "kind"
          ? c.kind
          : lanes === "stage"
            ? c.stage || "no stage"
            : lanes === "parent"
              ? c.parent || undefined
              : undefined,
    [lanes],
  )
  const groupLabel = (g: string) =>
    lanes === "kind"
      ? (kinds.get(g)?.plural ?? label(g))
      : lanes === "parent"
        ? (space.items.find((x) => x.id === g)?.title ?? g)
        : label(g)

  const { nodes, edges } = useMemo(() => {
    const byId = new Map(items.map((c) => [c.id, c]))
    const edges: FEdge[] = []
    for (const c of items)
      for (const l of c.links)
        if (RELS.some((r) => r.rel === l.rel) && rels.includes(l.rel) && byId.has(l.to))
          edges.push({ id: edgeId(c.id, l.rel, l.to), from: c.id, to: l.to, rel: l.rel })
    // Places: this window's move, the view's, else a tidy layout (so a new item lands somewhere sensible).
    const auto = tidy(
      items.map((c) => c.id),
      edges,
      (id) => groupOf(byId.get(id)),
      "LR",
    )
    const nodes: FNode[] = items.map((c) => {
      const p = moved[c.id] ?? saved[c.id] ?? auto[c.id] ?? { x: 0, y: 0 }
      return { card: c, x: p.x, y: p.y, group: groupOf(c) }
    })
    return { nodes, edges }
  }, [items, rels, saved, moved, groupOf])
  const nodeById = useMemo(() => new Map(nodes.map((n) => [n.card.id, n])), [nodes])
  const laneBoxes = useMemo(() => {
    if (!lanes) return []
    const out = new Map<string, { x1: number; y1: number; x2: number; y2: number }>()
    for (const n of nodes) {
      if (!n.group) continue
      const b = out.get(n.group) ?? { x1: n.x, y1: n.y, x2: n.x + W, y2: n.y + H }
      out.set(n.group, {
        x1: Math.min(b.x1, n.x),
        y1: Math.min(b.y1, n.y),
        x2: Math.max(b.x2, n.x + W),
        y2: Math.max(b.y2, n.y + H),
      })
    }
    return [...out.entries()]
  }, [nodes, lanes])

  const savePlaces = async (cards: Record<string, { x: number; y: number }>) => {
    try {
      await w.layout(ws, view.id, { cards })
      setError("")
    } catch (e) {
      setError(message(e))
    }
    changed()
  }
  const relink = async (
    from: string,
    change: (links: { rel: string; to: string }[]) => { rel: string; to: string }[],
  ) => {
    try {
      const d = await w.item(ws, from)
      const next = change(d.links)
      if (JSON.stringify(next) === JSON.stringify(d.links)) return
      await w.edit(ws, from, { base_rev: d.rev, links: next })
      setError("")
    } catch (e) {
      setError(message(e))
    }
    changed()
  }
  const removeEdge = (id: string) => {
    const [from, rel, to] = id.split(">")
    setSel(null)
    void relink(from, (ls) => ls.filter((l) => !(l.rel === rel && l.to === to)))
  }

  // ---- Pointer: pan, move, link ----
  const at = (e: { clientX: number; clientY: number }) => {
    const box = svg.current?.getBoundingClientRect()
    return { x: (e.clientX - (box?.left ?? 0) - vp.x) / vp.k, y: (e.clientY - (box?.top ?? 0) - vp.y) / vp.k }
  }
  const down = (e: RPointerEvent<SVGSVGElement>) => {
    if (e.button !== 0) return
    root.current?.focus({ preventScroll: true })
    const t = e.target as Element
    const node = t.closest<SVGGElement>("[data-node]")
    const edge = t.closest<SVGGElement>("[data-edge]")
    e.currentTarget.setPointerCapture(e.pointerId)
    if (t.closest("[data-handle]") && node) {
      const p = at(e)
      setWire({ from: node.dataset.node ?? "", x: p.x, y: p.y })
      return
    }
    if (node) {
      const id = node.dataset.node ?? ""
      const n = nodeById.get(id)
      setSel({ type: "node", id })
      if (n) drag.current = { kind: "node", id, sx: e.clientX, sy: e.clientY, ox: n.x, oy: n.y, moved: false }
      return
    }
    if (edge) return setSel({ type: "edge", id: edge.dataset.edge ?? "" })
    setSel(null)
    drag.current = { kind: "pan", sx: e.clientX, sy: e.clientY, ox: vp.x, oy: vp.y, moved: false }
  }
  const move = (e: RPointerEvent<SVGSVGElement>) => {
    if (wire) {
      const p = at(e)
      return setWire({ ...wire, x: p.x, y: p.y })
    }
    const d = drag.current
    if (!d) return
    const dx = e.clientX - d.sx
    const dy = e.clientY - d.sy
    if (Math.abs(dx) + Math.abs(dy) > 3) d.moved = true
    if (d.kind === "pan") setVp((v) => ({ ...v, x: d.ox + dx, y: d.oy + dy }))
    else if (d.id && d.moved) {
      const id = d.id
      setMoved((m) => ({ ...m, [id]: { x: d.ox + dx / vp.k, y: d.oy + dy / vp.k } }))
    }
  }
  const up = (e: RPointerEvent<SVGSVGElement>) => {
    if (wire) {
      const p = at(e)
      const to = nodes.find(
        (n) => n.card.id !== wire.from && p.x >= n.x && p.x <= n.x + W && p.y >= n.y && p.y <= n.y + H,
      )
      const from = wire.from
      setWire(null)
      if (to)
        void relink(from, (ls) =>
          ls.some((l) => l.rel === newRel && l.to === to.card.id) ? ls : [...ls, { rel: newRel, to: to.card.id }],
        )
      return
    }
    const d = drag.current
    drag.current = null
    if (d?.kind === "node" && d.id) {
      if (!d.moved) pick(d.id)
      else {
        const p = moved[d.id]
        if (p) void savePlaces({ [d.id]: { x: Math.round(p.x), y: Math.round(p.y) } })
      }
    }
  }
  // Wheel: a trackpad pans, a wheel or ⌘/Ctrl zooms (a native listener: React's is passive).
  useEffect(() => {
    const el = svg.current
    if (!el) return
    const onWheel = (e: WheelEvent) => {
      e.preventDefault()
      const box = el.getBoundingClientRect()
      setVp((v) => {
        if (!e.ctrlKey && !e.metaKey && e.deltaMode === 0 && !Number.isInteger(e.deltaY))
          return { ...v, x: v.x - e.deltaX, y: v.y - e.deltaY }
        const k = Math.min(2.5, Math.max(0.2, v.k * (e.deltaY < 0 ? 1.1 : 1 / 1.1)))
        const px = e.clientX - box.left
        const py = e.clientY - box.top
        return { k, x: px - ((px - v.x) / v.k) * k, y: py - ((py - v.y) / v.k) * k }
      })
    }
    el.addEventListener("wheel", onWheel, { passive: false })
    return () => el.removeEventListener("wheel", onWheel)
  })
  const fit = useCallback(() => {
    const box = svg.current?.getBoundingClientRect()
    if (!box || !nodes.length) return
    const x1 = Math.min(...nodes.map((n) => n.x)) - LANE_PAD * 2
    const y1 = Math.min(...nodes.map((n) => n.y)) - LANE_PAD * 2
    const x2 = Math.max(...nodes.map((n) => n.x + W)) + LANE_PAD
    const y2 = Math.max(...nodes.map((n) => n.y + H)) + LANE_PAD
    const k = Math.min(1.2, Math.max(0.2, Math.min((box.width - 60) / (x2 - x1), (box.height - 60) / (y2 - y1))))
    setVp({ k, x: (box.width - (x2 - x1) * k) / 2 - x1 * k, y: (box.height - (y2 - y1) * k) / 2 - y1 * k })
  }, [nodes])
  const fitted = useRef("")
  useEffect(() => {
    if (fitted.current !== view.id && nodes.length) {
      fitted.current = view.id
      requestAnimationFrame(fit)
    }
  }, [view.id, nodes.length, fit])
  // A read-back that moved a node replaces this window's own move.
  // biome-ignore lint/correctness/useExhaustiveDependencies: a newer view revision resets local moves
  useEffect(() => setMoved({}), [view.rev])

  const saveView = async (set: Record<string, unknown>) => {
    try {
      await w.saveView(ws, view.id, set)
    } catch (e) {
      setError(message(e))
    }
    changed()
  }
  const tidyNow = (dir: Dir) => {
    const byId = new Map(items.map((c) => [c.id, c]))
    const laid = tidy(
      items.map((c) => c.id),
      edges,
      (id) => groupOf(byId.get(id)),
      dir,
    )
    const cards: Record<string, { x: number; y: number }> = {}
    for (const [id, p] of Object.entries(laid)) cards[id] = { x: Math.round(p.x), y: Math.round(p.y) }
    setMoved(cards)
    void savePlaces(cards).then(() => requestAnimationFrame(fit))
  }

  const selected = sel ?? (picked && nodeById.has(picked) ? { type: "node" as const, id: picked } : null)
  const titleOf = (id: string) => items.find((c) => c.id === id)?.title ?? id

  return (
    <div
      ref={root}
      className="flex min-h-0 flex-1 flex-col outline-none"
      tabIndex={-1}
      onKeyDown={(e) => {
        const tag = (e.target as HTMLElement).tagName
        if (tag === "INPUT" || tag === "TEXTAREA") return
        if ((e.key === "Delete" || e.key === "Backspace") && sel?.type === "edge") {
          e.preventDefault()
          removeEdge(sel.id)
        } else if (e.key === "Escape") setSel(null)
      }}
    >
      <Bar>
        <KindChips kinds={space.kinds} shown={shownKinds} set={(k) => void saveView({ kinds: k })} />
      </Bar>
      <Bar>
        <span className="text-muted-foreground text-xs">Edges</span>
        {RELS.map((r) => {
          const on = rels.includes(r.rel)
          return (
            <Toggle
              key={r.rel}
              size="sm"
              pressed={on}
              onPressedChange={() => void saveView({ rel: on ? rels.filter((x) => x !== r.rel) : [...rels, r.rel] })}
            >
              <svg width="22" height="6" aria-hidden="true">
                <line x1="1" y1="3" x2="21" y2="3" className={r.line} strokeWidth="2" strokeDasharray={r.dash} />
              </svg>
              {r.name}
            </Toggle>
          )
        })}
        <Toggle
          size="sm"
          pressed={showUnlinked}
          title={
            showUnlinked ? "Hide items with no links drawn here" : "Show items with no links drawn here, to link them"
          }
          onPressedChange={() => void saveView({ filter: { ...(view.filter ?? {}), unlinked: !showUnlinked } })}
        >
          unlinked ({unlinked})
        </Toggle>
        <span className="ml-auto text-muted-foreground text-xs">New links</span>
        <Pick
          label="Relation for new links"
          value={newRel}
          onChange={setNewRel}
          options={RELS.map((r) => ({ value: r.rel, label: r.name }))}
        />
        <Pick
          label="Lanes"
          value={lanes}
          onChange={(v) => void saveView({ group_by: v || null })}
          options={[
            { value: "", label: "No lanes" },
            { value: "kind", label: "Lanes by kind" },
            { value: "stage", label: "Lanes by stage" },
            { value: "parent", label: "Lanes by parent" },
          ]}
        />
        <Button variant="outline" size="sm" onClick={() => tidyNow("LR")} title="Lay it out left to right">
          Tidy <Icon name="arrow-right" />
        </Button>
        <Button variant="outline" size="sm" onClick={() => tidyNow("TB")} title="Lay it out top to bottom">
          Tidy <Icon name="arrow-down" />
        </Button>
      </Bar>
      <div className="relative min-h-0 flex-1">
        <svg
          ref={svg}
          className="work-flow block h-full w-full bg-background"
          role="img"
          aria-label="Flow"
          onPointerDown={down}
          onPointerMove={move}
          onPointerUp={up}
        >
          <defs>
            <pattern id={`${uid}-grid`} width="20" height="20" patternUnits="userSpaceOnUse">
              <circle cx="1" cy="1" r="1" className="fill-border" />
            </pattern>
            {RELS.map((r) => (
              <marker
                key={r.rel}
                id={`${uid}-${r.rel}`}
                viewBox="0 0 10 10"
                refX="9"
                refY="5"
                markerWidth="7"
                markerHeight="7"
                orient="auto-start-reverse"
              >
                <path d="M0,0 L10,5 L0,10 z" className={r.head} />
              </marker>
            ))}
          </defs>
          <g transform={`translate(${vp.x} ${vp.y}) scale(${vp.k})`}>
            <rect x={-50000} y={-50000} width={100000} height={100000} fill={`url(#${uid}-grid)`} />
            {laneBoxes.map(([g, b]) => (
              <g key={g}>
                <rect
                  x={b.x1 - LANE_PAD}
                  y={b.y1 - LANE_PAD * 2}
                  width={b.x2 - b.x1 + LANE_PAD * 2}
                  height={b.y2 - b.y1 + LANE_PAD * 3}
                  rx={8}
                  className="fill-muted/40 stroke-border"
                />
                <text x={b.x1 - LANE_PAD + 8} y={b.y1 - LANE_PAD * 2 + 15} className="fill-muted-foreground text-xs">
                  {clip(groupLabel(g), 40)}
                </text>
              </g>
            ))}
            {edges.map((e) => {
              const a = nodeById.get(e.from)
              const b = nodeById.get(e.to)
              if (!a || !b) return null
              const r = RELS.find((x) => x.rel === e.rel)
              const c = curve(a, b)
              const on = selected?.type === "edge" && selected.id === e.id
              return (
                <g key={e.id} data-edge={e.id}>
                  <path d={c.d} fill="none" stroke="transparent" strokeWidth={12} />
                  <path
                    d={c.d}
                    fill="none"
                    className={on ? "stroke-ring" : r?.line}
                    strokeWidth={on ? 3 : 1.6}
                    strokeDasharray={r?.dash}
                    markerEnd={`url(#${uid}-${e.rel})`}
                  />
                </g>
              )
            })}
            {nodes.map((n) => {
              const k = kinds.get(n.card.kind)
              const on = selected?.type === "node" && selected.id === n.card.id
              const sub = [k?.label ?? n.card.kind, n.card.stage ? label(n.card.stage) : ""].filter(Boolean).join(" · ")
              return (
                <g key={n.card.id} data-node={n.card.id} transform={`translate(${n.x} ${n.y})`}>
                  <rect
                    width={W}
                    height={H}
                    rx={6}
                    className={on ? "fill-card stroke-ring" : "fill-card stroke-border"}
                    strokeWidth={on ? 2 : 1}
                  />
                  <g data-work-color={k?.color}>
                    <rect width={4} height={H} rx={2} style={{ fill: "var(--work-color, var(--border))" }} />
                  </g>
                  <text x={14} y={22} className="fill-foreground text-sm">
                    {clip(n.card.title || "Untitled", 24)}
                  </text>
                  <text x={14} y={40} className="fill-muted-foreground text-xs">
                    {clip(sub, 30)}
                  </text>
                  {on && (
                    <circle
                      data-handle
                      cx={W}
                      cy={H / 2}
                      r={6}
                      className="fill-primary stroke-background"
                      strokeWidth={2}
                    >
                      <title>Drag to another node to link them</title>
                    </circle>
                  )}
                </g>
              )
            })}
            {wire &&
              (() => {
                const a = nodeById.get(wire.from)
                if (!a) return null
                return (
                  <line
                    x1={a.x + W}
                    y1={a.y + H / 2}
                    x2={wire.x}
                    y2={wire.y}
                    className="stroke-ring"
                    strokeWidth={2}
                    strokeDasharray="5 4"
                  />
                )
              })()}
          </g>
        </svg>
        {!nodes.length && (
          <p className="absolute inset-x-0 top-8 text-center text-muted-foreground text-sm">
            Nothing to draw: choose kinds above, or show the unlinked items.
          </p>
        )}
        {nodes.length > 0 && !edges.length && (
          <p className="absolute inset-x-0 bottom-3 text-center text-muted-foreground text-xs">
            No links yet: select a node, then drag from its dot to another node.
          </p>
        )}
        {sel?.type === "edge" && (
          <div className="absolute top-2 left-2 flex items-center gap-2 rounded-md border bg-popover px-2 py-1 text-sm shadow-sm">
            <span>
              {(() => {
                const [from, rel, to] = sel.id.split(">")
                return `${titleOf(from)} ${RELS.find((r) => r.rel === rel)?.name ?? rel} ${titleOf(to)}`
              })()}
            </span>
            <Button variant="outline" size="sm" onClick={() => removeEdge(sel.id)}>
              <Icon name="trash-2" /> Remove link
            </Button>
          </div>
        )}
        <div className="absolute right-2 bottom-2 flex items-center gap-1 rounded-md border bg-popover p-0.5 shadow-sm">
          <Button
            variant="ghost"
            size="icon-xs"
            title="Zoom out"
            aria-label="Zoom out"
            onClick={() => setVp((v) => ({ ...v, k: Math.max(0.2, v.k / 1.2) }))}
          >
            <Icon name="minus" />
          </Button>
          <span className="w-10 text-center text-muted-foreground text-xs">{Math.round(vp.k * 100)}%</span>
          <Button
            variant="ghost"
            size="icon-xs"
            title="Zoom in"
            aria-label="Zoom in"
            onClick={() => setVp((v) => ({ ...v, k: Math.min(2.5, v.k * 1.2) }))}
          >
            <Icon name="plus" />
          </Button>
          <Button variant="ghost" size="icon-xs" title="Fit" aria-label="Fit" onClick={fit}>
            <Icon name="maximize" />
          </Button>
        </div>
      </div>
    </div>
  )
}
