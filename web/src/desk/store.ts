/**
 * The desk as the page knows it: the app's manifest (fetched once) and the window state,
 * which the server owns and streams. The page never changes window state by itself: it
 * posts an action and draws what comes back, so the owner, the agent and an outside client
 * all move the same desk.
 */
import { useSyncExternalStore } from "react"
import type { WindowTree } from "@/components/ui/window-manager"
import type { Desk, OpenOptions, Win } from "../sdk"
import { ApiError, call, getSetting, onEvent } from "../sdk/runtime"

export interface DeskWin {
  id: string
  view: string
  module: string
  title: string
  key?: string
  args: Record<string, unknown>
  state?: Record<string, unknown>
  dirty?: boolean
  /** Who opened it (owner, agent, reflex, outside), why, and whether the owner has made it theirs. */
  opened_by?: string
  reason?: string
  retention?: string
  kept?: boolean
  /** The owner has unsent input in it (the page reports it). */
  composing?: boolean
  /** A delegate's change to what it shows, waiting until the owner is done (rule 7). */
  held?: { args: Record<string, unknown>; title?: string; by: string; at: string }
}

/** A window a delegate opened that the owner has not made theirs (docs/desk-rules.md, rule 4). */
export const notYetKept = (w: DeskWin) => !!w.opened_by && w.opened_by !== "owner" && !w.kept

export interface DeskState {
  wins: DeskWin[]
  focus: string
  /** How the windows are arranged: the server's, drawn by the window manager. */
  tree: WindowTree
  layout: "tile" | "float"
  ratio: number
  active?: string
  mode?: string
  rev: number
}

export interface ViewDef {
  id: string
  title: string
  icon?: string
  width?: number
  height?: number
  menu?: boolean
  multi?: boolean
  tiling?: "full" | "tile" | string
  bare?: boolean
  render: { kind: "component" | "frame"; entry: string }
  opens?: string[]
  module: string
}

export interface CommandDef {
  id: string
  title: string
  section?: string
  keywords?: string
  keys?: string
  hidden?: boolean
  module: string
  /** True when the server runs it; false for a command of the page's own. */
  server: boolean
}

export interface AppManifest {
  name: string
  title: string
  modules: { id: string; title: string; about?: string; version?: string }[]
  views: ViewDef[]
  commands: CommandDef[]
}

export interface LayoutDef {
  name: string
  about?: string
  mode: string
  ratio?: number
  windows: { view: string; key?: string; title?: string; floating?: boolean; focus?: boolean }[]
  setup?: Record<string, unknown>
}

let manifest: AppManifest | null = null
let state: DeskState = { wins: [], focus: "", tree: { root: { type: "row" } }, layout: "tile", ratio: 0.56, rev: -1 }
const subs = new Set<() => void>()

const emit = () => {
  for (const fn of subs) fn()
}

onEvent("desk.changed", (ev) => {
  const next = ev.data as DeskState
  // The stream is ordered, but a reconnect replays the present state: never go backwards.
  if (next && next.rev >= state.rev) {
    state = next
    emit()
  }
})

export async function loadManifest(): Promise<AppManifest> {
  manifest = await call<AppManifest>("GET", "/api/app")
  return manifest
}

export const app = (): AppManifest => {
  if (!manifest) throw new Error("the app manifest is not loaded yet")
  return manifest
}
export const viewById = (id: string) => manifest?.views.find((v) => v.id === id)

/** The view that opens a file of this name: the first whose Opens patterns match it. */
export function viewForFile(name: string): ViewDef | undefined {
  const base = name.toLowerCase().split("/").pop() ?? ""
  const matches = (pattern: string) => {
    const p = pattern.toLowerCase()
    if (p.startsWith("*")) return base.endsWith(p.slice(1))
    return base === p
  }
  return manifest?.views.find((v) => v.opens?.some(matches))
}

export const deskState = () => state
const subscribe = (fn: () => void) => {
  subs.add(fn)
  return () => {
    subs.delete(fn)
  }
}
export function useDeskState(): DeskState {
  return useSyncExternalStore(subscribe, deskState)
}

/** The window in front (view, module and args), or null: for page-level code such as a module's overlay. */
export function useFocusedWindow(): Pick<DeskWin, "id" | "view" | "module" | "args"> | null {
  const d = useDeskState()
  const w = d.wins.find((x) => x.id === d.focus)
  return w ? { id: w.id, view: w.view, module: w.module, args: w.args } : null
}

