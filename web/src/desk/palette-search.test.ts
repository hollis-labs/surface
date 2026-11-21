import assert from "node:assert/strict"
import test from "node:test"
import { matchesPalette, type PaletteGroup, type PaletteResult, searchProviders } from "./palette-search.ts"

test("providers resolve independently and stale searches cannot publish", async () => {
  let finish!: (rows: PaletteResult[]) => void
  const slow = new Promise<PaletteResult[]>((resolve) => {
    finish = resolve
  })
  const states = new Map<string, PaletteGroup>()
  const cancel = searchProviders(
    [
      { id: "slow", title: "Slow", mode: "commands" },
      { id: "fast", title: "Fast", mode: "commands" },
      { id: "failed", title: "Failed", mode: "commands" },
    ],
    (p) =>
      p.id === "slow"
        ? slow
        : p.id === "failed"
          ? Promise.reject(new Error("offline"))
          : Promise.resolve([{ id: "one", title: "One" }]),
    (id, state) => states.set(id, state),
  )
  await Promise.resolve()
  assert.equal(states.get("slow")?.loading, true)
  assert.equal(states.get("fast")?.rows[0].id, "one")
  assert.equal(states.get("failed")?.error, "offline")
  cancel()
  finish([{ id: "stale", title: "Stale" }])
  await Promise.resolve()
  assert.equal(states.get("slow")?.loading, true)
})
test("command matching preserves multiword case-insensitive searches", () => {
  assert.equal(matchesPalette("NEW note", "Notes: new note"), true)
  assert.equal(matchesPalette("new task", "Notes: new note"), false)
})
