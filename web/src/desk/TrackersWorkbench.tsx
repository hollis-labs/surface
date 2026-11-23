/**
 * Native Trackers Workbench (`web/src/desk/TrackersWorkbench.tsx`).
 *
 * Categorized native tracker for related groups of tasks, topics, initiatives, and checklists
 * that the user and agent work through together:
 * - Supports taxonomy tags and categories to group items as needed.
 * - Each item has a clear lifecycle/status field (`todo`, `in_progress`, `blocked`, `review`,
 *   `done`, `parked`) so progress is visible at a glance.
 * - Supports arbitrary `metadata` and `extras` JSON objects for the agent to attach and read
 *   structured context on any tracker or item, tucked behind a clean inspector toggle.
 */
import {
  BracesIcon,
  CheckCircle2Icon,
  CircleDashedIcon,
  CircleDotIcon,
  FolderKanbanIcon,
  ListChecksIcon,
  PauseCircleIcon,
  PlusIcon,
  SearchIcon,
  TagIcon,
  Trash2Icon,
} from "lucide-react"
import { useEffect, useMemo, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Textarea } from "@/components/ui/textarea"
import { CiteText } from "../cite/ui"
import type { ViewProps } from "../sdk"

export type TrackerLifecycle = "active" | "paused" | "completed" | "archived"
export type TrackerItemStatus = "todo" | "in_progress" | "blocked" | "review" | "done" | "parked"

export interface TrackerItem {
  id: string
  tracker_id: string
  title: string
  summary?: string
  lifecycle: TrackerItemStatus
  category: string
  tags: string[]
  position: number
  assignee?: string
  metadata: Record<string, unknown>
  extras: Record<string, unknown>
  updated_at: string
}

export interface TrackerList {
  id: string
  slug: string
  title: string
  summary: string
  category: string
  tags: string[]
  lifecycle: TrackerLifecycle
  metadata: Record<string, unknown>
  extras: Record<string, unknown>
  updated_at: string
}

const STORAGE_TRACKERS_KEY = "surface.trackers.lists.v1"
const STORAGE_TRACKER_ITEMS_KEY = "surface.trackers.items.v1"

const SEED_TRACKERS: TrackerList[] = [
  {
    id: "trk-surface-ade",
    slug: "surface-ade-evolution",
    title: "Surface ADE & Visual Workflows Evolution",
    summary: "Tracking collaborative visual planning, entity cards, change approval diffs, and module polish.",
    category: "Engineering",
    tags: ["surface", "frontend", "genui", "cards"],
    lifecycle: "active",
    metadata: { repo: "hollis-labs/surface", torque_project: "PRJ-20261118-0001" },
    extras: { agent_seat: "surface-lead", parallel_branch: "feat/surface-backend-next" },
    updated_at: "2026-11-22T21:20:00Z",
  },
  {
    id: "trk-basalisk-personal",
    slug: "basalisk-macos-migration",
    title: "Personal macOS App & Basalisk OS Integration",
    summary: "Migrating hand-rolled macOS integration patterns and agent history from local Basalisk to Surface.",
    category: "Personal App",
    tags: ["basalisk", "macos", "agent-history", "webkit"],
    lifecycle: "active",
    metadata: {
      snapshot_path: "example-host:/srv/data/backups/laptop-basalisk-snapshot-20261122",
      messages_captured: 3953,
    },
    extras: { source_commit: "0e24fcc", target_framework: "substrate+surface" },
    updated_at: "2026-11-22T21:15:00Z",
  },
  {
    id: "trk-agent-onboarding",
    slug: "agent-setup-playbooks",
    title: "Agent Setup, Standing Prompts & Reflexes",
    summary: "Patterns learned from Basalisk usage history for configuring new autonomous agents and seats.",
    category: "Agent Ops",
    tags: ["agents", "prompts", "reflexes", "tether"],
    lifecycle: "active",
    metadata: { catalog: "/var/lib/fabric/tether/catalog" },
    extras: { reference_db: "agent.db", runs_analyzed: 469 },
    updated_at: "2026-11-22T21:10:00Z",
  },
]

