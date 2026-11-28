/**
 * Agent chat: the seats a seat host runs, their sessions, and a chat with each.
 *
 * Left: seats. A continuity seat has one session (click the seat); a concurrent seat lists its
 * sessions and offers a new one. Right: the selected session's chat on the agent/chat wire
 * (packages/chat), with its state, stop/restart/reset and logs.
 *
 * The composer never blocks: text sent while the agent is working queues and goes when the
 * run ends. Interrupting is the explicit stop button.
 *
 * The thread is the conversation: tool calls and reasoning fold into one activity line above
 * the composer (activity.tsx). A chat draws its latest messages at once and reads older ones
 * as you scroll up; search finds anything in it. The roster puts pinned seats first, then the
 * ones that answered recently (roster.ts). Approvals from every seat queue in one modal
 * (approvals.tsx), and anything the page can name can be forwarded to a seat (forward.ts).
 */
import {
  type ConversationStore,
  createConversationStore,
  type SearchHit,
  useChat,
  useChatBusy,
  useChatIds,
  useChatItem,
  useStickToBottom,
} from "@hollis-labs/surface-chat"
import {
  ChatItemView,
  Icon,
  type ModuleAPI,
  type ModuleWeb,
  ObjectActionsMenu,
  setSetting,
  useSetting,
  type ViewProps,
} from "@hollis-labs/surface-page/sdk"
import { type DragEvent, type FormEvent, memo, useCallback, useEffect, useMemo, useRef, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Conversation,
  ConversationContent,
  ConversationEmptyState,
  ConversationScrollButton,
  ConversationViewport,
} from "@/components/ui/conversation"
import { Input } from "@/components/ui/input"
import { MessageNotice } from "@/components/ui/message"
import { PromptInput, PromptInputSubmit, PromptInputTextarea } from "@/components/ui/prompt-input"
import { Spinner } from "@/components/ui/spinner"
import { ActivityLine, isFolded } from "./activity"
import { ApprovalsOverlay } from "./approvals"
import { setForwardSeats } from "./forward"
import { movePin, orderRoster, parsePins } from "./roster"

type State = "stopped" | "starting" | "idle" | "running" | "stalled" | "waiting" | "crashed"

interface Session {
  id: string
  seat: string
  title?: string
  created_at: string
  conversation_id?: string
  state: State
  last_error?: string
  last_event_at?: string
  active_run?: string
}

interface Seat {
  seat: string
  name: string
  summary?: string
  shape: "continuity" | "concurrent"
  max_sessions: number
  adapter: string
  model?: string
  effort?: string
  version?: string
  error?: string
  sessions: Session[]
}

interface SeatsReply {
  host: string
  seats: Seat[]
}

const WIRE_BASE = "/api/m/chat/h/chat/v1"
const POLL_MS = 4000

const stateTone: Record<State, string> = {
  stopped: "bg-[var(--color-muted-foreground)]",
  starting: "bg-amber-500",
  idle: "bg-emerald-500",
  running: "bg-sky-500 animate-pulse",
  stalled: "bg-orange-500",
  waiting: "bg-violet-500",
  crashed: "bg-red-500",
}

function StateDot({ state }: { state: State }) {
  return <span title={state} className={`inline-block size-2 shrink-0 rounded-full ${stateTone[state] ?? ""}`} />
}

function useSeats(api: ModuleAPI) {
  const [reply, setReply] = useState<SeatsReply | null>(null)
  const [error, setError] = useState("")
  const load = useCallback(async () => {
    try {
      setReply(await api.get<SeatsReply>("h/seats"))
      setError("")
    } catch (err) {
      setError(err instanceof Error ? err.message : "The seat host could not be reached.")
    }
  }, [api])
  useEffect(() => {
    void load()
    const t = setInterval(() => void load(), POLL_MS)
    return () => clearInterval(t)
  }, [load])
  return { reply, error, reload: load }
}

