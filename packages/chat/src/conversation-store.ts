/**
 * A conversation on the conversation wire, kept outside any interface framework. It offers
 * what the run store (store.ts) offers, so whatever draws one draws the other, and adds
 * what a durable conversation brings: it survives a reload, another tab can watch or take
 * over, and a turn started anywhere shows here.
 *
 * History policy, in one rule: everything is the event stream. start() attaches and reads
 * the stream from the beginning (history arrives without deltas); from then on the store
 * follows it, and a reconnect resumes after the last event seen. There is no separate
 * history read and nothing is fetched on a timer or on focus.
 *
 * It assumes nothing about which agent runs the turns: only the wire.
 */
import {
  type Attachment,
  type ConversationApi,
  type ConversationOptions,
  conversationApi,
  isClosed,
  isStale,
  type Role,
  type StreamMode,
} from "./conversation.ts"
import { reduceConversation, userItemId } from "./conversation-reduce.ts"
import { append, type ChatState, initialState, remove } from "./reduce.ts"
import type { RunEnd } from "./store.ts"
import { type Decision, HostError, type HostState, type StreamEvent } from "./wire.ts"

export interface ConversationStoreOptions extends ConversationOptions {
  /** The agent whose conversation this is. */
  agent: string
  /** "controller" may type; "viewer" only watches. Default controller. */
  role?: Role
  /** Default "deltas". */
  stream?: StreamMode
  /** Whether the host has an agent and a model. The wire does not say; a host that knows passes this. */
  hostState?: () => Promise<HostState>
  /** An api made elsewhere (tests). */
  api?: ConversationApi
  /** How listeners are told: default once per animation frame (a microtask without one). */
  schedule?: (flush: () => void) => void
}

const newId = () =>
  typeof crypto !== "undefined" && "randomUUID" in crypto
    ? crypto.randomUUID()
    : `c${Date.now().toString(36)}${Math.random().toString(36).slice(2)}`

const defaultSchedule = (flush: () => void) => {
  if (typeof requestAnimationFrame === "function") requestAnimationFrame(flush)
  else queueMicrotask(flush)
}

const ENDS: Record<string, RunEnd> = { "run.finish": "finish", "run.error": "error", "run.abort": "abort" }

