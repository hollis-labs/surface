import { isWordChar, matches } from "../scan-utils.ts"
import type { RefHit, RefKind } from "../types.ts"

// An Operations workstream: `ops:<id>`, where <id> is the workstream's slug (3 to 64 lower-case
// letters, digits and dashes). The prefix keeps an ordinary word from becoming a chip.
const OPS = /ops:([a-z0-9][a-z0-9-]{2,63})/g

export const OPS_WORKSTREAM_KIND = "ops.workstream"

export function createOpsKind(): RefKind {
  return {
    id: OPS_WORKSTREAM_KIND,
    label: "Workstream",
    icon: "radar",
    priority: 88,
    chipLabel: (ref) => `ops:${ref.canonical}`,
    open: () => ({ type: "none" }),
    scan(text) {
      const hits: RefHit[] = []
      for (const m of matches(OPS, text)) {
        const before = text[m.index - 1]
        if (isWordChar(before) || before === "/" || before === "." || before === "-") continue
        let end = m.index + m[0].length
        // A trailing dash is punctuation, not part of the id.
        while (text[end - 1] === "-") end--
        const id = text.slice(m.index + "ops:".length, end)
        if (id.length < 3 || isWordChar(text[end]) || text[end] === "/") continue
        hits.push({ start: m.index, end, canonical: id })
      }
      return hits
    },
  }
}
