import { isWordChar, matches, trimTrailing } from "../scan-utils.ts"
import type { RefHit, RefKind } from "../types.ts"

// A Registry record: `registry:<id>`, where <id> is the record's canonical id as the Registry
// gives it, `<kind>:<native id>` (registry:project:surface, registry:tool:mcp/tether/torque_task_get).
// The prefix keeps an ordinary "project:x" in prose from becoming a chip; the canonical form is
// the bare Registry id.
const REGISTRY = /registry:([a-z][a-z0-9_]{0,40}):([A-Za-z0-9][A-Za-z0-9._/:@+-]{0,400})/g

export const REGISTRY_RECORD_KIND = "registry.record"

export function createRegistryKind(): RefKind {
  return {
    id: REGISTRY_RECORD_KIND,
    label: "Registry record",
    icon: "registry",
    priority: 88,
    chipLabel: (ref) => ref.canonical,
    open: () => ({ type: "none" }),
    scan(text) {
      const hits: RefHit[] = []
      for (const m of matches(REGISTRY, text)) {
        if (isWordChar(text[m.index - 1]) || text[m.index - 1] === "/" || text[m.index - 1] === ".") continue
        const end = trimTrailing(text, m.index, m.index + m[0].length)
        const canonical = text.slice(m.index + "registry:".length, end)
        // A kind with nothing after its colon once punctuation is dropped is not a record.
        if (!/^[a-z][a-z0-9_]*:[A-Za-z0-9]/.test(canonical)) continue
        hits.push({ start: m.index, end, canonical })
      }
      return hits
    },
  }
}
