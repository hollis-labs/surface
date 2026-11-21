/**
 * A workstream's drill-in: its header (title, ops: chip, owner, stage and since, held, the
 * active-when checklist), then Overview (children, links as Cite chips by role, purpose and
 * notes), Tasks (the Torque roll-up, children's included), Activity (the merged feed) and
 * History (moves and Caster revisions, with a revision's text against the one before).
 * Activity opens first when the workstream is in flight, History otherwise.
 */
import { CiteText, citeMarkdownExtensions, Icon, useEvent, type ViewProps } from "@hollis-labs/surface-page/sdk"
import { useCallback, useEffect, useMemo, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import { Markdown } from "@/components/ui/markdown"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Spinner } from "@/components/ui/spinner"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { ActivityRow, SOURCE_ICON, SOURCE_LABEL } from "./activity-row"
import {
  age,
  type Detail,
  type History,
  type Link,
  message,
  opsClient,
  type Plan,
  type Rollup,
  STAGES,
  stageLabel,
  time,
} from "./api"
import { LinkChip, OpsChip, StageBadge } from "./parts"
import { PlanTasks } from "./plan-tasks"

export function WorkstreamView({ win, desk, api }: ViewProps) {
  const ops = useMemo(() => opsClient(api), [api])
  const id = String(win.args.id ?? win.key ?? "")
  const [d, setD] = useState<Detail | null>(null)
  const [roll, setRoll] = useState<Rollup | null>(null)
  const [error, setError] = useState("")
  const [rolling, setRolling] = useState(false)
  const [tab, setTab] = useState<string>("")

  const load = useCallback(async () => {
    try {
      const got = await ops.get(id)
      setD(got)
      setError("")
      void desk.setTitle(win.id, `ops:${id}`)
    } catch (e) {
      setError(message(e))
    }
  }, [ops, id, desk, win.id])
  const loadRoll = useCallback(
    async (refresh = false) => {
      setRolling(true)
      try {
        setRoll(await ops.activity(id, refresh))
      } catch (e) {
        setError(message(e))
      } finally {
        setRolling(false)
      }
    },
    [ops, id],
  )
  useEffect(() => {
    void load()
    void loadRoll()
  }, [load, loadRoll])
  useEvent<{ id?: string }>("ops.changed", (ev) => {
    if (!ev.data?.id || ev.data.id === id || d?.workstream.children.includes(ev.data.id)) void load()
  })
  useEvent<{ id?: string }>("ops.activity", (ev) => {
    if (!ev.data?.id || ev.data.id === id) void loadRoll()
  })
  useEffect(() => {
    desk.report(win.id, { kind: "workstream", ref: `ops:${id}` })
  }, [desk, win.id, id])

  if (!d) {
    return (
      <div className="flex h-full items-center justify-center p-4 text-sm">
        {error ? <span className="text-destructive">{error}</span> : <Spinner />}
      </div>
    )
  }
  const w = d.workstream
  const shown = tab || (w.stage === "in_flight" ? "activity" : "history")
  const act = async (fn: () => Promise<unknown>) => {
    try {
      await fn()
      setError("")
    } catch (e) {
      setError(message(e))
    }
    void load()
  }
  const done = w.active_when.filter((c) => c.done).length

  return (
    <div className="flex h-full min-h-0 flex-col bg-background">
      <header className="flex shrink-0 flex-col gap-2 border-b px-3 py-2">
        {(d.path?.length ?? 0) > 0 && (
          <nav aria-label="Inside" className="flex flex-wrap items-center gap-1 text-muted-foreground text-xs">
            {d.path?.map((p) => (
              <span key={p.id} className="flex items-center gap-1">
                <OpsChip id={p.id} />
                <Icon name="chevron-right" size={12} />
              </span>
            ))}
          </nav>
        )}
        <div className="flex flex-wrap items-center gap-2">
          <h2 className="font-semibold text-base">{w.title}</h2>
          <span className="font-mono text-muted-foreground text-xs">ops:{w.id}</span>
          <StageBadge stage={w.stage} />
          {w.stage_since && (
            <span className="text-muted-foreground text-xs" title={w.stage_since}>
              since {age(w.stage_since)} ago
            </span>
          )}
          {w.owner && <span className="text-xs">· {w.owner}</span>}
          <span className="ml-auto" />
          <MoveControl stage={w.stage} move={(stage, why) => act(() => ops.move(id, stage, why))} />
        </div>
        {w.held && (
          <p className="flex items-center gap-1.5 text-destructive text-xs">
            <Icon name="octagon-pause" size={12} />
            Held: {w.held.reason}
            <Button size="xs" variant="ghost" onClick={() => void act(() => ops.update(id, { held: "" }))}>
              Clear
            </Button>
          </p>
        )}
        {roll?.suggest && (
          <p className="flex items-center gap-1.5 text-warning text-xs">
            <Icon name="lightbulb" size={12} />
            Looks {stageLabel(roll.suggest)}: {roll.suggest_why}.
            <Button
              size="xs"
              variant="outline"
              onClick={() => void act(() => ops.move(id, roll.suggest ?? "", `suggested: ${roll.suggest_why}`))}
            >
              Move to {stageLabel(roll.suggest)}
            </Button>
          </p>
        )}
        <section aria-label="Complete when" className="flex flex-col gap-1">
          <span className="text-muted-foreground text-xs">
            Complete when ({done}/{w.active_when.length})
          </span>
          {w.active_when.map((c, i) => (
            <span key={c.text} className="flex items-center gap-2 text-sm">
              <Checkbox
                checked={c.done}
                aria-label={c.text}
                onCheckedChange={(on) =>
                  void act(() => ops.update(id, { base_rev: w.rev, tick: { index: i, done: on === true } }))
                }
              />
              <span className={c.done ? "text-muted-foreground line-through" : ""}>
                <CiteText text={c.text} />
              </span>
              {c.ref && <CiteText text={c.ref} />}
            </span>
          ))}
          <AddLine
            placeholder="Add what 'complete' means here…"
            add={(text) => act(() => ops.update(id, { base_rev: w.rev, add_check: { text, done: false } }))}
          />
        </section>
        {error && <p className="text-destructive text-xs">{error}</p>}
      </header>
      <Tabs value={shown} onValueChange={(v) => setTab(String(v))} className="min-h-0 flex-1">
        <TabsList className="mx-3 mt-2">
          <TabsTrigger value="overview">Overview</TabsTrigger>
          <TabsTrigger value="tasks">Tasks{roll ? ` (${roll.tasks.length})` : ""}</TabsTrigger>
          <TabsTrigger value="activity">Activity</TabsTrigger>
          <TabsTrigger value="history">History</TabsTrigger>
        </TabsList>
        <div className="min-h-0 flex-1 overflow-y-auto px-3 py-2">
          <TabsContent value="overview">
            <Overview d={d} desk={desk} act={act} ops={ops} />
          </TabsContent>
          <TabsContent value="tasks">
            <Tasks
              roll={roll}
              id={id}
              busy={rolling}
              refresh={() => void loadRoll(true)}
              linkPlan={async (plan) => {
                await ops.link(plan.workstream, { kind: "torque.plan", id: plan.id })
                await load()
                await loadRoll(true)
              }}
            />
          </TabsContent>
          <TabsContent value="activity">
            <Activity roll={roll} id={id} busy={rolling} refresh={() => void loadRoll(true)} />
          </TabsContent>
          <TabsContent value="history">
            {shown === "history" && <HistoryTab id={id} ops={ops} rev={w.rev} />}
          </TabsContent>
        </div>
      </Tabs>
    </div>
  )
}

