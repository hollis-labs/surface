/**
 * The Interaction Builder: describe a way of working and the agent sets the app up for it
 * (windows, mode, instructions, reflexes), saved as a flow to come back to and refine.
 *
 * The view composes nothing itself: it hands the description to the agent (agent.ask), whose
 * builder slot knows how, and lists the flows the agent saved.
 */
import { Icon, type ModuleWeb, useEvent, type ViewProps } from "@surface/sdk"
import { useCallback, useEffect, useState } from "react"
import "./builder.css"

interface LayoutWindow {
  view: string
  title?: string
  focus?: boolean
}
interface Flow {
  name: string
  goal: string
  description?: string
  version: number
  updated: string
  layout?: { mode: string; ratio?: number; windows: LayoutWindow[]; setup?: Record<string, unknown> }
  versions?: { version: number; note?: string; created: string }[]
}

const EXAMPLES = ["Triage my inbox", "Write an article", "Research a topic and keep notes", "Review a draft with me"]

function when(iso: string) {
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString(undefined, { dateStyle: "short", timeStyle: "short" })
}

function BuilderView({ win, desk, api }: ViewProps) {
  const [text, setText] = useState("")
  const [flows, setFlows] = useState<Flow[]>([])
  const [open, setOpen] = useState<string>(typeof win.state?.flow === "string" ? win.state.flow : "")
  const [detail, setDetail] = useState<Flow | null>(null)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState("")

  const load = useCallback(() => {
    api.get<Flow[]>("flows").then(setFlows, (e: Error) => setErr(e.message))
  }, [api])
  useEffect(load, [load])
  // flows: the detail is read again whenever the list is (a new version was saved).
  // biome-ignore lint/correctness/useExhaustiveDependencies: flows is a refresh signal
  useEffect(() => {
    if (!open) {
      setDetail(null)
      return
    }
    api.get<Flow>(`flows/${encodeURIComponent(open)}`).then(setDetail, () => setDetail(null))
  }, [open, api, flows])
  useEvent("builder.changed", load)
  useEvent("desk.layouts", load)

  const select = (name: string) => {
    const next = open === name ? "" : name
    setOpen(next)
    desk.report(win.id, next ? { flow: next } : {})
  }

  const ask = async (message: string) => {
    if (!message.trim()) return
    setBusy(true)
    setErr("")
    try {
      const sent = await desk.run<boolean>("agent.ask", { text: message })
      if (sent === false) setErr("The agent did not take the message (is it still answering the last one?).")
      else setText("")
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }
  const build = () => {
    const t = text.trim()
    if (!t) return
    const refining = flows.find((f) => t.toLowerCase().startsWith(`refine the flow “${f.name}”`))
    void ask(refining ? t : `Set up a flow for me: ${t}`)
  }

  const run = async (path: string) => {
    setErr("")
    try {
      await api.post(path)
      load()
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
    }
  }
  const remove = async (f: Flow) => {
    const ok = await desk.confirm({
      title: `Delete the flow “${f.name}”?`,
      message: "Its versions and layout go too. Its reflexes stay.",
      danger: true,
      confirmLabel: "Delete",
    })
    if (!ok) return
    await api.del(`flows/${encodeURIComponent(f.name)}`)
    setOpen("")
    load()
  }

  return (
    <div className="bl">
      <section className="bl-compose">
        <h2>Describe a way of working</h2>
        <p className="sf-dim">
          The agent arranges the windows, sets the mode and its own instructions, adds the reflexes the flow needs, and
          saves it as a flow. Then refine it in plain words.
        </p>
        <textarea
          aria-label="The flow"
          placeholder="Triage my inbox: mail on the left, a draft reply beside it, and remind me to file what I've answered."
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => {
            if ((e.metaKey || e.ctrlKey) && e.key === "Enter") build()
          }}
        />
        <div className="bl-actions">
          {EXAMPLES.map((x) => (
            <button key={x} type="button" className="bl-example" onClick={() => setText(x)}>
              {x}
            </button>
          ))}
          <button
            type="button"
            className="sf-btn bl-build"
            data-kind="primary"
            disabled={busy || !text.trim()}
            title="Build (⌘↩)"
            onClick={build}
          >
            <Icon name="wand-sparkles" size={14} /> {busy ? "Asking…" : "Build"}
          </button>
        </div>
        {err && <p className="sf-error">{err}</p>}
      </section>
      <section className="bl-flows">
        <h3 className="bl-head">Flows</h3>
        {flows.length === 0 && <p className="sf-dim bl-none">None yet. Describe one above.</p>}
        <ul className="bl-list">
          {flows.map((f) => (
            <li key={f.name} className="bl-flow" data-open={open === f.name}>
              <div className="bl-flow-row">
                <button type="button" className="bl-flow-name" onClick={() => select(f.name)}>
                  <strong>{f.name}</strong>
                  <span>
                    {f.goal} · v{f.version} · {when(f.updated)}
                  </span>
                </button>
                <button
                  type="button"
                  className="sf-btn"
                  title="Put this flow on the desk"
                  onClick={() => void run(`flows/${encodeURIComponent(f.name)}/open`)}
                >
                  <Icon name="play" size={14} />
                  <span className="bl-label">Open</span>
                </button>
                <button
                  type="button"
                  className="sf-btn"
                  title="Refine it in plain words"
                  onClick={() => setText(`Refine the flow “${f.name}”: `)}
                >
                  <Icon name="pencil" size={14} />
                  <span className="bl-label">Refine</span>
                </button>
              </div>
              {open === f.name && detail && (
                <FlowDetail
                  flow={detail}
                  onRestore={(v) => void run(`flows/${encodeURIComponent(f.name)}/versions/${v}/restore`)}
                  onDelete={() => void remove(f)}
                />
              )}
            </li>
          ))}
        </ul>
      </section>
    </div>
  )
}

