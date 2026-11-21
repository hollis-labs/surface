/** Async palette protocol: one independently cancellable request per provider. */
export interface PaletteProvider {
  id: string
  title: string
  mode: "commands" | "tabs"
}
export interface PaletteResult {
  id: string
  title: string
  description?: string
  icon?: string
  ref?: { kind: string; canonical: string }
  command?: { command: string; args?: Record<string, unknown> }
  window?: string
  close?: boolean
}
export interface PaletteGroup {
  rows: PaletteResult[]
  error?: string
  loading: boolean
}
export function matchesPalette(query: string, ...text: string[]): boolean {
  const value = text.join(" ").toLowerCase()
  return query
    .toLowerCase()
    .trim()
    .split(/\s+/)
    .every((part) => value.includes(part))
}

/** Cancellation also guards callbacks when a transport ignores AbortSignal. */
export function searchProviders(
  providers: PaletteProvider[],
  load: (provider: PaletteProvider, signal: AbortSignal) => Promise<PaletteResult[]>,
  update: (id: string, state: PaletteGroup) => void,
): () => void {
  const controller = new AbortController()
  for (const provider of providers) {
    update(provider.id, { rows: [], loading: true })
    void load(provider, controller.signal).then(
      (rows) => {
        if (!controller.signal.aborted) update(provider.id, { rows, loading: false })
      },
      (e: unknown) => {
        if (!controller.signal.aborted)
          update(provider.id, { rows: [], loading: false, error: e instanceof Error ? e.message : String(e) })
      },
    )
  }
  return () => controller.abort()
}

export async function fetchPaletteRows(provider: PaletteProvider, query: string, scope: string, signal: AbortSignal) {
  const params = new URLSearchParams({ provider: provider.id, q: query, scope })
  const res = await fetch(`/api/palette/search?${params}`, { signal })
  const body = await res.json()
  if (!res.ok) throw new Error(body.error ?? `Search failed (${res.status})`)
  return body as PaletteResult[]
}
