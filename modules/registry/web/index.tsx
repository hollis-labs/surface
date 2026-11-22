/**
 * Surface Registry — Operational Information System, System Wiki & Knowledge Graph.
 *
 * Adapted with the Operations dashboard's Evolved Blade Runner aesthetic:
 * - Synthesizes Registry records, relationships, and cross-system correlations (Ops workstreams,
 *   Torque projects/tasks, Cerberus fleet hosts, Tether agents, and Caster docs) into an
 *   information service and wiki dossier.
 * - Create and edit flows are tucked behind icon actions (`plus` and `pencil`), favoring
 *   in-place inline editing over heavy CRUD forms.
 * - Fields/Payload (`sliders-horizontal`), Relationships (`git-fork`), Schema (`braces`),
 *   and History/Governance (`history` / `shield-check`) are accessed via icon actions.
 * - Related objects and correlation links support interactive search-and-select linking.
 */
import { CiteText, Icon, type ModuleWeb, ObjectActionsMenu, type ViewProps } from "@hollis-labs/surface-page/sdk"
import { useCallback, useEffect, useMemo, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty"
import { Input } from "@/components/ui/input"
import { Markdown } from "@/components/ui/markdown"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Spinner } from "@/components/ui/spinner"
import { Textarea } from "@/components/ui/textarea"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { KnowledgeGraphCanvas, RegistryKindLineArt } from "./emblems"
import {
  ago,
  CreateRecordModal,
  FieldsInspectorModal,
  HistoryAndGovernanceModal,
  SchemaInspectorModal,
  tone,
} from "./inspectors"
import {
  buildEnrichedDetail,
  enrichOverview,
  enrichRecordsPage,
  formatHumanObjectName,
  humanizePredicate,
  inverseFor,
  loadLocalOverrides,
  SEED_KNOWLEDGE,
  saveLocalOverrides,
  synthesizeWikiData,
} from "./overlay"
import { getPredicateCategory, RelationshipLinkerModal } from "./relationship-picker"
import {
  type Brief,
  type CorrelationBinding,
  type Detail,
  DOMAIN_CLUSTERS,
  type Edge,
  getKindMeta,
  type LocalRecordOverride,
  type Overview,
  type Page,
  type Pub,
  type RegistryViewMode,
} from "./types"

const q = encodeURIComponent
const REFRESH_MS = 30_000
const ANY = "*any*"
const LIFECYCLES = ["active", "experimental", "deprecated", "retired", "archived"]

/**
 * Human-readable clickable object chip that never overflows its container.
 * Always shows the human-readable name (e.g. "Hollis Labs", "Surface Lead", "Agency Orchestration Architecture")
 * with the canonical URN in the hover tooltip.
 */
function ObjectLinkChip({
  id,
  catalogById,
  onNavigate,
}: {
  id: string
  catalogById?: Map<string, Brief>
  onNavigate?: (id: string) => void
}) {
  const cleanId = id.trim().replace(/^registry:/, "")
  const kind = cleanId.includes(":") ? cleanId.split(":")[0] : ""
  const humanName = formatHumanObjectName(cleanId, catalogById)
  const isNavigableRegistryKind =
    /^(project|tool|agent|agency|skill|document|workstream|correlation|service|config|knowledge|mcp_server|launch_profile|host):/.test(
      cleanId,
    )

  if (isNavigableRegistryKind && onNavigate) {
    const meta = getKindMeta(kind)
    return (
      <Button
        size="xs"
        variant="ghost"
        onClick={() => onNavigate(cleanId)}
        title={`${humanName} (registry:${cleanId}) — Click to open record`}
        className="h-5 max-w-full min-w-0 justify-start gap-1 rounded border border-border/60 bg-background/60 px-1.5 py-0 text-xs font-medium text-foreground hover:border-cyan-500/50 hover:bg-cyan-500/10 hover:text-cyan-200"
      >
        <span
          className={`size-1.5 shrink-0 rounded-full ${meta.tone.includes("cyan") ? "bg-cyan-400" : meta.tone.includes("emerald") ? "bg-emerald-400" : meta.tone.includes("amber") ? "bg-amber-400" : "bg-violet-400"}`}
        />
        <span className="min-w-0 truncate">{humanName}</span>
      </Button>
    )
  }

  return (
    <span
      className="inline-block max-w-full min-w-0 truncate rounded border border-border/50 bg-background/40 px-1.5 py-0.5 text-xs font-medium text-foreground"
      title={id}
    >
      {humanName}
    </span>
  )
}

