import assert from "node:assert/strict"
import { test } from "node:test"
import { createDraftStore, type DraftStorage } from "./drafts.ts"

/** A Storage like the browser's, kept in a map. Two stores over one map are a page and
 * the same page after a reload. */
function memoryStorage(): DraftStorage & { map: Map<string, string> } {
  const map = new Map<string, string>()
  return {
    map,
    getItem: (k) => map.get(k) ?? null,
    setItem: (k, v) => void map.set(k, v),
    removeItem: (k) => void map.delete(k),
    get length() {
      return map.size
    },
    key: (i) => [...map.keys()][i] ?? null,
  }
}

test("a draft typed into a card survives a reload and goes once the card is answered", () => {
  const storage = memoryStorage()
  const before = createDraftStore(storage)
  before.save("cd_1", { name: "Atlas", size: "3" })
  before.save("cd_2", { text: "half a thought" })

  const after = createDraftStore(storage) // the page reloaded
  assert.deepEqual(after.load("cd_1"), { name: "Atlas", size: "3" })
  assert.deepEqual(after.load("cd_2"), { text: "half a thought" })
  assert.equal(after.load("cd_3"), undefined)

  after.clear("cd_1") // answered
  assert.equal(createDraftStore(storage).load("cd_1"), undefined)
  assert.deepEqual(createDraftStore(storage).load("cd_2"), { text: "half a thought" })
})

test("an empty draft removes it, and drafts of cards that are gone are pruned", () => {
  const storage = memoryStorage()
  const drafts = createDraftStore(storage)
  drafts.save("a", { text: "x" })
  drafts.save("a", {})
  assert.equal(drafts.load("a"), undefined)
  drafts.save("b", { text: "keep" })
  drafts.save("c", { text: "gone" })
  storage.setItem("someone-else", "untouched")
  drafts.prune((id) => id === "b")
  assert.deepEqual(drafts.load("b"), { text: "keep" })
  assert.equal(drafts.load("c"), undefined)
  assert.equal(storage.getItem("someone-else"), "untouched")
})

test("no storage, a full one, or a draft that is not an object costs the draft, not the card", () => {
  const none = createDraftStore(undefined)
  none.save("a", { text: "x" })
  assert.equal(none.load("a"), undefined)

  const storage = memoryStorage()
  const full = createDraftStore({
    ...storage,
    length: 0,
    setItem: () => {
      throw new Error("QuotaExceededError")
    },
  })
  assert.doesNotThrow(() => full.save("a", { text: "x" }))

  storage.setItem("surface.card-draft:odd", "[1,2]")
  storage.setItem("surface.card-draft:bad", "{not json")
  const drafts = createDraftStore(storage)
  assert.equal(drafts.load("odd"), undefined)
  assert.equal(drafts.load("bad"), undefined)
})