/** Post one action to the server's window manager. */
export async function act(action: Record<string, unknown>): Promise<DeskWin | undefined> {
  const res = await call<{ window?: DeskWin }>("POST", "/api/desk/actions", action)
  return res.window
}

export const toWin = (w: DeskWin, focus: string): Win => ({
  id: w.id,
  view: w.view,
  module: w.module,
  title: w.title,
  key: w.key,
  args: w.args ?? {},
  state: w.state,
  focused: w.id === focus,
})

// Reports are small and frequent (a selection changing): send the last one, at most every 250ms.
const pendingReports = new Map<string, Record<string, unknown>>()
let reportTimer: number | undefined
function flushReports() {
  reportTimer = undefined
  for (const [id, st] of pendingReports) {
    void act({ type: "state", id, state: st }).catch(() => {})
  }
  pendingReports.clear()
}

type Confirm = (q: { title: string; message?: string; confirmLabel?: string; danger?: boolean }) => Promise<boolean>
let confirmImpl: Confirm = async (q) => window.confirm(q.title)
/** The desktop installs its dialog here. */
export const setConfirm = (fn: Confirm) => {
  confirmImpl = fn
}

/** Runs a command: the server's by id, or one of the page's own. */
type ClientRunner = (id: string, args?: Record<string, unknown>) => Promise<unknown> | undefined
let clientRunner: ClientRunner = () => undefined
export const setClientRunner = (fn: ClientRunner) => {
  clientRunner = fn
}

export async function runCommand<T = unknown>(id: string, args?: Record<string, unknown>): Promise<T> {
  const local = clientRunner(id, args)
  if (local !== undefined) return (await local) as T
  const res = await call<{ result: T }>("POST", `/api/commands/${encodeURIComponent(id)}`, { args: args ?? {} })
  return res.result
}

/** Close a window, asking first when it holds unsaved work. */
export async function closeWindow(id: string): Promise<void> {
  const w = state.wins.find((x) => x.id === id)
  // Asking first is a live setting: turn it off and the next close goes straight through.
  if (w?.dirty && getSetting("surface.confirm_close_dirty") === false) {
    await act({ type: "close", id, force: true })
    return
  }
  if (w?.dirty) {
    const yes = await confirmImpl({
      title: `Close “${w.title}”?`,
      message: "It holds unsaved work.",
      confirmLabel: "Close anyway",
      danger: true,
    })
    if (!yes) return
    await act({ type: "close", id, force: true })
    return
  }
  await act({ type: "close", id })
}

/**
 * A view's last word about its own window can arrive after the window is gone (the desk was
 * rearranged while a request was on its way). That is not an error worth raising.
 */
function unlessGone(err: unknown): undefined {
  if (err instanceof ApiError && /no window/.test(err.message)) return undefined
  throw err
}

/** The Desk a view is handed. */
export const desk: Desk = {
  async open(view: string, opts?: OpenOptions) {
    const w = await act({ type: "open", view, ...opts })
    if (!w) throw new Error(`could not open ${view}`)
    return toWin(w, w.id)
  },
  async openFile(name: string, opts?: OpenOptions) {
    const view = viewForFile(name)
    return view ? desk.open(view.id, opts) : null
  },
  close: closeWindow,
  async focus(id) {
    await act({ type: "focus", id }).catch(unlessGone)
  },
  async setTitle(id, title) {
    await act({ type: "title", id, title }).catch(unlessGone)
  },
  async setArgs(id, args) {
    await act({ type: "args", id, args }).catch(unlessGone)
  },
  report(id, st) {
    pendingReports.set(id, st)
    if (reportTimer === undefined) reportTimer = window.setTimeout(flushReports, 250)
  },
  setDirty(id, dirty) {
    const w = state.wins.find((x) => x.id === id)
    if (w && !!w.dirty !== dirty) void act({ type: "dirty", id, dirty }).catch(() => {})
  },
  run: runCommand,
  confirm: (q) => confirmImpl(q),
  windows: () => state.wins.map((w) => toWin(w, state.focus)),
}

export const listLayouts = () => call<LayoutDef[]>("GET", "/api/layouts")
export const saveLayout = (name: string, about?: string) => call<LayoutDef>("POST", "/api/layouts", { name, about })
export const loadLayout = (name: string) => call("POST", `/api/layouts/${encodeURIComponent(name)}/load`)
export const deleteLayout = (name: string) => call("DELETE", `/api/layouts/${encodeURIComponent(name)}`)
