/** The core's own windows: Settings, Layouts and Marks. They are views like any module's. */
import { useCallback, useEffect, useState } from "react"
import { Icon, type Mark, type ModuleWeb, replyMark, resolveMark, useEvent, type ViewProps } from "../sdk"
import { allSettings, call, loadSettings, type SettingEntry, setSetting } from "../sdk/runtime"
import { app, deleteLayout, deskState, type LayoutDef, listLayouts, loadLayout, saveLayout } from "./store"

function Settings(_: ViewProps) {
  const [entries, setEntries] = useState<SettingEntry[]>(allSettings())
  const [error, setError] = useState("")
  const refresh = useCallback(() => void loadSettings().then(setEntries), [])
  useEffect(refresh, [refresh])
  // A change made anywhere (another window, the agent, the CLI) shows here at once.
  useEvent("config.changed", refresh)

  const change = (e: SettingEntry, value: unknown) => {
    setError("")
    setSetting(e.full_key, value).catch((err: Error) => setError(`${e.title}: ${err.message}`))
  }
  const reset = (e: SettingEntry) => void call("DELETE", `/api/settings/${encodeURIComponent(e.full_key)}`).then(refresh)

  const scopes = [...new Set(entries.map((e) => e.scope))]
  const title = (scope: string) =>
    scope === "surface" ? "Desk" : (app().modules.find((m) => m.id === scope)?.title ?? scope)
  return (
    <div className="sf-pane">
      <p className="sf-note">Settings are kept in the app's database and take effect as you change them. Nothing restarts.</p>
      {error && <p className="sf-error">{error}</p>}
      {scopes.map((scope) => (
        <section key={scope} className="sf-group">
          <h3>{title(scope)}</h3>
          {entries
            .filter((e) => e.scope === scope)
            .map((e) => (
              <label key={e.full_key} className="sf-setting">
                <span className="sf-setting-text">
                  <span className="sf-setting-title">{e.title}</span>
                  {e.about && <span className="sf-setting-about">{e.about}</span>}
                  <code className="sf-setting-key">{e.full_key}</code>
                </span>
                <span className="sf-setting-control">
                  {e.type === "bool" && (
                    <input type="checkbox" checked={!!e.value} onChange={(ev) => change(e, ev.target.checked)} />
                  )}
                  {e.type === "enum" && (
                    <select className="sf-input" value={String(e.value)} onChange={(ev) => change(e, ev.target.value)}>
                      {e.options?.map((o) => (
                        <option key={o} value={o}>
                          {o}
                        </option>
                      ))}
                    </select>
                  )}
                  {e.type === "int" && (
                    <input
                      className="sf-input sf-input-num"
                      type="number"
                      value={Number(e.value)}
                      onChange={(ev) => {
                        const n = Number.parseInt(ev.target.value, 10)
                        if (Number.isFinite(n)) change(e, n)
                      }}
                    />
                  )}
                  {e.type === "string" && (
                    <input
                      className="sf-input"
                      defaultValue={String(e.value ?? "")}
                      key={String(e.value)}
                      onBlur={(ev) => {
                        if (ev.target.value !== e.value) change(e, ev.target.value)
                      }}
                    />
                  )}
                  {e.set && (
                    <button type="button" className="sf-link" title="Back to the default" onClick={() => reset(e)}>
                      reset
                    </button>
                  )}
                </span>
              </label>
            ))}
        </section>
      ))}
    </div>
  )
}