function ChatView({ win, desk, api }: ViewProps) {
  const { reply, error, reload } = useSeats(api)
  const selected = (win.args?.session as string | undefined) ?? ""
  const [busy, setBusy] = useState("")

  const select = useCallback(
    (id: string) => void desk.setArgs(win.id, { ...(win.args ?? {}), session: id }),
    [desk, win.id, win.args],
  )

  const open = useCallback(
    async (seat: string) => {
      setBusy(seat)
      try {
        const s = await api.post<Session>(`h/seats/${encodeURIComponent(seat)}/sessions`)
        select(s.id)
        await reload()
      } catch (err) {
        await desk.confirm({
          title: "Could not open a session",
          message: err instanceof Error ? err.message : String(err),
          confirmLabel: "OK",
        })
      } finally {
        setBusy("")
      }
    },
    [api, desk, reload, select],
  )

  const all = reply?.seats ?? []
  useEffect(() => setForwardSeats(all.map((s) => ({ seat: s.seat, name: s.name }))), [all])
  const pins = parsePins(useSetting<string>("chat.pinned_seats"))
  const recentMin = useSetting<number>("chat.recent_minutes") ?? 15
  const roster = orderRoster(all, pins, recentMin * 60_000, Date.now())
  const savePins = (next: string[]) => void setSetting("chat.pinned_seats", next.join(","))
  const togglePin = (seat: string) => savePins(pins.includes(seat) ? pins.filter((p) => p !== seat) : [...pins, seat])
  const [dragging, setDragging] = useState("")
  const dropOn = (before: string | null) => {
    if (dragging) savePins(movePin(pins, dragging, before))
    setDragging("")
  }
  const session = useMemo(() => all.flatMap((s) => s.sessions).find((s) => s.id === selected), [all, selected])
  const seat = useMemo(() => all.find((s) => s.seat === session?.seat), [all, session])

  useEffect(() => {
    desk.report(win.id, { kind: "chat", ref: selected ? `chat:${selected}` : "chat:seats" })
    void desk.setTitle(win.id, seat && session ? `Chat · ${seat.name}` : "Agent chat")
  }, [desk, win.id, selected, seat, session])

  return (
    <div className="flex h-full min-h-0 text-sm">
      <aside className="flex w-64 shrink-0 flex-col gap-1 overflow-y-auto border-r border-[var(--color-border)] p-2">
        <div className="flex items-center justify-between px-1 pb-1 text-xs text-[var(--color-muted-foreground)]">
          <span>{reply ? `Seats on ${reply.host}` : "Seats"}</span>
          <Button type="button" variant="ghost" size="icon-xs" title="Refresh" onClick={() => void reload()}>
            <Icon name="refresh-cw" size={12} />
          </Button>
        </div>
        {error && <MessageNotice tone="error">{error}</MessageNotice>}
        {roster.pinned.length > 0 && (
          <div
            className="flex flex-col"
            onDragOver={(e: DragEvent) => dragging && e.preventDefault()}
            onDrop={() => dropOn(null)}
          >
            <span className="px-1 pt-1 text-[10px] uppercase tracking-wide text-[var(--color-muted-foreground)]">
              Pinned
            </span>
            {roster.pinned.map((s) => (
              <div
                key={s.seat}
                draggable
                onDragStart={() => setDragging(s.seat)}
                onDragEnd={() => setDragging("")}
                onDragOver={(e: DragEvent) => dragging && e.preventDefault()}
                onDrop={(e: DragEvent) => {
                  e.stopPropagation()
                  dropOn(s.seat)
                }}
                className={dragging === s.seat ? "opacity-50" : ""}
              >
                <SeatRow
                  seat={s}
                  selected={selected}
                  busy={busy === s.seat}
                  onOpen={open}
                  onSelect={select}
                  pinned
                  onPin={togglePin}
                />
              </div>
            ))}
            <div className="my-1 border-b border-[var(--color-border)]" />
          </div>
        )}
        {roster.rest.map((s) => (
          <SeatRow
            key={s.seat}
            seat={s}
            selected={selected}
            busy={busy === s.seat}
            onOpen={open}
            onSelect={select}
            recent={roster.recent.has(s.seat)}
            onPin={togglePin}
          />
        ))}
        {reply && all.length === 0 && (
          <p className="px-1 text-xs text-[var(--color-muted-foreground)]">No seats on this host.</p>
        )}
      </aside>
      <main className="flex min-w-0 flex-1 flex-col">
        {session && seat ? (
          <SessionPane
            key={`${session.id}:${session.conversation_id ?? ""}`}
            api={api}
            seat={seat}
            session={session}
            onChanged={reload}
            onSelect={select}
          />
        ) : (
          <ConversationEmptyState
            icon={<Icon name="messages-square" size={28} />}
            title="Pick a seat"
            description="A continuity seat keeps one long-running session; a concurrent seat can run several."
          />
        )}
      </main>
    </div>
  )
}

