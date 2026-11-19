import { isWordChar, matches, trimTrailing } from "../scan-utils.ts"
import type { RefHit, RefKind } from "../types.ts"

// Tether addresses (`msg://agent/<scope>/agt_…`, `msg://user/…`) name an agent or a person in
// Tether's registry; a message is named explicitly, since its id is a bare UUID:
// `tach:message/<uuid>` (also as a Markdown link target).
const URN = /msg:\/\/[a-z][a-z0-9-]{0,31}\/[A-Za-z0-9_.@:/-]{1,300}/g
const UUID = "[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}"
const MESSAGE = new RegExp(`tach:/{0,2}(?:message|msg)/(${UUID})`, "gi")

export const TETHER_AGENT_KIND = "tether.agent"
export const TETHER_MESSAGE_KIND = "tether.message"

export function createTetherKinds(): RefKind[] {
  const agent: RefKind = {
    id: TETHER_AGENT_KIND,
    label: "Tether agent",
    icon: "agent",
    priority: 85,
    scan(text) {
      const hits: RefHit[] = []
      for (const m of matches(URN, text)) {
        if (isWordChar(text[m.index - 1])) continue
        const end = trimTrailing(text, m.index, m.index + m[0].length)
        hits.push({ start: m.index, end, canonical: text.slice(m.index, end) })
      }
      return hits
    },
    chipLabel: (ref) => ref.canonical.split("/").pop() || ref.canonical,
    open: () => ({ type: "none" }),
  }
  const message: RefKind = {
    id: TETHER_MESSAGE_KIND,
    label: "Tether message",
    icon: "message",
    priority: 95,
    scan(text) {
      const hits: RefHit[] = []
      for (const m of matches(MESSAGE, text)) {
        if (isWordChar(text[m.index - 1])) continue
        hits.push({ start: m.index, end: m.index + m[0].length, canonical: m[1].toLowerCase() })
      }
      return hits
    },
    chipLabel: (ref) => `message ${ref.canonical.slice(0, 8)}`,
    open: () => ({ type: "none" }),
  }
  return [agent, message]
}
