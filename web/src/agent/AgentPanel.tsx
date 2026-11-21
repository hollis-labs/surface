/**
 * The agent panel: the context strip (the slot in play, the tools top of mind there, and the
 * <context> block the next message will carry) above the conversation with the embedded
 * agent.
 *
 * The conversation is @hollis-labs/surface-chat's store, drawn with the registry's chat components. Each
 * part subscribes to its own slice, so typing redraws only the composer and a streamed token
 * redraws only the message it belongs to. In development the list, each row and the composer
 * count their own commits in window.__chatRenders, which is how that is checked.
 *
 * The context is the server's: it is assembled from the desk on every message and pushed
 * here as an "agent.context" event whenever the window in front changes. The page sends no
 * context and no tools of its own.
 */

import { useChat, useChatBusy, useChatIds, useChatItem, useStickToBottom } from "@hollis-labs/surface-chat"
import { Bot, Check, ChevronDown, ChevronUp, Copy, Ellipsis } from "lucide-react"
import { type FormEvent, memo, useCallback, useEffect, useLayoutEffect, useMemo, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card"
import {
  Conversation,
  ConversationContent,
  ConversationEmptyState,
  ConversationScrollButton,
  ConversationViewport,
} from "@/components/ui/conversation"
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { Item, ItemContent, ItemDescription, ItemFooter, ItemTitle } from "@/components/ui/item"
import { Label } from "@/components/ui/label"
import { MessageNotice } from "@/components/ui/message"
import { PromptInput, PromptInputFooter, PromptInputSubmit, PromptInputTextarea } from "@/components/ui/prompt-input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { CardDock } from "../cards/views"
import { type Desk, type SurfaceEvent, useEvent } from "../sdk"
import { AgentBindings } from "./AgentBindings"
import { type Assembly, agentApi, type ProfileState, type RecoverAction, type Setup, type SlotInfo } from "./api"
import { interruptedActions, interruptedReason } from "./interrupted"
import { ItemView } from "./items"
import { type AgentSession, createAgentSession } from "./session"

type ChatStore = AgentSession

let shared: AgentSession | null = null

/** The one conversation of the page: a run goes on while the panel is closed. */
export function agentSession(): AgentSession {
  if (!shared) {
    shared = createAgentSession()
    void shared.start()
  }
  return shared
}

/**
 * Development only: count the commits in which this component itself rendered (not its
 * children), in window.__chatRenders. A layout effect with no dependencies runs once per
 * such commit; StrictMode adds one at mount.
 */
function useCommits(id: string) {
  useLayoutEffect(() => {
    // Optional chaining: outside Vite (another bundler, a test) import.meta.env may not exist.
    if (!import.meta.env?.DEV) return
    const w = window as unknown as { __chatRenders?: Record<string, number> }
    w.__chatRenders ??= {}
    w.__chatRenders[id] = (w.__chatRenders[id] ?? 0) + 1
  })
}

export interface AgentPanelProps {
  /** The desk, for opening what the agent names. Optional. */
  desk?: Desk
  title?: string
}

export function AgentPanel({ title = "Agent" }: AgentPanelProps) {
  const store = useMemo(agentSession, [])
  // A turn started anywhere else (an outside agent, another tab) shows here too.
  useEvent("agent.turn", (ev: SurfaceEvent) =>
    store.turnElsewhere(String((ev.data as { run_id?: string })?.run_id ?? "")),
  )
  return (
    <div className="flex h-full min-h-0 flex-col bg-background text-foreground">
      <ContextStrip store={store} />
      <ChatHeader store={store} title={title} />
      <Thread store={store} />
      <CardDock />
      <ApprovalBar store={store} />
      <InterruptedBar store={store} />
      <InputHolder store={store} />
      <Composer store={store} />
    </div>
  )
}

/** What the agent has in mind: the slot, the tools, the block; the setup card until a model is set. */
function ContextStrip({ store }: { store: ChatStore }) {
  const [asm, setAsm] = useState<Assembly | null>(null)
  const [setup, setSetup] = useState<Setup | null>(null)
  const [slots, setSlots] = useState<SlotInfo[]>([])
  const [profiles, setProfiles] = useState<ProfileState | null>(null)
  const [open, setOpen] = useState(false)
  const [err, setErr] = useState("")

  const refresh = useCallback(() => {
    agentApi.context().then(setAsm, (e: Error) => setErr(e.message))
    agentApi.slots().then(setSlots, () => {})
    agentApi.profiles().then(setProfiles, () => {})
  }, [])
  useEffect(() => {
    refresh()
    agentApi.setup().then(setSetup, () => {})
  }, [refresh])
  useEvent("agent.context", (ev: SurfaceEvent) => setAsm(ev.data as Assembly))
  useEvent("agent.note", refresh)
  useEvent("agent.turn", refresh)
  useEvent("agent.profile", refresh)
  useEvent("config.changed", refresh)
  // A run's tools may have changed what is in front (a window opened, a note kept).
  useEffect(() => store.onRunEnd(() => refresh()), [store, refresh])

  const pin = (name: string) => agentApi.pin(name).then(setAsm, (e: Error) => setErr(e.message))
  const pinProfile = (profile: string) =>
    agentApi.pinProfile(profile).then(setProfiles, (e: Error) => setErr(e.message))
  const top = asm?.top_of_mind ?? []
  const why = asm?.why === "pinned" ? "pinned" : asm?.focused ? `from ${asm.focused.title}` : ""
  return (
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
        {profiles && (profiles.profiles.length > 0 || (profiles.bindings?.length ?? 0) > 0) && (
          <ProfilePicker state={profiles} onPick={(p) => void pinProfile(p)} />
        )}
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
      {setup && !setup.ready && <SetupCard path={setup.config_path} reason={setup.reason} />}
      {err && <div className="text-destructive">{err}</div>}
    </section>
  )
}

/** The agent profile in play, and the owner's pin: a module's, the app's own, or automatic. */
function ProfilePicker({ state, onPick }: { state: ProfileState; onPick: (profile: string) => void }) {
  const value = state.pinned ?? "auto"
  const items = [
    { value: "auto", label: `auto (${state.active.id || "default"})` },
    { value: "default", label: "default" },
    ...state.profiles.map((p) => ({ value: p.id, label: `${p.id} — ${p.name}` })),
  ]
  const active = state.profiles.find((p) => p.id === state.active.id)
  return (
    <>
      <Label htmlFor="agent-profile">Profile</Label>
      <Select disabled={!state.on} value={value} onValueChange={(v) => onPick(String(v ?? "auto"))} items={items}>
        <SelectTrigger
          id="agent-profile"
          size="sm"
          title={
            active
              ? `${active.title}: ${active.description ?? ""} (${state.active.why})`
              : `the app's own (${state.active.why})`
          }
        >
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {items.map((i) => (
            <SelectItem key={i.value} value={i.value}>
              {i.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <Dialog>
        <DialogTrigger render={<Button variant="ghost" size="xs" />}>Choose agents</DialogTrigger>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Module agents</DialogTitle>
          </DialogHeader>
          <AgentBindings />
        </DialogContent>
      </Dialog>
    </>
  )
}

function ChatHeader({ store, title }: { store: ChatStore; title: string }) {
  const [canStop, setCanStop] = useState(false)
  const [stopping, setStopping] = useState(false)
  const [notice, setNotice] = useState("")
  useEffect(() => {
    agentApi.setup().then(
      (s) => setCanStop(s.stop_all === true),
      () => {},
    )
  }, [])
  const stopAll = async () => {
    setStopping(true)
    try {
      setNotice((await agentApi.stopAll()).message)
    } catch (e) {
      setNotice(e instanceof Error ? e.message : "Could not stop agent work.")
    } finally {
      setStopping(false)
    }
  }
  const busy = useChatBusy(store)
  const model = useChat(store, (s) => s.host?.model ?? "")
  const empty = useChat(store, (s) => s.ids.length === 0)
  return (
    <>
      <div className="flex items-center gap-2 border-b border-border px-2.5 py-1.5">
        <span className="text-sm font-semibold">{title}</span>
        <span className="min-w-0 flex-1 truncate text-xs text-muted-foreground">{busy ? "working…" : model}</span>
        {canStop && (
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button variant="ghost" size="xs" aria-label="Agent menu">
                  <Ellipsis />
                </Button>
              }
            />
            <DropdownMenuContent>
              <DropdownMenuItem disabled={stopping} onClick={() => void stopAll()}>
                Stop everything
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        )}
        <Button variant="ghost" size="xs" disabled={busy || empty} onClick={() => void store.newThread()}>
          New
        </Button>
      </div>
      {notice && <MessageNotice>{notice}</MessageNotice>}
    </>
  )
}

/** The conversation: redraws when an item is added or removed, never for a token or a keystroke. */
const Thread = memo(function Thread({ store }: { store: ChatStore }) {
  const ids = useChatIds(store)
  const off = useChat(store, (s) => s.loaded && s.host !== null && (!s.host.available || !s.host.ready))
  const error = useChat(store, (s) => s.error)
  const { viewportRef, contentRef, atBottom, scrollToBottom } = useStickToBottom()
  useCommits("thread")
  return (
    <Conversation>
      <ConversationViewport ref={viewportRef}>
        <ConversationContent ref={contentRef}>
          {off && <MessageNotice>No model is connected. The desk still works by hand.</MessageNotice>}
          {!off && ids.length === 0 && (
            <ConversationEmptyState
              icon={<Bot />}
              title="Ask the agent"
              description="It sees the window in front and can work the desk for you."
            />
          )}
          {ids.map((id) => (
            <Row key={id} store={store} id={id} />
          ))}
          {error && <MessageNotice tone="error">{error}</MessageNotice>}
        </ConversationContent>
      </ConversationViewport>
      {!atBottom && <ConversationScrollButton onClick={scrollToBottom} />}
    </Conversation>
  )
})

/** One item: redraws only when that item changes. */
const Row = memo(function Row({ store, id }: { store: ChatStore; id: string }) {
  const item = useChatItem(store, id)
  useCommits(`row:${id}`)
  return item ? <ItemView item={item} /> : null
})

/**
 * On the conversation wire one window types at a time. When another holds the input, or this
 * window only watches, say so and offer to take over. Nothing is shown while the input is
 * free or this window's.
 */
function InputHolder({ store }: { store: ChatStore }) {
  const holder = useChat(store, (s) => s.holder ?? null)
  // A takeover changes this window's role and attachment, then the holder: redraw on it.
  useChat(store, (s) => s.error)
  // The attachment is known once the conversation is read; loaded changes then.
  useChat(store, (s) => s.loaded)
  if (!store.durable()) return null
  const mine = store.attachment()
  const watching = store.role() === "viewer"
  const elsewhere = holder !== null && holder !== mine
  if (!watching && !elsewhere) return null
  return (
    <div role="status" className="border-t border-border p-2.5">
      <Item variant="muted" size="sm">
        <ItemContent className="min-w-0">
          <ItemTitle>{elsewhere ? "Another window has the input" : "This window is watching"}</ItemTitle>
          <ItemDescription className="text-xs">
            {watching
              ? "It shows the conversation. Take over to type here."
              : "What you send here is refused until you take over."}
          </ItemDescription>
        </ItemContent>
        <ItemFooter className="justify-start">
          <Button variant="outline" size="xs" onClick={() => void store.takeover()}>
            Take over
          </Button>
        </ItemFooter>
      </Item>
    </div>
  )
}

/** The agent asks before a tool that changes things. Cards replace this in the next step. */
function ApprovalBar({ store }: { store: ChatStore }) {
  const q = useChat(store, (s) => s.approval)
  if (!q) return null
  const input = q.input === undefined ? "" : JSON.stringify(q.input)
  return (
    <div role="alertdialog" aria-label="Permission" className="border-t border-border p-2.5">
      <Item variant="outline" size="sm">
        <ItemContent className="min-w-0">
          <ItemTitle>{q.description || `Allow ${q.tool}?`}</ItemTitle>
          {!q.description && input && <ItemDescription className="font-mono text-xs">{input}</ItemDescription>}
        </ItemContent>
        <ItemFooter className="flex-wrap justify-start">
          <Button size="xs" onClick={() => void store.answer("once")}>
            Allow
          </Button>
          {q.standing && (
            <>
              <Button variant="outline" size="xs" onClick={() => void store.answer("session")}>
                {store.durable() ? "This conversation" : "This session"}
              </Button>
              <Button variant="outline" size="xs" onClick={() => void store.answer("always")}>
                Always
              </Button>
            </>
          )}
          <Button variant="ghost" size="xs" onClick={() => void store.answer("deny")}>
            Don't
          </Button>
        </ItemFooter>
      </Item>
    </div>
  )
}

const RECOVER: Record<RecoverAction, { label: string; variant: "default" | "outline" | "ghost" }> = {
  resume: { label: "Resume", variant: "default" },
  discard: { label: "Discard the turn", variant: "outline" },
  reset: { label: "Reset conversation", variant: "outline" },
}

/** A turn the previous run of the app left unfinished, and what the owner can do with it. */
function InterruptedBar({ store }: { store: ChatStore }) {
  const offer = useChat(store, (s) => s.interrupted ?? null)
  const [pending, setPending] = useState<RecoverAction | null>(null)
  const [error, setError] = useState("")
  if (!offer) return null
  const blocked = offer.integrity === "blocked"
  const act = (action: RecoverAction) => {
    setPending(action)
    setError("")
    store
      .recover(action)
      .catch((err: unknown) => setError(err instanceof Error ? err.message : "That did not work."))
      .finally(() => setPending(null))
  }
  return (
    <div role="alertdialog" aria-label="Interrupted turn" className="border-t border-border p-2.5">
      <Item variant="outline" size="sm">
        <ItemContent className="min-w-0">
          <ItemTitle>{blocked ? "This conversation needs a reset" : "The agent was interrupted"}</ItemTitle>
          <ItemDescription className="text-xs">{interruptedReason(offer)}</ItemDescription>
          {error && <ItemDescription className="text-xs text-destructive">{error}</ItemDescription>}
        </ItemContent>
        <ItemFooter className="flex-wrap justify-start">
          {interruptedActions(offer).map((action) => (
            <Button
              key={action}
              size="xs"
              variant={RECOVER[action].variant}
              disabled={pending !== null}
              onClick={() => act(action)}
            >
              {RECOVER[action].label}
            </Button>
          ))}
        </ItemFooter>
      </Item>
    </div>
  )
}

/** Where a message is written. It owns the draft: typing redraws nothing else. */
const Composer = memo(function Composer({ store }: { store: ChatStore }) {
  const [draft, setDraft] = useState("")
  const busy = useChatBusy(store)
  useCommits("composer")
  const submit = (e: FormEvent) => {
    e.preventDefault()
    const text = draft.trim()
    if (!text || busy) return
    setDraft("")
    void store.send(text).then((ok) => {
      if (!ok) setDraft(text)
    })
  }
  return (
    <div className="border-t border-border p-2.5">
      <PromptInput onSubmit={submit}>
        <PromptInputTextarea
          value={draft}
          aria-label="Message"
          placeholder="Ask, or /open, /layout, /run"
          onChange={(e) => setDraft(e.target.value)}
        />
        <PromptInputFooter>
          <span className="text-xs text-muted-foreground">Enter sends, Shift+Enter breaks the line</span>
          <PromptInputSubmit
            status={busy ? "streaming" : "ready"}
            onStop={store.stop}
            disabled={!busy && !draft.trim()}
          />
        </PromptInputFooter>
      </PromptInput>
    </div>
  )
})

/** The minimal agent.json, with the key file beside it. */
const example = (path: string) => `{
  "model": "your-model-name",
  "gateway_url": "https://your-gateway.example",
  "key_file": "${path.replace(/[^/]*$/, "") || "./"}agent-key"
}`

/** Shown until a model is configured: what to write where, ready to copy. */
function SetupCard({ path, reason }: { path: string; reason?: string }) {
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
        {reason && <p className="text-sm text-destructive">{reason}</p>}
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
