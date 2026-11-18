import assert from "node:assert/strict"
import { test } from "node:test"
import { createChatStore } from "./store.ts"
import { type HostApi, HostError, type StreamEvent } from "./wire.ts"

/** A host that answers from a script and lets the test push a run's events. */
function fakeHost() {
  const calls: string[] = []
  let push: ((ev: StreamEvent) => void) | null = null
  let finish: (() => void) | null = null
  let turnError: HostError | null = null
  let runs = 0
  const api: HostApi = {
    state: async () => {
      calls.push("state")
      return { available: true, ready: true, model: "m" }
    },
    thread: async () => {
      calls.push("thread")
      return { thread_id: "t", messages: [], active_run: "" }
    },
    newThread: async () => void calls.push("newThread"),
    turn: async () => {
      calls.push("turn")
      if (turnError) throw turnError
      return { run_id: `r${++runs}` }
    },
    cancel: async () => void calls.push("cancel"),
    approve: async () => void calls.push("approve"),
    follow: (_run, onEvent) =>
      new Promise<void>((resolve) => {
        push = onEvent
        finish = resolve
      }),
  }
  return {
    api,
    calls,
    emit: (ev: Partial<StreamEvent> & { verb: string }) => push?.({ seq: 0, ...ev }),
    end: () => finish?.(),
    failTurns: (e: HostError) => {
      turnError = e
    },
  }
}

/** A schedule the test flushes by hand, to count notifications. */
function manual() {
  const queue: (() => void)[] = []
  return {
    schedule: (f: () => void) => void queue.push(f),
    flush: () => {
      for (const f of queue.splice(0)) f()
    },
  }
}

const tick = () => new Promise((r) => setTimeout(r, 0))

test("the thread is read once, however often start is called", async () => {
  const host = fakeHost()
  const store = createChatStore({ api: host.api, schedule: queueMicrotask })
  await Promise.all([store.start(), store.start()])
  await store.start()
  assert.deepEqual(host.calls, ["state", "thread"])
})

test("a burst of tokens is one notification", async () => {
  const host = fakeHost()
  const clock = manual()
  const store = createChatStore({ api: host.api, schedule: clock.schedule })
  let told = 0
  store.subscribe(() => told++)
  await store.start()
  await store.send("hello")
  await tick()
  clock.flush()
  told = 0
  host.emit({ verb: "part.start", part_id: "p", kind: "text" })
  for (const t of ["a", "b", "c", "d", "e"]) host.emit({ verb: "part.delta", part_id: "p", text: t })
  clock.flush()
  assert.equal(told, 1)
  const s = store.get()
  assert.equal((s.items[s.ids[s.ids.length - 1]] as { text: string }).text, "abcde")
})

test("a turn this store started is not read again when the host announces it", async () => {
  const host = fakeHost()
  const store = createChatStore({ api: host.api, schedule: queueMicrotask })
  await store.start()
  await store.send("hello")
  store.turnElsewhere("r1")
  await tick()
  assert.deepEqual(host.calls, ["state", "thread", "turn"])
})

test("a turn started elsewhere is read and followed", async () => {
  const host = fakeHost()
  const store = createChatStore({ api: host.api, schedule: queueMicrotask })
  await store.start()
  store.turnElsewhere("r9")
  await tick()
  assert.deepEqual(host.calls, ["state", "thread", "thread"])
})

test("a message a reflex answered stays, with the answer", async () => {
  const host = fakeHost()
  host.failTurns(new HostError(422, "Cite your sources first.", "halted"))
  const store = createChatStore({ api: host.api, schedule: queueMicrotask })
  await store.start()
  assert.equal(await store.send("summarise this"), true)
  const s = store.get()
  assert.deepEqual(
    s.ids.map((id) => s.items[id]?.kind),
    ["user", "notice"],
  )
  assert.equal(s.run, null)
})

test("a refused message is taken back and the reason shown", async () => {
  const host = fakeHost()
  host.failTurns(new HostError(503, "The agent is not set up yet."))
  const store = createChatStore({ api: host.api, schedule: queueMicrotask })
  await store.start()
  assert.equal(await store.send("hi"), false)
  assert.equal(store.get().ids.length, 0)
  assert.equal(store.get().error, "The agent is not set up yet.")
})

test("the end of the stream ends the run and tells who asked", async () => {
  const host = fakeHost()
  const store = createChatStore({ api: host.api, schedule: queueMicrotask })
  const ended: string[] = []
  store.onRunEnd((run, how) => ended.push(`${run}:${how}`))
  await store.start()
  await store.send("hi")
  await tick()
  host.emit({ verb: "run.finish" })
  host.end()
  await tick()
  assert.equal(store.get().run, null)
  assert.deepEqual(ended, ["r1:finish"])
})