export function createConversationStore(opts: ConversationStoreOptions) {
  const api = opts.api ?? conversationApi(opts)
  const schedule = opts.schedule ?? defaultSchedule
  const role: Role = opts.role ?? "controller"
  const mode: StreamMode = opts.stream ?? "deltas"

  let state: ChatState = initialState
  let conversation = ""
  let attachment = ""
  /** The head when this store attached: history is everything up to it. */
  let historyHead = 0
  let sending = false
  let started: Promise<void> | null = null
  let following: AbortController | null = null
  let disposed = false
  let pending = false
  const listeners = new Set<() => void>()
  const runEnd = new Set<(run: string, how: RunEnd) => void>()

  const flush = () => {
    pending = false
    for (const fn of [...listeners]) fn()
  }
  const set = (next: ChatState) => {
    if (next === state) return
    state = next
    if (!pending) {
      pending = true
      schedule(flush)
    }
  }

  const onEvent = (ev: StreamEvent) => {
    let next = reduceConversation(state, ev)
    if (!next.loaded && (next.cursor ?? 0) >= historyHead) next = { ...next, loaded: true }
    set(next)
    const how = ENDS[ev.verb]
    // Only a run that ended while this page watched is announced, not one replayed from history.
    if (how && ev.run_id && ev.seq > historyHead) for (const fn of [...runEnd]) fn(ev.run_id, how)
  }

  async function attach(): Promise<Attachment> {
    const a = await api.attach(conversation, role, mode)
    attachment = a.attachment_id
    return a
  }

  /** Follow the stream until this store is disposed, attaching again when it must. */
  async function follow() {
    following?.abort()
    const control = new AbortController()
    following = control
    while (!control.signal.aborted && !disposed) {
      try {
        await api.follow(conversation, attachment, state.cursor ?? 0, onEvent, control.signal)
        return
      } catch (err) {
        if (control.signal.aborted || disposed) return
        try {
          if (isStale(err)) {
            // The agent's side was replaced. The log and the cursor are as they were.
            await attach()
            continue
          }
          if (isClosed(err)) {
            await moveTo(String(err.data.conversation_id ?? ""))
            continue
          }
        } catch {
          // Could not attach again: reported below.
        }
        set({ ...state, loaded: true, error: "The connection to the agent was lost. Reload to pick it up." })
        return
      }
    }
  }

  /** The conversation was replaced by a new one: start over on it. */
  async function moveTo(id: string) {
    if (!id) {
      const now = await api.open(opts.agent)
      id = now.conversation_id
    }
    conversation = id
    const a = await attach()
    historyHead = a.head
    set({ ...initialState, host: state.host, loaded: a.head === 0 })
  }

  /** Attach to the agent's conversation and follow it, once. Safe to call more than once. */
  function start(): Promise<void> {
    started ??= (async () => {
      try {
        if (opts.hostState) {
          const host = await opts.hostState()
          set({ ...state, host })
          if (!host.available) {
            set({ ...state, loaded: true })
            return
          }
        } else {
          set({ ...state, host: { available: true, ready: true, model: "" } })
        }
        const conv = await api.open(opts.agent)
        conversation = conv.conversation_id
        const a = await attach()
        historyHead = a.head
        const holder = conv.lease?.attachment ?? conv.lease?.holder
        set({ ...state, loaded: a.head === 0, holder: holder ?? null, generation: conv.generation })
        void follow()
      } catch (err) {
        set({ ...state, loaded: true, error: err instanceof Error ? err.message : "The agent could not be reached." })
      }
    })()
    return started
  }

  /**
   * Send a message. With `auto`, it is one the application wrote and the person did not
   * type. Resolves false when it was not sent: a run is going, this page may not type, or
   * the host refused it. A message the host answered itself counts as sent.
   */
  async function send(text: string, how: { auto?: string } = {}): Promise<boolean> {
    if (state.run || sending || !text.trim() || !attachment) return false
    if (role !== "controller") {
      set({ ...state, error: "This window is only watching. Take over to type." })
      return false
    }
    sending = true
    const request = newId()
    const id = userItemId(request)
    set(
      append(
        { ...state, error: "" },
        how.auto ? { kind: "user", id, text: "", auto: how.auto } : { kind: "user", id, text: text.trim() },
      ),
    )
    const turn = () =>
      api.submit(conversation, {
        attachment,
        text,
        request_id: request,
        ...(how.auto ? { meta: { auto: how.auto } } : {}),
      })
    try {
      try {
        await turn()
      } catch (err) {
        if (!isStale(err)) throw err
        // Attach again and send the same request: its id makes the retry safe.
        await attach()
        await turn()
      }
      return true
    } catch (err) {
      // The host refused the turn and said why (a reflex stopped it): the message stays.
      if (err instanceof HostError && err.status === 422) {
        set(append(state, { kind: "notice", id: `${id}:refused`, text: err.message, tone: "quiet" }))
        return true
      }
      const message =
        err instanceof HostError && err.status === 403
          ? "Another window has the input. Take over to type here."
          : err instanceof HostError
            ? err.message
            : "The message could not be sent."
      set({ ...remove(state, id), error: message })
      return false
    } finally {
      sending = false
    }
  }

  function stop() {
    if (state.run) void api.cancel(conversation, state.run, attachment).catch(() => undefined)
  }

  /** Answer the question the agent is waiting on. "session" lasts this conversation. */
  async function answer(decision: Decision) {
    const q = state.approval
    if (!q) return
    set({ ...state, approval: null })
    await api
      .approve(
        conversation,
        q.id,
        attachment,
        decision === "deny"
          ? { decision: "deny", scope: "once" }
          : { decision: "allow", scope: decision === "session" ? "conversation" : decision },
      )
      .catch((err) => {
        if (err instanceof HostError) set({ ...state, approval: q, error: err.message })
      })
  }

  /** End this conversation and start a new one. Every attached page follows to it. */
  async function newThread() {
    if (state.run) return
    try {
      await api.reset(opts.agent)
      // This page's own stream ends with "conversation closed" and follow() moves on.
    } catch (err) {
      set({ ...state, error: err instanceof Error ? err.message : "A new conversation could not be started." })
    }
  }

  /** Take the right to type from whoever holds it. */
  async function takeover(): Promise<boolean> {
    try {
      await api.takeover(conversation, attachment)
      set({ ...state, holder: attachment, error: "" })
      return true
    } catch (err) {
      set({ ...state, error: err instanceof Error ? err.message : "The input could not be taken over." })
      return false
    }
  }

  /** Stop following and let go of the attachment. A run in progress goes on without this page. */
  function dispose() {
    disposed = true
    following?.abort()
    if (conversation && attachment) void api.detach(conversation, attachment).catch(() => undefined)
  }

  return {
    get: (): ChatState => state,
    subscribe: (fn: () => void) => {
      listeners.add(fn)
      return () => {
        listeners.delete(fn)
      }
    },
    start,
    send,
    stop,
    answer,
    newThread,
    takeover,
    dispose,
    /** This page's attachment, to compare with `holder`. Empty until attached. */
    attachment: () => attachment,
    role,
    /** The stream is the history: there is nothing to read again. Kept so a caller of the run store need not change. */
    reload: async () => {},
    turnElsewhere: (_run: string) => {},
    /** Told when a run ended while this page watched. Returns the way to stop being told. */
    onRunEnd: (fn: (run: string, how: RunEnd) => void) => {
      runEnd.add(fn)
      return () => {
        runEnd.delete(fn)
      }
    },
  }
}

export type ConversationStore = ReturnType<typeof createConversationStore>
