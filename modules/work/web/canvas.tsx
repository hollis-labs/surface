/**
 * Canvas: cards placed freely, stacks (titled piles of cards) and free notes, on a plane you
 * pan (drag the background, or scroll) and zoom (⌘ or Ctrl with scroll, or the buttons).
 * Drag a card from the tray onto the canvas or into a stack, between stacks, or back to the
 * tray to take it off. A stack can be bound to a stage or a parent: dropping a card there
 * also sets that on the item. The arrangement is kept in the view item (layout), patched by
 * id so another window's changes are not written over; the pan and zoom are this window's.
 */
import { Icon } from "@hollis-labs/surface-page/sdk"
import { type PointerEvent as RPointerEvent, useEffect, useMemo, useRef, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Markdown } from "@/components/ui/markdown"
import { Textarea } from "@/components/ui/textarea"
import type { ViewArgs, WorkCanvasNote, WorkCard, WorkLayoutPatch, WorkPos, WorkStack, WorkView } from "./api"
import { Chip } from "./calendar"
import { Bar, ItemCard, KindChips, Pick } from "./parts"
import { label, message, newId } from "./util"

interface Layout {
  cards: Record<string, WorkPos>
  stacks: WorkStack[]
  notes: WorkCanvasNote[]
}
const layoutOf = (v: WorkView): Layout => {
  const l = (v.layout ?? {}) as Partial<Layout>
  return {
    cards: l.cards && typeof l.cards === "object" ? l.cards : {},
    stacks: Array.isArray(l.stacks) ? l.stacks.map((s) => ({ ...s, items: s.items ?? [] })) : [],
    notes: Array.isArray(l.notes) ? l.notes : [],
  }
}

type Drag =
  | { type: "card"; id: string; from: string; dx: number; dy: number; x: number; y: number; moved: boolean }
  | { type: "stack" | "note"; id: string; dx: number; dy: number; x: number; y: number; moved: boolean }
  | { type: "pan"; sx: number; sy: number; px: number; py: number }

