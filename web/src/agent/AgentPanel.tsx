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
import { Bot, Check, Copy, Ellipsis } from "lucide-react"
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
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { Item, ItemContent, ItemDescription, ItemFooter, ItemTitle } from "@/components/ui/item"
import { Label } from "@/components/ui/label"
import { MessageNotice } from "@/components/ui/message"
import { PromptInput, PromptInputSubmit, PromptInputTextarea } from "@/components/ui/prompt-input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { isLive, showCard, useCards } from "../cards/store"
import { CardDock } from "../cards/views"
import { desk as globalDesk } from "../desk/store"
import { type Desk, Icon, type SurfaceEvent, useEvent, useSetting } from "../sdk"
import { describeToolCall, inferTopicFromText } from "./ActivityTimeline"
import { AgentBindings } from "./AgentBindings"
import { AgentInspectorModal } from "./AgentInspectorModal"
import { type Assembly, agentApi, type ProfileState, type RecoverAction, type Setup, type SlotInfo } from "./api"
import { interruptedActions, interruptedReason } from "./interrupted"
import { ItemView, type ToolItem, toolCards } from "./items"
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

export function AgentPanel({ desk = globalDesk, title = "Agent" }: AgentPanelProps) {
  const store = useMemo(agentSession, [])
  // A turn started anywhere else (an outside agent, another tab) shows here too.
  useEvent("agent.turn", (ev: SurfaceEvent) =>
    store.turnElsewhere(String((ev.data as { run_id?: string })?.run_id ?? "")),
  )
  return (
    <div className="flex h-full min-h-0 flex-col bg-background text-foreground">
      <ChatHeader store={store} desk={desk} title={title} />
      <Thread store={store} desk={desk} />
      <CardDock />
      <ApprovalBar store={store} />
      <InterruptedBar store={store} />
      <InputHolder store={store} />
      <Composer store={store} />
    </div>
  )
}

function readLocalFlag(key: string): boolean {
  try {
    return localStorage.getItem(key) === "1"
  } catch {
    return false
  }
}

function writeLocalFlag(key: string, open: boolean) {
  try {
    localStorage.setItem(key, open ? "1" : "0")
  } catch {
    /* ignore */
  }
}

function ChatHeader({ store, desk, title }: { store: ChatStore; desk: Desk; title: string }) {
  const busy = useChatBusy(store)
  const model = useChat(store, (s) => s.host?.model ?? "")
  const empty = useChat(store, (s) => s.ids.length === 0)
  const devMode = (useSetting<boolean>("surface.developer_mode") ?? false) as boolean

  const [asm, setAsm] = useState<Assembly | null>(null)
  const [setup, setSetup] = useState<Setup | null>(null)
  const [slots, setSlots] = useState<SlotInfo[]>([])
  const [profiles, setProfiles] = useState<ProfileState | null>(null)
  const [err, setErr] = useState("")

  const [contextOpen, setContextOpenState] = useState(() => readLocalFlag("surface.agent.context.open"))
  const [inspectorOpen, setInspectorOpenState] = useState(() => readLocalFlag("surface.agent.inspector.open"))

  const setContextOpen = useCallback((next: boolean) => {
    setContextOpenState(next)
    writeLocalFlag("surface.agent.context.open", next)
  }, [])

  const setInspectorOpen = useCallback((next: boolean) => {
    setInspectorOpenState(next)
    writeLocalFlag("surface.agent.inspector.open", next)
  }, [])

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

  const [stopping, setStopping] = useState(false)
  const [notice, setNotice] = useState("")
  const canStop = setup?.stop_all === true
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

  const inlineCards = useCards("inline").filter((s) => isLive(s.card))
  const trayCards = useCards("tray").filter((s) => isLive(s.card))
  const openCardsCount = new Set([...inlineCards.map((s) => s.card.id), ...trayCards.map((s) => s.card.id)]).size

  const pin = (name: string) => agentApi.pin(name).then(setAsm, (e: Error) => setErr(e.message))
  const pinProfile = (profile: string) =>
    agentApi.pinProfile(profile).then(setProfiles, (e: Error) => setErr(e.message))

  const slotLabel = asm?.slot || "general"
  const pinned = asm?.why === "pinned"

  return (
    <>
      <div className="flex items-center gap-1.5 border-b border-border px-2.5 py-1.5">
        <span className="text-sm font-semibold">{title}</span>
        <span className="min-w-0 flex-1 truncate text-xs text-muted-foreground">{busy ? "working…" : model}</span>

        <Button
          variant={contextOpen || pinned ? "secondary" : "ghost"}
          size="xs"
          title={`Context: ${slotLabel} (${asm?.why ?? "auto"})`}
          aria-label="Context"
          onClick={() => setContextOpen(!contextOpen)}
        >
          <Icon name={pinned ? "pin" : "layers"} size={13} />
          <span className="max-w-20 truncate">{slotLabel}</span>
        </Button>

        <Button
          variant={openCardsCount > 0 ? "secondary" : "ghost"}
          size="xs"
          title="Open Cards window"
          aria-label="Open Cards window"
          onClick={() => void desk.open("surface.cards")}
        >
          <Icon name="gallery-vertical-end" size={13} className={openCardsCount > 0 ? "text-primary" : ""} />
          {openCardsCount > 0 && <span>{openCardsCount}</span>}
        </Button>

        <Button
          variant="ghost"
          size="icon-xs"
          title="Open Activity Timeline window"
          aria-label="Activity Timeline"
          onClick={() => void desk.open("surface.activity")}
        >
          <Icon name="activity" size={13} className="text-chart-2" />
        </Button>

        {devMode && (
          <Button
            variant={inspectorOpen ? "secondary" : "ghost"}
            size="icon-xs"
            title="Agent Inspector (Developer Mode)"
            aria-label="Agent Inspector"
            onClick={() => setInspectorOpen(!inspectorOpen)}
          >
            <Icon name="info" size={13} />
          </Button>
        )}

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

      {setup && !setup.ready && (
        <div className="border-b border-border p-2.5 text-xs">
          <SetupCard path={setup.config_path} reason={setup.reason} />
        </div>
      )}

      <ContextModal
        open={contextOpen}
        onOpenChange={setContextOpen}
        asm={asm}
        slots={slots}
        profiles={profiles}
        err={err}
        onPin={pin}
        onPinProfile={pinProfile}
      />

      {devMode && (
        <AgentInspectorModal
          open={inspectorOpen}
          onOpenChange={setInspectorOpen}
          asm={asm}
          setup={setup}
          slots={slots}
          profiles={profiles}
        />
      )}
    </>
  )
}

