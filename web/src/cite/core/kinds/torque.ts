import { isWordChar, matches } from "../scan-utils.ts"
import type { RefKind } from "../types.ts"

// Torque ids look like `CW-20261016-0101`: PREFIX-YYYYMMDD-NNNN.
const ID = /(CW|EP|PRJ|SP|PL)-\d{8}-\d{4}/g

interface TorqueEntity {
  id: string
  prefix: string
  label: string
  icon: string
}

export const TORQUE_ENTITIES: TorqueEntity[] = [
  { id: "torque.task", prefix: "CW", label: "Torque task", icon: "task" },
  { id: "torque.epic", prefix: "EP", label: "Torque epic", icon: "epic" },
  { id: "torque.project", prefix: "PRJ", label: "Torque project", icon: "project" },
  { id: "torque.sprint", prefix: "SP", label: "Torque sprint", icon: "sprint" },
  { id: "torque.plan", prefix: "PL", label: "Torque plan", icon: "plan" },
]

/** Is the id standing on its own, not inside a longer token, path or file name? */
function standalone(text: string, start: number, end: number): boolean {
  const before = text[start - 1]
  if (isWordChar(before) || before === "-" || before === "/" || before === "\\" || before === "." || before === "#")
    return false
  const after = text[end]
  if (isWordChar(after) || after === "/" || after === "\\") return false
  // `CW-20261016-0101-foo`, `CW-20261016-0101.md`: a longer token or a file name.
  if ((after === "-" || after === ".") && isWordChar(text[end + 1])) return false
  return true
}

/** One kind per Torque entity so each resolves and caches on its own. */
export function createTorqueKinds(): RefKind[] {
  return TORQUE_ENTITIES.map((e) => ({
    id: e.id,
    label: e.label,
    icon: e.icon,
    priority: 80,
    // Tasks resolve in one batch; a container is one read each, so it waits for hover, focus or a click.
    ...(e.id === "torque.task" ? {} : { resolveOn: "hover" as const }),
    scan(text) {
      const hits = []
      for (const m of matches(ID, text)) {
        if (m[1] !== e.prefix) continue
        const start = m.index
        const end = start + m[0].length
        if (!standalone(text, start, end)) continue
        hits.push({ start, end, canonical: m[0] })
      }
      return hits
    },
    // The record window shows it (the server has the record; Torque has no page here).
    open: () => ({ type: "none" }),
  }))
}
