/**
 * The page's durable conversation. Setup supplies the agent and endpoint;
 * an unconfigured app keeps the composer unavailable without opening a transport.
 *
 * Two address-bar switches, for looking at the conversation wire's other modes:
 * `?chat=viewer` attaches this page as a viewer (it watches and can take over), and
 * `?chat=committed` takes each part whole instead of as it is written.
 */
import {
  type ChatState,
  type ConversationStore,
  createConversationStore,
  type Decision,
  initialState,
  type RunEnd,
} from "@hollis-labs/surface-chat"
import { agentApi, type RecoverAction } from "./api"

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
  /** Answer the interrupted turn on offer: resume, discard, or reset the conversation. */
  recover(action: RecoverAction): Promise<void>
}

function mode(): { role: "controller" | "viewer"; stream: "deltas" | "committed" } {
  const want = typeof location === "undefined" ? "" : (new URLSearchParams(location.search).get("chat") ?? "")
  return { role: want === "viewer" ? "viewer" : "controller", stream: want === "committed" ? "committed" : "deltas" }
}

export function createAgentSession(): AgentSession {
  let unavailable: ChatState = initialState
  let conv: ConversationStore | null = null
  const inner = () => conv
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
      if (setup?.ready && setup.agent) {
        conv = createConversationStore({
          base: setup.wire_base ?? BASE,
          agent: setup.agent,
          role,
          stream,
          hostState: async () => ({ available: true, ready: setup.ready, model: setup.model }),
        })
      } else {
        unavailable = {
          ...initialState,
          loaded: true,
          host: { available: true, ready: false, model: setup?.model ?? "" },
          error: setup?.reason ?? "The agent is not set up yet.",
        }
        tell()
        return
      }
      // A page that goes away lets go of its attachment, and with it the input: otherwise a
      // reload would find its own earlier self still holding it.
      if (conv) {
        const leaving = conv
        window.addEventListener("pagehide", () => leaving.dispose(), { once: true })
      }
      const store = inner() as ConversationStore
      store.subscribe(tell)
      store.onRunEnd((id, how) => {
        for (const fn of [...ends]) fn(id, how)
      })
      tell()
      return store.start()
    })

  return {
    get: () => inner()?.get() ?? unavailable,
    subscribe: (fn) => {
      listeners.add(fn)
      return () => {
        listeners.delete(fn)
      }
    },
    start: () => boot,
    send: async (text, how) => {
      await boot
      return conv ? conv.send(text, how) : false
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
    recover: async (action) => {
      await boot
      const id = conv?.conversation()
      if (!id) throw new Error("The conversation is not open yet.")
      // The outcome arrives on the stream (a discard or reset notice, the resumed run).
      await agentApi.recover(id, action)
    },
  }
}
