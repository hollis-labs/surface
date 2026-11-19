/**
 * Diagram's window: the diagrams on the left, the canvas in the middle, the inspector on the right
 * when something is selected. Double-click the canvas for a node; drag a node (or a lane, with its
 * nodes) to move it; drag from the dot on a selected node to another node for an edge. ⌘S saves a
 * revision; Compare shows any revision against what is on the canvas. Ported from Basalisk's
 * Diagram window, with the drawings in the module's own store.
 */
import { ApiError, Icon, type ModuleWeb, useEvent, type ViewProps } from "@hollis-labs/surface-page/sdk"
import {
  type ReactNode,
  type PointerEvent as ReactPointerEvent,
  useCallback,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
} from "react"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Textarea } from "@/components/ui/textarea"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { buildSvg, download, svgToPng } from "./export"
import { type Direction, fromBody, tidy } from "./layout"
import {
  boxOf,
  compare,
  type DEdge,
  type DGroup,
  type Diagram,
  type DNode,
  type EdgeStyle,
  empty,
  freshId,
  KINDS,
  type NodeKind,
  SIDES,
  type Side,
  STYLE_LABEL,
  STYLES,
  serialize,
  THEMES,
  type Theme,
  TONES,
  type Tone,
} from "./model"
import { Defs, extent, ICON_NAMES, LEGEND_H, laneHeads, legendOf, Scene } from "./render"
import { routeAll } from "./route"
import { TEMPLATES, type Template, templateBody } from "./templates"
import { paletteFor } from "./theme"
import "./diagram.css"

interface Summary {
  id: string
  title: string
  rev: number
  updated: string
}
interface Doc extends Summary {
  body: string
}
interface Revision {
  n: number
  title: string
  created: string
}
type Sel = { type: "node" | "edge" | "group"; id: string } | null
interface View {
  x: number
  y: number
  k: number
}

