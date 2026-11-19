import { CASTER_KIND } from "./kinds/caster.ts"
import { FRAGMENT_KIND } from "./kinds/fragment.ts"
import { parseTesseractKey, TESSERACT_ITEM_KIND, TESSERACT_KEY_KIND } from "./kinds/tesseract.ts"
import { TETHER_MESSAGE_KIND } from "./kinds/tether.ts"
import { TORQUE_ENTITIES } from "./kinds/torque.ts"
import { WORK_ITEM_KIND } from "./kinds/work.ts"
import type { RefIdentity } from "./types.ts"

// `tach:<service>/<id-or-key>`: how an agent points at something without relying on pattern
// matching. Written as a markdown link so it reads as a normal label anywhere it is not
// rendered (the Tether skill `cite-references` teaches it):
//   [Handoff: approval execution](tach:tesseract/01M4SCJHDM8FV6MDPSDTMZ0X44)
//   [the executor decision](tach:tesseract/project/example/workspace/decisions#approved_action_executor)
//   [CW-20261019-0028](tach:torque/CW-20261019-0028)
// The service name is normalised here, so aliases cost nothing to add.

const ULID = /^[0-7][0-9A-HJKMNP-TV-Z]{25}$/
const TESSERACT = new Set(["tesseract", "tess", "memory"])
const TORQUE = new Set(["torque", "task", "tasks"])
const FRAGMENT = new Set(["fragment", "frag", "fe"])
const MESSAGE = new Set(["message", "msg"])

/** The reference a `tach:` href names, or null when the service, id or key is not one known here. */
export function parseTachHref(href: string | undefined): RefIdentity | null {
  const m = /^tach:\/{0,2}([A-Za-z][A-Za-z0-9_-]{0,31})\/(.+)$/i.exec((href ?? "").trim())
  if (!m) return null
  const service = m[1].toLowerCase()
  let rest: string
  try {
    rest = decodeURIComponent(m[2]).trim()
  } catch {
    return null
  }
  if (!rest) return null
  if (TESSERACT.has(service)) {
    const id = rest.toUpperCase()
    if (ULID.test(id)) return { kind: TESSERACT_ITEM_KIND, canonical: id }
    return parseTesseractKey(rest) ? { kind: TESSERACT_KEY_KIND, canonical: rest } : null
  }
  if (TORQUE.has(service)) {
    const id = rest.toUpperCase()
    const entity = TORQUE_ENTITIES.find((e) => new RegExp(`^${e.prefix}-\\d{8}-\\d{4}$`).test(id))
    return entity ? { kind: entity.id, canonical: id } : null
  }
  if (FRAGMENT.has(service)) {
    return /^[0-9a-f]{64}$/i.test(rest) ? { kind: FRAGMENT_KIND, canonical: rest.toLowerCase() } : null
  }
  if (MESSAGE.has(service)) {
    return /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(rest)
      ? { kind: TETHER_MESSAGE_KIND, canonical: rest.toLowerCase() }
      : null
  }
  if (service === "caster") {
    const ref = rest.startsWith("caster://") ? rest : `caster://${rest}`
    return /^caster:\/\/[A-Za-z0-9_.-]{1,128}\/[A-Za-z0-9_.:-]{1,256}$/.test(ref)
      ? { kind: CASTER_KIND, canonical: ref }
      : null
  }
  if (service === "work") {
    return /^[a-z0-9][a-z0-9-]{0,47}\/[a-z0-9]{8}$/.test(rest) ? { kind: WORK_ITEM_KIND, canonical: rest } : null
  }
  return null
}