function SeatRow({
  seat,
  selected,
  busy,
  onOpen,
  onSelect,
  pinned = false,
  recent = false,
  onPin,
}: {
  seat: Seat
  selected: string
  busy: boolean
  onOpen: (seat: string) => void
  onSelect: (id: string) => void
  pinned?: boolean
  recent?: boolean
  onPin: (seat: string) => void
}) {
  const continuity = seat.shape === "continuity"
  const only = continuity ? seat.sessions[0] : undefined
  const active = only ? only.id === selected : false
  return (
    <div className="group/seat relative flex flex-col">
      <Button
        variant="ghost"
        size="icon-xs"
        type="button"
        className={`absolute top-1 right-1 z-10 ${pinned ? "" : "opacity-0 group-hover/seat:opacity-100"}`}
        title={pinned ? "Unpin (drag pinned seats to reorder)" : "Pin to the top"}
        onClick={() => onPin(seat.seat)}
      >
        <Icon name={pinned ? "pin-off" : "pin"} size={11} />
      </Button>
      <Button
        variant="ghost"
        size="xs"
        type="button"
        disabled={!!seat.error || busy}
        onClick={() => (only ? onSelect(only.id) : continuity ? onOpen(seat.seat) : undefined)}
        className={`h-auto w-full justify-start flex items-center gap-2 rounded px-2 py-1.5 text-left hover:bg-[var(--color-accent)] ${active ? "bg-[var(--color-accent)]" : ""}`}
        title={seat.error || seat.summary}
      >
        {only ? <StateDot state={only.state} /> : <Icon name={continuity ? "user-round" : "users-round"} size={12} />}
        <span className="min-w-0 flex-1 truncate font-medium">{seat.name}</span>
        {recent && <span title="Recently active" className="size-1.5 shrink-0 rounded-full bg-sky-500" />}
        <Badge variant="outline" className="mr-5 text-[10px]">
          {continuity ? "continuity" : `${seat.sessions.length}/${seat.max_sessions}`}
        </Badge>
      </Button>
      {seat.error && <p className="px-2 text-xs text-red-500">{seat.error}</p>}
      {!continuity && (
        <div className="ml-4 flex flex-col">
          {seat.sessions.map((s) => (
            <Button
              variant="ghost"
              size="xs"
              key={s.id}
              type="button"
              onClick={() => onSelect(s.id)}
              className={`h-auto w-full justify-start flex items-center gap-2 rounded px-2 py-1 text-left text-xs hover:bg-[var(--color-accent)] ${s.id === selected ? "bg-[var(--color-accent)]" : ""}`}
            >
              <StateDot state={s.state} />
              <span className="min-w-0 flex-1 truncate">{s.title || "New session"}</span>
            </Button>
          ))}
          {seat.sessions.length < seat.max_sessions && (
            <Button
              variant="ghost"
              size="xs"
              type="button"
              disabled={busy}
              onClick={() => onOpen(seat.seat)}
              className="h-auto w-full justify-start flex items-center gap-2 rounded px-2 py-1 text-left text-xs text-[var(--color-muted-foreground)] hover:bg-[var(--color-accent)]"
            >
              <Icon name="plus" size={12} /> New session
            </Button>
          )}
        </div>
      )}
    </div>
  )
}