/** A labelled choice from a short list, on the registry's Select. */
function Pick<T extends string>({
  label,
  value,
  options,
  onChange,
  className,
}: {
  label: string
  className?: string
  value: T | ""
  options: { value: T | ""; label: string }[]
  onChange: (v: T | "") => void
}) {
  return (
    <div className={className ?? "flex min-w-0 flex-1 flex-col gap-1"}>
      {label && <span className="text-xs text-muted-foreground">{label}</span>}
      <Select<string>
        value={value as string}
        onValueChange={(v) => onChange(String(v ?? "") as T | "")}
        items={options}
      >
        <SelectTrigger size="sm" aria-label={label || "Revision"} className="w-full">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {options.map((o) => (
            <SelectItem key={o.value || "none"} value={o.value}>
              {o.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  )
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-1">
      <span className="text-xs text-muted-foreground">{label}</span>
      {children}
    </div>
  )
}

function DiagramWindow({ win, desk, api }: ViewProps) {
  const uid = `d${useId().replace(/[^a-zA-Z0-9]/g, "")}`
  const [list, setList] = useState<Summary[]>([])
  const [id, setId] = useState(typeof win.args.id === "string" ? win.args.id : "")
  const [listOpen, setListOpen] = useState(!id)
  const [doc, setDoc] = useState<Doc | null>(null)
  const [revs, setRevs] = useState<Revision[]>([])
  const [dia, setDia] = useState<Diagram>(empty)
  const [title, setTitle] = useState("")
  const [sel, setSel] = useState<Sel>(null)
  const [view, setView] = useState<View>({ x: 40, y: 30, k: 1 })
  const [wire, setWire] = useState<{ from: string; x: number; y: number } | null>(null)
  const [comparing, setComparing] = useState(false)
  const [from, setFrom] = useState(0)
  const [before, setBefore] = useState<Diagram | null>(null)
  const [status, setStatus] = useState<{ tone: "ok" | "warn"; text: string }>({ tone: "ok", text: "" })
  const svg = useRef<SVGSVGElement>(null)
  const root = useRef<HTMLDivElement>(null)
  const fitted = useRef("")
  const drag = useRef<{
    kind: "pan" | "node" | "group"
    id?: string
    sx: number
    sy: number
    ox: number
    oy: number
    origin?: Map<string, { x: number; y: number }>
    moved: boolean
  } | null>(null)
  const idRef = useRef(id)
  idRef.current = id
  const dirty = !!doc && (serialize(dia) !== serialize(fromBody(doc.body)) || title !== doc.title)
  const dirtyRef = useRef(dirty)
  dirtyRef.current = dirty
  const theme: Theme = dia.theme ?? "app"

  // The app look follows the app's palette: draw again when it changes.
  const [paletteTick, setPaletteTick] = useState(0)
  useEffect(() => {
    const o = new MutationObserver(() => setPaletteTick((t) => t + 1))
    o.observe(document.documentElement, { attributes: true, attributeFilter: ["data-palette", "style", "class"] })
    return () => o.disconnect()
  }, [])
  // biome-ignore lint/correctness/useExhaustiveDependencies: a new app palette makes a new app look
  const pal = useMemo(() => paletteFor(theme, dia.background), [theme, dia.background, paletteTick])

  const warn = useCallback(
    (e: unknown) => setStatus({ tone: "warn", text: e instanceof Error ? e.message : "That did not work." }),
    [],
  )
  const loadList = useCallback(() => api.get<Summary[]>("diagrams").then(setList).catch(warn), [api, warn])
  const load = useCallback(
    async (which: string, keepEdits = false) => {
      if (!which) return
      try {
        const r = await api.get<{ diagram: Doc; revisions: Revision[] }>(`diagrams/${which}`)
        if (idRef.current !== which) return
        setRevs(r.revisions)
        if (keepEdits && dirtyRef.current) {
          setDoc((old) => {
            if (old && r.diagram.rev > old.rev)
              setStatus({ tone: "warn", text: `Revision ${r.diagram.rev} was saved elsewhere while you were editing.` })
            return old
          })
          return
        }
        setDoc(r.diagram)
        resetHistory.current = true
        setDia(fromBody(r.diagram.body))
        setTitle(r.diagram.title)
      } catch (e) {
        warn(e)
      }
    },
    [api, warn],
  )
  useEffect(() => {
    void loadList()
  }, [loadList])
  useEffect(() => {
    setDoc(null)
    setSel(null)
    setComparing(false)
    setBefore(null)
    setFrom(0)
    void load(id)
  }, [id, load])
  useEvent("diagram.changed", (ev: { data: unknown }) => {
    void loadList()
    if ((ev.data as { id?: string })?.id === idRef.current) void load(idRef.current, true)
  })
  useEffect(() => {
    void desk.setTitle(win.id, doc ? `${dirty ? "● " : ""}Diagram: ${doc.title}` : "Diagram")
    desk.setDirty(win.id, dirty)
    desk.report(win.id, { kind: "data", ref: doc ? `diagram:${doc.id}` : "diagram:list" })
  }, [doc, dirty, desk, win.id])

  const open = (next: string) => {
    setId(next)
    setListOpen(false)
    void desk.setArgs(win.id, { id: next })
  }

  // The comparison's other side: an older revision.
  const fromRev = from || (doc ? Math.max(1, doc.rev - (dirty ? 0 : 1)) : 0)
  useEffect(() => {
    if (!comparing || !doc) return
    if (fromRev === doc.rev) return setBefore(fromBody(doc.body))
    let live = true
    void api
      .get<{ body: string }>(`diagrams/${doc.id}/revisions/${fromRev}`)
      .then((r) => live && setBefore(fromBody(r.body)))
      .catch(warn)
    return () => {
      live = false
    }
  }, [comparing, fromRev, doc, api, warn])

  const save = async () => {
    if (!doc) return
    try {
      const d = await api.put<Doc>(`diagrams/${doc.id}`, { body: serialize(dia), title, rev: doc.rev })
      setDoc(d)
      setDia(fromBody(d.body))
      setStatus({ tone: "ok", text: `Saved as revision ${d.rev}.` })
      void load(d.id)
      void loadList()
    } catch (e) {
      if (e instanceof ApiError && e.status === 409)
        setStatus({ tone: "warn", text: "It changed elsewhere since you opened it: reload it, or copy your change." })
      else warn(e)
    }
  }
  const create = async (t?: Template) => {
    try {
      const d = await api.post<Doc>("diagrams", {
        title: !t || t.id === "blank" ? "Untitled diagram" : t.label,
        body: t ? templateBody(t) : serialize(empty()),
      })
      await loadList()
      open(d.id)
    } catch (e) {
      warn(e)
    }
  }
  const removeDoc = async (d: Summary) => {
    const ok = await desk.confirm({
      title: `Delete “${d.title}”?`,
      message: `All ${d.rev} revision(s) go with it.`,
      confirmLabel: "Delete",
      danger: true,
    })
    if (!ok) return
    try {
      await api.del(`diagrams/${d.id}`)
      if (d.id === id) {
        setId("")
        setListOpen(true)
      }
      void loadList()
    } catch (e) {
      warn(e)
    }
  }

  // Canvas coordinates of a pointer.
  const at = (e: { clientX: number; clientY: number }) => {
    const box = svg.current?.getBoundingClientRect()
    return { x: (e.clientX - (box?.left ?? 0) - view.x) / view.k, y: (e.clientY - (box?.top ?? 0) - view.y) / view.k }
  }
  const patchNode = (nid: string, change: Partial<DNode>) =>
    setDia((d) => ({ ...d, nodes: d.nodes.map((n) => (n.id === nid ? { ...n, ...change } : n)) }))
  const patchEdge = (eid: string, change: Partial<DEdge>) =>
    setDia((d) => ({ ...d, edges: d.edges.map((e) => (e.id === eid ? { ...e, ...change } : e)) }))
  const patchGroup = (gid: string, change: Partial<DGroup>) =>
    setDia((d) => ({ ...d, groups: d.groups.map((g) => (g.id === gid ? { ...g, ...change } : g)) }))
  const addNode = (x: number, y: number) => {
    const nid = freshId("node", (i) => dia.nodes.some((n) => n.id === i))
    setDia((d) => ({
      ...d,
      nodes: [...d.nodes, { id: nid, label: "New node", kind: "service", x: x - 100, y: y - 24 }],
    }))
    setSel({ type: "node", id: nid })
  }
  const removeSel = () => {
    if (!sel || sel.type === "group") return
    setDia((d) =>
      sel.type === "node"
        ? {
            ...d,
            nodes: d.nodes.filter((n) => n.id !== sel.id),
            edges: d.edges.filter((e) => e.from !== sel.id && e.to !== sel.id),
          }
        : { ...d, edges: d.edges.filter((e) => e.id !== sel.id) },
    )
    setSel(null)
  }

  const down = (e: ReactPointerEvent<SVGSVGElement>) => {
    if (e.button !== 0) return
    root.current?.focus({ preventScroll: true })
    const target = e.target as Element
    const handle = target.closest("[data-handle]")
    const node = target.closest<SVGGElement>("[data-node]")
    const edge = target.closest<SVGGElement>("[data-edge]")
    const lane = target.closest<SVGGElement>("[data-group]")
    e.currentTarget.setPointerCapture(e.pointerId)
    if (handle && node && !comparing) {
      const p = at(e)
      setWire({ from: node.dataset.node ?? "", x: p.x, y: p.y })
      return
    }
    if (node) {
      const nid = node.dataset.node ?? ""
      const n = dia.nodes.find((x) => x.id === nid)
      setSel({ type: "node", id: nid })
      if (n && !comparing)
        drag.current = { kind: "node", id: nid, sx: e.clientX, sy: e.clientY, ox: n.x, oy: n.y, moved: false }
      return
    }
    if (edge) return setSel({ type: "edge", id: edge.dataset.edge ?? "" })
    if (lane && !comparing) {
      const gid = lane.dataset.group ?? ""
      setSel({ type: "group", id: gid })
      drag.current = {
        kind: "group",
        id: gid,
        sx: e.clientX,
        sy: e.clientY,
        ox: 0,
        oy: 0,
        origin: new Map(dia.nodes.filter((n) => n.group === gid).map((n) => [n.id, { x: n.x, y: n.y }])),
        moved: false,
      }
      return
    }
    setSel(null)
    drag.current = { kind: "pan", sx: e.clientX, sy: e.clientY, ox: view.x, oy: view.y, moved: false }
  }
  const move = (e: ReactPointerEvent<SVGSVGElement>) => {
    if (wire) {
      const p = at(e)
      return setWire({ ...wire, x: p.x, y: p.y })
    }
    const d = drag.current
    if (!d) return
    const dx = e.clientX - d.sx
    const dy = e.clientY - d.sy
    if (Math.abs(dx) + Math.abs(dy) > 2) d.moved = true
    if (d.kind === "pan") setView((v) => ({ ...v, x: d.ox + dx, y: d.oy + dy }))
    else if (d.kind === "node" && d.id) patchNode(d.id, { x: d.ox + dx / view.k, y: d.oy + dy / view.k })
    else if (d.kind === "group" && d.origin && d.moved) {
      const o = d.origin
      setDia((dd) => ({
        ...dd,
        nodes: dd.nodes.map((n) => {
          const p = o.get(n.id)
          return p ? { ...n, x: p.x + dx / view.k, y: p.y + dy / view.k } : n
        }),
      }))
    }
  }
  const up = (e: ReactPointerEvent<SVGSVGElement>) => {
    if (wire) {
      const p = at(e)
      const to = dia.nodes.find((n) => {
        const b = boxOf(n)
        return n.id !== wire.from && p.x >= b.x && p.x <= b.x + b.w && p.y >= b.y && p.y <= b.y + b.h
      })
      if (to && !dia.edges.some((x) => x.from === wire.from && x.to === to.id)) {
        const eid = freshId(`${wire.from}-${to.id}`, (i) => dia.edges.some((x) => x.id === i))
        setDia((d) => ({ ...d, edges: [...d.edges, { id: eid, from: wire.from, to: to.id }] }))
        setSel({ type: "edge", id: eid })
      }
      setWire(null)
    }
    drag.current = null
  }
  // The wheel zooms around the pointer; a trackpad's two-finger move pans (a native listener:
  // React's is passive).
  useEffect(() => {
    const el = svg.current
    if (!el) return
    const onWheel = (e: WheelEvent) => {
      e.preventDefault()
      const box = el.getBoundingClientRect()
      setView((v) => {
        if (
          !e.ctrlKey &&
          !e.metaKey &&
          Math.abs(e.deltaX) + Math.abs(e.deltaY) < 40 &&
          e.deltaMode === 0 &&
          !Number.isInteger(e.deltaY)
        )
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
  const zoomBy = (f: number) => {
    const box = svg.current?.getBoundingClientRect()
    const px = (box?.width ?? 800) / 2
    const py = (box?.height ?? 600) / 2
    setView((v) => {
      const k = Math.min(2.5, Math.max(0.2, v.k * f))
      return { k, x: px - ((px - v.x) / v.k) * k, y: py - ((py - v.y) / v.k) * k }
    })
  }
  const fit = useCallback(() => {
    const box = svg.current?.getBoundingClientRect()
    if (!box || !dia.nodes.length) return setView({ x: 40, y: 30, k: 1 })
    const ext = extent(dia)
    const h = ext.h + (legendOf(dia).length ? LEGEND_H + 34 : 0)
    const k = Math.min(1.4, Math.max(0.2, Math.min((box.width - 80) / ext.w, (box.height - 80) / h)))
    setView({ k, x: (box.width - ext.w * k) / 2 - ext.x * k, y: (box.height - h * k) / 2 - ext.y * k })
  }, [dia])
  const fitRef = useRef(fit)
  fitRef.current = fit
  useEffect(() => {
    if (doc && fitted.current !== doc.id) {
      fitted.current = doc.id
      requestAnimationFrame(() => fitRef.current())
    }
  }, [doc])

  const diff = useMemo(() => (comparing && before ? compare(before, dia) : null), [comparing, before, dia])
  const routes = useMemo(
    () =>
      routeAll([...(diff?.goneNodes ?? []), ...dia.nodes], [...(diff?.goneEdges ?? []), ...dia.edges], laneHeads(dia)),
    [dia, diff],
  )
  const counts = useMemo(() => {
    const c = { added: 0, removed: 0, changed: 0, moved: 0 }
    if (diff) for (const v of [...diff.nodes.values(), ...diff.edges.values()]) if (v !== "same") c[v]++
    return c
  }, [diff])
  const node = sel?.type === "node" ? dia.nodes.find((n) => n.id === sel.id) : undefined
  const edge = sel?.type === "edge" ? dia.edges.find((e) => e.id === sel.id) : undefined
  const group = sel?.type === "group" ? dia.groups.find((g) => g.id === sel.id) : undefined

  // ---- Undo and redo: snapshots of the diagram, a burst of changes (a drag, typing) as one ----
  const past = useRef<string[]>([])
  const future = useRef<string[]>([])
  const snap = useRef("")
  const restoring = useRef(false)
  const resetHistory = useRef(true)
  const [histTick, setHistTick] = useState(0)
  const pending = useRef<number | null>(null)
  const record = useCallback(() => {
    pending.current = null
    const cur = JSON.stringify(dia)
    if (cur === snap.current) return
    if (snap.current) past.current = [...past.current.slice(-99), snap.current]
    future.current = []
    snap.current = cur
    setHistTick((t) => t + 1)
  }, [dia])
  useEffect(() => {
    if (resetHistory.current) {
      resetHistory.current = false
      past.current = []
      future.current = []
      snap.current = JSON.stringify(dia)
      setHistTick((t) => t + 1)
      return
    }
    if (restoring.current) {
      restoring.current = false
      snap.current = JSON.stringify(dia)
      return
    }
    if (pending.current) window.clearTimeout(pending.current)
    pending.current = window.setTimeout(record, 400)
    return () => {
      if (pending.current) window.clearTimeout(pending.current)
    }
  }, [dia, record])
  const flush = () => {
    if (pending.current) {
      window.clearTimeout(pending.current)
      record()
    }
  }
  const undo = () => {
    flush()
    const prev = past.current.pop()
    if (!prev) return
    future.current.push(snap.current)
    snap.current = prev
    restoring.current = true
    setDia(JSON.parse(prev) as Diagram)
    setHistTick((t) => t + 1)
  }
  const redo = () => {
    flush()
    const next = future.current.pop()
    if (!next) return
    past.current.push(snap.current)
    snap.current = next
    restoring.current = true
    setDia(JSON.parse(next) as Diagram)
    setHistTick((t) => t + 1)
  }
  void histTick
  const canUndo = past.current.length > 0 || (!!pending.current && JSON.stringify(dia) !== snap.current)
  const canRedo = future.current.length > 0

  // ---- Export: SVG, or PNG at 1, 2 or 3 times, as a download ----
  const exportAs = async (format: "svg" | "png", scale = 2) => {
    try {
      const built = buildSvg(dia, pal, { title, background: true, grid: false, legend: true })
      const stem =
        title
          .toLowerCase()
          .replace(/[^a-z0-9]+/g, "-")
          .replace(/^-+|-+$/g, "") || "diagram"
      if (format === "svg") download(`${stem}.svg`, built.svg, "image/svg+xml")
      else download(`${stem}.png`, await svgToPng(built.svg, built.w, built.h, scale), "image/png")
      setStatus({ tone: "ok", text: `Exported ${stem}.${format}.` })
    } catch (e) {
      warn(e)
    }
  }

  const side = !!doc && !!sel && !comparing

  return (
    <div
      ref={root}
      className="flex h-full min-h-0 bg-background outline-none"
      tabIndex={-1}
      onKeyDown={(e) => {
        const t = e.target as HTMLElement
        const typing = t.tagName === "INPUT" || t.tagName === "TEXTAREA" || t.isContentEditable
        if ((e.metaKey || e.ctrlKey) && e.key === "s") {
          e.preventDefault()
          if (dirty) void save()
        } else if (!typing && (e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "z") {
          e.preventDefault()
          if (e.shiftKey) redo()
          else undo()
        } else if (!typing && !comparing && (e.key === "Delete" || e.key === "Backspace") && sel) {
          e.preventDefault()
          removeSel()
        } else if (!typing && e.key === "Escape") setSel(null)
      }}
    >
      {listOpen && (
        <nav className="flex w-64 shrink-0 flex-col border-r">
          <header className="flex items-center gap-1 border-b px-2 py-1.5">
            <span className="text-sm font-semibold">Diagrams</span>
            <span className="ml-auto" />
            <NewMenu onCreate={(t) => void create(t)} />
            {doc && (
              <Button variant="ghost" size="icon-xs" aria-label="Hide the list" onClick={() => setListOpen(false)}>
                <Icon name="x" />
              </Button>
            )}
          </header>
          <div className="min-h-0 flex-1 overflow-auto p-1">
            {list.map((d) => (
              <div key={d.id} className="group flex items-center gap-1">
                <Button
                  variant={d.id === id ? "secondary" : "ghost"}
                  size="sm"
                  className="min-w-0 flex-1 justify-between"
                  onClick={() => open(d.id)}
                >
                  <span className="truncate">{d.title}</span>
                  <span className="text-xs text-muted-foreground">r{d.rev}</span>
                </Button>
                <Button
                  variant="ghost"
                  size="icon-xs"
                  aria-label={`Delete ${d.title}`}
                  onClick={() => void removeDoc(d)}
                >
                  <Icon name="trash-2" />
                </Button>
              </div>
            ))}
            {!list.length && (
              <p className="p-3 text-xs text-muted-foreground">
                No diagrams yet. Start one with +, or ask the agent to draw how something fits together.
              </p>
            )}
          </div>
        </nav>
      )}

      {doc ? (
        <section className="flex min-w-0 flex-1 flex-col">
          <div className="flex shrink-0 flex-wrap items-center gap-1.5 border-b px-2 py-1.5">
            <Button
              variant={listOpen ? "secondary" : "ghost"}
              size="icon-sm"
              aria-label="Diagrams"
              aria-pressed={listOpen}
              onClick={() => setListOpen((o) => !o)}
            >
              <Icon name="panel-left" />
            </Button>
            <Input className="h-8 w-56" value={title} aria-label="Title" onChange={(e) => setTitle(e.target.value)} />
            <span className="text-xs text-muted-foreground">
              r{doc.rev} · {dia.nodes.length} nodes · {dia.edges.length} edges
            </span>
            <span className="ml-auto" />
            <ToggleGroup
              size="sm"
              spacing={0}
              value={[theme]}
              aria-label="Look"
              onValueChange={(v: string[]) =>
                v[0] && setDia((d) => ({ ...d, theme: v[0] === "app" ? undefined : (v[0] as Theme) }))
              }
            >
              {THEMES.map((t) => (
                <ToggleGroupItem
                  key={t}
                  value={t}
                  title={t === "app" ? "The app's palette (the default)" : `The ${t} look`}
                >
                  {t}
                </ToggleGroupItem>
              ))}
            </ToggleGroup>
            {!comparing && (
              <>
                <Button
                  variant="outline"
                  size="sm"
                  title="Add a node (or double-click the canvas)"
                  onClick={() => {
                    const box = svg.current?.getBoundingClientRect()
                    addNode(((box?.width ?? 600) / 2 - view.x) / view.k, ((box?.height ?? 400) / 2 - view.y) / view.k)
                  }}
                >
                  <Icon name="plus" /> Node
                </Button>
                {(["LR", "TB"] as Direction[]).map((d) => (
                  <Button
                    key={d}
                    variant="ghost"
                    size="sm"
                    title={d === "LR" ? "Lay it out left to right" : "Lay it out top to bottom"}
                    onClick={() => {
                      setDia((x) => tidy(x, d))
                      requestAnimationFrame(() => fitRef.current())
                    }}
                  >
                    {d === "LR" ? "Tidy →" : "Tidy ↓"}
                  </Button>
                ))}
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label="Undo"
                  title="Undo (⌘Z)"
                  disabled={!canUndo}
                  onClick={undo}
                >
                  <Icon name="undo-2" />
                </Button>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label="Redo"
                  title="Redo (⇧⌘Z)"
                  disabled={!canRedo}
                  onClick={redo}
                >
                  <Icon name="redo-2" />
                </Button>
              </>
            )}
            <Button
              variant={comparing ? "secondary" : "ghost"}
              size="sm"
              aria-pressed={comparing}
              title="Compare with another revision"
              onClick={() => setComparing((c) => !c)}
            >
              <Icon name="git-compare" /> Compare
            </Button>
            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                  <Button variant="ghost" size="sm" title="Export as SVG or PNG">
                    <Icon name="download" /> Export
                  </Button>
                }
              />
              <DropdownMenuContent align="end">
                <DropdownMenuItem onClick={() => void exportAs("svg")}>SVG</DropdownMenuItem>
                <DropdownMenuItem onClick={() => void exportAs("png", 1)}>PNG, 1×</DropdownMenuItem>
                <DropdownMenuItem onClick={() => void exportAs("png", 2)}>PNG, 2×</DropdownMenuItem>
                <DropdownMenuItem onClick={() => void exportAs("png", 3)}>PNG, 3×</DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
            <Button size="sm" disabled={!dirty} title="Save a new revision (⌘S)" onClick={() => void save()}>
              <Icon name="save" /> Save
            </Button>
          </div>

          {comparing && (
            <div className="flex shrink-0 flex-wrap items-center gap-2 border-b px-3 py-1.5 text-xs">
              <span>Compare</span>
              <Pick
                label=""
                className="w-36"
                value={String(fromRev)}
                options={revs.map((r) => ({ value: String(r.n), label: `revision ${r.n}` }))}
                onChange={(v) => setFrom(Number(v) || 0)}
              />
              <span className="text-muted-foreground">with what is on the canvas:</span>
              <span className="text-success">{counts.added} added</span>
              <span className="text-warning">{counts.changed} changed</span>
              <span className="text-destructive">{counts.removed} removed</span>
              <span className="text-muted-foreground">{counts.moved} moved</span>
            </div>
          )}

          <div className="flex min-h-0 flex-1">
            <div className="relative min-w-0 flex-1">
              <svg
                ref={svg}
                className="dia-canvas size-full"
                data-comparing={comparing}
                role="img"
                aria-label={`Diagram: ${title}`}
                style={{ background: pal.bg }}
                onPointerDown={down}
                onPointerMove={move}
                onPointerUp={up}
                onDoubleClick={(e) => {
                  if (comparing || (e.target as Element).closest("[data-node],[data-edge]")) return
                  const p = at(e)
                  addNode(p.x, p.y)
                }}
              >
                <Defs uid={uid} pal={pal} grid={dia.grid ?? "lines"} />
                <g transform={`translate(${view.x} ${view.y}) scale(${view.k})`}>
                  {(dia.grid ?? "lines") !== "none" && (
                    <rect x={-50000} y={-50000} width={100000} height={100000} fill={`url(#${uid}-grid)`} />
                  )}
                  <Scene
                    dia={dia}
                    pal={pal}
                    uid={uid}
                    routes={routes}
                    gone={diff ? { nodes: diff.goneNodes, edges: diff.goneEdges } : undefined}
                    changes={diff ?? undefined}
                    sel={sel}
                    handleOn={sel?.type === "node" && !comparing ? sel.id : undefined}
                  />
                  {wire &&
                    (() => {
                      const a = dia.nodes.find((n) => n.id === wire.from)
                      if (!a) return null
                      const b = boxOf(a)
                      return (
                        <line
                          x1={b.x + b.w}
                          y1={b.y + b.h / 2}
                          x2={wire.x}
                          y2={wire.y}
                          stroke={pal.select}
                          strokeWidth={2}
                          strokeDasharray="5 4"
                        />
                      )
                    })()}
                </g>
              </svg>
              <div className="absolute bottom-2 left-2 flex items-center gap-1 rounded-md border bg-popover p-0.5">
                <Button variant="ghost" size="icon-xs" aria-label="Zoom out" onClick={() => zoomBy(1 / 1.2)}>
                  <Icon name="minus" />
                </Button>
                <span className="w-10 text-center text-xs tabular-nums">{Math.round(view.k * 100)}%</span>
                <Button variant="ghost" size="icon-xs" aria-label="Zoom in" onClick={() => zoomBy(1.2)}>
                  <Icon name="plus" />
                </Button>
                <Button variant="ghost" size="icon-xs" aria-label="Fit" title="Fit the drawing" onClick={fit}>
                  <Icon name="maximize" />
                </Button>
              </div>
              {status.text && (
                <p
                  className={`absolute right-2 bottom-2 rounded-md border bg-popover px-2 py-1 text-xs ${status.tone === "warn" ? "text-destructive" : "text-muted-foreground"}`}
                >
                  {status.text}
                </p>
              )}
            </div>

            {side && (
              <aside className="flex w-72 shrink-0 flex-col gap-3 overflow-auto border-l p-3">
                <header className="flex items-center">
                  <span className="text-sm font-semibold">{node ? "Node" : edge ? "Edge" : "Lane"}</span>
                  <Button
                    variant="ghost"
                    size="icon-xs"
                    className="ml-auto"
                    aria-label="Close"
                    onClick={() => setSel(null)}
                  >
                    <Icon name="x" />
                  </Button>
                </header>
                {node && (
                  <NodeInspector node={node} dia={dia} setDia={setDia} patch={patchNode} toneColour={pal.tones} />
                )}
                {edge && <EdgeInspector edge={edge} patch={patchEdge} />}
                {group && (
                  <>
                    <Field label="Label">
                      <Input value={group.label} onChange={(e) => patchGroup(group.id, { label: e.target.value })} />
                    </Field>
                    <Field label="Subtitle">
                      <Input
                        value={group.subtitle ?? ""}
                        onChange={(e) => patchGroup(group.id, { subtitle: e.target.value || undefined })}
                      />
                    </Field>
                    <Pick<Tone>
                      label="Tone"
                      value={group.tone ?? ""}
                      options={[{ value: "", label: "(none)" }, ...TONES.map((t) => ({ value: t, label: t }))]}
                      onChange={(v) => patchGroup(group.id, { tone: v || undefined })}
                    />
                  </>
                )}
                {(node || edge) && (
                  <Button variant="outline" size="sm" className="mt-auto" onClick={removeSel}>
                    <Icon name="trash-2" /> Delete the {node ? "node" : "edge"}
                  </Button>
                )}
              </aside>
            )}
          </div>
        </section>
      ) : (
        <Empty className="flex-1">
          <EmptyHeader>
            <EmptyTitle>{id ? "Reading the diagram…" : "No diagram open"}</EmptyTitle>
            <EmptyDescription>Pick one from the list, or start one from a template.</EmptyDescription>
          </EmptyHeader>
          {!id && <NewMenu onCreate={(t) => void create(t)} label="New diagram" />}
        </Empty>
      )}
    </div>
  )
}

function NewMenu({ onCreate, label }: { onCreate: (t: Template) => void; label?: string }) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            variant={label ? "default" : "ghost"}
            size={label ? "sm" : "icon-xs"}
            aria-label="New diagram"
            title="New diagram"
          >
            <Icon name="file-plus-2" />
            {label}
          </Button>
        }
      />
      <DropdownMenuContent align="start" className="w-72">
        <DropdownMenuLabel>Start from</DropdownMenuLabel>
        <DropdownMenuSeparator />
        {TEMPLATES.map((t) => (
          <DropdownMenuItem key={t.id} onClick={() => onCreate(t)}>
            <span className="flex flex-col">
              <span className="font-medium">{t.label}</span>
              <span className="text-xs text-muted-foreground">{t.about}</span>
            </span>
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

function NodeInspector({
  node,
  dia,
  setDia,
  patch,
  toneColour,
}: {
  node: DNode
  dia: Diagram
  setDia: (f: (d: Diagram) => Diagram) => void
  patch: (id: string, change: Partial<DNode>) => void
  toneColour: Record<Tone, string>
}) {
  return (
    <>
      <Field label="Label">
        <Input value={node.label} onChange={(e) => patch(node.id, { label: e.target.value })} />
      </Field>
      <Field label="Subtitle">
        <Input
          value={node.subtitle ?? ""}
          placeholder="a second line"
          onChange={(e) => patch(node.id, { subtitle: e.target.value || undefined })}
        />
      </Field>
      <div className="flex gap-2">
        <Pick<NodeKind>
          label="Kind"
          value={node.kind ?? ""}
          options={[{ value: "", label: "(none)" }, ...KINDS.map((k) => ({ value: k, label: k }))]}
          onChange={(v) => patch(node.id, { kind: v || undefined })}
        />
        <Pick<string>
          label="Icon"
          value={node.icon ?? ""}
          options={[{ value: "", label: "(by kind)" }, ...ICON_NAMES.map((k) => ({ value: k, label: k }))]}
          onChange={(v) => patch(node.id, { icon: v || undefined })}
        />
      </div>
      <Field label="Tone">
        <div className="flex flex-wrap gap-1">
          <Button
            variant={node.tone ? "ghost" : "secondary"}
            size="icon-xs"
            aria-label="By kind"
            title="By kind"
            onClick={() => patch(node.id, { tone: undefined })}
          >
            ∅
          </Button>
          {TONES.map((t) => (
            <Button
              key={t}
              variant={node.tone === t ? "secondary" : "ghost"}
              size="icon-xs"
              aria-label={t}
              title={t}
              onClick={() => patch(node.id, { tone: t })}
            >
              <span className="size-3 rounded-full" style={{ background: toneColour[t] }} />
            </Button>
          ))}
        </div>
      </Field>
      <div className="flex gap-2">
        <Field label="Step">
          <Input
            type="number"
            min={0}
            value={node.step ?? ""}
            onChange={(e) => patch(node.id, { step: Number(e.target.value) || undefined })}
          />
        </Field>
        <Field label="Tag">
          <Input
            value={node.tag ?? ""}
            placeholder="RETRY"
            onChange={(e) => patch(node.id, { tag: e.target.value || undefined })}
          />
        </Field>
      </div>
      <Field label="Rows (one a line)">
        <Textarea
          rows={2}
          value={(node.items ?? []).join("\n")}
          onChange={(e) => {
            const items = e.target.value.split("\n").filter((x, i, a) => x.trim() || i < a.length - 1)
            patch(node.id, { items: items.length ? items : undefined })
          }}
        />
      </Field>
      <span className="flex items-center gap-2 text-sm">
        <Checkbox
          checked={!!node.highlight}
          aria-label="Highlight"
          onCheckedChange={(v) => patch(node.id, { highlight: v === true ? true : undefined })}
        />
        Highlight
      </span>
      <Pick<string>
        label="Lane"
        value={node.group ?? ""}
        options={[{ value: "", label: "(none)" }, ...dia.groups.map((g) => ({ value: g.id, label: g.label }))]}
        onChange={(v) => patch(node.id, { group: v || undefined })}
      />
      <Field label="New lane for it">
        <Input
          placeholder="name, then Enter"
          onKeyDown={(e) => {
            if (e.key !== "Enter") return
            const label = e.currentTarget.value.trim()
            if (!label) return
            const gid = freshId(label, (i) => dia.groups.some((g) => g.id === i))
            setDia((d) => ({
              ...d,
              groups: [...d.groups, { id: gid, label }],
              nodes: d.nodes.map((n) => (n.id === node.id ? { ...n, group: gid } : n)),
            }))
            e.currentTarget.value = ""
          }}
        />
      </Field>
      <Field label="Note">
        <Textarea
          rows={2}
          value={node.note ?? ""}
          onChange={(e) => patch(node.id, { note: e.target.value || undefined })}
        />
      </Field>
      <code className="text-xs text-muted-foreground">{node.id}</code>
    </>
  )
}

function EdgeInspector({ edge, patch }: { edge: DEdge; patch: (id: string, change: Partial<DEdge>) => void }) {
  return (
    <>
      <Field label="Label">
        <Input
          value={edge.label ?? ""}
          placeholder="what flows, or how"
          onChange={(e) => patch(edge.id, { label: e.target.value || undefined })}
        />
      </Field>
      <Pick<EdgeStyle>
        label="Style"
        value={edge.style ?? "flow"}
        options={STYLES.map((s) => ({ value: s, label: `${s} (${STYLE_LABEL[s]})` }))}
        onChange={(v) => patch(edge.id, { style: v === "flow" || !v ? undefined : v })}
      />
      <div className="flex gap-2">
        <Pick<Side>
          label="Leaves by"
          value={edge.fromSide ?? ""}
          options={[{ value: "", label: "auto" }, ...SIDES.map((s) => ({ value: s, label: s }))]}
          onChange={(v) => patch(edge.id, { fromSide: v || undefined })}
        />
        <Pick<Side>
          label="Enters by"
          value={edge.toSide ?? ""}
          options={[{ value: "", label: "auto" }, ...SIDES.map((s) => ({ value: s, label: s }))]}
          onChange={(v) => patch(edge.id, { toSide: v || undefined })}
        />
      </div>
      <span className="flex items-center gap-2 text-sm">
        <Checkbox
          checked={!!edge.both}
          aria-label="Both ways"
          onCheckedChange={(v) => patch(edge.id, { both: v === true ? true : undefined })}
        />
        Both ways
      </span>
      <div className="flex items-center gap-2">
        <code className="text-xs text-muted-foreground">
          {edge.from} → {edge.to}
        </code>
        <Button
          variant="outline"
          size="xs"
          className="ml-auto"
          onClick={() => patch(edge.id, { from: edge.to, to: edge.from, fromSide: edge.toSide, toSide: edge.fromSide })}
        >
          Reverse
        </Button>
      </div>
    </>
  )
}

export default { views: { "diagram/main": DiagramWindow } } satisfies ModuleWeb