/** The Context modal: slot picker & pin, profile picker, secondary slots, top-of-mind tools, and the <context> block. */
function ContextModal({
  open,
  onOpenChange,
  asm,
  slots,
  profiles,
  err,
  onPin,
  onPinProfile,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  asm: Assembly | null
  slots: SlotInfo[]
  profiles: ProfileState | null
  err: string
  onPin: (slot: string) => void
  onPinProfile: (profile: string) => void
}) {
  const top = asm?.top_of_mind ?? []
  const why = asm?.why === "pinned" ? "pinned" : asm?.focused ? `from ${asm.focused.title}` : "automatic"
  const askFeedbackCard = () => {
    void showCard({
      kind: "choice",
      title: "Context feedback: which slot fits what you are doing?",
      subject: "classify:slot",
      source: "agent",
      payload: {
        question: `Current slot is “${asm?.slot || "general"}” (${why}). Pick the slot that should be active, or use Other to explain:`,
        text: asm?.focused?.title ?? "",
        options: slots.map((s) => ({ id: s.name, label: s.name, description: s.about })),
        allow_other: true,
      },
    })
    onOpenChange(false)
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[80vh] flex-col gap-3 sm:max-w-xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Icon name="layers" size={16} />
            What the agent has in mind
          </DialogTitle>
          <DialogDescription>
            The active context slot, agent profile, top-of-mind tools, and the exact block sent with your next message.
          </DialogDescription>
        </DialogHeader>

        <div className="flex flex-col gap-3 overflow-y-auto text-xs">
          <div className="flex flex-wrap items-center gap-2">
            <Label htmlFor="agent-slot">Slot</Label>
            <Select
              value={asm?.slot ?? null}
              onValueChange={(v) => onPin(String(v ?? ""))}
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
              <ProfilePicker state={profiles} onPick={(p) => onPinProfile(p)} />
            )}
            <span className="min-w-0 flex-1 truncate text-muted-foreground">{why}</span>
            {asm?.why === "pinned" && (
              <Button variant="link" size="xs" onClick={() => onPin("")}>
                unpin
              </Button>
            )}
            <Button
              variant="outline"
              size="xs"
              title="Open a feedback card to train or correct the context slot"
              onClick={askFeedbackCard}
            >
              Suggest / correct via card
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
            <div className="flex flex-col gap-1">
              <span className="text-muted-foreground">Top-of-mind tools</span>
              <div className="flex flex-wrap gap-1" title="Top of mind">
                {top.map((t) => (
                  <Badge key={t} variant="secondary" className="font-mono">
                    {t}
                  </Badge>
                ))}
              </div>
            </div>
          )}

          {asm && (
            <div className="flex flex-col gap-1">
              <span className="text-muted-foreground">Context block ({asm.block.length.toLocaleString()} chars)</span>
              <pre className="m-0 max-h-[40vh] overflow-auto rounded-md bg-muted p-2.5 font-mono break-words whitespace-pre-wrap">
                {asm.block}
              </pre>
            </div>
          )}

          {err && <div className="text-destructive">{err}</div>}
        </div>
      </DialogContent>
    </Dialog>
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

