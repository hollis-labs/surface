/** The handful of assertions the ported tests use, over node:assert. Test support only. */
import assert from "node:assert/strict"

const partial = (actual: unknown, want: unknown): boolean => {
  if (want === null || typeof want !== "object") return Object.is(actual, want)
  if (actual === null || typeof actual !== "object") return false
  return Object.entries(want as Record<string, unknown>).every(([k, v]) =>
    partial((actual as Record<string, unknown>)[k], v),
  )
}

export function expect(actual: unknown) {
  const is = {
    toBe: (want: unknown) => assert.strictEqual(actual, want),
    toEqual: (want: unknown) =>
      assert.deepEqual(JSON.parse(JSON.stringify(actual ?? null)), JSON.parse(JSON.stringify(want ?? null))),
    toBeNull: () => assert.strictEqual(actual, null),
    toBeUndefined: () => assert.strictEqual(actual, undefined),
    toBeDefined: () => assert.notStrictEqual(actual, undefined),
    toHaveLength: (n: number) => assert.strictEqual((actual as { length: number }).length, n),
    toMatch: (re: RegExp | string) =>
      assert.match(String(actual), typeof re === "string" ? new RegExp(re.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")) : re),
    toMatchObject: (want: unknown) =>
      assert.ok(partial(actual, want), `${JSON.stringify(actual)} does not match ${JSON.stringify(want)}`),
  }
  return {
    ...is,
    not: { toThrow: () => assert.doesNotThrow(actual as () => unknown) },
  }
}
