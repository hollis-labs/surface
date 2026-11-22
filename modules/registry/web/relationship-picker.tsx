import { Icon, type ModuleAPI } from "@hollis-labs/surface-page/sdk"
import { useEffect, useMemo, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import {
  DEFAULT_EDGE_SPECS,
  enrichRecordsPage,
  formatHumanObjectName,
  humanizePredicate,
  inverseFor,
  synthesizeWikiData,
} from "./overlay"
import type { Brief, CorrelationBinding, Detail, Edge, EdgeSpec, LocalRecordOverride, Page, Pub } from "./types"
import { getKindMeta } from "./types"

const RELATION_CATEGORIES: {
  id: string
  label: string
  predicates: string[]
  defaultPredicate: string
  suggestedKinds: string[]
}[] = [
  {
    id: "governance",
    label: "Governance & Ownership",
    predicates: ["owned_by", "operated_by"],
    defaultPredicate: "owned_by",
    suggestedKinds: ["agency", "agent"],
  },
  {
    id: "topology",
    label: "Topology, Services & Runtime",
    predicates: ["part_of", "deployed_as", "runs_on", "consumes", "configures"],
    defaultPredicate: "consumes",
    suggestedKinds: ["project", "service", "host", "mcp_server", "config"],
  },
  {
    id: "correlation",
    label: "Workstreams & Correlations",
    predicates: ["correlates_with"],
    defaultPredicate: "correlates_with",
    suggestedKinds: ["correlation", "workstream", "project", "service"],
  },
  {
    id: "knowledge",
    label: "Docs, Skills & Tools",
    predicates: ["documented_by", "uses_skill", "exposes_tool"],
    defaultPredicate: "documented_by",
    suggestedKinds: ["document", "knowledge", "skill", "tool"],
  },
]

export function getPredicateCategory(type: string): string {
  for (const cat of RELATION_CATEGORIES) {
    if (cat.predicates.includes(type)) return cat.id
  }
  if (type === "owns" || type === "operates") return "governance"
  if (
    type === "contains" ||
    type === "deploys" ||
    type === "hosts" ||
    type === "consumed_by" ||
    type === "configured_by"
  )
    return "topology"
  if (type === "documents" || type === "used_by" || type === "exposed_by") return "knowledge"
  return "topology"
}

export function RelationshipLinkerModal({
  open,
  onOpenChange,
  api,
  pub,
  detail,
  edgeSpecs,
  catalog,
  overrides,
  initialPredicate,
  initialTargetKind,
  onAddEdge,
  onRemoveEdge,
  onAddCorrelation,
  onRemoveCorrelation,
  onSelectRecord,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  api: ModuleAPI
  pub: Pub
  detail: Detail
  edgeSpecs?: EdgeSpec[]
  catalog: Brief[]
  overrides: Record<string, LocalRecordOverride>
  initialPredicate?: string
  initialTargetKind?: string
  onAddEdge: (predicate: string, target: Brief) => void
  onRemoveEdge: (edgeId: string) => void
  onAddCorrelation: (binding: CorrelationBinding) => void
  onRemoveCorrelation: (bindingId: string) => void
  onSelectRecord: (id: string) => void
}) {
  const specs = edgeSpecs?.length ? edgeSpecs : DEFAULT_EDGE_SPECS
  const [predicate, setPredicate] = useState(initialPredicate || "owned_by")
  const [kindFilter, setKindFilter] = useState(initialTargetKind || "*all*")
  const [query, setQuery] = useState("")
  const [remoteResults, setRemoteResults] = useState<Brief[]>([])
  const [searching, setSearching] = useState(false)

  // Correlation quick-add state
  const [corrAuthority, setCorrAuthority] = useState<CorrelationBinding["authority"]>("ops")
  const [corrKind, setCorrKind] = useState("workstream")
  const [corrRef, setCorrRef] = useState("")
  const [corrLabel, setCorrLabel] = useState("")

  useEffect(() => {
    if (initialPredicate) setPredicate(initialPredicate)
  }, [initialPredicate])

  useEffect(() => {
    if (initialTargetKind) setKindFilter(initialTargetKind)
    else {
      // Auto-pick suggested target kind if the predicate constrains it
      const spec = specs.find((s) => s.Type === predicate)
      if (spec?.TargetKinds?.length === 1) {
        setKindFilter(spec.TargetKinds[0])
      } else {
        setKindFilter("*all*")
      }
    }
  }, [initialTargetKind, predicate, specs])

  // Fetch matching records from backend when query or kindFilter changes
  useEffect(() => {
    if (!open) return
    const p = new URLSearchParams({ publication: pub })
    if (kindFilter !== "*all*") p.set("kind", kindFilter)
    if (query.trim()) p.set("q", query.trim())
    setSearching(true)
    api
      .get<Page>(`records?${p.toString()}`)
      .then((res) => {
        setRemoteResults(res.records ?? [])
      })
      .catch(() => {
        setRemoteResults([])
      })
      .finally(() => setSearching(false))
  }, [api, pub, kindFilter, query, open])

  const candidates = useMemo(() => {
    const combined = [...catalog, ...remoteResults]
    return enrichRecordsPage(
      combined,
      {
        kind: kindFilter === "*all*" ? undefined : kindFilter,
        search: query,
      },
      overrides,
    ).filter((r) => r.id !== detail.brief.id)
  }, [catalog, remoteResults, kindFilter, query, overrides, detail.brief.id])

  const catalogMap = useMemo(() => {
    const m = new Map<string, Brief>()
    for (const c of catalog) m.set(c.id, c)
    for (const r of remoteResults) m.set(r.id, r)
    return m
  }, [catalog, remoteResults])

  const activeSpec = specs.find((s) => s.Type === predicate)
  const allowedKinds = activeSpec?.TargetKinds ?? null

  // Map currently linked target IDs for this predicate
  const linkedEdgeByTarget = useMemo(() => {
    const m = new Map<string, Edge>()
    for (const e of detail.outgoing) {
      if (e.relation.type === predicate) {
        m.set(e.relation.to.id, e)
      }
    }
    return m
  }, [detail.outgoing, predicate])

  const allPredicates = useMemo(() => {
    const set = new Set<string>()
    for (const s of DEFAULT_EDGE_SPECS) set.add(s.Type)
    for (const s of specs) set.add(s.Type)
    return [...set]
  }, [specs])

  const allKinds = [
    "*all*",
    "agency",
    "project",
    "agent",
    "service",
    "workstream",
    "correlation",
    "host",
    "document",
    "skill",
    "tool",
    "config",
    "mcp_server",
  ]

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[86vh] overflow-y-auto overflow-x-hidden overscroll-x-none sm:max-w-3xl lg:max-w-4xl">
        <DialogHeader>
          <div className="flex items-center gap-2">
            <Badge variant="outline" className="border-cyan-500/40 bg-cyan-500/10 font-mono text-[11px] text-cyan-300">
              KNOWLEDGE GRAPH LINKER
            </Badge>
            <span className="font-mono text-xs text-muted-foreground">registry:{detail.brief.id}</span>
          </div>
          <DialogTitle className="text-lg">Relationships & Correlations · {detail.brief.name}</DialogTitle>
          <DialogDescription>
            Search and select objects across the Registry to link or unlink relationships, or bind external Operations,
            Torque, Cerberus, and Caster identifiers.
          </DialogDescription>
        </DialogHeader>

        <Tabs defaultValue="link-objects" className="mt-1">
          <TabsList variant="line" className="w-full justify-start border-b border-border/60">
            <TabsTrigger value="link-objects">
              <Icon name="search" className="size-3.5" />
              Search & Link Registry Objects
            </TabsTrigger>
            <TabsTrigger value="active-edges">
              <Icon name="git-fork" className="size-3.5" />
              Active Graph Edges ({detail.outgoing.length + detail.incoming.length})
            </TabsTrigger>
            <TabsTrigger value="correlations">
              <Icon name="git-merge" className="size-3.5" />
              Cross-System Correlations
            </TabsTrigger>
          </TabsList>

          {/* TAB 1: SEARCH & LINK OBJECTS */}
          <TabsContent value="link-objects" className="flex flex-col gap-3 pt-3">
            <div className="grid grid-cols-1 gap-2 rounded-lg border border-border/70 bg-muted/20 p-3 sm:grid-cols-[13rem_11rem_1fr]">
              <div className="flex flex-col gap-1">
                <span className="font-mono text-[10px] font-semibold tracking-wider text-muted-foreground uppercase">
                  Relationship Type
                </span>
                <Select value={predicate} onValueChange={(v) => v && setPredicate(String(v))}>
                  <SelectTrigger size="sm" aria-label="Relationship predicate">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {allPredicates.map((p) => (
                      <SelectItem key={p} value={p}>
                        {p} (↔ {inverseFor(p)})
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>

              <div className="flex flex-col gap-1">
                <span className="font-mono text-[10px] font-semibold tracking-wider text-muted-foreground uppercase">
                  Target Domain Kind
                </span>
                <Select value={kindFilter} onValueChange={(v) => v && setKindFilter(String(v))}>
                  <SelectTrigger size="sm" aria-label="Target domain kind">
                    <SelectValue>{(v: string) => (v === "*all*" ? "All Domains" : getKindMeta(v).plural)}</SelectValue>
                  </SelectTrigger>
                  <SelectContent>
                    {allKinds.map((k) => (
                      <SelectItem key={k} value={k}>
                        {k === "*all*" ? "All Domains" : `${getKindMeta(k).plural} (${k})`}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>

              <div className="flex flex-col gap-1">
                <span className="font-mono text-[10px] font-semibold tracking-wider text-muted-foreground uppercase">
                  Search Objects to Link
                </span>
                <div className="relative flex items-center">
                  <Input
                    value={query}
                    onChange={(e) => setQuery(e.target.value)}
                    placeholder="Filter by name, ID, owner, or summary (e.g. hollis-labs, surface, ops-host)…"
                    className="pr-8 text-xs"
                  />
                  {query ? (
                    <Button
                      size="xs"
                      variant="ghost"
                      className="absolute right-1 size-6 p-0"
                      onClick={() => setQuery("")}
                      aria-label="Clear search"
                    >
                      <Icon name="x" className="size-3" />
                    </Button>
                  ) : null}
                </div>
              </div>
            </div>

            {allowedKinds && allowedKinds.length > 0 ? (
              <div className="flex flex-wrap items-center gap-1.5 px-1 text-xs text-muted-foreground">
                <span>Recommended target kinds for</span>
                <Badge variant="outline" className="font-mono text-[11px] text-cyan-300">
                  {predicate}
                </Badge>
                <span>:</span>
                {allowedKinds.map((ak) => (
                  <Button
                    key={ak}
                    size="xs"
                    variant={kindFilter === ak ? "secondary" : "ghost"}
                    className="h-5 px-2 font-mono text-[11px]"
                    onClick={() => setKindFilter(kindFilter === ak ? "*all*" : ak)}
                  >
                    {ak}
                  </Button>
                ))}
              </div>
            ) : null}

            <div className="max-h-[22rem] overflow-y-auto rounded-lg border border-border/70 bg-background/50 p-2">
              {candidates.length === 0 ? (
                <div className="flex h-32 flex-col items-center justify-center gap-1 text-center text-xs text-muted-foreground">
                  <span>
                    {searching ? "Searching Registry knowledge graph…" : "No matching Registry objects found."}
                  </span>
                  {kindFilter !== "*all*" ? (
                    <Button size="xs" variant="outline" onClick={() => setKindFilter("*all*")}>
                      Search across all domain kinds
                    </Button>
                  ) : null}
                </div>
              ) : (
                <div className="grid grid-cols-1 gap-1.5 sm:grid-cols-2">
                  {candidates.slice(0, 40).map((cand) => {
                    const existingEdge = linkedEdgeByTarget.get(cand.id)
                    const isLinked = Boolean(existingEdge)
                    const meta = getKindMeta(cand.kind)
                    return (
                      <div
                        key={cand.id}
                        className={`flex items-start justify-between gap-2 rounded-md border p-2.5 transition-colors ${
                          isLinked
                            ? "border-cyan-500/50 bg-cyan-500/10"
                            : "border-border/60 bg-card/40 hover:border-border"
                        }`}
                      >
                        <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                          <div className="flex items-center gap-1.5">
                            <Badge variant="outline" className={`px-1.5 py-0 font-mono text-[10px] ${meta.tone}`}>
                              {meta.label}
                            </Badge>
                            <span className="truncate text-xs font-semibold text-foreground">
                              {formatHumanObjectName(cand.id, catalogMap)}
                            </span>
                          </div>
                          {cand.summary ? (
                            <p className="line-clamp-1 text-[11px] text-muted-foreground">{cand.summary}</p>
                          ) : null}
                        </div>

                        <div className="flex shrink-0 items-center gap-1">
                          {isLinked && existingEdge ? (
                            <Button
                              size="xs"
                              variant="secondary"
                              className="border border-cyan-500/40 bg-cyan-500/15 text-cyan-200 hover:bg-destructive/20 hover:text-destructive"
                              onClick={() => onRemoveEdge(existingEdge.id)}
                              title="Click to unlink relationship"
                            >
                              <Icon name="check" className="size-3" />
                              Linked
                            </Button>
                          ) : (
                            <Button
                              size="xs"
                              variant="outline"
                              onClick={() => onAddEdge(predicate, cand)}
                              title={`Link ${detail.brief.id} --[${predicate}]--> ${cand.id}`}
                            >
                              <Icon name="plus" className="size-3" />
                              Link
                            </Button>
                          )}
                        </div>
                      </div>
                    )
                  })}
                </div>
              )}
            </div>
          </TabsContent>

          {/* TAB 2: ACTIVE GRAPH EDGES */}
          <TabsContent value="active-edges" className="flex flex-col gap-3 pt-3">
            <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
              <div className="flex flex-col gap-2 rounded-lg border border-border/70 bg-card/30 p-3">
                <div className="flex items-center justify-between">
                  <span className="font-mono text-xs font-semibold tracking-wider text-muted-foreground uppercase">
                    Outgoing Relations ({detail.outgoing.length})
                  </span>
                </div>
                {detail.outgoing.length === 0 ? (
                  <p className="text-xs text-muted-foreground">No outgoing relationships.</p>
                ) : (
                  <div className="flex flex-col gap-1.5">
                    {detail.outgoing.map((e) => (
                      <div
                        key={`out-${e.id}-${e.relation.to.id}`}
                        className="flex items-center justify-between gap-2 rounded-md border border-border/60 bg-background/60 px-2.5 py-1.5"
                      >
                        <div className="flex min-w-0 flex-1 flex-wrap items-center gap-1.5">
                          <Badge variant="outline" className="font-mono text-[10px] text-cyan-300">
                            {humanizePredicate(e.relation.type)}
                          </Badge>
                          <Button
                            size="xs"
                            variant="ghost"
                            className="h-5 px-1.5 text-xs font-medium text-foreground hover:text-cyan-300"
                            onClick={() => {
                              onSelectRecord(e.relation.to.id)
                              onOpenChange(false)
                            }}
                            title={e.relation.to.id}
                          >
                            {formatHumanObjectName(e.relation.to.id, catalogMap)}
                          </Button>
                          {e.relation.status ? (
                            <Badge variant="secondary" className="text-[10px]">
                              {e.relation.status}
                            </Badge>
                          ) : null}
                        </div>
                        <Button
                          size="xs"
                          variant="ghost"
                          className="size-6 p-0 text-muted-foreground hover:text-destructive"
                          onClick={() => onRemoveEdge(e.id)}
                          aria-label={`Remove relation to ${e.relation.to.id}`}
                          title="Unlink relationship"
                        >
                          <Icon name="trash-2" className="size-3.5" />
                        </Button>
                      </div>
                    ))}
                  </div>
                )}
              </div>

              <div className="flex flex-col gap-2 rounded-lg border border-border/70 bg-card/30 p-3">
                <div className="flex items-center justify-between">
                  <span className="font-mono text-xs font-semibold tracking-wider text-muted-foreground uppercase">
                    Incoming Relations ({detail.incoming.length})
                  </span>
                </div>
                {detail.incoming.length === 0 ? (
                  <p className="text-xs text-muted-foreground">No incoming relationships.</p>
                ) : (
                  <div className="flex flex-col gap-1.5">
                    {detail.incoming.map((e) => (
                      <div
                        key={`in-${e.id}-${e.relation.from.id}`}
                        className="flex items-center justify-between gap-2 rounded-md border border-border/60 bg-background/60 px-2.5 py-1.5"
                      >
                        <div className="flex min-w-0 flex-1 flex-wrap items-center gap-1.5">
                          <Badge variant="outline" className="font-mono text-[10px] text-emerald-300">
                            {humanizePredicate(e.inverse || `${e.relation.type} (in)`)}
                          </Badge>
                          <Button
                            size="xs"
                            variant="ghost"
                            className="h-5 px-1.5 text-xs font-medium text-foreground hover:text-emerald-300"
                            onClick={() => {
                              onSelectRecord(e.relation.from.id)
                              onOpenChange(false)
                            }}
                            title={e.relation.from.id}
                          >
                            {formatHumanObjectName(e.relation.from.id, catalogMap)}
                          </Button>
                        </div>
                        <Button
                          size="xs"
                          variant="ghost"
                          className="size-6 p-0 text-muted-foreground hover:text-destructive"
                          onClick={() => onRemoveEdge(e.id)}
                          aria-label={`Remove incoming relation from ${e.relation.from.id}`}
                          title="Unlink relationship"
                        >
                          <Icon name="trash-2" className="size-3.5" />
                        </Button>
                      </div>
                    ))}
                  </div>
                )}
              </div>
            </div>
          </TabsContent>

          {/* TAB 3: CROSS-SYSTEM CORRELATIONS */}
          <TabsContent value="correlations" className="flex flex-col gap-3 pt-3">
            <div className="rounded-lg border border-border/70 bg-muted/20 p-3">
              <span className="font-mono text-xs font-semibold tracking-wider text-foreground uppercase">
                Bind External Operational Entity (Ops / Torque / Cerberus / Caster)
              </span>
              <p className="mt-0.5 text-xs text-muted-foreground">
                Connect this Registry record to an active Operations workstream, Torque project or task, Cerberus host,
                or Caster plan document.
              </p>
              <div className="mt-2.5 grid grid-cols-1 gap-2 sm:grid-cols-[9rem_9rem_1fr_1fr_auto]">
                <Select
                  value={corrAuthority}
                  onValueChange={(v) => v && setCorrAuthority(v as CorrelationBinding["authority"])}
                >
                  <SelectTrigger size="sm" aria-label="Authority">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="ops">ops (Workstream)</SelectItem>
                    <SelectItem value="torque">torque (PRJ / CW)</SelectItem>
                    <SelectItem value="cerberus">cerberus (Host)</SelectItem>
                    <SelectItem value="caster">caster (Plan Doc)</SelectItem>
                    <SelectItem value="tether">tether (Session)</SelectItem>
                  </SelectContent>
                </Select>

                <Input
                  value={corrKind}
                  onChange={(e) => setCorrKind(e.target.value)}
                  placeholder="Role (e.g. task, host)"
                  className="text-xs"
                />
                <Input
                  value={corrRef}
                  onChange={(e) => setCorrRef(e.target.value)}
                  placeholder="Ref ID (e.g. CW-20261121-0178 or PRJ-...)"
                  className="font-mono text-xs"
                />
                <Input
                  value={corrLabel}
                  onChange={(e) => setCorrLabel(e.target.value)}
                  placeholder="Display label (optional)"
                  className="text-xs"
                />
                <Button
                  size="sm"
                  disabled={!corrRef.trim()}
                  onClick={() => {
                    const cleanRef = corrRef.trim()
                    onAddCorrelation({
                      id: `corr-custom-${Date.now()}`,
                      authority: corrAuthority,
                      kind: corrKind.trim() || "ref",
                      ref: cleanRef,
                      label: corrLabel.trim() || `${corrAuthority.toUpperCase()} · ${cleanRef}`,
                      role: corrKind.trim() || "linked",
                    })
                    setCorrRef("")
                    setCorrLabel("")
                  }}
                >
                  <Icon name="plus" className="size-3.5" />
                  Bind
                </Button>
              </div>
            </div>

            <div className="flex flex-col gap-1.5 rounded-lg border border-border/70 bg-card/30 p-3">
              <span className="font-mono text-xs font-semibold tracking-wider text-muted-foreground uppercase">
                Active Correlation Bindings ({synthesizeWikiData(detail, overrides).correlationBindings.length})
              </span>
              {synthesizeWikiData(detail, overrides).correlationBindings.map((cb) => (
                <div
                  key={cb.id}
                  className="flex items-center justify-between gap-2 rounded-md border border-border/60 bg-background/60 px-2.5 py-1.5"
                >
                  <div className="flex min-w-0 flex-1 items-center gap-2">
                    <Badge variant="outline" className="font-mono text-[10px] text-emerald-300">
                      {cb.authority}:{cb.kind}
                    </Badge>
                    <span className="font-mono text-xs text-foreground">{cb.ref}</span>
                    <span className="truncate text-xs text-muted-foreground">{cb.label}</span>
                  </div>
                  <Button
                    size="xs"
                    variant="ghost"
                    className="size-6 p-0 text-muted-foreground hover:text-destructive"
                    onClick={() => onRemoveCorrelation(cb.id)}
                    aria-label={`Remove correlation ${cb.label}`}
                  >
                    <Icon name="trash-2" className="size-3.5" />
                  </Button>
                </div>
              ))}
            </div>
          </TabsContent>
        </Tabs>
      </DialogContent>
    </Dialog>
  )
}
