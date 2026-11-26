// Reference detection and resolution, framework-free and with no dependencies: the kind
// registry, the scanner that splits text into plain and reference segments, the batching
// resolver cache, the `tach:` link parser, and the kinds. Lifted from Tachyon's console
// (plugins/console/ui/src/refs, MIT, same owner) by way of Basalisk (frontend/src/cite/core),
// with the kinds Surface's modules resolve. Keep the syntax in step with the Tether skill
// `cite-references`, which teaches it to agents.
import { createCasterKind } from "./kinds/caster.ts"
import { createFragmentKind } from "./kinds/fragment.ts"
import { createOpsKind } from "./kinds/ops.ts"
import { createPathKinds } from "./kinds/path.ts"
import { createRegistryKind } from "./kinds/registry.ts"
import { createTesseractKinds } from "./kinds/tesseract.ts"
import { createTetherKinds } from "./kinds/tether.ts"
import { createTorqueKinds } from "./kinds/torque.ts"
import { createUrlKind } from "./kinds/url.ts"
import { createWorkKind } from "./kinds/work.ts"
import { createRefRegistry, type RefRegistry } from "./registry.ts"
import { isWordChar, matches } from "./scan-utils.ts"
import type { RefHit, RefKind } from "./types.ts"

function createPrefixKind(id: string, label: string, icon: string, prefix: string, re: RegExp): RefKind {
  return {
    id,
    label,
    icon,
    priority: 88,
    chipLabel: (ref) => `${prefix}:${ref.canonical}`,
    open: () => ({ type: "none" }),
    scan(text) {
      const hits: RefHit[] = []
      for (const m of matches(re, text)) {
        const before = text[m.index - 1]
        if (isWordChar(before) || before === "/" || before === "." || before === "-") continue
        const end = m.index + m[0].length
        const canonical = text.slice(m.index + prefix.length + 1, end)
        if (!canonical || isWordChar(text[end]) || text[end] === "/") continue
        hits.push({ start: m.index, end, canonical })
      }
      return hits
    },
  }
}

export { codeRanges, detect, detectRefs, refKey, uniqueRefs } from "./detect.ts"
export { CASTER_KIND } from "./kinds/caster.ts"
export { FRAGMENT_KIND } from "./kinds/fragment.ts"
export { OPS_WORKSTREAM_KIND } from "./kinds/ops.ts"
export { PATH_KIND, RELATIVE_PATH_KIND } from "./kinds/path.ts"
export { REGISTRY_RECORD_KIND } from "./kinds/registry.ts"
export { TESSERACT_ITEM_KIND, TESSERACT_KEY_KIND } from "./kinds/tesseract.ts"
export { TETHER_AGENT_KIND, TETHER_MESSAGE_KIND } from "./kinds/tether.ts"
export { WORK_ITEM_KIND } from "./kinds/work.ts"
export { buildPreviewCard } from "./preview.ts"
export { createRefRegistry, type RefRegistry } from "./registry.ts"
export { createRefResolverClient, type RefResolverClient, type ResolverClientOptions } from "./resolver.ts"
export { parseTachHref } from "./tach.ts"
export type * from "./types.ts"

/** A registry with every kind Surface knows. */
export function createDefaultRefRegistry(): RefRegistry {
  const r = createRefRegistry()
  for (const k of [
    createUrlKind(),
    createFragmentKind(),
    ...createTesseractKinds(),
    ...createTorqueKinds(),
    ...createTetherKinds(),
    createCasterKind(),
    createWorkKind(),
    createRegistryKind(),
    createOpsKind(),
    createPrefixKind(
      "align.proposal",
      "Alignment proposal",
      "compass",
      "align",
      /align:([A-Za-z0-9][A-Za-z0-9_-]{1,63})/g,
    ),
    createPrefixKind("align.droppad", "Drop Pad", "folder", "droppad", /droppad:([A-Za-z0-9][A-Za-z0-9_-]{1,63})/g),
    createPrefixKind("tracker.list", "Tracker", "list-todo", "tracker", /tracker:([A-Za-z0-9][A-Za-z0-9_-]{1,63})/g),
    createPrefixKind(
      "tracker.item",
      "Tracker item",
      "list-todo",
      "tracker.item",
      /tracker\.item:([A-Za-z0-9][A-Za-z0-9_-]{1,63})/g,
    ),
    createPrefixKind("usage.account", "Usage account", "gauge", "usage", /usage:([A-Za-z0-9][A-Za-z0-9._-]{1,63})/g),
    ...createPathKinds(),
  ]) {
    r.register(k)
  }
  return r
}
