/**
 * The cards as the page knows them. The server holds each card and its state and streams
 * changes; the page keeps the latest of each, and the engine from @hollis-labs/surface-cards decides
 * where each one shows, from rules that are data.
 */
import {
  acceptsLateAnswer,
  type CardDraft,
  type CardOutcome,
  type CardRule,
  type CardViewModel,
  createCardEngine,
  createDraftStore,
  type Surface,
} from "@hollis-labs/surface-cards"
import { useSyncExternalStore } from "react"
import { ApiError, call, onEvent } from "../sdk/runtime"

/** A card as the server sends it. */
export interface ServerCard {
  id: string
  kind: string
  version?: string
  title?: string
  source?: string
  /** The outside agent that asked (a seat, over MCP); empty for the embedded agent. */
  agent?: string
  subject?: string
  risk?: string
  blocking?: boolean
  payload?: Record<string, unknown>
  status?: string
  outcome?: CardOutcome
  revision?: number
  superseded_by?: string
  created?: string
  responded?: string
  expires_at?: string
  /** Answered after its sender stopped waiting (an agent's question, abandoned or expired). */
  late?: boolean
  /** When a late answer reached the agent. */
  delivered?: string
}

/** How long after the agent stopped waiting a card is still offered for a late answer
 * (the server's cards.LateWindow). */
const LATE_WINDOW_MS = 7 * 24 * 60 * 60 * 1000

/** A closed card that still takes one late answer: the agent's question, which the agent
 * stopped waiting for (its run ended, or its wait ran out), not long ago. */
export function lateOpen(c: ServerCard): boolean {
  if (!acceptsLateAnswer(c)) return false
  const at = c.responded ? Date.parse(c.responded) : Number.NaN
  return !Number.isFinite(at) || Date.now() - at < LATE_WINDOW_MS
}

/** The card still takes an answer: it is open, or it takes a late one. */
export const isLive = (c: ServerCard) => !c.status || lateOpen(c)

/** What the owner typed into open cards, kept in the browser across a reload. */
const drafts = createDraftStore(typeof localStorage === "undefined" ? undefined : localStorage)
export const loadDraft = (id: string): CardDraft | undefined => drafts.load(id)
export function saveDraft(id: string, draft: CardDraft) {
  const c = cards.get(id)
  if (c && !isLive(c)) return // answered meanwhile: nothing to keep
  drafts.save(id, draft)
}

/** Answers given in this page, kept in the agent's panel for a brief moment as a tiny toast
 * so the owner sees them go ("Answer submitted · Sent to agent"). */
const lateShown = new Map<string, number>()
const LATE_SHOWN_MS = 3_200
export const recentlyAnsweredLate = (c: ServerCard) =>
  c.status === "submitted" && Date.now() - (lateShown.get(c.id) ?? 0) < LATE_SHOWN_MS

/**
 * Cards a module draws in a place of its own (the chat module's approvals queue claims the
 * MCP approval-bridge cards): the Cards window leaves them out.
 */
const claims = new Map<string, (c: ServerCard) => boolean>()
export function claimCards(id: string, claims_: (c: ServerCard) => boolean): () => void {
  claims.set(id, claims_)
  return () => {
    claims.delete(id)
  }
}
export const isClaimed = (c: ServerCard): boolean => [...claims.values()].some((fn) => fn(c))

/** Tracks whether the Cards window was opened automatically or from the chat stream, as opposed
 * to the user deliberately opening Cards from outside chat (menu / command palette). */
let openedFromChat = false
export const wasOpenedFromChat = () => openedFromChat
export const setOpenedFromChat = (fromChat: boolean) => {
  openedFromChat = fromChat
}

/** True when the user's keyboard/pointer focus is currently inside the agent chat sidebar. */
export function isUserInChat(): boolean {
  if (typeof document === "undefined") return false
  const active = document.activeElement
  if (active?.closest('[data-slot="app-frame-side"], [data-slot="agent-panel"], [data-slot="card-dock"]')) {
    return true
  }
  return false
}

export const ANSWERABLE = ["confirm", "prompt", "choice", "form", "approval"]
const answer = [{ id: "answer", label: "Answer" }]

