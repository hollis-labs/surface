import assert from "node:assert/strict"
import { test } from "node:test"
import { fakeHost, says, until } from "../test/fake-host.ts"
import { reduceConversation } from "./conversation-reduce.ts"
import { createConversationStore } from "./conversation-store.ts"
import { type ChatItem, type ChatState, initialState } from "./reduce.ts"
import type { StreamEvent } from "./wire.ts"

const now = (flush: () => void) => flush()
const open = (host: ReturnType<typeof fakeHost>, more: Record<string, unknown> = {}) =>
  createConversationStore({ agent: "desk", fetch: host.fetch, schedule: now, ...more })

/** What a person would read: each item's kind and words, in order. */
const shown = (s: ChatState) =>
  s.ids.map((id) => {
    const it = s.items[id] as ChatItem
    if (it.kind === "user") return `user: ${it.auto ? `[${it.auto}]` : it.text}`
    if (it.kind === "text") return `agent: ${it.text}${it.streaming ? "…" : ""}`
    if (it.kind === "tool") return `tool ${it.name}: ${it.status}${it.result ? ` (${it.result})` : ""}`
    return `note: ${it.text}`
  })

test("a turn streams from the conversation, and the sender's message is not shown twice", async () => {
  const host = fakeHost()
  host.script((run) => says(run, ["Hel", "lo."]))
  const store = open(host)
  await store.start()
  assert.equal(await store.send("hi"), true)
  // Drawn at once, before the stream says anything.
  assert.deepEqual(shown(store.get())[0], "user: hi")
  await until(() => store.get().run === null && store.get().ids.length === 2)
  assert.deepEqual(shown(store.get()), ["user: hi", "agent: Hello."])
  assert.equal(store.get().holder, store.attachment())
})

test("a reload shows the history without replaying deltas, and a second tab sees the same", async () => {
  const host = fakeHost()
  host.script((run, text) => says(run, [`You said `, text, `.`]))
  const first = open(host)
  await first.start()
  await first.send("one")
  await until(() => first.get().run === null && first.get().ids.length === 2)
  await first.send("two")
  await until(() => first.get().run === null && first.get().ids.length === 4)

  const before = host.requests.length
  const reloaded = open(host)
  await reloaded.start()
  await until(() => reloaded.get().loaded && reloaded.get().ids.length === 4)
  assert.deepEqual(shown(reloaded.get()), shown(first.get()))
  assert.deepEqual(shown(reloaded.get()), ["user: one", "agent: You said one.", "user: two", "agent: You said two."])
  // History is one stream read from the start: no separate thread read.
  const paths = host.requests.slice(before).map((r) => `${r.method} ${r.path.replace(/att-\d+/, "att")}`)
  assert.deepEqual(paths, [
    "POST /agents/desk/conversation",
    `POST /conversations/${host.conversation().id}/attachments`,
    `GET /conversations/${host.conversation().id}/events?attachment=att&after=0`,
  ])
})

test("a tab that joins while the answer is being written gets what was written so far and the rest", async () => {
  const host = fakeHost()
  host.script(() => [])
  const sender = open(host)
  await sender.start()
  await sender.send("go")
  await until(() => sender.get().run !== null)
  const run = sender.get().run as string
  host.publish([
    { verb: "message.start", run_id: run, role: "assistant", message_id: "a" },
    { verb: "part.start", run_id: run, part_id: "p1", kind: "text" },
    { verb: "part.delta", run_id: run, part_id: "p1", text: "First half, " },
  ])
  const viewer = open(host, { role: "viewer" })
  await viewer.start()
  await until(() => shown(viewer.get()).includes("agent: First half, …"))
  host.publish([
    { verb: "part.delta", run_id: run, part_id: "p1", text: "second half." },
    { verb: "part.end", run_id: run, part_id: "p1", final: { text: "First half, second half." } },
    { verb: "run.finish", run_id: run, reason: "stop" },
  ])
  await until(() => viewer.get().run === null)
  assert.deepEqual(shown(viewer.get()), ["user: go", "agent: First half, second half."])
  assert.deepEqual(shown(sender.get()), shown(viewer.get()))
  // A viewer cannot type until it takes over, which makes it a controller.
  assert.equal(await viewer.send("me too"), false)
  assert.equal(viewer.role(), "viewer")
  assert.equal(await viewer.takeover(), true)
  assert.equal(viewer.role(), "controller")
  host.script((r) => says(r, ["ok"]))
  assert.equal(await viewer.send("now me"), true)
  await until(() => sender.get().run === null && sender.get().ids.length === 4)
  assert.equal(sender.get().holder, viewer.attachment())
})