const SEED_ITEMS: TrackerItem[] = [
  {
    id: "ti-101",
    tracker_id: "trk-surface-ade",
    title: "Module Entity Cards in Chatstream & Cards Window",
    summary: "Render Ops Kanban, Agent session, Fleet host, Registry record, and Tracker cards via card_show/card_ask.",
    lifecycle: "done",
    category: "Cards & GenUI",
    tags: ["cards", "ops", "registry"],
    position: 1,
    assignee: "agent:frontend",
    metadata: { torque_task: "CW-20261122-0008", component: "web/src/cards/EntityCard.tsx" },
    extras: { supports_answerable: true, live_cite_hydration: true },
    updated_at: "2026-11-22T21:22:00Z",
  },
  {
    id: "ti-102",
    tracker_id: "trk-surface-ade",
    title: "Visual Planning: Architecture Flow, Bucket Triage & Phased Timeline",
    summary: "Interactive proposal canvas so human and agent align visually before execution begins.",
    lifecycle: "done",
    category: "Visual Alignment",
    tags: ["architecture", "triage", "timeline"],
    position: 2,
    assignee: "agent:frontend",
    metadata: { torque_task: "CW-20261122-0009", view: "surface.align" },
    extras: { modes: ["architecture", "triage", "timeline", "droppad"] },
    updated_at: "2026-11-22T21:24:00Z",
  },
  {
    id: "ti-103",
    tracker_id: "trk-surface-ade",
    title: "Drop Pad for Collections & File Copy/Move Staging",
    summary: "Drag-and-drop pad to bundle files/refs into shareable collections or stage file operations.",
    lifecycle: "done",
    category: "Visual Alignment",
    tags: ["droppad", "files", "collections"],
    position: 3,
    assignee: "agent:frontend",
    metadata: { view: "surface.align?mode=droppad" },
    extras: { clipboard_context_export: true },
    updated_at: "2026-11-22T21:25:00Z",
  },
  {
    id: "ti-104",
    tracker_id: "trk-surface-ade",
    title: "Visual Change Approvals with Now vs After Diff & Callouts",
    summary: "Highlight setting/config/architecture changes side-by-side with concern, warning, and info callouts.",
    lifecycle: "done",
    category: "Approvals",
    tags: ["approvals", "diff", "callouts"],
    position: 4,
    assignee: "agent:frontend",
    metadata: { torque_task: "CW-20261122-0010", registry_item: "card-approval" },
    extras: { view_modes: ["diff", "side-by-side", "architecture"] },
    updated_at: "2026-11-22T21:26:00Z",
  },
  {
    id: "ti-105",
    tracker_id: "trk-surface-ade",
    title: "Merge Parallel Backend Worktree (feat/surface-backend-next)",
    summary:
      "Pull Go SQLite stores and MCP tools from the parallel backend agent once CW-20261122-0008..0011 complete.",
    lifecycle: "in_progress",
    category: "Backend Integration",
    tags: ["surface-next-be", "go", "merge"],
    position: 5,
    assignee: "agent:backend",
    metadata: { worktree: "/workspace/example/hollis-labs/worktrees/surface-backend-next" },
    extras: { tasks: ["CW-20261122-0008", "CW-20261122-0009", "CW-20261122-0010", "CW-20261122-0011"] },
    updated_at: "2026-11-22T21:27:00Z",
  },
  {
    id: "ti-201",
    tracker_id: "trk-basalisk-personal",
    title: "Snapshot Local Basalisk Code & SQLite History to example-host",
    summary: "Checkpoint agent.db (3,953 messages, 469 runs) and source code to /srv/data/backups.",
    lifecycle: "done",
    category: "Data Migration",
    tags: ["snapshot", "sqlite", "example-host"],
    position: 1,
    assignee: "agent:frontend",
    metadata: { path: "example-host:/srv/data/backups/laptop-basalisk-snapshot-20261122" },
    extras: { size_mb: 27 },
    updated_at: "2026-11-22T17:51:00Z",
  },
  {
    id: "ti-202",
    tracker_id: "trk-basalisk-personal",
    title: "Review Local Basalisk Standing Instructions & Slot Prompts",
    summary: "Extract effective standing instructions, roster definitions, and reflex patterns from agent.db.",
    lifecycle: "todo",
    category: "Agent Learning",
    tags: ["standing-md", "roster", "prompts"],
    position: 2,
    assignee: "user:chrispian",
    metadata: { tables: ["standing", "prompts", "roster", "reflexes"] },
    extras: {},
    updated_at: "2026-11-22T21:15:00Z",
  },
  {
    id: "ti-203",
    tracker_id: "trk-basalisk-personal",
    title: "Design macOS Native Shell Bridge on Surface Pattern",
    summary:
      "Bring macOS-specific window/hotkey/companion capabilities onto the Substrate + Surface module architecture.",
    lifecycle: "todo",
    category: "OS Integration",
    tags: ["macos", "shell", "companion"],
    position: 3,
    assignee: "user:chrispian",
    metadata: { source_dir: "code/basalisk-main/cmd/basalisk-mac" },
    extras: {},
    updated_at: "2026-11-22T21:15:00Z",
  },
]