/** Left Knowledge Explorer Rail: Domain clusters, kind pills, and scannable record list. */
function KnowledgeExplorerRail({
  overview,
  kind,
  selectedId,
  records,
  loadingRecords,
  search,
  onSearchChange,
  lifecycle,
  onLifecycleChange,
  onSelectKind,
  onSelectRecord,
  onOpenSchema,
  catalogById,
}: {
  overview: Overview
  kind: string
  selectedId: string
  records: Brief[]
  loadingRecords: boolean
  search: string
  onSearchChange: (v: string) => void
  lifecycle: string
  onLifecycleChange: (v: string) => void
  onSelectKind: (k: string) => void
  onSelectRecord: (b: Brief) => void
  onOpenSchema: (k: string) => void
  catalogById: Map<string, Brief>
}) {
  const [domainsCollapsed, setDomainsCollapsed] = useState(false)
  const countByKind = useMemo(() => {
    const m = new Map<string, number>()
    for (const k of overview.kinds) m.set(k.kind, k.count)
    return m
  }, [overview.kinds])

  return (
    <aside className="flex h-full min-h-0 w-72 shrink-0 flex-col overflow-hidden border-r border-border/70 bg-card/20">
      {/* Domain Ontology Clusters (Scrollable so all 4 clusters are always accessible) */}
      <div className="flex shrink-0 flex-col gap-1.5 border-b border-border/70 p-2">
        <div className="flex items-center justify-between px-1">
          <Button
            size="xs"
            variant="ghost"
            className="h-5 gap-1 px-1 font-mono text-[10px] font-semibold tracking-wider text-muted-foreground uppercase hover:text-foreground"
            onClick={() => setDomainsCollapsed((c) => !c)}
            title={domainsCollapsed ? "Expand Knowledge Domains" : "Collapse Knowledge Domains"}
          >
            <Icon name={domainsCollapsed ? "chevron-right" : "chevron-down"} className="size-3" />
            Knowledge Domains ({DOMAIN_CLUSTERS.length})
          </Button>
          <Button
            size="xs"
            variant={kind === "" ? "secondary" : "ghost"}
            className="h-5 px-2 font-mono text-[10px]"
            onClick={() => onSelectKind("")}
          >
            All ({overview.total})
          </Button>
        </div>

        {!domainsCollapsed ? (
          <div className="flex max-h-60 flex-col gap-1.5 overflow-y-auto overscroll-y-contain pr-0.5">
            {DOMAIN_CLUSTERS.map((cluster) => (
              <div
                key={cluster.id}
                className="flex shrink-0 flex-col gap-1 rounded-md border border-border/60 bg-background/50 p-1.5"
              >
                <div className="flex items-center gap-1.5 px-0.5">
                  <Icon name={cluster.icon} className="size-3 shrink-0 text-muted-foreground" />
                  <span className="truncate text-[11px] font-semibold text-foreground">{cluster.title}</span>
                </div>
                <div className="flex flex-wrap gap-1">
                  {cluster.kinds.map((k) => {
                    const count = countByKind.get(k) ?? 0
                    const active = kind === k
                    const meta = getKindMeta(k)
                    return (
                      <Button
                        key={k}
                        size="xs"
                        variant={active ? "secondary" : "ghost"}
                        className={`h-5 gap-1 px-1.5 font-mono text-[10px] ${
                          active ? "border border-cyan-500/40 bg-cyan-500/15 text-cyan-200" : "text-muted-foreground"
                        }`}
                        onClick={() => onSelectKind(active ? "" : k)}
                        title={`${meta.plural}: ${meta.description}`}
                      >
                        <span>{meta.label}</span>
                        <span className="rounded bg-muted/60 px-1 text-[9px]">{count}</span>
                      </Button>
                    )
                  })}
                </div>
              </div>
            ))}
          </div>
        ) : null}
      </div>

      {/* Record Filter & Search */}
      <div className="flex shrink-0 flex-col gap-1.5 border-b border-border/70 p-2">
        <div className="flex items-center justify-between">
          <span className="font-mono text-[11px] font-semibold text-foreground uppercase">
            {kind ? getKindMeta(kind).plural : "Catalog Index"} ({records.length})
          </span>
          {kind ? (
            <Button
              size="xs"
              variant="ghost"
              className="h-5 px-1.5 text-[11px] text-muted-foreground hover:text-cyan-300"
              onClick={() => onOpenSchema(kind)}
              title={`View ${kind} schema & ontology`}
            >
              <Icon name="braces" className="size-3" />
            </Button>
          ) : null}
        </div>
        <div className="flex items-center gap-1">
          <Input
            value={search}
            onChange={(e) => onSearchChange(e.target.value)}
            placeholder={kind ? `Filter ${getKindMeta(kind).plural.toLowerCase()}…` : "Filter catalog…"}
            className="h-7 text-xs"
            aria-label="Filter records"
          />
          <Select value={lifecycle} onValueChange={(v) => onLifecycleChange(String(v ?? ANY))}>
            <SelectTrigger size="sm" aria-label="Status filter" className="h-7 w-24 shrink-0 text-[11px]">
              <SelectValue>{(v: string) => (v === ANY ? "Status" : v)}</SelectValue>
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ANY}>Any status</SelectItem>
              {LIFECYCLES.map((l) => (
                <SelectItem key={l} value={l}>
                  {l}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </div>

      {/* Scannable Record List */}
      <div className="min-h-0 flex-1 overflow-y-auto overflow-x-hidden overscroll-y-contain p-1.5">
        {loadingRecords && records.length === 0 ? (
          <div className="grid h-24 place-items-center">
            <Spinner />
          </div>
        ) : records.length === 0 ? (
          <div className="flex h-32 flex-col items-center justify-center gap-1 p-3 text-center text-xs text-muted-foreground">
            <span>No matching records in this filter.</span>
            {kind || search || lifecycle !== ANY ? (
              <Button
                size="xs"
                variant="outline"
                onClick={() => {
                  onSelectKind("")
                  onSearchChange("")
                  onLifecycleChange(ANY)
                }}
              >
                Reset filters
              </Button>
            ) : null}
          </div>
        ) : (
          <div className="flex flex-col gap-1">
            {records.map((r) => {
              const selected = r.id === selectedId
              const meta = getKindMeta(r.kind)
              const displayTitle = formatHumanObjectName(r.id, catalogById)
              const ownerHuman = r.owner ? formatHumanObjectName(r.owner, catalogById) : ""
              const dotColor =
                r.lifecycle === "active"
                  ? "bg-emerald-400"
                  : r.lifecycle === "experimental"
                    ? "bg-amber-400"
                    : r.lifecycle === "deprecated" || r.lifecycle === "retired"
                      ? "bg-rose-400"
                      : "bg-slate-400"
              return (
                <Button
                  key={r.id}
                  variant="ghost"
                  onClick={() => onSelectRecord(r)}
                  className={`h-auto w-full flex-col items-start justify-start gap-1 overflow-hidden rounded-md border px-2.5 py-2 text-left transition-all ${
                    selected
                      ? "border-cyan-500/50 bg-cyan-500/12 shadow-[inset_2px_0_0_#22d3ee]"
                      : "border-transparent hover:border-border/60 hover:bg-muted/30"
                  }`}
                >
                  <div className="flex w-full min-w-0 items-center justify-between gap-1.5">
                    <div className="flex min-w-0 items-center gap-1.5">
                      <span className={`size-1.5 shrink-0 rounded-full ${dotColor}`} />
                      <span className="truncate text-xs font-semibold text-foreground">{displayTitle}</span>
                    </div>
                    <div className="flex shrink-0 items-center gap-1">
                      {r.isLocalOverlay ? (
                        <span className="size-1.5 rounded-full bg-cyan-400" title="Modified in local overlay" />
                      ) : null}
                      {r.edges > 0 ? (
                        <span className="rounded bg-muted/70 px-1 font-mono text-[10px] text-muted-foreground">
                          {r.edges}
                        </span>
                      ) : null}
                    </div>
                  </div>
                  <div className="flex w-full min-w-0 items-center justify-between gap-1 text-[10px] text-muted-foreground">
                    <span className="truncate">{r.summary || r.id}</span>
                    {!kind ? (
                      <span className={`shrink-0 rounded border px-1 py-0 font-mono text-[9px] ${meta.tone}`}>
                        {meta.label}
                      </span>
                    ) : ownerHuman ? (
                      <span className="max-w-24 shrink-0 truncate text-[10px]">{ownerHuman}</span>
                    ) : null}
                  </div>
                </Button>
              )
            })}
          </div>
        )}
      </div>
    </aside>
  )
}

/**
 * Primary Wiki & Operational Dossier View (`w-[92%] max-w-6xl mx-auto`, unboxed layout).
 * Supports direct inline editing (toggled by the `pencil` icon or per-section edit icon),
 * search-and-select relationship linking, and icon-gated inspectors.
 */
function WikiDossierView({
  detail,
  overrides,
  catalogById,
  inlineEditMode,
  canGoBack,
  backTargetName,
  onGoBack,
  onToggleInlineEdit,
  onSaveInlineBriefAndPayload,
  onSaveWikiSection,
  onOpenRelationshipsModal,
  onOpenFieldsModal,
  onOpenSchemaModal,
  onOpenHistoryModal,
  onRemoveEdge,
  onRemoveCorrelation,
  onSelectRecordId,
  onSwitchToGraph,
}: {
  detail: Detail
  overrides: Record<string, LocalRecordOverride>
  catalogById: Map<string, Brief>
  inlineEditMode: boolean
  canGoBack: boolean
  backTargetName?: string
  onGoBack: () => void
  onToggleInlineEdit: () => void
  onSaveInlineBriefAndPayload: (patch: {
    name?: string
    summary?: string
    lifecycle?: string
    owner?: string
    operator?: string
    repo?: string
    torqueProjectId?: string
  }) => void
  onSaveWikiSection: (section: "overviewMd" | "architectureMd" | "runbookMd", content: string) => void
  onOpenRelationshipsModal: (predicate?: string, targetKind?: string) => void
  onOpenFieldsModal: () => void
  onOpenSchemaModal: () => void
  onOpenHistoryModal: (tab?: "history" | "review" | "diagnostics") => void
  onRemoveEdge: (edgeId: string) => void
  onRemoveCorrelation: (corrId: string) => void
  onSelectRecordId: (id: string) => void
  onSwitchToGraph: () => void
}) {
  const b = detail.brief
  const meta = getKindMeta(b.kind)
  const wiki = useMemo(() => synthesizeWikiData(detail, overrides), [detail, overrides])
  const humanRecordTitle = useMemo(() => formatHumanObjectName(b.id, catalogById), [b.id, catalogById])

  // Inline header edit draft state
  const [draftName, setDraftName] = useState(b.name)
  const [draftSummary, setDraftSummary] = useState(b.summary ?? "")
  const [draftLifecycle, setDraftLifecycle] = useState(b.lifecycle ?? "active")
  const [draftOwner, setDraftOwner] = useState(b.owner ?? "agency:hollis-labs")
  const [draftOperator, setDraftOperator] = useState(wiki.operator ?? "")
  const [draftRepo, setDraftRepo] = useState(wiki.repo ?? "")
  const [draftTorquePrj, setDraftTorquePrj] = useState(wiki.torqueProjectId ?? "")

  // Per-section inline markdown editor state
  const [editingSection, setEditingSection] = useState<"overviewMd" | "architectureMd" | "runbookMd" | null>(null)
  const [sectionDraft, setSectionDraft] = useState("")

  useEffect(() => {
    setDraftName(b.name)
    setDraftSummary(b.summary ?? "")
    setDraftLifecycle(b.lifecycle ?? "active")
    setDraftOwner(b.owner ?? "agency:hollis-labs")
    setDraftOperator(wiki.operator ?? "")
    setDraftRepo(wiki.repo ?? "")
    setDraftTorquePrj(wiki.torqueProjectId ?? "")
    setEditingSection(null)
  }, [b.name, b.summary, b.lifecycle, b.owner, wiki.operator, wiki.repo, wiki.torqueProjectId])

  const commitHeaderEdits = () => {
    onSaveInlineBriefAndPayload({
      name: draftName.trim() || b.name,
      summary: draftSummary.trim(),
      lifecycle: draftLifecycle,
      owner: draftOwner.trim(),
      operator: draftOperator.trim(),
      repo: draftRepo.trim(),
      torqueProjectId: draftTorquePrj.trim(),
    })
    onToggleInlineEdit()
  }

  // Group all relationships into the 4 operational constellation buckets using human-readable names
  const groupedRelations = useMemo(() => {
    const buckets: Record<
      string,
      {
        slot: string
        edgeId: string
        predicate: string
        predicateLabel: string
        targetId: string
        targetName: string
        targetKind: string
        targetSummary?: string
        direction: "out" | "in"
        status?: string
      }[]
    > = {
      governance: [],
      topology: [],
      correlation: [],
      knowledge: [],
    }

    for (const e of detail.outgoing) {
      const cat = getPredicateCategory(e.relation.type)
      const tId = e.relation.to.id
      const targetBrief = catalogById.get(tId)
      const tKind = targetBrief?.kind ?? e.relation.to.kind ?? (tId.split(":")[0] || "record")
      buckets[cat].push({
        slot: `out-${e.id}-${tId}`,
        edgeId: e.id,
        predicate: e.relation.type,
        predicateLabel: humanizePredicate(e.relation.type),
        targetId: tId,
        targetName: formatHumanObjectName(tId, catalogById),
        targetKind: tKind,
        targetSummary: targetBrief?.summary,
        direction: "out",
        status: e.relation.status,
      })
    }

    for (const e of detail.incoming) {
      const pred = e.inverse || inverseFor(e.relation.type)
      const cat = getPredicateCategory(pred)
      const fId = e.relation.from.id
      const fromBrief = catalogById.get(fId)
      const fKind = fromBrief?.kind ?? e.relation.from.kind ?? (fId.split(":")[0] || "record")
      buckets[cat].push({
        slot: `in-${e.id}-${fId}`,
        edgeId: e.id,
        predicate: pred,
        predicateLabel: humanizePredicate(pred),
        targetId: fId,
        targetName: formatHumanObjectName(fId, catalogById),
        targetKind: fKind,
        targetSummary: fromBrief?.summary,
        direction: "in",
        status: e.relation.status,
      })
    }

    return buckets
  }, [detail.outgoing, detail.incoming, catalogById])

  const domainCluster = DOMAIN_CLUSTERS.find((c) => c.kinds.includes(b.kind)) ?? DOMAIN_CLUSTERS[0]
  const pendingReview = b.review?.queue_state === "pending"

  return (
    <div className="min-h-0 min-w-0 flex-1 overflow-y-auto overflow-x-hidden overscroll-x-none">
      <div className="mx-auto flex w-[92%] max-w-6xl min-w-0 flex-col gap-6 overflow-x-hidden py-5">
        {/* 1. Breadcrumb, Back Navigation & Provenance Bar */}
        <div className="flex flex-wrap items-center justify-between gap-2 border-b border-border/60 pb-2.5 text-xs text-muted-foreground">
          <div className="flex min-w-0 flex-wrap items-center gap-1.5">
            {canGoBack ? (
              <Button
                size="xs"
                variant="outline"
                onClick={onGoBack}
                className="mr-1 h-6 gap-1 border-cyan-500/40 bg-cyan-500/10 px-2 text-xs font-medium text-cyan-200 hover:bg-cyan-500/20"
                title={backTargetName ? `Back to ${backTargetName}` : "Go back to previous record"}
              >
                <Icon name="arrow-left" className="size-3" />
                <span>Back{backTargetName ? ` to ${backTargetName}` : ""}</span>
              </Button>
            ) : null}
            <Badge variant="outline" className={`font-mono text-[10px] ${domainCluster.badgeClass}`}>
              {domainCluster.title}
            </Badge>
            <span>›</span>
            <Badge variant="outline" className={`font-mono text-[10px] ${meta.tone}`}>
              {meta.label}
            </Badge>
            {b.owner ? (
              <>
                <span>›</span>
                <span className="inline-flex min-w-0 items-center gap-1">
                  <span>Owner:</span>
                  <ObjectLinkChip id={b.owner} catalogById={catalogById} onNavigate={onSelectRecordId} />
                </span>
              </>
            ) : null}
            <span>›</span>
            <span className="truncate font-mono text-xs text-muted-foreground" title={`registry:${b.id}`}>
              {humanRecordTitle}
            </span>
          </div>

          <div className="flex shrink-0 items-center gap-2">
            {wiki.verifiedBy ? (
              <Badge
                variant="outline"
                className="border-emerald-500/30 bg-emerald-500/10 font-mono text-[10px] text-emerald-300"
                title={`Verified by ${wiki.verifiedBy} (${wiki.verificationScope ?? "source_attestation"})`}
              >
                <Icon name="shield-check" className="mr-1 size-3" />
                Attested · {formatHumanObjectName(wiki.verifiedBy, catalogById)}
              </Badge>
            ) : null}
            {b.isLocalOverlay ? (
              <Badge
                variant="outline"
                className="border-cyan-500/40 bg-cyan-500/10 font-mono text-[10px] text-cyan-300"
                title="Local inline edits active (backed by local overlay; backend write API tracked in CW-20261121-0178)"
              >
                Local Wiki Overlay
              </Badge>
            ) : null}
            <span className="font-mono text-[11px]" title={b.updated_at}>
              Updated {ago(b.updated_at)}
            </span>
          </div>
        </div>

        {/* 2. Dossier Hero Header with Icon-Only Action Bar */}
        <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
          <div className="flex min-w-0 flex-1 items-start gap-3.5">
            <RegistryKindLineArt kind={b.kind} lifecycle={b.lifecycle} size="lg" />
            <div className="flex min-w-0 flex-1 flex-col gap-1.5">
              {inlineEditMode ? (
                <div className="flex flex-col gap-2 rounded-lg border border-cyan-500/40 bg-cyan-500/5 p-3">
                  <div className="flex flex-wrap items-center gap-2">
                    <Input
                      value={draftName}
                      onChange={(e) => setDraftName(e.target.value)}
                      className="h-8 max-w-md text-base font-bold"
                      placeholder="Record title"
                    />
                    <Select value={draftLifecycle} onValueChange={(v) => v && setDraftLifecycle(String(v))}>
                      <SelectTrigger size="sm" aria-label="Lifecycle" className="h-8 w-36">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        {LIFECYCLES.map((l) => (
                          <SelectItem key={l} value={l}>
                            {l}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </div>
                  <Input
                    value={draftSummary}
                    onChange={(e) => setDraftSummary(e.target.value)}
                    className="h-8 text-xs"
                    placeholder="Concise summary and operational role…"
                  />
                  <div className="grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-4">
                    <div className="flex flex-col gap-0.5">
                      <span className="font-mono text-[10px] text-muted-foreground uppercase">Owner URN</span>
                      <Input
                        value={draftOwner}
                        onChange={(e) => setDraftOwner(e.target.value)}
                        className="h-7 font-mono text-xs"
                        placeholder="agency:hollis-labs"
                      />
                    </div>
                    <div className="flex flex-col gap-0.5">
                      <span className="font-mono text-[10px] text-muted-foreground uppercase">Operator Agent</span>
                      <Input
                        value={draftOperator}
                        onChange={(e) => setDraftOperator(e.target.value)}
                        className="h-7 font-mono text-xs"
                        placeholder="agent:surface-lead"
                      />
                    </div>
                    <div className="flex flex-col gap-0.5">
                      <span className="font-mono text-[10px] text-muted-foreground uppercase">Repo / Endpoint</span>
                      <Input
                        value={draftRepo}
                        onChange={(e) => setDraftRepo(e.target.value)}
                        className="h-7 font-mono text-xs"
                        placeholder="hollis-labs/surface"
                      />
                    </div>
                    <div className="flex flex-col gap-0.5">
                      <span className="font-mono text-[10px] text-muted-foreground uppercase">Torque Project ID</span>
                      <Input
                        value={draftTorquePrj}
                        onChange={(e) => setDraftTorquePrj(e.target.value)}
                        className="h-7 font-mono text-xs"
                        placeholder="PRJ-20261118-0001"
                      />
                    </div>
                  </div>
                  <div className="flex items-center justify-end gap-2 pt-1">
                    <Button size="xs" variant="ghost" onClick={onToggleInlineEdit}>
                      Cancel
                    </Button>
                    <Button size="xs" onClick={commitHeaderEdits}>
                      <Icon name="check" className="size-3.5" />
                      Save Inline Changes
                    </Button>
                  </div>
                </div>
              ) : (
                <>
                  <div className="flex flex-wrap items-center gap-2">
                    <h1 className="text-xl font-bold tracking-tight text-foreground">{humanRecordTitle}</h1>
                    {b.lifecycle ? <Badge variant={tone(b.lifecycle)}>{b.lifecycle}</Badge> : null}
                    {b.subtype ? (
                      <Badge variant="outline" className="font-mono text-[11px]">
                        {b.subtype}
                      </Badge>
                    ) : null}
                    <Badge variant="outline" className="font-mono text-[11px]">
                      {detail.publication === "published" ? "published" : "working copy"}
                    </Badge>
                    {pendingReview ? <Badge variant="secondary">review pending</Badge> : null}
                  </div>
                  {b.summary ? (
                    <p className="max-w-3xl text-sm leading-relaxed text-muted-foreground">{b.summary}</p>
                  ) : null}
                </>
              )}
            </div>
          </div>

          {/* Icon-Only Inspector & Edit Action Toolbar (per user specification) */}
          <div className="flex shrink-0 items-center gap-1 rounded-lg border border-border/70 bg-card/50 p-1">
            <Button
              size="xs"
              variant={inlineEditMode ? "secondary" : "ghost"}
              className={`size-7 p-0 ${inlineEditMode ? "border border-cyan-500/50 bg-cyan-500/20 text-cyan-200" : ""}`}
              onClick={onToggleInlineEdit}
              title={inlineEditMode ? "Exit inline edit mode" : "Inline edit mode (Title, attributes & wiki sections)"}
              aria-label="Toggle inline edit mode"
            >
              <Icon name="pencil" className="size-3.5" />
            </Button>

            <Button
              size="xs"
              variant="ghost"
              className="size-7 p-0"
              onClick={() => onOpenRelationshipsModal()}
              title={`Relationships & Correlation Linker (${detail.outgoing.length + detail.incoming.length} edges)`}
              aria-label="Relationships & Correlation Linker"
            >
              <Icon name="git-fork" className="size-3.5" />
            </Button>

            <Button
              size="xs"
              variant="ghost"
              className="size-7 p-0"
              onClick={onOpenFieldsModal}
              title="Structured Fields, Inline Key-Value Editor & Raw JSON"
              aria-label="Structured Fields & Payload"
            >
              <Icon name="sliders-horizontal" className="size-3.5" />
            </Button>

            <Button
              size="xs"
              variant="ghost"
              className="size-7 p-0"
              onClick={onOpenSchemaModal}
              title={`Kind Schema & Ontology (${b.schema})`}
              aria-label="Kind Schema & Ontology"
            >
              <Icon name="braces" className="size-3.5" />
            </Button>

            <Button
              size="xs"
              variant="ghost"
              className="size-7 p-0"
              onClick={() => onOpenHistoryModal("history")}
              title={`Revision History, Diff & Review Governance (${detail.revisions.length} revisions)`}
              aria-label="Revision History & Governance"
            >
              <Icon name="rotate-ccw-clock" className="size-3.5" />
            </Button>

            <ObjectActionsMenu
              object={{
                kind: "registry.record",
                id: b.id,
                title: b.name,
                subtitle: `${b.kind} · ${b.schema}`,
                url: `registry:${b.id}`,
              }}
            />
          </div>
        </div>

        {/* 3. Operational Context & Cross-System Synthesis Strip (4 Ops-Style Telemetry Boxes) */}
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
          {/* Box 1: Governance & Ownership */}
          <div className="flex min-w-0 flex-col justify-between gap-2 overflow-hidden rounded-lg border border-border/70 bg-card/30 p-3">
            <div className="flex items-center justify-between gap-1">
              <span className="truncate font-mono text-[10px] font-semibold tracking-wider text-cyan-400 uppercase">
                Governance & Ownership
              </span>
              <Button
                size="xs"
                variant="ghost"
                className="size-5 shrink-0 p-0 text-muted-foreground hover:text-cyan-300"
                onClick={() => onOpenRelationshipsModal("owned_by", "agency")}
                title="Change or link owner / operator"
                aria-label="Link owner or operator"
              >
                <Icon name="link-2" className="size-3" />
              </Button>
            </div>
            <div className="flex min-w-0 flex-col gap-1.5 text-xs">
              <div className="flex min-w-0 items-center justify-between gap-2">
                <span className="shrink-0 text-muted-foreground">Owner</span>
                <div className="flex min-w-0 flex-1 justify-end overflow-hidden">
                  {b.owner ? (
                    <ObjectLinkChip id={b.owner} catalogById={catalogById} onNavigate={onSelectRecordId} />
                  ) : (
                    <span className="text-muted-foreground">unassigned</span>
                  )}
                </div>
              </div>
              <div className="flex min-w-0 items-center justify-between gap-2">
                <span className="shrink-0 text-muted-foreground">Operator</span>
                <div className="flex min-w-0 flex-1 justify-end overflow-hidden">
                  {wiki.operator ? (
                    <ObjectLinkChip id={wiki.operator} catalogById={catalogById} onNavigate={onSelectRecordId} />
                  ) : (
                    <span className="font-mono text-[11px] text-muted-foreground">—</span>
                  )}
                </div>
              </div>
            </div>
          </div>

          {/* Box 2: Execution & Cross-System Correlations (Ops / Torque) */}
          <div className="flex min-w-0 flex-col justify-between gap-2 overflow-hidden rounded-lg border border-border/70 bg-card/30 p-3">
            <div className="flex items-center justify-between gap-1">
              <span className="truncate font-mono text-[10px] font-semibold tracking-wider text-emerald-400 uppercase">
                Ops & Torque Correlation
              </span>
              <Button
                size="xs"
                variant="ghost"
                className="size-5 shrink-0 p-0 text-muted-foreground hover:text-emerald-300"
                onClick={() => onOpenRelationshipsModal("correlates_with", "workstream")}
                title="Link workstream, correlation, or Torque project/task"
                aria-label="Link workstream or correlation"
              >
                <Icon name="plus" className="size-3" />
              </Button>
            </div>
            <div className="flex min-w-0 flex-col gap-1.5 text-xs">
              <div className="flex min-w-0 items-center justify-between gap-2">
                <span className="shrink-0 text-muted-foreground">Torque</span>
                {wiki.torqueProjectId || wiki.opsTaskId ? (
                  <span className="min-w-0 truncate font-mono text-[11px] text-emerald-300">
                    <CiteText text={[wiki.torqueProjectId, wiki.opsTaskId].filter(Boolean).join(" · ")} />
                  </span>
                ) : (
                  <span className="font-mono text-[11px] text-muted-foreground">none bound</span>
                )}
              </div>
              <div className="flex min-w-0 items-center justify-between gap-2">
                <span className="shrink-0 text-muted-foreground">Bindings</span>
                <span className="truncate font-mono text-[11px] text-foreground">
                  {wiki.correlationBindings.length} active correlation{wiki.correlationBindings.length === 1 ? "" : "s"}
                </span>
              </div>
            </div>
          </div>

          {/* Box 3: Runtime, Repo & Fleet Infrastructure */}
          <div className="flex min-w-0 flex-col justify-between gap-2 overflow-hidden rounded-lg border border-border/70 bg-card/30 p-3">
            <div className="flex items-center justify-between gap-1">
              <span className="truncate font-mono text-[10px] font-semibold tracking-wider text-amber-400 uppercase">
                Runtime & Infrastructure
              </span>
              <Button
                size="xs"
                variant="ghost"
                className="size-5 shrink-0 p-0 text-muted-foreground hover:text-amber-300"
                onClick={() => onOpenRelationshipsModal("deployed_as", "service")}
                title="Link deployed service or Cerberus host"
                aria-label="Link deployed service or host"
              >
                <Icon name="server" className="size-3" />
              </Button>
            </div>
            <div className="flex min-w-0 flex-col gap-1.5 text-xs">
              <div className="flex min-w-0 items-center justify-between gap-2">
                <span className="shrink-0 text-muted-foreground">Source/Bind</span>
                <span className="min-w-0 truncate font-mono text-[11px] text-foreground">
                  {wiki.repo || wiki.endpoints[0] || humanRecordTitle}
                </span>
              </div>
              <div className="flex min-w-0 items-center justify-between gap-2">
                <span className="shrink-0 text-muted-foreground">Interfaces</span>
                <span className="min-w-0 truncate font-mono text-[11px] text-amber-300">
                  {wiki.interfaces.length > 0
                    ? wiki.interfaces.join(", ")
                    : wiki.hostId
                      ? formatHumanObjectName(wiki.hostId, catalogById)
                      : (wiki.disposition ?? "standard")}
                </span>
              </div>
            </div>
          </div>

          {/* Box 4: Knowledge Graph Connectivity */}
          <div className="flex min-w-0 flex-col justify-between gap-2 overflow-hidden rounded-lg border border-border/70 bg-card/30 p-3">
            <div className="flex items-center justify-between gap-1">
              <span className="truncate font-mono text-[10px] font-semibold tracking-wider text-violet-400 uppercase">
                Knowledge Graph Topology
              </span>
              <Button
                size="xs"
                variant="ghost"
                className="h-5 shrink-0 gap-1 px-1.5 font-mono text-[10px] text-violet-300 hover:bg-violet-500/15"
                onClick={onSwitchToGraph}
                title="Open visual Knowledge Graph constellation"
              >
                <Icon name="git-fork" className="size-3" />
                Graph
              </Button>
            </div>
            <div className="flex min-w-0 flex-col gap-1.5 text-xs">
              <div className="flex min-w-0 items-center justify-between gap-2">
                <span className="shrink-0 text-muted-foreground">Edges</span>
                <span className="truncate font-mono text-[11px] text-foreground">
                  {detail.outgoing.length} out · {detail.incoming.length} in
                </span>
              </div>
              <div className="flex min-w-0 items-center justify-between gap-2">
                <span className="shrink-0 text-muted-foreground">Revisions</span>
                <span className="truncate font-mono text-[11px] text-muted-foreground">
                  #{detail.revisions[0]?.sequence ?? 1} ({detail.revisions.length} total)
                </span>
              </div>
            </div>
          </div>
        </div>

        {/* 4. Interactive Related Objects & Correlation Constellation Cards (Search & Select to Add/Edit) */}
        <div className="flex flex-col gap-3">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div className="flex items-center gap-2">
              <Icon name="git-merge" className="size-4 text-cyan-400" />
              <h2 className="font-mono text-xs font-bold tracking-wider text-foreground uppercase">
                Connected Objects & System Correlations
              </h2>
              <Badge variant="secondary" className="font-mono text-[10px]">
                {detail.outgoing.length + detail.incoming.length + wiki.correlationBindings.length} linked
              </Badge>
            </div>
            <Button
              size="xs"
              variant="outline"
              className="gap-1 border-cyan-500/40 bg-cyan-500/10 text-cyan-200 hover:bg-cyan-500/20"
              onClick={() => onOpenRelationshipsModal()}
            >
              <Icon name="plus" className="size-3" />
              Link Object or Correlation
            </Button>
          </div>

          <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
            {/* Cluster A: Governance & Ownership */}
            <RelationClusterCard
              title="Governance & Ownership"
              subtitle="Owning agency, designated operator agent, and chartered portfolio items"
              accentClass="text-cyan-400"
              items={groupedRelations.governance}
              onAdd={() => onOpenRelationshipsModal("owned_by", "agency")}
              onNavigate={onSelectRecordId}
              onRemove={onRemoveEdge}
            />

            {/* Cluster B: Topology, Services & Runtime */}
            <RelationClusterCard
              title="System Topology, Services & Runtime"
              subtitle="Deployed services, fleet hosts, consumed dependencies & runtime configs"
              accentClass="text-amber-400"
              items={groupedRelations.topology}
              onAdd={() => onOpenRelationshipsModal("consumes", "project")}
              onNavigate={onSelectRecordId}
              onRemove={onRemoveEdge}
            />

            {/* Cluster C: Correlated Workstreams & Execution */}
            <div className="flex flex-col gap-2 rounded-lg border border-border/70 bg-card/25 p-3">
              <div className="flex items-center justify-between gap-2">
                <div>
                  <span className="font-mono text-xs font-semibold tracking-wider text-emerald-400 uppercase">
                    Correlated Workstreams & Execution
                  </span>
                  <p className="text-[11px] text-muted-foreground">
                    Cross-system bindings to Operations workstreams, Torque tasks, Cerberus & Caster
                  </p>
                </div>
                <Button
                  size="xs"
                  variant="ghost"
                  className="size-6 p-0 text-muted-foreground hover:text-emerald-300"
                  onClick={() => onOpenRelationshipsModal("correlates_with", "workstream")}
                  title="Search and link workstream or correlation object"
                  aria-label="Add correlation or workstream"
                >
                  <Icon name="plus" className="size-3.5" />
                </Button>
              </div>

              {groupedRelations.correlation.length === 0 && wiki.correlationBindings.length === 0 ? (
                <div className="flex items-center justify-between rounded-md border border-dashed border-border/60 px-3 py-2 text-xs text-muted-foreground">
                  <span>No workstreams or correlation bindings linked yet.</span>
                  <Button
                    size="xs"
                    variant="ghost"
                    className="h-6 text-[11px] text-emerald-300"
                    onClick={() => onOpenRelationshipsModal("correlates_with", "correlation")}
                  >
                    + Link Correlation
                  </Button>
                </div>
              ) : (
                <div className="flex flex-col gap-1.5">
                  {groupedRelations.correlation.map((item) => (
                    <RelatedObjectPill
                      key={item.slot}
                      item={item}
                      onNavigate={onSelectRecordId}
                      onRemove={onRemoveEdge}
                    />
                  ))}
                  {wiki.correlationBindings.map((cb) => (
                    <div
                      key={cb.id}
                      className="group flex min-w-0 items-center justify-between gap-2 overflow-hidden rounded-md border border-emerald-500/25 bg-emerald-500/5 px-2.5 py-1.5"
                    >
                      <div className="flex min-w-0 flex-1 items-center gap-1.5 overflow-hidden">
                        <Badge
                          variant="outline"
                          className="shrink-0 border-emerald-500/40 bg-emerald-500/10 font-mono text-[10px] text-emerald-300"
                        >
                          {cb.authority}:{cb.kind}
                        </Badge>
                        {cb.authority === "registry" ||
                        cb.ref.startsWith("workstream:") ||
                        cb.ref.startsWith("project:") ? (
                          <ObjectLinkChip id={cb.ref} catalogById={catalogById} onNavigate={onSelectRecordId} />
                        ) : (
                          <span className="shrink-0 font-mono text-xs text-foreground">
                            <CiteText text={cb.ref} />
                          </span>
                        )}
                        <span className="min-w-0 truncate text-xs text-muted-foreground">{cb.label}</span>
                      </div>
                      <Button
                        size="xs"
                        variant="ghost"
                        className="size-5 shrink-0 p-0 text-muted-foreground opacity-70 hover:text-destructive group-hover:opacity-100"
                        onClick={() => onRemoveCorrelation(cb.id)}
                        title="Unlink correlation binding"
                        aria-label={`Unlink ${cb.label}`}
                      >
                        <Icon name="x" className="size-3" />
                      </Button>
                    </div>
                  ))}
                </div>
              )}
            </div>

            {/* Cluster D: Agent Fabric, Skills & Documentation */}
            <RelationClusterCard
              title="Agent Fabric, Skills & Documentation"
              subtitle="Architecture specs, SOP runbooks, agent skills, and exposed MCP tools"
              accentClass="text-violet-400"
              items={groupedRelations.knowledge}
              onAdd={() => onOpenRelationshipsModal("documented_by", "document")}
              onNavigate={onSelectRecordId}
              onRemove={onRemoveEdge}
            />
          </div>
        </div>

        {/* 5. Wiki Narrative & Operational Documentation (Inline-Editable Markdown Articles) */}
        <div className="grid grid-cols-1 gap-5 lg:grid-cols-12">
          <div className="flex flex-col gap-5 lg:col-span-7">
            <WikiMarkdownSection
              title="Overview & Operational Purpose"
              icon="book-open"
              content={wiki.overviewMd}
              isEditing={editingSection === "overviewMd"}
              draft={sectionDraft}
              onStartEdit={() => {
                setEditingSection("overviewMd")
                setSectionDraft(wiki.overviewMd)
              }}
              onChangeDraft={setSectionDraft}
              onCancel={() => setEditingSection(null)}
              onSave={() => {
                onSaveWikiSection("overviewMd", sectionDraft)
                setEditingSection(null)
              }}
            />

            <WikiMarkdownSection
              title="Architecture, Composition & Interfaces"
              icon="layers"
              content={wiki.architectureMd}
              isEditing={editingSection === "architectureMd"}
              draft={sectionDraft}
              onStartEdit={() => {
                setEditingSection("architectureMd")
                setSectionDraft(wiki.architectureMd)
              }}
              onChangeDraft={setSectionDraft}
              onCancel={() => setEditingSection(null)}
              onSave={() => {
                onSaveWikiSection("architectureMd", sectionDraft)
                setEditingSection(null)
              }}
            />
          </div>

          <div className="flex flex-col gap-5 lg:col-span-5">
            <WikiMarkdownSection
              title="Operational Runbook & Usage"
              icon="terminal"
              content={wiki.runbookMd}
              isEditing={editingSection === "runbookMd"}
              draft={sectionDraft}
              onStartEdit={() => {
                setEditingSection("runbookMd")
                setSectionDraft(wiki.runbookMd)
              }}
              onChangeDraft={setSectionDraft}
              onCancel={() => setEditingSection(null)}
              onSave={() => {
                onSaveWikiSection("runbookMd", sectionDraft)
                setEditingSection(null)
              }}
            />

            {/* Quick Provenance & Metadata Summary Card */}
            <div className="flex flex-col gap-2 rounded-lg border border-border/70 bg-card/20 p-3.5">
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <Icon name="shield-check" className="size-3.5 text-cyan-400" />
                  <span className="font-mono text-xs font-semibold tracking-wider text-foreground uppercase">
                    Attestation & Record Provenance
                  </span>
                </div>
                <Button
                  size="xs"
                  variant="ghost"
                  className="size-6 p-0 text-muted-foreground hover:text-cyan-300"
                  onClick={onOpenFieldsModal}
                  title="Inspect all structured fields & raw payload"
                  aria-label="Inspect structured fields"
                >
                  <Icon name="sliders-horizontal" className="size-3" />
                </Button>
              </div>
              <div className="grid grid-cols-2 gap-x-3 gap-y-1.5 text-xs">
                <div>
                  <span className="text-muted-foreground">Canonical URN</span>
                  <p className="truncate font-mono text-[11px] text-foreground">registry:{b.id}</p>
                </div>
                <div>
                  <span className="text-muted-foreground">Schema</span>
                  <p className="truncate font-mono text-[11px] text-foreground">{b.schema}</p>
                </div>
                <div>
                  <span className="text-muted-foreground">Verified By</span>
                  <p className="truncate font-mono text-[11px] text-emerald-300">{wiki.verifiedBy ?? "working copy"}</p>
                </div>
                <div>
                  <span className="text-muted-foreground">Scope</span>
                  <p className="truncate font-mono text-[11px] text-muted-foreground">
                    {wiki.verificationScope ?? "draft"}
                  </p>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}

function RelationClusterCard({
  title,
  subtitle,
  accentClass,
  items,
  onAdd,
  onNavigate,
  onRemove,
}: {
  title: string
  subtitle: string
  accentClass: string
  items: {
    slot: string
    edgeId: string
    predicate: string
    predicateLabel: string
    targetId: string
    targetName: string
    targetKind: string
    targetSummary?: string
    direction: "out" | "in"
    status?: string
  }[]
  onAdd: () => void
  onNavigate: (id: string) => void
  onRemove: (edgeId: string) => void
}) {
  return (
    <div className="flex min-w-0 flex-col gap-2 overflow-hidden rounded-lg border border-border/70 bg-card/25 p-3">
      <div className="flex items-center justify-between gap-2">
        <div className="min-w-0">
          <span className={`font-mono text-xs font-semibold tracking-wider uppercase ${accentClass}`}>{title}</span>
          <p className="truncate text-[11px] text-muted-foreground">{subtitle}</p>
        </div>
        <Button
          size="xs"
          variant="ghost"
          className="size-6 shrink-0 p-0 text-muted-foreground hover:text-cyan-300"
          onClick={onAdd}
          title={`Search and link ${title.toLowerCase()}`}
          aria-label={`Add ${title}`}
        >
          <Icon name="plus" className="size-3.5" />
        </Button>
      </div>

      {items.length === 0 ? (
        <div className="flex items-center justify-between rounded-md border border-dashed border-border/60 px-3 py-2 text-xs text-muted-foreground">
          <span>No linked objects in this category.</span>
          <Button size="xs" variant="ghost" className="h-6 text-[11px]" onClick={onAdd}>
            + Search & Link
          </Button>
        </div>
      ) : (
        <div className="flex min-w-0 flex-col gap-1.5">
          {items.map((item) => (
            <RelatedObjectPill key={item.slot} item={item} onNavigate={onNavigate} onRemove={onRemove} />
          ))}
        </div>
      )}
    </div>
  )
}

function RelatedObjectPill({
  item,
  onNavigate,
  onRemove,
}: {
  item: {
    slot: string
    edgeId: string
    predicate: string
    predicateLabel: string
    targetId: string
    targetName: string
    targetKind: string
    targetSummary?: string
    direction: "out" | "in"
    status?: string
  }
  onNavigate: (id: string) => void
  onRemove: (edgeId: string) => void
}) {
  const meta = getKindMeta(item.targetKind)
  return (
    <div className="group flex min-w-0 items-center justify-between gap-2 overflow-hidden rounded-md border border-border/60 bg-background/60 px-2.5 py-1.5 transition-colors hover:border-cyan-500/40">
      <div className="flex min-w-0 flex-1 items-center gap-1.5 overflow-hidden">
        <Badge variant="outline" className="shrink-0 font-mono text-[10px] text-muted-foreground">
          {item.predicateLabel}
        </Badge>
        <Badge variant="outline" className={`shrink-0 px-1.5 py-0 font-mono text-[10px] ${meta.tone}`}>
          {meta.label}
        </Badge>
        <Button
          size="xs"
          variant="ghost"
          className="h-5 min-w-0 flex-1 justify-start px-1 text-left text-xs font-semibold text-foreground hover:text-cyan-300"
          onClick={() => onNavigate(item.targetId)}
          title={`${item.targetName} (registry:${item.targetId})${item.targetSummary ? ` — ${item.targetSummary}` : ""}`}
        >
          <span className="min-w-0 truncate">{item.targetName}</span>
        </Button>
        {item.status === "proposed" ? (
          <Badge variant="secondary" className="shrink-0 text-[9px]">
            proposed
          </Badge>
        ) : null}
      </div>
      <div className="flex shrink-0 items-center gap-1">
        <Button
          size="xs"
          variant="ghost"
          className="size-5 p-0 text-muted-foreground hover:text-cyan-300"
          onClick={() => onNavigate(item.targetId)}
          title={`Open ${item.targetName}`}
          aria-label={`Open ${item.targetName}`}
        >
          <Icon name="arrow-up-right" className="size-3" />
        </Button>
        <Button
          size="xs"
          variant="ghost"
          className="size-5 p-0 text-muted-foreground opacity-60 hover:text-destructive group-hover:opacity-100"
          onClick={() => onRemove(item.edgeId)}
          title="Unlink relationship"
          aria-label={`Unlink ${item.targetName}`}
        >
          <Icon name="x" className="size-3" />
        </Button>
      </div>
    </div>
  )
}

function WikiMarkdownSection({
  title,
  icon,
  content,
  isEditing,
  draft,
  onStartEdit,
  onChangeDraft,
  onCancel,
  onSave,
}: {
  title: string
  icon: string
  content: string
  isEditing: boolean
  draft: string
  onStartEdit: () => void
  onChangeDraft: (v: string) => void
  onCancel: () => void
  onSave: () => void
}) {
  return (
    <div className="flex min-w-0 flex-col gap-2.5 overflow-hidden rounded-lg border border-border/70 bg-card/20 p-4">
      <div className="flex items-center justify-between border-b border-border/50 pb-2">
        <div className="flex items-center gap-2">
          <Icon name={icon} className="size-3.5 text-cyan-400" />
          <h3 className="font-mono text-xs font-bold tracking-wider text-foreground uppercase">{title}</h3>
        </div>
        {!isEditing ? (
          <Button
            size="xs"
            variant="ghost"
            className="size-6 p-0 text-muted-foreground hover:text-cyan-300"
            onClick={onStartEdit}
            title={`Inline edit ${title}`}
            aria-label={`Inline edit ${title}`}
          >
            <Icon name="pencil" className="size-3" />
          </Button>
        ) : (
          <div className="flex items-center gap-1">
            <Button size="xs" variant="ghost" onClick={onCancel}>
              Cancel
            </Button>
            <Button size="xs" onClick={onSave}>
              <Icon name="check" className="size-3" />
              Save
            </Button>
          </div>
        )}
      </div>

      {isEditing ? (
        <Textarea
          value={draft}
          onChange={(e) => onChangeDraft(e.target.value)}
          rows={7}
          className="font-mono text-xs leading-relaxed"
          placeholder="Write wiki Markdown (supports Cite references like registry:project:surface, CW-..., ops:...)…"
        />
      ) : (
        <div className="prose prose-invert max-w-none min-w-0 overflow-x-hidden break-words text-sm leading-relaxed [&_pre]:max-w-full [&_pre]:overflow-x-auto">
          <Markdown>{content}</Markdown>
        </div>
      )}
    </div>
  )
}

/**
 * Domain Atlas View: Ops-styled card grid across all domains or the selected domain kind.
 */
function DomainAtlasView({
  records,
  selectedId,
  catalogById,
  onSelectRecord,
  onOpenRelationshipsFor,
}: {
  records: Brief[]
  selectedId: string
  catalogById: Map<string, Brief>
  onSelectRecord: (b: Brief) => void
  onOpenRelationshipsFor: (b: Brief) => void
}) {
  return (
    <div className="min-h-0 min-w-0 flex-1 overflow-y-auto overflow-x-hidden overscroll-x-none p-5">
      <div className="mx-auto flex w-[94%] max-w-7xl min-w-0 flex-col gap-4 overflow-x-hidden">
        <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3">
          {records.map((r) => {
            const meta = getKindMeta(r.kind)
            const active = r.id === selectedId
            const humanTitle = formatHumanObjectName(r.id, catalogById)
            const ownerHuman = r.owner ? formatHumanObjectName(r.owner, catalogById) : "unassigned"
            return (
              <div
                key={r.id}
                className={`flex min-w-0 flex-col justify-between gap-3 overflow-hidden rounded-lg border p-3.5 transition-all ${
                  active
                    ? "border-cyan-500/60 bg-cyan-500/10"
                    : "border-border/70 bg-card/30 hover:border-border hover:bg-card/50"
                }`}
              >
                <div className="flex min-w-0 items-start gap-3">
                  <RegistryKindLineArt kind={r.kind} lifecycle={r.lifecycle} size="sm" />
                  <div className="flex min-w-0 flex-1 flex-col gap-1">
                    <div className="flex flex-wrap items-center gap-1.5">
                      <Badge variant="outline" className={`px-1.5 py-0 font-mono text-[10px] ${meta.tone}`}>
                        {meta.label}
                      </Badge>
                      {r.lifecycle ? <Badge variant={tone(r.lifecycle)}>{r.lifecycle}</Badge> : null}
                      {r.isLocalOverlay ? (
                        <Badge variant="outline" className="border-cyan-500/40 font-mono text-[10px] text-cyan-300">
                          edited
                        </Badge>
                      ) : null}
                    </div>
                    <Button
                      variant="ghost"
                      className="h-auto min-w-0 justify-start p-0 text-left text-sm font-bold text-foreground hover:bg-transparent hover:text-cyan-300"
                      onClick={() => onSelectRecord(r)}
                      title={`${humanTitle} (registry:${r.id})`}
                    >
                      <span className="truncate">{humanTitle}</span>
                    </Button>
                    {r.summary ? (
                      <p className="line-clamp-2 text-xs leading-relaxed text-muted-foreground">{r.summary}</p>
                    ) : null}
                  </div>
                </div>

                <div className="flex min-w-0 items-center justify-between gap-2 border-t border-border/50 pt-2 text-xs">
                  <div className="flex min-w-0 items-center gap-1.5 text-muted-foreground">
                    <span className="truncate text-[11px]" title={r.owner ? `Owner: ${r.owner}` : "Unassigned"}>
                      {ownerHuman}
                    </span>
                  </div>
                  <div className="flex shrink-0 items-center gap-1">
                    <Button
                      size="xs"
                      variant="ghost"
                      className="h-6 gap-1 px-1.5 font-mono text-[11px] text-muted-foreground hover:text-cyan-300"
                      onClick={() => onOpenRelationshipsFor(r)}
                      title="Link or inspect relationships"
                    >
                      <Icon name="git-fork" className="size-3" />
                      {r.edges}
                    </Button>
                    <Button
                      size="xs"
                      variant="outline"
                      className="h-6 px-2 text-[11px]"
                      onClick={() => onSelectRecord(r)}
                    >
                      Open Wiki
                    </Button>
                  </div>
                </div>
              </div>
            )
          })}
        </div>
      </div>
    </div>
  )
}

/** Main Registry Module Window */
function Registry({ win, desk, api }: ViewProps) {
  const args = win.args ?? {}
  const pub: Pub = args.publication === "published" ? "published" : "draft"
  const kind = typeof args.kind === "string" ? args.kind : ""
  const id = typeof args.id === "string" ? args.id : "project:surface"

  const [viewMode, setViewMode] = useState<RegistryViewMode>("wiki")
  const [railCollapsed, setRailCollapsed] = useState(false)
  const [globalSearch, setGlobalSearch] = useState("")
  const [railLifecycle, setRailLifecycle] = useState(ANY)

  const [rawOverview, setRawOverview] = useState<Overview | null>(null)
  const [liveRecords, setLiveRecords] = useState<Brief[]>([])
  const [preloadedCatalog, setPreloadedCatalog] = useState<Brief[]>([])
  const [preloadedCounts, setPreloadedCounts] = useState<Record<string, number>>({})
  const [loadingRecords, setLoadingRecords] = useState(false)
  const [liveDetail, setLiveDetail] = useState<Detail | null>(null)
  const [error, setError] = useState("")
  const [tick, setTick] = useState(0)
  const [busy, setBusy] = useState(false)

  // In-module navigation history stack for the Back button
  const [navHistory, setNavHistory] = useState<string[]>([])

  // Persistent local overlay for inline edits, newly created wiki records, and relationship/correlation links
  const [overrides, setOverrides] = useState<Record<string, LocalRecordOverride>>(() => loadLocalOverrides())

  const updateOverrides = useCallback(
    (updater: (prev: Record<string, LocalRecordOverride>) => Record<string, LocalRecordOverride>) => {
      setOverrides((prev) => {
        const next = updater(prev)
        saveLocalOverrides(next)
        return next
      })
    },
    [],
  )

  // Inspector & creation modal states (all tucked behind icon actions)
  const [inlineEditMode, setInlineEditMode] = useState(false)
  const [relModalOpen, setRelModalOpen] = useState(false)
  const [relInitPredicate, setRelInitPredicate] = useState<string | undefined>(undefined)
  const [relInitKind, setRelInitKind] = useState<string | undefined>(undefined)
  const [fieldsModalOpen, setFieldsModalOpen] = useState(false)
  const [schemaModalOpen, setSchemaModalOpen] = useState(false)
  const [schemaModalKind, setSchemaModalKind] = useState<string | undefined>(undefined)
  const [historyModalOpen, setHistoryModalOpen] = useState(false)
  const [historyModalTab, setHistoryModalTab] = useState<"history" | "review" | "diagnostics">("history")
  const [createModalOpen, setCreateModalOpen] = useState(false)

  const go = useCallback(
    (next: Record<string, unknown>) => {
      const a: Record<string, unknown> = { publication: pub, kind, id, ...next }
      for (const k of Object.keys(a)) if (a[k] === "" || a[k] === undefined) delete a[k]
      void desk.setArgs(win.id, a)
    },
    [desk, win.id, pub, kind, id],
  )

  // 1. Load Overview
  const loadOverview = useCallback(() => {
    setBusy(true)
    api
      .get<Overview>(`overview?publication=${pub}`)
      .then((v) => {
        setRawOverview(v)
        setError("")
      })
      .catch((e: Error) => setError(e.message))
      .finally(() => setBusy(false))
  }, [api, pub])

  // biome-ignore lint/correctness/useExhaustiveDependencies: tick triggers refresh
  useEffect(loadOverview, [loadOverview, tick])

  // 1b. Preload catalog briefs across all core kinds so human names & per-kind counts are always available
  // biome-ignore lint/correctness/useExhaustiveDependencies: tick triggers refresh
  useEffect(() => {
    const coreKinds = [
      "agency",
      "project",
      "workstream",
      "correlation",
      "config",
      "service",
      "agent",
      "skill",
      "tool",
      "document",
    ]
    Promise.all(
      coreKinds.map((k) =>
        api
          .get<Page>(`records?publication=${pub}&kind=${k}`)
          .then((p) => ({ kind: k, records: p.records ?? [] }))
          .catch(() => ({ kind: k, records: [] as Brief[] })),
      ),
    ).then((results) => {
      const all: Brief[] = []
      const counts: Record<string, number> = {}
      for (const res of results) {
        counts[res.kind] = res.records.length
        for (const r of res.records) all.push(r)
      }
      setPreloadedCatalog(all)
      setPreloadedCounts(counts)
    })
  }, [api, pub, tick])

  // 2. Load Records for current kind / search
  // biome-ignore lint/correctness/useExhaustiveDependencies: tick triggers refresh
  useEffect(() => {
    const p = new URLSearchParams({ publication: pub })
    if (kind) p.set("kind", kind)
    if (globalSearch.trim()) p.set("q", globalSearch.trim())
    if (railLifecycle !== ANY) p.set("lifecycle", railLifecycle)
    setLoadingRecords(true)
    api
      .get<Page>(`records?${p.toString()}`)
      .then((page) => {
        setLiveRecords(page.records ?? [])
      })
      .catch(() => {
        setLiveRecords([])
      })
      .finally(() => setLoadingRecords(false))
  }, [api, pub, kind, globalSearch, railLifecycle, tick])

  // 3. Load Record Detail for selected id
  // biome-ignore lint/correctness/useExhaustiveDependencies: tick triggers refresh
  useEffect(() => {
    if (!id) {
      setLiveDetail(null)
      return
    }
    api
      .get<Detail>(`record?id=${q(id)}&publication=${pub}`)
      .then((d) => {
        setLiveDetail(d)
      })
      .catch(() => {
        setLiveDetail(null)
      })
  }, [api, id, pub, tick])

  useEffect(
    () => desk.report(win.id, id ? { kind: "record", ref: `registry:${id}` } : { kind: "data", ref: "registry" }),
    [desk, win.id, id],
  )

  useEffect(() => {
    if (!win.focused) return
    const t = window.setInterval(() => {
      if (document.visibilityState === "visible") setTick((n) => n + 1)
    }, REFRESH_MS)
    return () => window.clearInterval(t)
  }, [win.focused])

  const overview = useMemo(() => {
    const base = rawOverview
      ? enrichOverview(rawOverview, overrides)
      : enrichOverview(
          {
            publication: pub,
            kinds: [],
            reviews: [],
            total: 0,
          },
          overrides,
        )
    const kinds = base.kinds.map((k) => ({
      ...k,
      count: Math.max(k.count, preloadedCounts[k.kind] ?? 0),
    }))
    const total = kinds.reduce((acc, k) => acc + k.count, 0)
    return { ...base, kinds, total }
  }, [rawOverview, overrides, pub, preloadedCounts])

  const records = useMemo(
    () =>
      enrichRecordsPage(
        kind ? liveRecords : [...preloadedCatalog, ...liveRecords],
        {
          kind: kind || undefined,
          search: globalSearch,
          lifecycle: railLifecycle,
        },
        overrides,
      ),
    [liveRecords, preloadedCatalog, kind, globalSearch, railLifecycle, overrides],
  )

  const catalogById = useMemo(() => {
    const m = new Map<string, Brief>()
    for (const entry of Object.values(SEED_KNOWLEDGE)) m.set(entry.brief.id, entry.brief)
    for (const r of preloadedCatalog) m.set(r.id, r)
    for (const r of liveRecords) m.set(r.id, r)
    for (const r of records) m.set(r.id, r)
    for (const ov of Object.values(overrides)) m.set(ov.brief.id, ov.brief)
    return m
  }, [preloadedCatalog, liveRecords, records, overrides])

  const detail = useMemo(
    () => (id ? buildEnrichedDetail(id, liveDetail?.brief.id === id ? liveDetail : null, overrides) : null),
    [id, liveDetail, overrides],
  )

  // Mutation handlers (persisted in local overlay)
  const ensureOverride = useCallback(
    (targetDetail: Detail): LocalRecordOverride => {
      const existing = overrides[targetDetail.brief.id]
      if (existing) return existing
      return {
        brief: { ...targetDetail.brief, isLocalOverlay: true },
        payload: { ...((targetDetail.record.payload as Record<string, unknown> | undefined) ?? {}) },
        metadata: { ...((targetDetail.record.metadata as Record<string, unknown> | undefined) ?? {}) },
        wiki: {},
        addedEdges: [],
        removedEdgeIds: [],
        addedCorrelations: [],
        removedCorrelationIds: [],
        updatedAt: new Date().toISOString(),
      }
    },
    [overrides],
  )

  const handleSaveInlineBriefAndPayload = (patch: {
    name?: string
    summary?: string
    lifecycle?: string
    owner?: string
    operator?: string
    repo?: string
    torqueProjectId?: string
  }) => {
    if (!detail) return
    updateOverrides((prev) => {
      const base = prev[detail.brief.id] ?? ensureOverride(detail)
      const nextPayload = { ...base.payload }
      const nextPayloadMeta = { ...((nextPayload.metadata as Record<string, unknown> | undefined) ?? {}) }
      if (patch.operator !== undefined) nextPayload.operator = patch.operator
      if (patch.repo !== undefined) nextPayload.repo = patch.repo
      if (patch.torqueProjectId !== undefined) nextPayloadMeta.torque_project_id = patch.torqueProjectId
      nextPayload.metadata = nextPayloadMeta

      return {
        ...prev,
        [detail.brief.id]: {
          ...base,
          brief: {
            ...base.brief,
            name: patch.name ?? base.brief.name,
            summary: patch.summary ?? base.brief.summary,
            lifecycle: patch.lifecycle ?? base.brief.lifecycle,
            owner: patch.owner ?? base.brief.owner,
            updated_at: new Date().toISOString(),
            isLocalOverlay: true,
          },
          payload: nextPayload,
          updatedAt: new Date().toISOString(),
        },
      }
    })
  }

  const handleSaveWikiSection = (section: "overviewMd" | "architectureMd" | "runbookMd", content: string) => {
    if (!detail) return
    updateOverrides((prev) => {
      const base = prev[detail.brief.id] ?? ensureOverride(detail)
      return {
        ...prev,
        [detail.brief.id]: {
          ...base,
          wiki: {
            ...(base.wiki ?? {}),
            [section]: content,
          },
          updatedAt: new Date().toISOString(),
        },
      }
    })
  }

  const handleUpdatePayloadField = (key: string, value: unknown) => {
    if (!detail) return
    updateOverrides((prev) => {
      const base = prev[detail.brief.id] ?? ensureOverride(detail)
      return {
        ...prev,
        [detail.brief.id]: {
          ...base,
          payload: {
            ...base.payload,
            [key]: value,
          },
          updatedAt: new Date().toISOString(),
        },
      }
    })
  }

  const handleAddEdge = (predicate: string, target: Brief) => {
    if (!detail) return
    const newEdge: Edge = {
      id: `edge:local:${detail.brief.id}:${predicate}:${target.id}:${Date.now()}`,
      inverse: inverseFor(predicate),
      target_state: "resolved",
      relation: {
        type: predicate,
        status: "reviewed",
        source: "authored",
        from: { authority: "registry", kind: detail.brief.kind, id: detail.brief.id },
        to: { authority: "registry", kind: target.kind, id: target.id },
      },
    }
    updateOverrides((prev) => {
      const base = prev[detail.brief.id] ?? ensureOverride(detail)
      const nextBrief = { ...base.brief }
      if (predicate === "owned_by") nextBrief.owner = target.id
      return {
        ...prev,
        [detail.brief.id]: {
          ...base,
          brief: { ...nextBrief, updated_at: new Date().toISOString(), isLocalOverlay: true },
          addedEdges: [...base.addedEdges, newEdge],
          updatedAt: new Date().toISOString(),
        },
      }
    })
  }

  const handleRemoveEdge = (edgeId: string) => {
    if (!detail) return
    updateOverrides((prev) => {
      const base = prev[detail.brief.id] ?? ensureOverride(detail)
      return {
        ...prev,
        [detail.brief.id]: {
          ...base,
          addedEdges: base.addedEdges.filter((e) => e.id !== edgeId),
          removedEdgeIds: [...new Set([...base.removedEdgeIds, edgeId])],
          updatedAt: new Date().toISOString(),
        },
      }
    })
  }

  const handleAddCorrelation = (binding: CorrelationBinding) => {
    if (!detail) return
    updateOverrides((prev) => {
      const base = prev[detail.brief.id] ?? ensureOverride(detail)
      return {
        ...prev,
        [detail.brief.id]: {
          ...base,
          addedCorrelations: [...base.addedCorrelations, binding],
          updatedAt: new Date().toISOString(),
        },
      }
    })
  }

  const handleRemoveCorrelation = (bindingId: string) => {
    if (!detail) return
    updateOverrides((prev) => {
      const base = prev[detail.brief.id] ?? ensureOverride(detail)
      return {
        ...prev,
        [detail.brief.id]: {
          ...base,
          addedCorrelations: base.addedCorrelations.filter((c) => c.id !== bindingId),
          removedCorrelationIds: [...new Set([...base.removedCorrelationIds, bindingId])],
          updatedAt: new Date().toISOString(),
        },
      }
    })
  }

  const handleCreateRecord = (newBrief: Brief, initialOverviewMd: string) => {
    const ownerEdges: Edge[] =
      newBrief.owner?.includes(":") && !newBrief.owner.startsWith("user:")
        ? [
            {
              id: `edge:local:${newBrief.id}:owned_by:${newBrief.owner}`,
              inverse: "owns",
              target_state: "resolved",
              relation: {
                type: "owned_by",
                status: "reviewed",
                source: "authored",
                from: { authority: "registry", kind: newBrief.kind, id: newBrief.id },
                to: {
                  authority: "registry",
                  kind: newBrief.owner.split(":")[0] || "agency",
                  id: newBrief.owner,
                },
              },
            },
          ]
        : []

    updateOverrides((prev) => ({
      ...prev,
      [newBrief.id]: {
        brief: newBrief,
        payload: {
          id: newBrief.key ?? newBrief.id,
          name: newBrief.name,
          schema: newBrief.schema,
          lifecycle: newBrief.lifecycle,
          owner: newBrief.owner,
          description: newBrief.summary,
        },
        wiki: {
          overviewMd: initialOverviewMd,
        },
        addedEdges: ownerEdges,
        removedEdgeIds: [],
        addedCorrelations: [],
        removedCorrelationIds: [],
        updatedAt: new Date().toISOString(),
      },
    }))

    go({ kind: newBrief.kind, id: newBrief.id })
    setViewMode("wiki")
    setInlineEditMode(true)
  }

  const selectRecordById = useCallback(
    (targetId: string) => {
      const clean = targetId.replace(/^registry:/, "")
      if (clean && id && clean !== id) {
        setNavHistory((prev) => [...prev.slice(-19), id])
      }
      const targetKind = clean.includes(":") ? clean.split(":")[0] : kind
      go({ kind: targetKind, id: clean })
    },
    [go, kind, id],
  )

  const handleGoBack = useCallback(() => {
    setNavHistory((prev) => {
      if (prev.length === 0) return prev
      const previousId = prev[prev.length - 1]
      const targetKind = previousId.includes(":") ? previousId.split(":")[0] : kind
      go({ kind: targetKind, id: previousId })
      return prev.slice(0, -1)
    })
  }, [go, kind])

  const lastHistoryId = navHistory.length > 0 ? navHistory[navHistory.length - 1] : undefined
  const backTargetName = useMemo(
    () => (lastHistoryId ? formatHumanObjectName(lastHistoryId, catalogById) : undefined),
    [lastHistoryId, catalogById],
  )

  return (
    <div className="@container flex h-full min-h-0 w-full flex-col overflow-x-hidden overscroll-x-none bg-background">
      {/* Top Ops-Styled Telemetry & Knowledge Navigation Bar */}
      <header className="flex shrink-0 flex-wrap items-center justify-between gap-2 border-b border-border/70 bg-card/30 px-3 py-2">
        <div className="flex flex-wrap items-center gap-2">
          <Button
            size="xs"
            variant="ghost"
            className="size-7 p-0"
            onClick={() => setRailCollapsed((c) => !c)}
            title={railCollapsed ? "Expand Knowledge Explorer rail" : "Collapse Knowledge Explorer rail"}
            aria-label="Toggle Knowledge Explorer rail"
          >
            <Icon name={railCollapsed ? "panel-left-open" : "panel-left-close"} className="size-4" />
          </Button>

          {navHistory.length > 0 ? (
            <Button
              size="xs"
              variant="outline"
              onClick={handleGoBack}
              className="h-7 gap-1 border-cyan-500/40 bg-cyan-500/10 px-2 text-xs text-cyan-200 hover:bg-cyan-500/20"
              title={backTargetName ? `Back to ${backTargetName}` : "Back to previous record"}
            >
              <Icon name="arrow-left" className="size-3.5" />
              <span className="max-w-32 truncate">{backTargetName ?? "Back"}</span>
            </Button>
          ) : null}

          <div className="flex items-center gap-2">
            <span className="size-2 rounded-full bg-cyan-400 shadow-[0_0_8px_#22d3ee]" />
            <span className="text-sm font-bold tracking-tight">System Knowledge & Registry</span>
          </div>

          <div className="hidden items-center gap-1.5 @3xl:flex">
            <Badge variant="outline" className="font-mono text-[10px] text-cyan-300">
              {overview.total} records
            </Badge>
            <Badge variant="outline" className="font-mono text-[10px] text-muted-foreground">
              {overview.kinds.length} domains
            </Badge>
            {overview.reviews.length > 0 ? (
              <Badge variant="secondary" className="font-mono text-[10px]">
                {overview.reviews.length} review
              </Badge>
            ) : null}
          </div>

          <ToggleGroup
            size="sm"
            spacing={0}
            value={[pub]}
            onValueChange={(v: string[]) => v[0] && go({ publication: v[0], id })}
            aria-label="Publication state"
          >
            <ToggleGroupItem value="draft" title="Working copy (all draft & active records)">
              Working
            </ToggleGroupItem>
            <ToggleGroupItem value="published" title="Published snapshot">
              Published
            </ToggleGroupItem>
          </ToggleGroup>
        </div>

        {/* Center View Mode Switcher & Omni-Search */}
        <div className="flex flex-wrap items-center gap-2">
          <ToggleGroup
            size="sm"
            spacing={0}
            value={[viewMode]}
            onValueChange={(v: string[]) => v[0] && setViewMode(v[0] as RegistryViewMode)}
            aria-label="Registry view mode"
          >
            <ToggleGroupItem value="wiki" title="Wiki Dossier & Operational Synthesis">
              <Icon name="book-open" className="mr-1 size-3.5" />
              Wiki
            </ToggleGroupItem>
            <ToggleGroupItem value="graph" title="Interactive Knowledge Graph Constellation">
              <Icon name="git-fork" className="mr-1 size-3.5" />
              Graph
            </ToggleGroupItem>
            <ToggleGroupItem value="atlas" title="Domain Atlas Card Grid">
              <Icon name="layout-grid" className="mr-1 size-3.5" />
              Atlas
            </ToggleGroupItem>
          </ToggleGroup>

          <div className="relative w-52 @4xl:w-64">
            <Input
              value={globalSearch}
              onChange={(e) => setGlobalSearch(e.target.value)}
              placeholder="Search knowledge graph, owners, IDs…"
              className="h-7 pr-7 text-xs"
              aria-label="Global Registry search"
            />
            {globalSearch ? (
              <Button
                size="xs"
                variant="ghost"
                className="absolute top-1 right-1 size-5 p-0"
                onClick={() => setGlobalSearch("")}
                aria-label="Clear search"
              >
                <Icon name="x" className="size-3" />
              </Button>
            ) : null}
          </div>

          {/* Icon-Only Action Buttons for Create, Schema, Governance/Diagnostics & Refresh */}
          <div className="flex items-center gap-1 rounded-md border border-border/60 bg-background/60 p-0.5">
            <Button
              size="xs"
              variant="ghost"
              className="size-7 p-0 text-cyan-300 hover:bg-cyan-500/15"
              onClick={() => setCreateModalOpen(true)}
              title="Create new Wiki Record (+)"
              aria-label="Create new Wiki Record"
            >
              <Icon name="plus" className="size-3.5" />
            </Button>

            <Button
              size="xs"
              variant="ghost"
              className="size-7 p-0"
              onClick={() => {
                setSchemaModalKind(detail?.brief.kind || kind || "project")
                setSchemaModalOpen(true)
              }}
              title="Schema & Ontology Inspector"
              aria-label="Schema & Ontology Inspector"
            >
              <Icon name="braces" className="size-3.5" />
            </Button>

            <Button
              size="xs"
              variant="ghost"
              className="size-7 p-0"
              onClick={() => {
                setHistoryModalTab(overview.reviews.length > 0 ? "review" : "diagnostics")
                setHistoryModalOpen(true)
              }}
              title={`Governance, Reviews (${overview.reviews.length}) & Diagnostics`}
              aria-label="Governance & Diagnostics"
            >
              <Icon name="shield-check" className="size-3.5" />
            </Button>

            <Button
              size="xs"
              variant="ghost"
              className="size-7 p-0"
              disabled={busy}
              onClick={() => setTick((n) => n + 1)}
              title="Refresh Registry"
              aria-label="Refresh Registry"
            >
              {busy ? <Spinner /> : <Icon name="refresh-cw" className="size-3.5" />}
            </Button>
          </div>
        </div>
      </header>

      {error ? (
        <div className="flex items-center justify-between border-b border-destructive/40 bg-destructive/10 px-3 py-1 text-xs text-destructive">
          <span>{error} — showing cached & local knowledge graph overlay.</span>
          <Button size="xs" variant="ghost" onClick={() => setTick((n) => n + 1)}>
            Retry
          </Button>
        </div>
      ) : null}

      {/* Main Content Area: Collapsible Left Rail + Wiki / Graph / Atlas Canvas */}
      <div className="flex min-h-0 min-w-0 flex-1 overflow-x-hidden">
        {!railCollapsed ? (
          <KnowledgeExplorerRail
            overview={overview}
            kind={kind}
            selectedId={id}
            records={records}
            loadingRecords={loadingRecords}
            search={globalSearch}
            onSearchChange={setGlobalSearch}
            lifecycle={railLifecycle}
            onLifecycleChange={setRailLifecycle}
            catalogById={catalogById}
            onSelectKind={(k) => {
              const pool = [...preloadedCatalog, ...liveRecords]
              const firstOfKind = k ? enrichRecordsPage(pool, { kind: k }, overrides)[0]?.id : "project:surface"
              go({ kind: k, id: firstOfKind || id })
            }}
            onSelectRecord={(b) => {
              selectRecordById(b.id)
              if (viewMode === "atlas") setViewMode("wiki")
            }}
            onOpenSchema={(k) => {
              setSchemaModalKind(k)
              setSchemaModalOpen(true)
            }}
          />
        ) : null}

        {viewMode === "atlas" ? (
          <DomainAtlasView
            records={records}
            selectedId={id}
            catalogById={catalogById}
            onSelectRecord={(b) => {
              selectRecordById(b.id)
              setViewMode("wiki")
            }}
            onOpenRelationshipsFor={(b) => {
              selectRecordById(b.id)
              setRelInitPredicate(undefined)
              setRelInitKind(undefined)
              setRelModalOpen(true)
            }}
          />
        ) : viewMode === "graph" && detail ? (
          <div className="min-h-0 min-w-0 flex-1 overflow-y-auto overflow-x-hidden overscroll-x-none p-5">
            <div className="mx-auto flex w-[92%] max-w-6xl min-w-0 flex-col gap-4 overflow-x-hidden">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <div className="flex items-center gap-2.5">
                  {navHistory.length > 0 ? (
                    <Button
                      size="xs"
                      variant="outline"
                      onClick={handleGoBack}
                      className="h-7 gap-1 border-cyan-500/40 bg-cyan-500/10 px-2 text-xs text-cyan-200 hover:bg-cyan-500/20"
                    >
                      <Icon name="arrow-left" className="size-3.5" />
                      Back
                    </Button>
                  ) : null}
                  <RegistryKindLineArt kind={detail.brief.kind} lifecycle={detail.brief.lifecycle} size="sm" />
                  <div>
                    <div className="flex items-center gap-2">
                      <h2 className="text-base font-bold">{formatHumanObjectName(detail.brief.id, catalogById)}</h2>
                      <Badge variant="outline" className="font-mono text-[10px]">
                        {getKindMeta(detail.brief.kind).label}
                      </Badge>
                    </div>
                    <p className="text-xs text-muted-foreground">
                      Click any connected node in the constellation to traverse the knowledge graph, or link new objects
                      below.
                    </p>
                  </div>
                </div>
                <div className="flex items-center gap-1.5">
                  <Button
                    size="xs"
                    variant="outline"
                    className="border-cyan-500/40 bg-cyan-500/10 text-cyan-200"
                    onClick={() => {
                      setRelInitPredicate(undefined)
                      setRelInitKind(undefined)
                      setRelModalOpen(true)
                    }}
                  >
                    <Icon name="plus" className="size-3.5" />
                    Link Object
                  </Button>
                  <Button size="xs" variant="secondary" onClick={() => setViewMode("wiki")}>
                    <Icon name="book-open" className="size-3.5" />
                    Open Full Wiki Dossier
                  </Button>
                </div>
              </div>

              <KnowledgeGraphCanvas
                center={{ ...detail.brief, name: formatHumanObjectName(detail.brief.id, catalogById) }}
                outgoing={detail.outgoing}
                incoming={detail.incoming}
                catalogById={catalogById}
                onSelectId={selectRecordById}
              />
            </div>
          </div>
        ) : detail ? (
          <WikiDossierView
            detail={detail}
            overrides={overrides}
            catalogById={catalogById}
            inlineEditMode={inlineEditMode}
            canGoBack={navHistory.length > 0}
            backTargetName={backTargetName}
            onGoBack={handleGoBack}
            onToggleInlineEdit={() => setInlineEditMode((v) => !v)}
            onSaveInlineBriefAndPayload={handleSaveInlineBriefAndPayload}
            onSaveWikiSection={handleSaveWikiSection}
            onOpenRelationshipsModal={(pred, tKind) => {
              setRelInitPredicate(pred)
              setRelInitKind(tKind)
              setRelModalOpen(true)
            }}
            onOpenFieldsModal={() => setFieldsModalOpen(true)}
            onOpenSchemaModal={() => {
              setSchemaModalKind(detail.brief.kind)
              setSchemaModalOpen(true)
            }}
            onOpenHistoryModal={(tab = "history") => {
              setHistoryModalTab(tab)
              setHistoryModalOpen(true)
            }}
            onRemoveEdge={handleRemoveEdge}
            onRemoveCorrelation={handleRemoveCorrelation}
            onSelectRecordId={selectRecordById}
            onSwitchToGraph={() => setViewMode("graph")}
          />
        ) : (
          <Empty className="h-full flex-1">
            <EmptyHeader>
              <EmptyTitle>Select a Knowledge Graph Node</EmptyTitle>
              <EmptyDescription>
                Choose a domain or record from the left explorer rail, or create a new record with the + button above.
              </EmptyDescription>
            </EmptyHeader>
            <Button size="sm" variant="outline" onClick={() => selectRecordById("project:surface")}>
              Open Surface Wiki Dossier
            </Button>
          </Empty>
        )}
      </div>

      {/* Icon-Triggered Modals */}
      {detail ? (
        <>
          <RelationshipLinkerModal
            open={relModalOpen}
            onOpenChange={setRelModalOpen}
            api={api}
            pub={pub}
            detail={detail}
            edgeSpecs={overview.edges}
            catalog={[...catalogById.values()]}
            overrides={overrides}
            initialPredicate={relInitPredicate}
            initialTargetKind={relInitKind}
            onAddEdge={handleAddEdge}
            onRemoveEdge={handleRemoveEdge}
            onAddCorrelation={handleAddCorrelation}
            onRemoveCorrelation={handleRemoveCorrelation}
            onSelectRecord={selectRecordById}
          />
          <FieldsInspectorModal
            open={fieldsModalOpen}
            onOpenChange={setFieldsModalOpen}
            detail={detail}
            onUpdatePayloadField={handleUpdatePayloadField}
          />
        </>
      ) : null}

      <SchemaInspectorModal
        open={schemaModalOpen}
        onOpenChange={setSchemaModalOpen}
        api={api}
        overview={overview}
        initialKind={schemaModalKind || detail?.brief.kind || kind || "project"}
      />

      <HistoryAndGovernanceModal
        open={historyModalOpen}
        onOpenChange={setHistoryModalOpen}
        api={api}
        overview={overview}
        detail={detail}
        initialTab={historyModalTab}
        onSelectRecord={selectRecordById}
      />

      <CreateRecordModal
        open={createModalOpen}
        onOpenChange={setCreateModalOpen}
        defaultKind={kind || detail?.brief.kind || "project"}
        catalog={[...catalogById.values()]}
        onCreate={handleCreateRecord}
      />
    </div>
  )
}

export default { views: { "registry/main": Registry } } satisfies ModuleWeb