function Layouts(_: ViewProps) {
  const [layouts, setLayouts] = useState<LayoutDef[]>([])
  const [name, setName] = useState("")
  const [error, setError] = useState("")
  const refresh = useCallback(() => void listLayouts().then(setLayouts), [])
  useEffect(refresh, [refresh])
  useEvent("desk.layouts", refresh)
  const active = deskState().active

  const save = (n: string) => {
    setError("")
    saveLayout(n)
      .then(() => setName(""))
      .catch((err: Error) => setError(err.message))
  }
  return (
    <div className="sf-pane">
      <p className="sf-note">
        A layout is an arrangement you can come back to: which windows are open, how they sit, and the setup that
        goes with them (your mode, the agent's standing instructions, the reflexes that are on).
      </p>
      <form
        className="sf-row"
        onSubmit={(e) => {
          e.preventDefault()
          if (name.trim()) save(name.trim())
        }}
      >
        <input className="sf-input sf-grow" placeholder="Save the desk as it is now, as…" value={name} onChange={(e) => setName(e.target.value)} />
        <button type="submit" className="sf-btn" data-kind="primary" disabled={!name.trim()}>
          Save
        </button>
      </form>
      {error && <p className="sf-error">{error}</p>}
      {layouts.length === 0 && <p className="sf-dim">No saved layouts yet.</p>}
      {layouts.map((l) => (
        <div key={l.name} className="sf-card" data-on={l.name === active}>
          <div className="sf-card-head">
            <Icon name="layout-dashboard" size={15} />
            <strong>{l.name}</strong>
            {l.name === active && <span className="sf-chip">in use</span>}
            <span className="sf-grow" />
            <button type="button" className="sf-btn" onClick={() => void loadLayout(l.name)}>
              Arrange
            </button>
            <button type="button" className="sf-btn" title="Replace it with the desk as it is now" onClick={() => save(l.name)}>
              Update
            </button>
            <button type="button" className="sf-btn" data-kind="danger" onClick={() => void deleteLayout(l.name).then(refresh)}>
              Delete
            </button>
          </div>
          {l.about && <p className="sf-card-about">{l.about}</p>}
          <p className="sf-dim">
            {l.mode} · {l.windows.map((w) => w.title || w.view).join(", ") || "no windows"}
          </p>
          {l.setup && Object.keys(l.setup).length > 0 && (
            <dl className="sf-setup">
              {Object.entries(l.setup).map(([k, v]) => (
                <div key={k}>
                  <dt>{k}</dt>
                  <dd>{typeof v === "string" ? v : JSON.stringify(v)}</dd>
                </div>
              ))}
            </dl>
          )}
        </div>
      ))}
    </div>
  )
}

function Marks({ desk }: ViewProps) {
  const [marks, setMarks] = useState<Mark[]>([])
  const refresh = useCallback(() => void call<Mark[]>("GET", "/api/marks").then(setMarks), [])
  useEffect(refresh, [refresh])
  useEvent("marks.changed", refresh)
  return (
    <div className="sf-pane">
      <p className="sf-note">Marks are notes pinned to a part of something, by you or the agent. These are the open ones.</p>
      {marks.length === 0 && <p className="sf-dim">No open marks.</p>}
      {marks.map((m) => (
        <MarkCard key={m.id} mark={m} onOpen={() => void desk.run("marks.open", { ref: m.ref }).catch(() => {})} />
      ))}
    </div>
  )
}

export function MarkCard({ mark, onOpen }: { mark: Mark; onOpen?: () => void }) {
  const [reply, setReply] = useState("")
  return (
    <div className="sf-card sf-mark" data-author={mark.author}>
      <div className="sf-card-head">
        <Icon name={mark.author === "agent" ? "bot" : "user"} size={14} />
        <code className="sf-mark-ref" title={mark.ref} onClick={onOpen}>
          {mark.ref}
        </code>
        <span className="sf-grow" />
        <button type="button" className="sf-link" onClick={() => void resolveMark(mark.id)}>
          resolve
        </button>
      </div>
      {mark.quote && <blockquote>{mark.quote}</blockquote>}
      <p>{mark.body}</p>
      {mark.replies?.map((r) => (
        <p key={r.created + r.body} className="sf-mark-reply">
          <Icon name={r.author === "agent" ? "bot" : "user"} size={12} /> {r.body}
        </p>
      ))}
      <form
        className="sf-row"
        onSubmit={(e) => {
          e.preventDefault()
          if (reply.trim()) void replyMark(mark.id, reply.trim()).then(() => setReply(""))
        }}
      >
        <input className="sf-input sf-grow" placeholder="Reply…" value={reply} onChange={(e) => setReply(e.target.value)} />
      </form>
    </div>
  )
}

export const coreWeb: ModuleWeb = {
  views: { "surface/settings": Settings, "surface/layouts": Layouts, "surface/marks": Marks },
}
