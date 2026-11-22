import { Icon, type ModuleAPI, RecordBody } from "@hollis-labs/surface-page/sdk"
import { useEffect, useMemo, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Markdown } from "@/components/ui/markdown"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Spinner } from "@/components/ui/spinner"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import type { Brief, Change, Detail, EdgeSpec, Kind, Overview, Prop } from "./types"
import { getKindMeta, KIND_META } from "./types"

const q = encodeURIComponent

export function tone(status?: string): "default" | "secondary" | "destructive" | "outline" {
  switch (status) {
    case "active":
    case "reviewed":
    case "published":
      return "default"
    case "deprecated":
    case "retired":
    case "deleted":
    case "denied":
    case "quarantined":
      return "destructive"
    case "experimental":
    case "unreviewed":
    case "pending":
    case "proposed":
    case "needs_human":
      return "secondary"
  }
  return "outline"
}

export function ago(iso?: string): string {
  if (!iso) return ""
  const t = new Date(iso).getTime()
  if (Number.isNaN(t)) return iso
  const s = Math.round((Date.now() - t) / 1000)
  if (s < 60) return "just now"
  if (s < 3600) return `${Math.round(s / 60)}m ago`
  if (s < 86400) return `${Math.round(s / 3600)}h ago`
  return `${Math.round(s / 86400)}d ago`
}

function describeProp(p: Prop): string {
  if (p.const !== undefined) return `= ${JSON.stringify(p.const)}`
  if (p.enum) return p.enum.map((v) => JSON.stringify(v)).join(" | ")
  const t = Array.isArray(p.type) ? p.type.join(" | ") : (p.type ?? "any")
  if (t === "array" && p.items) return `list of ${describeProp(p.items)}`
  return p.pattern ? `${t} /${p.pattern}/` : t
}

/**
 * Icon-triggered Fields, Structured Payload & Raw JSON Inspector Modal (`sliders-horizontal` icon).
 * Supports inline field editing & adding custom key-value attributes without a heavy CRUD form.
 */
