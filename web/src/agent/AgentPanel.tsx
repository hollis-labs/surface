/**
 * The agent panel: the conversation with the embedded agent (ade-react's AgentPanel over an
 * ade-agent session on the agent-host/1 wire), with the context strip above it: the slot in
 * play, the tools top of mind there, and the <context> block the next message will carry.
 *
 * The context is the server's: it is assembled from the desk on every message, and pushed
 * here as an "agent.context" event whenever the window in front changes. The page sends no
 * context and no tools of its own.
 */
import { type AgentSession, createAgentSession } from "@hollis-labs/ade-agent"
import { AgentPanel as Chat } from "@hollis-labs/ade-react"
import "@hollis-labs/ade-react/styles.css"
import { useCallback, useEffect, useMemo, useState } from "react"
import { type Desk, type SurfaceEvent, useEvent } from "../sdk"
import { type Assembly, agentApi, type Setup, type SlotInfo } from "./api"
import "./agent.css"

let shared: AgentSession | null = null

/** The one session of the page: a run goes on while the panel is closed. */
export function agentSession(): AgentSession {
  if (!shared) {
    shared = createAgentSession({ base: "/api/agent", tools: () => [] })
    void shared.start()
  }
  return shared
}

export interface AgentPanelProps {
  /** The desk, for opening what the agent names. Optional. */
  desk?: Desk
  title?: string
}

export function AgentPanel({ title = "Agent" }: AgentPanelProps) {
  const session = useMemo(agentSession, [])
  const [asm, setAsm] = useState<Assembly | null>(null)
  const [setup, setSetup] = useState<Setup | null>(null)
  const [slots, setSlots] = useState<SlotInfo[]>([])
  const [open, setOpen] = useState(false)
  const [err, setErr] = useState("")

  const refresh = useCallback(() => {
    agentApi.context().then(setAsm, (e: Error) => setErr(e.message))
    agentApi.slots().then(setSlots, () => {})
  }, [])

  useEffect(() => {
    refresh()
    agentApi.setup().then(setSetup, () => {})
  }, [refresh])

  useEvent("agent.context", (ev: SurfaceEvent) => setAsm(ev.data as Assembly))
  useEvent("agent.note", refresh)
  useEvent("agent.turn", refresh)

  // The thread may have been answered elsewhere (a reflex, an outside agent): reload it when a run ends.
  useEffect(() => {
    session.onRunEnd(() => refresh())
    return () => session.onRunEnd(null)
  }, [session, refresh])

  const pin = (name: string) => agentApi.pin(name).then(setAsm, (e: Error) => setErr(e.message))
  const top = asm?.top_of_mind ?? []

  return (
    <div className="sf-agent">
      <section className="sf-agent-context" aria-label="What the agent has in mind">
        <div className="sf-agent-row">
          <label className="sf-agent-slot">
            <span className="sf-agent-label">Slot</span>
            <select
              value={asm?.slot ?? ""}
              onChange={(e) => void pin(e.target.value)}
              title={asm ? `${asm.slots[0]?.about ?? ""} (${asm.why})` : ""}
            >
              {slots.map((s) => (
                <option key={s.name} value={s.name}>
                  {s.name}
                </option>
              ))}
            </select>
          </label>
          <span className="sf-agent-why">
            {asm?.why === "pinned" ? "pinned" : asm?.focused ? `from ${asm.focused.title}` : ""}
          </span>
          {asm?.why === "pinned" && (
            <button type="button" className="sf-agent-link" onClick={() => void pin("")}>
              unpin
            </button>
          )}
          <button type="button" className="sf-agent-link" aria-expanded={open} onClick={() => setOpen(!open)}>
            {open ? "hide context" : "context"}
          </button>
        </div>
        {asm && asm.slots.length > 1 && (
          <div className="sf-agent-row sf-agent-also">
            also:{" "}
            {asm.slots
              .slice(1)
              .map((s) => s.name)
              .join(", ")}
          </div>
        )}
        {top.length > 0 && (
          <div className="sf-agent-tools" title="Top of mind">
            {top.map((t) => (
              <code key={t}>{t}</code>
            ))}
          </div>
        )}
        {open && asm && <pre className="sf-agent-block">{asm.block}</pre>}
        {setup && !setup.ready && <SetupCard path={setup.config_path} />}
        {err && <div className="sf-agent-off">{err}</div>}
      </section>
      <Chat session={session} title={title} placeholder="Ask, or /open, /layout, /run" />
    </div>
  )
}

/** The minimal agent.json, with the key file beside it. */
const example = (path: string) => `{
  "model": "your-model-name",
  "gateway_url": "https://your-gateway.example",
  "key_file": "${path.replace(/[^/]*$/, "") || "./"}agent-key"
}`

/** Shown until a model is configured: what to write where, ready to copy. */
function SetupCard({ path }: { path: string }) {
  const [copied, setCopied] = useState(false)
  const text = example(path)
  const copy = () => {
    void navigator.clipboard?.writeText(text).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    })
  }
  return (
    <div className="sf-agent-setup">
      <strong>The agent is not set up yet.</strong>
      <span>
        Everything else works by hand. To talk to the agent, write <code>{path}</code>:
      </span>
      <pre>{text}</pre>
      <span>
        The gateway is any Anthropic-compatible endpoint; the key file holds its key. Restart the app after writing it.{" "}
        <button type="button" className="sf-agent-link" onClick={copy}>
          {copied ? "copied" : "copy the example"}
        </button>
      </span>
    </div>
  )
}