/**
 * Where cards show. The agent's sit in its panel, beside the conversation that raised
 * them. A question from anything else pops up and stays in the Cards window until it is
 * answered; one its sender is waiting on takes the middle of the screen. Progress and
 * plans live in the Cards window. A plain notice pops up and goes.
 */
const rules: CardRule[] = [
  { id: "agent-asks", match: { source: "agent", kind: ANSWERABLE }, surface: "inline", actions: answer },
  { id: "agent-shows", match: { source: "agent" }, surface: "inline" },
  { id: "asks", match: { kind: ANSWERABLE }, surface: "toast", actions: answer, escalateBlocking: true },
  { id: "work", match: { kind: ["progress", "plan"] }, surface: "tray" },
  { id: "notices", match: {}, surface: "toast", ttlMs: 12_000 },
]

const engine = createCardEngine({ rules, now: () => Date.now() })
const cards = new Map<string, ServerCard>()
const stale = new Set<string>()
const subs = new Set<() => void>()
let version = 0
const emit = () => {
  version++
  for (const fn of subs) fn()
}
engine.subscribe(emit)

function take(c: ServerCard) {
  const have = cards.get(c.id)
  if (have && (have.revision ?? 0) > (c.revision ?? 0)) return
  const isNewLiveAgentQuestion = !have && c.source === "agent" && ANSWERABLE.includes(c.kind) && isLive(c)
  cards.set(c.id, c)
  const expiresAt = c.expires_at ? Date.parse(c.expires_at) : undefined
  engine.ingest({
    id: c.id,
    kind: c.kind,
    source: c.source,
    subject: c.subject,
    risk: c.risk,
    blocking: c.blocking,
    revision: c.revision,
    expiresAt: Number.isFinite(expiresAt) ? expiresAt : undefined,
  })
  if (c.status) engine.applyServerState(c.id, c.status, { supersededBy: c.superseded_by })
  // Answered or dismissed (here or elsewhere): its draft has done its job.
  if (!isLive(c)) drafts.clear(c.id)
  if (isNewLiveAgentQuestion && isUserInChat()) {
    openedFromChat = true
  }
  emit()
}

let started = false
/** Reads the open cards once and follows changes from then on. */
export function startCards() {
  if (started) return
  started = true
  onEvent("cards.changed", (ev) => take(ev.data as ServerCard))
  // The open cards, and the agent's questions that still take a late answer.
  void call<ServerCard[]>("GET", "/api/cards?late=1")
    .then((live) => {
      live.forEach(take)
      // A draft for a card that is no longer live (answered while this page was away).
      const ids = new Set(live.map((c) => c.id))
      drafts.prune((id) => ids.has(id))
    })
    .catch(() => {})
  // A notice that pops up goes away by itself; a question never does.
  window.setInterval(() => engine.dismissExpiredToasts(), 2000)
}

/** Sends the person's answer. A card that closed meanwhile comes back as it stands. */
export async function respond(id: string, outcome: CardOutcome): Promise<void> {
  try {
    const was = cards.get(id)
    if (was && (lateOpen(was) || was.source === "agent") && outcome.status === "submitted") {
      lateShown.set(id, Date.now())
      window.setTimeout(emit, LATE_SHOWN_MS + 100)
    }
    const updated = await call<ServerCard>("POST", `/api/cards/${encodeURIComponent(id)}/response`, outcome)
    take(updated)
    // When a classifier/training feedback card is answered, record the feedback with the classifier.
    if (outcome.status === "submitted" && updated.subject?.startsWith("classify:")) {
      const text = typeof updated.payload?.text === "string" ? updated.payload.text : ""
      const facet = updated.subject.slice("classify:".length) || "slot"
      const ans = outcome.answers?.[0]
      const label =
        typeof ans?.note === "string" && ans.note ? ans.note : typeof ans?.value === "string" ? ans.value : ""
      if (label) {
        void call("POST", "/api/agent/classify/feedback", {
          text,
          facet,
          label,
          accepted: ans?.value !== "other",
        }).catch(() => {})
      }
    }
  } catch (err) {
    // 409: the card is no longer open. Show what it became instead of an error.
    if (err instanceof ApiError && err.status === 409) {
      const now = await call<ServerCard>("GET", `/api/cards/${encodeURIComponent(id)}`).catch(() => null)
      if (now) take(now)
      else {
        stale.add(id)
        emit()
      }
      return
    }
    throw err
  }
}

