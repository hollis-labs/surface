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

/** Late answers given in this page, kept in the agent's panel a while so the owner sees
 * them go ("Sent to the agent"). */
const lateShown = new Map<string, number>()
const LATE_SHOWN_MS = 15_000
export const recentlyAnsweredLate = (c: ServerCard) =>
  c.late === true && c.status === "submitted" && Date.now() - (lateShown.get(c.id) ?? 0) < LATE_SHOWN_MS

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
    if (was && lateOpen(was) && outcome.status === "submitted") {
      lateShown.set(id, Date.now())
      window.setTimeout(emit, LATE_SHOWN_MS + 100)
    }
    take(await call<ServerCard>("POST", `/api/cards/${encodeURIComponent(id)}/response`, outcome))
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
