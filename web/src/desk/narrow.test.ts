import assert from "node:assert/strict"
import test from "node:test"
import { narrowProjection } from "./narrow.ts"

test("a narrow desk shows only the focused window, full frame", () => {
  const wins = [{ id: "a" }, { id: "b" }, { id: "c" }]
  const p = narrowProjection(wins, "b")
  assert.deepEqual([...p.visibleIds], ["b"])
  assert.deepEqual(p.backgroundIds, ["a", "c"])
  assert.deepEqual(p.projectedTree, {
    root: { type: "row", children: [{ type: "stack", id: "stack-narrow", wins: ["b"], selected: 0 }] },
  })
})

test("with no focus it shows the newest window, and nothing on an empty desk", () => {
  assert.deepEqual([...narrowProjection([{ id: "a" }, { id: "b" }], "").visibleIds], ["b"])
  const empty = narrowProjection([], "")
  assert.equal(empty.visibleIds.size, 0)
  assert.deepEqual(empty.projectedTree, { root: { type: "row", children: [] } })
})