export const dismissToast = (id: string) => engine.dismissToast(id)
export const isStale = (id: string) => stale.has(id)
export const cardById = (id: string) => cards.get(id)

/** Puts a card in front of the user from the page (such as a classifier feedback/training card). */
export async function showCard(card: Omit<ServerCard, "id">): Promise<ServerCard> {
  const created = await call<ServerCard>("POST", "/api/cards", card)
  take(created)
  return created
}

/** Releases a blocking card to the Cards window so the owner can chat with the agent or inspect the desk before answering. */
export async function holdCard(id: string): Promise<void> {
  const updated = await call<ServerCard>("POST", `/api/cards/${encodeURIComponent(id)}/hold`)
  take(updated)
}

/** Builds a CardOutcome from a card's saved draft when the person has filled it in, or null if incomplete. */
export function outcomeFromDraft(c: ServerCard, draft?: CardDraft): CardOutcome | null {
  if (!draft) return null
  if (typeof draft.__other === "string" && draft.__other.trim() !== "") {
    return {
      status: "submitted",
      answers: [{ questionId: "other", value: "other", note: draft.__other.trim() }],
    }
  }
  switch (c.kind) {
    case "prompt": {
      const text = typeof draft.text === "string" ? draft.text.trim() : ""
      return text ? { status: "submitted", answers: [{ questionId: "answer", value: text }] } : null
    }
    case "choice": {
      const picked = Array.isArray(draft.picked) ? draft.picked.filter((x): x is string => typeof x === "string") : []
      const other = typeof draft.other === "string" ? draft.other.trim() : ""
      if (picked.length === 0) return null
      const otherOn = picked.includes("other")
      if (otherOn && !other) return null
      const multiple = c.payload?.multiple === true
      return {
        status: "submitted",
        answers: [{ questionId: "choice", value: multiple ? picked : picked[0], ...(otherOn ? { note: other } : {}) }],
      }
    }
    case "form": {
      const { __other: _, ...values } = draft
      return Object.keys(values).length > 0 ? { status: "submitted", data: values } : null
    }
    default:
      return null
  }
}

export interface Shown {
  card: ServerCard
  view: CardViewModel
}

const subscribe = (fn: () => void) => {
  subs.add(fn)
  return () => {
    subs.delete(fn)
  }
}

const cache = new Map<Surface, { at: number; list: Shown[] }>()
function shown(surface: Surface): Shown[] {
  const hit = cache.get(surface)
  if (hit && hit.at === version) return hit.list
  const list = engine.select(surface).flatMap((view) => {
    const card = cards.get(view.id)
    return card ? [{ card, view }] : []
  })
  cache.set(surface, { at: version, list })
  return list
}

/** The cards on one surface, in the order they arrived. */
export function useCards(surface: Surface): Shown[] {
  return useSyncExternalStore(subscribe, () => shown(surface))
}

/** One card by id, following its changes. */
export function useCard(id: string): ServerCard | undefined {
  return useSyncExternalStore(subscribe, () => cards.get(id))
}

const byIdsCache = new Map<string, { at: number; list: ServerCard[] }>()
/** Several cards by id, following their changes in the store. */
export function useCardsByIds(ids: readonly string[]): ServerCard[] {
  const key = ids.join(",")
  return useSyncExternalStore(subscribe, () => {
    const hit = byIdsCache.get(key)
    if (hit && hit.at === version) return hit.list
    const list = ids.flatMap((id) => {
      const c = cards.get(id)
      return c ? [c] : []
    })
    byIdsCache.set(key, { at: version, list })
    return list
  })
}

/** Count of currently live (open or late-open) cards in the store. */
export function liveCardCount(): number {
  let n = 0
  for (const c of cards.values()) {
    if (isLive(c)) n++
  }
  return n
}
