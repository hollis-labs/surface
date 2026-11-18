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
import { Check, ChevronDown, ChevronUp, Copy } from "lucide-react"
import { useCallback, useEffect, useMemo, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
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

  const why = asm?.why === "pinned" ? "pinned" : asm?.focused ? `from ${asm.focused.title}` : ""
  return (
    <div className="agent-panel flex h-full min-h-0 flex-col bg-background text-foreground">
      <section
        className="flex flex-col gap-1.5 border-b border-border px-2.5 py-2 text-xs"
        aria-label="What the agent has in mind"
      >
        <div className="flex flex-wrap items-center gap-2">
          <Label htmlFor="agent-slot">Slot</Label>
          <Select
            value={asm?.slot ?? null}
            onValueChange={(v) => void pin(String(v ?? ""))}
            items={slots.map((s) => ({ value: s.name, label: s.name }))}
          >
            <SelectTrigger id="agent-slot" size="sm" title={asm ? `${asm.slots[0]?.about ?? ""} (${asm.why})` : ""}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {slots.map((s) => (
                <SelectItem key={s.name} value={s.name}>
                  {s.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <span className="min-w-0 flex-1 truncate text-muted-foreground">{why}</span>
          {asm?.why === "pinned" && (
            <Button variant="link" size="xs" onClick={() => void pin("")}>
              unpin
            </Button>
          )}
          <Button variant="ghost" size="xs" aria-expanded={open} onClick={() => setOpen(!open)}>
            {open ? <ChevronUp /> : <ChevronDown />}
            context
          </Button>
        </div>
        {asm && asm.slots.length > 1 && (
          <div className="text-muted-foreground">
            also:{" "}
            {asm.slots
              .slice(1)
              .map((s) => s.name)
              .join(", ")}
          </div>
        )}
        {top.length > 0 && (
          <div className="flex flex-wrap gap-1" title="Top of mind">
            {top.map((t) => (
              <Badge key={t} variant="secondary" className="font-mono">
                {t}
              </Badge>
            ))}
          </div>
        )}
        {open && asm && (
          <pre className="m-0 max-h-[40vh] overflow-auto rounded-md bg-muted p-2 font-mono break-words whitespace-pre-wrap">
            {asm.block}
          </pre>
        )}
        {setup && !setup.ready && <SetupCard path={setup.config_path} />}
        {err && <div className="text-destructive">{err}</div>}
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
    <Card size="sm">
      <CardHeader>
        <CardTitle>The agent is not set up yet</CardTitle>
        <CardDescription>
          Everything else works by hand. To talk to the agent, write <code className="break-all">{path}</code>:
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-2 text-muted-foreground">
        <pre className="m-0 overflow-x-auto rounded-md bg-muted p-2 font-mono text-xs text-foreground">{text}</pre>
        <p>
          The gateway is any Anthropic-compatible endpoint; the key file holds its key. Restart the app after writing
          it.
        </p>
      </CardContent>
      <CardFooter>
        <Button variant="outline" size="xs" onClick={copy}>
          {copied ? <Check /> : <Copy />}
          {copied ? "Copied" : "Copy the example"}
        </Button>
      </CardFooter>
    </Card>
  )
}