function SessionPane({
  api,
  seat,
  session,
  onChanged,
  onSelect,
}: {
  api: ModuleAPI
  seat: Seat
  session: Session
  onChanged: () => Promise<void>
  onSelect: (id: string) => void
}) {
  const [store, setStore] = useState<ConversationStore | null>(null)
  const messages = useSetting<number>("chat.history_messages") ?? 25
  const runs = Math.max(1, Math.ceil(messages / 2))
  // biome-ignore lint/correctness/useExhaustiveDependencies: a changed window size applies to the next chat opened
  useEffect(() => {
    const s = createConversationStore({ base: WIRE_BASE, agent: session.id, historyRuns: runs })
    setStore(s)
    void s.start()
    return () => s.dispose()
  }, [session.id])
  const [searching, setSearching] = useState(false)
  const [focus, setFocus] = useState("")

  const [logs, setLogs] = useState<string[] | null>(null)
  const [acting, setActing] = useState("")
  const act = useCallback(
    async (verb: "stop" | "restart" | "reset" | "start" | "archive") => {
      setActing(verb)
      try {
        if (verb === "archive") {
          await api.del(`h/sessions/${encodeURIComponent(session.id)}`)
          onSelect("")
        } else {
          await api.post(
            `h/sessions/${encodeURIComponent(session.id)}/${verb}`,
            verb === "reset" ? { attachment: store?.attachment() ?? "" } : {},
          )
        }
      } catch {
        // The state pill and last error say what happened.
      } finally {
        setActing("")
        await onChanged()
      }
    },
    [api, session.id, store, onChanged, onSelect],
  )
  const showLogs = useCallback(async () => {
    if (logs) return setLogs(null)
    const r = await api
      .get<{ lines: string[] }>(`h/sessions/${encodeURIComponent(session.id)}/logs?lines=200`)
      .catch(() => ({ lines: ["(could not read the log)"] }))
    setLogs(r.lines)
  }, [api, session.id, logs])

  return (
    <div className="flex h-full min-h-0 flex-col">
      <header className="flex items-center gap-2 border-b border-[var(--color-border)] px-3 py-2">
        <StateDot state={session.state} />
        <div className="min-w-0 flex-1">
          <div className="truncate font-medium">
            {seat.name}
            {seat.shape === "concurrent" && session.title ? (
              <span className="font-normal text-[var(--color-muted-foreground)]"> · {session.title}</span>
            ) : null}
          </div>
          <div className="truncate text-xs text-[var(--color-muted-foreground)]">
            {session.state} · {seat.adapter}
            {seat.model ? ` · ${seat.model}` : ""}
            {seat.effort ? ` · ${seat.effort}` : ""}
            {session.last_error ? ` · ${session.last_error}` : ""}
          </div>
        </div>
        {session.state === "stopped" || session.state === "crashed" ? (
          <Button size="sm" variant="outline" disabled={!!acting} onClick={() => void act("start")}>
            Start
          </Button>
        ) : (
          <Button
            size="sm"
            variant="ghost"
            disabled={!!acting}
            onClick={() => void act("stop")}
            title="Stop the agent process (the session stays resumable)"
          >
            Stop
          </Button>
        )}
        <Button
          size="sm"
          variant="ghost"
          disabled={!!acting}
          onClick={() => void act("restart")}
          title="Stop and resume the agent"
        >
          Restart
        </Button>
        {seat.shape === "continuity" ? (
          <Button
            size="sm"
            variant="ghost"
            disabled={!!acting}
            onClick={() => void act("reset")}
            title="A fresh session that continues from the seat's progress and worklist"
          >
            Reset
          </Button>
        ) : (
          <Button
            size="sm"
            variant="ghost"
            disabled={!!acting}
            onClick={() => void act("archive")}
            title="Stop and archive this session"
          >
            Close
          </Button>
        )}
        <Button size="sm" variant="ghost" onClick={() => setSearching((v) => !v)} title="Search this chat">
          <Icon name="search" size={14} />
        </Button>
        <Button size="sm" variant="ghost" onClick={() => void showLogs()} title="Agent log">
          <Icon name="scroll-text" size={14} />
        </Button>
      </header>
      {logs && (
        <pre className="max-h-48 overflow-auto border-b border-[var(--color-border)] bg-[var(--color-muted)] p-2 text-[11px] leading-tight">
          {logs.join("\n") || "(empty)"}
        </pre>
      )}
      {store && searching && <SearchBar store={store} onJump={setFocus} onClose={() => setSearching(false)} />}
      {store && <Thread store={store} session={session.id} seatName={seat.name} focus={focus} />}
      {store && <ActivityLine store={store} />}
      {store && <Composer store={store} />}
    </div>
  )
}