function loadTrackerLists(): TrackerList[] {
  try {
    const raw = window.localStorage.getItem(STORAGE_TRACKERS_KEY)
    if (!raw) return SEED_TRACKERS
    const parsed = JSON.parse(raw) as TrackerList[]
    return parsed.length > 0 ? parsed : SEED_TRACKERS
  } catch {
    return SEED_TRACKERS
  }
}

function saveTrackerLists(lists: TrackerList[]) {
  try {
    window.localStorage.setItem(STORAGE_TRACKERS_KEY, JSON.stringify(lists))
  } catch {}
}

function loadTrackerItems(): TrackerItem[] {
  try {
    const raw = window.localStorage.getItem(STORAGE_TRACKER_ITEMS_KEY)
    if (!raw) return SEED_ITEMS
    const parsed = JSON.parse(raw) as TrackerItem[]
    return parsed.length > 0 ? parsed : SEED_ITEMS
  } catch {
    return SEED_ITEMS
  }
}

function saveTrackerItems(items: TrackerItem[]) {
  try {
    window.localStorage.setItem(STORAGE_TRACKER_ITEMS_KEY, JSON.stringify(items))
  } catch {}
}

const ITEM_STATUSES: TrackerItemStatus[] = ["todo", "in_progress", "blocked", "review", "done", "parked"]

function statusMeta(s: TrackerItemStatus) {
  switch (s) {
    case "done":
      return {
        label: "Done",
        badge: "border-success/40 bg-success/15 text-success",
        dot: "bg-success",
      }
    case "in_progress":
      return {
        label: "In Progress",
        badge: "border-primary/40 bg-primary/15 text-primary",
        dot: "bg-primary shadow-[0_0_6px_var(--color-primary)]",
      }
    case "review":
      return {
        label: "In Review",
        badge: "border-chart-2/40 bg-chart-2/15 text-chart-2",
        dot: "bg-chart-2",
      }
    case "blocked":
      return {
        label: "Blocked",
        badge: "border-destructive/40 bg-destructive/15 text-destructive",
        dot: "bg-destructive",
      }
    case "parked":
      return {
        label: "Parked",
        badge: "border-border bg-muted/40 text-muted-foreground",
        dot: "bg-muted-foreground",
      }
    default:
      return {
        label: "To Do",
        badge: "border-border text-muted-foreground",
        dot: "bg-muted-foreground/70",
      }
  }
}

