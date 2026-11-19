import assert from "node:assert/strict"
import { describe, it } from "node:test"
import { createRefResolverClient } from "./resolver.ts"
import type { KindResolver, ResolveOutcome } from "./types.ts"

const tick = (ms = 5) => new Promise((r) => setTimeout(r, ms))

function fake(table: Record<string, string>, fail = { on: false }) {
  const calls: string[][] = []
  const r: KindResolver = {
    async resolveMany(ids) {
      calls.push([...ids])
      if (fail.on) throw new Error("down")
      const out: Record<string, ResolveOutcome> = {}
      for (const id of ids)
        out[id] = table[id] ? { status: "found", entity: { title: table[id] } } : { status: "not-found" }
      return out
    },
  }
  return { r, calls }
}

describe("resolver client", () => {
  it("batches requests in one window into one call per kind, and caches", async () => {
    let now = 0
    const { r, calls } = fake({ a: "A", b: "B" })
    const c = createRefResolverClient({ resolvers: { k: r }, batchWindowMs: 1, now: () => now })
    c.request([{ kind: "k", canonical: "a" }])
    c.request([
      { kind: "k", canonical: "b" },
      { kind: "k", canonical: "a" },
      { kind: "k", canonical: "z" },
    ])
    assert.equal(c.get("k", "a").status, "loading")
    await tick()
    assert.deepEqual(calls, [["a", "b", "z"]])
    const a = c.get("k", "a")
    assert.equal(a.status === "found" && a.entity.title, "A")
    assert.equal(c.get("k", "z").status, "not-found")
    c.request([{ kind: "k", canonical: "a" }])
    await tick()
    assert.equal(calls.length, 1, "a fresh answer is not asked again")
    now += 61_000
    c.request([{ kind: "k", canonical: "a" }])
    await tick()
    assert.equal(calls.length, 2, "a stale answer is asked again")
    c.dispose()
  })
  it("an error is an error, keeps the stale entity, and a kind with no resolver is unsupported", async () => {
    let now = 0
    const fail = { on: false }
    const { r } = fake({ a: "A" }, fail)
    const c = createRefResolverClient({ resolvers: { k: r }, batchWindowMs: 1, now: () => now })
    c.request([
      { kind: "k", canonical: "a" },
      { kind: "other", canonical: "x" },
    ])
    await tick()
    assert.equal(c.get("other", "x").status, "unsupported")
    fail.on = true
    now += 61_000
    c.request([{ kind: "k", canonical: "a" }])
    await tick()
    const s = c.get("k", "a")
    assert.equal(s.status, "error")
    assert.equal(s.status === "error" && s.stale?.title, "A")
    c.dispose()
  })
})
