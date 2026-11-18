import assert from "node:assert/strict"
import { test } from "node:test"
import { createCardRegistry } from "./registry.ts"

test("a card resolves to its renderer, or to a miss that says why", () => {
  const r = createCardRegistry<string>()
  r.register("choice", "1", "ChoiceV1")
  r.register("choice", "2", "ChoiceV2")
  assert.deepEqual(r.resolve("choice"), { ok: true, component: "ChoiceV1", version: "1" })
  assert.deepEqual(r.resolve("choice", "2"), { ok: true, component: "ChoiceV2", version: "2" })
  assert.deepEqual(r.resolve("choice", "3"), {
    ok: false,
    miss: { code: "version-skew", kind: "choice", version: "3", known: ["1", "2"] },
  })
  assert.deepEqual(r.resolve("mystery"), { ok: false, miss: { code: "unknown-kind", kind: "mystery", version: "1" } })
  assert.throws(() => r.register("choice", "1", "Again"), /registered twice/)
})
