/**
 * The Operations module's routes (/api/m/ops/), as the views call them. Types mirror the Go
 * (model.go, activity.go, seed.go).
 */
import type { ModuleAPI } from "@hollis-labs/surface-page/sdk"

export type Stage = "planning" | "planned" | "queued" | "in_flight" | "in_review" | "adoption" | "active" | "dropped"

export const STAGES: Stage[] = [
  "planning",
  "planned",
  "queued",
  "in_flight",
  "in_review",
  "adoption",
  "active",
  "dropped",
]

export const STAGE_LABEL: Record<string, string> = {
  planning: "Planning",
  planned: "Planned",
  queued: "Queued",
  in_flight: "In Flight",
  in_review: "In Review",
  adoption: "Adoption",
  active: "Active",
  dropped: "Dropped",
}

export const stageLabel = (s: string) => STAGE_LABEL[s] ?? s

export interface Held {
  reason: string
  since?: string
}

export interface Check {
  text: string
  done: boolean
  ref?: string
}

export interface Link {
  kind: string
  id: string
  role?: string
  label?: string
}

export interface Transition {
  from: string
  to: string
  at: string
  by?: string
  why?: string
}

export interface Workstream {
  id: string
  title: string
  owner?: string
  project?: string
  status_ns?: string
  stage: Stage
  stage_since?: string
  parent?: string
  held?: Held
  active_when: Check[]
  links: Link[]
  correlation: { labels?: string[]; tether_workstreams?: string[] }
  transitions: Transition[]
  purpose?: string
  active_means?: string
  notes?: string
  rev: number
  updated?: string
  children: string[]
  problem?: string
}

export interface Entry {
  at: string
  source: "torque" | "comment" | "status" | "ops"
  state?: string
  summary: string
  subject_urn?: string
  task?: string
  workstream: string
  verified: boolean
}

export interface Card extends Workstream {
  tasks?: string
  last?: Entry
  suggest?: string
  suggest_why?: string
}

export interface BoardData {
  workstreams: Card[]
  queue: string[]
  followups: string[]
  error?: string
}

export interface Task {
  id: string
  title: string
  status: string
  updated?: string
  workstream: string
  via?: string
  plan_id?: string
  phase_id?: string
}

export interface Plan {
  id: string
  title: string
  status: string
  workstream: string
  phases: { id: string; name: string; order: number; state: string }[]
  truncated?: boolean
}

export interface Rollup {
  workstream: string
  counts: Record<string, number>
  tasks: Task[]
  plans?: Plan[]
  entries: Entry[]
  last?: Entry
  suggest?: string
  suggest_why?: string
  errors?: string[]
  fetched_at: string
  unavailable?: boolean
}

export interface Detail {
  workstream: Workstream
  children: { id: string; title: string; stage: Stage; held?: Held; children: number }[]
  path: { id: string; title: string }[] | null
}

export interface Revision {
  rev: number
  at: string
  title: string
}

export interface History {
  transitions: Transition[]
  revisions: Revision[]
  ref: string
  error?: string
}

export interface SeedItem {
  include: boolean
  id: string
  title: string
  stage: Stage
  from_stage: string
  phase?: string
  owner?: string
  parent?: string
  notes: string
  links: Link[]
  exists?: boolean
  problems?: string[]
}

export interface SeedConcept {
  accept: boolean
  id: string
  title: string
  why: string
  members: string[]
  stage: Stage
  exists?: boolean
}

export interface SeedFollowup {
  index: number
  text: string
  workstream?: string
  why?: string
}

export interface SeedPlan {
  id: string
  source: string
  tracker_updated?: string
  created_at: string
  applied_at?: string
  workstreams: SeedItem[]
  concepts: SeedConcept[]
  followups: SeedFollowup[]
  mapping: string[]
}

export interface SeedResult {
  created: string[]
  skipped: string[]
  failed: string[]
  followups_for_owner: number
  noted: string[]
}

const q = encodeURIComponent

export function opsClient(api: ModuleAPI) {
  return {
    board: (force = false) => api.get<BoardData>(`board${force ? "?force=1" : ""}`),
    create: (w: Partial<Workstream> & { id: string; title: string }) => api.post<Workstream>("workstreams", w),
    get: (id: string) => api.get<Detail>(`workstreams/${q(id)}`),
    update: (id: string, patch: Record<string, unknown>) => api.put<Workstream>(`workstreams/${q(id)}`, patch),
    move: (id: string, stage: string, why: string) => api.post<Workstream>(`workstreams/${q(id)}/move`, { stage, why }),
    link: (id: string, l: Link) => api.post<Workstream>(`workstreams/${q(id)}/links`, l),
    unlink: (id: string, kind: string, ref: string) =>
      api.del<Workstream>(`workstreams/${q(id)}/links?kind=${q(kind)}&ref=${q(ref)}`),
    activity: (id: string, refresh = false) =>
      api.get<Rollup>(`workstreams/${q(id)}/activity${refresh ? "?refresh=1" : ""}`),
    history: (id: string) => api.get<History>(`workstreams/${q(id)}/history`),
    at: (id: string, rev: number) => api.get<{ rev: number; content: string }>(`workstreams/${q(id)}/at/${rev}`),
    reorder: (id: string, where: { before?: string; after?: string; dir?: "up" | "down" }) =>
      api.post<{ queue: string[] }>("queue/reorder", { id, ...where }),
    seedPreview: (src: { path?: string; json?: string }) => api.post<SeedPlan>("seed/preview", src),
    seedGet: (id: string) => api.get<SeedPlan>(`seed/${q(id)}`),
    seedSave: (p: SeedPlan) => api.put<SeedPlan>(`seed/${q(p.id)}`, p),
    /** Applies the preview as it is kept: save the owner's edits first. */
    seedApply: (id: string) => api.post<SeedResult>(`seed/${q(id)}/apply`),
  }
}

export type OpsClient = ReturnType<typeof opsClient>

export const message = (e: unknown) => (e instanceof Error ? e.message : String(e))

/** "3m", "5h", "2d": how long ago, short. */
export function age(iso?: string): string {
  if (!iso) return ""
  const ms = Date.now() - new Date(iso).getTime()
  if (Number.isNaN(ms)) return ""
  const m = Math.max(0, Math.round(ms / 60000))
  if (m < 60) return `${m}m`
  const h = Math.round(m / 60)
  if (h < 48) return `${h}h`
  return `${Math.round(h / 24)}d`
}

export const time = (iso?: string) => (iso ? new Date(iso).toLocaleString() : "")

/** The Cite kinds the page resolves; any other link shows as text. */
export const RESOLVED_KINDS = new Set([
  "ops.workstream",
  "work.item",
  "torque.task",
  "torque.epic",
  "torque.plan",
  "torque.project",
  "torque.sprint",
  "registry.record",
  "caster.doc",
  "tesseract.item",
  "tesseract.key",
  "tether.agent",
  "tether.message",
  "url",
])
