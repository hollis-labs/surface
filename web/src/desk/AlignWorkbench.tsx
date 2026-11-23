/**
 * Visual Strategy, Alignment Proposals & Drop Pad Workbench (`web/src/desk/AlignWorkbench.tsx`).
 *
 * Implements the 4 visual alignment & organization workflows:
 * 1. Architecture Flow Proposal: Co-editable visual node/edge/lane diagram with draft vs baseline
 *    diff highlights, callouts, revision snapshots, and Approve & Save Reference.
 * 2. Bucket Triage Proposal: Unassigned staging pool + defined target containers where the agent
 *    proposes bucket placements and the user drags/moves/parks/defers/removes items before approving.
 * 3. Phased Timeline Proposal: Flight-itinerary / activity-timeline style phases & milestones with
 *    drag-and-drop phase assignment, ordering, and defer/park actions.
 * 4. Drop Pad: Visual drag-and-drop pad for organizing files and Surface references into named
 *    collections (to share with an agent or reference elsewhere) or staging batch file move/copy ops.
 */
import { classifyPriorResponse } from "@hollis-labs/surface-cards"
import {
  ArchiveIcon,
  ArrowDownIcon,
  ArrowRightIcon,
  ArrowUpIcon,
  CheckCircle2Icon,
  ClockIcon,
  CopyIcon,
  FileIcon,
  FolderInputIcon,
  FolderPlusIcon,
  GitBranchIcon,
  KanbanIcon,
  LayersIcon,
  NetworkIcon,
  PauseCircleIcon,
  PlusIcon,
  SendIcon,
  SparklesIcon,
  Trash2Icon,
  XCircleIcon,
} from "lucide-react"
import { type DragEvent, useEffect, useMemo, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { CardBody, CardFoot, CardFrame, CardStatus } from "@/components/ui/card-frame"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import type { CardRendererProps } from "../cards/renderers"
import { CiteText } from "../cite/ui"
import type { ViewProps } from "../sdk"
import { desk } from "./store"

export type AlignMode = "architecture" | "triage" | "timeline" | "droppad"
export type ProposalStatus = "draft" | "proposed" | "approved" | "changes_requested" | "parked"

export interface ArchNode {
  id: string
  label: string
  kind: "service" | "store" | "agent" | "client" | "queue" | "decision"
  subtitle?: string
  lane: "client" | "core" | "fabric"
  diff?: "added" | "modified" | "unchanged"
  note?: string
}

export interface ArchEdge {
  id: string
  from: string
  to: string
  label?: string
  style?: "flow" | "async" | "human" | "data"
}

export interface TriageBucket {
  id: string
  title: string
  subtitle?: string
  tone: "cyan" | "emerald" | "amber" | "violet" | "slate"
}

export interface TriageItem {
  id: string
  title: string
  summary?: string
  ref?: string
  bucketId: string
  proposedBucketId?: string
  disposition: "active" | "parked" | "deferred" | "removed"
  tags?: string[]
}

export interface TimelinePhase {
  id: string
  title: string
  window: string
  subtitle: string
}

export interface TimelineStep {
  id: string
  title: string
  summary: string
  ref?: string
  phaseId: string
  status: "planned" | "in_progress" | "done" | "deferred" | "parked"
  owner?: string
}

export interface DropPadItem {
  id: string
  kind: "file" | "ref" | "folder"
  pathOrRef: string
  label: string
  sizeOrMeta?: string
  destPath?: string
  status: "staged" | "copied" | "moved"
}

export interface DropPadCollection {
  id: string
  title: string
  summary: string
  mode: "collection" | "file_ops"
  operation: "reference" | "copy" | "move"
  targetDir: string
  items: DropPadItem[]
  updatedAt: string
}

export interface AlignProposal {
  id: string
  title: string
  summary: string
  subtype: "architecture" | "triage" | "timeline"
  status: ProposalStatus
  author: string
  revision: number
  updatedAt: string
  archNodes?: ArchNode[]
  archEdges?: ArchEdge[]
  buckets?: TriageBucket[]
  triageItems?: TriageItem[]
  phases?: TimelinePhase[]
  timelineSteps?: TimelineStep[]
}

const STORAGE_PROPOSALS_KEY = "surface.align.proposals.v1"
const STORAGE_DROPPADS_KEY = "surface.align.droppads.v1"

const SEED_PROPOSALS: AlignProposal[] = [
  {
    id: "prop-arch-surface-v2",
    title: "Surface ADE ↔ Agent OS Event & Card Federation Architecture",
    summary:
      "Proposed architecture connecting Surface Desk modules (Ops, Registry, Align, Trackers) with Tether MCP and live entity cards.",
    subtype: "architecture",
    status: "proposed",
    author: "agent:archie",
    revision: 3,
    updatedAt: "2026-11-22T21:15:00Z",
    archNodes: [
      {
        id: "ui-desk",
        label: "Surface Desk UI",
        kind: "client",
        subtitle: "React 19 · @surface/ui",
        lane: "client",
        diff: "modified",
      },
      {
        id: "card-engine",
        label: "Entity & Diff Cards",
        kind: "client",
        subtitle: "CardEntity · CardApproval",
        lane: "client",
        diff: "added",
        note: "Renders live Ops, Agent, Host, Registry & Tracker cards inline",
      },
      {
        id: "kernel-cite",
        label: "Surface Kernel & Cite Bus",
        kind: "service",
        subtitle: "core/internal/kernel",
        lane: "core",
        diff: "modified",
      },
      {
        id: "mod-align",
        label: "Align & Drop Pad Store",
        kind: "store",
        subtitle: "modules/align (SQLite)",
        lane: "core",
        diff: "added",
      },
      {
        id: "mod-trackers",
        label: "Native Trackers Store",
        kind: "store",
        subtitle: "modules/trackers (SQLite)",
        lane: "core",
        diff: "added",
      },
      {
        id: "tether-mux",
        label: "Tether MCP Gateway",
        kind: "queue",
        subtitle: "tetherd.sock",
        lane: "fabric",
        diff: "unchanged",
      },
      {
        id: "agent-seats",
        label: "Nanite & Specialist Agents",
        kind: "agent",
        subtitle: "Parallel Backend & Frontend",
        lane: "fabric",
        diff: "unchanged",
      },
    ],
    archEdges: [
      { id: "e1", from: "ui-desk", to: "kernel-cite", label: "HTTP / SSE", style: "flow" },
      { id: "e2", from: "card-engine", to: "kernel-cite", label: "/api/cite/resolve", style: "data" },
      { id: "e3", from: "kernel-cite", to: "mod-align", label: "align_*", style: "flow" },
      { id: "e4", from: "kernel-cite", to: "mod-trackers", label: "tracker_*", style: "flow" },
      { id: "e5", from: "kernel-cite", to: "tether-mux", label: "tetherlink", style: "async" },
      { id: "e6", from: "agent-seats", to: "card-engine", label: "card_show / card_ask", style: "human" },
    ],
  },
  {
    id: "prop-triage-q4-wave",
    title: "Surface Q4 Feature Wave — Bucket Triage Proposal",
    summary:
      "Agent proposed bucket assignments for 8 incoming items across Immediate Frontend, Parallel Backend, and Personal macOS App.",
    subtype: "triage",
    status: "proposed",
    author: "agent:surface-lead",
    revision: 2,
    updatedAt: "2026-11-22T21:20:00Z",
    buckets: [
      { id: "unassigned", title: "Staging / Unassigned Pool", subtitle: "Items awaiting placement", tone: "slate" },
      { id: "bucket-fe", title: "Slice A · Surface Frontend", subtitle: "Immediate UI & Registry items", tone: "cyan" },
      {
        id: "bucket-be",
        title: "Slice B · Parallel Backend",
        subtitle: "Torque tag: surface-next-be",
        tone: "emerald",
      },
      {
        id: "bucket-mac",
        title: "Slice C · Personal macOS App",
        subtitle: "OS-level Basalisk migration",
        tone: "violet",
      },
    ],
    triageItems: [
      {
        id: "ti-1",
        title: "Module Entity Cards in Chat & Cards Window",
        summary: "Render Ops Kanban, Agent, Fleet Host, Registry, and Tracker cards via card_show/card_ask.",
        ref: "CW-20261122-0008",
        bucketId: "bucket-fe",
        proposedBucketId: "bucket-fe",
        disposition: "active",
        tags: ["cards", "ops", "registry"],
      },
      {
        id: "ti-2",
        title: "Visual Before/After Change Approval Diff & Callouts",
        summary: "Side-by-side Now vs After config/architecture comparison with concern & warning callouts.",
        ref: "CW-20261122-0010",
        bucketId: "bucket-fe",
        proposedBucketId: "bucket-fe",
        disposition: "active",
        tags: ["approvals", "diff"],
      },
      {
        id: "ti-3",
        title: "Go SQLite Store & MCP Tools for modules/align & modules/trackers",
        summary: "Backend persistence, Cite resolvers, and agent tool wiring in feat/surface-backend-next.",
        ref: "CW-20261122-0009",
        bucketId: "bucket-be",
        proposedBucketId: "bucket-be",
        disposition: "active",
        tags: ["surface-next-be", "go"],
      },
      {
        id: "ti-4",
        title: "Cerberus Extended Host Logs & Probe History Endpoint",
        summary: "Project live cerberus_host_get and service unit logs into modules/ops/fleet.go.",
        ref: "CW-20261121-0130",
        bucketId: "bucket-be",
        proposedBucketId: "bucket-be",
        disposition: "active",
        tags: ["cerberus", "fleet"],
      },
      {
        id: "ti-5",
        title: "macOS Native WebKit Shell & Global Hotkey Frag Capture",
        summary: "Port hand-rolled macOS OS-level integration from local Basalisk snapshot to Surface pattern.",
        bucketId: "bucket-mac",
        proposedBucketId: "bucket-mac",
        disposition: "active",
        tags: ["macos", "basalisk"],
      },
      {
        id: "ti-6",
        title: "Registry Authenticated HTTP Write Endpoints",
        summary: "Expose owner-gated POST/PUT/DELETE routes in modules/registry/registry.go.",
        ref: "CW-20261121-0178",
        bucketId: "unassigned",
        proposedBucketId: "bucket-be",
        disposition: "active",
        tags: ["registry", "api"],
      },
    ],
  },
  {
    id: "prop-timeline-rollout",
    title: "Basalisk → Surface ADE Convergence Timeline Plan",
    summary:
      "Phased rollout sequence aligning frontend visual components, parallel backend stores, and local macOS app migration.",
    subtype: "timeline",
    status: "proposed",
    author: "agent:agent-ops",
    revision: 1,
    updatedAt: "2026-11-22T21:25:00Z",
    phases: [
      {
        id: "phase-1",
        title: "Phase 1 · Visual Primitives & Cards",
        window: "Current Session",
        subtitle: "Frontend & Registry",
      },
      {
        id: "phase-2",
        title: "Phase 2 · Backend Store & Tool Merge",
        window: "Parallel Branch Merge",
        subtitle: "feat/surface-backend-next",
      },
      {
        id: "phase-3",
        title: "Phase 3 · Personal macOS Shell Port",
        window: "Next Milestone",
        subtitle: "OS-level integration",
      },
    ],
    timelineSteps: [
      {
        id: "ts-1",
        title: "Ship Module Entity Cards & Before/After Approval Diffs",
        summary: "Enable rich Ops/Registry/Tracker cards and side-by-side approval diffs with callouts.",
        ref: "CW-20261122-0008",
        phaseId: "phase-1",
        status: "done",
        owner: "Frontend Agent",
      },
      {
        id: "ts-2",
        title: "Ship Visual Alignment Workbench, Drop Pad & Native Trackers UI",
        summary: "Interactive Architecture Flow, Bucket Triage, Timeline Planner, Drop Pad, and Trackers views.",
        ref: "CW-20261122-0009",
        phaseId: "phase-1",
        status: "in_progress",
        owner: "Frontend Agent",
      },
      {
        id: "ts-3",
        title: "Merge feat/surface-backend-next Go Modules & Verify Suite",
        summary: "Pull parallel backend commits for modules/align, modules/trackers, and kernel card resolvers.",
        ref: "CW-20261122-0011",
        phaseId: "phase-2",
        status: "planned",
        owner: "Backend + Frontend",
      },
      {
        id: "ts-4",
        title: "Adopt Surface Core in Personal macOS Basalisk App",
        summary: "Use example-host:/srv/data/backups/laptop-basalisk-snapshot-20261122 as reference for OS integration.",
        phaseId: "phase-3",
        status: "planned",
        owner: "Chrispian",
      },
    ],
  },
]

const SEED_DROPPADS: DropPadCollection[] = [
  {
    id: "dp-basalisk-migration",
    title: "Basalisk v1 → Surface ADE Reference Collection",
    summary: "Curated code paths, data snapshots, and architecture refs for migrating OS-level Basalisk features.",
    mode: "collection",
    operation: "reference",
    targetDir: "example-host:/srv/data/backups/laptop-basalisk-snapshot-20261122",
    updatedAt: "2026-11-22T21:18:00Z",
    items: [
      {
        id: "dpi-1",
        kind: "folder",
        pathOrRef: "example-host:/srv/data/backups/laptop-basalisk-snapshot-20261122/data",
        label: "Checkpointed Basalisk SQLite Databases (agent.db, basalisk.db, assist.db)",
        sizeOrMeta: "22 MB · 7 DBs",
        status: "staged",
      },
      {
        id: "dpi-2",
        kind: "file",
        pathOrRef: "example-host:/srv/data/backups/laptop-basalisk-snapshot-20261122/config/standing.md",
        label: "Standing Agent Instructions & Roster Config",
        sizeOrMeta: "4.1 KB",
        status: "staged",
      },
      {
        id: "dpi-3",
        kind: "ref",
        pathOrRef: "registry:project:surface",
        label: "Surface ADE Framework Registry Dossier",
        sizeOrMeta: "project/2",
        status: "staged",
      },
      {
        id: "dpi-4",
        kind: "ref",
        pathOrRef: "CW-20261122-0009",
        label: "Torque Task · Visual Alignment & Drop Pad Module",
        sizeOrMeta: "surface-next-be",
        status: "staged",
      },
    ],
  },
  {
    id: "dp-file-staging",
    title: "UI Assets & Design Specs Export Staging Pad",
    summary: "Stage files to copy or move into the shared agency artifacts inbox or target project folder.",
    mode: "file_ops",
    operation: "copy",
    targetDir: "/workspace/example/workspaces/drafts/surface-next",
    updatedAt: "2026-11-22T21:20:00Z",
    items: [
      {
        id: "dpi-5",
        kind: "file",
        pathOrRef: "/workspace/example/hollis-labs/apps/surface/docs/taste.md",
        label: "taste.md (Evolved Blade Runner Design Guide)",
        sizeOrMeta: "14 KB",
        destPath: "/workspace/example/workspaces/drafts/surface-next/taste.md",
        status: "staged",
      },
      {
        id: "dpi-6",
        kind: "file",
        pathOrRef: "/workspace/example/hollis-labs/apps/surface/registry/README.md",
        label: "README.md (@surface UI Registry Specification)",
        sizeOrMeta: "8.2 KB",
        destPath: "/workspace/example/workspaces/drafts/surface-next/registry-README.md",
        status: "staged",
      },
    ],
  },
]

function loadProposals(): AlignProposal[] {
  try {
    const raw = window.localStorage.getItem(STORAGE_PROPOSALS_KEY)
    if (!raw) return SEED_PROPOSALS
    const parsed = JSON.parse(raw) as AlignProposal[]
    return parsed.length > 0 ? parsed : SEED_PROPOSALS
  } catch {
    return SEED_PROPOSALS
  }
}

function saveProposals(list: AlignProposal[]) {
  try {
    window.localStorage.setItem(STORAGE_PROPOSALS_KEY, JSON.stringify(list))
  } catch {}
}

function loadDropPads(): DropPadCollection[] {
  try {
    const raw = window.localStorage.getItem(STORAGE_DROPPADS_KEY)
    if (!raw) return SEED_DROPPADS
    const parsed = JSON.parse(raw) as DropPadCollection[]
    return parsed.length > 0 ? parsed : SEED_DROPPADS
  } catch {
    return SEED_DROPPADS
  }
}

function saveDropPads(list: DropPadCollection[]) {
  try {
    window.localStorage.setItem(STORAGE_DROPPADS_KEY, JSON.stringify(list))
  } catch {}
}

function statusBadgeTone(status: ProposalStatus) {
  switch (status) {
    case "approved":
      return "border-success/40 bg-success/15 text-success"
    case "proposed":
      return "border-primary/40 bg-primary/15 text-primary"
    case "changes_requested":
      return "border-warning/40 bg-warning/15 text-warning"
    case "parked":
      return "border-border bg-muted/40 text-muted-foreground"
    default:
      return "border-border text-muted-foreground"
  }
}

/**
 * Full Visual Alignment & Drop Pad Workbench Window (`surface.align` / `align/main`).
 */
export function AlignWorkbench({ initialMode, win }: Partial<ViewProps> & { initialMode?: AlignMode } = {}) {
  const argMode =
    win?.args?.mode === "architecture" ||
    win?.args?.mode === "triage" ||
    win?.args?.mode === "timeline" ||
    win?.args?.mode === "droppad"
      ? (win.args.mode as AlignMode)
      : undefined
  const [proposals, setProposals] = useState<AlignProposal[]>(() => loadProposals())
  const [dropPads, setDropPads] = useState<DropPadCollection[]>(() => loadDropPads())
  const [mode, setMode] = useState<AlignMode>(argMode ?? initialMode ?? "architecture")
  const [selectedProposalId, setSelectedProposalId] = useState<string>(() => SEED_PROPOSALS[0].id)
  const [selectedPadId, setSelectedPadId] = useState<string>(() => SEED_DROPPADS[0].id)
  const [notice, setNotice] = useState<string>("")

  // Sync mode selection with matching proposal subtype
  const handleSelectMode = (nextMode: AlignMode) => {
    setMode(nextMode)
    if (nextMode !== "droppad") {
      const match = proposals.find((p) => p.subtype === nextMode)
      if (match) setSelectedProposalId(match.id)
    }
  }

  const activeProposal = useMemo(
    () =>
      proposals.find((p) => p.id === selectedProposalId) ?? proposals.find((p) => p.subtype === mode) ?? proposals[0],
    [proposals, selectedProposalId, mode],
  )

  const activePad = useMemo(
    () => dropPads.find((d) => d.id === selectedPadId) ?? dropPads[0],
    [dropPads, selectedPadId],
  )

  const updateProposal = (id: string, fn: (p: AlignProposal) => AlignProposal) => {
    setProposals((prev) => {
      const next = prev.map((p) => (p.id === id ? fn(p) : p))
      saveProposals(next)
      return next
    })
  }

  const updatePad = (id: string, fn: (d: DropPadCollection) => DropPadCollection) => {
    setDropPads((prev) => {
      const next = prev.map((d) => (d.id === id ? fn(d) : d))
      saveDropPads(next)
      return next
    })
  }

  const flash = (msg: string) => {
    setNotice(msg)
    window.setTimeout(() => setNotice(""), 3200)
  }

  // Architecture editor state
  const [newNodeLabel, setNewNodeLabel] = useState("")
  const [newNodeKind, setNewNodeKind] = useState<ArchNode["kind"]>("service")
  const [newNodeLane, setNewNodeLane] = useState<ArchNode["lane"]>("core")
  const [showDiffHighlight, setShowDiffHighlight] = useState(true)

  // Triage editor state
  const [draggedItemId, setDraggedItemId] = useState<string | null>(null)
  const [dragOverBucketId, setDragOverBucketId] = useState<string | null>(null)
  const [newTriageTitle, setNewTriageTitle] = useState("")
  const [newBucketTitle, setNewBucketTitle] = useState("")

  // Timeline editor state
  const [draggedStepId, setDraggedStepId] = useState<string | null>(null)
  const [dragOverPhaseId, setDragOverPhaseId] = useState<string | null>(null)
  const [newStepTitle, setNewStepTitle] = useState("")

  const startCardDrag = (e: DragEvent<HTMLElement>, payloadKey: string, id: string, title: string, tag?: string) => {
    e.stopPropagation()
    try {
      window.getSelection()?.removeAllRanges()
    } catch {}
    e.dataTransfer.effectAllowed = "move"
    e.dataTransfer.setData("text/plain", id)
    e.dataTransfer.setData(payloadKey, id)

    // Build an isolated, single-item drag ghost inside the visible viewport so macOS WebKit/Blink
    // always snapshots only this compact card pill and never falls back to the parent composite layer.
    const ghost = document.createElement("div")
    ghost.style.position = "fixed"
    ghost.style.top = `${Math.max(8, (e.clientY || 100) - 16)}px`
    ghost.style.left = `${Math.max(8, (e.clientX || 100) - 16)}px`
    ghost.style.zIndex = "99999"
    ghost.style.width = "240px"
    ghost.style.maxWidth = "240px"
    ghost.style.padding = "8px 10px"
    ghost.style.borderRadius = "5px"
    ghost.style.border = "1px solid rgba(229, 164, 99, 0.75)"
    ghost.style.background = "#121a1f"
    ghost.style.color = "#e8eef2"
    ghost.style.boxShadow = "0 8px 20px rgba(0,0,0,0.45)"
    ghost.style.fontFamily = "ui-monospace, SFMono-Regular, Menlo, monospace"
    ghost.style.fontSize = "11px"
    ghost.style.fontWeight = "600"
    ghost.style.lineHeight = "1.35"
    ghost.style.pointerEvents = "none"
    ghost.style.whiteSpace = "nowrap"
    ghost.style.overflow = "hidden"
    ghost.style.textOverflow = "ellipsis"
    ghost.style.contain = "paint layout"
    ghost.textContent = tag ? `[${tag}] ${title}` : title
    document.body.appendChild(ghost)
    e.dataTransfer.setDragImage(ghost, 16, 16)
    window.setTimeout(() => {
      if (ghost.parentNode) ghost.parentNode.removeChild(ghost)
    }, 0)
  }

  // Drop Pad state
  const [newDropPath, setNewDropPath] = useState("")
  const [newDropLabel, setNewDropLabel] = useState("")
  const [newPadTitle, setNewPadTitle] = useState("")
  const [dragOverPad, setDragOverPad] = useState(false)

  useEffect(() => {
    // Opportunistically fetch from /api/m/align/proposals if backend module is mounted
    fetch("/api/m/align/proposals")
      .then((r) => (r.ok ? r.json() : null))
      .then((data) => {
        if (Array.isArray(data) && data.length > 0) {
          setProposals(data as AlignProposal[])
        }
      })
      .catch(() => {})
  }, [])

  const decideProposal = (status: ProposalStatus) => {
    if (!activeProposal) return
    updateProposal(activeProposal.id, (p) => ({
      ...p,
      status,
      revision: p.revision + 1,
      updatedAt: new Date().toISOString(),
    }))
    flash(
      status === "approved"
        ? `✓ Approved & saved "${activeProposal.title}" (rev #${activeProposal.revision + 1}) as reference`
        : status === "parked"
          ? `Parked proposal "${activeProposal.title}"`
          : `Marked "${activeProposal.title}" for revision`,
    )
  }

  return (
    <div className="flex h-full min-h-0 min-w-0 flex-col overflow-x-hidden overscroll-x-none bg-background">
      {/* Invariant Top Subnav Bar */}
      <header className="flex h-11 shrink-0 items-center justify-between gap-2 border-b border-border/80 bg-card/40 px-3">
        <div className="flex min-w-0 items-center gap-2">
          <SparklesIcon className="size-4 shrink-0 text-primary" />
          <span className="font-semibold text-sm tracking-tight">Visual Alignment &amp; Drop Pad</span>
          <span className="text-border">/</span>
          <Badge variant="outline" className="h-5 px-2 font-mono text-[11px] text-primary">
            {mode === "architecture"
              ? "Architecture Flow"
              : mode === "triage"
                ? "Bucket Triage"
                : mode === "timeline"
                  ? "Phased Timeline"
                  : "Drop Pad"}
          </Badge>
          {notice && (
            <Badge variant="outline" className="border-success/40 bg-success/10 font-mono text-[10.5px] text-success">
              {notice}
            </Badge>
          )}
        </div>

        {/* Mode Switcher */}
        <div className="flex shrink-0 items-center gap-1 rounded-sm border border-border/75 bg-muted/30 p-0.5">
          <Button
            size="xs"
            variant={mode === "architecture" ? "secondary" : "ghost"}
            className="h-6 gap-1.5 px-2 font-mono text-[11px]"
            onClick={() => handleSelectMode("architecture")}
            title="Architecture Flow Diagram Co-Editor"
          >
            <NetworkIcon className="size-3.5" />
            Architecture
          </Button>
          <Button
            size="xs"
            variant={mode === "triage" ? "secondary" : "ghost"}
            className="h-6 gap-1.5 px-2 font-mono text-[11px]"
            onClick={() => handleSelectMode("triage")}
            title="Bucket Triage & Container Alignment"
          >
            <KanbanIcon className="size-3.5" />
            Bucket Triage
          </Button>
          <Button
            size="xs"
            variant={mode === "timeline" ? "secondary" : "ghost"}
            className="h-6 gap-1.5 px-2 font-mono text-[11px]"
            onClick={() => handleSelectMode("timeline")}
            title="Phased Timeline & Milestone Alignment"
          >
            <ClockIcon className="size-3.5" />
            Timeline
          </Button>
          <Button
            size="xs"
            variant={mode === "droppad" ? "secondary" : "ghost"}
            className="h-6 gap-1.5 px-2 font-mono text-[11px]"
            onClick={() => handleSelectMode("droppad")}
            title="Drop Pad — File & Object Collections and Move/Copy Staging"
          >
            <FolderInputIcon className="size-3.5" />
            Drop Pad
          </Button>
        </div>
      </header>

      {/* Main Content Canvas */}
      <div className="flex min-h-0 min-w-0 flex-1 flex-col gap-4 overflow-y-auto overflow-x-hidden overscroll-x-none p-4">
        {mode !== "droppad" && activeProposal && (
          <div className="flex flex-wrap items-center justify-between gap-3 rounded-md border border-border/80 bg-card/80 px-4 py-3">
            <div className="flex min-w-0 flex-1 flex-col gap-1">
              <div className="flex flex-wrap items-center gap-2">
                <Badge
                  variant="outline"
                  className={`font-mono text-[10px] uppercase ${statusBadgeTone(activeProposal.status)}`}
                >
                  {activeProposal.status.replace("_", " ")}
                </Badge>
                <span className="font-mono text-[11px] text-muted-foreground">
                  Rev #{activeProposal.revision} · Proposed by {activeProposal.author}
                </span>
              </div>
              <h2 className="font-bold text-base text-foreground tracking-tight">{activeProposal.title}</h2>
              <p className="text-muted-foreground text-xs">{activeProposal.summary}</p>
            </div>

            <div className="flex shrink-0 flex-wrap items-center gap-2">
              <Button
                size="sm"
                variant="outline"
                className="h-7 gap-1 text-xs"
                onClick={() => decideProposal("parked")}
              >
                <PauseCircleIcon className="size-3.5" />
                Park
              </Button>
              <Button
                size="sm"
                variant="outline"
                className="h-7 gap-1 text-xs"
                onClick={() => decideProposal("changes_requested")}
              >
                <GitBranchIcon className="size-3.5" />
                Save Draft Revision
              </Button>
              <Button
                size="sm"
                variant="default"
                className="h-7 gap-1 text-xs"
                onClick={() => decideProposal("approved")}
              >
                <CheckCircle2Icon className="size-3.5" />
                Approve &amp; Save Reference
              </Button>
            </div>
          </div>
        )}

        {/* ==================== MODE 1: ARCHITECTURE FLOW PROPOSAL ==================== */}
        {mode === "architecture" && activeProposal && (
          <div className="flex flex-col gap-4">
            {/* Controls to co-edit nodes & toggle diff highlights */}
            <div className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-border/70 bg-muted/20 px-3 py-2">
              <div className="flex flex-wrap items-center gap-2">
                <Input
                  value={newNodeLabel}
                  onChange={(e) => setNewNodeLabel(e.target.value)}
                  placeholder="Add component node (e.g. Gatecrasher Verifier)…"
                  className="h-7 w-56 text-xs"
                />
                <Select value={newNodeKind} onValueChange={(v) => v && setNewNodeKind(v as ArchNode["kind"])}>
                  <SelectTrigger size="sm" className="h-7 w-28 font-mono text-xs" aria-label="Node kind">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="service">service</SelectItem>
                    <SelectItem value="store">store</SelectItem>
                    <SelectItem value="agent">agent</SelectItem>
                    <SelectItem value="client">client</SelectItem>
                    <SelectItem value="queue">queue</SelectItem>
                  </SelectContent>
                </Select>
                <Select value={newNodeLane} onValueChange={(v) => v && setNewNodeLane(v as ArchNode["lane"])}>
                  <SelectTrigger size="sm" className="h-7 w-32 font-mono text-xs" aria-label="Architecture lane">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="client">Client Tier</SelectItem>
                    <SelectItem value="core">Core / Modules</SelectItem>
                    <SelectItem value="fabric">Agent OS Fabric</SelectItem>
                  </SelectContent>
                </Select>
                <Button
                  size="xs"
                  disabled={!newNodeLabel.trim()}
                  onClick={() => {
                    const node: ArchNode = {
                      id: `node-${Date.now()}`,
                      label: newNodeLabel.trim(),
                      kind: newNodeKind,
                      lane: newNodeLane,
                      subtitle: `${newNodeKind} · co-authored`,
                      diff: "added",
                    }
                    updateProposal(activeProposal.id, (p) => ({
                      ...p,
                      archNodes: [...(p.archNodes ?? []), node],
                    }))
                    setNewNodeLabel("")
                    flash(`Added architecture node "${node.label}"`)
                  }}
                >
                  <PlusIcon className="size-3" />
                  Add Node
                </Button>
              </div>

              <div className="flex items-center gap-2">
                <Button
                  size="xs"
                  variant={showDiffHighlight ? "secondary" : "outline"}
                  onClick={() => setShowDiffHighlight((v) => !v)}
                  className="font-mono text-[11px]"
                >
                  {showDiffHighlight ? "✓ Highlighting Proposed Diffs" : "Show Proposed Diffs"}
                </Button>
              </div>
            </div>

            {/* 3-Lane Interactive Visual Architecture Canvas */}
            <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
              {(
                [
                  { id: "client", title: "1. Client & ADE Surface Tier", subtitle: "React 19 · Windows · Cards" },
                  { id: "core", title: "2. Surface Kernel & Module Stores", subtitle: "Go Host · Cite Bus · SQLite" },
                  { id: "fabric", title: "3. Agent OS & Tether Fabric", subtitle: "MCP Gateway · Specialist Seats" },
                ] as const
              ).map((lane) => {
                const laneNodes = (activeProposal.archNodes ?? []).filter((n) => n.lane === lane.id)
                return (
                  <div
                    key={lane.id}
                    className="flex flex-col gap-3 rounded-lg border border-border/80 bg-card/45 p-3.5"
                  >
                    <div className="flex items-center justify-between border-b border-border/60 pb-2">
                      <div>
                        <h3 className="font-mono text-xs font-bold uppercase tracking-wider text-foreground">
                          {lane.title}
                        </h3>
                        <p className="font-mono text-[10.5px] text-muted-foreground">{lane.subtitle}</p>
                      </div>
                      <Badge variant="secondary" className="font-mono text-[10px]">
                        {laneNodes.length}
                      </Badge>
                    </div>

                    <div className="flex flex-col gap-2.5">
                      {laneNodes.map((node) => {
                        const outgoing = (activeProposal.archEdges ?? []).filter((e) => e.from === node.id)
                        const diffBorder =
                          showDiffHighlight && node.diff === "added"
                            ? "border-success/60 bg-success/10 shadow-[inset_2px_0_0_var(--color-success)]"
                            : showDiffHighlight && node.diff === "modified"
                              ? "border-primary/60 bg-primary/10 shadow-[inset_2px_0_0_var(--color-primary)]"
                              : "border-border/75 bg-card/80"

                        return (
                          <div
                            key={node.id}
                            className={`flex flex-col gap-1.5 rounded-md border p-3 transition-all ${diffBorder}`}
                          >
                            <div className="flex items-start justify-between gap-2">
                              <div className="flex min-w-0 flex-col">
                                <div className="flex flex-wrap items-center gap-1.5">
                                  <span className="font-semibold text-foreground text-xs">{node.label}</span>
                                  <Badge variant="outline" className="h-4 px-1 font-mono text-[9px] uppercase">
                                    {node.kind}
                                  </Badge>
                                  {showDiffHighlight && node.diff && node.diff !== "unchanged" && (
                                    <Badge
                                      variant="outline"
                                      className={`h-4 px-1 font-mono text-[9px] ${
                                        node.diff === "added"
                                          ? "border-success/40 bg-success/15 text-success"
                                          : "border-primary/40 bg-primary/15 text-primary"
                                      }`}
                                    >
                                      {node.diff === "added" ? "+ new" : "~ updated"}
                                    </Badge>
                                  )}
                                </div>
                                {node.subtitle && (
                                  <span className="font-mono text-[10.5px] text-muted-foreground">{node.subtitle}</span>
                                )}
                              </div>

                              <div className="flex shrink-0 items-center gap-0.5">
                                <Select
                                  value={node.lane}
                                  onValueChange={(v) =>
                                    v &&
                                    updateProposal(activeProposal.id, (p) => ({
                                      ...p,
                                      archNodes: (p.archNodes ?? []).map((n) =>
                                        n.id === node.id ? { ...n, lane: v as ArchNode["lane"], diff: "modified" } : n,
                                      ),
                                    }))
                                  }
                                >
                                  <SelectTrigger
                                    size="sm"
                                    className="h-5 w-20 px-1.5 font-mono text-[9.5px]"
                                    aria-label="Move node lane"
                                  >
                                    <SelectValue />
                                  </SelectTrigger>
                                  <SelectContent>
                                    <SelectItem value="client">client</SelectItem>
                                    <SelectItem value="core">core</SelectItem>
                                    <SelectItem value="fabric">fabric</SelectItem>
                                  </SelectContent>
                                </Select>
                                <Button
                                  size="icon-xs"
                                  variant="ghost"
                                  className="size-5 text-muted-foreground hover:text-destructive"
                                  onClick={() =>
                                    updateProposal(activeProposal.id, (p) => ({
                                      ...p,
                                      archNodes: (p.archNodes ?? []).filter((n) => n.id !== node.id),
                                      archEdges: (p.archEdges ?? []).filter(
                                        (e) => e.from !== node.id && e.to !== node.id,
                                      ),
                                    }))
                                  }
                                  title="Remove node"
                                  aria-label={`Remove ${node.label}`}
                                >
                                  <Trash2Icon className="size-3" />
                                </Button>
                              </div>
                            </div>

                            {node.note && (
                              <p className="rounded-sm border-l-2 border-primary/50 bg-background/60 px-2 py-1 text-[11px] text-foreground/90">
                                {node.note}
                              </p>
                            )}

                            {outgoing.length > 0 && (
                              <div className="flex flex-wrap items-center gap-1 border-t border-border/50 pt-1.5">
                                <span className="font-mono text-[9.5px] text-muted-foreground">Flows to:</span>
                                {outgoing.map((edge) => {
                                  const target = (activeProposal.archNodes ?? []).find((n) => n.id === edge.to)
                                  return (
                                    <Badge
                                      key={edge.id}
                                      variant="outline"
                                      className="h-4 gap-1 px-1.5 font-mono text-[9.5px] text-primary"
                                    >
                                      <ArrowRightIcon className="size-2.5" />
                                      {target?.label ?? edge.to}
                                      {edge.label ? ` (${edge.label})` : ""}
                                    </Badge>
                                  )
                                })}
                              </div>
                            )}
                          </div>
                        )
                      })}
                    </div>
                  </div>
                )
              })}
            </div>
          </div>
        )}

        {/* ==================== MODE 2: BUCKET TRIAGE & CONTAINER ALIGNMENT ==================== */}
        {mode === "triage" && activeProposal && (
          <div className="flex flex-col gap-4">
            {/* Add Item / Add Bucket Controls */}
            <div className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-border/70 bg-muted/20 px-3 py-2">
              <div className="flex flex-wrap items-center gap-2">
                <Input
                  value={newTriageTitle}
                  onChange={(e) => setNewTriageTitle(e.target.value)}
                  placeholder="Add item to triage pool…"
                  className="h-7 w-60 text-xs"
                />
                <Button
                  size="xs"
                  disabled={!newTriageTitle.trim()}
                  onClick={() => {
                    const item: TriageItem = {
                      id: `ti-${Date.now()}`,
                      title: newTriageTitle.trim(),
                      bucketId: "unassigned",
                      disposition: "active",
                    }
                    updateProposal(activeProposal.id, (p) => ({
                      ...p,
                      triageItems: [...(p.triageItems ?? []), item],
                    }))
                    setNewTriageTitle("")
                  }}
                >
                  <PlusIcon className="size-3" />
                  Add Item
                </Button>
              </div>

              <div className="flex items-center gap-2">
                <Input
                  value={newBucketTitle}
                  onChange={(e) => setNewBucketTitle(e.target.value)}
                  placeholder="New bucket container…"
                  className="h-7 w-48 text-xs"
                />
                <Button
                  size="xs"
                  variant="outline"
                  disabled={!newBucketTitle.trim()}
                  onClick={() => {
                    const b: TriageBucket = {
                      id: `bucket-${Date.now()}`,
                      title: newBucketTitle.trim(),
                      subtitle: "Custom target container",
                      tone: "cyan",
                    }
                    updateProposal(activeProposal.id, (p) => ({
                      ...p,
                      buckets: [...(p.buckets ?? []), b],
                    }))
                    setNewBucketTitle("")
                  }}
                >
                  <FolderPlusIcon className="size-3" />
                  Add Container
                </Button>
              </div>
            </div>

            {/* Drag-and-Drop Bucket Containers Grid */}
            <div className="grid grid-cols-1 gap-3.5 md:grid-cols-2 xl:grid-cols-4">
              {(activeProposal.buckets ?? []).map((bucket) => {
                const items = (activeProposal.triageItems ?? []).filter(
                  (i) => i.bucketId === bucket.id && i.disposition !== "removed",
                )
                return (
                  <div
                    key={bucket.id}
                    onDragOver={(e) => {
                      e.preventDefault()
                      e.dataTransfer.dropEffect = "move"
                      if (dragOverBucketId !== bucket.id) setDragOverBucketId(bucket.id)
                    }}
                    onDragLeave={() => {
                      if (dragOverBucketId === bucket.id) setDragOverBucketId(null)
                    }}
                    onDrop={(e) => {
                      e.preventDefault()
                      setDragOverBucketId(null)
                      const droppedId =
                        e.dataTransfer.getData("application/x-surface-triage-item") ||
                        e.dataTransfer.getData("text/plain") ||
                        draggedItemId
                      if (!droppedId) return
                      updateProposal(activeProposal.id, (p) => ({
                        ...p,
                        triageItems: (p.triageItems ?? []).map((it) =>
                          it.id === droppedId ? { ...it, bucketId: bucket.id } : it,
                        ),
                      }))
                      setDraggedItemId(null)
                    }}
                    className={`flex min-h-64 flex-col gap-2.5 rounded-lg border p-3 transition-colors ${
                      dragOverBucketId === bucket.id ? "border-primary/70 bg-primary/10" : "border-border/80 bg-card/45"
                    }`}
                  >
                    <div className="flex items-center justify-between border-b border-border/60 pb-2">
                      <div className="min-w-0">
                        <h3 className="truncate font-mono text-xs font-bold uppercase tracking-wider text-foreground">
                          {bucket.title}
                        </h3>
                        {bucket.subtitle && (
                          <p className="truncate font-mono text-[10.5px] text-muted-foreground">{bucket.subtitle}</p>
                        )}
                      </div>
                      <Badge variant="secondary" className="font-mono text-[10px]">
                        {items.length}
                      </Badge>
                    </div>

                    <div className="flex flex-1 flex-col gap-2">
                      {items.length === 0 ? (
                        <div className="flex flex-1 items-center justify-center rounded-md border border-dashed border-border/60 p-4 text-center font-mono text-[11px] text-muted-foreground">
                          Drag items here
                        </div>
                      ) : (
                        items.map((item) => {
                          const proposedBucket = (activeProposal.buckets ?? []).find(
                            (b) => b.id === item.proposedBucketId,
                          )
                          const isDragging = draggedItemId === item.id
                          return (
                            <div
                              key={item.id}
                              draggable
                              onDragStart={(e) => {
                                startCardDrag(e, "application/x-surface-triage-item", item.id, item.title, item.ref)
                                window.requestAnimationFrame(() => setDraggedItemId(item.id))
                              }}
                              onDragEnd={() => {
                                setDraggedItemId(null)
                                setDragOverBucketId(null)
                              }}
                              style={{ contain: "paint" }}
                              className={`relative isolate flex cursor-grab select-none flex-col gap-1.5 overflow-hidden rounded-md border p-2.5 transition-all active:cursor-grabbing ${
                                isDragging
                                  ? "border-primary/60 bg-primary/10 opacity-45"
                                  : item.disposition === "parked" || item.disposition === "deferred"
                                    ? "border-border/50 bg-muted/20 opacity-65"
                                    : "border-border/80 bg-card/85 hover:border-primary/50"
                              }`}
                            >
                              <div className="flex items-start justify-between gap-1.5">
                                <span className="font-semibold text-foreground text-xs leading-snug">{item.title}</span>
                                {item.disposition !== "active" && (
                                  <Badge variant="outline" className="h-4 shrink-0 px-1 font-mono text-[9px] uppercase">
                                    {item.disposition}
                                  </Badge>
                                )}
                              </div>

                              {item.summary && (
                                <p className="line-clamp-2 text-[11px] text-muted-foreground leading-snug">
                                  <CiteText text={item.summary} />
                                </p>
                              )}

                              {proposedBucket && item.bucketId !== proposedBucket.id && (
                                <div className="flex items-center justify-between rounded-sm border border-primary/30 bg-primary/10 px-1.5 py-0.5 text-[10px] text-primary">
                                  <span className="truncate font-mono">Agent proposed → {proposedBucket.title}</span>
                                  <Button
                                    size="xs"
                                    variant="ghost"
                                    className="h-4 px-1 font-mono text-[9.5px] text-primary"
                                    onClick={() =>
                                      updateProposal(activeProposal.id, (p) => ({
                                        ...p,
                                        triageItems: (p.triageItems ?? []).map((it) =>
                                          it.id === item.id ? { ...it, bucketId: proposedBucket.id } : it,
                                        ),
                                      }))
                                    }
                                  >
                                    Accept
                                  </Button>
                                </div>
                              )}

                              <div className="flex flex-wrap items-center justify-between gap-1 border-t border-border/50 pt-1.5">
                                <Select
                                  value={item.bucketId}
                                  onValueChange={(v) =>
                                    v &&
                                    updateProposal(activeProposal.id, (p) => ({
                                      ...p,
                                      triageItems: (p.triageItems ?? []).map((it) =>
                                        it.id === item.id ? { ...it, bucketId: String(v) } : it,
                                      ),
                                    }))
                                  }
                                >
                                  <SelectTrigger
                                    size="sm"
                                    className="h-5 w-28 px-1.5 font-mono text-[9.5px]"
                                    aria-label="Target bucket"
                                  >
                                    <SelectValue />
                                  </SelectTrigger>
                                  <SelectContent>
                                    {(activeProposal.buckets ?? []).map((b) => (
                                      <SelectItem key={b.id} value={b.id}>
                                        {b.title}
                                      </SelectItem>
                                    ))}
                                  </SelectContent>
                                </Select>

                                <div className="flex items-center gap-0.5">
                                  <Button
                                    size="icon-xs"
                                    variant="ghost"
                                    className="size-5 text-muted-foreground hover:text-warning"
                                    title={item.disposition === "deferred" ? "Restore active" : "Defer item"}
                                    onClick={() =>
                                      updateProposal(activeProposal.id, (p) => ({
                                        ...p,
                                        triageItems: (p.triageItems ?? []).map((it) =>
                                          it.id === item.id
                                            ? {
                                                ...it,
                                                disposition: it.disposition === "deferred" ? "active" : "deferred",
                                              }
                                            : it,
                                        ),
                                      }))
                                    }
                                  >
                                    <ClockIcon className="size-3" />
                                  </Button>
                                  <Button
                                    size="icon-xs"
                                    variant="ghost"
                                    className="size-5 text-muted-foreground hover:text-primary"
                                    title={item.disposition === "parked" ? "Restore active" : "Park item"}
                                    onClick={() =>
                                      updateProposal(activeProposal.id, (p) => ({
                                        ...p,
                                        triageItems: (p.triageItems ?? []).map((it) =>
                                          it.id === item.id
                                            ? { ...it, disposition: it.disposition === "parked" ? "active" : "parked" }
                                            : it,
                                        ),
                                      }))
                                    }
                                  >
                                    <ArchiveIcon className="size-3" />
                                  </Button>
                                  <Button
                                    size="icon-xs"
                                    variant="ghost"
                                    className="size-5 text-muted-foreground hover:text-destructive"
                                    title="Remove item"
                                    onClick={() =>
                                      updateProposal(activeProposal.id, (p) => ({
                                        ...p,
                                        triageItems: (p.triageItems ?? []).map((it) =>
                                          it.id === item.id ? { ...it, disposition: "removed" } : it,
                                        ),
                                      }))
                                    }
                                  >
                                    <XCircleIcon className="size-3" />
                                  </Button>
                                </div>
                              </div>
                            </div>
                          )
                        })
                      )}
                    </div>
                  </div>
                )
              })}
            </div>
          </div>
        )}

        {/* ==================== MODE 3: PHASED TIMELINE / ACTIVITY PLAN ==================== */}
        {mode === "timeline" && activeProposal && (
          <div className="flex flex-col gap-4">
            <div className="flex flex-wrap items-center gap-2 rounded-md border border-border/70 bg-muted/20 px-3 py-2">
              <Input
                value={newStepTitle}
                onChange={(e) => setNewStepTitle(e.target.value)}
                placeholder="Add milestone or plan step to Phase 1…"
                className="h-7 w-72 text-xs"
              />
              <Button
                size="xs"
                disabled={!newStepTitle.trim()}
                onClick={() => {
                  const firstPhase = activeProposal.phases?.[0]?.id ?? "phase-1"
                  const step: TimelineStep = {
                    id: `ts-${Date.now()}`,
                    title: newStepTitle.trim(),
                    summary: "Co-authored milestone step",
                    phaseId: firstPhase,
                    status: "planned",
                    owner: "Team",
                  }
                  updateProposal(activeProposal.id, (p) => ({
                    ...p,
                    timelineSteps: [...(p.timelineSteps ?? []), step],
                  }))
                  setNewStepTitle("")
                }}
              >
                <PlusIcon className="size-3" />
                Add Timeline Step
              </Button>
              <span className="ml-auto font-mono text-[11px] text-muted-foreground">
                Drag steps between phases or reorder with arrows
              </span>
            </div>

            <div className="flex flex-col gap-4">
              {(activeProposal.phases ?? []).map((phase, pIdx) => {
                const steps = (activeProposal.timelineSteps ?? []).filter((s) => s.phaseId === phase.id)
                return (
                  <div
                    key={phase.id}
                    onDragOver={(e) => {
                      e.preventDefault()
                      e.dataTransfer.dropEffect = "move"
                      if (dragOverPhaseId !== phase.id) setDragOverPhaseId(phase.id)
                    }}
                    onDragLeave={() => {
                      if (dragOverPhaseId === phase.id) setDragOverPhaseId(null)
                    }}
                    onDrop={(e) => {
                      e.preventDefault()
                      setDragOverPhaseId(null)
                      const droppedId =
                        e.dataTransfer.getData("application/x-surface-timeline-step") ||
                        e.dataTransfer.getData("text/plain") ||
                        draggedStepId
                      if (!droppedId) return
                      updateProposal(activeProposal.id, (p) => ({
                        ...p,
                        timelineSteps: (p.timelineSteps ?? []).map((s) =>
                          s.id === droppedId ? { ...s, phaseId: phase.id } : s,
                        ),
                      }))
                      setDraggedStepId(null)
                    }}
                    className={`relative flex flex-col gap-2.5 rounded-lg border p-4 transition-colors ${
                      dragOverPhaseId === phase.id ? "border-primary/70 bg-primary/10" : "border-border/80 bg-card/45"
                    }`}
                  >
                    <div className="flex flex-wrap items-center justify-between gap-2 border-b border-dashed border-border/70 pb-2.5">
                      <div className="flex items-center gap-2.5">
                        <span className="flex size-6 items-center justify-center rounded-full border border-primary/40 bg-primary/15 font-mono text-xs font-bold text-primary">
                          {pIdx + 1}
                        </span>
                        <div>
                          <h3 className="font-mono text-xs font-bold uppercase tracking-wider text-foreground">
                            {phase.title}
                          </h3>
                          <p className="font-mono text-[10.5px] text-muted-foreground">
                            {phase.window} · {phase.subtitle}
                          </p>
                        </div>
                      </div>
                      <Badge variant="secondary" className="font-mono text-[10px]">
                        {steps.length} steps
                      </Badge>
                    </div>

                    <div className="flex flex-col gap-2 pl-2">
                      {steps.map((step, sIdx) => (
                        <div
                          key={step.id}
                          draggable
                          onDragStart={(e) => {
                            startCardDrag(e, "application/x-surface-timeline-step", step.id, step.title, step.owner)
                            window.requestAnimationFrame(() => setDraggedStepId(step.id))
                          }}
                          onDragEnd={() => {
                            setDraggedStepId(null)
                            setDragOverPhaseId(null)
                          }}
                          style={{ contain: "paint" }}
                          className={`relative isolate flex cursor-grab select-none flex-wrap items-center justify-between gap-3 overflow-hidden rounded-md border px-3 py-2.5 transition-all active:cursor-grabbing ${
                            draggedStepId === step.id
                              ? "border-primary/60 bg-primary/10 opacity-45"
                              : "border-border/75 bg-card/85"
                          }`}
                        >
                          <div className="flex min-w-0 flex-1 items-start gap-2.5">
                            <span
                              className={`mt-1 size-2 shrink-0 rounded-full ${
                                step.status === "done"
                                  ? "bg-success"
                                  : step.status === "in_progress"
                                    ? "bg-primary shadow-[0_0_6px_var(--color-primary)]"
                                    : step.status === "deferred" || step.status === "parked"
                                      ? "bg-warning"
                                      : "bg-muted-foreground"
                              }`}
                            />
                            <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                              <div className="flex flex-wrap items-center gap-2">
                                <span className="font-semibold text-foreground text-xs">{step.title}</span>
                                {step.ref && (
                                  <span className="font-mono text-[10.5px] text-primary">
                                    <CiteText text={step.ref} />
                                  </span>
                                )}
                                {step.owner && (
                                  <Badge variant="outline" className="h-4 px-1.5 font-mono text-[9.5px]">
                                    {step.owner}
                                  </Badge>
                                )}
                              </div>
                              <p className="text-[11px] text-muted-foreground">
                                <CiteText text={step.summary} />
                              </p>
                            </div>
                          </div>

                          <div className="flex shrink-0 items-center gap-1">
                            <Select
                              value={step.status}
                              onValueChange={(v) =>
                                v &&
                                updateProposal(activeProposal.id, (p) => ({
                                  ...p,
                                  timelineSteps: (p.timelineSteps ?? []).map((s) =>
                                    s.id === step.id ? { ...s, status: v as TimelineStep["status"] } : s,
                                  ),
                                }))
                              }
                            >
                              <SelectTrigger
                                size="sm"
                                className="h-6 w-28 font-mono text-[10px]"
                                aria-label="Step status"
                              >
                                <SelectValue />
                              </SelectTrigger>
                              <SelectContent>
                                <SelectItem value="planned">planned</SelectItem>
                                <SelectItem value="in_progress">in_progress</SelectItem>
                                <SelectItem value="done">done</SelectItem>
                                <SelectItem value="deferred">deferred</SelectItem>
                                <SelectItem value="parked">parked</SelectItem>
                              </SelectContent>
                            </Select>

                            <Select
                              value={step.phaseId}
                              onValueChange={(v) =>
                                v &&
                                updateProposal(activeProposal.id, (p) => ({
                                  ...p,
                                  timelineSteps: (p.timelineSteps ?? []).map((s) =>
                                    s.id === step.id ? { ...s, phaseId: String(v) } : s,
                                  ),
                                }))
                              }
                            >
                              <SelectTrigger
                                size="sm"
                                className="h-6 w-28 font-mono text-[10px]"
                                aria-label="Move to phase"
                              >
                                <SelectValue />
                              </SelectTrigger>
                              <SelectContent>
                                {(activeProposal.phases ?? []).map((ph) => (
                                  <SelectItem key={ph.id} value={ph.id}>
                                    {ph.title.split("·")[0]}
                                  </SelectItem>
                                ))}
                              </SelectContent>
                            </Select>

                            <Button
                              size="icon-xs"
                              variant="ghost"
                              className="size-6"
                              disabled={sIdx === 0}
                              title="Move step up"
                              onClick={() =>
                                updateProposal(activeProposal.id, (p) => {
                                  const all = [...(p.timelineSteps ?? [])]
                                  const idx = all.findIndex((x) => x.id === step.id)
                                  if (idx > 0) {
                                    const tmp = all[idx - 1]
                                    all[idx - 1] = all[idx]
                                    all[idx] = tmp
                                  }
                                  return { ...p, timelineSteps: all }
                                })
                              }
                            >
                              <ArrowUpIcon className="size-3" />
                            </Button>
                            <Button
                              size="icon-xs"
                              variant="ghost"
                              className="size-6"
                              disabled={sIdx === steps.length - 1}
                              title="Move step down"
                              onClick={() =>
                                updateProposal(activeProposal.id, (p) => {
                                  const all = [...(p.timelineSteps ?? [])]
                                  const idx = all.findIndex((x) => x.id === step.id)
                                  if (idx >= 0 && idx < all.length - 1) {
                                    const tmp = all[idx + 1]
                                    all[idx + 1] = all[idx]
                                    all[idx] = tmp
                                  }
                                  return { ...p, timelineSteps: all }
                                })
                              }
                            >
                              <ArrowDownIcon className="size-3" />
                            </Button>
                          </div>
                        </div>
                      ))}
                    </div>
                  </div>
                )
              })}
            </div>
          </div>
        )}

        {/* ==================== MODE 4: DROP PAD (COLLECTIONS & FILE STAGING) ==================== */}
        {mode === "droppad" && activePad && (
          <div className="grid grid-cols-1 gap-4 lg:grid-cols-12">
            {/* Left Rail: Collections / Drop Pads List */}
            <div className="flex flex-col gap-3 rounded-lg border border-border/80 bg-card/40 p-3 lg:col-span-4">
              <div className="flex items-center justify-between border-b border-border/60 pb-2">
                <span className="font-mono text-xs font-bold uppercase tracking-wider text-foreground">
                  Drop Pads &amp; Collections ({dropPads.length})
                </span>
              </div>

              <div className="flex flex-col gap-1.5">
                {dropPads.map((pad) => (
                  <Button
                    key={pad.id}
                    variant="ghost"
                    onClick={() => setSelectedPadId(pad.id)}
                    className={`h-auto min-w-0 w-full flex-col items-start justify-start gap-1 overflow-hidden rounded-md border p-2.5 text-left whitespace-normal ${
                      pad.id === activePad.id
                        ? "border-primary/50 bg-primary/10 shadow-[inset_2px_0_0_var(--color-primary)]"
                        : "border-border/60 bg-background/40 hover:border-border"
                    }`}
                  >
                    <div className="flex min-w-0 w-full items-center justify-between gap-2">
                      <span className="min-w-0 flex-1 truncate font-semibold text-foreground text-xs">{pad.title}</span>
                      <Badge variant="outline" className="h-4 shrink-0 px-1 font-mono text-[9px] uppercase">
                        {pad.mode === "collection" ? "collection" : pad.operation}
                      </Badge>
                    </div>
                    <p className="w-full line-clamp-2 break-words font-normal text-[11px] text-muted-foreground leading-snug">
                      {pad.summary}
                    </p>
                    <span className="font-mono text-[10px] text-primary">{pad.items.length} items on pad</span>
                  </Button>
                ))}
              </div>

              {/* Create New Drop Pad */}
              <div className="mt-auto flex flex-col gap-1.5 border-t border-border/60 pt-2.5">
                <span className="font-mono text-[10px] text-muted-foreground uppercase">New Collection / Drop Pad</span>
                <div className="flex items-center gap-1.5">
                  <Input
                    value={newPadTitle}
                    onChange={(e) => setNewPadTitle(e.target.value)}
                    placeholder="Collection title…"
                    className="h-7 text-xs"
                  />
                  <Button
                    size="xs"
                    disabled={!newPadTitle.trim()}
                    onClick={() => {
                      const pad: DropPadCollection = {
                        id: `dp-${Date.now()}`,
                        title: newPadTitle.trim(),
                        summary: "Custom drag-and-drop reference & file staging collection.",
                        mode: "collection",
                        operation: "reference",
                        targetDir: "/workspace/example",
                        items: [],
                        updatedAt: new Date().toISOString(),
                      }
                      const next = [pad, ...dropPads]
                      setDropPads(next)
                      saveDropPads(next)
                      setSelectedPadId(pad.id)
                      setNewPadTitle("")
                    }}
                  >
                    <PlusIcon className="size-3" />
                  </Button>
                </div>
              </div>
            </div>

            {/* Right Area: Active Drop Pad Interactive Zone */}
            <div className="flex flex-col gap-3 rounded-lg border border-border/80 bg-card/45 p-4 lg:col-span-8">
              <div className="flex flex-wrap items-start justify-between gap-3 border-b border-border/60 pb-3">
                <div>
                  <div className="flex items-center gap-2">
                    <h2 className="font-bold text-base text-foreground">{activePad.title}</h2>
                    <Badge variant="outline" className="font-mono text-[10px] uppercase">
                      droppad:{activePad.id}
                    </Badge>
                  </div>
                  <p className="text-muted-foreground text-xs">{activePad.summary}</p>
                </div>

                <div className="flex flex-wrap items-center gap-1.5">
                  <Select
                    value={activePad.operation}
                    onValueChange={(v) =>
                      v &&
                      updatePad(activePad.id, (d) => ({
                        ...d,
                        operation: v as DropPadCollection["operation"],
                        mode: v === "reference" ? "collection" : "file_ops",
                      }))
                    }
                  >
                    <SelectTrigger size="sm" className="h-7 w-40 font-mono text-xs" aria-label="Drop pad mode">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="reference">Collection (Reference)</SelectItem>
                      <SelectItem value="copy">Stage File Copy</SelectItem>
                      <SelectItem value="move">Stage File Move</SelectItem>
                    </SelectContent>
                  </Select>

                  <Button
                    size="sm"
                    variant="outline"
                    className="h-7 gap-1 text-xs"
                    onClick={() => {
                      const bundle = [
                        `# Drop Pad Collection: ${activePad.title} (droppad:${activePad.id})`,
                        `Mode: ${activePad.operation} | Target: ${activePad.targetDir}`,
                        ...activePad.items.map((it) => `- [${it.kind}] ${it.label} (${it.pathOrRef})`),
                      ].join("\n")
                      void navigator.clipboard?.writeText(bundle)
                      flash("✓ Copied Drop Pad collection bundle to clipboard for agent context")
                    }}
                  >
                    <CopyIcon className="size-3.5" />
                    Copy Context for Agent
                  </Button>

                  {activePad.operation !== "reference" && (
                    <Button
                      size="sm"
                      variant="default"
                      className="h-7 gap-1 text-xs"
                      onClick={() => {
                        updatePad(activePad.id, (d) => ({
                          ...d,
                          items: d.items.map((it) => ({
                            ...it,
                            status: d.operation === "move" ? "moved" : "copied",
                          })),
                        }))
                        flash(
                          `✓ Executed staged ${activePad.operation} (${activePad.items.length} items → ${activePad.targetDir})`,
                        )
                      }}
                    >
                      <SendIcon className="size-3.5" />
                      Execute {activePad.operation === "move" ? "Move" : "Copy"}
                    </Button>
                  )}
                </div>
              </div>

              {/* Target Destination Directory Bar */}
              <div className="flex flex-wrap items-center gap-2 rounded-md border border-border/70 bg-muted/20 px-3 py-2 text-xs">
                <span className="font-mono text-[10.5px] font-semibold text-muted-foreground uppercase">
                  {activePad.operation === "reference" ? "Collection Scope / Root:" : "Target Destination Folder:"}
                </span>
                <Input
                  value={activePad.targetDir}
                  onChange={(e) => updatePad(activePad.id, (d) => ({ ...d, targetDir: e.target.value }))}
                  className="h-7 flex-1 font-mono text-xs"
                />
              </div>

              {/* Interactive Drag-and-Drop Pad Target Zone */}
              <div
                onDragOver={(e: DragEvent) => {
                  e.preventDefault()
                  if (!dragOverPad) setDragOverPad(true)
                }}
                onDragLeave={() => setDragOverPad(false)}
                onDrop={(e: DragEvent) => {
                  e.preventDefault()
                  setDragOverPad(false)
                  const droppedFiles = Array.from(e.dataTransfer.files)
                  const citeJson = e.dataTransfer.getData("application/x-surface-cite")
                  const textData = e.dataTransfer.getData("text/plain")
                  const newItems: DropPadItem[] = []

                  const classifyRefOrPath = (rawInput: string): { kind: DropPadItem["kind"]; token: string } => {
                    const trimmed = rawInput.trim().replace(/^\[|\]$/g, "")
                    if (
                      /^(cite:|registry:|ops:|align:|droppad:|tracker:|tracker\.item:|work:|frag:|caster:|tether:|tesseract:|CW-\d|PRJ-\d|EPIC-\d)/i.test(
                        trimmed,
                      )
                    ) {
                      return { kind: "ref", token: trimmed.replace(/^cite:/i, "") }
                    }
                    if (trimmed.startsWith("/") || trimmed.startsWith("~/") || trimmed.includes("/")) {
                      return { kind: "file", token: trimmed }
                    }
                    if (/^[a-z][a-z0-9._-]*:[A-Za-z0-9]/.test(trimmed)) {
                      return { kind: "ref", token: trimmed }
                    }
                    return { kind: trimmed.includes(".") ? "file" : "ref", token: trimmed }
                  }

                  if (citeJson) {
                    try {
                      const parsed = JSON.parse(citeJson) as {
                        kind?: string
                        canonical?: string
                        raw?: string
                        label?: string
                      }
                      const token = (parsed.raw || parsed.canonical || "").trim()
                      if (token) {
                        newItems.push({
                          id: `dpi-${Date.now()}`,
                          kind: "ref",
                          pathOrRef: token,
                          label: parsed.label || token,
                          sizeOrMeta: parsed.kind || "cite ref",
                          destPath: `${activePad.targetDir.replace(/\/$/, "")}/${token.split("/").pop() || token}`,
                          status: "staged",
                        })
                      }
                    } catch {}
                  }

                  for (const f of droppedFiles) {
                    newItems.push({
                      id: `dpi-${Date.now()}-${f.name}`,
                      kind: "file",
                      pathOrRef: f.name,
                      label: f.name,
                      sizeOrMeta: `${Math.max(1, Math.round(f.size / 1024))} KB`,
                      destPath: `${activePad.targetDir.replace(/\/$/, "")}/${f.name}`,
                      status: "staged",
                    })
                  }
                  if (newItems.length === 0 && textData.trim()) {
                    const { kind, token } = classifyRefOrPath(textData)
                    const base = token.split("/").pop() || token
                    newItems.push({
                      id: `dpi-${Date.now()}`,
                      kind,
                      pathOrRef: token,
                      label: base,
                      sizeOrMeta: kind === "ref" ? "cite ref" : undefined,
                      destPath: `${activePad.targetDir.replace(/\/$/, "")}/${base}`,
                      status: "staged",
                    })
                  }
                  if (newItems.length > 0) {
                    updatePad(activePad.id, (d) => ({ ...d, items: [...d.items, ...newItems] }))
                    flash(`Added ${newItems.length} item(s) to Drop Pad`)
                  }
                }}
                className={`flex flex-col items-center justify-center gap-2 rounded-lg border-2 border-dashed p-5 text-center transition-all ${
                  dragOverPad
                    ? "border-primary bg-primary/15"
                    : "border-primary/35 bg-primary/5 hover:border-primary/55"
                }`}
              >
                <FolderInputIcon className="size-6 text-primary" />
                <div>
                  <p className="font-semibold text-foreground text-xs">
                    Drop Files, Folders, or Surface References Here
                  </p>
                  <p className="text-[11px] text-muted-foreground">
                    Drag files from Finder/Files or add a file path / Cite reference (e.g.{" "}
                    <code className="font-mono">registry:project:surface</code>,{" "}
                    <code className="font-mono">ops:...</code>, <code className="font-mono">/path/to/file</code>) below
                  </p>
                </div>
                <div className="mt-1 flex w-full max-w-xl flex-wrap items-center gap-1.5">
                  <Input
                    value={newDropPath}
                    onChange={(e) => setNewDropPath(e.target.value)}
                    placeholder="File path or ref (e.g. /workspace/example/... or registry:project:surface)"
                    className="h-7 flex-1 font-mono text-xs"
                  />
                  <Input
                    value={newDropLabel}
                    onChange={(e) => setNewDropLabel(e.target.value)}
                    placeholder="Label (optional)"
                    className="h-7 w-44 text-xs"
                  />
                  <Button
                    size="xs"
                    disabled={!newDropPath.trim()}
                    onClick={() => {
                      const raw = newDropPath.trim().replace(/^\[|\]$/g, "")
                      const isRef =
                        /^(cite:|registry:|ops:|align:|droppad:|tracker:|tracker\.item:|work:|frag:|caster:|tether:|tesseract:|CW-\d|PRJ-\d|EPIC-\d)/i.test(
                          raw,
                        ) ||
                        (!raw.startsWith("/") && !raw.startsWith("~/") && /^[a-z][a-z0-9._-]*:[A-Za-z0-9]/.test(raw))
                      const p = raw.replace(/^cite:/i, "")
                      const base = p.split("/").pop() || p
                      const it: DropPadItem = {
                        id: `dpi-${Date.now()}`,
                        kind: isRef ? "ref" : p.startsWith("/") || p.includes(".") ? "file" : "ref",
                        pathOrRef: p,
                        label: newDropLabel.trim() || base,
                        sizeOrMeta: isRef ? "cite ref" : undefined,
                        destPath: `${activePad.targetDir.replace(/\/$/, "")}/${base}`,
                        status: "staged",
                      }
                      updatePad(activePad.id, (d) => ({ ...d, items: [...d.items, it] }))
                      setNewDropPath("")
                      setNewDropLabel("")
                    }}
                  >
                    <PlusIcon className="size-3" />
                    Add to Pad
                  </Button>
                </div>
              </div>

              {/* Staged Items List */}
              <div className="flex flex-col gap-1.5">
                {activePad.items.map((item) => (
                  <div
                    key={item.id}
                    draggable
                    onDragStart={(e) =>
                      startCardDrag(e, "application/x-surface-droppad-item", item.pathOrRef, item.label, item.kind)
                    }
                    style={{ contain: "paint" }}
                    className="relative isolate flex cursor-grab select-none flex-wrap items-center justify-between gap-2 overflow-hidden rounded-md border border-border/75 bg-card/80 px-3 py-2 text-xs active:cursor-grabbing"
                  >
                    <div className="flex min-w-0 flex-1 items-center gap-2.5">
                      <FileIcon className="size-4 shrink-0 text-primary" />
                      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                        <div className="flex flex-wrap items-center gap-1.5">
                          <span className="truncate font-semibold text-foreground">{item.label}</span>
                          <Badge variant="outline" className="h-4 px-1 font-mono text-[9px] uppercase">
                            {item.kind}
                          </Badge>
                          {item.sizeOrMeta && (
                            <span className="font-mono text-[10px] text-muted-foreground">{item.sizeOrMeta}</span>
                          )}
                        </div>
                        <div className="flex flex-wrap items-center gap-1.5 font-mono text-[10.5px] text-muted-foreground">
                          <span className="min-w-0 truncate">
                            <CiteText text={item.pathOrRef} />
                          </span>
                          {activePad.operation !== "reference" && item.destPath && (
                            <>
                              <ArrowRightIcon className="size-3 shrink-0 text-primary" />
                              <span className="truncate text-primary">{item.destPath}</span>
                            </>
                          )}
                        </div>
                      </div>
                    </div>

                    <div className="flex shrink-0 items-center gap-1.5">
                      <Badge
                        variant="outline"
                        className={`h-4 px-1.5 font-mono text-[9.5px] ${
                          item.status !== "staged"
                            ? "border-success/40 bg-success/10 text-success"
                            : "text-muted-foreground"
                        }`}
                      >
                        {item.status}
                      </Badge>
                      <Button
                        size="icon-xs"
                        variant="ghost"
                        className="size-6 text-muted-foreground hover:text-destructive"
                        onClick={() =>
                          updatePad(activePad.id, (d) => ({
                            ...d,
                            items: d.items.filter((x) => x.id !== item.id),
                          }))
                        }
                        title="Remove from Drop Pad"
                      >
                        <Trash2Icon className="size-3.5" />
                      </Button>
                    </div>
                  </div>
                ))}
              </div>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}

/**
 * Interactive Alignment Proposal Card Renderer (`kind: "align"` / `"proposal"`).
 * Renders a concise visual preview of an Architecture, Triage, or Timeline proposal inside chat / Cards
 * with 1-click "Open Visual Workbench" and Approve / Request Changes actions.
 */
export function AlignProposalCardRenderer({ card, respond: send }: CardRendererProps) {
  const payload = (card.payload ?? {}) as Record<string, unknown>
  const state = classifyPriorResponse(card.status).kind
  const open = state === "open" || state === "pending"
  const subtype =
    payload.subtype === "triage" || payload.subtype === "timeline" || payload.subtype === "droppad"
      ? (payload.subtype as AlignMode)
      : "architecture"

  const summary =
    typeof payload.summary === "string"
      ? payload.summary
      : "Visual alignment proposal ready for co-editing and approval."

  return (
    <CardFrame
      data-slot="card-align-proposal"
      title={card.title || "Visual Alignment Proposal"}
      meta={
        <>
          <Badge variant="outline" className="border-primary/40 bg-primary/10 font-mono text-[10px] text-primary">
            {subtype}
          </Badge>
          <CardStatus state={state} />
        </>
      }
      footer={
        <CardFoot state={state} allowOther onRespond={send}>
          <Button
            size="sm"
            variant="outline"
            onClick={() =>
              void desk.open("surface.workflows", {
                title: "Visual Alignment & Drop Pad",
                args: { tab: subtype === "droppad" ? "droppad" : "align", mode: subtype },
              })
            }
          >
            <LayersIcon className="size-3.5" />
            Open Visual Workbench
          </Button>
          <Button
            size="sm"
            variant="default"
            disabled={!open}
            onClick={() => void send({ status: "submitted", decisions: [{ itemId: card.id, action: "approve" }] })}
          >
            <CheckCircle2Icon className="size-3.5" />
            Approve Proposal
          </Button>
        </CardFoot>
      }
    >
      <CardBody className="text-foreground">{summary}</CardBody>
    </CardFrame>
  )
}
