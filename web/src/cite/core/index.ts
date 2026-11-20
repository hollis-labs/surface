// Reference detection and resolution, framework-free and with no dependencies: the kind
// registry, the scanner that splits text into plain and reference segments, the batching
// resolver cache, the `tach:` link parser, and the kinds. Lifted from Tachyon's console
// (plugins/console/ui/src/refs, MIT, same owner) by way of Basalisk (frontend/src/cite/core),
// with the kinds Surface's modules resolve. Keep the syntax in step with the Tether skill
// `cite-references`, which teaches it to agents.
import { createCasterKind } from "./kinds/caster.ts"
import { createFragmentKind } from "./kinds/fragment.ts"
import { createPathKinds } from "./kinds/path.ts"
import { createRegistryKind } from "./kinds/registry.ts"
import { createTesseractKinds } from "./kinds/tesseract.ts"
import { createTetherKinds } from "./kinds/tether.ts"
import { createTorqueKinds } from "./kinds/torque.ts"
import { createUrlKind } from "./kinds/url.ts"
import { createWorkKind } from "./kinds/work.ts"
import { createRefRegistry, type RefRegistry } from "./registry.ts"

export { codeRanges, detect, detectRefs, refKey, uniqueRefs } from "./detect.ts"
export { CASTER_KIND } from "./kinds/caster.ts"
export { FRAGMENT_KIND } from "./kinds/fragment.ts"
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
    ...createPathKinds(),
  ]) {
    r.register(k)
  }
  return r
}
