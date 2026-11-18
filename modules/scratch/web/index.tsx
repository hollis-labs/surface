/** Scratch's window: a box to jot into and the list of what was jotted. */
import { useCallback, useEffect, useRef, useState } from "react"
import { Icon, type ModuleWeb, useEvent, useSetting, type ViewProps } from "@surface/sdk"
import "./scratch.css"

interface Item {
  id: string
  text: string
  kind: "note" | "todo" | "idea" | "followup"
  done: boolean
  created: string
}

const KIND_ICON: Record<Item["kind"], string> = {
  note: "sticky-note",
  todo: "square-check",
  idea: "lightbulb",
  followup: "corner-up-right",
}

function Scratch({ win, desk, api }: ViewProps) {
  const [items, setItems] = useState<Item[]>([])
  const [text, setText] = useState("")
  const [filter, setFilter] = useState("")
  const [error, setError] = useState("")
  const showDone = useSetting<boolean>("scratch.show_done")
  const box = useRef<HTMLTextAreaElement>(null)

  const load = useCallback(() => {
    api
      .get<Item[]>(`items${filter ? `?q=${encodeURIComponent(filter)}` : ""}`)
      .then(setItems)
      .catch((e: Error) => setError(e.message))
  }, [api, filter])
  useEffect(load, [load])
  // The agent, a command or the CLI may have changed the items: the module says so on the bus.
  useEvent("scratch.changed", load)

  useEffect(() => {
    if (win.focused) box.current?.focus()
  }, [win.focused, win.args.compose])

  // What this window is showing, for the agent and the context slots.
  useEffect(() => {
    desk.report(win.id, { kind: "data", ref: "scratch:items", filter: filter || undefined })
  }, [desk, win.id, filter])

  const add = () => {
    const t = text.trim()
    if (!t) return
    setError("")
    api
      .post("items", { text: t })
      .then(() => setText(""))
      .catch((e: Error) => setError(e.message))
  }

  return (
    <div className="scratch">
      <div className="scratch-compose">
        <textarea
          ref={box}
          className="sf-input"
          rows={2}
          value={text}
          placeholder="Jot something down. Start with “todo:”, “idea:” or “followup:” to file it."
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.shiftKey) {
              e.preventDefault()
              add()
            }
          }}
        />
        <div className="sf-row">
          <input className="sf-input sf-grow" placeholder="Filter…" value={filter} onChange={(e) => setFilter(e.target.value)} />
          <button type="button" className="sf-btn" data-kind="primary" disabled={!text.trim()} onClick={add}>
            Keep
          </button>
        </div>
        {error && <p className="sf-error">{error}</p>}
      </div>
      <ul className="scratch-list">
        {items.length === 0 && <li className="sf-dim scratch-none">Nothing here{filter ? " matches" : " yet"}.</li>}
        {items.map((it) => (
          <li key={it.id} className="scratch-item" data-done={it.done}>
            <button
              type="button"
              className="scratch-tick"
              title={it.done ? "Open again" : "Done"}
              onClick={() => void api.put(`items/${it.id}`, { done: !it.done })}
            >
              <Icon name={it.done ? "circle-check" : "circle"} size={16} />
            </button>
            <span className="scratch-kind" title={it.kind}>
              <Icon name={KIND_ICON[it.kind]} size={13} />
            </span>
            <span className="scratch-text">{it.text}</span>
            <button type="button" className="scratch-x" title="Put away" onClick={() => void api.del(`items/${it.id}`)}>
              <Icon name="x" size={13} />
            </button>
          </li>
        ))}
      </ul>
      <footer className="scratch-foot sf-dim">
        {items.filter((i) => !i.done).length} open{showDone ? ` · ${items.filter((i) => i.done).length} done` : " · finished items hidden"}
      </footer>
    </div>
  )
}

export default { views: { "scratch/main": Scratch } } satisfies ModuleWeb
