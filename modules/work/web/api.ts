/**
 * The Work engine's shapes and its client (modules/work/README.md). The JSON is Basalisk's
 * (frontend/src/api/work.ts); the routes are the module's own, under /api/m/work/.
 */
import type { ModuleAPI } from "@hollis-labs/surface-page/sdk"

export interface WorkField {
  key: string
  label?: string
  type: "text" | "number" | "date" | "enum" | "ref" | "refs" | "url" | "bool"
  options?: string[]
}

export interface WorkKind {
  kind: string
  label: string
  plural?: string
  icon?: string
  stages: string[]
  fields: WorkField[]
  sections: string[]
  children: string[]
  color?: string
  rev: number
  problem?: string
}

export interface WorkView {
  id: string
  view: "board" | "calendar" | "outline" | "canvas" | "flow"
  title: string
  kinds: string[]
  filter?: Record<string, unknown>
  group_by?: string
  date_field?: string
  root?: string
  rel?: string[]
  layout?: Record<string, unknown>
  rev: number
  problem?: string
}

/** A canvas's arrangement (internal/work/layout.go). */
export interface WorkPos {
  x: number
  y: number
}
export interface WorkStack {
  id: string
  title: string
  x: number
  y: number
  items: string[]
  bind?: { key: "stage" | "parent"; value: string }
}
export interface WorkCanvasNote {
  id: string
  x: number
  y: number
  text: string
}
export interface WorkLayoutPatch {
  cards?: Record<string, WorkPos | null>
  stacks?: Record<string, WorkStack | null>
  notes?: Record<string, WorkCanvasNote | null>
}

export interface WorkLink {
  rel: string
  to: string
}

export interface WorkMaterial {
  ref: string
  caption?: string
}

export interface WorkPublished {
  at: string
  where: string
  url?: string
  note?: string
}

export interface WorkCard {
  id: string
  kind: string
  title: string
  stage: string
  fields: Record<string, unknown>
  parent?: string
  children: string[]
  links: WorkLink[]
  material: WorkMaterial[]
  published: WorkPublished[]
  task?: string
  tags: string[]
  cut?: boolean
  /** Signed off; stale_sign: and its text changed since. */
  signed?: boolean
  stale_sign?: boolean
  excerpt?: string
  words: number
  rev: number
  updated: string
  problem?: string
}

export interface WorkNote {
  id: string
  item: string
  item_rev?: number
  kind: string
  section?: string
  quote?: string
  replace?: string
  text?: string
  created_at: string
}

export interface WorkDetail extends WorkCard {
  ref: string
  sections: Record<string, string>
  order: string[]
  lead?: string
  notes: WorkNote[]
  /** Sent rounds, newest first. */
  rounds: WorkRound[]
  sign_off?: WorkSignOff
  changed_since_round: boolean
  /** The workspace's owners: who rounds and sign-offs are mailed to. */
  owners: string[]
}

export interface WorkRoundNote {
  id: string
  kind: string
  section?: string
  quote?: string
  replace?: string
  text?: string
  state: string
  reply?: string
}

export interface WorkRound {
  round: number
  ref: string
  sent_at: string
  item_rev: number
  notes: WorkRoundNote[]
  open: number
  problem?: string
}

export interface WorkSignOff {
  at: string
  rev: number
  digest: string
  pinned?: string
  round?: number
}

export interface Workspace {
  archived: boolean
  deleted: boolean
  slug: string
  title: string
  app: string
  preset: string
  ns: string
  owners: string[]
  created?: string
  body: string
  items: number
  rev: number
  updated: string
  problem?: string
}

export interface WorkPreset {
  id: string
  app: string
  label: string
  kinds: WorkKind[]
  views: WorkView[]
  /** Starting trees (Planner). */
  templates?: { id: string; label: string; about: string }[]
}

export interface WorkSpace {
  /** The view the workspace opens on (its preset's first). */
  default_view: string
  workspace: Workspace
  kinds: WorkKind[]
  items: WorkCard[]
  views: WorkView[]
}

export interface WorkRevision {
  rev: number
  at: string
  title: string
}

export interface WorkEdit {
  base_rev: number
  title?: string
  fields?: Record<string, unknown>
  sections?: Record<string, string>
  tags?: string[]
  stage?: string
  material?: WorkMaterial[]
  published?: WorkPublished[]
  links?: WorkLink[]
}

const enc = encodeURIComponent

