/**
 * A workstream's drill-in: its header (title, ops: chip, owner, stage and since, held, the
 * active-when checklist), then Overview (children, links as Cite chips by role, purpose and
 * notes), Tasks (the Torque roll-up, children's included), Activity (the flight-itinerary
 * inspired timeline feed) and History (moves and Caster revisions, with a revision's text
 * against the one before).
 * Activity opens first when the workstream is in flight, History otherwise.
 */
import {
  CiteText,
  citeMarkdownExtensions,
  Icon,
  ObjectActionsMenu,
  useEvent,
  type ViewProps,
} from "@hollis-labs/surface-page/sdk"
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
import { WorkstreamActions } from "./actions"
import {
  ActivityRow,
  clockTime,
  dayLabel,
  EpisodeHeaderBar,
  SOURCE_ICON,
  SOURCE_LABEL,
  spanBetween,
} from "./activity-row"
import {
  age,
  type Detail,
  type Entry,
  type History,
  type Link,
  message,
  opsClient,
  type Plan,
  type Rollup,
  STAGES,
  stageLabel,
  time,
  type Workstream,
} from "./api"
import { computeWorkUnits, LinkChip, OpsChip, requestWorkstreamStatus, StageBadge, UnitNotesInline } from "./parts"
import { PlanTasks } from "./plan-tasks"

