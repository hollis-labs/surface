/**
 * The Interaction Builder: describe a way of working and the agent sets the app up for it
 * (windows, mode, instructions, reflexes), saved as a flow to come back to and refine.
 *
 * The view composes nothing and takes no description itself: the owner talks to the agent in
 * its panel, whose builder slot knows how, and this window lists the flows the agent saved.
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

function when(iso: string) {
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString(undefined, { dateStyle: "short", timeStyle: "short" })
}

function BuilderView({ win, desk, api }: ViewProps) {
  const [flows, setFlows] = useState<Flow[]>([])
  const [open, setOpen] = useState<string>(typeof win.state?.flow === "string" ? win.state.flow : "")
  const [detail, setDetail] = useState<Flow | null>(null)
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
        <h2>Flows</h2>
        <p className="sf-dim">
          Describe a way of working to the agent, in its panel: “set up a flow to triage my inbox”, “write an article”.
          It arranges the windows, sets the mode and its own instructions, adds the reflexes the flow needs, and saves
          it here. To change one, tell the agent what to change; each change is a new version.
        </p>
        <button type="button" className="sf-btn" onClick={() => void desk.run("agent.toggle", { open: true })}>
          <Icon name="bot" size={14} /> Open the agent
        </button>
        {err && <p className="sf-error">{err}</p>}
      </section>
      <section className="bl-flows">
        {flows.length === 0 && <p className="sf-dim bl-none">None yet.</p>}
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