function FlowDetail({
  flow,
  onRestore,
  onDelete,
}: {
  flow: Flow
  onRestore: (v: number) => void
  onDelete: () => void
}) {
  const setup = flow.layout?.setup ?? {}
  const reflexes = Array.isArray(setup.reflexes) ? (setup.reflexes as string[]) : []
  return (
    <div className="bl-detail">
      {flow.description && <p>{flow.description}</p>}
      <p>
        <span className="bl-key">Windows ({flow.layout?.mode ?? "tile"}): </span>
        <span className="bl-pills">
          {(flow.layout?.windows ?? []).map((w, i) => (
            // biome-ignore lint/suspicious/noArrayIndexKey: a layout's windows have no id; their order is their identity
            <span key={i} className="bl-pill">
              {w.title || w.view}
              {w.focus ? " ●" : ""}
            </span>
          ))}
        </span>
      </p>
      {typeof setup.mode === "string" && setup.mode && (
        <p>
          <span className="bl-key">Mode: </span>
          {setup.mode}
        </p>
      )}
      {typeof setup.instructions === "string" && setup.instructions && (
        <p>
          <span className="bl-key">Agent instructions: </span>
          {setup.instructions}
        </p>
      )}
      {reflexes.length > 0 && (
        <p>
          <span className="bl-key">Reflexes: </span>
          <span className="bl-pills">
            {reflexes.map((r) => (
              <span key={r} className="bl-pill">
                <code>{r}</code>
              </span>
            ))}
          </span>
        </p>
      )}
      {flow.versions && flow.versions.length > 1 && (
        <details className="bl-versions">
          <summary>Versions ({flow.versions.length})</summary>
          <ul>
            {flow.versions.map((v) => (
              <li key={v.version}>
                <span className="bl-v">v{v.version}</span>
                <span className="bl-v-note">
                  {v.note || "—"} · {when(v.created)}
                </span>
                {v.version !== flow.version && (
                  <button type="button" className="sf-link" onClick={() => onRestore(v.version)}>
                    restore
                  </button>
                )}
              </li>
            ))}
          </ul>
        </details>
      )}
      <button type="button" className="sf-link bl-delete" onClick={onDelete}>
        Delete flow
      </button>
    </div>
  )
}

const web: ModuleWeb = { views: { "builder/main": BuilderView } }
export default web
