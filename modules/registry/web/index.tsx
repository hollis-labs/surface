/**
 * The Registry's window: a read-only browser. Kinds with their counts, the review queue and the
 * diagnostics on the left; the records of the selected kind in the middle (filtered by lifecycle
 * and owner, or searched); the record on the right: its fields, relations both ways, its history
 * with a diff between two revisions, its review, and the raw JSON. A kind's schema shows there too.
 *
 * Nothing here changes the Registry: the owner asks the agent, which has the registry_* tools.
 * What the window shows is in its args (kind, id, publication), so the agent knows the record in
 * front. It refreshes on its button, and every 30 seconds while the window is in front.
 */
import {
  CiteRef,
  CiteText,
  Icon,
  type ModuleAPI,
  type ModuleWeb,
  RecordBody,
  type ViewProps,
} from "@hollis-labs/surface-page/sdk"
import { type ReactNode, useCallback, useEffect, useMemo, useRef, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty"
import { Input } from "@/components/ui/input"
import { Item, ItemContent, ItemDescription, ItemTitle } from "@/components/ui/item"
import { Markdown } from "@/components/ui/markdown"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Spinner } from "@/components/ui/spinner"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"

// ---------- the module's API ----------

type Pub = "draft" | "published"

interface Review {
  state?: string
  queue_state?: string
  definition_hash?: string
  proposed_by?: string
  reviewed_by?: string
  reviewed_at?: string
  reason?: string
}

interface Brief {
  id: string
  kind: string
  schema: string
  key?: string
  name: string
  summary?: string
  owner?: string
  subtype?: string
  lifecycle?: string
  authority?: string
  publication?: string
  updated_at?: string
  revision_id?: string
  version_token?: string
  review?: Review
  edges: number
  proposed?: number
}

interface Kind {
  kind: string
  schemas: string[]
  count: number
  more?: boolean
}

interface EdgeSpec {
  Type: string
  Inverse: string
  SourceKinds: string[] | null
  TargetKinds: string[] | null
}

interface Overview {
  publication: Pub
  kinds: Kind[]
  edges?: EdgeSpec[]
  reviews: Brief[]
  diagnostics?: { ready?: boolean; storage?: string; schemas?: string[] }
  total: number
  as_of?: string
  errors?: Record<string, string>
}

interface Page {
  records: Brief[]
  cursor?: string
  has_more: boolean
}

interface Ref {
  authority: string
  kind: string
  id: string
  uri?: string
}

interface Edge {
  id: string
  relation: { type: string; from: Ref; to: Ref; status?: string; source?: string; weight?: number }
  inverse?: string
  target_state?: string
}

interface Revision {
  revision_id: string
  version_token?: string
  sequence: number
  updated_at?: string
  lifecycle?: string
  publication?: string
  name?: string
}

interface Detail {
  record: Record<string, unknown> & { payload?: unknown }
  brief: Brief
  publication: Pub
  outgoing: Edge[]
  incoming: Edge[]
  revisions: Revision[]
  more_history?: boolean
  errors?: Record<string, string>
}

interface Change {
  path: string
  op: "added" | "removed" | "changed"
  before?: string
  after?: string
}

const q = encodeURIComponent

// ---------- small parts ----------

const LIFECYCLES = ["experimental", "active", "deprecated", "retired", "archived"]
const ANY = "*any*"

function tone(status?: string): "default" | "secondary" | "destructive" | "outline" {
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

function ago(iso?: string): string {
  if (!iso) return ""
  const t = new Date(iso).getTime()
  if (Number.isNaN(t)) return iso
  const s = Math.round((Date.now() - t) / 1000)
  if (s < 60) return "just now"
  if (s < 3600) return `${Math.round(s / 60)}m ago`
  if (s < 86400) return `${Math.round(s / 3600)}h ago`
  return `${Math.round(s / 86400)}d ago`
}

/** A Registry id as a chip that opens it; anything else (an owner like user:x) as text. */
function RecordRef({ id, known }: { id: string; known?: boolean }) {
  const kind = id.split(":")[0]
  if (
    known ||
    /^(project|tool|agent|agency|skill|document|workstream|correlation|service|config|knowledge|mcp_server|launch_profile):/.test(
      id,
    )
  )
    return <CiteRef refId={{ kind: "registry.record", canonical: id }} raw={id} mono />
  return (
    <span className="font-mono text-xs" title={kind}>
      {id}
    </span>
  )
}

/** The far end of a relation: a local record as its chip, an outside one as text (its own chip if Cite knows it). */
function EndRef({ r }: { r: Ref }) {
  if (r.authority === "registry") return <RecordRef id={r.id} known />
  return (
    <span className="inline-flex items-center gap-1 text-xs">
      <Badge variant="outline">{r.authority}</Badge>
      <CiteText text={r.id} />
    </span>
  )
}

function Section({ title, children, right }: { title: string; children: ReactNode; right?: ReactNode }) {
  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex items-center gap-2 px-1">
        <span className="text-xs font-semibold tracking-wide text-muted-foreground uppercase">{title}</span>
        <span className="ml-auto">{right}</span>
      </div>
      {children}
    </div>
  )
}

