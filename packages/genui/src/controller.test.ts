import assert from "node:assert/strict"
import { test } from "node:test"
import { fixture, response } from "../test/helpers.ts"
import { GenuiController, type GenuiTransport } from "./controller.ts"

/** A transport that answers each submit with the next canned outcome and records the calls. */
function transport(outcomes: unknown[], confirm?: () => Promise<string>) {
  const sent: string[] = []
  const t: GenuiTransport = {
    submit: async (call) => {
      sent.push(call)
      return response(call, outcomes.shift())
    },
    recover: async () => {
      throw new Error("not used")
    },
    ...(confirm ? { confirm } : {}),
  }
  return { t, sent }
}
const ids = () => {
  let n = 0
  return () => `call-${++n}`
}

test("a choice is submitted once and a known terminal outcome ends the surface", async () => {
  const { t, sent } = transport([{ status: "ok", receipt: "done" }])
  const c = new GenuiController(fixture("approval"), t, ids())
  assert.equal(c.getSnapshot().phase, "ready")
  await c.submit(0)
  const s = c.getSnapshot()
  assert.equal(s.phase, "terminal")
  assert.equal(s.observation?.kind, "known")
  // Terminal means no further submission, whatever is clicked.
  await c.submit(1)
  assert.equal(sent.length, 1)
})

test("an outcome this build does not know is left unresolved, never treated as success", async () => {
  const { t } = transport([{ status: "teleported" }])
  const c = new GenuiController(fixture("approval"), t, ids())
  await c.submit(0)
  assert.equal(c.getSnapshot().phase, "unknown")
  assert.equal(c.getSnapshot().observation?.kind, "unknown")
})

test("a transport failure is uncertainty: nothing is retried", async () => {
  let calls = 0
  const c = new GenuiController(
    fixture("approval"),
    {
      submit: async () => {
        calls++
        throw new Error("network")
      },
      recover: async () => {
        throw new Error("not used")
      },
    },
    ids(),
  )
  await c.submit(0)
  assert.equal(c.getSnapshot().phase, "unknown")
  await c.submit(0)
  assert.equal(calls, 1)
})

test("a confirmation is the host's proof attached to the same action with a fresh call id", async () => {
  const need = { status: "needs_confirmation", confirm: { what: "Confirm it", scope: ["answer"], ttlMs: 60000 } }
  const { t, sent } = transport([need, { status: "ok" }], async () => "proof-from-host")
  const c = new GenuiController(fixture("approval"), t, ids())
  await c.submit(0)
  assert.equal(c.getSnapshot().phase, "confirmation")
  assert.equal(c.getSnapshot().canConfirm, true)
  await c.confirm()
  assert.equal(c.getSnapshot().phase, "terminal")
  const [first, second] = sent.map((raw) => JSON.parse(raw).callAgentFunction)
  assert.equal(second.callFunction.args.genui_action.id, first.callFunction.args.genui_action.id)
  assert.equal(second.callFunction.args.genui_action.confirmation, "proof-from-host")
  assert.equal(first.callFunction.args.genui_action.confirmation, undefined)
  assert.notEqual(second.functionCallId, first.functionCallId)
})

test("a form refuses a draft that breaks its fields before anything is sent", async () => {
  const { t, sent } = transport([{ status: "ok" }])
  const c = new GenuiController(fixture("form"), t, ids())
  await assert.rejects(c.submit(0, { no_such_field: "x" }))
  assert.equal(sent.length, 0)
})
