import { isWordChar, matches, trimTrailing } from "../scan-utils.ts"
import type { RefHit, RefKind } from "../types.ts"

// A Caster document: `caster://<namespace>/<name>`.
const CASTER = /caster:\/\/[A-Za-z0-9_.-]{1,128}\/[A-Za-z0-9_.:-]{1,256}/g

export const CASTER_KIND = "caster.doc"

export function createCasterKind(): RefKind {
  return {
    id: CASTER_KIND,
    label: "Caster document",
    icon: "document",
    priority: 88,
    chipLabel: (ref) => ref.canonical.slice("caster://".length),
    open: () => ({ type: "none" }),
    scan(text) {
      const hits: RefHit[] = []
      for (const m of matches(CASTER, text)) {
        if (isWordChar(text[m.index - 1])) continue
        const end = trimTrailing(text, m.index, m.index + m[0].length)
        hits.push({ start: m.index, end, canonical: text.slice(m.index, end) })
      }
      return hits
    },
  }
}
