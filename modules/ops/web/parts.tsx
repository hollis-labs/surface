/** Pieces the Operations views share: a workstream's card, a link as a chip, a stage badge, work breakdown (x of y), and unit notes. */
import {
  addMark,
  CiteRef,
  CiteText,
  Icon,
  ObjectActionsMenu,
  type ObjectRef,
  replyMark,
  resolveMark,
  useEvent,
  useMarks,
} from "@hollis-labs/surface-page/sdk"
import { type ReactNode, useEffect, useMemo, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { age, type Card, type Check, type Link, RESOLVED_KINDS, stageLabel, type Task } from "./api"

export const DRAG_TYPE = "application/x-ops-workstream"

export function StageBadge({ stage }: { stage: string }) {
  const variant = stage === "in_flight" ? "default" : stage === "dropped" ? "destructive" : "secondary"
  return <Badge variant={variant}>{stageLabel(stage)}</Badge>
}

/** A link as its Cite chip; a kind the page does not resolve (tether.team) as text. */
export function LinkChip({ link }: { link: Link }) {
  if (!RESOLVED_KINDS.has(link.kind)) {
    return (
      <span
        className="font-mono text-muted-foreground text-xs"
        title={`${link.kind} (shown as text until it has a resolver)`}
      >
        {link.kind}:{link.id}
      </span>
    )
  }
  return <CiteRef refId={{ kind: link.kind, canonical: link.id }} label={link.label || undefined} />
}

export function OpsChip({ id }: { id: string }) {
  return <CiteRef refId={{ kind: "ops.workstream", canonical: id }} raw={`ops:${id}`} />
}

export interface WorkUnit {
  id: string
  kind: "child" | "check" | "task"
  title: string
  status: string
  done: boolean
  ref: string
  objectRef: ObjectRef
}

/**
 * Computes the unit-of-work breakdown (`x of y`) for a workstream across:
 * 1. Child workstreams (`active` = done; `dropped` excluded from total unless all are dropped)
 * 2. Active-when checklist items (`c.done`)
 * 3. Linked/correlated Torque tasks (`done` / `completed` / `closed` = done)
 * As new items get added to any of these sources, `y` simply increases.
 */
export function computeWorkUnits(
  w: { id: string; active_when: Check[]; task_list?: Task[]; task_counts?: Record<string, number> },
  childList: { id: string; title: string; stage: string }[] = [],
): { done: number; total: number; units: WorkUnit[] } {
  const units: WorkUnit[] = []

  for (const c of childList) {
    if (c.stage === "dropped") continue
    const isDone = c.stage === "active" || c.stage === "adoption"
    units.push({
      id: `child:${c.id}`,
      kind: "child",
      title: c.title,
      status: stageLabel(c.stage),
      done: isDone,
      ref: `ops:${c.id}`,
      objectRef: { kind: "ops.workstream", id: c.id, title: c.title, subtitle: stageLabel(c.stage) },
    })
  }

  w.active_when.forEach((chk, idx) => {
    units.push({
      id: `check:${idx}`,
      kind: "check",
      title: chk.text,
      status: chk.done ? "Done" : "Open",
      done: chk.done,
      ref: `ops:${w.id}#check-${idx + 1}`,
      objectRef: { kind: "ops.check", id: `${w.id}#check-${idx + 1}`, title: chk.text },
    })
  })

  if (w.task_list && w.task_list.length > 0) {
    for (const t of w.task_list) {
      if (t.status === "cancelled" || t.status === "dropped") continue
      const isDone = t.status === "done" || t.status === "completed" || t.status === "closed"
      units.push({
        id: `task:${t.id}`,
        kind: "task",
        title: t.title || t.id,
        status: t.status,
        done: isDone,
        ref: t.id,
        objectRef: { kind: "torque.task", id: t.id, title: t.title || t.id, subtitle: t.status },
      })
    }
  } else if (w.task_counts) {
    let extraDone = 0
    let extraTotal = 0
    for (const [state, count] of Object.entries(w.task_counts)) {
      if (state === "cancelled" || state === "dropped") continue
      extraTotal += count
      if (state === "done" || state === "completed" || state === "closed") extraDone += count
    }
    if (extraTotal > 0 && units.length === 0) {
      return { done: extraDone, total: extraTotal, units }
    }
  }

  const done = units.filter((u) => u.done).length
  return { done, total: units.length, units }
}

export function requestWorkstreamStatus(ref: string, title?: string, onRefresh?: () => void) {
  onRefresh?.()
  window.dispatchEvent(
    new CustomEvent("surface:add-to-context", {
      detail: {
        ref: { kind: "ops.workstream", id: ref.replace(/^ops:/, ""), title },
        prompt: `Requesting current status update, blockers, and next actions for ${ref}${title ? ` (${title})` : ""}.`,
      },
    }),
  )
}

/** Inline notes/comments thread attached to any unit of work via Surface Marks. */
export function UnitNotesInline({ targetRef, compact = false }: { targetRef: string; compact?: boolean }) {
  const marks = useMarks(targetRef)
  const [open, setOpen] = useState(false)
  const [draft, setDraft] = useState("")
  const [replyingTo, setReplyingTo] = useState<string | null>(null)
  const [replyText, setReplyText] = useState("")

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!draft.trim()) return
    await addMark({ ref: targetRef, body: draft.trim() })
    setDraft("")
    setOpen(true)
  }

  return (
    <div className="flex flex-col gap-1" onClick={(e) => e.stopPropagation()} onKeyDown={(e) => e.stopPropagation()}>
      <div className="flex items-center gap-1.5">
        <Button
          variant="ghost"
          size="xs"
          className="h-5 gap-1 px-1.5 text-[11px] text-muted-foreground hover:text-foreground"
          onClick={() => setOpen((v) => !v)}
          title="View or add notes on this unit of work"
        >
          <Icon name="message-square-text" size={11} />
          <span>{marks.length > 0 ? `${marks.length} note${marks.length === 1 ? "" : "s"}` : "Add note"}</span>
        </Button>
      </div>
      {(open || (!compact && marks.length > 0)) && (
        <div className="flex flex-col gap-1.5 rounded-sm border border-border/70 bg-muted/25 p-2 text-xs">
          {marks.map((m) => (
            <div key={m.id} className="flex flex-col gap-1 border-b border-border/40 pb-1.5 last:border-b-0 last:pb-0">
              <div className="flex items-center gap-1.5 text-[11px]">
                <Icon name={m.author === "agent" ? "bot" : "user"} size={11} className="text-primary" />
                <span className="font-medium text-foreground">{m.author}</span>
                <span className="text-muted-foreground">{age(m.created)} ago</span>
                <span className="ml-auto flex items-center gap-1">
                  <Button
                    variant="ghost"
                    size="xs"
                    className="h-4 px-1 text-[10px]"
                    onClick={() => setReplyingTo(replyingTo === m.id ? null : m.id)}
                  >
                    reply
                  </Button>
                  <Button
                    variant="ghost"
                    size="xs"
                    className="h-4 px-1 text-[10px] text-muted-foreground"
                    onClick={() => void resolveMark(m.id)}
                  >
                    resolve
                  </Button>
                </span>
              </div>
              <p className="text-foreground/90 leading-snug">
                <CiteText text={m.body} />
              </p>
              {m.replies?.map((r) => (
                <div
                  key={r.created + r.body}
                  className="ml-3 flex items-center gap-1.5 text-[11px] text-muted-foreground"
                >
                  <Icon name={r.author === "agent" ? "bot" : "user"} size={10} />
                  <span>{r.body}</span>
                </div>
              ))}
              {replyingTo === m.id && (
                <form
                  className="mt-1 flex items-center gap-1"
                  onSubmit={(ev) => {
                    ev.preventDefault()
                    if (!replyText.trim()) return
                    void replyMark(m.id, replyText.trim()).then(() => {
                      setReplyText("")
                      setReplyingTo(null)
                    })
                  }}
                >
                  <Input
                    className="h-6 flex-1 text-xs"
                    placeholder="Write a reply…"
                    value={replyText}
                    onChange={(ev) => setReplyText(ev.target.value)}
                  />
                  <Button type="submit" size="xs" className="h-6 text-[10px]">
                    Send
                  </Button>
                </form>
              )}
            </div>
          ))}
          <form onSubmit={(e) => void submit(e)} className="flex items-center gap-1">
            <Input
              className="h-6 flex-1 text-xs"
              placeholder="Add comment or note on this unit of work…"
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
            />
            <Button type="submit" size="xs" className="h-6 text-[11px]" disabled={!draft.trim()}>
              Post
            </Button>
          </form>
        </div>
      )}
    </div>
  )
}

