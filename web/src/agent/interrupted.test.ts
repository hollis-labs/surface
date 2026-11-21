import assert from "node:assert/strict"
import test from "node:test"
import { interruptedActions, interruptedReason } from "./interrupted.ts"

test("an ordinary offer shows resume and discard", () => {
  const offer = { turn: "t1", actions: ["discard", "resume"], integrity: "", reason: "" }
  assert.deepEqual(interruptedActions(offer), ["resume", "discard"])
  assert.match(interruptedReason(offer), /stopped before the agent finished/)
})

test("a blocked offer shows only the offered discard and reset, with the reason", () => {
  const withTurn = { turn: "t1", actions: ["resume", "discard", "reset"], integrity: "blocked", reason: "" }
  assert.deepEqual(interruptedActions(withTurn), ["discard", "reset"])
  assert.match(interruptedReason(withTurn), /failed its integrity checks, so the interrupted turn cannot be resumed/)

  const resetOnly = { turn: "", actions: ["reset"], integrity: "blocked", reason: "orphan_tool_result" }
  assert.deepEqual(interruptedActions(resetOnly), ["reset"])
  assert.match(interruptedReason(resetOnly), /integrity checks \(orphan_tool_result\) and takes no new messages/)

  // Nothing that is not offered is shown.
  assert.deepEqual(interruptedActions({ ...withTurn, actions: ["discard"] }), ["discard"])
})
