/**
 * Calendar: items placed by the view's date field (publish_on in Content, due in Planner), by
 * month or by week. Dragging an item to another day sets that date (one new revision, nothing
 * else changed); items with no date wait in the side list, and dropping one there takes the
 * date out. The kinds shown and the date field are kept in the view item; month or week is
 * this window's own.
 */
import { Icon } from "@hollis-labs/surface-page/sdk"
import { type DragEvent, useMemo, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import type { ViewArgs, WorkCard, WorkKind, WorkView } from "./api"
import { Bar, KindChips, KindIcon, MakeMenu, Pick } from "./parts"
import { DRAG_TYPE, label, message, ymd } from "./util"

const NONE = ""
const DAYS = ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"]

const addDays = (d: Date, n: number) => new Date(d.getFullYear(), d.getMonth(), d.getDate() + n)
/** The Monday on or before d. */
const monday = (d: Date) => addDays(d, -((d.getDay() + 6) % 7))
/** A field's value as a day, or "" (a time keeps its date part). */
const dayOf = (v: unknown) => (typeof v === "string" && /^\d{4}-\d{2}-\d{2}/.test(v) ? v.slice(0, 10) : NONE)

export function Calendar({ w, ws, space, view, picked, pick, changed, setError }: ViewArgs & { view: WorkView }) {
  const kinds = space.kinds
  const shownKinds = view.kinds.length ? view.kinds : kinds.map((k) => k.kind)
  const [mode, setMode] = useState<"month" | "week">("month")
  const [anchor, setAnchor] = useState(() => new Date())
  const [moved, setMoved] = useState<Record<string, string>>({})
  const [over, setOver] = useState<string | null>(null)

  const dateFields = useMemo(() => {
    const out = new Map<string, string>()
    for (const k of kinds)
      if (shownKinds.includes(k.kind))
        for (const f of k.fields) if (f.type === "date" && !out.has(f.key)) out.set(f.key, f.label || f.key)
    return [...out.entries()]
  }, [kinds, shownKinds])
  const field =
    (view.date_field && dateFields.some(([k]) => k === view.date_field) && view.date_field) ||
    dateFields[0]?.[0] ||
    "publish_on"
  const dated = kinds.filter((k) => k.fields.some((f) => f.key === field && f.type === "date"))
  const datedKinds = new Set(dated.map((k) => k.kind))

  const cards = space.items.filter((c) => !c.cut && shownKinds.includes(c.kind) && datedKinds.has(c.kind))
  const dayOfCard = (c: WorkCard) => moved[c.id] ?? dayOf(c.fields[field])
  const byDay = new Map<string, WorkCard[]>()
  for (const c of cards) {
    const d = dayOfCard(c)
    byDay.set(d, [...(byDay.get(d) ?? []), c])
  }

  const first = mode === "month" ? monday(new Date(anchor.getFullYear(), anchor.getMonth(), 1)) : monday(anchor)
  const weeks =
    mode === "month"
      ? Math.ceil(
          ((addDays(new Date(anchor.getFullYear(), anchor.getMonth() + 1, 1), -1).getTime() - first.getTime()) /
            86_400_000 +
            1) /
            7,
        )
      : 1
  const days = Array.from({ length: weeks * 7 }, (_, i) => addDays(first, i))
  const today = ymd(new Date())
  const heading =
    mode === "month"
      ? anchor.toLocaleDateString(undefined, { month: "long", year: "numeric" })
      : `${first.toLocaleDateString(undefined, { month: "short", day: "numeric" })} – ${addDays(
          first,
          6,
        ).toLocaleDateString(undefined, { month: "short", day: "numeric", year: "numeric" })}`
  const step = (n: number) =>
    setAnchor((a) => (mode === "month" ? new Date(a.getFullYear(), a.getMonth() + n, 1) : addDays(a, 7 * n)))

  const saveView = async (set: Record<string, unknown>) => {
    try {
      await w.saveView(ws, view.id, set)
    } catch (e) {
      setError(message(e))
    }
    changed()
  }

  const drop = async (e: DragEvent, day: string) => {
    e.preventDefault()
    setOver(null)
    const id = e.dataTransfer.getData(DRAG_TYPE)
    const c = cards.find((x) => x.id === id)
    if (!c || dayOfCard(c) === day) return
    setMoved((m) => ({ ...m, [id]: day }))
    try {
      await w.move(ws, id, field, day)
      setError("")
    } catch (err) {
      setError(message(err))
    }
    setMoved((m) => {
      const { [id]: _, ...rest } = m
      return rest
    })
    changed()
  }

  const make = async (kind: string, day: string) => {
    try {
      const r = await w.make(ws, { kind, fields: { [field]: day } })
      pick(r.id)
    } catch (e) {
      setError(message(e))
    }
    changed()
  }
  const makeable = dated.filter((k) => shownKinds.includes(k.kind))
  const target = (key: string, day: string) => ({
    "data-over": over === key,
    onDragOver: (e: DragEvent) => {
      e.preventDefault()
      if (over !== key) setOver(key)
    },
    onDragLeave: (e: DragEvent) => {
      if (!e.currentTarget.contains(e.relatedTarget as globalThis.Node)) setOver(null)
    },
    onDrop: (e: DragEvent) => void drop(e, day),
  })

  const chip = (c: WorkCard) => (
    <Chip key={c.id} card={c} kind={kinds.find((x) => x.kind === c.kind)} picked={picked === c.id} pick={pick} />
  )

  const undated = byDay.get(NONE) ?? []
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <Bar>
        <KindChips kinds={kinds} shown={shownKinds} set={(k) => void saveView({ kinds: k })} />
        <span className="ml-auto" />
        {dateFields.length > 1 && (
          <Pick
            label="Placed by"
            value={field}
            onChange={(v) => void saveView({ date_field: v })}
            options={dateFields.map(([key, name]) => ({ value: key, label: `By ${name.toLowerCase()}` }))}
          />
        )}
      </Bar>
      <Bar>
        <Button variant="ghost" size="icon-sm" onClick={() => step(-1)} title="Earlier" aria-label="Earlier">
          <Icon name="chevron-left" />
        </Button>
        <Button variant="ghost" size="icon-sm" onClick={() => step(1)} title="Later" aria-label="Later">
          <Icon name="chevron-right" />
        </Button>
        <Button variant="outline" size="sm" onClick={() => setAnchor(new Date())}>
          Today
        </Button>
        <span className="font-medium text-sm">{heading}</span>
        <span className="ml-auto" />
        <ToggleGroup
          size="sm"
          spacing={0}
          value={[mode]}
          aria-label="Month or week"
          onValueChange={(v: string[]) => v[0] && setMode(v[0] as "month" | "week")}
        >
          <ToggleGroupItem value="month">Month</ToggleGroupItem>
          <ToggleGroupItem value="week">Week</ToggleGroupItem>
        </ToggleGroup>
      </Bar>
      <div className="flex min-h-0 flex-1">
        <div className="grid min-h-0 flex-1 auto-rows-fr grid-cols-7 gap-px overflow-auto bg-border p-px">
          {DAYS.map((d) => (
            <div key={d} className="bg-background px-1.5 py-1 text-muted-foreground text-xs">
              {d}
            </div>
          ))}
          {days.map((d) => {
            const day = ymd(d)
            const list = byDay.get(day) ?? []
            const out = mode === "month" && d.getMonth() !== anchor.getMonth()
            return (
              <section
                key={day}
                data-day={day}
                className={`work-drop group flex min-h-24 min-w-0 flex-col gap-1 p-1 ${out ? "bg-muted/40" : "bg-background"}`}
                {...target(day, day)}
              >
                <header className="flex items-center gap-1 text-xs">
                  <span
                    className={
                      day === today
                        ? "rounded-sm bg-primary px-1 text-primary-foreground"
                        : out
                          ? "text-muted-foreground"
                          : ""
                    }
                  >
                    {d.getDate()}
                  </span>
                  <span className="ml-auto opacity-0 group-hover:opacity-100">
                    <MakeMenu kinds={makeable} make={(kind) => void make(kind, day)} title="New item on this day" />
                  </span>
                </header>
                {list.map(chip)}
              </section>
            )
          })}
        </div>
        <aside
          className="work-drop flex w-56 shrink-0 flex-col gap-1 overflow-y-auto border-l p-2"
          {...target("\u0000none", NONE)}
        >
          <h3 className="flex items-center gap-1.5 font-medium text-sm">
            No date <Badge variant="secondary">{undated.length}</Badge>
          </h3>
          {undated.length === 0 && (
            <p className="text-muted-foreground text-xs">Drop an item here to take its date out.</p>
          )}
          {undated.map(chip)}
        </aside>
      </div>
    </div>
  )
}

/** One item as a small draggable row: a calendar day's, or a side list's. */
export function Chip({
  card,
  kind,
  picked,
  pick,
  onPointerDown,
}: {
  card: WorkCard
  kind?: WorkKind
  picked: boolean
  pick?: (id: string, newWindow?: boolean) => void
  onPointerDown?: (e: React.PointerEvent) => void
}) {
  return (
    <div
      data-work-color={kind?.color}
      data-picked={picked}
      className="work-card flex min-w-0 cursor-pointer items-center gap-1 rounded-sm border bg-card px-1.5 py-0.5 text-xs hover:bg-accent"
      draggable={!onPointerDown}
      onDragStart={(e) => {
        e.dataTransfer.setData(DRAG_TYPE, card.id)
        e.dataTransfer.effectAllowed = "move"
      }}
      onPointerDown={onPointerDown}
      onClick={pick ? (e) => pick(card.id, e.metaKey || e.ctrlKey || e.shiftKey) : undefined}
      title={`${kind?.label ?? card.kind} · ${label(card.stage) || "no stage"} · ${card.title}`}
    >
      <KindIcon kind={kind} size={11} />
      <span className="truncate">{card.title || "Untitled"}</span>
    </div>
  )
}
