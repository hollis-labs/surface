import { useEffect, useMemo, useRef, useState } from "react"
import { Icon } from "../sdk"
import { showKeys } from "./keys"
import { app, type CommandDef, desk, type LayoutDef, listLayouts, loadLayout, runCommand } from "./store"

interface Row {
  id: string
  title: string
  section: string
  icon?: string
  hint?: string
  words: string
  run: () => unknown
}

/** How well a row fits what was typed: every word must appear; earlier and in the title is better. */
function score(row: Row, words: string[]): number {
  const title = row.title.toLowerCase()
  let total = 0
  for (const w of words) {
    const at = title.indexOf(w)
    if (at >= 0) total += at === 0 ? 100 : title[at - 1] === " " ? 60 : 30
    else if (row.words.includes(w)) total += 10
    else return -1
  }
  return total
}

/** The command palette: every command, every view and every saved layout, by what you type. */
export function Palette({ commands, onClose }: { commands: CommandDef[]; onClose: () => void }) {
  const [query, setQuery] = useState("")
  const [at, setAt] = useState(0)
  const [layouts, setLayouts] = useState<LayoutDef[]>([])
  const listRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    void listLayouts()
      .then(setLayouts)
      .catch(() => {})
  }, [])

  const rows = useMemo<Row[]>(() => {
    const out: Row[] = []
    for (const c of commands) {
      if (c.hidden) continue
      const viewId = c.id.startsWith("open.") ? c.id.slice(5) : ""
      out.push({
        id: c.id,
        title: c.title,
        section: c.section ?? "Commands",
        icon: viewId ? app().views.find((v) => v.id === viewId)?.icon : undefined,
        hint: showKeys(c.keys),
        words: `${c.title} ${c.keywords ?? ""} ${c.section ?? ""} ${c.id}`.toLowerCase(),
        run: () => runCommand(c.id),
      })
    }
    for (const l of layouts) {
      out.push({
        id: `layout:${l.name}`,
        title: `Layout: ${l.name}`,
        section: "Layouts",
        icon: "layout-dashboard",
        hint: l.about,
        words: `layout ${l.name} ${l.about ?? ""} arrange saved view`.toLowerCase(),
        run: () => loadLayout(l.name),
      })
    }
    return out
  }, [commands, layouts])

  const shown = useMemo(() => {
    const words = query.toLowerCase().split(/\s+/).filter(Boolean)
    if (!words.length) return rows
    return rows
      .map((r) => ({ r, s: score(r, words) }))
      .filter((x) => x.s >= 0)
      .sort((a, b) => b.s - a.s)
      .map((x) => x.r)
  }, [rows, query])

  // biome-ignore lint/correctness/useExhaustiveDependencies: a new query starts the cursor at the top
  useEffect(() => setAt(0), [query])
  // biome-ignore lint/correctness/useExhaustiveDependencies: scroll when the cursor moves
  useEffect(() => {
    listRef.current?.querySelector('[data-at="true"]')?.scrollIntoView({ block: "nearest" })
  }, [at])

  const pick = (row: Row | undefined) => {
    if (!row) return
    onClose()
    void Promise.resolve(row.run()).catch((err) => console.warn("palette: failed", row.id, err))
  }

  let lastSection = ""
  return (
    <div className="sf-overlay sf-overlay-top" onClick={onClose}>
      <div className="sf-palette" role="dialog" aria-label="Command palette" onClick={(e) => e.stopPropagation()}>
        <div className="sf-palette-input">
          <Icon name="search" size={15} />
          <input
            autoFocus
            value={query}
            placeholder="Run a command, open a window, load a layout…"
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Escape") onClose()
              else if (e.key === "ArrowDown") {
                e.preventDefault()
                setAt((i) => Math.min(i + 1, shown.length - 1))
              } else if (e.key === "ArrowUp") {
                e.preventDefault()
                setAt((i) => Math.max(i - 1, 0))
              } else if (e.key === "Enter") {
                e.preventDefault()
                pick(shown[at])
              }
            }}
          />
        </div>
        <div className="sf-palette-list" ref={listRef}>
          {shown.length === 0 && <div className="sf-palette-none">Nothing matches.</div>}
          {shown.map((r, i) => {
            const head = !query && r.section !== lastSection ? r.section : ""
            lastSection = r.section
            return (
              <div key={r.id}>
                {head && <div className="sf-palette-head">{head}</div>}
                <button
                  type="button"
                  className="sf-palette-row"
                  data-at={i === at}
                  onMouseMove={() => setAt(i)}
                  onClick={() => pick(r)}
                >
                  <Icon name={r.icon ?? "chevron-right"} size={14} />
                  <span className="sf-palette-title">{r.title}</span>
                  {query && <span className="sf-palette-sec">{r.section}</span>}
                  {r.hint && <span className="sf-palette-hint">{r.hint}</span>}
                </button>
              </div>
            )
          })}
        </div>
      </div>
    </div>
  )
}

// The palette opens views through the desk like everything else.
void desk