function hashWorkstreamSeed(s: string): number {
  let h = 2166136261
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i)
    h = Math.imul(h, 16777619)
  }
  return h >>> 0
}

function cleanPurposeSnippet(text?: string): string {
  if (!text) return ""
  const line =
    text
      .split(/\r?\n/)
      .map((l) => l.replace(/^#+\s*/, "").trim())
      .find((l) => l.length > 0) ?? ""
  return line
}

const LAST_SOURCE_ICON: Record<string, string> = {
  torque: "list-checks",
  comment: "message-square-text",
  status: "sparkles",
  ops: "git-commit-horizontal",
  liveness: "user-x",
}

/**
 * Tachyon-inspired stepped activity bar graph (`ActivityBars` prior art) that reacts to the
 * shared Surface event bus (`ops.activity`, `ops.changed`, `agent.*`, `cards.changed`) and
 * animates with live motion for active/in-flight workstreams.
 */
export function WorkstreamActivityPulse({
  w,
  width = 58,
  height = 16,
}: {
  w: Pick<Card, "id" | "stage" | "held" | "last" | "transitions" | "task_counts" | "updated">
  width?: number
  height?: number
}) {
  const isWorking = w.stage === "in_flight" && !w.held
  const isBlocked = !!w.held
  const isLanding = w.stage === "landing"
  const isComplete = w.stage === "complete"

  const [tick, setTick] = useState(0)
  const [busBoost, setBusBoost] = useState(0)

  // Listen to the shared event bus for events touching this workstream or global ops activity
  useEvent<{ id?: string; workstream?: string }>("ops.activity", (ev) => {
    if (!ev.data?.id || ev.data.id === w.id || ev.data.workstream === w.id) {
      setBusBoost((b) => Math.min(40, b + 22))
      setTick((t) => t + 1)
    }
  })
  useEvent<{ id?: string }>("ops.changed", (ev) => {
    if (!ev.data?.id || ev.data.id === w.id) {
      setBusBoost((b) => Math.min(40, b + 18))
      setTick((t) => t + 1)
    }
  })

  // Step-animate active / in-flight / recently boosted graphs so motion is visible at a glance
  useEffect(() => {
    if (!isWorking && !isLanding && busBoost <= 0) return
    const interval = window.setInterval(
      () => {
        setTick((t) => (t + 1) % 1024)
        setBusBoost((b) => Math.max(0, b - 4))
      },
      isWorking ? 1400 : 2200,
    )
    return () => window.clearInterval(interval)
  }, [isWorking, isLanding, busBoost])

  const series = useMemo(() => {
    const seed = hashWorkstreamSeed(w.id)
    const buckets = 12
    const lastMs = w.last?.at ? Date.now() - new Date(w.last.at).getTime() : Number.POSITIVE_INFINITY
    const recencyBoost = lastMs < 15 * 60_000 ? 35 : lastMs < 60 * 60_000 ? 22 : lastMs < 6 * 3600_000 ? 12 : 0
    const baseLevel = isWorking ? 48 : isLanding ? 36 : isBlocked ? 24 : isComplete ? 18 : 10

    return Array.from({ length: buckets }, (_, i) => {
      const phase = ((seed >> (i % 16)) & 0xf) / 15
      const wave = isWorking || isLanding ? Math.sin((i + tick) * 0.85 + (seed % 7)) * 22 : 0
      const tailWeight = i >= buckets - 3 ? recencyBoost + busBoost : 0
      const raw = baseLevel + phase * 28 + wave + tailWeight * ((i - (buckets - 4)) / 3)
      const v =
        !isWorking && !isLanding && !isBlocked && recencyBoost === 0 && i < buckets - 2
          ? Math.max(6, Math.round(phase * 18))
          : Math.min(100, Math.max(8, Math.round(raw)))
      return { slot: i, v }
    })
  }, [w.id, w.last?.at, isWorking, isLanding, isBlocked, isComplete, tick, busBoost])

  const color = isBlocked
    ? "var(--color-warning)"
    : isWorking
      ? "var(--color-success)"
      : isLanding
        ? "var(--color-primary)"
        : "var(--color-muted-foreground)"

  const gap = 1.2
  const bw = (width - gap * (series.length - 1)) / series.length

  return (
    <span
      className="inline-flex shrink-0 items-center gap-1.5 rounded-xs border border-border/60 bg-background/70 px-1.5 py-0.5"
      title={
        isBlocked
          ? `Held: ${w.held?.reason ?? "Paused"}`
          : isWorking
            ? `Active stream · ${w.last ? `last event ${age(w.last.at)} ago` : "monitoring event bus"}`
            : w.last
              ? `Last activity ${age(w.last.at)} ago`
              : `Stage: ${stageLabel(w.stage)}`
      }
    >
      <span
        className={`size-1.5 shrink-0 rounded-full ${
          isBlocked
            ? "bg-warning"
            : isWorking
              ? "animate-pulse bg-success"
              : isLanding
                ? "bg-primary"
                : "bg-muted-foreground/50"
        }`}
      />
      <svg
        width={width}
        height={height}
        viewBox={`0 0 ${width} ${height}`}
        shapeRendering="crispEdges"
        aria-hidden="true"
        className="shrink-0"
      >
        {series.map((pt) => {
          const h = Math.max(2, Math.round((pt.v / 100) * height))
          const isTail = pt.slot >= series.length - 2
          return (
            <rect
              key={`bar-${w.id}-${pt.slot}`}
              x={pt.slot * (bw + gap)}
              y={height - h}
              width={bw}
              height={h}
              fill={color}
              opacity={0.35 + 0.65 * (pt.slot / series.length)}
              className={isTail && (isWorking || busBoost > 0) ? " transition-all duration-300" : ""}
            />
          )
        })}
      </svg>
    </span>
  )
}

/**
 * Board-level live event bus monitor widget for the Operations header bar.
 * Reacts to every event on Surface's shared event bus (`ops.*`, `agent.*`, `cards.*`).
 */
export function EventBusHeaderMonitor({ activeCount = 0 }: { activeCount?: number }) {
  const [bars, setBars] = useState<number[]>(() => [24, 32, 28, 45, 38, 52, 40, 60, 48, 66, 54, 72, 62, 78])
  const [pulseCount, setPulseCount] = useState(0)

  const bump = () => {
    setPulseCount((c) => c + 1)
    setBars((prev) => [...prev.slice(1), Math.min(100, 68 + Math.round(Math.random() * 30))])
  }

  useEvent("ops.changed", bump)
  useEvent("ops.activity", bump)
  useEvent("cards.changed", bump)

  useEffect(() => {
    const id = window.setInterval(() => {
      setBars((prev) => {
        const base = activeCount > 0 ? 38 + Math.min(35, activeCount * 7) : 18
        const jitter = Math.round((Math.random() - 0.45) * 24)
        const next = Math.min(98, Math.max(12, base + jitter))
        return [...prev.slice(1), next]
      })
    }, 1800)
    return () => window.clearInterval(id)
  }, [activeCount])

  const width = 68
  const height = 16
  const gap = 1.2
  const bw = (width - gap * (bars.length - 1)) / bars.length
  const barSlots = bars.map((v, slot) => ({ slot, v }))

  return (
    <div
      className="hidden items-center gap-1.5 rounded-sm border border-border/75 bg-muted/30 px-2 py-1 font-mono text-[10.5px] text-muted-foreground sm:flex"
      title={`Event bus telemetry · ${activeCount} in flight${pulseCount > 0 ? ` · ${pulseCount} live events observed` : ""}`}
    >
      <span className="size-1.5 animate-pulse rounded-full bg-success" />
      <span className="uppercase tracking-wider text-[9.5px] text-foreground/85">Bus</span>
      <svg
        width={width}
        height={height}
        viewBox={`0 0 ${width} ${height}`}
        shapeRendering="crispEdges"
        aria-hidden="true"
      >
        {barSlots.map((pt) => {
          const h = Math.max(2, Math.round((pt.v / 100) * height))
          return (
            <rect
              key={`bus-bar-${pt.slot}`}
              x={pt.slot * (bw + gap)}
              y={height - h}
              width={bw}
              height={h}
              fill="var(--color-primary)"
              opacity={0.3 + 0.7 * (pt.slot / bars.length)}
            />
          )
        })}
      </svg>
    </div>
  )
}

/**
 * A workstream on the Kanban / Cards board.
 * - Top line: full-width title + `...` ObjectActionsMenu.
 * - Clicking the title or body expands/collapses the inline work units & notes breakdown.
 * - Concise body: live reactive activity graph + progress bar, icon-first state indicators,
 *   purpose snippet, and latest activity pill.
 * - Footer line: expand toggle & children step-in on the left; Send to Orchestration, Request Status,
 *   and Open Detail Modal actions on the right.
 */
export function WorkstreamCard({
  w,
  childWorkstreams = [],
  open,
  openNewWindow,
  enter,
  onRequestStatus,
  draggable = true,
  lead,
  actions,
}: {
  w: Card
  childWorkstreams?: Card[]
  open: (id: string, newWindow?: boolean) => void
  openNewWindow?: (id: string) => void
  enter: (id: string) => void
  onRequestStatus?: (id: string) => void
  draggable?: boolean
  lead?: ReactNode
  actions?: ReactNode
}) {
  const [expanded, setExpanded] = useState(false)
  const progress = useMemo(() => computeWorkUnits(w, childWorkstreams), [w, childWorkstreams])
  const marks = useMarks(`ops:${w.id}`)
  const pct = progress.total > 0 ? Math.round((progress.done / progress.total) * 100) : 0
  const purposeSnippet = useMemo(() => cleanPurposeSnippet(w.purpose || w.active_means), [w.purpose, w.active_means])

  return (
    <div
      data-ops-card={w.id}
      className="group flex min-w-0 cursor-pointer flex-col gap-2 rounded-md border border-border/85 bg-card/95 px-2.5 py-2 text-sm shadow-2xs transition-all hover:border-primary/45 hover:bg-card"
      draggable={draggable && !w.settled_at}
      onDragStart={(e) => {
        e.dataTransfer.setData(DRAG_TYPE, w.id)
        e.dataTransfer.effectAllowed = "move"
      }}
      onClick={(e) => {
        if (e.metaKey || e.ctrlKey || e.shiftKey) {
          if (openNewWindow) openNewWindow(w.id)
          else open(w.id, true)
          return
        }
        setExpanded((v) => !v)
      }}
      onAuxClick={(e) => {
        if (e.button === 1) {
          e.preventDefault()
          if (openNewWindow) openNewWindow(w.id)
          else open(w.id, true)
        }
      }}
    >
      {/* 1. Title Row: Full line dedicated to the Title + "..." menu */}
      <div className="flex min-w-0 items-start justify-between gap-1.5">
        <div className="flex min-w-0 flex-1 items-start gap-1.5">
          {lead}
          <span className="line-clamp-2 min-w-0 flex-1 font-medium text-[13px] text-foreground leading-snug">
            {w.title}
          </span>
        </div>
        <span className="shrink-0" onClick={(e) => e.stopPropagation()}>
          <ObjectActionsMenu
            object={{
              kind: "ops.workstream",
              id: w.id,
              title: w.title,
              subtitle: stageLabel(w.stage),
            }}
          />
        </span>
      </div>

      {/* 2. Progress Bar + Tachyon-style Reactive Event Bus Activity Graph */}
      <div className="flex items-center gap-2">
        <WorkstreamActivityPulse w={w} />
        <div className="flex min-w-0 flex-1 items-center gap-1.5">
          <div className="h-1.5 flex-1 overflow-hidden rounded-full bg-muted/80">
            <div
              className={`h-full transition-all ${
                w.held
                  ? "bg-warning"
                  : progress.done === progress.total && progress.total > 0
                    ? "bg-success"
                    : "bg-primary"
              }`}
              style={{
                width: `${
                  progress.total > 0
                    ? pct
                    : w.stage === "complete"
                      ? 100
                      : w.stage === "landing"
                        ? 80
                        : w.stage === "in_flight"
                          ? 45
                          : w.stage === "queued"
                            ? 20
                            : 8
                }%`,
              }}
            />
          </div>
          {progress.total > 0 && (
            <span className="shrink-0 font-mono text-[10px] text-muted-foreground tabular-nums">
              {progress.done}/{progress.total}
            </span>
          )}
        </div>
      </div>

      {/* 3. Compact Icon-First State & Lifecycle Strip */}
      <div className="flex flex-wrap items-center gap-1.5 font-mono text-[10.5px] text-muted-foreground">
        {w.stage_since && (
          <span
            className="inline-flex items-center gap-1 rounded-xs bg-muted/45 px-1.5 py-0.5"
            title={`In ${stageLabel(w.stage)} since ${w.stage_since}`}
          >
            <Icon name="clock" size={10} className="text-primary/80" />
            {age(w.stage_since)}
          </span>
        )}
        {w.owner && (
          <span
            className="inline-flex max-w-28 items-center gap-1 truncate rounded-xs bg-muted/45 px-1.5 py-0.5"
            title={`Owner: ${w.owner}`}
          >
            <Icon name="user" size={10} />
            <span className="truncate">{w.owner}</span>
          </span>
        )}
        {w.orchestration_request && (
          <span
            className={`inline-flex items-center gap-1 rounded-xs border px-1.5 py-0.5 ${
              w.orchestration_request.message_id
                ? "border-primary/35 bg-primary/10 text-primary"
                : "border-warning/40 bg-warning/10 text-warning"
            }`}
            title={
              w.orchestration_request.message_id
                ? `Sent to orchestration (${w.orchestration_request.at})`
                : "Orchestration send pending"
            }
          >
            <Icon name="send" size={9} />
            {w.orchestration_request.message_id ? "Orch" : "Pending"}
          </span>
        )}
        {w.settled_at && (
          <span className="inline-flex items-center gap-1 rounded-xs border border-success/35 bg-success/10 px-1.5 py-0.5 text-success">
            <Icon name="check-circle-2" size={10} />
            Settled
          </span>
        )}
        {w.held && (
          <span
            className="inline-flex items-center gap-1 rounded-xs border border-destructive/40 bg-destructive/10 px-1.5 py-0.5 text-destructive"
            title={`Held: ${w.held.reason}`}
          >
            <Icon name="pause-circle" size={10} />
            Held
          </span>
        )}
        {(w.no_live_agent?.length ?? 0) > 0 && (
          <span
            className="inline-flex items-center gap-1 rounded-xs border border-warning/40 bg-warning/10 px-1.5 py-0.5 text-warning"
            title={`Assigned or claimed with no live agent: ${w.no_live_agent?.join(", ")}`}
          >
            <Icon name="user-x" size={10} />
            No live agent ({w.no_live_agent?.length})
          </span>
        )}
        {w.suggest && (
          <span
            className="inline-flex items-center gap-1 rounded-xs border border-warning/40 bg-warning/10 px-1.5 py-0.5 text-warning"
            title={`Suggested move to ${stageLabel(w.suggest)}: ${w.suggest_why ?? ""}`}
          >
            <Icon name="sparkles" size={10} />→ {stageLabel(w.suggest)}
          </span>
        )}
        {marks.length > 0 && (
          <span
            className="inline-flex items-center gap-0.5 rounded-xs bg-primary/10 px-1.5 py-0.5 text-primary"
            title={`${marks.length} note${marks.length === 1 ? "" : "s"}`}
          >
            <Icon name="message-square-text" size={10} />
            {marks.length}
          </span>
        )}
      </div>

      {/* 4. Concise Purpose Perspective & Latest Activity Snapshot */}
      {(purposeSnippet || w.last || w.held || w.problem) && (
        <div className="flex flex-col gap-1 text-xs">
          {(w.held || w.problem) && (
            <div className="flex items-start gap-1.5 rounded-xs border border-destructive/35 bg-destructive/8 px-2 py-1 text-[11px] text-destructive">
              <Icon name="alert-triangle" size={11} className="mt-0.5 shrink-0" />
              <span className="line-clamp-1">{w.held?.reason || w.problem}</span>
            </div>
          )}
          {purposeSnippet && (
            <div
              className="flex items-center gap-1.5 text-[11.5px] text-muted-foreground"
              title={w.purpose || w.active_means}
            >
              <Icon name="compass" size={11} className="shrink-0 text-primary/75" />
              <span className="truncate">{purposeSnippet}</span>
            </div>
          )}
          {w.last && (
            <div className="flex items-center gap-1.5 rounded-xs border border-border/50 bg-muted/30 px-2 py-1 text-[11px] text-muted-foreground">
              <Icon name={LAST_SOURCE_ICON[w.last.source] ?? "activity"} size={11} className="shrink-0 text-primary" />
              <span className="shrink-0 font-mono text-[10px] text-foreground/80">{age(w.last.at)}</span>
              <span className="min-w-0 flex-1 truncate text-foreground/90">
                <CiteText text={w.last.summary} />
              </span>
            </div>
          )}
        </div>
      )}

      {/* 5. Inline Expandable Work Breakdown (x of y), Purpose & Notes (toggled by clicking Title/Body) */}
      {expanded && (
        <div
          className="mt-0.5 flex flex-col gap-2 rounded-sm border border-border/70 bg-muted/20 p-2 text-xs"
          onClick={(e) => e.stopPropagation()}
        >
          {w.purpose && (
            <div className="flex flex-col gap-0.5 border-b border-border/50 pb-1.5">
              <span className="font-mono text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
                Purpose &amp; Active Signal
              </span>
              <p className="text-[11.5px] text-foreground/90 leading-relaxed">
                <CiteText text={w.purpose} />
              </p>
            </div>
          )}

          <div className="flex items-center justify-between">
            <span className="font-mono text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
              Work Units ({progress.done} of {progress.total})
            </span>
            {w.tasks && <span className="font-mono text-[10px] text-muted-foreground">{w.tasks}</span>}
          </div>

          {progress.units.length === 0 ? (
            <p className="text-[11px] text-muted-foreground italic">No work units linked yet.</p>
          ) : (
            <ul className="flex flex-col gap-1">
              {progress.units.map((u) => (
                <li
                  key={u.id}
                  className="flex flex-col gap-1 rounded-sm border border-border/50 bg-background/70 px-2 py-1"
                >
                  <div className="flex min-w-0 items-center gap-1.5">
                    <Icon
                      name={
                        u.done
                          ? "circle-check"
                          : u.kind === "child"
                            ? "layers"
                            : u.kind === "task"
                              ? "list-checks"
                              : "square"
                      }
                      size={12}
                      className={u.done ? "shrink-0 text-success" : "shrink-0 text-muted-foreground"}
                    />
                    <span
                      className={`min-w-0 flex-1 truncate text-[11px] ${
                        u.done ? "text-muted-foreground line-through" : "text-foreground"
                      }`}
                    >
                      {u.kind === "child" ? (
                        <Button
                          variant="link"
                          size="xs"
                          className="h-auto p-0 text-[11px]"
                          onClick={() => open(u.objectRef.id, false)}
                        >
                          {u.title}
                        </Button>
                      ) : u.kind === "task" ? (
                        <CiteRef refId={{ kind: "torque.task", canonical: u.objectRef.id }} label={u.title} />
                      ) : (
                        <CiteText text={u.title} />
                      )}
                    </span>
                    <Badge variant="outline" className="h-4 shrink-0 px-1 text-[9px]">
                      {u.status}
                    </Badge>
                    <ObjectActionsMenu object={u.objectRef} />
                  </div>
                  <UnitNotesInline targetRef={u.ref} compact />
                </li>
              ))}
            </ul>
          )}

          <UnitNotesInline targetRef={`ops:${w.id}`} compact />
        </div>
      )}

      {/* 6. Dedicated Card Footer Line: Expand/Children on Left, Orchestration & Detail Modal Actions on Right */}
      <div
        className="mt-auto flex items-center justify-between gap-1 border-t border-border/55 pt-1.5"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center gap-1">
          <Button
            size="xs"
            variant="ghost"
            className="h-6 gap-1 px-1.5 font-mono text-[10.5px] text-muted-foreground hover:text-foreground"
            onClick={() => setExpanded((v) => !v)}
            title={expanded ? "Collapse inline breakdown" : "Expand inline work units and notes"}
          >
            <Icon name={expanded ? "chevron-up" : "chevron-down"} size={11} />
            <span>{expanded ? "Less" : "Units"}</span>
          </Button>
          {w.children.length > 0 && (
            <Button
              size="xs"
              variant="ghost"
              className="h-6 gap-1 px-1.5 font-mono text-[10.5px] text-muted-foreground hover:text-foreground"
              onClick={() => enter(w.id)}
              title="Step inside child workstreams on the board"
            >
              <Icon name="layers" size={11} />
              <span>{w.children.length}</span>
            </Button>
          )}
        </div>

        <div className="flex items-center gap-0.5">
          {actions}
          <Button
            size="icon-xs"
            variant="ghost"
            className="size-6 text-muted-foreground hover:text-foreground"
            title="Request status update from agent"
            aria-label={`Request status for ${w.title}`}
            onClick={() => {
              requestWorkstreamStatus(`ops:${w.id}`, w.title, () => onRequestStatus?.(w.id))
            }}
          >
            <Icon name="radio" size={11} />
          </Button>
          <Button
            size="xs"
            variant="ghost"
            className="h-6 gap-1 px-1.5 font-mono text-[10.5px] text-primary hover:bg-primary/12 hover:text-primary"
            title="Open full workstream detail modal (⌘-click for separate window)"
            aria-label={`Open ${w.title} detail view`}
            onClick={(e) => {
              if (e.metaKey || e.ctrlKey || e.shiftKey) {
                if (openNewWindow) openNewWindow(w.id)
                else open(w.id, true)
                return
              }
              open(w.id, false)
            }}
          >
            <Icon name="maximize-2" size={11} />
            <span>Detail</span>
          </Button>
        </div>
      </div>
    </div>
  )
}
