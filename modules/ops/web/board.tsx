/**
 * The Operations board: Planned, Parked, the Queued band, In Flight, Landing and Complete.
 * The Queued band is its own frame in launch order, and is
 * the only place that is ordered: drag a workstream within it, or use its arrows (siblings
 * only). Dragging a card to a lane moves it. The board shows one level at a time: the top
 * concepts by default; "N inside" steps into one. What it shows is in the window's args
 * (parent, layout), so the agent knows the level in front.
 */
import { Icon, useEvent, type ViewProps } from "@hollis-labs/surface-page/sdk"
import { type DragEvent, type ReactNode, useCallback, useEffect, useMemo, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog"
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Spinner } from "@/components/ui/spinner"
import { Switch } from "@/components/ui/switch"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { WorkstreamActions } from "./actions"
import { BoardActivity } from "./activity"
import { BoardAgents } from "./agents"
import { type BoardData, type Card, message, opsClient, STAGES, type Stage, stageLabel } from "./api"
import { Fleet } from "./fleet"
import { FollowupsView } from "./followups"
import { DRAG_TYPE, EventBusHeaderMonitor, WorkstreamCard } from "./parts"
import { SeedView } from "./seed"
import { Usage } from "./usage"
import { WorkstreamView } from "./workstream"

const MAIN: Stage[] = ["planned", "parked", "queued", "in_flight", "landing", "complete"]