function Facts({ rows }: { rows: [string, ReactNode][] }) {
  return (
    <dl className="grid grid-cols-[max-content_1fr] gap-x-4 gap-y-1 text-sm">
      {rows.map(([k, v]) => (
        <div key={k} className="contents">
          <dt className="text-muted-foreground">{k}</dt>
          <dd className="min-w-0 break-all">{v}</dd>
        </div>
      ))}
    </dl>
  )
}

function Problem({ text }: { text?: string }) {
  return text ? <p className="px-1 text-xs text-destructive">{text}</p> : null
}

// ---------- the left: kinds, the review queue, diagnostics ----------

function Sidebar({
  o,
  kind,
  id,
  pick,
  openRecord,
}: {
  o: Overview
  kind: string
  id: string
  pick: (kind: string) => void
  openRecord: (b: Brief) => void
}) {
  const d = o.diagnostics
  return (
    <div className="flex w-60 shrink-0 flex-col gap-4 overflow-auto border-r p-2 @max-5xl:w-44">
      <Section title="Kinds" right={<span className="text-xs text-muted-foreground">{o.total} records</span>}>
        <Problem text={o.errors?.kinds ?? o.errors?.counts} />
        <div className="flex flex-col gap-0.5">
          {o.kinds.map((k) => (
            <Item
              key={k.kind}
              size="sm"
              variant={kind === k.kind ? "muted" : "default"}
              render={<a href={`#${k.kind}`} />}
              onClick={(e) => {
                e.preventDefault()
                pick(k.kind)
              }}
            >
              <ItemContent>
                <ItemTitle className="w-full">
                  <span className="min-w-0 flex-1 truncate">{k.kind}</span>
                  <Badge variant={k.count ? "secondary" : "outline"}>
                    {k.count}
                    {k.more ? "+" : ""}
                  </Badge>
                </ItemTitle>
              </ItemContent>
            </Item>
          ))}
        </div>
      </Section>
      <Section
        title="Review queue"
        right={<Badge variant={o.reviews.length ? "secondary" : "outline"}>{o.reviews.length}</Badge>}
      >
        <Problem text={o.errors?.reviews} />
        {o.reviews.length === 0 ? (
          <p className="px-1 text-xs text-muted-foreground">Nothing waits for review.</p>
        ) : (
          <div className="flex flex-col gap-0.5">
            {o.reviews.map((r) => (
              <Item
                key={r.id}
                size="sm"
                variant={id === r.id ? "muted" : "default"}
                render={<a href={`#${r.id}`} />}
                onClick={(e) => {
                  e.preventDefault()
                  openRecord(r)
                }}
              >
                <ItemContent>
                  <ItemTitle className="w-full">
                    <span className="min-w-0 flex-1 truncate">{r.name}</span>
                  </ItemTitle>
                  <ItemDescription className="truncate">
                    {r.review?.state ?? "pending"} · {r.kind}
                  </ItemDescription>
                </ItemContent>
              </Item>
            ))}
          </div>
        )}
      </Section>
      <Section title="Diagnostics">
        <Problem text={o.errors?.diagnostics} />
        {d ? (
          <div className="px-1">
            <Facts
              rows={[
                [
                  "Ready",
                  <Badge key="r" variant={d.ready ? "default" : "destructive"}>
                    {d.ready ? "ready" : "not ready"}
                  </Badge>,
                ],
                ["Storage", d.storage ?? "?"],
                ["Schemas", String(d.schemas?.length ?? 0)],
                ["As of", o.as_of && !o.as_of.startsWith("1970") ? ago(o.as_of) : "nothing written yet"],
              ]}
            />
          </div>
        ) : null}
      </Section>
    </div>
  )
}

// ---------- the middle: records of a kind ----------

