/**
 * The page's side of the module contract (Go: github.com/hollis-labs/surface/module).
 *
 * A module's web entry (modules/<id>/web/index.tsx) default-exports a ModuleWeb: the
 * components for the views its manifest declares with module.Component("<entry>").
 * A module imports only "@hollis-labs/surface-page/sdk"; the desk, the palette and the agent panel are the
 * core's and are reached through what a view is handed.
 */
import type { ComponentType } from "react"

/** One open window, as the server holds it (module.Window). */
export interface Win {
  id: string
  view: string
  module: string
  title: string
  key?: string
  args: Record<string, unknown>
  state?: Record<string, unknown>
  focused: boolean
}

export interface OpenOptions {
  args?: Record<string, unknown>
  key?: string
  title?: string
  background?: boolean
}

/** An event from the app's bus (module.Event). */
export interface SurfaceEvent<T = unknown> {
  topic: string
  data: T
}

/** What a view may ask of the desk. Every call goes to the server, which owns the state. */
export interface Desk {
  open(view: string, opts?: OpenOptions): Promise<Win>
  close(winId: string): Promise<void>
  focus(winId: string): Promise<void>
  setTitle(winId: string, title: string): Promise<void>
  /** Replace what the window was opened with (the note it shows, the page it is on). */
  setArgs(winId: string, args: Record<string, unknown>): Promise<void>
  /**
   * Report what the window is showing right now (selection, position). Small. The agent
   * and the context slots read it; call it when it changes, not on every keystroke.
   */
  report(winId: string, state: Record<string, unknown>): void
  /** Say whether the window holds unsaved work; closing it then asks first. */
  setDirty(winId: string, dirty: boolean): void
  /**
   * Open a file in the view that declares it opens such files (View.Opens, "*.md"). The
   * first view that matches gets args as given. Resolves to null when no view in this app
   * opens it, so a module can offer "open" without knowing which modules are present.
   */
  openFile(name: string, opts?: OpenOptions): Promise<Win | null>
  /** Run any command by id. */
  run<T = unknown>(command: string, args?: Record<string, unknown>): Promise<T>
  /** Ask a yes/no question in a dialog on the desk. */
  confirm(q: { title: string; message?: string; confirmLabel?: string; danger?: boolean }): Promise<boolean>
  windows(): Win[]
}

/** Calls to the module's own routes: path is relative to /api/m/<module>/. */
export interface ModuleAPI {
  get<T = unknown>(path: string): Promise<T>
  post<T = unknown>(path: string, body?: unknown): Promise<T>
  put<T = unknown>(path: string, body?: unknown): Promise<T>
  del<T = unknown>(path: string): Promise<T>
  /** The absolute URL of a route, for an <img>, a frame or a download. */
  url(path: string): string
}

/** What a view's component receives. */
export interface ViewProps {
  win: Win
  desk: Desk
  api: ModuleAPI
}

/** A card as a module's card renderer receives it (module.Card). */
export interface ModuleCard {
  id: string
  kind: string
  version?: string
  title?: string
  source?: string
  subject?: string
  risk?: string
  blocking?: boolean
  payload?: Record<string, unknown>
  status?: string
  expires_at?: string
}

/** An answer to a card (module.CardOutcome). */
export interface ModuleCardOutcome {
  status: "submitted" | "canceled"
  data?: Record<string, unknown>
  answers?: { questionId: string; value: unknown; note?: string }[]
  decisions?: { itemId: string; action: string; note?: string }[]
}

/** What a module's card renderer receives: the card, how to answer it, and the module's routes. */
export interface CardProps {
  card: ModuleCard
  respond: (outcome: ModuleCardOutcome) => Promise<void>
  api: ModuleAPI
}

/** A module's web entry. */
export interface ModuleWeb {
  /** By Render.Entry. */
  views: Record<string, ComponentType<ViewProps>>
  /** Renderers for card kinds of the module's own, by kind (version 1), or "kind@version". */
  cards?: Record<string, ComponentType<CardProps>>
  /**
   * Drawn once over the whole page, whatever window is in front: for something that must
   * reach the owner anywhere (a queue that opens itself as a modal). Keep it small; it is
   * mounted for the page's life.
   */
  overlay?: ComponentType<{ api: ModuleAPI }>
}

/** A mark (module.Mark). */
export interface Mark {
  id: string
  ref: string
  anchor?: unknown
  quote?: string
  body: string
  author: "owner" | "agent"
  status: "open" | "resolved"
  created: string
  replies?: { body: string; author: string; created: string }[]
}