export function Board({ win, desk, api }: ViewProps) {
  const ops = useMemo(() => opsClient(api), [api])
  const parent = typeof win.args.parent === "string" ? win.args.parent : ""
  const activeWorkstream = typeof win.args.view_workstream === "string" ? win.args.view_workstream : ""
  const activeSeed = win.args.seed === true
  const layout =
    win.args.layout === "fleet"
      ? "fleet"
      : win.args.layout === "usage"
        ? "usage"
        : win.args.layout === "agents"
          ? "agents"
          : win.args.layout === "followups"
            ? "followups"
            : win.args.layout === "activity" || win.args.layout === "timeline"
              ? "activity"
              : win.args.layout === "cards"
                ? "cards"
                : "kanban"
  const showSettled = win.args.show_settled === true || layout === "activity"
  const dropped = win.args.dropped === true
  const [data, setData] = useState<BoardData | null>(null)
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)
  const [over, setOver] = useState<string | null>(null)
  const [making, setMaking] = useState(false)

  const load = useCallback(
    async (force = false) => {
      try {
        if (force) setBusy(true)
        const d = await ops.board(force, showSettled)
        setData(d)
        setError(d.error ?? "")
      } catch (e) {
        setError(message(e))
      } finally {
        setBusy(false)
      }
    },
    [ops, showSettled],
  )
  useEffect(() => {
    void load()
  }, [load])
  useEvent("ops.changed", () => void load())
  useEvent("ops.activity", () => void load())

  const go = (set: Record<string, unknown>) => {
    const next = { ...win.args, ...set }
    for (const [k, v] of Object.entries(next)) if (v === undefined) delete next[k]
    void desk.setArgs(win.id, next)
  }
  useEffect(() => {
    desk.report(win.id, {
      kind: "data",
      ref:
        parent && layout !== "activity" && layout !== "fleet" && layout !== "usage" && layout !== "agents"
          ? `ops:${parent}`
          : "ops",
    })
  }, [desk, win.id, parent, layout])

  const byID = useMemo(() => new Map((data?.workstreams ?? []).map((w) => [w.id, w])), [data])
  const childrenByParent = useMemo(() => {
    const m = new Map<string, Card[]>()
    for (const w of data?.workstreams ?? []) {
      const p = w.parent ?? ""
      const list = m.get(p) ?? []
      list.push(w)
      m.set(p, list)
    }
    return m
  }, [data])
  const level = (data?.workstreams ?? []).filter((w) => (w.parent ?? "") === parent)
  const queued = (data?.queue ?? [])
    .map((id) => byID.get(id))
    .filter((w): w is Card => !!w && (w.parent ?? "") === parent)
  const path: Card[] = []
  for (let p = byID.get(parent); p && path.length < 8; p = p.parent ? byID.get(p.parent) : undefined) path.unshift(p)

  const open = (id: string, newWindow = false) => {
    if (newWindow) {
      void desk.run("ops.open", { id })
      return
    }
    go({ view_workstream: id, seed: undefined })
  }
  const enter = (id: string) => go({ parent: id, view_workstream: undefined, seed: undefined })

  if (activeSeed) {
    return <SeedView win={win} desk={desk} api={api} onBack={() => go({ seed: undefined, plan: undefined })} />
  }

  const act = async (fn: () => Promise<unknown>) => {
    try {
      await fn()
      setError("")
    } catch (e) {
      setError(message(e))
    }
    void load()
  }
  const move = (id: string, stage: Stage) => {
    const w = byID.get(id)
    if (!w || w.stage === stage) return
    void act(() => ops.move(id, stage, "moved on the board"))
  }
  const dropOnLane = (e: DragEvent, stage: Stage) => {
    e.preventDefault()
    setOver(null)
    const id = e.dataTransfer.getData(DRAG_TYPE)
    if (id) move(id, stage)
  }
  // In the band: onto a queued sibling puts it before that one; anywhere else in the band, or
  // from another lane, queues it at the end.
  const dropInBand = (e: DragEvent, before?: string) => {
    e.preventDefault()
    e.stopPropagation()
    setOver(null)
    const id = e.dataTransfer.getData(DRAG_TYPE)
    const w = byID.get(id)
    if (!w || id === before) return
    if (w.stage !== "queued") return move(id, "queued")
    if (before) void act(() => ops.reorder(id, { before }))
    else {
      const last = queued[queued.length - 1]
      if (last && last.id !== id) void act(() => ops.reorder(id, { after: last.id }))
    }
  }
  const laneProps = (key: string, onDrop: (e: DragEvent) => void) => ({
    "data-over": over === key,
    onDragOver: (e: DragEvent) => {
      e.preventDefault()
      if (over !== key) setOver(key)
    },
    onDragLeave: (e: DragEvent) => {
      if (!e.currentTarget.contains(e.relatedTarget as Node)) setOver(null)
    },
    onDrop,
  })

  const lanes: Stage[] = [...MAIN, ...(dropped ? (["dropped"] as Stage[]) : [])]
  const inLane = (s: Stage) => level.filter((w) => w.stage === s).sort((a, b) => a.title.localeCompare(b.title))
  const unknown = level.filter((w) => !STAGES.some((s) => s === w.stage))

  const card = (w: Card) => (
    <WorkstreamCard
      key={w.id}
      w={w}
      childWorkstreams={childrenByParent.get(w.id) ?? []}
      open={open}
      enter={enter}
      onRequestStatus={(id) => void ops.activity(id, true).then(() => load())}
      actions={<WorkstreamActions w={w} ops={ops} refresh={() => void load()} />}
    />
  )

  const unknownLane = unknown.length > 0 && (
    <section aria-label="Unknown stage" className="flex min-w-72 flex-col gap-2 p-2">
      <h3 className="font-semibold text-sm">Unknown stage</h3>
      {unknown.map(card)}
    </section>
  )

  const band = (
    <section
      aria-label="Queued, in launch order"
      data-ops-band
      className="flex min-w-0 flex-col gap-1.5 rounded-lg border-2 border-primary/40 border-dashed bg-background p-2 data-[over=true]:border-primary"
      {...laneProps("queued", (e) => dropInBand(e))}
    >
      <h3 className="flex items-center gap-1.5 px-1 font-semibold text-sm">
        <Icon name="list" size={14} />
        Queued, in launch order
        <Badge variant="secondary">{queued.length}</Badge>
      </h3>
      <p className="px-1 text-muted-foreground text-xs">Advisory: the order does not drive dispatch.</p>
      {queued.length === 0 && <p className="px-1 text-muted-foreground text-xs">Drag a workstream here to queue it.</p>}
      <ul className="flex flex-col gap-1.5">
        {queued.map((w, i) => (
          <li
            key={w.id}
            className="flex min-w-0 items-start gap-1.5"
            onDragOver={(e) => e.preventDefault()}
            onDrop={(e) => dropInBand(e, w.id)}
          >
            <div className="min-w-0 flex-1">
              <WorkstreamCard
                w={w}
                childWorkstreams={childrenByParent.get(w.id) ?? []}
                open={open}
                enter={enter}
                onRequestStatus={(id) => void ops.activity(id, true).then(() => load())}
                actions={
                  <span className="flex shrink-0">
                    <WorkstreamActions w={w} ops={ops} refresh={() => void load()} />
                    <Button
                      size="icon-xs"
                      variant="ghost"
                      aria-label={`Move ${w.title} up`}
                      disabled={i === 0}
                      onClick={(e) => {
                        e.stopPropagation()
                        void act(() => ops.reorder(w.id, { dir: "up" }))
                      }}
                    >
                      <Icon name="arrow-up" />
                    </Button>
                    <Button
                      size="icon-xs"
                      variant="ghost"
                      aria-label={`Move ${w.title} down`}
                      disabled={i === queued.length - 1}
                      onClick={(e) => {
                        e.stopPropagation()
                        void act(() => ops.reorder(w.id, { dir: "down" }))
                      }}
                    >
                      <Icon name="arrow-down" />
                    </Button>
                  </span>
                }
              />
            </div>
          </li>
        ))}
      </ul>
    </section>
  )

  const lane = (s: Stage, wide = false): ReactNode => {
    if (s === "queued")
      return (
        <div key={s} className={wide ? "" : "w-72 shrink-0"}>
          {band}
        </div>
      )
    const list = inLane(s)
    return (
      <section
        key={s}
        aria-label={stageLabel(s)}
        className={`flex min-w-0 flex-col gap-1.5 rounded-md bg-muted/40 p-1.5 data-[over=true]:ring-2 data-[over=true]:ring-ring ${wide ? "" : "w-64 shrink-0"}`}
        {...laneProps(s, (e) => dropOnLane(e, s))}
      >
        <h3 className="flex items-center gap-1.5 px-1 font-medium text-sm">
          {stageLabel(s)}
          <Badge variant="secondary">{list.length}</Badge>
        </h3>
        <div
          className={wide ? "grid grid-cols-[repeat(auto-fill,minmax(16rem,1fr))] gap-1.5" : "flex flex-col gap-1.5"}
        >
          {list.map(card)}
        </div>
      </section>
    )
  }

  const layoutLabel =
    layout === "fleet"
      ? "Fleet"
      : layout === "usage"
        ? "Usage"
        : layout === "agents"
          ? "Agents"
          : layout === "followups"
            ? "Follow-ups"
            : layout === "activity"
              ? "Activity"
              : layout === "cards"
                ? "Cards"
                : "Kanban"

  const isBoardView = layout === "kanban" || layout === "cards"
  const inFlightCount = (data?.workstreams ?? []).filter((w) => w.stage === "in_flight").length

  const workstreamsToolbar = (
    <div className="flex shrink-0 flex-wrap items-center justify-between gap-2.5 rounded-md border border-border/80 bg-card/80 px-3.5 py-2">
      <div className="flex min-w-0 flex-wrap items-center gap-2">
        <span className="flex size-7 shrink-0 items-center justify-center rounded-sm border border-primary/30 bg-primary/10 text-primary">
          <Icon name={layout === "cards" ? "layout-grid" : "columns-3"} size={14} />
        </span>
        <nav aria-label="Level" className="flex min-w-0 flex-wrap items-center gap-1 text-xs">
          <Button
            size="xs"
            variant={parent ? "ghost" : "secondary"}
            className="h-6 px-2 font-mono text-[11px]"
            onClick={() => go({ parent: "" })}
          >
            All Workstreams
          </Button>
          {path.map((p) => (
            <span key={p.id} className="flex items-center gap-1">
              <Icon name="chevron-right" size={12} className="text-muted-foreground" />
              <Button
                size="xs"
                variant={p.id === parent ? "secondary" : "ghost"}
                className="h-6 px-2 font-mono text-[11px]"
                onClick={() => go({ parent: p.id })}
              >
                {p.title}
              </Button>
            </span>
          ))}
          {parent && (
            <Button
              size="icon-xs"
              variant="ghost"
              className="size-6"
              onClick={() => open(parent)}
              title="Open this workstream"
              aria-label="Open this workstream"
            >
              <Icon name="external-link" size={12} />
            </Button>
          )}
        </nav>
        <div className="hidden items-center gap-1.5 sm:flex">
          <Badge
            variant="outline"
            className="h-4 border-primary/40 bg-primary/10 px-1.5 font-mono text-[10px] text-primary"
          >
            {inFlightCount} in flight
          </Badge>
          <Badge variant="secondary" className="h-4 px-1.5 font-mono text-[10px]">
            {level.length} shown
          </Badge>
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-2.5">
        <Label className="flex items-center gap-1.5 font-mono text-[11px] text-muted-foreground">
          <Switch checked={showSettled} onCheckedChange={(c) => go({ show_settled: c })} aria-label="Show settled" />
          Show settled
        </Label>
        <Label className="flex items-center gap-1.5 font-mono text-[11px] text-muted-foreground">
          <Switch checked={dropped} onCheckedChange={(c) => go({ dropped: c })} aria-label="Dropped" />
          Dropped
        </Label>
        <div className="h-4 w-px bg-border/70" />
        <Button
          size="xs"
          variant={making ? "secondary" : "outline"}
          className="h-6 gap-1 px-2 font-mono text-[11px]"
          onClick={() => setMaking((v) => !v)}
        >
          <Icon name="plus" size={12} />
          New
        </Button>
        <Button
          size="xs"
          variant="outline"
          className="h-6 gap-1 px-2 font-mono text-[11px]"
          onClick={(e) => {
            if (e.metaKey || e.ctrlKey || e.shiftKey) void desk.run("ops.seed")
            else go({ seed: true })
          }}
          title="Seed Operations from the tracker (⌘-click for new window)"
        >
          <Icon name="sprout" size={12} />
          Seed
        </Button>
      </div>
    </div>
  )

  return (
    <div className="flex h-full min-h-0 flex-col bg-background">
      {/* Invariant Top Subnav Bar — fixed layout across all views so tabs never jump */}
      <div className="flex h-11 shrink-0 items-center justify-between gap-2 border-b bg-card/30 px-3">
        <div className="flex min-w-0 items-center gap-2">
          <Icon name="radar" className="size-4 shrink-0 text-primary" />
          <span className="font-semibold text-sm tracking-tight">Operations</span>
          <span className="text-border">/</span>
          <Badge variant="outline" className="h-5 px-2 font-mono text-[11px] text-muted-foreground">
            {layoutLabel}
          </Badge>
        </div>

        <div className="flex shrink-0 items-center gap-2">
          <EventBusHeaderMonitor activeCount={inFlightCount} />
          <ToggleGroup
            size="sm"
            spacing={0}
            value={[layout]}
            onValueChange={(v: string[]) => v[0] && go({ layout: v[0] })}
            aria-label="Layout"
          >
            <ToggleGroupItem value="kanban" aria-label="Kanban" title="Kanban Board" className="size-7 p-0">
              <Icon name="columns-3" size={14} />
            </ToggleGroupItem>
            <ToggleGroupItem value="cards" aria-label="Cards" title="Cards Grid" className="size-7 p-0">
              <Icon name="layout-grid" size={14} />
            </ToggleGroupItem>
            <ToggleGroupItem value="activity" aria-label="Activity" title="Activity Stream" className="size-7 p-0">
              <Icon name="activity" size={14} />
            </ToggleGroupItem>
            <ToggleGroupItem value="fleet" aria-label="Fleet" title="Fleet Telemetry" className="size-7 p-0">
              <Icon name="server" size={14} />
            </ToggleGroupItem>
            <ToggleGroupItem value="agents" aria-label="Agents" title="Agents & Sessions" className="size-7 p-0">
              <Icon name="bot" size={14} />
            </ToggleGroupItem>
            <ToggleGroupItem value="usage" aria-label="Usage" title="Provider Usage & Quotas" className="size-7 p-0">
              <Icon name="gauge" size={14} />
            </ToggleGroupItem>
            <ToggleGroupItem
              value="followups"
              aria-label="Follow-ups"
              title={`Follow-ups${(data?.followups.length ?? 0) > 0 ? ` (${data?.followups.length})` : ""}`}
              className="relative size-7 p-0"
            >
              <Icon name="bookmark-check" size={14} />
              {(data?.followups.length ?? 0) > 0 && (
                <span className="absolute top-1 right-1 size-1.5 rounded-full bg-primary" />
              )}
            </ToggleGroupItem>
          </ToggleGroup>
          <Button
            size="icon-xs"
            variant="ghost"
            className="size-7"
            aria-label="Refresh"
            title="Refresh"
            onClick={() => void load(true)}
            disabled={busy}
          >
            {busy ? <Spinner /> : <Icon name="refresh-cw" size={14} />}
          </Button>
        </div>
      </div>
      {error && layout !== "agents" && (
        <p className="shrink-0 border-b px-3 py-1.5 text-destructive text-xs">{error}</p>
      )}
      {making && isBoardView && (
        <NewForm
          parent={parent}
          done={() => setMaking(false)}
          create={(w) => act(() => ops.create(w)).then(() => setMaking(false))}
        />
      )}
      {layout === "fleet" ? (
        <Fleet win={win} desk={desk} api={api} />
      ) : layout === "usage" ? (
        <Usage win={win} desk={desk} api={api} />
      ) : layout === "agents" ? (
        <BoardAgents win={win} desk={desk} api={api} />
      ) : !data ? (
        <div className="flex flex-1 items-center justify-center">
          <Spinner />
        </div>
      ) : layout === "followups" ? (
        <FollowupsView
          followups={data.followups}
          workstreams={data.workstreams}
          ops={ops}
          onOpenWorkstream={open}
          onRefresh={() => void load()}
        />
      ) : layout === "activity" ? (
        <BoardActivity win={win} desk={desk} api={api} workstreams={data.workstreams} onOpenWorkstream={open} />
      ) : (
        <div className="flex min-h-0 flex-1 flex-col gap-2 overflow-hidden p-3">
          {workstreamsToolbar}
          {level.length === 0 ? (
            <Empty className="flex-1">
              <EmptyHeader>
                <EmptyTitle>{parent ? "Nothing inside yet" : "No workstreams yet"}</EmptyTitle>
                <EmptyDescription>
                  Add one with New, ask the agent, or seed the board from the tracker (a preview first; nothing is
                  written until you apply it).
                </EmptyDescription>
              </EmptyHeader>
            </Empty>
          ) : layout === "kanban" ? (
            <div className="flex min-h-0 flex-1 items-start gap-2 overflow-auto">
              {lanes.map((s) => lane(s))}
              {unknownLane}
            </div>
          ) : (
            <div className="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto">
              {lanes.map((s) => lane(s, true))}
              {unknownLane}
            </div>
          )}
        </div>
      )}

      {/* Large Workstream Detail Modal */}
      <Dialog
        open={!!activeWorkstream}
        onOpenChange={(isOpen) => {
          if (!isOpen) go({ view_workstream: undefined })
        }}
      >
        <DialogContent className="flex h-[88vh] max-h-[88vh] w-[min(60rem,calc(100vw-2rem))] max-w-[calc(100vw-2rem)] flex-col gap-0 overflow-hidden overscroll-x-none p-0 sm:max-w-4xl lg:max-w-[60rem]">
          <DialogTitle className="sr-only">
            {activeWorkstream ? `Workstream ops:${activeWorkstream}` : "Workstream Detail"}
          </DialogTitle>
          <DialogDescription className="sr-only">
            Full workstream overview, tasks, activity timeline, and revision history.
          </DialogDescription>
          {activeWorkstream && (
            <WorkstreamView
              win={{ ...win, args: { ...win.args, id: activeWorkstream } }}
              desk={desk}
              api={api}
              onBack={() => go({ view_workstream: undefined })}
              onOpenWorkstream={(nextId, newWin) => open(nextId, newWin)}
            />
          )}
        </DialogContent>
      </Dialog>
    </div>
  )
}