export function TrackersWorkbench(_props: Partial<ViewProps> = {}) {
  const [lists, setLists] = useState<TrackerList[]>(() => loadTrackerLists())
  const [items, setItems] = useState<TrackerItem[]>(() => loadTrackerItems())
  const [selectedTrackerId, setSelectedTrackerId] = useState<string>(() => SEED_TRACKERS[0].id)
  const [groupBy, setGroupBy] = useState<"category" | "lifecycle" | "tag">("category")
  const [statusFilter, setStatusFilter] = useState<string>("all")
  const [tagFilter, setTagFilter] = useState<string>("all")
  const [search, setSearch] = useState("")

  // New tracker / new item inline inputs
  const [newTrackerTitle, setNewTrackerTitle] = useState("")
  const [newTrackerCat, setNewTrackerCat] = useState("Engineering")
  const [newItemTitle, setNewItemTitle] = useState("")
  const [newItemCat, setNewItemCat] = useState("General")
  const [newItemTags, setNewItemTags] = useState("")

  // JSON metadata & extras modal inspector
  const [inspectedItem, setInspectedItem] = useState<TrackerItem | null>(null)
  const [metaDraft, setMetaDraft] = useState("")
  const [extrasDraft, setExtrasDraft] = useState("")
  const [jsonError, setJsonError] = useState("")

  // Opportunistically sync with /api/m/trackers/lists when backend module is mounted
  useEffect(() => {
    fetch("/api/m/trackers/lists")
      .then((r) => (r.ok ? r.json() : null))
      .then((data) => {
        if (Array.isArray(data) && data.length > 0) {
          setLists(data as TrackerList[])
        }
      })
      .catch(() => {})
  }, [])

  const activeTracker = useMemo(
    () => lists.find((l) => l.id === selectedTrackerId) ?? lists[0],
    [lists, selectedTrackerId],
  )

  const trackerItems = useMemo(
    () => items.filter((i) => i.tracker_id === activeTracker?.id).sort((a, b) => a.position - b.position),
    [items, activeTracker],
  )

  const allTags = useMemo(() => {
    const s = new Set<string>()
    for (const it of trackerItems) {
      for (const t of it.tags) s.add(t)
    }
    return [...s].sort()
  }, [trackerItems])

  const filteredItems = useMemo(() => {
    const q = search.trim().toLowerCase()
    return trackerItems.filter((it) => {
      if (statusFilter === "open" && (it.lifecycle === "done" || it.lifecycle === "parked")) return false
      if (statusFilter !== "all" && statusFilter !== "open" && it.lifecycle !== statusFilter) return false
      if (tagFilter !== "all" && !it.tags.includes(tagFilter)) return false
      if (q) {
        const hay = `${it.title} ${it.summary ?? ""} ${it.category} ${it.tags.join(" ")}`.toLowerCase()
        if (!hay.includes(q)) return false
      }
      return true
    })
  }, [trackerItems, statusFilter, tagFilter, search])

  const groupedItems = useMemo(() => {
    const map = new Map<string, TrackerItem[]>()
    for (const it of filteredItems) {
      if (groupBy === "lifecycle") {
        const k = statusMeta(it.lifecycle).label
        map.set(k, [...(map.get(k) ?? []), it])
      } else if (groupBy === "tag") {
        const tags = it.tags.length > 0 ? it.tags : ["untagged"]
        for (const t of tags) {
          map.set(`#${t}`, [...(map.get(`#${t}`) ?? []), it])
        }
      } else {
        const k = it.category || "General"
        map.set(k, [...(map.get(k) ?? []), it])
      }
    }
    return [...map.entries()]
  }, [filteredItems, groupBy])

  const updateItem = (id: string, fn: (it: TrackerItem) => TrackerItem) => {
    setItems((prev) => {
      const next = prev.map((it) => (it.id === id ? fn(it) : it))
      saveTrackerItems(next)
      return next
    })
  }

  const cycleStatus = (it: TrackerItem) => {
    const nextStatus: TrackerItemStatus =
      it.lifecycle === "todo" ? "in_progress" : it.lifecycle === "in_progress" ? "done" : "todo"
    updateItem(it.id, (curr) => ({ ...curr, lifecycle: nextStatus, updated_at: new Date().toISOString() }))
  }

  const openInspector = (it: TrackerItem) => {
    setInspectedItem(it)
    setMetaDraft(JSON.stringify(it.metadata ?? {}, null, 2))
    setExtrasDraft(JSON.stringify(it.extras ?? {}, null, 2))
    setJsonError("")
  }

  return (
    <div className="flex h-full min-h-0 min-w-0 flex-col overflow-x-hidden overscroll-x-none bg-background">
      {/* Top Invariant Header */}
      <header className="flex h-11 shrink-0 items-center justify-between gap-2 border-b border-border/80 bg-card/40 px-3">
        <div className="flex min-w-0 items-center gap-2">
          <ListChecksIcon className="size-4 shrink-0 text-primary" />
          <span className="font-semibold text-sm tracking-tight">Trackers</span>
          {activeTracker && (
            <>
              <span className="text-border">/</span>
              <Badge variant="outline" className="h-5 px-2 font-mono text-[11px] text-primary">
                {activeTracker.title}
              </Badge>
            </>
          )}
        </div>

        <div className="flex shrink-0 items-center gap-2">
          <div className="flex items-center gap-1 rounded-sm border border-border/75 bg-muted/30 p-0.5">
            {(
              [
                { id: "category", label: "By Category" },
                { id: "lifecycle", label: "By Status" },
                { id: "tag", label: "By Tag" },
              ] as const
            ).map((g) => (
              <Button
                key={g.id}
                size="xs"
                variant={groupBy === g.id ? "secondary" : "ghost"}
                className="h-6 px-2 font-mono text-[11px]"
                onClick={() => setGroupBy(g.id)}
              >
                {g.label}
              </Button>
            ))}
          </div>
        </div>
      </header>

      {/* Main Split View: Left Trackers List + Right Categorized Items */}
      <div className="flex min-h-0 min-w-0 flex-1 overflow-x-hidden">
        {/* Left Rail: Tracker Initiatives */}
        <aside className="flex w-72 shrink-0 flex-col justify-between border-r border-border/75 bg-card/25 p-2.5">
          <div className="flex min-h-0 flex-1 flex-col gap-2 overflow-y-auto">
            <div className="flex items-center justify-between px-1">
              <span className="font-mono text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
                Active Trackers ({lists.length})
              </span>
            </div>

            <div className="flex flex-col gap-1.5">
              {lists.map((trk) => {
                const tItems = items.filter((i) => i.tracker_id === trk.id)
                const doneCount = tItems.filter((i) => i.lifecycle === "done").length
                const pct = tItems.length > 0 ? Math.round((doneCount / tItems.length) * 100) : 0
                const active = trk.id === activeTracker?.id

                return (
                  <Button
                    key={trk.id}
                    variant="ghost"
                    onClick={() => {
                      setSelectedTrackerId(trk.id)
                      setTagFilter("all")
                    }}
                    className={`h-auto w-full flex-col items-start justify-start gap-1.5 rounded-md border p-2.5 text-left transition-all ${
                      active
                        ? "border-primary/50 bg-primary/10 shadow-[inset_2px_0_0_var(--color-primary)]"
                        : "border-border/60 bg-background/40 hover:border-border"
                    }`}
                  >
                    <div className="flex w-full items-center justify-between gap-1.5">
                      <span className="truncate font-semibold text-foreground text-xs">{trk.title}</span>
                      <Badge variant="outline" className="h-4 shrink-0 px-1 font-mono text-[9px]">
                        {doneCount}/{tItems.length}
                      </Badge>
                    </div>
                    <p className="line-clamp-2 text-[11px] text-muted-foreground leading-snug">{trk.summary}</p>
                    <div className="flex w-full items-center gap-2 pt-0.5">
                      <div className="h-1 flex-1 overflow-hidden rounded-full bg-muted">
                        <div className="h-full bg-primary" style={{ width: `${pct}%` }} />
                      </div>
                      <span className="font-mono text-[9.5px] text-muted-foreground">{trk.category}</span>
                    </div>
                  </Button>
                )
              })}
            </div>
          </div>

          {/* Create New Tracker */}
          <div className="flex flex-col gap-1.5 border-t border-border/60 pt-2.5">
            <span className="font-mono text-[10px] font-semibold uppercase text-muted-foreground">
              + New Tracker Group
            </span>
            <Input
              value={newTrackerTitle}
              onChange={(e) => setNewTrackerTitle(e.target.value)}
              placeholder="Tracker title (e.g. Q4 Launch)…"
              className="h-7 text-xs"
            />
            <div className="flex items-center gap-1.5">
              <Input
                value={newTrackerCat}
                onChange={(e) => setNewTrackerCat(e.target.value)}
                placeholder="Category"
                className="h-7 flex-1 text-xs"
              />
              <Button
                size="xs"
                disabled={!newTrackerTitle.trim()}
                onClick={() => {
                  const slug = newTrackerTitle
                    .trim()
                    .toLowerCase()
                    .replace(/[^a-z0-9]+/g, "-")
                  const trk: TrackerList = {
                    id: `trk-${Date.now()}`,
                    slug,
                    title: newTrackerTitle.trim(),
                    summary: "Categorized tracker list for user & agent collaboration.",
                    category: newTrackerCat.trim() || "General",
                    tags: [slug],
                    lifecycle: "active",
                    metadata: {},
                    extras: {},
                    updated_at: new Date().toISOString(),
                  }
                  const next = [trk, ...lists]
                  setLists(next)
                  saveTrackerLists(next)
                  setSelectedTrackerId(trk.id)
                  setNewTrackerTitle("")
                }}
              >
                <PlusIcon className="size-3" />
                Create
              </Button>
            </div>
          </div>
        </aside>

        {/* Main Tracker Items Canvas */}
        {activeTracker && (
          <div className="flex min-h-0 min-w-0 flex-1 flex-col gap-4 overflow-y-auto overflow-x-hidden overscroll-x-none p-4">
            {/* Tracker Summary & Taxonomy Filter Strip */}
            <div className="flex flex-col gap-3 rounded-md border border-border/80 bg-card/80 p-3.5">
              <div className="flex flex-wrap items-start justify-between gap-3">
                <div className="flex items-start gap-2.5">
                  <span className="flex size-8 shrink-0 items-center justify-center rounded-sm border border-primary/30 bg-primary/10 text-primary">
                    <FolderKanbanIcon className="size-4" />
                  </span>
                  <div>
                    <div className="flex flex-wrap items-center gap-2">
                      <h2 className="font-bold text-base text-foreground">{activeTracker.title}</h2>
                      <Badge variant="outline" className="font-mono text-[10px]">
                        tracker:{activeTracker.slug}
                      </Badge>
                      <Badge variant="secondary" className="font-mono text-[10px]">
                        {activeTracker.category}
                      </Badge>
                    </div>
                    <p className="text-muted-foreground text-xs">{activeTracker.summary}</p>
                  </div>
                </div>

                {/* Lifecycle Filter Pills */}
                <div className="flex flex-wrap items-center gap-1 rounded-sm border border-border/75 bg-muted/30 p-0.5">
                  {(
                    [
                      { id: "all", label: `All (${trackerItems.length})` },
                      {
                        id: "open",
                        label: `Open (${trackerItems.filter((i) => i.lifecycle !== "done" && i.lifecycle !== "parked").length})`,
                      },
                      {
                        id: "in_progress",
                        label: `In Progress (${trackerItems.filter((i) => i.lifecycle === "in_progress").length})`,
                      },
                      {
                        id: "done",
                        label: `Done (${trackerItems.filter((i) => i.lifecycle === "done").length})`,
                      },
                    ] as const
                  ).map((f) => (
                    <Button
                      key={f.id}
                      size="xs"
                      variant={statusFilter === f.id ? "secondary" : "ghost"}
                      className="h-6 px-2 font-mono text-[11px]"
                      onClick={() => setStatusFilter(f.id)}
                    >
                      {f.label}
                    </Button>
                  ))}
                </div>
              </div>

              {/* Taxonomy Tag Filter Bar + Quick Search */}
              <div className="flex flex-wrap items-center justify-between gap-2 border-t border-border/60 pt-2.5">
                <div className="flex flex-wrap items-center gap-1">
                  <TagIcon className="mr-1 size-3.5 text-muted-foreground" />
                  <Button
                    size="xs"
                    variant={tagFilter === "all" ? "secondary" : "ghost"}
                    className="h-5 px-2 font-mono text-[10.5px]"
                    onClick={() => setTagFilter("all")}
                  >
                    All tags
                  </Button>
                  {allTags.map((t) => (
                    <Button
                      key={t}
                      size="xs"
                      variant={tagFilter === t ? "secondary" : "ghost"}
                      className="h-5 px-2 font-mono text-[10.5px]"
                      onClick={() => setTagFilter(tagFilter === t ? "all" : t)}
                    >
                      #{t}
                    </Button>
                  ))}
                </div>

                <div className="relative w-56">
                  <SearchIcon className="absolute top-1.5 left-2 size-3.5 text-muted-foreground" />
                  <Input
                    value={search}
                    onChange={(e) => setSearch(e.target.value)}
                    placeholder="Filter items, tags, category…"
                    className="h-6 pl-7 text-xs"
                  />
                </div>
              </div>
            </div>

            {/* Quick Add Tracker Item Bar */}
            <div className="flex flex-wrap items-center gap-2 rounded-md border border-border/75 bg-muted/20 px-3 py-2">
              <Input
                value={newItemTitle}
                onChange={(e) => setNewItemTitle(e.target.value)}
                placeholder="Add new task or topic to this tracker…"
                className="h-7 min-w-56 flex-1 text-xs"
                onKeyDown={(e) => {
                  if (e.key === "Enter" && newItemTitle.trim()) {
                    const tags = newItemTags
                      .split(",")
                      .map((s) => s.trim().replace(/^#/, ""))
                      .filter(Boolean)
                    const it: TrackerItem = {
                      id: `ti-${Date.now()}`,
                      tracker_id: activeTracker.id,
                      title: newItemTitle.trim(),
                      lifecycle: "todo",
                      category: newItemCat.trim() || "General",
                      tags,
                      position: trackerItems.length + 1,
                      assignee: "user:chrispian",
                      metadata: {},
                      extras: {},
                      updated_at: new Date().toISOString(),
                    }
                    const next = [...items, it]
                    setItems(next)
                    saveTrackerItems(next)
                    setNewItemTitle("")
                    setNewItemTags("")
                  }
                }}
              />
              <Input
                value={newItemCat}
                onChange={(e) => setNewItemCat(e.target.value)}
                placeholder="Category (e.g. Frontend)"
                className="h-7 w-36 text-xs"
              />
              <Input
                value={newItemTags}
                onChange={(e) => setNewItemTags(e.target.value)}
                placeholder="Tags (comma-separated)"
                className="h-7 w-44 font-mono text-xs"
              />
              <Button
                size="xs"
                disabled={!newItemTitle.trim()}
                onClick={() => {
                  const tags = newItemTags
                    .split(",")
                    .map((s) => s.trim().replace(/^#/, ""))
                    .filter(Boolean)
                  const it: TrackerItem = {
                    id: `ti-${Date.now()}`,
                    tracker_id: activeTracker.id,
                    title: newItemTitle.trim(),
                    lifecycle: "todo",
                    category: newItemCat.trim() || "General",
                    tags,
                    position: trackerItems.length + 1,
                    assignee: "user:chrispian",
                    metadata: {},
                    extras: {},
                    updated_at: new Date().toISOString(),
                  }
                  const next = [...items, it]
                  setItems(next)
                  saveTrackerItems(next)
                  setNewItemTitle("")
                  setNewItemTags("")
                }}
              >
                <PlusIcon className="size-3" />
                Add Item
              </Button>
            </div>

            {/* Categorized / Grouped Items List */}
            <div className="flex flex-col gap-4">
              {groupedItems.map(([groupName, groupList]) => (
                <div key={groupName} className="flex flex-col gap-2">
                  <div className="flex items-center justify-between border-b border-border/60 px-1 pb-1">
                    <span className="font-mono text-xs font-bold uppercase tracking-wider text-primary">
                      {groupName}
                    </span>
                    <Badge variant="secondary" className="h-4 px-1.5 font-mono text-[10px]">
                      {groupList.length}
                    </Badge>
                  </div>

                  <div className="flex flex-col gap-1.5">
                    {groupList.map((item) => {
                      const sm = statusMeta(item.lifecycle)
                      const isDone = item.lifecycle === "done"
                      const hasMetaOrExtras =
                        Object.keys(item.metadata ?? {}).length > 0 || Object.keys(item.extras ?? {}).length > 0

                      return (
                        <div
                          key={item.id}
                          className={`flex flex-wrap items-center justify-between gap-3 rounded-md border px-3 py-2.5 transition-all ${
                            isDone
                              ? "border-border/50 bg-card/35 opacity-75"
                              : "border-border/80 bg-card/80 hover:border-primary/40"
                          }`}
                        >
                          <div className="flex min-w-0 flex-1 items-start gap-2.5">
                            <Button
                              size="icon-xs"
                              variant="ghost"
                              className="mt-0.5 size-5 shrink-0"
                              onClick={() => cycleStatus(item)}
                              title={`Status: ${sm.label} (click to cycle todo → in_progress → done)`}
                            >
                              {isDone ? (
                                <CheckCircle2Icon className="size-4 text-success" />
                              ) : item.lifecycle === "in_progress" ? (
                                <CircleDotIcon className="size-4 text-primary" />
                              ) : item.lifecycle === "parked" ? (
                                <PauseCircleIcon className="size-4 text-muted-foreground" />
                              ) : (
                                <CircleDashedIcon className="size-4 text-muted-foreground" />
                              )}
                            </Button>

                            <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                              <div className="flex flex-wrap items-center gap-1.5">
                                <span
                                  className={`font-semibold text-xs ${
                                    isDone ? "text-muted-foreground line-through" : "text-foreground"
                                  }`}
                                >
                                  {item.title}
                                </span>
                                <Badge variant="outline" className="h-4 px-1.5 font-mono text-[9.5px]">
                                  {item.category}
                                </Badge>
                                {item.tags.map((t) => (
                                  <Badge key={t} variant="secondary" className="h-4 px-1.5 font-mono text-[9.5px]">
                                    #{t}
                                  </Badge>
                                ))}
                              </div>
                              {item.summary && (
                                <p className="text-[11px] text-muted-foreground">
                                  <CiteText text={item.summary} />
                                </p>
                              )}
                            </div>
                          </div>

                          <div className="flex shrink-0 items-center gap-1.5">
                            <Select
                              value={item.lifecycle}
                              onValueChange={(v) =>
                                v &&
                                updateItem(item.id, (curr) => ({
                                  ...curr,
                                  lifecycle: v as TrackerItemStatus,
                                  updated_at: new Date().toISOString(),
                                }))
                              }
                            >
                              <SelectTrigger
                                size="sm"
                                className={`h-6 w-28 font-mono text-[10px] ${sm.badge}`}
                                aria-label="Lifecycle status"
                              >
                                <SelectValue />
                              </SelectTrigger>
                              <SelectContent>
                                {ITEM_STATUSES.map((st) => (
                                  <SelectItem key={st} value={st}>
                                    {st}
                                  </SelectItem>
                                ))}
                              </SelectContent>
                            </Select>

                            <Button
                              size="icon-xs"
                              variant={hasMetaOrExtras ? "secondary" : "ghost"}
                              className="size-6"
                              onClick={() => openInspector(item)}
                              title="Inspect / Edit Metadata & Agent Extras JSON"
                              aria-label="Inspect Metadata and Extras JSON"
                            >
                              <BracesIcon className="size-3.5" />
                            </Button>

                            <Button
                              size="icon-xs"
                              variant="ghost"
                              className="size-6 text-muted-foreground hover:text-destructive"
                              onClick={() => {
                                const next = items.filter((x) => x.id !== item.id)
                                setItems(next)
                                saveTrackerItems(next)
                              }}
                              title="Delete item"
                            >
                              <Trash2Icon className="size-3.5" />
                            </Button>
                          </div>
                        </div>
                      )
                    })}
                  </div>
                </div>
              ))}
            </div>
          </div>
        )}
      </div>

      {/* Metadata & Agent Extras JSON Inspector Modal */}
      <Dialog open={!!inspectedItem} onOpenChange={(o) => !o && setInspectedItem(null)}>
        <DialogContent className="sm:max-w-2xl">
          {inspectedItem && (
            <>
              <DialogHeader>
                <div className="flex items-center gap-2">
                  <Badge variant="outline" className="font-mono text-[10px] text-primary">
                    TRACKER METADATA &amp; AGENT EXTRAS
                  </Badge>
                  <span className="font-mono text-xs text-muted-foreground">tracker.item:{inspectedItem.id}</span>
                </div>
                <DialogTitle className="text-base">{inspectedItem.title}</DialogTitle>
                <DialogDescription>
                  Arbitrary structured JSON objects attached to this tracker item for user and agent context.
                </DialogDescription>
              </DialogHeader>

              <div className="grid grid-cols-1 gap-3 pt-2 sm:grid-cols-2">
                <div className="flex flex-col gap-1">
                  <span className="font-mono text-[10.5px] font-semibold uppercase text-muted-foreground">
                    metadata (JSON Object)
                  </span>
                  <Textarea
                    value={metaDraft}
                    onChange={(e) => setMetaDraft(e.target.value)}
                    rows={8}
                    className="font-mono text-xs"
                  />
                </div>
                <div className="flex flex-col gap-1">
                  <span className="font-mono text-[10.5px] font-semibold uppercase text-muted-foreground">
                    extras (Agent Extras JSON Object)
                  </span>
                  <Textarea
                    value={extrasDraft}
                    onChange={(e) => setExtrasDraft(e.target.value)}
                    rows={8}
                    className="font-mono text-xs"
                  />
                </div>
              </div>

              {jsonError && <p className="text-destructive text-xs">{jsonError}</p>}

              <div className="flex justify-end gap-2 pt-2">
                <Button size="sm" variant="ghost" onClick={() => setInspectedItem(null)}>
                  Cancel
                </Button>
                <Button
                  size="sm"
                  onClick={() => {
                    try {
                      const parsedMeta = JSON.parse(metaDraft || "{}") as Record<string, unknown>
                      const parsedExtras = JSON.parse(extrasDraft || "{}") as Record<string, unknown>
                      updateItem(inspectedItem.id, (curr) => ({
                        ...curr,
                        metadata: parsedMeta,
                        extras: parsedExtras,
                        updated_at: new Date().toISOString(),
                      }))
                      setInspectedItem(null)
                    } catch (err) {
                      setJsonError((err as Error).message)
                    }
                  }}
                >
                  Save JSON Objects
                </Button>
              </div>
            </>
          )}
        </DialogContent>
      </Dialog>
    </div>
  )
}