export function Canvas({ w, ws, space, view, picked, pick, changed, setError }: ViewArgs & { view: WorkView }) {
  const kinds = useMemo(() => new Map(space.kinds.map((k) => [k.kind, k])), [space.kinds])
  const shownKinds = view.kinds.length ? view.kinds : space.kinds.map((k) => k.kind)
  const [lay, setLay] = useState<Layout>(() => layoutOf(view))
  const [pan, setPan] = useState({ x: 40, y: 40 })
  const [zoom, setZoom] = useState(1)
  const [drag, setDrag] = useState<Drag | null>(null)
  const [editNote, setEditNote] = useState<string | null>(null)
  const [editStack, setEditStack] = useState<string | null>(null)
  const plane = useRef<HTMLDivElement>(null)
  const tray = useRef<HTMLElement>(null)
  const dragRef = useRef<Drag | null>(null)
  dragRef.current = drag

  // Take the view's arrangement when it changes in Caster, unless a drag is under way.
  // biome-ignore lint/correctness/useExhaustiveDependencies: a newer revision of the view resets it
  useEffect(() => {
    if (!dragRef.current) setLay(layoutOf(view))
  }, [view.rev])

  const byId = useMemo(() => new Map(space.items.filter((c) => !c.cut).map((c) => [c.id, c])), [space.items])
  const shown = (c: WorkCard | undefined): c is WorkCard => !!c && shownKinds.includes(c.kind)
  const inStack = new Set(lay.stacks.flatMap((s) => s.items))
  const trayCards = space.items.filter(
    (c) => !c.cut && shownKinds.includes(c.kind) && !lay.cards[c.id] && !inStack.has(c.id),
  )

  const toPlane = (cx: number, cy: number) => {
    const r = plane.current?.getBoundingClientRect()
    return { x: (cx - (r?.left ?? 0) - pan.x) / zoom, y: (cy - (r?.top ?? 0) - pan.y) / zoom }
  }

  const save = async (patch: WorkLayoutPatch) => {
    try {
      await w.layout(ws, view.id, patch)
      setError("")
    } catch (e) {
      setError(message(e))
    }
    changed()
  }

  // A card dropped into a bound stack: set that stage or parent on the item too.
  const bindDrop = async (st: WorkStack, id: string) => {
    const c = byId.get(id)
    if (!st.bind || !c) return
    try {
      if (st.bind.key === "stage") {
        if (c.stage === st.bind.value) return
        if (!kinds.get(c.kind)?.stages.includes(st.bind.value)) {
          setError(
            `A ${kinds.get(c.kind)?.label.toLowerCase() ?? c.kind} has no stage "${st.bind.value}"; it is in the stack, its stage is as it was.`,
          )
          return
        }
        await w.move(ws, id, "stage", st.bind.value)
      } else if (c.parent !== st.bind.value) {
        await w.place(ws, id, st.bind.value, "", "")
      }
    } catch (e) {
      setError(`In the stack, but not changed: ${message(e)}`)
    }
  }

  // ---- Dragging: pointer events on the window while a drag is under way ----
  // biome-ignore lint/correctness/useExhaustiveDependencies: one listener pair a drag; the drag is read through a ref, the rest from the latest render
  useEffect(() => {
    if (!drag) return
    const move = (e: PointerEvent) => {
      const d = dragRef.current
      if (!d) return
      if (d.type === "pan") {
        setPan({ x: d.px + e.clientX - d.sx, y: d.py + e.clientY - d.sy })
        return
      }
      const moved = d.moved || Math.abs(e.clientX - d.x) + Math.abs(e.clientY - d.y) > 4
      setDrag({ ...d, x: e.clientX, y: e.clientY, moved })
    }
    const up = (e: PointerEvent) => {
      const d = dragRef.current
      setDrag(null)
      if (!d || d.type === "pan") return
      if (!d.moved) {
        if (d.type === "card") pick(d.id)
        return
      }
      const p = toPlane(e.clientX - d.dx, e.clientY - d.dy)
      const pos = { x: Math.round(p.x), y: Math.round(p.y) }
      if (d.type === "stack") {
        const st = lay.stacks.find((s) => s.id === d.id)
        if (!st) return
        const next = { ...st, ...pos }
        setLay((l) => ({ ...l, stacks: l.stacks.map((s) => (s.id === d.id ? next : s)) }))
        void save({ stacks: { [d.id]: next } })
        return
      }
      if (d.type === "note") {
        const n = lay.notes.find((x) => x.id === d.id)
        if (!n) return
        const next = { ...n, ...pos }
        setLay((l) => ({ ...l, notes: l.notes.map((x) => (x.id === d.id ? next : x)) }))
        void save({ notes: { [d.id]: next } })
        return
      }
      if (d.type !== "card") return
      // A card: into a stack, back to the tray, or onto the canvas.
      const under = document.elementFromPoint(e.clientX, e.clientY)
      const stackEl = under?.closest<HTMLElement>("[data-stack]")
      const id = d.id
      const out = (l: Layout): Layout => ({
        cards: Object.fromEntries(Object.entries(l.cards).filter(([k]) => k !== id)),
        stacks: l.stacks.map((s) => ({ ...s, items: s.items.filter((x) => x !== id) })),
        notes: l.notes,
      })
      if (stackEl?.dataset.stack) {
        const sid = stackEl.dataset.stack
        const st = lay.stacks.find((s) => s.id === sid)
        if (!st || d.from === sid) return
        const next = { ...st, items: [...st.items.filter((x) => x !== id), id] }
        const patch: WorkLayoutPatch = { stacks: { [sid]: next } }
        if (d.from === "canvas") patch.cards = { [id]: null }
        else if (d.from !== "tray") {
          const was = lay.stacks.find((s) => s.id === d.from)
          if (was) patch.stacks = { ...patch.stacks, [was.id]: { ...was, items: was.items.filter((x) => x !== id) } }
        }
        setLay((l) => {
          const o = out(l)
          return { ...o, stacks: o.stacks.map((s) => (s.id === sid ? next : s)) }
        })
        void save(patch).then(() => bindDrop(next, id).then(changed))
        return
      }
      if (under && tray.current?.contains(under)) {
        if (d.from === "tray") return
        setLay(out)
        if (d.from === "canvas") void save({ cards: { [id]: null } })
        else {
          const st = lay.stacks.find((s) => s.id === d.from)
          if (st) void save({ stacks: { [st.id]: { ...st, items: st.items.filter((x) => x !== id) } } })
        }
        return
      }
      if (under && plane.current?.contains(under)) {
        const patch: WorkLayoutPatch = { cards: { [id]: pos } }
        if (d.from !== "canvas" && d.from !== "tray") {
          const was = lay.stacks.find((s) => s.id === d.from)
          if (was) patch.stacks = { [was.id]: { ...was, items: was.items.filter((x) => x !== id) } }
        }
        setLay((l) => {
          const o = out(l)
          return { ...o, cards: { ...o.cards, [id]: pos } }
        })
        void save(patch)
      }
    }
    window.addEventListener("pointermove", move)
    window.addEventListener("pointerup", up)
    return () => {
      window.removeEventListener("pointermove", move)
      window.removeEventListener("pointerup", up)
    }
  }, [drag !== null, lay, pan, zoom])

  const grab = (
    e: RPointerEvent,
    d: { type: "card"; id: string; from: string } | { type: "stack" | "note"; id: string },
  ) => {
    if (e.button !== 0) return
    e.stopPropagation()
    e.preventDefault()
    // Where it was grabbed, in screen pixels from its corner, so it does not jump.
    const el = e.currentTarget as HTMLElement
    const box = (d.type === "card" ? el : (el.closest("[data-box]") ?? el)).getBoundingClientRect()
    setDrag({
      ...d,
      dx: e.clientX - box.left,
      dy: e.clientY - box.top,
      x: e.clientX,
      y: e.clientY,
      moved: false,
    } as Drag)
  }

  const live = (type: string, id: string) =>
    drag && drag.type === type && "id" in drag && drag.id === id && drag.moved ? drag : null
  const placeOf = (type: "card" | "stack" | "note", id: string, at: WorkPos) => {
    const d = live(type, id)
    if (!d || ("from" in d && d.from !== "canvas")) return at
    return toPlane(d.x - d.dx, d.y - d.dy)
  }

  const addStack = () => {
    const r = plane.current?.getBoundingClientRect()
    const c = toPlane((r?.left ?? 0) + 80, (r?.top ?? 0) + 60)
    const st: WorkStack = { id: newId(), title: "New stack", x: Math.round(c.x), y: Math.round(c.y), items: [] }
    setLay((l) => ({ ...l, stacks: [...l.stacks, st] }))
    setEditStack(st.id)
    void save({ stacks: { [st.id]: st } })
  }
  const addNote = () => {
    const r = plane.current?.getBoundingClientRect()
    const c = toPlane((r?.left ?? 0) + 120, (r?.top ?? 0) + 120)
    const n: WorkCanvasNote = { id: newId(), x: Math.round(c.x), y: Math.round(c.y), text: "" }
    setLay((l) => ({ ...l, notes: [...l.notes, n] }))
    setEditNote(n.id)
    void save({ notes: { [n.id]: n } })
  }
  const zoomBy = (f: number, cx?: number, cy?: number) => {
    const r = plane.current?.getBoundingClientRect()
    const ax = cx ?? (r ? r.left + r.width / 2 : 0)
    const ay = cy ?? (r ? r.top + r.height / 2 : 0)
    const next = Math.min(2.5, Math.max(0.25, zoom * f))
    const p = toPlane(ax, ay)
    setPan({ x: ax - (r?.left ?? 0) - p.x * next, y: ay - (r?.top ?? 0) - p.y * next })
    setZoom(next)
  }

  // What a stack can be bound to: the shown kinds' stages, and the items that hold others.
  const stages = [...new Set(space.kinds.filter((k) => shownKinds.includes(k.kind)).flatMap((k) => k.stages))]
  const parents = space.items.filter((c) => !c.cut && (kinds.get(c.kind)?.children.length ?? 0) > 0)
  const bindOptions = [
    { value: "", label: "Just a pile" },
    ...stages.map((s) => ({ value: `stage:${s}`, label: `Sets stage: ${label(s)}` })),
    ...parents.map((p) => ({ value: `parent:${p.id}`, label: `Puts in: ${p.title}` })),
  ]

  const dragged = drag && drag.type === "card" && drag.moved && drag.from !== "canvas" ? drag : null
  const draggedCard = dragged ? byId.get(dragged.id) : undefined
  const cardDragging = drag?.type === "card" && drag.moved

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <Bar>
        <KindChips
          kinds={space.kinds}
          shown={shownKinds}
          set={async (k) => {
            try {
              await w.saveView(ws, view.id, { kinds: k })
            } catch (e) {
              setError(message(e))
            }
            changed()
          }}
        />
        <span className="ml-auto" />
        <Button variant="outline" size="sm" onClick={addStack}>
          <Icon name="layers" /> Stack
        </Button>
        <Button variant="outline" size="sm" onClick={addNote}>
          <Icon name="sticky-note" /> Note
        </Button>
        <Button variant="ghost" size="icon-sm" title="Zoom out" aria-label="Zoom out" onClick={() => zoomBy(1 / 1.2)}>
          <Icon name="minus" />
        </Button>
        <Button
          variant="ghost"
          size="sm"
          title="Back to 100%"
          onClick={() => {
            setZoom(1)
            setPan({ x: 40, y: 40 })
          }}
        >
          {Math.round(zoom * 100)}%
        </Button>
        <Button variant="ghost" size="icon-sm" title="Zoom in" aria-label="Zoom in" onClick={() => zoomBy(1.2)}>
          <Icon name="plus" />
        </Button>
      </Bar>
      <div className="flex min-h-0 flex-1">
        <div
          ref={plane}
          className="work-plane-host relative min-w-0 flex-1 overflow-hidden"
          data-panning={drag?.type === "pan"}
          onPointerDown={(e) => {
            if (e.button !== 0) return
            setDrag({ type: "pan", sx: e.clientX, sy: e.clientY, px: pan.x, py: pan.y })
          }}
          onWheel={(e) => {
            if (e.metaKey || e.ctrlKey) zoomBy(e.deltaY < 0 ? 1.1 : 1 / 1.1, e.clientX, e.clientY)
            else setPan((p) => ({ x: p.x - e.deltaX, y: p.y - e.deltaY }))
          }}
        >
          <div className="work-plane" style={{ transform: `translate(${pan.x}px, ${pan.y}px) scale(${zoom})` }}>
            {lay.notes.map((n) => {
              const at = placeOf("note", n.id, n)
              return (
                <div
                  key={n.id}
                  data-box
                  className="work-onplane flex w-48 flex-col rounded-md border bg-popover shadow-sm"
                  data-dragging={live("note", n.id) ? true : undefined}
                  style={{ left: at.x, top: at.y }}
                >
                  <div
                    className="flex h-5 cursor-move items-center border-b px-1"
                    onPointerDown={(e) => grab(e, { type: "note", id: n.id })}
                  >
                    <Icon name="sticky-note" size={11} className="text-muted-foreground" />
                    <Button
                      variant="ghost"
                      size="icon-xs"
                      className="ml-auto"
                      title="Take the note off"
                      aria-label="Take the note off"
                      onPointerDown={(e) => e.stopPropagation()}
                      onClick={() => {
                        setLay((l) => ({ ...l, notes: l.notes.filter((x) => x.id !== n.id) }))
                        void save({ notes: { [n.id]: null } })
                      }}
                    >
                      <Icon name="x" />
                    </Button>
                  </div>
                  {editNote === n.id ? (
                    <NoteText
                      text={n.text}
                      done={(text) => {
                        setEditNote(null)
                        if (text === n.text) return
                        const next = { ...n, text }
                        setLay((l) => ({ ...l, notes: l.notes.map((x) => (x.id === n.id ? next : x)) }))
                        void save({ notes: { [n.id]: next } })
                      }}
                    />
                  ) : (
                    <div
                      className={`min-h-12 p-2 text-sm wrap-anywhere ${n.text ? "" : "text-muted-foreground"}`}
                      onPointerDown={(e) => e.stopPropagation()}
                      onDoubleClick={() => setEditNote(n.id)}
                      title="Double-click to write"
                    >
                      {n.text ? <Markdown>{n.text}</Markdown> : "Double-click to write"}
                    </div>
                  )}
                </div>
              )
            })}
            {lay.stacks.map((st) => {
              const at = placeOf("stack", st.id, st)
              const cards = st.items.map((id) => byId.get(id)).filter(shown)
              return (
                <section
                  key={st.id}
                  data-box
                  data-stack={st.id}
                  className={`work-onplane flex w-60 flex-col gap-1.5 rounded-md border bg-muted p-1.5 shadow-sm ${cardDragging ? "ring-1 ring-ring" : ""}`}
                  data-dragging={live("stack", st.id) ? true : undefined}
                  style={{ left: at.x, top: at.y }}
                  onPointerDown={(e) => e.stopPropagation()}
                >
                  <header
                    className="flex cursor-move items-center gap-1.5 text-sm"
                    onPointerDown={(e) => grab(e, { type: "stack", id: st.id })}
                  >
                    {editStack === st.id ? (
                      <Input
                        autoFocus
                        className="h-7"
                        defaultValue={st.title}
                        onPointerDown={(e) => e.stopPropagation()}
                        onKeyDown={(e) => {
                          if (e.key === "Enter") e.currentTarget.blur()
                          if (e.key === "Escape") setEditStack(null)
                        }}
                        onBlur={(e) => {
                          setEditStack(null)
                          const title = e.target.value.trim() || st.title
                          if (title === st.title) return
                          const next = { ...st, title }
                          setLay((l) => ({ ...l, stacks: l.stacks.map((s) => (s.id === st.id ? next : s)) }))
                          void save({ stacks: { [st.id]: next } })
                        }}
                        aria-label="Stack title"
                      />
                    ) : (
                      <span
                        className="truncate font-medium"
                        onDoubleClick={() => setEditStack(st.id)}
                        title="Double-click to rename"
                      >
                        {st.title}
                      </span>
                    )}
                    <Badge variant="secondary">{cards.length}</Badge>
                    <Button
                      variant="ghost"
                      size="icon-xs"
                      className="ml-auto"
                      title="Take the stack off (its cards go back to the tray)"
                      aria-label="Take the stack off"
                      onPointerDown={(e) => e.stopPropagation()}
                      onClick={() => {
                        setLay((l) => ({ ...l, stacks: l.stacks.filter((s) => s.id !== st.id) }))
                        void save({ stacks: { [st.id]: null } })
                      }}
                    >
                      <Icon name="x" />
                    </Button>
                  </header>
                  <Pick
                    label="What dropping a card here sets on it"
                    className="w-full"
                    value={st.bind ? `${st.bind.key}:${st.bind.value}` : ""}
                    options={bindOptions}
                    onChange={(v) => {
                      const i = v.indexOf(":")
                      const bind = v ? { key: v.slice(0, i) as "stage" | "parent", value: v.slice(i + 1) } : undefined
                      const next = { ...st, bind }
                      setLay((l) => ({ ...l, stacks: l.stacks.map((s) => (s.id === st.id ? next : s)) }))
                      void save({ stacks: { [st.id]: next } })
                    }}
                  />
                  <div className="flex flex-col gap-1">
                    {cards.length === 0 && (
                      <p className="py-2 text-center text-muted-foreground text-xs">Drop cards here</p>
                    )}
                    {cards.map((c) => (
                      <div
                        key={c.id}
                        className={live("card", c.id) ? "opacity-50" : ""}
                        onPointerDown={(e) => grab(e, { type: "card", id: c.id, from: st.id })}
                      >
                        <ItemCard
                          card={c}
                          kind={kinds.get(c.kind)}
                          picked={picked === c.id}
                          pick={() => {}}
                          draggable={false}
                          compact
                        />
                      </div>
                    ))}
                  </div>
                </section>
              )
            })}
            {Object.entries(lay.cards).map(([id, pos]) => {
              const c = byId.get(id)
              if (!shown(c)) return null
              const at = placeOf("card", id, pos)
              return (
                <div
                  key={id}
                  className="work-onplane w-52"
                  data-dragging={live("card", id) ? true : undefined}
                  style={{ left: at.x, top: at.y }}
                  onPointerDown={(e) => grab(e, { type: "card", id, from: "canvas" })}
                >
                  <ItemCard
                    card={c}
                    kind={kinds.get(c.kind)}
                    picked={picked === id}
                    pick={() => {}}
                    draggable={false}
                    meta={`${kinds.get(c.kind)?.label ?? c.kind}${c.stage ? ` · ${label(c.stage)}` : ""}${c.words > 0 ? ` · ${c.words} words` : ""}`}
                  />
                </div>
              )
            })}
          </div>
        </div>
        <aside
          ref={tray}
          className={`flex w-56 shrink-0 flex-col gap-1 overflow-y-auto border-l p-2 ${cardDragging ? "bg-accent/40" : ""}`}
        >
          <h3 className="flex items-center gap-1.5 font-medium text-sm">
            Not on the canvas <Badge variant="secondary">{trayCards.length}</Badge>
          </h3>
          {trayCards.length === 0 && <p className="text-muted-foreground text-xs">Drop a card here to take it off.</p>}
          {trayCards.map((c) => (
            <Chip
              key={c.id}
              card={c}
              kind={kinds.get(c.kind)}
              picked={picked === c.id}
              onPointerDown={(e) => grab(e, { type: "card", id: c.id, from: "tray" })}
            />
          ))}
        </aside>
      </div>
      {dragged && draggedCard && (
        <div className="work-ghost w-52" style={{ left: dragged.x - dragged.dx, top: dragged.y - dragged.dy }}>
          <ItemCard
            card={draggedCard}
            kind={kinds.get(draggedCard.kind)}
            picked={false}
            pick={() => {}}
            draggable={false}
            compact
          />
        </div>
      )}
    </div>
  )
}

function NoteText({ text, done }: { text: string; done: (t: string) => void }) {
  const [t, setT] = useState(text)
  return (
    <Textarea
      autoFocus
      className="min-h-20"
      value={t}
      onChange={(e) => setT(e.target.value)}
      onPointerDown={(e) => e.stopPropagation()}
      onBlur={() => done(t)}
      onKeyDown={(e) => {
        if (e.key === "Escape") done(text)
        if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) e.currentTarget.blur()
      }}
      rows={4}
      aria-label="Note"
    />
  )
}