test("a committed-only attachment draws the same final messages as a streaming one", async () => {
  const host = fakeHost()
  host.script((run) => [
    { verb: "message.start", run_id: run, role: "assistant", message_id: "a" },
    { verb: "part.start", run_id: run, part_id: "t1", kind: "tool_call", meta: { name: "notes_read" } },
    { verb: "part.end", run_id: run, part_id: "t1", final: { path: "a.md" } },
    { verb: "part.start", run_id: run, part_id: "r1", kind: "tool_result", meta: { call_id: "t1" } },
    { verb: "part.delta", run_id: run, part_id: "r1", text: "# A" },
    { verb: "part.end", run_id: run, part_id: "r1", final: { text: "# A", is_error: false } },
    ...says(run, ["It is ", "a note."]).slice(1),
  ])
  const streaming = open(host)
  const committed = open(host, { role: "viewer", stream: "committed" })
  await streaming.start()
  await committed.start()
  await streaming.send("read it")
  await until(() => streaming.get().run === null && streaming.get().ids.length === 3)
  await until(() => committed.get().run === null && committed.get().ids.length === 3)
  assert.deepEqual(shown(committed.get()), ["user: read it", "tool notes_read: done (# A)", "agent: It is a note."])
  assert.deepEqual(shown(committed.get()), shown(streaming.get()))
  assert.deepEqual(committed.get().items, streaming.get().items)
})

test("a dropped stream resumes after the last event seen, with nothing doubled or missed", async () => {
  const host = fakeHost()
  host.script(() => [])
  const store = open(host)
  await store.start()
  await store.send("go")
  await until(() => store.get().run !== null)
  const run = store.get().run as string
  host.publish([
    { verb: "message.start", run_id: run, role: "assistant", message_id: "a" },
    { verb: "part.start", run_id: run, part_id: "p1", kind: "text" },
    { verb: "part.delta", run_id: run, part_id: "p1", text: "one " },
  ])
  await until(() => shown(store.get()).includes("agent: one …"))
  host.dropStreams()
  host.publish([{ verb: "part.delta", run_id: run, part_id: "p1", text: "two " }])
  await until(() => shown(store.get()).includes("agent: one two …"), 4000)
  const resumed = host.requests.filter((r) => r.path.includes("/events")).at(-1)
  assert.ok(Number(resumed?.headers["Last-Event-ID"]) > 0, "the reconnect names the cursor it has")
})

test("after a generation bump the page attaches again and keeps its place", async () => {
  const host = fakeHost()
  host.script((run) => says(run, ["ok"]))
  const store = open(host)
  await store.start()
  await store.send("before")
  await until(() => store.get().run === null && store.get().ids.length === 2)
  const was = store.attachment()
  host.bump()
  await until(() => store.attachment() !== was && store.get().generation === 2, 4000)
  assert.equal(store.get().holder, null, "the lease is not carried across")
  assert.equal(await store.send("after"), true)
  await until(() => store.get().run === null && store.get().ids.length === 4)
  assert.deepEqual(shown(store.get()), ["user: before", "agent: ok", "user: after", "agent: ok"])
})

