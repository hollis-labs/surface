/**
 * The Operations board. Planning, Planned, the Queued band and In Flight; Review, Adoption and
 * Active behind "Later". The Queued band is its own frame, numbered in launch order, and is
 * the only place that is ordered: drag a workstream within it, or use its arrows (siblings
 * only). Dragging a card to a lane moves it. The board shows one level at a time: the top
 * concepts by default; "N inside" steps into one. What it shows is in the window's args
 * (parent, layout, later), so the agent knows the level in front.
 */
import { CiteText, Icon, useEvent, type ViewProps } from "@hollis-labs/surface-page/sdk"
import { type DragEvent, type ReactNode, useCallback, useEffect, useMemo, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Spinner } from "@/components/ui/spinner"
import { Switch } from "@/components/ui/switch"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { BoardActivity } from "./activity"
import { type BoardData, type Card, message, opsClient, type Stage, stageLabel } from "./api"
import { DRAG_TYPE, WorkstreamCard } from "./parts"

const MAIN: Stage[] = ["planning", "planned", "queued", "in_flight"]
const LATER: Stage[] = ["in_review", "adoption", "active"]

export function Board({ win, desk, api }: ViewProps) {
  const ops = useMemo(() => opsClient(api), [api])
  const parent = typeof win.args.parent === "string" ? win.args.parent : ""
  const layout = win.args.layout === "activity" ? "activity" : win.args.layout === "cards" ? "cards" : "kanban"
  const later = win.args.later === true
  const dropped = win.args.dropped === true
  const [data, setData] = useState<BoardData | null>(null)
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)
  const [over, setOver] = useState<string | null>(null)
  const [making, setMaking] = useState(false)
  const [showFollowups, setShowFollowups] = useState(false)

  const load = useCallback(
    async (force = false) => {
      try {
        if (force) setBusy(true)
        const d = await ops.board(force)
        setData(d)
        setError(d.error ?? "")
      } catch (e) {
        setError(message(e))
      } finally {
        setBusy(false)
      }
    },
    [ops],
  )
  useEffect(() => {
    void load()
  }, [load])
  useEvent("ops.changed", () => void load())
  useEvent("ops.activity", () => void load())

  const go = (set: Record<string, unknown>) => void desk.setArgs(win.id, { ...win.args, ...set })
  useEffect(() => {
    desk.report(win.id, { kind: "data", ref: parent && layout !== "activity" ? `ops:${parent}` : "ops" })
  }, [desk, win.id, parent, layout])

  const byID = useMemo(() => new Map((data?.workstreams ?? []).map((w) => [w.id, w])), [data])
  const level = (data?.workstreams ?? []).filter((w) => (w.parent ?? "") === parent)
  const queued = (data?.queue ?? [])
    .map((id) => byID.get(id))
    .filter((w): w is Card => !!w && (w.parent ?? "") === parent)
  const path: Card[] = []
  for (let p = byID.get(parent); p && path.length < 8; p = p.parent ? byID.get(p.parent) : undefined) path.unshift(p)

  const open = (id: string) => void desk.run("ops.open", { id })
  const enter = (id: string) => go({ parent: id })

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

  const lanes: Stage[] = [...MAIN, ...(later ? LATER : []), ...(dropped ? (["dropped"] as Stage[]) : [])]
  const inLane = (s: Stage) => level.filter((w) => w.stage === s).sort((a, b) => a.title.localeCompare(b.title))

  const card = (w: Card) => <WorkstreamCard key={w.id} w={w} open={open} enter={enter} />

  const band = (
    <section
      aria-label="Queued, in launch order"
      data-ops-band
      className="flex min-w-0 flex-col gap-1.5 rounded-lg border-2 border-primary/40 border-dashed bg-background p-2 data-[over=true]:border-primary"
      {...laneProps("queued", (e) => dropInBand(e))}
    >
      <h3 className="flex items-center gap-1.5 px-1 font-semibold text-sm">
        <Icon name="list-ordered" size={14} />
        Queued, in launch order
        <Badge variant="secondary">{queued.length}</Badge>
      </h3>
      <p className="px-1 text-muted-foreground text-xs">Advisory: the order does not drive dispatch.</p>
      {queued.length === 0 && <p className="px-1 text-muted-foreground text-xs">Drag a workstream here to queue it.</p>}
      <ol className="flex flex-col gap-1.5">
        {queued.map((w, i) => (
          <li
            key={w.id}
            className="flex min-w-0 items-start gap-1.5"
            onDragOver={(e) => e.preventDefault()}
            onDrop={(e) => dropInBand(e, w.id)}
          >
            <span className="w-5 shrink-0 pt-1.5 text-right font-mono font-semibold text-muted-foreground text-sm">
              {i + 1}
            </span>
            <div className="min-w-0 flex-1">
              <WorkstreamCard
                w={w}
                open={open}
                enter={enter}
                actions={
                  <span className="flex shrink-0">
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
      </ol>
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

  return (
    <div className="flex h-full min-h-0 flex-col bg-background">
      <div className="flex shrink-0 flex-wrap items-center gap-2 border-b px-3 py-2">
        <Icon name="radar" className="size-4" />
        <nav aria-label="Level" className="flex min-w-0 items-center gap-1 text-sm">
          <Button size="xs" variant={parent ? "ghost" : "secondary"} onClick={() => go({ parent: "" })}>
            Operations
          </Button>
          {path.map((p) => (
            <span key={p.id} className="flex items-center gap-1">
              <Icon name="chevron-right" size={12} className="text-muted-foreground" />
              <Button size="xs" variant={p.id === parent ? "secondary" : "ghost"} onClick={() => go({ parent: p.id })}>
                {p.title}
              </Button>
            </span>
          ))}
          {parent && (
            <Button size="xs" variant="ghost" onClick={() => open(parent)} title="Open this workstream">
              <Icon name="external-link" />
            </Button>
          )}
        </nav>
        <span className="ml-auto" />
        <ToggleGroup
          size="sm"
          spacing={0}
          value={[layout]}
          onValueChange={(v: string[]) => v[0] && go({ layout: v[0] })}
          aria-label="Layout"
        >
          <ToggleGroupItem value="kanban">Kanban</ToggleGroupItem>
          <ToggleGroupItem value="cards">Cards</ToggleGroupItem>
          <ToggleGroupItem value="activity">Activity</ToggleGroupItem>
        </ToggleGroup>
        {layout !== "activity" && (
          <>
            <Label className="flex items-center gap-1.5 text-xs">
              <Switch checked={later} onCheckedChange={(c) => go({ later: c })} aria-label="Later lanes" />
              Later
            </Label>
            <Label className="flex items-center gap-1.5 text-xs">
              <Switch checked={dropped} onCheckedChange={(c) => go({ dropped: c })} aria-label="Dropped" />
              Dropped
            </Label>
          </>
        )}
        {(data?.followups.length ?? 0) > 0 && (
          <Button size="sm" variant={showFollowups ? "secondary" : "ghost"} onClick={() => setShowFollowups((v) => !v)}>
            <Icon name="inbox" />
            Follow-ups {data?.followups.length}
          </Button>
        )}
        <Button size="sm" variant="ghost" onClick={() => setMaking((v) => !v)}>
          <Icon name="plus" />
          New
        </Button>
        <Button
          size="sm"
          variant="ghost"
          onClick={() => void desk.run("ops.seed")}
          title="Seed Operations from the tracker (a preview first)"
        >
          <Icon name="sprout" />
          Seed
        </Button>
        <Button size="sm" variant="ghost" aria-label="Refresh" onClick={() => void load(true)} disabled={busy}>
          {busy ? <Spinner /> : <Icon name="refresh-cw" />}
        </Button>
      </div>
      {error && <p className="shrink-0 border-b px-3 py-1.5 text-destructive text-xs">{error}</p>}
      {making && (
        <NewForm
          parent={parent}
          done={() => setMaking(false)}
          create={(w) => act(() => ops.create(w)).then(() => setMaking(false))}
        />
      )}
      {showFollowups && data && (
        <section aria-label="Follow-ups" className="max-h-56 shrink-0 overflow-y-auto border-b px-3 py-2 text-xs">
          <h3 className="mb-1 font-medium text-sm">Follow-ups the seed could not place (caster://ops/followups)</h3>
          <ul className="flex list-disc flex-col gap-1 pl-4">
            {data.followups.map((f) => (
              <li key={f}>
                <CiteText text={f} />
              </li>
            ))}
          </ul>
        </section>
      )}
      {!data ? (
        <div className="flex flex-1 items-center justify-center">
          <Spinner />
        </div>
      ) : layout === "activity" ? (
        <BoardActivity win={win} desk={desk} api={api} workstreams={data.workstreams} />
      ) : level.length === 0 ? (
        <Empty className="flex-1">
          <EmptyHeader>
            <EmptyTitle>{parent ? "Nothing inside yet" : "No workstreams yet"}</EmptyTitle>
            <EmptyDescription>
              Add one with New, ask the agent, or seed the board from the tracker (a preview first; nothing is written
              until you apply it).
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : layout === "kanban" ? (
        <div className="flex min-h-0 flex-1 items-start gap-2 overflow-auto p-2">{lanes.map((s) => lane(s))}</div>
      ) : (
        <div className="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto p-2">
          {(["in_flight", "queued", "planned", "planning", ...lanes.filter((s) => !MAIN.includes(s))] as Stage[]).map(
            (s) => lane(s, true),
          )}
        </div>
      )}
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
  const [stage, setStage] = useState<Stage>("planning")
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
          {(["planning", "planned", "queued", "in_flight"] as Stage[]).map((s) => (
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
