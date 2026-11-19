/**
 * Cite's one instance for the whole page: the kind registry, the resolver cache, and what
 * "open" does. Detection is ./core; resolution is the app's server (GET /api/cite/...), which
 * asks the module that owns each kind (module.RefKind).
 */
import { useSyncExternalStore } from "react"
import { app, desk } from "../desk/store"
import { call } from "../sdk/runtime"
import {
  createDefaultRefRegistry,
  createRefResolverClient,
  detect,
  type KindResolver,
  type RefEntity,
  type RefIdentity,
  type RefResolverClient,
  type RefState,
  type ResolveOutcome,
  type Segment,
} from "./core/index.ts"

export type { RefEntity, RefIdentity, RefState, Segment }

/** A record as /api/cite/record returns it: the preview and the whole text. */
export interface CiteRecord extends RefEntity {
  body?: string
}

/** A kind the app's server resolves (GET /api/cite). */
export interface ServerKind {
  kind: string
  label: string
  module: string
  record?: boolean
}

export const registry = createDefaultRefRegistry()

function serverResolver(kind: string): KindResolver {
  return {
    async resolveMany(canonicals) {
      const ids = canonicals.map(encodeURIComponent).join(",")
      const out = await call<{ results: Record<string, RefEntity | null> }>(
        "GET",
        `/api/cite/resolve?kind=${encodeURIComponent(kind)}&ids=${ids}`,
      )
      const mapped: Record<string, ResolveOutcome | undefined> = {}
      for (const id of canonicals) {
        const e = out.results[id]
        mapped[id] = e ? { status: "found", entity: e } : { status: "not-found" }
      }
      return mapped
    },
  }
}

// Until the server says which kinds it resolves, none is: chips show what was written.
let client: RefResolverClient = createRefResolverClient({ resolvers: {} })
let kinds = new Map<string, ServerKind>()
let version = 0
const listeners = new Set<() => void>()
const emit = () => {
  version++
  for (const fn of listeners) fn()
}
let unsubscribe = client.subscribe(emit)
let started: Promise<void> | null = null

/** Ask the server what it resolves. Called once when the page is ready; safe to call again. */
export function initCite(): Promise<void> {
  started ??= call<{ kinds: ServerKind[] }>("GET", "/api/cite")
    .then((state) => {
      kinds = new Map(state.kinds.map((k) => [k.kind, k]))
      unsubscribe()
      client.dispose()
      client = createRefResolverClient({
        resolvers: Object.fromEntries(state.kinds.map((k) => [k.kind, serverResolver(k.kind)])),
      })
      unsubscribe = client.subscribe(emit)
      emit()
    })
    .catch(() => {
      started = null
    })
  return started ?? Promise.resolve()
}

/** Whether a kind has a record behind it that this app can look up. */
export const canResolve = (kind: string) => kinds.has(kind)
export const kindLabel = (kind: string) => kinds.get(kind)?.label ?? registry.get(kind)?.label ?? kind

const subscribe = (fn: () => void) => {
  listeners.add(fn)
  return () => {
    listeners.delete(fn)
  }
}

/** The state of one reference; with fetch, asks for it to be looked up. */
export function useRefState(kind: string, canonical: string, fetch: boolean): RefState {
  useSyncExternalStore(subscribe, () => version)
  const state = client.get(kind, canonical)
  if (fetch && state.status === "idle") queueMicrotask(() => client.request([{ kind, canonical }]))
  return state
}

/** Ask for a reference now (the card opened before the chip asked). */
export const requestRef = (ref: RefIdentity) => client.request([{ kind: ref.kind, canonical: ref.canonical }])

/** Text split into plain and reference segments. */
export const segments = (text: string): Segment[] => detect(text, registry)

export const loadRecord = (ref: RefIdentity) =>
  call<CiteRecord>(
    "GET",
    `/api/cite/record?kind=${encodeURIComponent(ref.kind)}&id=${encodeURIComponent(ref.canonical)}`,
  )

const hasCommand = (id: string) => app().commands.some((c) => c.id === id)

/** Open a URL: in the app's Browser when it has one, else in a new tab. */
export function openURL(url: string) {
  if (hasCommand("browser.open"))
    void desk.run("browser.open", { url }).catch(() => window.open(url, "_blank", "noopener"))
  else window.open(url, "_blank", "noopener")
}

/** The record window for a reference. */
export const openRecord = (ref: RefIdentity) =>
  desk.open("surface.cite", {
    key: `${ref.kind}:${ref.canonical}`,
    args: { kind: ref.kind, id: ref.canonical },
  })

/**
 * Open a reference: the window its module names (a Planner item in Work, a path in Files), a
 * URL in the Browser, else the record window; a reference nothing here resolves is copied.
 */
export function openCite(ref: RefIdentity) {
  const state = client.get(ref.kind, ref.canonical)
  const entity = state.status === "found" ? state.entity : undefined
  if (entity?.open && hasCommand(entity.open.command)) {
    void desk.run(entity.open.command, entity.open.args ?? {}).catch(() => openRecord(ref))
    return
  }
  if (ref.kind === "url") {
    openURL(ref.canonical)
    return
  }
  if (canResolve(ref.kind)) {
    void openRecord(ref)
    return
  }
  void navigator.clipboard?.writeText(ref.canonical).catch(() => undefined)
}
