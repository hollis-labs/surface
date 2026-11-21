import assert from "node:assert/strict"
import { test } from "node:test"
import { append, endRun, initialState, reduceEvent, startRun } from "./reduce.ts"

const ev = (verb: string, more: Record<string, unknown> = {}) => ({ seq: 0, run_id: "r1", verb, ...more })

function streaming() {
  let s = append(initialState, { kind: "user", id: "u1", text: "hi" })
  s = startRun(s, "r1")
  s = reduceEvent(s, ev("part.start", { part_id: "p1", kind: "text" }))
  return s
}

test("a streamed token changes only the last item", () => {
  const before = streaming()
  const after = reduceEvent(before, ev("part.delta", { part_id: "p1", text: "Hel" }))
  assert.equal(after.ids, before.ids, "the order keeps its identity")
  assert.equal(after.items.u1, before.items.u1, "earlier items keep their identity")
  assert.notEqual(after.items["r1:p1"], before.items["r1:p1"])
  assert.deepEqual(after.items["r1:p1"], { kind: "text", id: "r1:p1", text: "Hel", streaming: true })
})

test("a new part adds to the order", () => {
  const s = streaming()
  assert.deepEqual(s.ids, ["u1", "r1:p1"])
})

test("a tool call takes its input at the end and its result as it streams", () => {
  let s = startRun(initialState, "r1")
  s = reduceEvent(s, ev("part.start", { part_id: "c1", kind: "tool_call", meta: { name: "desk_open" } }))
  s = reduceEvent(s, ev("part.end", { part_id: "c1", final: { view: "notes" } }))
  s = reduceEvent(s, ev("part.start", { part_id: "x1", kind: "tool_result", meta: { call_id: "c1" } }))
  s = reduceEvent(s, ev("part.delta", { part_id: "x1", text: "opened" }))
  assert.deepEqual(s.items["r1:c1"], {
    kind: "tool",
    id: "r1:c1",
    name: "desk_open",
    input: { view: "notes" },
    result: "opened",
    status: "done",
  })
})

test("events of another run change nothing", () => {
  const s = streaming()
  assert.equal(reduceEvent(s, { seq: 1, run_id: "r2", verb: "part.delta", part_id: "p1", text: "x" }), s)
})

test("an approval is asked and cleared by its activity", () => {
  let s = startRun(initialState, "r1")
  s = reduceEvent(s, ev("approval.request", { approval_id: "a1", descriptor: { tool: "drafts_write", input: {} } }))
  assert.equal(s.approval?.id, "a1")
  s = reduceEvent(s, ev("activity", { kind: "ade.approval", value: { approval_id: "a1" } }))
  assert.equal(s.approval, null)
})

test("the end of a run settles what was still going", () => {
  let s = streaming()
  s = reduceEvent(s, ev("part.start", { part_id: "c1", kind: "tool_call", meta: { name: "x" } }))
  s = endRun(reduceEvent(s, ev("run.error", { message: "boom" })))
  assert.equal(s.run, null)
  assert.equal((s.items["r1:p1"] as { streaming: boolean }).streaming, false)
  assert.equal((s.items["r1:c1"] as { status: string }).status, "done")
  assert.deepEqual(s.items["r1:error"], { kind: "notice", id: "r1:error", text: "boom", tone: "error" })
})

test("Nanite's daily budget owner notice reaches the conversation", () => {
  const s = reduceEvent(
    startRun(initialState, "r1"),
    ev("activity", {
      kind: "nanite.notice",
      value: { id: "budget-day", kind: "budget", body: "Daily token budget reached 80%: 80/100 tokens." },
    }),
  )
  assert.deepEqual(s.items["r1:budget:budget-day"], {
    kind: "notice",
    id: "r1:budget:budget-day",
    text: "Daily token budget reached 80%: 80/100 tokens.",
    tone: "quiet",
  })
})
