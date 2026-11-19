/**
 * What the person has typed into an open card, kept in the browser until they answer it.
 *
 * A card's own state lives in its component, so a reload or a server restart would lose a
 * half-filled form. A draft is saved per card id as they type, read back when the card is
 * drawn again, and removed once the card is answered or dismissed. It stays in the browser
 * (any `Storage`, `localStorage` in a page): the server never sees a draft.
 */

/** The part of `Storage` a draft store needs. */
export interface DraftStorage {
  getItem(key: string): string | null
  setItem(key: string, value: string): void
  removeItem(key: string): void
  readonly length: number
  key(index: number): string | null
}

/** A card's draft: whatever the card's component keeps, by name. */
export type CardDraft = Readonly<Record<string, unknown>>

export interface DraftStore {
  /** The draft saved for a card, or undefined. */
  load(id: string): CardDraft | undefined
  /** Saves a card's draft. An empty one removes it. */
  save(id: string, draft: CardDraft): void
  /** Removes a card's draft: it was answered or dismissed. */
  clear(id: string): void
  /** Removes every draft whose card id `keep` refuses (cards that are gone). */
  prune(keep: (id: string) => boolean): void
}

const PREFIX = "surface.card-draft:"

/** A draft store over `storage`. Without one (no `localStorage`) it keeps nothing. Storage
 * errors (full, blocked) are swallowed: a lost draft is not worth breaking the card for. */
export function createDraftStore(storage: DraftStorage | undefined, prefix = PREFIX): DraftStore {
  const guard = <T>(fn: () => T, fallback: T): T => {
    if (!storage) return fallback
    try {
      return fn()
    } catch {
      return fallback
    }
  }
  return {
    load(id) {
      return guard(() => {
        const raw = storage?.getItem(prefix + id)
        if (!raw) return undefined
        const v: unknown = JSON.parse(raw)
        return v && typeof v === "object" && !Array.isArray(v) ? (v as CardDraft) : undefined
      }, undefined)
    },
    save(id, draft) {
      guard(() => {
        if (Object.keys(draft).length === 0) storage?.removeItem(prefix + id)
        else storage?.setItem(prefix + id, JSON.stringify(draft))
      }, undefined)
    },
    clear(id) {
      guard(() => storage?.removeItem(prefix + id), undefined)
    },
    prune(keep) {
      guard(() => {
        if (!storage) return
        const gone: string[] = []
        for (let i = 0; i < storage.length; i++) {
          const k = storage.key(i)
          if (k?.startsWith(prefix) && !keep(k.slice(prefix.length))) gone.push(k)
        }
        for (const k of gone) storage.removeItem(k)
      }, undefined)
    },
  }
}