test("New resets the conversation and every attached page follows to the new one", async () => {
  const host = fakeHost()
  host.script((run) => says(run, ["ok"]))
  const a = open(host)
  const b = open(host, { role: "viewer" })
  await a.start()
  await b.start()
  await a.send("old talk")
  await until(() => b.get().run === null && b.get().ids.length === 2)
  const old = host.conversation().id
  await a.newThread()
  await until(() => a.get().ids.length === 0 && b.get().ids.length === 0, 4000)
  assert.notEqual(host.conversation().id, old)
  await a.send("new talk")
  await until(() => b.get().run === null && b.get().ids.length === 2, 4000)
  assert.deepEqual(shown(b.get()), ["user: new talk", "agent: ok"])
})

test("a second controller is refused until it takes over; then the first is", async () => {
  const host = fakeHost()
  host.script((run) => says(run, ["ok"]))
  const one = open(host)
  const two = open(host)
  await one.start()
  await two.start()
  await one.send("mine")
  await until(() => two.get().run === null && two.get().ids.length === 2)
  assert.equal(await two.send("not yet"), false)
  assert.match(two.get().error, /Another window has the input/)
  assert.deepEqual(shown(two.get()), ["user: mine", "agent: ok"], "the refused message is taken back")
  assert.equal(await two.takeover(), true)
  assert.equal(await two.send("now mine"), true)
  await until(() => one.get().run === null && one.get().ids.length === 4)
  assert.equal(one.get().holder, two.attachment())
  assert.equal(await one.send("too late"), false)
})

test("a turn the host refuses keeps the message and shows the reason", async () => {
  const host = fakeHost()
  const store = open(host)
  await store.start()
  host.refuseNext("halted", "A reflex stopped this: not during focus time.")
  assert.equal(await store.send("do the thing"), true)
  assert.deepEqual(shown(store.get()), ["user: do the thing", "note: A reflex stopped this: not during focus time."])
  assert.equal(store.get().run, null)
})

test("a message the app wrote shows its label, for the sender and for a viewer", async () => {
  const host = fakeHost()
  host.script((run) => says(run, ["done"]))
  const sender = open(host)
  const viewer = open(host, { role: "viewer" })
  await sender.start()
  await viewer.start()
  await sender.send("(long generated prompt)", { auto: "Morning summary" })
  await until(() => viewer.get().run === null && viewer.get().ids.length === 2)
  assert.deepEqual(shown(viewer.get()), ["user: [Morning summary]", "agent: done"])
  assert.deepEqual(shown(sender.get()), shown(viewer.get()))
})

test("the reducer ignores an event it has already taken, and a gap drops what was half drawn", () => {
  const evs: StreamEvent[] = [
    { seq: 1, verb: "run.start", run_id: "r" },
    { seq: 2, verb: "message.start", run_id: "r", role: "assistant" },
    { seq: 3, verb: "part.start", run_id: "r", part_id: "p", kind: "text" },
    { seq: 4, verb: "part.delta", run_id: "r", part_id: "p", text: "half" },
  ]
  let s = evs.reduce(reduceConversation, initialState)
  const same = reduceConversation(s, evs[3] as StreamEvent)
  assert.equal(same, s, "a repeated event changes nothing")
  // A gap carries no cursor: the place is kept.
  s = reduceConversation(s, { verb: "gap", reason: "retention" } as StreamEvent)
  assert.deepEqual(shown(s), ["note: Part of this conversation could not be replayed."])
  assert.equal(s.cursor, 4)
})