const Thread = memo(function Thread({
  store,
  session,
  seatName,
  focus,
}: {
  store: ConversationStore
  session: string
  seatName: string
  focus: string
}) {
  const ids = useChatIds(store)
  const error = useChat(store, (s) => s.error)
  const loaded = useChat(store, (s) => s.loaded)
  const more = useChat(store, (s) => s.older?.more ?? false)
  const loadingOlder = useChat(store, (s) => s.older?.loading ?? false)
  const { viewportRef, contentRef, atBottom, scrollToBottom } = useStickToBottom()
  const top = useRef<HTMLDivElement>(null)

  // Older messages load as the top comes into view; the reading position is kept.
  const older = useCallback(async () => {
    const view = viewportRef.current
    if (!view) return
    const height = view.scrollHeight
    const at = view.scrollTop
    await store.loadOlder()
    requestAnimationFrame(() => {
      view.scrollTop = at + (view.scrollHeight - height)
    })
  }, [store, viewportRef])
  useEffect(() => {
    const el = top.current
    const view = viewportRef.current
    if (!el || !view || !more) return
    const io = new IntersectionObserver((e) => e[0]?.isIntersecting && void older(), {
      root: view,
      rootMargin: "200px 0px 0px 0px",
    })
    io.observe(el)
    return () => io.disconnect()
  }, [more, older, viewportRef])

  // A search hit: bring its message into view and mark it for a moment.
  useEffect(() => {
    if (!focus) return
    const el = viewportRef.current?.querySelector(`[data-item="${CSS.escape(focus.split("#")[0] ?? "")}"]`)
    if (!el) return
    el.scrollIntoView({ block: "center" })
    el.classList.add("ring-1", "ring-[var(--color-primary)]")
    const t = setTimeout(() => el.classList.remove("ring-1", "ring-[var(--color-primary)]"), 2000)
    return () => clearTimeout(t)
  }, [focus, viewportRef])

  // The viewport is always drawn (the stick-to-bottom hook attaches to it once, on mount);
  // until the history window is in, it holds a spinner.
  return (
    <Conversation className="min-h-0 flex-1">
      <ConversationViewport ref={viewportRef}>
        <ConversationContent ref={contentRef} className="gap-2.5 p-3">
          {!loaded && (
            <div className="grid flex-1 place-items-center py-16">
              <Spinner />
            </div>
          )}
          <div ref={top} />
          {more && (
            <div className="flex justify-center">
              <Button size="xs" variant="ghost" disabled={loadingOlder} onClick={() => void older()}>
                {loadingOlder ? <Spinner className="size-3" /> : <Icon name="chevrons-up" size={12} />} Older messages
              </Button>
            </div>
          )}
          {loaded && ids.length === 0 && (
            <ConversationEmptyState
              icon={<Icon name="message-circle" size={24} />}
              title="Say hello"
              description="The seat's agent answers here."
            />
          )}
          {ids.map((id) => (
            <Row key={id} store={store} id={id} session={session} seatName={seatName} />
          ))}
          {error && <MessageNotice tone="error">{error}</MessageNotice>}
        </ConversationContent>
      </ConversationViewport>
      {!atBottom && <ConversationScrollButton onClick={scrollToBottom} />}
    </Conversation>
  )
})

const Row = memo(function Row({
  store,
  id,
  session,
  seatName,
}: {
  store: ConversationStore
  id: string
  session: string
  seatName: string
}) {
  const item = useChatItem(store, id)
  if (!item || isFolded(item)) return null
  const text = item.kind === "user" ? item.text : item.kind === "text" ? item.text : ""
  return (
    <div data-item={id} className="group/msg relative rounded">
      <ChatItemView item={item} />
      {text && !(item.kind === "text" && item.streaming) && (
        <div className="absolute top-0 right-0 opacity-0 transition-opacity group-hover/msg:opacity-100">
          <ObjectActionsMenu
            obj={{
              kind: "chat.message",
              id: `${session}/${id}`,
              title: `${item.kind === "user" ? "You" : seatName}: ${text.split("\n")[0]?.slice(0, 80) ?? ""}`,
              body: text,
            }}
            align="end"
          />
        </div>
      )}
    </div>
  )
})