function NewForm({
  parent,
  create,
  done,
}: {
  parent: string
  create: (w: { id: string; title: string; stage: Stage; owner?: string; parent?: string }) => Promise<void>
  done: () => void
}) {
  const [title, setTitle] = useState("")
  const [id, setID] = useState("")
  const [owner, setOwner] = useState("")
  const [stage, setStage] = useState<Stage>("planned")
  const slug =
    id ||
    title
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-+|-+$/g, "")
      .slice(0, 48)
  return (
    <form
      className="flex shrink-0 flex-wrap items-end gap-2 border-b px-3 py-2"
      onSubmit={(e) => {
        e.preventDefault()
        if (title.trim())
          void create({
            id: slug,
            title: title.trim(),
            stage,
            owner: owner.trim() || undefined,
            parent: parent || undefined,
          })
      }}
    >
      <Label className="flex flex-col items-start gap-1 text-xs">
        Title
        <Input value={title} onChange={(e) => setTitle(e.target.value)} className="w-72" autoFocus />
      </Label>
      <Label className="flex flex-col items-start gap-1 text-xs">
        Id (never changes)
        <Input value={slug} onChange={(e) => setID(e.target.value)} className="w-56 font-mono" />
      </Label>
      <Label className="flex flex-col items-start gap-1 text-xs">
        Owner
        <Input value={owner} onChange={(e) => setOwner(e.target.value)} className="w-40" />
      </Label>
      <Select value={stage} onValueChange={(v) => setStage(String(v) as Stage)}>
        <SelectTrigger size="sm" aria-label="Stage">
          <SelectValue>{(v: string) => stageLabel(v)}</SelectValue>
        </SelectTrigger>
        <SelectContent>
          {MAIN.map((s) => (
            <SelectItem key={s} value={s}>
              {stageLabel(s)}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <Button size="sm" type="submit" disabled={!title.trim()}>
        Create{parent ? " inside" : ""}
      </Button>
      <Button size="sm" variant="ghost" type="button" onClick={done}>
        Cancel
      </Button>
    </form>
  )
}