function MoveControl({ stage, move }: { stage: string; move: (stage: string, why: string) => Promise<void> }) {
  const [to, setTo] = useState(stage)
  const [why, setWhy] = useState("")
  useEffect(() => setTo(stage), [stage])
  return (
    <form
      className="flex items-center gap-1"
      onSubmit={(e) => {
        e.preventDefault()
        if (to !== stage) void move(to, why.trim()).then(() => setWhy(""))
      }}
    >
      <Select value={to} onValueChange={(v) => setTo(String(v))}>
        <SelectTrigger size="sm" aria-label="Stage">
          <SelectValue>{(v: string) => stageLabel(v)}</SelectValue>
        </SelectTrigger>
        <SelectContent>
          {STAGES.map((s) => (
            <SelectItem key={s} value={s}>
              {stageLabel(s)}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      {to !== stage && (
        <>
          <Input value={why} onChange={(e) => setWhy(e.target.value)} placeholder="Why?" className="h-7 w-48" />
          <Button size="sm" type="submit">
            Move
          </Button>
        </>
      )}
    </form>
  )
}

function AddLine({ placeholder, add }: { placeholder: string; add: (text: string) => Promise<void> }) {
  const [text, setText] = useState("")
  return (
    <form
      className="flex items-center gap-1"
      onSubmit={(e) => {
        e.preventDefault()
        if (text.trim()) void add(text.trim()).then(() => setText(""))
      }}
    >
      <Input
        value={text}
        onChange={(e) => setText(e.target.value)}
        placeholder={placeholder}
        className="h-7 max-w-md"
      />
      {text.trim() && (
        <Button size="sm" variant="ghost" type="submit">
          Add
        </Button>
      )}
    </form>
  )
}

const ROLE_ORDER = ["origin", "scope", "identity", "architecture", "design", "task", "team", "agent", "thread"]

function Overview({
  d,
  desk,
  act,
  ops,
}: {
  d: Detail
  desk: ViewProps["desk"]
  act: (fn: () => Promise<unknown>) => Promise<void>
  ops: ReturnType<typeof opsClient>
}) {
  const w = d.workstream
  const groups = new Map<string, Link[]>()
  for (const l of w.links) {
    const role = l.role || (l.kind.startsWith("torque.") ? "scope" : "other")
    groups.set(role, [...(groups.get(role) ?? []), l])
  }
  const roles = [...groups.keys()].sort((a, b) => {
    const ia = ROLE_ORDER.indexOf(a)
    const ib = ROLE_ORDER.indexOf(b)
    return (ia < 0 ? 99 : ia) - (ib < 0 ? 99 : ib) || a.localeCompare(b)
  })
  const [held, setHeld] = useState("")
  return (
    <div className="flex flex-col gap-4 text-sm">
      {d.children.length > 0 && (
        <section aria-label="Inside">
          <h3 className="mb-1 font-medium">Inside ({d.children.length})</h3>
          <div className="flex flex-col gap-1">
            {d.children.map((c) => (
              <span key={c.id} className="flex items-center gap-2">
                <OpsChip id={c.id} />
                <StageBadge stage={c.stage} />
                {c.held && <Badge variant="destructive">held</Badge>}
                {c.children > 0 && <span className="text-muted-foreground text-xs">{c.children} inside</span>}
              </span>
            ))}
          </div>
        </section>
      )}
      <section aria-label="Links">
        <h3 className="mb-1 font-medium">Links</h3>
        {roles.length === 0 && (
          <p className="text-muted-foreground text-xs">
            No links yet. Ask the agent to link a task, an epic, a design or the Planner item it came from.
          </p>
        )}
        <dl className="grid grid-cols-[8rem_1fr] gap-x-3 gap-y-1.5">
          {roles.map((r) => (
            <div key={r} className="contents">
              <dt className="text-muted-foreground text-xs capitalize">{r}</dt>
              <dd className="flex flex-wrap items-center gap-1.5">
                {groups.get(r)?.map((l) => (
                  <span key={`${l.kind}:${l.id}`} className="inline-flex items-center">
                    <LinkChip link={l} />
                    <Button
                      size="icon-xs"
                      variant="ghost"
                      aria-label={`Unlink ${l.id}`}
                      onClick={() => void act(() => ops.unlink(w.id, l.kind, l.id))}
                    >
                      <Icon name="x" />
                    </Button>
                  </span>
                ))}
              </dd>
            </div>
          ))}
        </dl>
      </section>
      {!w.held && (
        <section aria-label="Hold" className="flex items-center gap-1">
          <Input
            value={held}
            onChange={(e) => setHeld(e.target.value)}
            placeholder="Hold it: why?"
            className="h-7 max-w-sm"
          />
          <Button
            size="sm"
            variant="outline"
            disabled={!held.trim()}
            onClick={() => void act(() => ops.update(w.id, { held: held.trim() }))}
          >
            Hold
          </Button>
        </section>
      )}
      {(["purpose", "active_means", "notes"] as const).map((k) =>
        w[k]?.trim() ? (
          <section key={k} aria-label={k}>
            <h3 className="mb-1 font-medium">
              {k === "active_means" ? "Complete means" : k === "purpose" ? "Purpose" : "Notes"}
            </h3>
            <Markdown {...citeMarkdownExtensions}>{w[k] ?? ""}</Markdown>
          </section>
        ) : null,
      )}
      {w.correlation.labels?.length ? (
        <p className="text-muted-foreground text-xs">Answers to labels: {w.correlation.labels.join(", ")}</p>
      ) : null}
      <p className="text-muted-foreground text-xs">
        caster://ops/ws.{w.id} · revision {w.rev}
        {w.problem ? ` · ${w.problem}` : ""}
        <Button size="xs" variant="ghost" onClick={() => void desk.run("ops.open", {})}>
          Board
        </Button>
      </p>
    </div>
  )
}

function Tasks({
  roll,
  id,
  busy,
  refresh,
  linkPlan,
}: {
  roll: Rollup | null
  id: string
  busy: boolean
  refresh: () => void
  linkPlan: (plan: Plan) => Promise<void>
}) {
  if (!roll) return <Spinner />
  const counts = Object.entries(roll.counts).sort((a, b) => b[1] - a[1])
  return (
    <div className="flex flex-col gap-2 text-sm">
      <div className="flex flex-wrap items-center gap-1.5">
        {counts.map(([s, n]) => (
          <Badge key={s} variant={s === "doing" ? "default" : "secondary"}>
            {n} {s}
          </Badge>
        ))}
        {counts.length === 0 && <span className="text-muted-foreground text-xs">No tasks linked, here or inside.</span>}
        <span className="ml-auto text-muted-foreground text-xs">read {age(roll.fetched_at)} ago</span>
        <Button size="icon-xs" variant="ghost" aria-label="Read again" onClick={refresh} disabled={busy}>
          {busy ? <Spinner /> : <Icon name="refresh-cw" />}
        </Button>
      </div>
      <Problems roll={roll} />
      <PlanTasks roll={roll} id={id} linkPlan={linkPlan} />
    </div>
  )
}

function Problems({ roll }: { roll: Rollup }) {
  if (!roll.errors?.length) return null
  return (
    <ul className="text-warning text-xs">
      {roll.errors.map((e) => (
        <li key={e}>{e}</li>
      ))}
    </ul>
  )
}

function Activity({
  roll,
  id,
  busy,
  refresh,
}: {
  roll: Rollup | null
  id: string
  busy: boolean
  refresh: () => void
}) {
  const [sources, setSources] = useState<string[]>(["torque", "comment", "status", "ops"])
  const [open, setOpen] = useState<string | null>(null)
  if (!roll) return <Spinner />
  const rows = roll.entries.filter((e) => sources.includes(e.source))
  return (
    <div className="flex flex-col gap-2 text-sm">
      <div className="flex flex-wrap items-center gap-2">
        <ToggleGroup
          multiple
          size="sm"
          value={sources}
          onValueChange={(v: string[]) => setSources(v)}
          aria-label="Sources"
        >
          {Object.entries(SOURCE_LABEL).map(([k, l]) => (
            <ToggleGroupItem key={k} value={k}>
              <Icon name={SOURCE_ICON[k]} />
              {l}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
        <span className="ml-auto text-muted-foreground text-xs">read {age(roll.fetched_at)} ago</span>
        <Button size="icon-xs" variant="ghost" aria-label="Read again" onClick={refresh} disabled={busy}>
          {busy ? <Spinner /> : <Icon name="refresh-cw" />}
        </Button>
      </div>
      <Problems roll={roll} />
      {rows.length === 0 && <p className="text-muted-foreground text-xs">Nothing yet.</p>}
      <ol className="flex flex-col">
        {rows.map((e, i) => {
          const key = `${e.at}:${e.source}:${i}`
          return (
            <li key={key} className="border-b py-1 last:border-0">
              <ActivityRow e={e} id={id} open={open === key} toggle={() => setOpen(open === key ? null : key)} />
            </li>
          )
        })}
      </ol>
    </div>
  )
}

function HistoryTab({ id, ops, rev }: { id: string; ops: ReturnType<typeof opsClient>; rev: number }) {
  const [h, setH] = useState<History | null>(null)
  const [pick, setPick] = useState<number | null>(null)
  const [diff, setDiff] = useState<DiffLine[] | null>(null)
  const [error, setError] = useState("")
  useEffect(() => {
    void rev
    ops
      .history(id)
      .then(setH)
      .catch((e) => setError(message(e)))
  }, [ops, id, rev])
  useEffect(() => {
    if (!pick) return
    setDiff(null)
    Promise.all([ops.at(id, pick), pick > 1 ? ops.at(id, pick - 1) : Promise.resolve({ rev: 0, content: "" })])
      .then(([a, b]) => setDiff(lineDiff(b.content, a.content)))
      .catch((e) => setError(message(e)))
  }, [ops, id, pick])
  if (!h) return error ? <p className="text-destructive text-xs">{error}</p> : <Spinner />
  return (
    <div className="flex flex-col gap-4 text-sm">
      <section aria-label="Moves">
        <h3 className="mb-1 font-medium">Moves</h3>
        <ol className="flex flex-col gap-1">
          {[...h.transitions].reverse().map((t) => (
            <li key={`${t.at}:${t.from}:${t.to}`} className="flex flex-wrap items-center gap-1.5 text-xs">
              <span className="w-36 text-muted-foreground">{time(t.at)}</span>
              {t.from ? <StageBadge stage={t.from} /> : <span className="text-muted-foreground">created</span>}
              <Icon name="arrow-right" size={12} />
              <StageBadge stage={t.to} />
              {t.why && <span>{t.why}</span>}
              {t.by && <span className="text-muted-foreground">({t.by})</span>}
            </li>
          ))}
        </ol>
      </section>
      <section aria-label="Revisions">
        <h3 className="mb-1 font-medium">Revisions in Caster</h3>
        {h.error && <p className="text-warning text-xs">{h.error}</p>}
        <div className="flex flex-wrap gap-1">
          {h.revisions.map((r) => (
            <Button
              key={r.rev}
              size="xs"
              variant={pick === r.rev ? "secondary" : "outline"}
              onClick={() => setPick(r.rev)}
              title={time(r.at)}
            >
              @{r.rev} · {age(r.at)}
            </Button>
          ))}
        </div>
        {pick && !diff && <Spinner />}
        {pick && diff && (
          <pre className="mt-2 max-h-96 overflow-auto rounded-md border bg-muted/40 p-2 font-mono text-xs">
            {diff.map((l) => (
              <div
                key={l.n}
                className={
                  l.kind === "+"
                    ? "bg-success/15 text-success"
                    : l.kind === "-"
                      ? "bg-destructive/15 text-destructive"
                      : ""
                }
              >
                {l.kind} {l.line}
              </div>
            ))}
          </pre>
        )}
      </section>
    </div>
  )
}

/** A line diff (longest common subsequence), enough for a document of a few hundred lines. */
type DiffLine = { n: number; line: string; kind: "+" | "-" | " " }

function lineDiff(a: string, b: string): DiffLine[] {
  const x = a ? a.split("\n") : []
  const y = b.split("\n")
  const n = x.length
  const m = y.length
  const t: number[][] = Array.from({ length: n + 1 }, () => new Array<number>(m + 1).fill(0))
  for (let i = n - 1; i >= 0; i--)
    for (let j = m - 1; j >= 0; j--) t[i][j] = x[i] === y[j] ? t[i + 1][j + 1] + 1 : Math.max(t[i + 1][j], t[i][j + 1])
  const out: DiffLine[] = []
  const push = (line: string, kind: DiffLine["kind"]) => out.push({ n: out.length, line, kind })
  let i = 0
  let j = 0
  while (i < n && j < m) {
    if (x[i] === y[j]) {
      push(x[i], " ")
      i++
      j++
    } else if (t[i + 1][j] >= t[i][j + 1]) push(x[i++], "-")
    else push(y[j++], "+")
  }
  while (i < n) push(x[i++], "-")
  while (j < m) push(y[j++], "+")
  return out
}