/** The Work module's routes (modules/work/README.md), over the view's own api. */
export function workClient(api: ModuleAPI) {
  return {
    updateSpace: (ws: string, change: { title?: string; archived?: boolean }) => api.put(`work/${enc(ws)}`, change),
    list: (app: string, refresh = false) =>
      api.get<{ workspaces: Workspace[]; presets: WorkPreset[]; problem?: string }>(
        `work?app=${enc(app)}${refresh ? "&refresh=1" : ""}`,
      ),
    create: (workspace: string, title: string, preset: string, about: string, template = "") =>
      api.post<{ workspace: string; problem?: string }>("work", { workspace, title, preset, about, template }),
    /** Make the item's Torque task (once: a second send returns the same task). */
    sendToTorque: (ws: string, id: string) =>
      api.post<{ task: string; created: boolean }>(`work/${enc(ws)}/items/${enc(id)}/torque`),
    torqueStatus: (ws: string, id: string) =>
      api.get<{ task: { id: string; title?: string; status?: string } | null; problem?: string }>(
        `work/${enc(ws)}/items/${enc(id)}/torque`,
      ),
    open: (ws: string, refresh = false) =>
      api.get<{ space: WorkSpace; relations: string[]; problem?: string }>(
        `work/${enc(ws)}${refresh ? "?refresh=1" : ""}`,
      ),
    item: (ws: string, id: string) => api.get<WorkDetail>(`work/${enc(ws)}/items/${enc(id)}`),
    make: (
      ws: string,
      item: {
        kind: string
        title?: string
        stage?: string
        parent?: string
        fields?: Record<string, unknown>
        tags?: string[]
        links?: WorkLink[]
        sections?: Record<string, string>
      },
    ) => api.post<{ id: string }>(`work/${enc(ws)}/items`, item),
    edit: (ws: string, id: string, edit: WorkEdit) =>
      api.put<{ rev: number }>(`work/${enc(ws)}/items/${enc(id)}`, edit),
    move: (ws: string, id: string, key: string, value: string) =>
      api.put<{ rev: number }>(`work/${enc(ws)}/items/${enc(id)}/move`, { key, value }),
    /** Put an item in the outline: under parent ("" for the top), before a sibling ("" for the end). */
    place: (ws: string, id: string, parent: string, before: string, view: string) =>
      api.put(`work/${enc(ws)}/items/${enc(id)}/place`, { parent, before, view }),
    cut: (ws: string, id: string) => api.post(`work/${enc(ws)}/items/${enc(id)}/cut`),
    restore: (ws: string, id: string) => api.post(`work/${enc(ws)}/items/${enc(id)}/restore`),
    dropNote: (ws: string, id: string) => api.del(`work/${enc(ws)}/notes/${enc(id)}`),
    /** A note in the item's draft round (the owner sends it). */
    addNote: (
      ws: string,
      id: string,
      note: { kind: string; section?: string; quote?: string; replace?: string; text?: string },
    ) => api.post<WorkNote>(`work/${enc(ws)}/items/${enc(id)}/notes`, note),
    /** Send the draft round: only ever from the owner's click. */
    sendRound: (ws: string, id: string) =>
      api.post<{
        round: number
        ref: string
        notes: number
        mailed: string[]
        not_mailed: string[]
        commented?: string
      }>(`work/${enc(ws)}/items/${enc(id)}/round`),
    /** Sign the item off: only ever from the owner's click. */
    signOff: (ws: string, id: string, anyway = false) =>
      api.post<WorkSignOff & { stage?: string; mailed: string[]; not_mailed: string[]; problem?: string }>(
        `work/${enc(ws)}/items/${enc(id)}/signoff`,
        { anyway },
      ),
    unsign: (ws: string, id: string) => api.del(`work/${enc(ws)}/items/${enc(id)}/signoff`),
    setOwners: (ws: string, owners: string[]) => api.put(`work/${enc(ws)}/owners`, { owners }),
    saveView: (ws: string, id: string, set: Record<string, unknown>) =>
      api.put<{ id: string; rev: number }>(`work/${enc(ws)}/views/${enc(id)}`, set),
    newView: (ws: string, set: Record<string, unknown>) =>
      api.post<{ id: string; rev: number }>(`work/${enc(ws)}/views`, set),
    layout: (ws: string, view: string, patch: WorkLayoutPatch) =>
      api.put<{ rev: number }>(`work/${enc(ws)}/views/${enc(view)}/layout`, patch),
    log: (ws: string, name: string) => api.get<{ revisions: WorkRevision[] }>(`work/${enc(ws)}/log?name=${enc(name)}`),
    at: (ws: string, name: string, rev: number) =>
      api.get<{
        name: string
        rev: number
        content: string
        title?: string
        stage?: string
        sections?: Record<string, string>
      }>(`work/${enc(ws)}/at?name=${enc(name)}&rev=${rev}`),
  }
}

export type WorkClient = ReturnType<typeof workClient>

/** What every view of a workspace is handed. */
export interface ViewArgs {
  w: WorkClient
  ws: string
  space: WorkSpace
  picked: string | null
  pick: (id: string, newWindow?: boolean) => void
  changed: () => void
  setError: (s: string) => void
}
