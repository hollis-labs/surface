/**
 * The page's one conversation with the agent, whichever wire the server speaks. The server
 * says which in /api/agent/setup: the conversation wire (a durable conversation the page
 * attaches to, shared with other tabs) or the run-at-a-time one. Both stores offer the same
 * surface, so the panel draws either; this wrapper exists because the answer arrives after
 * the page has started, and callers want the session at once.
 *
 * Two address-bar switches, for looking at the conversation wire's other modes:
 * `?chat=viewer` attaches this page as a viewer (it watches and can take over), and
 * `?chat=committed` takes each part whole instead of as it is written.
 */
import {
  type ChatState,
  type ChatStore,
  type ConversationStore,
  createChatStore,
  createConversationStore,
  type Decision,
  initialState,
  type RunEnd,
} from "@hollis-labs/surface-chat"
import { agentApi } from "./api"

const BASE = "/api/agent"

export interface AgentSession {
  get(): ChatState
  subscribe(fn: () => void): () => void
  start(): Promise<void>
  send(text: string, how?: { auto?: string }): Promise<boolean>
  stop(): void
  answer(decision: Decision): Promise<void>
  newThread(): Promise<void>
  turnElsewhere(run: string): void
  onRunEnd(fn: (run: string, how: RunEnd) => void): () => void
  /** True on the conversation wire. False until the server has said. */
  durable(): boolean
  /** This page's attachment on the conversation wire, to compare with the holder of the input. */
  attachment(): string
  /** "viewer" when this page only watches. */
  role(): "controller" | "viewer"
  /** Take the input from whoever holds it. */
  takeover(): Promise<boolean>
}

function mode(): { role: "controller" | "viewer"; stream: "deltas" | "committed" } {
  const want = typeof location === "undefined" ? "" : (new URLSearchParams(location.search).get("chat") ?? "")
  return { role: want === "viewer" ? "viewer" : "controller", stream: want === "committed" ? "committed" : "deltas" }
}

export function createAgentSession(): AgentSession {
  let run: ChatStore | null = null
  let conv: ConversationStore | null = null
  const inner = () => conv ?? run
  const listeners = new Set<() => void>()
  const ends = new Set<(run: string, how: RunEnd) => void>()
  const tell = () => {
    for (const fn of [...listeners]) fn()
  }
  const { role, stream } = mode()

  const boot = agentApi
    .setup()
    .catch(() => null)
    .then((setup) => {
      if (setup?.wire === "conversation" && setup.agent) {
        conv = createConversationStore({
          base: BASE,
          agent: setup.agent,
          role,
          stream,
          hostState: async () => ({ available: true, ready: setup.ready, model: setup.model }),
        })
      } else {
        run = createChatStore({ base: BASE })
      }
      // A page that goes away lets go of its attachment, and with it the input: otherwise a
      // reload would find its own earlier self still holding it.
      if (conv) {
        const leaving = conv
        window.addEventListener("pagehide", () => leaving.dispose(), { once: true })
      }
      const store = inner() as ChatStore | ConversationStore
      store.subscribe(tell)
      store.onRunEnd((id, how) => {
        for (const fn of [...ends]) fn(id, how)
      })
      tell()
      return store.start()
    })

  return {
    get: () => inner()?.get() ?? initialState,
    subscribe: (fn) => {
      listeners.add(fn)
      return () => {
        listeners.delete(fn)
      }
    },
    start: () => boot,
    send: async (text, how) => {
      await boot
      return (inner() as ChatStore | ConversationStore).send(text, how)
    },
    stop: () => inner()?.stop(),
    answer: async (decision) => {
      await inner()?.answer(decision)
    },
    newThread: async () => {
      await inner()?.newThread()
    },
    turnElsewhere: (id) => inner()?.turnElsewhere(id),
    onRunEnd: (fn) => {
      ends.add(fn)
      return () => {
        ends.delete(fn)
      }
    },
    durable: () => conv !== null,
    attachment: () => conv?.attachment() ?? "",
    role: () => (conv ? conv.role() : "controller"),
    takeover: async () => (conv ? conv.takeover() : false),
  }
}
