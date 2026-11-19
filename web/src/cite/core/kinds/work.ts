import { isWordChar, matches } from "../scan-utils.ts"
import type { RefHit, RefKind } from "../types.ts"

// A Planner, Content or Writer item: `work:<workspace>/<id>`, the ref a Work window reports.
const WORK = /work:([a-z0-9][a-z0-9-]{0,47})\/([a-z0-9]{8})/g

export const WORK_ITEM_KIND = "work.item"

export function createWorkKind(): RefKind {
  return {
    id: WORK_ITEM_KIND,
    label: "Work item",
    icon: "target",
    priority: 88,
    chipLabel: (ref) => ref.canonical,
    open: () => ({ type: "none" }),
    scan(text) {
      const hits: RefHit[] = []
      for (const m of matches(WORK, text)) {
        const end = m.index + m[0].length
        if (isWordChar(text[m.index - 1]) || isWordChar(text[end]) || text[end] === "/" || text[end] === "-") continue
        hits.push({ start: m.index, end, canonical: `${m[1]}/${m[2]}` })
      }
      return hits
    },
  }
}