export function WorkstreamView({
  win,
  desk,
  api,
  onBack,
  onOpenWorkstream,
}: ViewProps & {
  onBack?: () => void
  onOpenWorkstream?: (id: string, newWindow?: boolean) => void
}) {
  const ops = useMemo(() => opsClient(api), [api])
  const id = String(win.args.id ?? win.key ?? "")
  const openWS = useCallback(
    (targetId: string, newWindow = false) => {
      if (newWindow) {
        void desk.run("ops.open", { id: targetId })
        return
      }
      if (onOpenWorkstream) {
        onOpenWorkstream(targetId, false)
        return
      }
      void desk.setArgs(win.id, { ...win.args, id: targetId })
    },
    [desk, onOpenWorkstream, win.args, win.id],
  )
  const [d, setD] = useState<Detail | null>(null)
  const [roll, setRoll] = useState<Rollup | null>(null)
  const [error, setError] = useState("")
  const [rolling, setRolling] = useState(false)
  const [tab, setTab] = useState<string>("")
  const [checksOpen, setChecksOpen] = useState(false)

  const load = useCallback(async () => {
    try {
      const got = await ops.get(id)
      setD(got)
      setError("")
      if (!onBack) void desk.setTitle(win.id, `ops:${id}`)
    } catch (e) {
      setError(message(e))
    }
  }, [ops, id, desk, win.id, onBack])
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
  const workProgress = computeWorkUnits(
    { id: w.id, active_when: w.active_when, task_list: roll?.tasks, task_counts: roll?.counts },
    d.children,
  )

  return (
    <div className="flex h-full min-h-0 min-w-0 flex-col overflow-x-hidden overscroll-x-none bg-background">
      <Tabs value={shown} onValueChange={(v) => setTab(String(v))} className="flex min-h-0 flex-1 flex-col gap-0">
        {/* Compact, Polished Header Aligned to 90% Width */}
        <header className="shrink-0 border-b border-border/80 bg-muted/25 px-4 pt-2.5 pb-0">
          <div className={`mx-auto flex w-[90%] flex-col gap-1.5 ${onBack ? "pr-6" : ""}`}>
            {/* Row 1: Breadcrumb (if nested) + Title, Stage, Progress & Compact Actions */}
            <div className="flex flex-wrap items-center gap-1.5">
              {(d.path?.length ?? 0) > 0 && (
                <nav aria-label="Inside" className="flex items-center gap-1 text-muted-foreground text-xs">
                  {d.path?.map((p) => (
                    <span key={p.id} className="flex items-center gap-1">
                      <OpsChip id={p.id} />
                      <Icon name="chevron-right" size={11} />
                    </span>
                  ))}
                </nav>
              )}
              <h2 className="truncate font-semibold text-[15px] tracking-tight text-foreground">{w.title}</h2>
              <span className="rounded-xs bg-muted/70 px-1.5 py-0.5 font-mono text-[11px] text-muted-foreground">
                ops:{w.id}
              </span>
              <StageBadge stage={w.stage} />
              {workProgress.total > 0 && (
                <Badge
                  variant={workProgress.done === workProgress.total ? "default" : "secondary"}
                  className="h-4.5 px-1.5 font-mono text-[10.5px]"
                  title={`${workProgress.done} of ${workProgress.total} units of work complete`}
                >
                  {workProgress.done}/{workProgress.total}
                </Badge>
              )}
              {w.stage_since && (
                <span className="font-mono text-[11px] text-muted-foreground" title={w.stage_since}>
                  · {age(w.stage_since)}
                </span>
              )}
              {w.owner && <span className="font-mono text-[11px] text-muted-foreground">· {w.owner}</span>}

              <div className="ml-auto flex flex-wrap items-center gap-1">
                <WorkstreamActions w={w} ops={ops} refresh={() => void load()} />
                <Button
                  size="xs"
                  variant="outline"
                  className="h-6 gap-1 px-2 font-mono text-[10.5px]"
                  disabled={rolling}
                  onClick={() => requestWorkstreamStatus(`ops:${w.id}`, w.title, () => void loadRoll(true))}
                  title="Refresh live activity and request status update"
                >
                  {rolling ? <Spinner /> : <Icon name="radio" size={11} />}
                  Status
                </Button>
                <MoveControl stage={w.stage} move={(stage, why) => act(() => ops.move(id, stage, why))} />
                {onBack && (
                  <Button
                    size="icon-xs"
                    variant="ghost"
                    className="size-6 text-muted-foreground hover:text-foreground"
                    onClick={() => void desk.run("ops.open", { id: w.id })}
                    title="Pop out workstream into its own window"
                    aria-label="Open workstream in new window"
                  >
                    <Icon name="external-link" size={12} />
                  </Button>
                )}
                <ObjectActionsMenu
                  object={{
                    kind: "ops.workstream",
                    id: w.id,
                    title: w.title,
                    subtitle: stageLabel(w.stage),
                  }}
                />
              </div>
            </div>

            {/* Row 2: Integrated TabsList + Compact Checklist/Hold/Suggestion Pills */}
            <div className="flex flex-wrap items-center justify-between gap-2 pt-0.5">
              <TabsList variant="line" className="h-7 border-b-0 p-0">
                <TabsTrigger value="overview" className="px-2.5 py-1 text-xs">
                  Overview
                </TabsTrigger>
                <TabsTrigger value="tasks" className="px-2.5 py-1 text-xs">
                  Tasks{roll ? ` (${roll.tasks.length})` : ""}
                </TabsTrigger>
                <TabsTrigger value="activity" className="px-2.5 py-1 text-xs">
                  Activity
                </TabsTrigger>
                <TabsTrigger value="history" className="px-2.5 py-1 text-xs">
                  History
                </TabsTrigger>
              </TabsList>

              <div className="flex flex-wrap items-center gap-1.5 pb-1">
                {w.held && (
                  <span className="inline-flex items-center gap-1 rounded-xs border border-destructive/40 bg-destructive/10 px-1.5 py-0.5 font-mono text-[10.5px] text-destructive">
                    <Icon name="octagon-pause" size={11} />
                    <span className="max-w-48 truncate" title={w.held.reason}>
                      Held: {w.held.reason}
                    </span>
                    <Button
                      size="xs"
                      variant="ghost"
                      className="h-4 px-1 text-[10px] text-destructive hover:text-destructive"
                      onClick={() => void act(() => ops.update(id, { held: "" }))}
                    >
                      Clear
                    </Button>
                  </span>
                )}
                {roll?.suggest && (
                  <span
                    className="inline-flex items-center gap-1 rounded-xs border border-warning/40 bg-warning/10 px-1.5 py-0.5 font-mono text-[10.5px] text-warning"
                    title={roll.suggest_why}
                  >
                    <Icon name="lightbulb" size={11} />
                    <span>Looks {stageLabel(roll.suggest)}</span>
                    <Button
                      size="xs"
                      variant="ghost"
                      className="h-4 px-1 text-[10px] text-warning hover:text-foreground"
                      onClick={() => void act(() => ops.move(id, roll.suggest ?? "", `suggested: ${roll.suggest_why}`))}
                    >
                      Move →
                    </Button>
                  </span>
                )}
                <Button
                  size="xs"
                  variant={checksOpen ? "secondary" : "ghost"}
                  className="h-5.5 gap-1 px-2 font-mono text-[10.5px] text-muted-foreground hover:text-foreground"
                  onClick={() => setChecksOpen((v) => !v)}
                  title="Toggle 'Complete when' exit criteria checklist"
                >
                  <Icon name="list-checks" size={11} className="text-primary" />
                  <span>
                    Complete when ({done}/{w.active_when.length})
                  </span>
                  <Icon name={checksOpen ? "chevron-up" : "chevron-down"} size={10} />
                </Button>
                <UnitNotesInline targetRef={`ops:${w.id}`} compact />
              </div>
            </div>

            {/* Optional Collapsible Tray for 'Complete when' Exit Criteria */}
            {checksOpen && (
              <section
                aria-label="Complete when"
                className="mb-2 flex flex-col gap-1.5 rounded-md border border-border/75 bg-card/90 px-3 py-2 text-xs"
              >
                <div className="flex items-center justify-between">
                  <span className="font-mono text-[10.5px] font-semibold uppercase tracking-wider text-muted-foreground">
                    Complete when ({done}/{w.active_when.length})
                  </span>
                  <Button
                    size="icon-xs"
                    variant="ghost"
                    className="size-5"
                    onClick={() => setChecksOpen(false)}
                    aria-label="Close checklist"
                  >
                    <Icon name="x" size={11} />
                  </Button>
                </div>
                {w.active_when.map((c, i) => (
                  <span key={c.text} className="flex items-center gap-2 text-xs">
                    <Checkbox
                      checked={c.done}
                      aria-label={c.text}
                      onCheckedChange={(on) =>
                        void act(() => ops.update(id, { base_rev: w.rev, tick: { index: i, done: on === true } }))
                      }
                    />
                    <span className={c.done ? "text-muted-foreground line-through" : "text-foreground"}>
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
            )}

            {error && <p className="pb-1.5 text-destructive text-xs">{error}</p>}
          </div>
        </header>

        {/* Tab Contents: Every Tab Uses the Same max-w-4xl Width as Activity/Timeline */}
        <div className="min-h-0 flex-1 overflow-x-hidden overflow-y-auto overscroll-x-none px-4 py-3">
          <TabsContent value="overview" className="mt-0">
            <Overview d={d} desk={desk} act={act} ops={ops} onBack={onBack} onOpenWorkstream={openWS} />
          </TabsContent>
          <TabsContent value="tasks" className="mt-0">
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
          <TabsContent value="activity" className="mt-0">
            <Activity
              roll={roll}
              id={id}
              workstream={w}
              workProgress={workProgress}
              busy={rolling}
              refresh={() => void loadRoll(true)}
              onOpenWorkstream={openWS}
            />
          </TabsContent>
          <TabsContent value="history" className="mt-0">
            {shown === "history" && <HistoryTab id={id} workstream={w} ops={ops} rev={w.rev} />}
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
  onBack,
  onOpenWorkstream,
}: {
  d: Detail
  desk: ViewProps["desk"]
  act: (fn: () => Promise<unknown>) => Promise<void>
  ops: ReturnType<typeof opsClient>
  onBack?: () => void
  onOpenWorkstream?: (id: string, newWindow?: boolean) => void
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
  const doneChecks = w.active_when.filter((c) => c.done).length
  return (
    <div className="mx-auto flex w-[90%] flex-col gap-4 py-1 text-sm">
      <section
        aria-label="Complete when"
        className="flex flex-col gap-1.5 border-b border-dashed border-border/75 pb-3"
      >
        <span className="font-mono text-muted-foreground text-xs uppercase tracking-wider">
          Complete when ({doneChecks}/{w.active_when.length})
        </span>
        {w.active_when.map((c, i) => (
          <span key={c.text} className="flex items-center gap-2 text-sm">
            <Checkbox
              checked={c.done}
              aria-label={c.text}
              onCheckedChange={(on) =>
                void act(() => ops.update(w.id, { base_rev: w.rev, tick: { index: i, done: on === true } }))
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
          add={(text) => act(() => ops.update(w.id, { base_rev: w.rev, add_check: { text, done: false } }))}
        />
      </section>
      {d.children.length > 0 && (
        <section aria-label="Inside">
          <h3 className="mb-1 font-medium">Inside ({d.children.length})</h3>
          <div className="flex flex-col gap-1">
            {d.children.map((c) => (
              <div
                key={c.id}
                onClick={(e) => onOpenWorkstream?.(c.id, e.metaKey || e.ctrlKey || e.shiftKey)}
                className="group flex cursor-pointer items-center gap-2 rounded-sm px-1.5 py-0.5 hover:bg-accent"
              >
                <span className="font-mono text-primary text-xs">ops:{c.id}</span>
                <span className="font-medium">{c.title}</span>
                <StageBadge stage={c.stage} />
                {c.held && <Badge variant="destructive">held</Badge>}
                {c.children > 0 && <span className="text-muted-foreground text-xs">{c.children} inside</span>}
                <Button
                  size="icon-xs"
                  variant="ghost"
                  className="ml-auto opacity-0 transition-opacity group-hover:opacity-100"
                  title="Open in new window"
                  aria-label={`Open ${c.title} in new window`}
                  onClick={(e) => {
                    e.stopPropagation()
                    onOpenWorkstream?.(c.id, true)
                  }}
                >
                  <Icon name="external-link" />
                </Button>
              </div>
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
        <Button
          size="xs"
          variant="ghost"
          onClick={() => {
            if (onBack) onBack()
            else void desk.run("ops.open", {})
          }}
        >
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
    <div className="mx-auto flex w-[90%] flex-col gap-3 py-1 text-sm">
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
  workstream,
  workProgress,
  busy,
  refresh,
  onOpenWorkstream,
}: {
  roll: Rollup | null
  id: string
  workstream: Workstream
  workProgress: { done: number; total: number }
  busy: boolean
  refresh: () => void
  onOpenWorkstream?: (id: string, newWindow?: boolean) => void
}) {
  const [sources, setSources] = useState<string[]>(["torque", "comment", "status", "ops", "liveness"])
  const [open, setOpen] = useState<string | null>(null)
  const rows = useMemo(() => (roll?.entries ?? []).filter((e) => sources.includes(e.source)), [roll, sources])
  const episodes = useMemo(() => {
    const groups: { day: string; entries: Entry[] }[] = []
    for (const e of rows) {
      const day = dayLabel(e.at)
      const last = groups[groups.length - 1]
      if (last && last.day === day) {
        last.entries.push(e)
      } else {
        groups.push({ day, entries: [e] })
      }
    }
    return groups
  }, [rows])

  if (!roll) return <Spinner />

  const lastTransition = workstream.transitions?.[workstream.transitions.length - 1]
  const fromStageLabel = lastTransition?.from ? stageLabel(lastTransition.from) : "Origin"
  const toStageLabel = stageLabel(workstream.stage)

  return (
    <div className="mx-auto flex w-[90%] flex-col gap-4 py-1 text-sm">
      {/* Flight-Itinerary Style Header for this Workstream */}
      <header className="flex flex-col gap-2 border-b border-dashed border-border/80 pb-3.5">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div className="flex flex-wrap items-center gap-2">
            <span className="font-semibold text-base text-muted-foreground">{fromStageLabel}</span>
            <Icon name="arrow-left-right" size={15} className="text-muted-foreground" />
            <span className="font-semibold text-base text-primary">{toStageLabel}</span>
            <span className="text-border">·</span>
            <span className="font-semibold text-base text-foreground">{workstream.title}</span>
          </div>
          <div className="flex items-center gap-1.5">
            <span className="text-muted-foreground text-xs">read {age(roll.fetched_at)} ago</span>
            <Button size="icon-xs" variant="outline" aria-label="Read again" onClick={refresh} disabled={busy}>
              {busy ? <Spinner /> : <Icon name="refresh-cw" />}
            </Button>
          </div>
        </div>

        <div className="flex flex-wrap items-center gap-2 text-muted-foreground text-xs">
          <span>
            Workstream: <strong className="font-mono text-foreground">ops:{id}</strong>
          </span>
          {workstream.owner && (
            <>
              <span className="text-border">|</span>
              <span>
                Owner: <strong className="font-medium text-foreground">{workstream.owner}</strong>
              </span>
            </>
          )}
          {workProgress.total > 0 && (
            <>
              <span className="text-border">|</span>
              <span>
                Units:{" "}
                <strong className="font-mono text-foreground">
                  {workProgress.done} of {workProgress.total}
                </strong>
              </span>
            </>
          )}
          <span className="text-border">|</span>
          <span>All timestamps in local time</span>
        </div>

        <div className="mt-1 flex flex-wrap items-center gap-2">
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
        </div>
      </header>

      <Problems roll={roll} />

      {/* Distinct Shaded Callout Div for Hold or Stage Suggestion */}
      {(workstream.held || roll.suggest) && (
        <div className="rounded-md border border-border/50 bg-muted/45 px-3.5 py-2.5 text-xs">
          <span className="font-semibold text-warning">
            {workstream.held
              ? "Hold / Blocker on Workstream"
              : `Stage Advisory · Looks ${stageLabel(roll.suggest ?? "")}`}
          </span>
          <ul className="mt-1 flex list-disc flex-col gap-1 pl-4 text-muted-foreground">
            {workstream.held && <li className="text-destructive">Held: {workstream.held.reason}</li>}
            {roll.suggest && (
              <li>
                Looks {stageLabel(roll.suggest)}: {roll.suggest_why}
              </li>
            )}
          </ul>
        </div>
      )}

      {rows.length === 0 && <p className="py-6 text-center text-muted-foreground text-xs">Nothing yet.</p>}

      <div className="flex flex-col gap-4">
        {episodes.map((ep) => {
          const newest = ep.entries[0]?.at
          const oldest = ep.entries[ep.entries.length - 1]?.at
          const windowSpan = ep.entries.length > 1 ? spanBetween(newest, oldest) : ""
          return (
            <div key={ep.day} className="flex flex-col gap-2">
              <EpisodeHeaderBar
                badgeText={toStageLabel}
                badgeTone={workstream.stage === "in_flight" || workstream.stage === "complete" ? "success" : "primary"}
                dateText={ep.day}
                routeText={`${workstream.title} (ops:${id})`}
                durationText={
                  windowSpan || (workstream.stage_since ? `${age(workstream.stage_since)} in stage` : undefined)
                }
                countText={`${ep.entries.length} ${ep.entries.length === 1 ? "event" : "events"}`}
              />
              <ol className="flex flex-col">
                {ep.entries.map((e, i) => {
                  const key = `${e.at}:${e.source}:${i}`
                  const nextEntry = ep.entries[i + 1]
                  return (
                    <li key={key}>
                      <ActivityRow
                        e={e}
                        id={id}
                        open={open === key}
                        toggle={() => setOpen(open === key ? null : key)}
                        workstreamCard={workstream}
                        nextAt={nextEntry?.at}
                        onOpenWorkstream={onOpenWorkstream}
                      />
                    </li>
                  )
                })}
              </ol>
            </div>
          )
        })}
      </div>
    </div>
  )
}

function HistoryTab({
  id,
  workstream,
  ops,
  rev,
}: {
  id: string
  workstream: Workstream
  ops: ReturnType<typeof opsClient>
  rev: number
}) {
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
  const reversedMoves = [...h.transitions].reverse()
  return (
    <div className="mx-auto flex w-[90%] flex-col gap-4 py-1 text-sm">
      {h.settled_at && (
        <section
          aria-label="Settlement"
          className="rounded-md border border-border/50 bg-muted/45 px-3.5 py-2.5 text-xs"
        >
          <h3 className="font-semibold text-primary">
            Settled {time(h.settled_at)} by {h.settled_by}
          </h3>
          <p className="mt-1 text-foreground/90">{h.settled_summary}</p>
        </section>
      )}
      <section aria-label="Moves" className="flex flex-col gap-2">
        <EpisodeHeaderBar
          badgeText="Stage Itinerary"
          badgeTone="primary"
          dateText={dayLabel(reversedMoves[0]?.at)}
          routeText={`${workstream.title} (ops:${id})`}
          countText={`${reversedMoves.length} ${reversedMoves.length === 1 ? "move" : "moves"}`}
        />
        <ol className="flex flex-col">
          {reversedMoves.map((t, idx) => {
            const nextMove = reversedMoves[idx + 1]
            const delta = nextMove ? spanBetween(t.at, nextMove.at) : age(t.at)
            return (
              <li
                key={`${t.at}:${t.from}:${t.to}`}
                className="grid grid-cols-[4.5rem_1.75rem_1fr] items-stretch gap-x-2"
              >
                <div className="flex flex-col items-end pr-1 pt-0.5 text-right font-mono">
                  <span className="font-semibold text-foreground text-xs leading-5" title={time(t.at)}>
                    {clockTime(t.at)}
                  </span>
                  <span className="mt-2 text-[11px] text-muted-foreground">{delta}</span>
                </div>
                <div className="relative flex flex-col items-center">
                  <div className="absolute inset-y-0 left-1/2 w-px -translate-x-1/2 bg-border/80" />
                  <span className="relative z-10 mt-1.5 size-2.5 shrink-0 rounded-full border-2 border-primary bg-background" />
                </div>
                <div className="flex flex-col gap-1.5 pb-3.5 pt-0.5">
                  <div className="flex flex-wrap items-center gap-1.5 text-xs">
                    <span className="font-mono text-muted-foreground">{time(t.at)}</span>
                    {t.from ? <StageBadge stage={t.from} /> : <span className="text-muted-foreground">created</span>}
                    <Icon name="arrow-right" size={12} />
                    <StageBadge stage={t.to} />
                    {t.by && <span className="text-muted-foreground">({t.by})</span>}
                  </div>
                  {t.why && (
                    <div className="max-w-xl rounded-md border border-border/50 bg-muted/45 px-3 py-2 text-xs">
                      <span className="font-semibold text-primary">Transition Note</span>
                      <ul className="mt-0.5 list-disc pl-4 text-foreground/90">
                        <li>{t.why}</li>
                      </ul>
                    </div>
                  )}
                </div>
              </li>
            )
          })}
        </ol>
      </section>
      <section aria-label="Revisions" className="flex flex-col gap-2 border-t border-dashed border-border/80 pt-3">
        <h3 className="font-semibold text-xs uppercase tracking-wider text-muted-foreground">Revisions in Caster</h3>
        {h.error && <p className="text-warning text-xs">{h.error}</p>}
        <div className="flex flex-wrap gap-1.5">
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
