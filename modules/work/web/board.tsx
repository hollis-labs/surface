/**
 * Board: cards in columns by stage, by kind or by an enum field; dragging a card to another
 * column sets that value (one new revision of the item, nothing else changed). Columns come
 * from the shown kinds' stages (or the field's options); a value no column names goes to
 * "Other", never lost. The kind filter and grouping are kept in the view item.
 */
import { type DragEvent, useMemo, useState } from "react"
import { Badge } from "@/components/ui/badge"
import type { ViewArgs, WorkCard, WorkView } from "./api"
import { Bar, ItemCard, KindChips, MakeMenu, Pick } from "./parts"
import { DRAG_TYPE, label, message } from "./util"

const OTHER = "\u0000other"

export function Board({ w, ws, space, view, picked, pick, changed, setError }: ViewArgs & { view: WorkView | null }) {
  const kinds = space.kinds
  const groupBy = view?.group_by || "stage"
  const shownKinds = view?.kinds.length ? view.kinds : kinds.map((k) => k.kind)
  // The board's own optimistic changes, until the next read-back shows them.
  const [moved, setMoved] = useState<Record<string, string>>({})
  const [over, setOver] = useState<string | null>(null)

  const byKind = groupBy === "kind"
  const groupOf = (c: WorkCard) =>
    moved[c.id] ?? (groupBy === "stage" ? c.stage : byKind ? c.kind : String(c.fields[groupBy] ?? ""))
  const enumFields = useMemo(() => {
    const out = new Map<string, { key: string; label: string; options: string[] }>()
    for (const k of kinds)
      for (const f of k.fields)
        if (f.type === "enum" && !out.has(f.key))
          out.set(f.key, { key: f.key, label: f.label || f.key, options: f.options ?? [] })
    return [...out.values()]
  }, [kinds])

  const columns = useMemo(() => {
    const cols: string[] = []
    if (byKind) cols.push(...shownKinds)
    else if (groupBy === "stage") {
      for (const k of kinds)
        if (shownKinds.includes(k.kind)) for (const s of k.stages) if (!cols.includes(s)) cols.push(s)
    } else cols.push(...(enumFields.find((f) => f.key === groupBy)?.options ?? []), "")
    return cols
  }, [kinds, shownKinds, groupBy, byKind, enumFields])

  const cards = space.items.filter((c) => !c.cut && shownKinds.includes(c.kind))
  const byCol = new Map<string, WorkCard[]>()
  for (const c of cards) {
    const v = groupOf(c)
    const col = columns.includes(v) ? v : OTHER
    byCol.set(col, [...(byCol.get(col) ?? []), c])
  }
  const shownCols = [...columns, ...(byCol.has(OTHER) ? [OTHER] : [])]

  const saveView = async (set: Record<string, unknown>) => {
    try {
      if (view) await w.saveView(ws, view.id, set)
      else await w.newView(ws, { view: "board", title: "Board", ...set })
    } catch (e) {
      setError(message(e))
    }
    changed()
  }

  const drop = async (e: DragEvent, col: string) => {
    e.preventDefault()
    setOver(null)
    const id = e.dataTransfer.getData(DRAG_TYPE)
    const c = cards.find((x) => x.id === id)
    if (!c || col === OTHER || byKind || groupOf(c) === col) return
    const k = kinds.find((x) => x.kind === c.kind)
    if (groupBy === "stage" && k && !k.stages.includes(col)) {
      setError(`A ${k.label.toLowerCase()} has no stage "${col}".`)
      return
    }
    setMoved((m) => ({ ...m, [id]: col }))
    try {
      await w.move(ws, id, groupBy, col)
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

  const make = async (kind: string, value: string) => {
    try {
      const k = kinds.find((x) => x.kind === kind)
      const stage = groupBy === "stage" && k?.stages.includes(value) ? value : undefined
      const fields = groupBy !== "stage" && !byKind && value ? { [groupBy]: value } : undefined
      const r = await w.make(ws, { kind, stage, fields })
      pick(r.id)
    } catch (e) {
      setError(message(e))
    }
    changed()
  }

  const makeable = kinds.filter((k) => shownKinds.includes(k.kind))
  const colTitle = (col: string) =>
    col === OTHER
      ? "Other"
      : col === ""
        ? "None"
        : byKind
          ? (kinds.find((k) => k.kind === col)?.plural ?? label(col))
          : label(col)

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <Bar>
        <KindChips kinds={kinds} shown={shownKinds} set={(k) => void saveView({ kinds: k })} />
        <span className="ml-auto" />
        <Pick
          label="Columns by"
          value={groupBy}
          onChange={(v) => void saveView({ group_by: v })}
          options={[
            { value: "stage", label: "By stage" },
            { value: "kind", label: "By kind" },
            ...enumFields.map((f) => ({ value: f.key, label: `By ${f.label.toLowerCase()}` })),
          ]}
        />
      </Bar>
      <div className="flex min-h-0 flex-1 gap-2 overflow-x-auto p-2">
        {shownCols.map((col) => {
          const list = byCol.get(col) ?? []
          return (
            <section
              key={col}
              data-over={over === col}
              className="work-drop flex w-64 shrink-0 flex-col gap-1.5 rounded-md bg-muted/40 p-1.5"
              onDragOver={(e) => {
                e.preventDefault()
                if (over !== col) setOver(col)
              }}
              onDragLeave={(e) => {
                if (!e.currentTarget.contains(e.relatedTarget as Node)) setOver(null)
              }}
              onDrop={(e) => void drop(e, col)}
            >
              <h3 className="flex items-center gap-1.5 px-1 font-medium text-sm">
                {colTitle(col)}
                <Badge variant="secondary">{list.length}</Badge>
                <span className="ml-auto" />
                {col !== OTHER && (
                  <MakeMenu
                    kinds={byKind ? makeable.filter((k) => k.kind === col) : makeable}
                    make={(kind) => void make(kind, col)}
                    title={`New item in ${colTitle(col)}`}
                  />
                )}
              </h3>
              <div className="flex min-h-0 flex-1 flex-col gap-1.5 overflow-y-auto">
                {list.map((c) => (
                  <ItemCard
                    key={c.id}
                    card={c}
                    kind={kinds.find((x) => x.kind === c.kind)}
                    picked={picked === c.id}
                    pick={pick}
                  />
                ))}
              </div>
            </section>
          )
        })}
      </div>
    </div>
  )
}