test("the Nanite runner's events draw a turn: steps, usage and slots change nothing, tools pair by call", () => {
  // The shapes are the real runner's (nanite/chatrunner): a step is step.start, an assistant
  // message, step.finish; a tool call's part is named by its call id and its result by
  // "<call>#result"; a call to a tool that does not exist is still a call and an error result.
  const run = "run_1"
  let seq = 0
  const ev = (verb: string, more: Record<string, unknown> = {}): StreamEvent =>
    ({ v: "chatstream/v1", seq: ++seq, run_id: run, verb, ...more }) as StreamEvent
  const events: StreamEvent[] = [
    ev("run.start"),
    ev("message.start", { role: "user", message_id: "msg_u", meta: { request_id: "r1" } }),
    ev("part.start", { part_id: "part_u", kind: "text" }),
    ev("part.end", { part_id: "part_u", final: { text: "what notes are there?" } }),
    ev("message.end", { message_id: "msg_u" }),
    ev("activity", { kind: "nanite.slots", value: { slots: [{ name: "agent", tokens: 331, changed: true }] } }),
    ev("usage", { usage: { scope: "delta", uncached_input: 4, output: 0, total: 4 } }),
    ev("step.start", { step_id: "step-1" }),
    ev("message.start", { role: "assistant", message_id: "msg-1" }),
    ev("part.start", { part_id: "msg-1.reasoning-1", kind: "reasoning" }),
    ev("part.delta", { part_id: "msg-1.reasoning-1", text: "thinking" }),
    ev("part.end", { part_id: "msg-1.reasoning-1", final: { text: "thinking" } }),
    ev("part.start", { part_id: "toolu_1", kind: "tool_call", meta: { call_id: "toolu_1", name: "notes_list" } }),
    ev("part.end", { part_id: "toolu_1", final: {} }),
    ev("part.start", { part_id: "toolu_2", kind: "tool_call", meta: { call_id: "toolu_2", name: "no_such_tool" } }),
    ev("part.end", { part_id: "toolu_2", final: { a: 1 } }),
    ev("part.start", { part_id: "toolu_1#result", kind: "tool_result", meta: { call_id: "toolu_1", is_error: false } }),
    ev("part.end", { part_id: "toolu_1#result", final: { text: '{"notes":[]}', is_error: false } }),
    ev("part.start", { part_id: "toolu_2#result", kind: "tool_result", meta: { call_id: "toolu_2", is_error: true } }),
    ev("part.end", { part_id: "toolu_2#result", final: { text: "unknown tool", is_error: true } }),
    ev("message.end", { message_id: "msg-1" }),
    ev("step.finish", { step_id: "step-1" }),
    ev("activity", { kind: "nanite.compact", value: { state: "start", before: 9000 } }),
    ev("activity", { kind: "nanite.compact", value: { state: "done", before: 9000, after: 1200 } }),
    ev("activity", { kind: "nanite.subagent", value: { state: "start", id: "c1" } }),
    ev("step.start", { step_id: "step-2" }),
    ev("message.start", { role: "assistant", message_id: "msg-2" }),
    ev("part.start", { part_id: "msg-2.text-1", kind: "text" }),
    ev("part.delta", { part_id: "msg-2.text-1", text: "None " }),
    ev("part.delta", { part_id: "msg-2.text-1", text: "yet." }),
    ev("part.end", { part_id: "msg-2.text-1", final: { text: "None yet." } }),
    ev("message.end", { message_id: "msg-2" }),
    ev("step.finish", { step_id: "step-2" }),
    ev("run.finish", { reason: "stop", usage: { scope: "final", total: 10 } }),
  ]
  let s: ChatState = initialState
  const idsAt: number[] = []
  for (const e of events) {
    s = reduceConversation(s, e)
    idsAt.push(s.ids.length)
  }
  assert.deepEqual(shown(s), [
    "user: what notes are there?",
    'tool notes_list: done ({"notes":[]})',
    "tool no_such_tool: error (unknown tool)",
    "note: The older part of this conversation was summarised for the model. Every message is still here.",
    "agent: None yet.",
  ])
  assert.equal(s.run, null)
  // Steps, usage, slots, reasoning and subagent activity add no row.
  for (const i of [5, 6, 7, 9, 10, 11, 21, 24, 25]) assert.equal(idsAt[i], idsAt[i - 1], events[i].verb)
  const call = s.items[`${run}:toolu_2`]
  assert.deepEqual(call?.kind === "tool" && call.input, { a: 1 })
})