type ThreadEntry =
  | { kind: "item"; id: string; topic: string }
  | { kind: "activity_fold"; id: string; toolIds: string[]; topic: string }

/**
 * Consecutive non-card tool calls are collapsed into a single compact Activity Line.
 * Clicking the line opens the dedicated Activity Timeline window (`surface.activity`).
 */
function ActivityFoldRow({
  store,
  toolIds,
  topic,
  desk,
}: {
  store: ChatStore
  toolIds: string[]
  topic: string
  desk: Desk
}) {
  const items = useChat(store, (s) => s.items)
  const tools = useMemo(
    () => toolIds.map((id) => items[id]).filter((it): it is ToolItem => it?.kind === "tool"),
    [toolIds, items],
  )
  if (tools.length === 0) return null
  const running = tools.some((t) => t.status === "running")
  const failed = tools.some((t) => t.status === "error")
  const last = tools[tools.length - 1]
  const lastDesc = describeToolCall(last.name, last.input)
  const uniqueNames = [...new Set(tools.map((t) => t.name))]
  const chain = uniqueNames.slice(0, 3).join(" → ") + (uniqueNames.length > 3 ? ` +${uniqueNames.length - 3}` : "")

  return (
    <Button
      variant="outline"
      size="xs"
      onClick={() => void desk.open("surface.activity", { args: { group_id: last.id, topic } })}
      title="Click to inspect full tool activity timeline"
      className="group/fold flex h-auto w-full min-w-0 max-w-full shrink items-center justify-start gap-2 overflow-hidden rounded-sm border-border/70 bg-card/50 px-2.5 py-1 text-left font-mono text-[11px] font-normal text-muted-foreground transition-colors hover:border-primary/40 hover:bg-card hover:text-foreground"
    >
      <span
        className={`size-2 shrink-0 rounded-full ${
          running ? "animate-ping bg-primary" : failed ? "bg-destructive" : "bg-success"
        }`}
      />
      <Icon name="activity" size={12} className={running ? "text-primary" : "text-chart-2"} />
      <span className="shrink-0 font-semibold text-foreground">
        {tools.length} {tools.length === 1 ? "step" : "steps"}
      </span>
      <span className="text-border">·</span>
      <span className="truncate text-chart-2">{chain}</span>
      {lastDesc.detail && <span className="hidden truncate text-muted-foreground @sm:inline">({lastDesc.detail})</span>}
      <span className="ml-auto flex shrink-0 items-center gap-1 text-[10px] text-muted-foreground group-hover/fold:text-primary">
        Timeline
        <Icon name="arrow-up-right" size={11} />
      </span>
    </Button>
  )
}

