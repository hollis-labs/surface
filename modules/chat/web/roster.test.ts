import assert from "node:assert/strict"
import { test } from "node:test"
import { movePin, orderRoster, parsePins } from "./roster.ts"

const seat = (id: string, name: string, at?: number, state = "idle") => ({
  seat: id,
  name,
  sessions: at === undefined ? [] : [{ state, last_event_at: new Date(at).toISOString() }],
})

test("pinned first in order; then recently active, newest first; then the rest by name", () => {
  const now = Date.parse("2026-11-28T12:00:00Z")
  const min = 60_000
  const seats = [
    seat("c", "Charlie", now - 30 * min),
    seat("a", "Alpha"),
    seat("d", "Delta", now - 2 * min),
    seat("b", "Bravo", now - 5 * min),
    seat("e", "Echo", now - 60 * min, "running"),
    seat("z", "Zulu", now - 1 * min),
  ]
  const o = orderRoster(seats, ["z", "a", "gone"], 15 * min, now)
  assert.deepEqual(
    o.pinned.map((s) => s.seat),
    ["z", "a"],
  )
  assert.deepEqual(
    o.rest.map((s) => s.seat),
    ["e", "d", "b", "c"],
  )
  assert.deepEqual([...o.recent].sort(), ["b", "d", "e"])
})

test("pins parse and move", () => {
  assert.deepEqual(parsePins(" a, b ,,c"), ["a", "b", "c"])
  assert.deepEqual(movePin(["a", "b", "c"], "c", "a"), ["c", "a", "b"])
  assert.deepEqual(movePin(["a", "b", "c"], "a", null), ["b", "c", "a"])
  assert.deepEqual(movePin(["a", "b"], "n", "b"), ["a", "n", "b"])
})
