/**
 * A conversation with an agent host, kept outside any interface framework: a run goes on
 * whether or not anything is showing it, and whatever draws it subscribes.
 *
 * History policy (the one rule for when the thread is read):
 *
 *   1. The thread is read once, by start(), and kept in this store for the life of the page.
 *      Closing and opening a panel does not read it again.
 *   2. A run this store started, or one it picks up as active, is drawn from its event stream,
 *      never from the stored copy; a dropped stream resumes from the last event seen.
 *   3. The thread is read again only when someone else changed it: a turn started outside
 *      this page (turnElsewhere), or an explicit reload(). Never on a timer, never on focus.
 *   4. newThread() empties the store; nothing is read.
 *
 * Every tool runs on the server, so the turns this store sends declare none. Notifications
 * are coalesced to one per animation frame, so a burst of tokens is one redraw.
 */
import {
  type Approval,
  append,
  type ChatState,
  endRun,
  fromThread,
  initialState,
  reduceEvent,
  remove,
  startRun,
} from "./reduce.ts"
import { type Decision, type HostApi, HostError, type HostOptions, hostApi } from "./wire.ts"

export interface ChatStoreOptions extends HostOptions {
  /** A host made elsewhere (tests). */
  api?: HostApi
  /** How listeners are told: default once per animation frame (a microtask without one). */
  schedule?: (flush: () => void) => void
}

/** How a followed run ended: "finish", "error", "abort", or "lost" (the stream could not be followed). */
export type RunEnd = "finish" | "error" | "abort" | "lost"

const newId = () =>
  typeof crypto !== "undefined" && "randomUUID" in crypto
    ? crypto.randomUUID()
    : `c${Date.now().toString(36)}${Math.random().toString(36).slice(2)}`

const defaultSchedule = (flush: () => void) => {
  if (typeof requestAnimationFrame === "function") requestAnimationFrame(flush)
  else queueMicrotask(flush)
}

export function createChatStore(opts: ChatStoreOptions = {}) {
  const api = opts.api ?? hostApi(opts)
  const schedule = opts.schedule ?? defaultSchedule
  /** This page, to the host. */
  const client = newId()
  /** Runs this store started, so their agent.turn echo is not taken for someone else's. */
  const mine = new Set<string>()
  let state: ChatState = initialState
  let sending = false
  let started: Promise<void> | null = null
  let following: AbortController | null = null
  let pending = false
  let pendingSeq = 0
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

  async function follow(run: string) {
    following?.abort()
    const control = new AbortController()
    following = control
    set(startRun(state, run))
    let ended: RunEnd = "lost"
    const onEvent = (ev: Parameters<typeof reduceEvent>[1]) => {
      if (ev.verb === "run.finish") ended = "finish"
      else if (ev.verb === "run.error") ended = "error"
      else if (ev.verb === "run.abort") ended = "abort"
      set(reduceEvent(state, ev))
    }
    try {
      await api.follow(run, onEvent, control.signal)
    } catch {
      if (!control.signal.aborted)
        set({ ...state, error: "The connection to the agent was lost. Reload to pick it up." })
    }
    if (following === control) {
      following = null
      set(endRun(state))
      for (const fn of [...runEnd]) fn(run, ended)
    }
  }

  /** Read the thread and pick up a run that is still going. */
  async function read() {
    const loaded = await api.thread()
    set({ ...state, ...fromThread(loaded.messages, loaded.active_run), loaded: true, error: "" })
    if (loaded.active_run) void follow(loaded.active_run)
  }

  /** Ask the host what it is and read the thread, once. Safe to call more than once. */
  function start(): Promise<void> {
    started ??= (async () => {
      try {
        const host = await api.state()
        set({ ...state, host })
        if (!host.available) {
          set({ ...state, loaded: true })
          return
        }
        await read()
      } catch (err) {
        set({ ...state, loaded: true, error: err instanceof Error ? err.message : "The agent could not be reached." })
      }
    })()
    return started
  }

  /**
   * Send a message. With `auto`, it is one the application wrote and the person did not
   * type: the label is shown in its place. Resolves false when it was not sent (a run is
   * going, the host refused it); a message a reflex answered counts as sent.
   */
  async function send(text: string, how: { auto?: string } = {}): Promise<boolean> {
    if (state.run || sending || !text.trim()) return false
    sending = true
    const temp = `pending:${++pendingSeq}`
    set(
      append(
        { ...state, error: "" },
        how.auto ? { kind: "user", id: temp, text: "", auto: how.auto } : { kind: "user", id: temp, text: text.trim() },
      ),
    )
    try {
      const { run_id } = await api.turn({ text, client_id: client, ...(how.auto ? { auto: how.auto } : {}) })
      mine.add(run_id)
      sending = false
      void follow(run_id)
      return true
    } catch (err) {
      sending = false
      // A reflex answered instead of the model: the message stays, with its answer.
      if (err instanceof HostError && err.code === "halted") {
        set(append(state, { kind: "notice", id: `${temp}:halted`, text: err.message, tone: "quiet" }))
        return true
      }
      set({ ...remove(state, temp), error: err instanceof HostError ? err.message : "The message could not be sent." })
      return false
    }
  }

  function stop() {
    if (state.run) void api.cancel(state.run).catch(() => undefined)
  }

  async function answer(decision: Decision) {
    const q: Approval | null = state.approval
    if (!q) return
    set({ ...state, approval: null })
    await api.approve(q.run, q.id, decision).catch(() => undefined)
  }

  /** Put the thread away and start a new one. */
  async function newThread() {
    if (state.run) return
    try {
      await api.newThread()
      set({ ...state, ids: [], items: {}, error: "" })
    } catch (err) {
      set({ ...state, error: err instanceof Error ? err.message : "A new conversation could not be started." })
    }
  }

  /** Read the thread again (rule 3). Does nothing while a run is followed. */
  async function reload() {
    if (state.run || sending) return
    try {
      await read()
    } catch {
      // Left as it was.
    }
  }

  /**
   * Someone started a turn: the host announces every one. One this store did not start (the
   * agent driven from outside, another tab) is read and followed here too (rule 3).
   */
  function turnElsewhere(run: string) {
    if (!run || mine.has(run) || sending || state.run === run) return
    void reload()
  }

  return {
    client,
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
    reload,
    turnElsewhere,
    /** Told when a run this store followed has ended. Returns the way to stop being told. */
    onRunEnd: (fn: (run: string, how: RunEnd) => void) => {
      runEnd.add(fn)
      return () => {
        runEnd.delete(fn)
      }
    },
  }
}

export type ChatStore = ReturnType<typeof createChatStore>
