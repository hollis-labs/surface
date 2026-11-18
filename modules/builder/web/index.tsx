/**
 * The Interaction Builder: describe a way of working and the agent sets the app up for it
 * (windows, mode, instructions, reflexes), saved as a flow to come back to and refine.
 *
 * The view composes nothing itself: it hands the description to the agent (agent.ask), whose
 * builder slot knows how, and lists the flows the agent saved.
 */
import { Icon, type ModuleWeb, useEvent, type ViewProps } from "@surface/sdk"
import { useCallback, useEffect, useState } from "react"

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
    <div className="flex h-full min-h-0 flex-col text-sm">
      <section className="border-b border-black/10 p-3 dark:border-white/10">
        <h2 className="mb-1 font-semibold">Describe a way of working</h2>
        <p className="mb-2 text-xs opacity-70">
          The agent arranges the windows, sets the mode and its own instructions, adds the reflexes the flow needs, and
          saves it as a flow. Then refine it in plain words.
        </p>
        <textarea
          className="h-24 w-full resize-none rounded border border-black/15 bg-transparent p-2 dark:border-white/15"
          placeholder="Triage my inbox: mail on the left, a draft reply beside it, and remind me to file what I've answered."
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => {
            if ((e.metaKey || e.ctrlKey) && e.key === "Enter") build()
          }}
        />
        <div className="mt-2 flex flex-wrap items-center gap-1">
          {EXAMPLES.map((x) => (
            <button
              key={x}
              type="button"
              className="rounded-full border border-black/15 px-2 py-0.5 text-xs hover:bg-black/5 dark:border-white/15 dark:hover:bg-white/10"
              onClick={() => setText(x)}
            >
              {x}
            </button>
          ))}
          <button
            type="button"
            className="ml-auto inline-flex items-center gap-1 rounded bg-black px-3 py-1 text-xs text-white disabled:opacity-40 dark:bg-white dark:text-black"
            disabled={busy || !text.trim()}
            onClick={build}
          >
            <Icon name="wand-sparkles" size={14} /> {busy ? "Asking…" : "Build"}
          </button>
        </div>
        {err && <div className="mt-2 text-xs text-red-600">{err}</div>}
      </section>
      <section className="min-h-0 flex-1 overflow-auto">
        <h2 className="px-3 pb-1 pt-3 text-xs font-semibold uppercase tracking-wide opacity-60">Flows</h2>
        {flows.length === 0 && <p className="px-3 text-xs opacity-60">None yet. Describe one above.</p>}
        <ul>
          {flows.map((f) => (
            <li key={f.name} className="border-b border-black/5 dark:border-white/5">
              <div className="flex items-center gap-2 px-3 py-2">
                <button type="button" className="min-w-0 flex-1 text-left" onClick={() => select(f.name)}>
                  <div className="truncate font-medium">{f.name}</div>
                  <div className="truncate text-xs opacity-60">
                    {f.goal} · v{f.version} · {when(f.updated)}
                  </div>
                </button>
                <button
                  type="button"
                  className="inline-flex items-center gap-1 rounded px-2 py-1 text-xs hover:bg-black/5 dark:hover:bg-white/10"
                  title="Put this flow on the desk"
                  onClick={() => void run(`flows/${encodeURIComponent(f.name)}/open`)}
                >
                  <Icon name="play" size={14} /> Open
                </button>
                <button
                  type="button"
                  className="inline-flex items-center gap-1 rounded px-2 py-1 text-xs hover:bg-black/5 dark:hover:bg-white/10"
                  onClick={() => setText(`Refine the flow “${f.name}”: `)}
                >
                  <Icon name="pencil" size={14} /> Refine
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
    <div className="space-y-2 px-3 pb-3 text-xs">
      {flow.description && <p className="opacity-80">{flow.description}</p>}
      <div>
        <span className="opacity-60">Windows ({flow.layout?.mode ?? "tile"}): </span>
        {(flow.layout?.windows ?? []).map((w, i) => (
          // biome-ignore lint/suspicious/noArrayIndexKey: a layout's windows have no id; their order is their identity
          <span key={i} className="mr-1 inline-block rounded bg-black/5 px-1.5 py-0.5 dark:bg-white/10">
            {w.title || w.view}
            {w.focus ? " ●" : ""}
          </span>
        ))}
      </div>
      {typeof setup.mode === "string" && setup.mode && (
        <div>
          <span className="opacity-60">Mode: </span>
          {setup.mode}
        </div>
      )}
      {typeof setup.instructions === "string" && setup.instructions && (
        <div>
          <span className="opacity-60">Agent instructions: </span>
          {setup.instructions}
        </div>
      )}
      {reflexes.length > 0 && (
        <div>
          <span className="opacity-60">Reflexes: </span>
          {reflexes.map((r) => (
            <code key={r} className="mr-1 rounded bg-black/5 px-1 dark:bg-white/10">
              {r}
            </code>
          ))}
        </div>
      )}
      {flow.versions && flow.versions.length > 1 && (
        <details>
          <summary className="cursor-pointer opacity-70">Versions ({flow.versions.length})</summary>
          <ul className="mt-1 space-y-1">
            {flow.versions.map((v) => (
              <li key={v.version} className="flex items-center gap-2">
                <span className="w-8">v{v.version}</span>
                <span className="flex-1 truncate opacity-70">
                  {v.note || "—"} · {when(v.created)}
                </span>
                {v.version !== flow.version && (
                  <button type="button" className="underline" onClick={() => onRestore(v.version)}>
                    restore
                  </button>
                )}
              </li>
            ))}
          </ul>
        </details>
      )}
      <button type="button" className="text-red-600 underline" onClick={onDelete}>
        Delete flow
      </button>
    </div>
  )
}

const web: ModuleWeb = { views: { "builder/main": BuilderView } }
export default web