/** The conversation: redraws when an item is added or removed, never for a token or a keystroke. */
const Thread = memo(function Thread({ store, desk }: { store: ChatStore; desk: Desk }) {
  const ids = useChatIds(store)
  const items = useChat(store, (s) => s.items)
  const off = useChat(store, (s) => s.loaded && s.host !== null && (!s.host.available || !s.host.ready))
  const error = useChat(store, (s) => s.error)
  const [activeSlot, setActiveSlot] = useState("general")
  useEvent("agent.context", (ev: SurfaceEvent) => {
    const slot = (ev.data as Assembly | undefined)?.slot
    if (slot) setActiveSlot(slot)
  })
  useEffect(() => {
    agentApi
      .context()
      .then((a) => a?.slot && setActiveSlot(a.slot))
      .catch(() => {})
  }, [])

  const entries = useMemo<ThreadEntry[]>(() => {
    const out: ThreadEntry[] = []
    let currentTopic = activeSlot || "general"
    let pendingTools: string[] = []

    const flushTools = () => {
      if (pendingTools.length > 0) {
        out.push({
          kind: "activity_fold",
          id: `fold:${pendingTools[0]}`,
          toolIds: pendingTools,
          topic: currentTopic,
        })
        pendingTools = []
      }
    }

    for (const id of ids) {
      const it = items[id]
      if (!it) continue
      if (it.kind === "user") {
        flushTools()
        currentTopic = inferTopicFromText(it.text || it.auto || "", activeSlot || "general")
        out.push({ kind: "item", id, topic: currentTopic })
      } else if (it.kind === "tool" && !(it.name in toolCards)) {
        pendingTools.push(id)
      } else {
        flushTools()
        out.push({ kind: "item", id, topic: currentTopic })
      }
    }
    flushTools()
    return out
  }, [ids, items, activeSlot])

  const { viewportRef, contentRef, atBottom, scrollToBottom } = useStickToBottom()
  useCommits("thread")
  return (
    <Conversation>
      <ConversationViewport ref={viewportRef}>
        <ConversationContent ref={contentRef} className="gap-2.5 p-3">
          {off && <MessageNotice>No model is connected. The desk still works by hand.</MessageNotice>}
          {!off && entries.length === 0 && (
            <ConversationEmptyState
              icon={<Bot />}
              title="Ask the agent"
              description="It sees the window in front and can work the desk for you."
            />
          )}
          {entries.map((entry) =>
            entry.kind === "activity_fold" ? (
              <ActivityFoldRow key={entry.id} store={store} toolIds={entry.toolIds} topic={entry.topic} desk={desk} />
            ) : (
              <Row key={entry.id} store={store} id={entry.id} topic={entry.topic} slot={activeSlot} />
            ),
          )}
          {error && <MessageNotice tone="error">{error}</MessageNotice>}
        </ConversationContent>
      </ConversationViewport>
      {!atBottom && <ConversationScrollButton onClick={scrollToBottom} />}
    </Conversation>
  )
})

/** One item: redraws only when that item changes. */
const Row = memo(function Row({
  store,
  id,
  topic,
  slot,
}: {
  store: ChatStore
  id: string
  topic: string
  slot: string
}) {
  const item = useChatItem(store, id)
  useCommits(`row:${id}`)
  return item ? <ItemView item={item} topic={topic} slot={slot} /> : null
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
  const [attached, setAttached] = useState<{ ref: string; title: string }[]>([])
  const busy = useChatBusy(store)
  useCommits("composer")

  useEffect(() => {
    const onAdd = (e: Event) => {
      const detail = (e as CustomEvent<{ ref: string; title?: string }>).detail
      if (!detail?.ref) return
      setAttached((prev) =>
        prev.some((x) => x.ref === detail.ref)
          ? prev
          : [...prev, { ref: detail.ref, title: detail.title || detail.ref }],
      )
    }
    window.addEventListener("surface:add-to-context", onAdd)
    return () => window.removeEventListener("surface:add-to-context", onAdd)
  }, [])

  const submit = (e: FormEvent) => {
    e.preventDefault()
    const raw = draft.trim()
    if ((!raw && attached.length === 0) || busy) return
    const prefix = attached.length > 0 ? `Context: ${attached.map((a) => a.ref).join(", ")}\n\n` : ""
    const text = `${prefix}${raw || "Analyze this context."}`.trim()
    setDraft("")
    setAttached([])
    void store.send(text).then((ok) => {
      if (!ok) setDraft(raw)
    })
  }
  return (
    <div className="border-t border-border bg-card/40 px-2.5 py-2">
      {attached.length > 0 && (
        <div className="mb-1.5 flex flex-wrap items-center gap-1">
          <span className="font-mono text-[10px] text-muted-foreground uppercase tracking-wider">Context:</span>
          {attached.map((a) => (
            <Badge key={a.ref} variant="secondary" className="gap-1 pr-1 font-mono text-[11px]">
              <span className="max-w-40 truncate" title={a.title}>
                {a.ref}
              </span>
              <Button
                variant="ghost"
                size="icon-xs"
                className="size-3.5 rounded-xs p-0 text-muted-foreground hover:text-foreground"
                aria-label={`Remove ${a.ref}`}
                onClick={() => setAttached((prev) => prev.filter((x) => x.ref !== a.ref))}
              >
                <Icon name="x" size={10} />
              </Button>
            </Badge>
          ))}
        </div>
      )}
      <PromptInput onSubmit={submit}>
        <PromptInputTextarea
          value={draft}
          aria-label="Message"
          placeholder="Ask, or /open, /layout, /run…"
          onChange={(e) => setDraft(e.target.value)}
        />
        <PromptInputSubmit
          status={busy ? "streaming" : "ready"}
          onStop={store.stop}
          disabled={!busy && !draft.trim() && attached.length === 0}
        />
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