export function FieldsInspectorModal({
  open,
  onOpenChange,
  detail,
  onUpdatePayloadField,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  detail: Detail
  onUpdatePayloadField: (key: string, value: unknown) => void
}) {
  const b = detail.brief
  const rec = detail.record
  const payload = (rec.payload ?? {}) as Record<string, unknown>
  const [editingKey, setEditingKey] = useState<string | null>(null)
  const [editVal, setEditVal] = useState("")
  const [newKey, setNewKey] = useState("")
  const [newVal, setNewVal] = useState("")

  const payloadJson = useMemo(() => JSON.stringify(payload, null, 2), [payload])
  const rawJson = useMemo(() => JSON.stringify(rec, null, 2), [rec])

  const entries = useMemo(() => Object.entries(payload).sort(([a], [b]) => a.localeCompare(b)), [payload])

  const commitField = (k: string, rawStr: string) => {
    let parsed: unknown = rawStr
    const trimmed = rawStr.trim()
    if (
      (trimmed.startsWith("{") && trimmed.endsWith("}")) ||
      (trimmed.startsWith("[") && trimmed.endsWith("]")) ||
      trimmed === "true" ||
      trimmed === "false" ||
      (trimmed !== "" && !Number.isNaN(Number(trimmed)))
    ) {
      try {
        parsed = JSON.parse(trimmed)
      } catch {
        parsed = rawStr
      }
    }
    onUpdatePayloadField(k, parsed)
    setEditingKey(null)
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[86vh] overflow-y-auto overflow-x-hidden overscroll-x-none sm:max-w-3xl lg:max-w-4xl">
        <DialogHeader>
          <div className="flex items-center gap-2">
            <Badge variant="outline" className="border-cyan-500/40 bg-cyan-500/10 font-mono text-[11px] text-cyan-300">
              STRUCTURED FIELDS & PAYLOAD
            </Badge>
            <span className="font-mono text-xs text-muted-foreground">registry:{b.id}</span>
          </div>
          <DialogTitle className="text-lg">Fields & Raw Record · {b.name}</DialogTitle>
          <DialogDescription>
            Click any field value below to edit it inline, add a structured attribute, or inspect the raw Registry JSON
            envelope.
          </DialogDescription>
        </DialogHeader>

        <Tabs defaultValue="inline-fields" className="mt-1">
          <TabsList variant="line" className="w-full justify-start border-b border-border/60">
            <TabsTrigger value="inline-fields">
              <Icon name="sliders-horizontal" className="size-3.5" />
              Inline Structured Fields ({entries.length})
            </TabsTrigger>
            <TabsTrigger value="record-body">
              <Icon name="list-tree" className="size-3.5" />
              Record Envelope & Metadata
            </TabsTrigger>
            <TabsTrigger value="raw-json">
              <Icon name="code-2" className="size-3.5" />
              Raw JSON
            </TabsTrigger>
          </TabsList>

          <TabsContent value="inline-fields" className="flex flex-col gap-3 pt-3">
            <Table className="table-fixed">
              <TableHeader>
                <TableRow>
                  <TableHead className="w-1/3">Field Key</TableHead>
                  <TableHead>Value (Click to edit inline)</TableHead>
                  <TableHead className="w-20 text-right">Action</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {entries.map(([k, v]) => {
                  const isEditing = editingKey === k
                  const formatted = typeof v === "string" ? v : JSON.stringify(v)
                  return (
                    <TableRow key={k}>
                      <TableCell className="align-top font-mono text-xs font-medium text-cyan-300">{k}</TableCell>
                      <TableCell className="align-top font-mono text-xs break-all whitespace-pre-wrap">
                        {isEditing ? (
                          <div className="flex items-center gap-1.5">
                            <Input
                              value={editVal}
                              onChange={(e) => setEditVal(e.target.value)}
                              className="h-7 font-mono text-xs"
                              onKeyDown={(e) => {
                                if (e.key === "Enter") commitField(k, editVal)
                                if (e.key === "Escape") setEditingKey(null)
                              }}
                            />
                            <Button size="xs" onClick={() => commitField(k, editVal)}>
                              Save
                            </Button>
                            <Button size="xs" variant="ghost" onClick={() => setEditingKey(null)}>
                              Cancel
                            </Button>
                          </div>
                        ) : (
                          <span className="text-foreground">{formatted}</span>
                        )}
                      </TableCell>
                      <TableCell className="align-top text-right">
                        {!isEditing ? (
                          <Button
                            size="xs"
                            variant="ghost"
                            onClick={() => {
                              setEditingKey(k)
                              setEditVal(formatted)
                            }}
                            title={`Edit ${k}`}
                          >
                            <Icon name="pencil" className="size-3" />
                          </Button>
                        ) : null}
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>

            <div className="flex flex-wrap items-center gap-2 rounded-lg border border-border/70 bg-muted/20 p-2.5">
              <span className="font-mono text-[11px] font-semibold text-muted-foreground uppercase">+ Add Field:</span>
              <Input
                value={newKey}
                onChange={(e) => setNewKey(e.target.value)}
                placeholder="field_name (e.g. endpoint, tier)"
                className="h-7 w-44 font-mono text-xs"
              />
              <Input
                value={newVal}
                onChange={(e) => setNewVal(e.target.value)}
                placeholder="Field value or JSON"
                className="h-7 min-w-48 flex-1 font-mono text-xs"
              />
              <Button
                size="xs"
                disabled={!newKey.trim()}
                onClick={() => {
                  commitField(newKey.trim(), newVal)
                  setNewKey("")
                  setNewVal("")
                }}
              >
                <Icon name="plus" className="size-3" />
                Add Attribute
              </Button>
            </div>
          </TabsContent>

          <TabsContent value="record-body" className="flex flex-col gap-3 pt-3">
            <div className="grid grid-cols-2 gap-2 rounded-lg border border-border/70 bg-muted/20 p-3 text-xs sm:grid-cols-4">
              <div>
                <span className="text-muted-foreground">Key</span>
                <p className="truncate font-mono text-foreground">{b.key ?? b.id}</p>
              </div>
              <div>
                <span className="text-muted-foreground">Authority</span>
                <p className="truncate font-mono text-foreground">{b.authority ?? "registry"}</p>
              </div>
              <div>
                <span className="text-muted-foreground">Revision ID</span>
                <p className="truncate font-mono text-foreground">{b.revision_id ?? "working"}</p>
              </div>
              <div>
                <span className="text-muted-foreground">Version Token</span>
                <p className="truncate font-mono text-foreground">{b.version_token ?? "draft"}</p>
              </div>
            </div>
            <RecordBody body={payloadJson} />
          </TabsContent>

          <TabsContent value="raw-json" className="pt-3">
            <Markdown>{`\`\`\`json\n${rawJson}\n\`\`\``}</Markdown>
          </TabsContent>
        </Tabs>
      </DialogContent>
    </Dialog>
  )
}

/**
 * Icon-triggered Schema & Ontology Inspector Modal (`braces` icon).
 */
export function SchemaInspectorModal({
  open,
  onOpenChange,
  api,
  overview,
  initialKind,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  api: ModuleAPI
  overview: Overview
  initialKind?: string
}) {
  const activeKindName = initialKind || overview.kinds[0]?.kind || "project"
  const [selectedKind, setSelectedKind] = useState(activeKindName)
  const kindObj: Kind = useMemo(
    () =>
      overview.kinds.find((k) => k.kind === selectedKind) ?? {
        kind: selectedKind,
        schemas: [getKindMeta(selectedKind).defaultSchema],
        count: 0,
      },
    [overview.kinds, selectedKind],
  )

  const [schema, setSchema] = useState(kindObj.schemas[kindObj.schemas.length - 1] ?? "")
  const [doc, setDoc] = useState<{ properties?: Record<string, Prop>; required?: string[] } | null>(null)
  const [error, setError] = useState("")

  useEffect(() => {
    if (initialKind) setSelectedKind(initialKind)
  }, [initialKind])

  useEffect(() => {
    setSchema(kindObj.schemas[kindObj.schemas.length - 1] ?? "")
  }, [kindObj])

  useEffect(() => {
    if (!open || !schema) return
    setDoc(null)
    api
      .get<{ properties?: Record<string, Prop>; required?: string[] }>(`schema?schema=${q(schema)}`)
      .then((d) => {
        setDoc(d)
        setError("")
      })
      .catch((e: Error) => setError(e.message))
  }, [api, schema, open])

  const out = (overview.edges ?? []).filter((e) => !e.SourceKinds || e.SourceKinds.includes(kindObj.kind))
  const into = (overview.edges ?? []).filter((e) => !e.TargetKinds || e.TargetKinds.includes(kindObj.kind))
  const required = new Set(doc?.required ?? [])
  const props = Object.entries(doc?.properties ?? {}).sort(
    ([a], [b]) => Number(required.has(b)) - Number(required.has(a)) || a.localeCompare(b),
  )

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[86vh] overflow-y-auto overflow-x-hidden overscroll-x-none sm:max-w-3xl lg:max-w-4xl">
        <DialogHeader>
          <div className="flex flex-wrap items-center justify-between gap-2 pr-6">
            <div className="flex items-center gap-2">
              <Badge
                variant="outline"
                className="border-violet-500/40 bg-violet-500/10 font-mono text-[11px] text-violet-300"
              >
                ONTOLOGY & SCHEMA SPECIFICATION
              </Badge>
              <Badge variant="secondary" className="font-mono text-xs">
                {schema || `${selectedKind}/1`}
              </Badge>
            </div>
            <div className="flex items-center gap-1.5">
              <Select value={selectedKind} onValueChange={(v) => v && setSelectedKind(String(v))}>
                <SelectTrigger size="sm" aria-label="Domain kind" className="w-40">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {overview.kinds.map((k) => (
                    <SelectItem key={k.kind} value={k.kind}>
                      {k.kind} ({k.count})
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              {kindObj.schemas.length > 1 ? (
                <Select value={schema} onValueChange={(v) => v && setSchema(String(v))}>
                  <SelectTrigger size="sm" aria-label="Schema version" className="w-32">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {kindObj.schemas.map((s) => (
                      <SelectItem key={s} value={s}>
                        {s}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              ) : null}
            </div>
          </div>
          <DialogTitle className="text-lg">
            {getKindMeta(selectedKind).label} Ontology ({selectedKind})
          </DialogTitle>
          <DialogDescription>{getKindMeta(selectedKind).description}</DialogDescription>
        </DialogHeader>

        <Tabs defaultValue="fields" className="mt-1">
          <TabsList variant="line" className="w-full justify-start border-b border-border/60">
            <TabsTrigger value="fields">Schema Fields ({props.length})</TabsTrigger>
            <TabsTrigger value="edges">Allowed Graph Edges ({out.length + into.length})</TabsTrigger>
            <TabsTrigger value="json">Raw Schema JSON</TabsTrigger>
          </TabsList>

          <TabsContent value="fields" className="pt-3">
            {error ? <p className="text-xs text-destructive">{error}</p> : null}
            {doc === null && !error ? (
              <div className="grid h-24 place-items-center">
                <Spinner />
              </div>
            ) : (
              <Table className="table-fixed">
                <TableHeader>
                  <TableRow>
                    <TableHead className="w-1/3">Property</TableHead>
                    <TableHead>Constraint / Type</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {props.map(([name, p]) => (
                    <TableRow key={name}>
                      <TableCell className="font-mono text-xs whitespace-normal">
                        <div className="flex flex-wrap items-center gap-1.5">
                          <span className="font-semibold text-foreground">{name}</span>
                          {required.has(name) ? (
                            <Badge variant="secondary" className="text-[10px]">
                              required
                            </Badge>
                          ) : null}
                        </div>
                      </TableCell>
                      <TableCell className="font-mono text-xs break-all whitespace-normal text-muted-foreground">
                        {describeProp(p)}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </TabsContent>

          <TabsContent value="edges" className="flex flex-col gap-4 pt-3">
            <div>
              <h4 className="mb-1.5 font-mono text-xs font-semibold tracking-wider text-muted-foreground uppercase">
                Outgoing Predicates (From {selectedKind})
              </h4>
              <EdgeSpecsTable specs={out} side="TargetKinds" />
            </div>
            <div>
              <h4 className="mb-1.5 font-mono text-xs font-semibold tracking-wider text-muted-foreground uppercase">
                Incoming Predicates (To {selectedKind})
              </h4>
              <EdgeSpecsTable specs={into} side="SourceKinds" />
            </div>
          </TabsContent>

          <TabsContent value="json" className="pt-3">
            {doc ? <Markdown>{`\`\`\`json\n${JSON.stringify(doc, null, 2)}\n\`\`\``}</Markdown> : null}
          </TabsContent>
        </Tabs>
      </DialogContent>
    </Dialog>
  )
}

function EdgeSpecsTable({ specs, side }: { specs: EdgeSpec[]; side: "SourceKinds" | "TargetKinds" }) {
  if (specs.length === 0) return <p className="text-xs text-muted-foreground">None.</p>
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Predicate</TableHead>
          <TableHead>Inverse</TableHead>
          <TableHead>{side === "TargetKinds" ? "Valid Target Kinds" : "Valid Source Kinds"}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {specs.map((s) => (
          <TableRow key={`${side}-${s.Type}`}>
            <TableCell className="font-mono text-xs text-cyan-300">{s.Type}</TableCell>
            <TableCell className="font-mono text-xs text-muted-foreground">{s.Inverse || "—"}</TableCell>
            <TableCell className="text-xs">{s[side]?.join(", ") ?? "any kind"}</TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}

/**
 * Icon-triggered Revision History, Diff, Review Queue & Diagnostics Modal (`history` / `shield-check` icon).
 */
export function HistoryAndGovernanceModal({
  open,
  onOpenChange,
  api,
  overview,
  detail,
  initialTab = "history",
  onSelectRecord,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  api: ModuleAPI
  overview: Overview
  detail: Detail | null
  initialTab?: "history" | "review" | "diagnostics"
  onSelectRecord: (id: string) => void
}) {
  const revisions = detail?.revisions ?? []
  const [from, setFrom] = useState(revisions[1]?.revision_id ?? "")
  const [to, setTo] = useState(revisions[0]?.revision_id ?? "")
  const [changes, setChanges] = useState<Change[] | null>(null)
  const [error, setError] = useState("")

  useEffect(() => {
    setFrom(revisions[1]?.revision_id ?? "")
    setTo(revisions[0]?.revision_id ?? "")
    setChanges(null)
  }, [revisions])

  const items = revisions.map((r) => ({ value: r.revision_id, label: `#${r.sequence} · ${ago(r.updated_at)}` }))

  const compare = () => {
    if (!detail) return
    api
      .get<Change[]>(`diff?id=${q(detail.brief.id)}&from=${q(from)}&to=${q(to)}`)
      .then((c) => {
        setChanges(c)
        setError("")
      })
      .catch((e: Error) => setError(e.message))
  }

  const d = overview.diagnostics

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[86vh] overflow-y-auto overflow-x-hidden overscroll-x-none sm:max-w-3xl lg:max-w-4xl">
        <DialogHeader>
          <div className="flex items-center gap-2">
            <Badge
              variant="outline"
              className="border-emerald-500/40 bg-emerald-500/10 font-mono text-[11px] text-emerald-300"
            >
              PROVENANCE, REVISIONS & GOVERNANCE
            </Badge>
            {detail ? (
              <span className="font-mono text-xs text-muted-foreground">registry:{detail.brief.id}</span>
            ) : null}
          </div>
          <DialogTitle className="text-lg">
            {detail ? `History & Governance · ${detail.brief.name}` : "Registry Governance & Diagnostics"}
          </DialogTitle>
          <DialogDescription>
            Inspect revision history, compare field diffs across versions, review pending tool/relation approvals, and
            verify Registry storage health.
          </DialogDescription>
        </DialogHeader>

        <Tabs defaultValue={detail ? initialTab : "diagnostics"} className="mt-1">
          <TabsList variant="line" className="w-full justify-start border-b border-border/60">
            {detail ? (
              <TabsTrigger value="history">
                <Icon name="history" className="size-3.5" />
                Revisions & Diff ({revisions.length})
              </TabsTrigger>
            ) : null}
            <TabsTrigger value="review">
              <Icon name="shield-check" className="size-3.5" />
              Review Queue ({overview.reviews.length})
            </TabsTrigger>
            <TabsTrigger value="diagnostics">
              <Icon name="activity" className="size-3.5" />
              Storage & Schema Diagnostics
            </TabsTrigger>
          </TabsList>

          {detail ? (
            <TabsContent value="history" className="flex flex-col gap-3 pt-3">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>#</TableHead>
                    <TableHead>Updated</TableHead>
                    <TableHead>Lifecycle</TableHead>
                    <TableHead>Revision Hash</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {revisions.map((r) => (
                    <TableRow key={r.revision_id}>
                      <TableCell className="font-mono text-xs">{r.sequence}</TableCell>
                      <TableCell className="text-xs" title={r.updated_at}>
                        {ago(r.updated_at)}
                      </TableCell>
                      <TableCell>
                        {r.lifecycle ? <Badge variant={tone(r.lifecycle)}>{r.lifecycle}</Badge> : null}
                      </TableCell>
                      <TableCell className="max-w-56 truncate font-mono text-xs text-muted-foreground">
                        {r.revision_id}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>

              {revisions.length >= 2 ? (
                <div className="flex flex-col gap-2 rounded-lg border border-border/70 bg-muted/20 p-3">
                  <span className="font-mono text-xs font-semibold text-muted-foreground uppercase">
                    Compare Revisions
                  </span>
                  <div className="flex items-center gap-2">
                    <Select value={from} onValueChange={(v) => setFrom(String(v ?? ""))} items={items}>
                      <SelectTrigger size="sm" aria-label="Older revision" className="min-w-0 flex-1">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        {items.map((o) => (
                          <SelectItem key={o.value} value={o.value}>
                            {o.label}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                    <Icon name="arrow-right" className="size-4 shrink-0 text-muted-foreground" />
                    <Select value={to} onValueChange={(v) => setTo(String(v ?? ""))} items={items}>
                      <SelectTrigger size="sm" aria-label="Newer revision" className="min-w-0 flex-1">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        {items.map((o) => (
                          <SelectItem key={o.value} value={o.value}>
                            {o.label}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => void compare()}
                      disabled={!from || !to || from === to}
                    >
                      <Icon name="git-compare" className="size-3.5" />
                      Compare
                    </Button>
                  </div>
                  {error ? <p className="text-xs text-destructive">{error}</p> : null}
                  {changes !== null ? (
                    changes.length === 0 ? (
                      <p className="text-xs text-muted-foreground">No field differences between selected revisions.</p>
                    ) : (
                      <Table className="table-fixed">
                        <TableHeader>
                          <TableRow>
                            <TableHead className="w-2/5">Path</TableHead>
                            <TableHead>Before</TableHead>
                            <TableHead>After</TableHead>
                          </TableRow>
                        </TableHeader>
                        <TableBody>
                          {changes.map((c) => (
                            <TableRow key={c.path}>
                              <TableCell className="align-top font-mono text-xs break-all whitespace-normal">
                                <Badge
                                  variant={
                                    c.op === "removed" ? "destructive" : c.op === "added" ? "default" : "secondary"
                                  }
                                >
                                  {c.op}
                                </Badge>{" "}
                                {c.path}
                              </TableCell>
                              <TableCell className="align-top text-xs break-all whitespace-pre-wrap text-muted-foreground line-through">
                                {c.before}
                              </TableCell>
                              <TableCell className="align-top text-xs break-all whitespace-pre-wrap">
                                {c.after}
                              </TableCell>
                            </TableRow>
                          ))}
                        </TableBody>
                      </Table>
                    )
                  ) : null}
                </div>
              ) : null}
            </TabsContent>
          ) : null}

          <TabsContent value="review" className="flex flex-col gap-3 pt-3">
            {detail?.brief.review ? (
              <div className="rounded-lg border border-border/70 bg-muted/20 p-3">
                <span className="font-mono text-xs font-semibold text-foreground uppercase">
                  Active Record Review State · {detail.brief.id}
                </span>
                <div className="mt-2 grid grid-cols-2 gap-2 text-xs sm:grid-cols-3">
                  <div>
                    <span className="text-muted-foreground">State</span>
                    <p>
                      <Badge variant={tone(detail.brief.review.state)}>{detail.brief.review.state ?? "pending"}</Badge>
                    </p>
                  </div>
                  <div>
                    <span className="text-muted-foreground">Queue</span>
                    <p className="font-mono">{detail.brief.review.queue_state ?? "—"}</p>
                  </div>
                  <div>
                    <span className="text-muted-foreground">Reviewed By</span>
                    <p className="font-mono">{detail.brief.review.reviewed_by ?? "unreviewed"}</p>
                  </div>
                </div>
              </div>
            ) : null}

            <div className="flex flex-col gap-2">
              <span className="font-mono text-xs font-semibold text-muted-foreground uppercase">
                Global Pending Review Queue ({overview.reviews.length})
              </span>
              {overview.reviews.length === 0 ? (
                <p className="text-xs text-muted-foreground">No tools or records are currently waiting for review.</p>
              ) : (
                <div className="flex flex-col gap-1.5">
                  {overview.reviews.map((r) => (
                    <div
                      key={r.id}
                      className="flex items-center justify-between rounded-md border border-border/60 bg-card/40 px-3 py-2"
                    >
                      <div className="flex min-w-0 flex-col">
                        <span className="truncate text-xs font-semibold">{r.name}</span>
                        <span className="truncate font-mono text-[11px] text-muted-foreground">
                          {r.id} · {r.review?.state ?? "pending"}
                        </span>
                      </div>
                      <Button
                        size="xs"
                        variant="outline"
                        onClick={() => {
                          onSelectRecord(r.id)
                          onOpenChange(false)
                        }}
                      >
                        Inspect
                      </Button>
                    </div>
                  ))}
                </div>
              )}
            </div>
          </TabsContent>

          <TabsContent value="diagnostics" className="flex flex-col gap-3 pt-3">
            {d ? (
              <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
                <div className="rounded-lg border border-border/70 bg-card/40 p-3">
                  <span className="font-mono text-[10px] text-muted-foreground uppercase">Gateway Status</span>
                  <div className="mt-1">
                    <Badge variant={d.ready ? "default" : "destructive"}>{d.ready ? "READY" : "NOT READY"}</Badge>
                  </div>
                </div>
                <div className="rounded-lg border border-border/70 bg-card/40 p-3">
                  <span className="font-mono text-[10px] text-muted-foreground uppercase">Storage Engine</span>
                  <p className="mt-1 font-mono text-sm font-semibold">{d.storage ?? "sqlite"}</p>
                </div>
                <div className="rounded-lg border border-border/70 bg-card/40 p-3">
                  <span className="font-mono text-[10px] text-muted-foreground uppercase">Registered Schemas</span>
                  <p className="mt-1 font-mono text-sm font-semibold">{d.schemas?.length ?? 0}</p>
                </div>
                <div className="rounded-lg border border-border/70 bg-card/40 p-3">
                  <span className="font-mono text-[10px] text-muted-foreground uppercase">Last Write Snapshot</span>
                  <p className="mt-1 font-mono text-xs">
                    {overview.as_of && !overview.as_of.startsWith("1970") ? ago(overview.as_of) : "working copy"}
                  </p>
                </div>
              </div>
            ) : (
              <p className="text-xs text-muted-foreground">Diagnostics unavailable.</p>
            )}
          </TabsContent>
        </Tabs>
      </DialogContent>
    </Dialog>
  )
}

/**
 * Icon-triggered Create New Wiki Record Modal (`plus` icon in top header).
 * Creates a record and immediately opens it in Inline Edit Mode on the Wiki canvas.
 */
export function CreateRecordModal({
  open,
  onOpenChange,
  defaultKind,
  catalog,
  onCreate,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  defaultKind?: string
  catalog: Brief[]
  onCreate: (brief: Brief, initialOverviewMd: string) => void
}) {
  const [kind, setKind] = useState(defaultKind || "project")
  const [name, setName] = useState("")
  const [slug, setSlug] = useState("")
  const [owner, setOwner] = useState("agency:hollis-labs")
  const [lifecycle, setLifecycle] = useState("active")
  const [summary, setSummary] = useState("")

  useEffect(() => {
    if (defaultKind) setKind(defaultKind)
  }, [defaultKind])

  const computedSlug =
    slug.trim() ||
    name
      .trim()
      .toLowerCase()
      .replace(/[^a-z0-9._-]+/g, "-")
      .replace(/^-+|-+$/g, "")

  const ownerCandidates = useMemo(
    () => catalog.filter((r) => r.kind === "agency" || r.kind === "agent").slice(0, 25),
    [catalog],
  )

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <div className="flex items-center gap-2">
            <Badge variant="outline" className="border-cyan-500/40 bg-cyan-500/10 font-mono text-[11px] text-cyan-300">
              NEW KNOWLEDGE GRAPH NODE
            </Badge>
          </div>
          <DialogTitle className="text-lg">Create Wiki Record</DialogTitle>
          <DialogDescription>
            Bootstrap a new entity in the Registry knowledge graph. It will open directly in inline-edit mode so you can
            author its wiki sections and link relationships in place.
          </DialogDescription>
        </DialogHeader>

        <div className="flex flex-col gap-3 pt-2">
          <div className="grid grid-cols-2 gap-2.5">
            <div className="flex flex-col gap-1">
              <span className="font-mono text-[10px] font-semibold text-muted-foreground uppercase">Domain Kind</span>
              <Select value={kind} onValueChange={(v) => v && setKind(String(v))}>
                <SelectTrigger size="sm" aria-label="Kind">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {Object.keys(KIND_META).map((k) => (
                    <SelectItem key={k} value={k}>
                      {KIND_META[k].label} ({k})
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            <div className="flex flex-col gap-1">
              <span className="font-mono text-[10px] font-semibold text-muted-foreground uppercase">Lifecycle</span>
              <Select value={lifecycle} onValueChange={(v) => v && setLifecycle(String(v))}>
                <SelectTrigger size="sm" aria-label="Lifecycle">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {["active", "experimental", "deprecated", "retired"].map((l) => (
                    <SelectItem key={l} value={l}>
                      {l}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>

          <div className="grid grid-cols-1 gap-2.5 sm:grid-cols-2">
            <div className="flex flex-col gap-1">
              <span className="font-mono text-[10px] font-semibold text-muted-foreground uppercase">Display Name</span>
              <Input
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="e.g. Tachyon Event Bus"
                className="text-xs"
              />
            </div>

            <div className="flex flex-col gap-1">
              <span className="font-mono text-[10px] font-semibold text-muted-foreground uppercase">
                Canonical ID ({kind}:…)
              </span>
              <Input
                value={slug}
                onChange={(e) => setSlug(e.target.value)}
                placeholder={computedSlug || "tachyon-event-bus"}
                className="font-mono text-xs"
              />
            </div>
          </div>

          <div className="flex flex-col gap-1">
            <span className="font-mono text-[10px] font-semibold text-muted-foreground uppercase">
              Governance Owner (Agency or Agent)
            </span>
            <Select value={owner} onValueChange={(v) => v && setOwner(String(v))}>
              <SelectTrigger size="sm" aria-label="Owner">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="agency:hollis-labs">agency:hollis-labs (Hollis Labs)</SelectItem>
                <SelectItem value="agency:personal">agency:personal (Personal)</SelectItem>
                <SelectItem value="user:chrispian">user:chrispian</SelectItem>
                {ownerCandidates
                  .filter((c) => c.id !== "agency:hollis-labs" && c.id !== "agency:personal")
                  .map((c) => (
                    <SelectItem key={c.id} value={c.id}>
                      {c.id} ({c.name})
                    </SelectItem>
                  ))}
              </SelectContent>
            </Select>
          </div>

          <div className="flex flex-col gap-1">
            <span className="font-mono text-[10px] font-semibold text-muted-foreground uppercase">
              One-Line Summary / Purpose
            </span>
            <Input
              value={summary}
              onChange={(e) => setSummary(e.target.value)}
              placeholder="Concise operational purpose of this system entity…"
              className="text-xs"
            />
          </div>

          <div className="mt-1 flex items-center justify-end gap-2">
            <Button size="sm" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button
              size="sm"
              disabled={!name.trim() || !computedSlug}
              onClick={() => {
                const fullId = `${kind}:${computedSlug}`
                const meta = getKindMeta(kind)
                const newBrief: Brief = {
                  id: fullId,
                  kind,
                  schema: meta.defaultSchema,
                  key: computedSlug,
                  name: name.trim(),
                  summary: summary.trim() || `${meta.label} record for ${name.trim()}`,
                  owner,
                  lifecycle,
                  authority: "registry",
                  publication: "draft",
                  updated_at: new Date().toISOString(),
                  revision_id: `local-${Date.now()}`,
                  version_token: `vt-${Date.now()}`,
                  edges: owner.includes(":") && !owner.startsWith("user:") ? 1 : 0,
                  isLocalOverlay: true,
                }
                onCreate(
                  newBrief,
                  `**${name.trim()}** (\`registry:${fullId}\`) is a ${lifecycle} **${meta.label}** owned by \`${owner}\`.\n\n${summary.trim()}`,
                )
                setName("")
                setSlug("")
                setSummary("")
                onOpenChange(false)
              }}
            >
              <Icon name="plus" className="size-3.5" />
              Create & Edit Wiki Page
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}