/** Search the whole chat; a hit is read back to and scrolled to. */
function SearchBar({
  store,
  onJump,
  onClose,
}: {
  store: ConversationStore
  onJump: (id: string) => void
  onClose: () => void
}) {
  const [q, setQ] = useState("")
  const [hits, setHits] = useState<SearchHit[] | null>(null)
  const [busy, setBusy] = useState(false)
  const run = async (e: FormEvent) => {
    e.preventDefault()
    if (!q.trim()) return
    setBusy(true)
    try {
      setHits(await store.search(q))
    } catch {
      setHits([])
    } finally {
      setBusy(false)
    }
  }
  const jump = async (h: SearchHit) => {
    const id = await store.jumpTo(h)
    if (id) onJump(`${id}#${Date.now()}`)
  }
  return (
    <div className="flex flex-col gap-1 border-b border-[var(--color-border)] p-2">
      <form className="flex items-center gap-1.5" onSubmit={(e) => void run(e)}>
        <Input
          autoFocus
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder="Search this chat…"
          aria-label="Search this chat"
          className="h-7 text-xs"
        />
        <Button size="xs" type="submit" disabled={busy || !q.trim()}>
          {busy ? <Spinner className="size-3" /> : "Search"}
        </Button>
        <Button size="icon-xs" variant="ghost" type="button" title="Close search" onClick={onClose}>
          <Icon name="x" size={12} />
        </Button>
      </form>
      {hits && (
        <div className="flex max-h-48 flex-col overflow-y-auto">
          {hits.length === 0 && <p className="px-1 text-xs text-[var(--color-muted-foreground)]">No matches.</p>}
          {hits.map((h) => (
            <Button
              key={`${h.seq}`}
              variant="ghost"
              size="xs"
              type="button"
              className="h-auto justify-start whitespace-normal px-1 py-1 text-left text-xs"
              onClick={() => void jump(h)}
            >
              {h.snippet}
            </Button>
          ))}
        </div>
      )}
    </div>
  )
}

function Composer({ store }: { store: ConversationStore }) {
  const busy = useChatBusy(store)
  const approval = useChat(store, (s) => s.approval)
  const [draft, setDraft] = useState("")
  const [queue, setQueue] = useState<{ id: number; text: string }[]>([])
  const nextId = useRef(0)
  const queueRef = useRef(queue)
  queueRef.current = queue

  // When a run ends, send the next queued message.
  useEffect(
    () =>
      store.onRunEnd(() => {
        const [next, ...rest] = queueRef.current
        if (next === undefined) return
        setQueue(rest)
        // The store clears its run on the same event; send once it has.
        setTimeout(() => void store.send(next.text), 0)
      }),
    [store],
  )

  const submit = (e: FormEvent) => {
    e.preventDefault()
    const text = draft.trim()
    if (!text) return
    setDraft("")
    if (busy || queueRef.current.length > 0) setQueue((q) => [...q, { id: nextId.current++, text }])
    else void store.send(text)
  }

  return (
    <div className="flex flex-col gap-2 border-t border-[var(--color-border)] p-2">
      {approval && (
        <div className="flex items-center gap-2 rounded border border-violet-500/40 bg-violet-500/10 px-2 py-1.5 text-xs">
          <span className="min-w-0 flex-1 truncate">
            Approve <b>{approval.tool || "tool call"}</b>
            {approval.description ? `: ${approval.description}` : ""}
          </span>
          <Button size="sm" onClick={() => void store.answer("once")}>
            Allow
          </Button>
          {approval.standing && (
            <Button size="sm" variant="outline" onClick={() => void store.answer("session")}>
              Allow for session
            </Button>
          )}
          <Button size="sm" variant="ghost" onClick={() => void store.answer("deny")}>
            Deny
          </Button>
        </div>
      )}
      {queue.length > 0 && (
        <div className="flex flex-wrap gap-1">
          {queue.map((q) => (
            <Badge key={q.id} variant="secondary" className="max-w-full gap-1 text-xs">
              <Icon name="clock" size={10} />
              <span className="max-w-64 truncate">{q.text}</span>
              <Button
                type="button"
                variant="ghost"
                size="icon-xs"
                title="Remove"
                onClick={() => setQueue((all) => all.filter((x) => x.id !== q.id))}
              >
                <Icon name="x" size={10} />
              </Button>
            </Badge>
          ))}
          <span className="text-xs text-[var(--color-muted-foreground)]">
            queued: sent when the agent finishes, or stop it to send now
          </span>
        </div>
      )}
      <PromptInput onSubmit={submit}>
        <PromptInputTextarea
          value={draft}
          aria-label="Message"
          placeholder={busy ? "The agent is working. Type to queue the next message…" : "Message the seat…"}
          onChange={(e) => setDraft(e.target.value)}
        />
        <PromptInputSubmit
          status={busy && !draft.trim() ? "streaming" : "ready"}
          onStop={store.stop}
          disabled={!busy && !draft.trim()}
        />
      </PromptInput>
    </div>
  )
}

export default { views: { "chat/main": ChatView }, overlay: ApprovalsOverlay } satisfies ModuleWeb
