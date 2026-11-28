/**
 * Agent chat: the seats a seat host runs, their sessions, and a chat with each.
 *
 * Left: seats. A continuity seat has one session (click the seat); a concurrent seat lists its
 * sessions and offers a new one. Right: the selected session's chat on the agent/chat wire
 * (packages/chat), with its state, stop/restart/reset and logs.
 *
 * The composer never blocks: text sent while the agent is working queues and goes when the
 * run ends. Interrupting is the explicit stop button.
 */
import {
  type ConversationStore,
  createConversationStore,
  useChat,
  useChatBusy,
  useChatIds,
  useChatItem,
  useStickToBottom,
} from "@hollis-labs/surface-chat"
import { ChatItemView, Icon, type ModuleAPI, type ModuleWeb, type ViewProps } from "@hollis-labs/surface-page/sdk"
import { type FormEvent, memo, useCallback, useEffect, useMemo, useRef, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Conversation,
  ConversationContent,
  ConversationEmptyState,
  ConversationScrollButton,
  ConversationViewport,
} from "@/components/ui/conversation"
import { MessageNotice } from "@/components/ui/message"
import { PromptInput, PromptInputSubmit, PromptInputTextarea } from "@/components/ui/prompt-input"

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
        {all.map((s) => (
          <SeatRow key={s.seat} seat={s} selected={selected} busy={busy === s.seat} onOpen={open} onSelect={select} />
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
}: {
  seat: Seat
  selected: string
  busy: boolean
  onOpen: (seat: string) => void
  onSelect: (id: string) => void
}) {
  const continuity = seat.shape === "continuity"
  const only = continuity ? seat.sessions[0] : undefined
  const active = only ? only.id === selected : false
  return (
    <div className="flex flex-col">
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
        <Badge variant="outline" className="text-[10px]">
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
  useEffect(() => {
    const s = createConversationStore({ base: WIRE_BASE, agent: session.id })
    setStore(s)
    void s.start()
    return () => s.dispose()
  }, [session.id])

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
        <Button size="sm" variant="ghost" onClick={() => void showLogs()} title="Agent log">
          <Icon name="scroll-text" size={14} />
        </Button>
      </header>
      {logs && (
        <pre className="max-h-48 overflow-auto border-b border-[var(--color-border)] bg-[var(--color-muted)] p-2 text-[11px] leading-tight">
          {logs.join("\n") || "(empty)"}
        </pre>
      )}
      {store && <Thread store={store} />}
      {store && <Composer store={store} />}
    </div>
  )
}

const Thread = memo(function Thread({ store }: { store: ConversationStore }) {
  const ids = useChatIds(store)
  const error = useChat(store, (s) => s.error)
  const loaded = useChat(store, (s) => s.loaded)
  const { viewportRef, contentRef, atBottom, scrollToBottom } = useStickToBottom()
  return (
    <Conversation className="min-h-0 flex-1">
      <ConversationViewport ref={viewportRef}>
        <ConversationContent ref={contentRef} className="gap-2.5 p-3">
          {loaded && ids.length === 0 && (
            <ConversationEmptyState
              icon={<Icon name="message-circle" size={24} />}
              title="Say hello"
              description="The seat's agent answers here."
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

const Row = memo(function Row({ store, id }: { store: ConversationStore; id: string }) {
  const item = useChatItem(store, id)
  return item ? <ChatItemView item={item} /> : null
})

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

export default { views: { "chat/main": ChatView } } satisfies ModuleWeb
