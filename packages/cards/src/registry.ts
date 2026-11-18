/**
 * The table of renderers: which component draws a card of a kind at a version. It is an
 * instance an app creates, not module state, and it is generic over the component type so
 * nothing here knows React. An unknown kind or version resolves to a miss the app draws
 * plainly; a card is never drawn blank.
 */
export type CardMiss =
  | { readonly code: "unknown-kind"; readonly kind: string; readonly version: string }
  | {
      readonly code: "version-skew"
      readonly kind: string
      readonly version: string
      readonly known: readonly string[]
    }

export type CardResolution<C> =
  | { readonly ok: true; readonly component: C; readonly version: string }
  | { readonly ok: false; readonly miss: CardMiss }

export interface CardRegistry<C> {
  /** Registers a renderer. The same kind and version twice throws: order never decides. */
  register(kind: string, version: string, component: C): void
  resolve(kind: string, version?: string): CardResolution<C>
  kinds(): readonly string[]
}

export function createCardRegistry<C>(): CardRegistry<C> {
  const table = new Map<string, Map<string, C>>()
  return {
    register(kind, version, component) {
      const versions = table.get(kind) ?? new Map<string, C>()
      if (versions.has(version)) throw new Error(`cards: ${kind}@${version} is registered twice`)
      versions.set(version, component)
      table.set(kind, versions)
    },
    resolve(kind, version = "1") {
      const versions = table.get(kind)
      if (!versions) return { ok: false, miss: { code: "unknown-kind", kind, version } }
      const component = versions.get(version)
      if (component === undefined) {
        return { ok: false, miss: { code: "version-skew", kind, version, known: [...versions.keys()] } }
      }
      return { ok: true, component, version }
    },
    kinds: () => [...table.keys()],
  }
}
