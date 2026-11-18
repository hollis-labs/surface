/** Test support: the contract's fixtures, and the few assertions the ported tests use. */
import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import path from "node:path"
import { fileURLToPath } from "node:url"
import type { Call } from "../src/generated.ts"

const fixtures = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../contract/fixtures")
export const fixture = (name: string): string => readFileSync(path.join(fixtures, `${name}.json`), "utf8")

export function response(call: string | Call, value: unknown): string {
  const parsed = typeof call === "string" ? (JSON.parse(call) as Call) : call
  return JSON.stringify({
    version: "1.0",
    agentFunctionResponse: { functionCallId: parsed.callAgentFunction.functionCallId, value },
  })
}

const plain = (v: unknown) => JSON.parse(JSON.stringify(v ?? null))

export function expect(actual: unknown) {
  return {
    toBe: (want: unknown) => assert.strictEqual(actual, want),
    toEqual: (want: unknown) => assert.deepEqual(plain(actual), plain(want)),
    toBeNull: () => assert.strictEqual(actual, null),
    toBeUndefined: () => assert.strictEqual(actual, undefined),
    toThrow: () => assert.throws(actual as () => unknown),
    not: {
      toBe: (want: unknown) => assert.notStrictEqual(actual, want),
      toThrow: () => assert.doesNotThrow(actual as () => unknown),
      toContain: (want: string) => assert.ok(!String(actual).includes(want)),
    },
  }
}