function Records({
  api,
  pub,
  kind,
  id,
  tick,
  openRecord,
  showKind,
}: {
  api: ModuleAPI
  pub: Pub
  kind: string
  id: string
  tick: number
  openRecord: (b: Brief) => void
  showKind: () => void
}) {
  const [search, setSearch] = useState("")
  const [typed, setTyped] = useState("")
  const [lifecycle, setLifecycle] = useState(ANY)
  const [owner, setOwner] = useState(ANY)
  const [page, setPage] = useState<Page | null>(null)
  const [error, setError] = useState("")
  // Owners and lifecycles seen in this kind, for the filters.
  const seen = useRef(new Map<string, { owners: Set<string>; lifecycles: Set<string> }>())

  // biome-ignore lint/correctness/useExhaustiveDependencies: a new kind starts the filters afresh
  useEffect(() => {
    setSearch("")
    setTyped("")
    setLifecycle(ANY)
    setOwner(ANY)
    setPage(null)
  }, [kind])

  const path = useCallback(
    (cursor?: string) => {
      const p = new URLSearchParams({ publication: pub })
      if (kind) p.set("kind", kind)
      if (search) p.set("q", search)
      if (lifecycle !== ANY) p.set("lifecycle", lifecycle)
      if (owner !== ANY) p.set("owner", owner)
      if (cursor) p.set("cursor", cursor)
      return `records?${p.toString()}`
    },
    [pub, kind, search, lifecycle, owner],
  )
  const note = useCallback(
    (recs: Brief[]) => {
      let s = seen.current.get(kind)
      if (!s) {
        s = { owners: new Set(), lifecycles: new Set() }
        seen.current.set(kind, s)
      }
      for (const r of recs) {
        if (r.owner) s.owners.add(r.owner)
        if (r.lifecycle) s.lifecycles.add(r.lifecycle)
      }
    },
    [kind],
  )
  // biome-ignore lint/correctness/useExhaustiveDependencies: tick is the refresh signal
  useEffect(() => {
    if (!kind && !search) return
    api
      .get<Page>(path())
      .then((p) => {
        note(p.records)
        setPage(p)
        setError("")
      })
      .catch((e: Error) => setError(e.message))
  }, [api, path, note, kind, search, tick])

  const more = () => {
    if (!page?.cursor) return
    api
      .get<Page>(path(page.cursor))
      .then((p) => {
        note(p.records)
        setPage({ ...p, records: [...page.records, ...p.records] })
      })
      .catch((e: Error) => setError(e.message))
  }

  const s = seen.current.get(kind)
  const owners = [...(s?.owners ?? [])].sort()
  const lifecycles = [...new Set([...LIFECYCLES, ...(s?.lifecycles ?? [])])]

  return (
    <div className="@container/list flex w-[30rem] shrink-0 flex-col border-r @max-6xl:w-80 @max-4xl:w-60">
      <div className="flex shrink-0 flex-col gap-2 border-b p-2">
        <div className="flex items-center gap-2">
          <span className="text-sm font-semibold">{search ? `Search: ${search}` : kind || "Records"}</span>
          {kind ? (
            <Button size="xs" variant="ghost" className="ml-auto" onClick={showKind}>
              <Icon name="braces" />
              Schema
            </Button>
          ) : null}
        </div>
        <form
          className="flex gap-1"
          onSubmit={(e) => {
            e.preventDefault()
            setSearch(typed.trim())
          }}
        >
          <Input
            placeholder={kind ? `Search ${kind} records` : "Search every kind"}
            value={typed}
            onChange={(e) => {
              setTyped(e.target.value)
              if (!e.target.value) setSearch("")
            }}
            aria-label="Search records"
          />
          <Button size="sm" variant="outline" type="submit" aria-label="Search">
            <Icon name="search" />
          </Button>
        </form>
        <div className="flex gap-1">
          <Select value={lifecycle} onValueChange={(v) => setLifecycle(String(v ?? ANY))}>
            <SelectTrigger size="sm" aria-label="Lifecycle" className="min-w-0 flex-1">
              <SelectValue>{(v: string) => (v === ANY ? "Any status" : v)}</SelectValue>
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ANY}>Any status</SelectItem>
              {lifecycles.map((l) => (
                <SelectItem key={l} value={l}>
                  {l}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Select value={owner} onValueChange={(v) => setOwner(String(v ?? ANY))}>
            <SelectTrigger size="sm" aria-label="Owner" className="min-w-0 flex-1">
              <SelectValue>{(v: string) => (v === ANY ? "Any owner" : v)}</SelectValue>
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ANY}>Any owner</SelectItem>
              {owners.map((o) => (
                <SelectItem key={o} value={o}>
                  {o}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </div>
      <Problem text={error} />
      <div className="min-h-0 flex-1 overflow-auto">
        {!kind && !search ? (
          <Empty className="h-full">
            <EmptyHeader>
              <EmptyTitle>Choose a kind</EmptyTitle>
              <EmptyDescription>Or search every kind above.</EmptyDescription>
            </EmptyHeader>
          </Empty>
        ) : page === null ? (
          <div className="grid h-24 place-items-center">
            <Spinner />
          </div>
        ) : page.records.length === 0 ? (
          <Empty>
            <EmptyHeader>
              <EmptyTitle>No records</EmptyTitle>
              <EmptyDescription>
                {pub === "published" ? "Nothing of this kind is published. Try Working." : "Nothing matches."}
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        ) : (
          <Table className="table-fixed">
            <TableHeader>
              <TableRow>
                <TableHead className="pl-3">Record</TableHead>
                <TableHead className="w-28">Status</TableHead>
                <TableHead className="hidden w-32 @md/list:table-cell">Owner</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {page.records.map((r) => (
                <TableRow
                  key={r.id}
                  data-state={r.id === id ? "selected" : undefined}
                  className="cursor-pointer"
                  onClick={() => openRecord(r)}
                >
                  <TableCell className="py-1.5 pl-3">
                    <div className="flex min-w-0 flex-col">
                      <span className="truncate font-medium">{r.name}</span>
                      <span className="truncate font-mono text-xs text-muted-foreground">{r.id}</span>
                    </div>
                  </TableCell>
                  <TableCell className="py-1.5">
                    <div className="flex flex-wrap gap-1">
                      {r.lifecycle ? <Badge variant={tone(r.lifecycle)}>{r.lifecycle}</Badge> : null}
                      {r.review?.queue_state === "pending" ? <Badge variant="secondary">review</Badge> : null}
                    </div>
                  </TableCell>
                  <TableCell className="hidden truncate py-1.5 font-mono text-xs @md/list:table-cell">
                    {r.owner ?? ""}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
        {page?.has_more ? (
          <div className="p-2">
            <Button size="sm" variant="outline" onClick={more}>
              Load more
            </Button>
          </div>
        ) : null}
      </div>
    </div>
  )
}

// ---------- the right: a record ----------

/** Relations grouped by type, the other end as a chip. */
function Relations({ edges, incoming }: { edges: Edge[]; incoming?: boolean }) {
  const groups = useMemo(() => {
    const m = new Map<string, Edge[]>()
    for (const e of edges) {
      const name = incoming ? e.inverse || `${e.relation.type} (in)` : e.relation.type
      m.set(name, [...(m.get(name) ?? []), e])
    }
    return [...m.entries()].sort(([a], [b]) => a.localeCompare(b))
  }, [edges, incoming])
  if (edges.length === 0)
    return <p className="px-1 text-xs text-muted-foreground">{incoming ? "Nothing points here." : "None."}</p>
  return (
    <div className="flex flex-col gap-2">
      {groups.map(([type, list]) => (
        <div key={type} className="flex flex-col gap-1 rounded-md border p-2">
          <span className="font-mono text-xs font-semibold">{type}</span>
          {list.map((e) => (
            <div key={`${e.id}-${incoming ? "in" : "out"}`} className="flex flex-wrap items-center gap-1.5">
              <EndRef r={incoming ? e.relation.from : e.relation.to} />
              {e.relation.status ? <Badge variant={tone(e.relation.status)}>{e.relation.status}</Badge> : null}
              {e.target_state && e.target_state !== "resolved" ? (
                <Badge variant="outline">{e.target_state}</Badge>
              ) : null}
              {e.relation.weight !== undefined ? (
                <span className="text-xs text-muted-foreground">weight {e.relation.weight}</span>
              ) : null}
            </div>
          ))}
        </div>
      ))}
    </div>
  )
}

function History({ api, id, revisions, more }: { api: ModuleAPI; id: string; revisions: Revision[]; more?: boolean }) {
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
  const compare = () =>
    api
      .get<Change[]>(`diff?id=${q(id)}&from=${q(from)}&to=${q(to)}`)
      .then((c) => {
        setChanges(c)
        setError("")
      })
      .catch((e: Error) => setError(e.message))
  const pick = (value: string, set: (v: string) => void, label: string) => (
    <Select value={value} onValueChange={(v) => set(String(v ?? ""))} items={items}>
      <SelectTrigger size="sm" aria-label={label} className="min-w-0 flex-1">
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
  )
  return (
    <div className="flex flex-col gap-3">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>#</TableHead>
            <TableHead>When</TableHead>
            <TableHead>Status</TableHead>
            <TableHead>Revision</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {revisions.map((r) => (
            <TableRow key={r.revision_id}>
              <TableCell>{r.sequence}</TableCell>
              <TableCell title={r.updated_at}>{ago(r.updated_at)}</TableCell>
              <TableCell>{r.lifecycle ? <Badge variant={tone(r.lifecycle)}>{r.lifecycle}</Badge> : null}</TableCell>
              <TableCell className="max-w-48 truncate font-mono text-xs">{r.revision_id}</TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
      {more ? <p className="text-xs text-muted-foreground">Older revisions are not shown.</p> : null}
      {revisions.length < 2 ? (
        <p className="text-xs text-muted-foreground">One revision: nothing to compare.</p>
      ) : (
        <div className="flex flex-col gap-2">
          <div className="flex items-center gap-1">
            {pick(from, setFrom, "Older revision")}
            <Icon name="arrow-right" className="size-4 shrink-0 text-muted-foreground" />
            {pick(to, setTo, "Newer revision")}
            <Button size="sm" variant="outline" onClick={() => void compare()} disabled={!from || !to || from === to}>
              <Icon name="git-compare" />
              Compare
            </Button>
          </div>
          <Problem text={error} />
          {changes === null ? null : changes.length === 0 ? (
            <p className="text-xs text-muted-foreground">No differences.</p>
          ) : (
            <Table className="table-fixed">
              <TableHeader>
                <TableRow>
                  <TableHead className="w-2/5">Field</TableHead>
                  <TableHead>Before</TableHead>
                  <TableHead>After</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {changes.map((c) => (
                  <TableRow key={c.path}>
                    <TableCell className="align-top font-mono text-xs break-all whitespace-normal">
                      <Badge variant={c.op === "removed" ? "destructive" : c.op === "added" ? "default" : "secondary"}>
                        {c.op}
                      </Badge>{" "}
                      {c.path}
                    </TableCell>
                    <TableCell className="align-top text-xs break-all whitespace-pre-wrap text-muted-foreground line-through">
                      {c.before}
                    </TableCell>
                    <TableCell className="align-top text-xs break-all whitespace-pre-wrap">{c.after}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </div>
      )}
    </div>
  )
}

function ReviewPane({ d }: { d: Detail }) {
  const r = d.brief.review
  const proposed = [...d.outgoing].filter((e) => e.relation.status === "proposed")
  return (
    <div className="flex flex-col gap-4">
      {r ? (
        <Facts
          rows={[
            [
              "State",
              <Badge key="s" variant={tone(r.state)}>
                {r.state ?? "?"}
              </Badge>,
            ],
            ["Queue", r.queue_state ?? ""],
            [
              "Definition hash",
              <span key="h" className="font-mono text-xs">
                {r.definition_hash}
              </span>,
            ],
            ["Proposed by", r.proposed_by ?? ""],
            ["Reviewed by", r.reviewed_by ? `${r.reviewed_by} · ${ago(r.reviewed_at)}` : "not yet"],
            ["Reason", r.reason ?? ""],
          ]}
        />
      ) : (
        <p className="text-sm text-muted-foreground">This record has no review of its own (only tools are reviewed).</p>
      )}
      <Section title={`Proposed relations (${proposed.length})`}>
        <Relations edges={proposed} />
      </Section>
      <p className="text-xs text-muted-foreground">
        Decisions are made by the agent with registry_review_decide, on your word: say "approve this one" with the
        record in front.
      </p>
    </div>
  )
}

function RecordPane({ api, d }: { api: ModuleAPI; d: Detail }) {
  const b = d.brief
  const rec = d.record
  const payload = useMemo(() => JSON.stringify(rec.payload ?? {}, null, 2), [rec.payload])
  const raw = useMemo(() => JSON.stringify(rec, null, 2), [rec])
  const pending = b.review?.queue_state === "pending"
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex shrink-0 flex-col gap-1.5 border-b px-4 py-3">
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-base font-semibold">{b.name}</span>
          {b.lifecycle ? <Badge variant={tone(b.lifecycle)}>{b.lifecycle}</Badge> : null}
          <Badge variant="outline">{d.publication === "published" ? "published" : "working copy"}</Badge>
          {pending ? <Badge variant="secondary">waiting for review</Badge> : null}
        </div>
        <span className="font-mono text-xs text-muted-foreground break-all">registry:{b.id}</span>
        {b.summary ? <p className="text-sm">{b.summary}</p> : null}
        <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
          <span>
            {b.kind} · {b.schema}
            {b.subtype ? ` · ${b.subtype}` : ""}
          </span>
          {b.owner ? (
            <span className="inline-flex items-center gap-1">
              Owner <RecordRef id={b.owner} />
            </span>
          ) : null}
          <span title={b.updated_at}>Updated {ago(b.updated_at)}</span>
        </div>
      </div>
      <Tabs defaultValue="fields" className="min-h-0 flex-1">
        <TabsList variant="line" className="shrink-0 px-2">
          <TabsTrigger value="fields">Fields</TabsTrigger>
          <TabsTrigger value="relations">Relations ({d.outgoing.length + d.incoming.length})</TabsTrigger>
          <TabsTrigger value="history">History ({d.revisions.length})</TabsTrigger>
          <TabsTrigger value="review">Review{pending || (b.proposed ?? 0) > 0 ? " •" : ""}</TabsTrigger>
          <TabsTrigger value="json">JSON</TabsTrigger>
        </TabsList>
        <div className="min-h-0 flex-1 overflow-auto p-4">
          <TabsContent value="fields" className="flex flex-col gap-4">
            <Facts
              rows={[
                [
                  "Key",
                  <span key="k" className="font-mono text-xs">
                    {b.key}
                  </span>,
                ],
                ["Authority", b.authority ?? ""],
                [
                  "Revision",
                  <span key="r" className="font-mono text-xs">
                    {b.revision_id}
                  </span>,
                ],
                [
                  "Version token",
                  <span key="v" className="font-mono text-xs">
                    {b.version_token}
                  </span>,
                ],
              ]}
            />
            <RecordBody body={payload} />
          </TabsContent>
          <TabsContent value="relations" className="flex flex-col gap-4">
            <Problem text={d.errors?.incoming} />
            <Section title={`Outgoing (${d.outgoing.length})`}>
              <Relations edges={d.outgoing} />
            </Section>
            <Section title={`Incoming (${d.incoming.length})`}>
              <Relations edges={d.incoming} incoming />
            </Section>
          </TabsContent>
          <TabsContent value="history">
            <Problem text={d.errors?.revisions} />
            <History api={api} id={b.id} revisions={d.revisions} more={d.more_history} />
          </TabsContent>
          <TabsContent value="review">
            <ReviewPane d={d} />
          </TabsContent>
          <TabsContent value="json">
            <Markdown>{`\`\`\`json\n${raw}\n\`\`\``}</Markdown>
          </TabsContent>
        </div>
      </Tabs>
    </div>
  )
}

// ---------- the right: a kind ----------

interface Prop {
  type?: string | string[]
  enum?: unknown[]
  const?: unknown
  pattern?: string
  minLength?: number
  items?: Prop
}

function describeProp(p: Prop): string {
  if (p.const !== undefined) return `= ${JSON.stringify(p.const)}`
  if (p.enum) return p.enum.map((v) => JSON.stringify(v)).join(" | ")
  const t = Array.isArray(p.type) ? p.type.join(" | ") : (p.type ?? "any")
  if (t === "array" && p.items) return `list of ${describeProp(p.items)}`
  return p.pattern ? `${t} /${p.pattern}/` : t
}

function KindPane({ api, kind, o }: { api: ModuleAPI; kind: Kind; o: Overview }) {
  const [schema, setSchema] = useState(kind.schemas[kind.schemas.length - 1] ?? "")
  const [doc, setDoc] = useState<{ properties?: Record<string, Prop>; required?: string[] } | null>(null)
  const [error, setError] = useState("")
  useEffect(() => setSchema(kind.schemas[kind.schemas.length - 1] ?? ""), [kind])
  useEffect(() => {
    if (!schema) return
    setDoc(null)
    api
      .get<{ properties?: Record<string, Prop>; required?: string[] }>(`schema?schema=${q(schema)}`)
      .then((d) => {
        setDoc(d)
        setError("")
      })
      .catch((e: Error) => setError(e.message))
  }, [api, schema])
  const out = (o.edges ?? []).filter((e) => !e.SourceKinds || e.SourceKinds.includes(kind.kind))
  const into = (o.edges ?? []).filter((e) => e.TargetKinds?.includes(kind.kind))
  const required = new Set(doc?.required ?? [])
  const props = Object.entries(doc?.properties ?? {}).sort(
    ([a], [b]) => Number(required.has(b)) - Number(required.has(a)) || a.localeCompare(b),
  )
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex shrink-0 flex-wrap items-center gap-2 border-b px-4 py-3">
        <span className="text-base font-semibold">{kind.kind}</span>
        <Badge variant="secondary">
          {kind.count}
          {kind.more ? "+" : ""} records
        </Badge>
        {kind.schemas.length > 1 ? (
          <Select value={schema} onValueChange={(v) => setSchema(String(v ?? ""))}>
            <SelectTrigger size="sm" aria-label="Schema version" className="ml-auto w-36">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {kind.schemas.map((s) => (
                <SelectItem key={s} value={s}>
                  {s}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        ) : (
          <Badge variant="outline" className="ml-auto">
            {schema}
          </Badge>
        )}
      </div>
      <Tabs defaultValue="fields" className="min-h-0 flex-1">
        <TabsList variant="line" className="shrink-0 px-2">
          <TabsTrigger value="fields">Fields ({props.length})</TabsTrigger>
          <TabsTrigger value="relations">Relations ({out.length + into.length})</TabsTrigger>
          <TabsTrigger value="json">JSON</TabsTrigger>
        </TabsList>
        <div className="min-h-0 flex-1 overflow-auto p-4">
          <Problem text={error} />
          <TabsContent value="fields">
            {doc === null && !error ? (
              <Spinner />
            ) : (
              <Table className="table-fixed">
                <TableHeader>
                  <TableRow>
                    <TableHead className="w-1/3">Field</TableHead>
                    <TableHead>Type</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {props.map(([name, p]) => (
                    <TableRow key={name}>
                      <TableCell className="font-mono text-xs whitespace-normal">
                        <div className="flex flex-wrap items-center gap-1">
                          <span className="break-all">{name}</span>
                          {required.has(name) ? <Badge variant="secondary">required</Badge> : null}
                        </div>
                      </TableCell>
                      <TableCell className="font-mono text-xs break-all whitespace-normal">{describeProp(p)}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </TabsContent>
          <TabsContent value="relations" className="flex flex-col gap-4">
            <Section title="From this kind">
              <EdgeSpecs specs={out} side="TargetKinds" />
            </Section>
            <Section title="To this kind">
              <EdgeSpecs specs={into} side="SourceKinds" />
            </Section>
          </TabsContent>
          <TabsContent value="json">
            {doc ? <Markdown>{`\`\`\`json\n${JSON.stringify(doc, null, 2)}\n\`\`\``}</Markdown> : null}
          </TabsContent>
        </div>
      </Tabs>
    </div>
  )
}

function EdgeSpecs({ specs, side }: { specs: EdgeSpec[]; side: "SourceKinds" | "TargetKinds" }) {
  if (specs.length === 0) return <p className="px-1 text-xs text-muted-foreground">None.</p>
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Type</TableHead>
          <TableHead>Inverse</TableHead>
          <TableHead>{side === "TargetKinds" ? "To" : "From"}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {specs.map((s) => (
          <TableRow key={s.Type}>
            <TableCell className="font-mono text-xs">{s.Type}</TableCell>
            <TableCell className="font-mono text-xs">{s.Inverse || "—"}</TableCell>
            <TableCell className="text-xs">{s[side]?.join(", ") ?? "any kind"}</TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}

// ---------- the window ----------

const REFRESH_MS = 30_000

function Registry({ win, desk, api }: ViewProps) {
  const args = win.args ?? {}
  const pub: Pub = args.publication === "published" ? "published" : "draft"
  const kind = typeof args.kind === "string" ? args.kind : ""
  const id = typeof args.id === "string" ? args.id : ""
  const pane = args.pane === "kind" ? "kind" : "record"

  const [o, setO] = useState<Overview | null>(null)
  const [detail, setDetail] = useState<Detail | null>(null)
  const [error, setError] = useState("")
  const [detailError, setDetailError] = useState("")
  const [tick, setTick] = useState(0)
  const [busy, setBusy] = useState(false)

  const go = useCallback(
    (next: Record<string, unknown>) => {
      const a: Record<string, unknown> = { publication: pub, kind, ...next }
      for (const k of Object.keys(a)) if (a[k] === "" || a[k] === undefined) delete a[k]
      void desk.setArgs(win.id, a)
    },
    [desk, win.id, pub, kind],
  )

  const loadOverview = useCallback(() => {
    setBusy(true)
    api
      .get<Overview>(`overview?publication=${pub}`)
      .then((v) => {
        setO(v)
        setError("")
      })
      .catch((e: Error) => setError(e.message))
      .finally(() => setBusy(false))
  }, [api, pub])
  // biome-ignore lint/correctness/useExhaustiveDependencies: tick is the refresh signal
  useEffect(loadOverview, [loadOverview, tick])

  // biome-ignore lint/correctness/useExhaustiveDependencies: tick is the refresh signal
  useEffect(() => {
    if (!id) {
      setDetail(null)
      return
    }
    api
      .get<Detail>(`record?id=${q(id)}&publication=${pub}`)
      .then((d) => {
        setDetail(d)
        setDetailError("")
      })
      .catch((e: Error) => setDetailError(e.message))
  }, [api, id, pub, tick])

  useEffect(
    () => desk.report(win.id, id ? { kind: "record", ref: `registry:${id}` } : { kind: "data", ref: "registry" }),
    [desk, win.id, id],
  )

  // A light refresh while the window is in front: the agent changes the Registry from outside.
  useEffect(() => {
    if (!win.focused) return
    const t = window.setInterval(() => {
      if (document.visibilityState === "visible") setTick((n) => n + 1)
    }, REFRESH_MS)
    return () => window.clearInterval(t)
  }, [win.focused])

  const openRecord = (b: Brief) => go({ kind: b.kind, id: b.id, pane: "record" })
  const k = o?.kinds.find((x) => x.kind === kind)

  return (
    <div className="@container flex h-full min-h-0 flex-col bg-background">
      <div className="flex shrink-0 flex-wrap items-center gap-2 border-b px-3 py-2">
        <Icon name="boxes" className="size-4" />
        <span className="text-sm font-semibold">Registry</span>
        <ToggleGroup
          size="sm"
          spacing={0}
          value={[pub]}
          onValueChange={(v: string[]) => v[0] && go({ publication: v[0], id })}
          aria-label="Which copy"
        >
          <ToggleGroupItem value="draft" title="The working copy: every record, as last written">
            Working
          </ToggleGroupItem>
          <ToggleGroupItem value="published" title="Only what has been published">
            Published
          </ToggleGroupItem>
        </ToggleGroup>
        <span className="text-xs text-muted-foreground">Read-only: ask the agent to change or approve.</span>
        <Button
          size="sm"
          variant="ghost"
          className="ml-auto"
          aria-label="Refresh"
          title="Refresh"
          disabled={busy}
          onClick={() => setTick((n) => n + 1)}
        >
          {busy ? <Spinner /> : <Icon name="refresh-cw" />}
        </Button>
      </div>
      {error ? <p className="border-b px-3 py-1 text-xs text-destructive">{error}</p> : null}
      {!o ? (
        error ? (
          <Empty className="h-full">
            <EmptyHeader>
              <EmptyTitle>The Registry cannot be read</EmptyTitle>
              <EmptyDescription>{error}</EmptyDescription>
            </EmptyHeader>
            <Button size="sm" variant="outline" onClick={() => setTick((n) => n + 1)}>
              Try again
            </Button>
          </Empty>
        ) : (
          <div className="grid h-full place-items-center">
            <Spinner />
          </div>
        )
      ) : (
        <div className="flex min-h-0 flex-1">
          <Sidebar
            o={o}
            kind={kind}
            id={id}
            pick={(k) => go({ kind: k, id: "", pane: "kind" })}
            openRecord={openRecord}
          />
          <Records
            api={api}
            pub={pub}
            kind={kind}
            id={id}
            tick={tick}
            openRecord={openRecord}
            showKind={() => go({ id: "", pane: "kind" })}
          />
          <div className="flex min-w-0 flex-1 flex-col">
            {id ? (
              detail && detail.brief.id === id ? (
                <RecordPane api={api} d={detail} />
              ) : detailError ? (
                <Empty className="h-full">
                  <EmptyHeader>
                    <EmptyTitle>{id}</EmptyTitle>
                    <EmptyDescription>{detailError}</EmptyDescription>
                  </EmptyHeader>
                </Empty>
              ) : (
                <div className="grid h-full place-items-center">
                  <Spinner />
                </div>
              )
            ) : pane === "kind" && k ? (
              <KindPane api={api} kind={k} o={o} />
            ) : (
              <Empty className="h-full">
                <EmptyHeader>
                  <EmptyTitle>{o.total === 0 ? "The Registry is empty" : "No record open"}</EmptyTitle>
                  <EmptyDescription>
                    {o.total === 0
                      ? "Ask the agent to add records; they show here as they are written."
                      : "Choose a kind, then a record."}
                  </EmptyDescription>
                </EmptyHeader>
              </Empty>
            )}
          </div>
        </div>
      )}
    </div>
  )
}

export default { views: { "registry/main": Registry } } satisfies ModuleWeb
