/**
 * Itinerary-inspired Activity Row & Episode Header for Operations (`modules/ops`).
 *
 * Visual structure (inspired by the flight itinerary reference):
 * - Left column (`w-20 text-right`): Crisp bold local timestamp (`14:10`) at the node, with
 *   muted elapsed duration / relative age (`3h 25m`) along the vertical segment below it.
 * - Middle spine column (`w-8`): Continuous 1px vertical spine with circular milestone dots and
 *   semantic action icons (`list-checks`, `arrow-right-left`, `radio`, `message-square-text`)
 *   sitting directly on the spine.
 * - Right content column:
 *   1. Milestone headline (Workstream / Stage / Target context).
 *   2. Clickable Rich Item Card (circular colored action badge on the left, bold primary title,
 *      related aspects separated by `|`, `<ObjectActionsMenu />`, and a right chevron `>` that
 *      opens the richer view of that item).
 *   3. Distinct Shaded Callout Div (`bg-muted/45`) with a colored header and bulleted related
 *      aspects, details, and inline unit notes/comments.
 */
import { CiteRef, CiteText, Icon, ObjectActionsMenu, type ObjectRef } from "@hollis-labs/surface-page/sdk"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { age, type Card, type Entry, stageLabel, time } from "./api"
import { OpsChip, StageBadge, UnitNotesInline } from "./parts"

export const SOURCE_ICON: Record<string, string> = {
  torque: "list-checks",
  comment: "message-square",
  status: "activity",
  ops: "radar",
}
export const SOURCE_LABEL: Record<string, string> = {
  torque: "Torque",
  comment: "Status comments",
  status: "Status stream",
  ops: "Ops moves",
}

/** Format an ISO timestamp as a crisp 24-hour local clock time (`14:10`). */
export function clockTime(iso?: string): string {
  if (!iso) return "—"
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return "—"
  return d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", hour12: false })
}

/** Format an ISO timestamp as a short day header (`Thu, Nov 21`). */
export function dayLabel(iso?: string): string {
  if (!iso) return "Recent"
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return "Recent"
  return d.toLocaleDateString([], { weekday: "short", month: "short", day: "numeric" })
}

/** Compute human-friendly elapsed duration between two ISO timestamps (`3h 25m`, `14m`, `2d 4h`). */
export function spanBetween(newerIso?: string, olderIso?: string): string {
  if (!newerIso || !olderIso) return ""
  const ms = Math.abs(new Date(newerIso).getTime() - new Date(olderIso).getTime())
  if (Number.isNaN(ms) || ms < 30_000) return "<1m"
  const totalMin = Math.round(ms / 60_000)
  if (totalMin < 60) return `${totalMin}m`
  const h = Math.floor(totalMin / 60)
  const m = totalMin % 60
  if (h < 24) return m > 0 ? `${h}h ${m}m` : `${h}h`
  const d = Math.floor(h / 24)
  const remH = h % 24
  return remH > 0 ? `${d}d ${remH}h` : `${d}d`
}

interface ActionVisual {
  icon: string
  spineIcon: string
  circleClass: string
  spineColorClass: string
  calloutTitleClass: string
  actionVerb: string
}

function visualForEntry(e: Entry): ActionVisual {
  const st = (e.state ?? "").toLowerCase()
  if (e.source === "ops") {
    return {
      icon: "arrow-right-left",
      spineIcon: "git-commit-horizontal",
      circleClass: "bg-primary/15 text-primary border border-primary/30",
      spineColorClass: "text-primary",
      calloutTitleClass: "text-primary",
      actionVerb: "Stage Transition",
    }
  }
  if (e.source === "torque") {
    if (st === "done" || st === "completed" || st === "closed") {
      return {
        icon: "circle-check",
        spineIcon: "check",
        circleClass: "bg-success/15 text-success border border-success/30",
        spineColorClass: "text-success",
        calloutTitleClass: "text-success",
        actionVerb: "Task Completed",
      }
    }
    if (st === "doing" || st === "in_progress" || st === "active") {
      return {
        icon: "play",
        spineIcon: "refresh-cw",
        circleClass: "bg-primary/15 text-primary border border-primary/30",
        spineColorClass: "text-primary",
        calloutTitleClass: "text-primary",
        actionVerb: "Task In Flight",
      }
    }
    if (st === "blocked" || st === "failed") {
      return {
        icon: "triangle-alert",
        spineIcon: "octagon-pause",
        circleClass: "bg-destructive/15 text-destructive border border-destructive/30",
        spineColorClass: "text-destructive",
        calloutTitleClass: "text-destructive",
        actionVerb: "Task Blocked",
      }
    }
    return {
      icon: "list-checks",
      spineIcon: "list-checks",
      circleClass: "bg-chart-2/15 text-chart-2 border border-chart-2/30",
      spineColorClass: "text-chart-2",
      calloutTitleClass: "text-chart-2",
      actionVerb: "Torque Task",
    }
  }
  if (e.source === "status") {
    return {
      icon: "activity",
      spineIcon: "refresh-cw",
      circleClass: "bg-warning/15 text-warning border border-warning/30",
      spineColorClass: "text-warning",
      calloutTitleClass: "text-warning",
      actionVerb: e.verified ? "Verified Status" : "Status Report",
    }
  }
  return {
    icon: "message-square-text",
    spineIcon: "message-square",
    circleClass: "bg-chart-3/15 text-chart-3 border border-chart-3/30",
    spineColorClass: "text-chart-3",
    calloutTitleClass: "text-chart-3",
    actionVerb: "Status Comment",
  }
}

export function EpisodeHeaderBar({
  badgeText,
  badgeTone = "primary",
  dateText,
  routeText,
  durationText,
  countText,
}: {
  badgeText: string
  badgeTone?: "primary" | "success" | "chart-2" | "warning"
  dateText: string
  routeText: string
  durationText?: string
  countText?: string
}) {
  const pillTone =
    badgeTone === "success"
      ? "bg-success/15 text-success border-success/30"
      : badgeTone === "chart-2"
        ? "bg-chart-2/15 text-chart-2 border-chart-2/30"
        : badgeTone === "warning"
          ? "bg-warning/15 text-warning border-warning/30"
          : "bg-primary/15 text-primary border-primary/30"

  return (
    <div className="flex flex-wrap items-center gap-2.5 py-2 text-xs">
      <span
        className={`inline-flex items-center gap-1 rounded-sm border px-2 py-0.5 font-mono text-[11px] font-semibold ${pillTone}`}
      >
        {badgeText}
        <Icon name="arrow-right" size={11} />
      </span>
      <span className="font-semibold text-foreground">{dateText}</span>
      <span className="text-border">|</span>
      <span className="font-semibold text-foreground">{routeText}</span>
      {(durationText || countText) && (
        <>
          <span className="text-border">|</span>
          <span className="inline-flex items-center gap-1 font-mono text-muted-foreground">
            <Icon name="clock" size={12} />
            {[durationText, countText].filter(Boolean).join(" · ")}
          </span>
        </>
      )}
    </div>
  )
}

export function ActivityRow({
  e,
  id,
  open,
  toggle,
  workstreamCard,
  nextAt,
  onOpenWorkstream,
}: {
  e: Entry
  id: string
  open: boolean
  toggle: () => void
  workstreamCard?: Card
  nextAt?: string
  onOpenWorkstream?: (wsId: string, newWindow?: boolean) => void
}) {
  const vis = visualForEntry(e)
  const deltaText = nextAt ? spanBetween(e.at, nextAt) : age(e.at)
  const primaryObject: ObjectRef = e.task
    ? {
        kind: "torque.task",
        id: e.task,
        title: `${e.task} · ${e.summary.slice(0, 72)}`,
        subtitle: e.state || SOURCE_LABEL[e.source],
      }
    : {
        kind: "ops.workstream",
        id: e.workstream,
        title: workstreamCard?.title || `ops:${e.workstream}`,
        subtitle: e.state || SOURCE_LABEL[e.source],
      }

  const showCalloutByDefault = e.source === "comment" || e.source === "status" || open
  const headlineTitle = e.task
    ? `${e.task} · ${vis.actionVerb}`
    : workstreamCard?.title
      ? `${workstreamCard.title}`
      : `${vis.actionVerb} · ops:${e.workstream}`

  const aspects: { label: string; value: string }[] = [{ label: "Source", value: SOURCE_LABEL[e.source] ?? e.source }]
  if (e.state) aspects.push({ label: "State", value: e.state })
  if (workstreamCard?.stage) aspects.push({ label: "Stage", value: stageLabel(workstreamCard.stage) })
  if (workstreamCard?.owner) aspects.push({ label: "Owner", value: workstreamCard.owner })
  if (e.source === "status") aspects.push({ label: "Trust", value: e.verified ? "verified" : "claimed" })

  const noteTargetRef = e.task || `ops:${e.workstream}`

  return (
    <div className="grid grid-cols-[4.5rem_1.75rem_1fr] items-stretch gap-x-2">
      {/* Column 1: Timestamp & Elapsed Duration */}
      <div className="flex flex-col items-end pr-1 pt-0.5 text-right font-mono">
        <span className="font-semibold text-foreground text-xs leading-5" title={time(e.at)}>
          {clockTime(e.at)}
        </span>
        <span className="mt-3 text-[11px] text-muted-foreground" title={`Age: ${age(e.at)} ago`}>
          {deltaText || age(e.at)}
        </span>
      </div>

      {/* Column 2: Vertical Spine, Milestone Node & Action Icon */}
      <div className="relative flex flex-col items-center">
        <div className="absolute inset-y-0 left-1/2 w-px -translate-x-1/2 bg-border/80" />
        <span className="relative z-10 mt-1.5 size-2.5 shrink-0 rounded-full border-2 border-muted-foreground/70 bg-background" />
        <span
          className={`relative z-10 mt-2.5 flex size-5 shrink-0 items-center justify-center rounded-full bg-background ${vis.spineColorClass}`}
          title={SOURCE_LABEL[e.source]}
        >
          <Icon name={SOURCE_ICON[e.source] ?? vis.spineIcon} size={13} aria-label={SOURCE_LABEL[e.source]} />
        </span>
      </div>

      {/* Column 3: Milestone Line, Clickable Rich Item Card & Shaded Callout Div */}
      <div className="flex min-w-0 flex-col gap-2 pb-4 pt-0.5">
        {/* Top Milestone Checkpoint Line */}
        <div className="flex flex-wrap items-center gap-2 text-xs">
          <span className="font-semibold text-foreground">
            {workstreamCard?.title ?? (e.workstream ? `Workstream ${e.workstream}` : vis.actionVerb)}
          </span>
          {e.workstream !== id && <OpsChip id={e.workstream} />}
          {workstreamCard?.stage && <StageBadge stage={workstreamCard.stage} />}
          {e.source === "status" && (
            <Badge
              variant="outline"
              className="h-4 px-1.5 text-[10px]"
              title="Shown as the writer claimed it: no writer is verified yet"
            >
              {e.verified ? "verified" : "claimed"}
            </Badge>
          )}
          <span className="ml-auto font-mono text-[11px] text-muted-foreground" title={time(e.at)}>
            {age(e.at)} ago
          </span>
        </div>

        {/* Clickable Rich Entity / Action Card (inspired by the flight leg card) */}
        <div
          onClick={toggle}
          className="group/actcard flex max-w-2xl cursor-pointer items-center gap-3 rounded-md border border-border/90 bg-card px-3.5 py-2.5 shadow-2xs transition-all hover:border-primary/50 hover:bg-accent/35"
          title="Click to inspect full activity details, related references, and notes"
        >
          <div className={`flex size-8 shrink-0 items-center justify-center rounded-full ${vis.circleClass}`}>
            <Icon name={vis.icon} size={15} />
          </div>

          <div className="flex min-w-0 flex-1 flex-col gap-0.5">
            <div className="flex flex-wrap items-center gap-2">
              <span className="truncate font-semibold text-foreground text-xs">{headlineTitle}</span>
              {e.state && (
                <Badge
                  variant={e.state === "doing" || e.state === "in_flight" ? "default" : "secondary"}
                  className="h-4 px-1.5 font-mono text-[10px]"
                >
                  {e.state}
                </Badge>
              )}
            </div>

            {/* Related Aspects Strip separated by vertical pipes */}
            <div className="flex flex-wrap items-center gap-x-2 gap-y-0.5 text-[11px] text-muted-foreground">
              {aspects.map((asp, idx) => (
                <span key={asp.label} className="inline-flex items-center gap-1.5">
                  {idx > 0 && <span className="text-border">|</span>}
                  <span>
                    {asp.label}: <strong className="font-medium text-foreground/85">{asp.value}</strong>
                  </span>
                </span>
              ))}
              {e.task && (
                <span className="inline-flex items-center gap-1.5" onClick={(ev) => ev.stopPropagation()}>
                  <span className="text-border">|</span>
                  <CiteRef refId={{ kind: "torque.task", canonical: e.task }} />
                </span>
              )}
            </div>
          </div>

          <div className="ml-auto flex shrink-0 items-center gap-1" onClick={(ev) => ev.stopPropagation()}>
            {onOpenWorkstream && e.workstream && e.workstream !== id && (
              <Button
                variant="ghost"
                size="xs"
                className="hidden h-6 px-2 text-[11px] group-hover/actcard:inline-flex"
                onClick={() => onOpenWorkstream(e.workstream, false)}
                title="Open workstream detail"
              >
                Open
              </Button>
            )}
            <ObjectActionsMenu obj={primaryObject} />
            <Button
              size="icon-xs"
              variant="ghost"
              aria-label={open ? "Less" : "More"}
              onClick={toggle}
              className="text-muted-foreground group-hover/actcard:text-foreground"
            >
              <Icon name={open ? "chevron-up" : "chevron-right"} size={14} />
            </Button>
          </div>
        </div>

        {/* Distinct Shaded Callout Div (inspired by the "Transfer in Frankfurt" shaded container) */}
        <div className="max-w-2xl rounded-md border border-border/50 bg-muted/45 px-3.5 py-2.5 text-xs">
          <div className="flex items-center justify-between gap-2">
            <span className={`font-semibold text-xs ${vis.calloutTitleClass}`}>
              {vis.actionVerb}
              {e.task ? ` · ${e.task}` : e.state ? ` · ${e.state}` : ""}
            </span>
            {!open && (
              <Button
                variant="ghost"
                size="xs"
                className="h-5 px-1.5 text-[10px] text-muted-foreground hover:text-foreground"
                onClick={toggle}
              >
                Details
                <Icon name="chevron-right" size={10} />
              </Button>
            )}
          </div>

          <ul className="mt-1 flex list-disc flex-col gap-1 pl-4 text-foreground/90 leading-relaxed">
            <li>
              <CiteText text={e.summary} />
            </li>
            {showCalloutByDefault && e.subject_urn && (
              <li className="text-muted-foreground">
                Subject: <CiteText text={e.subject_urn} />
              </li>
            )}
          </ul>

          {/* Expanded Rich Inspector & Metadata (<dl> kept for semantic & test parity) */}
          {open && (
            <div className="mt-2.5 flex flex-col gap-2 border-t border-border/60 pt-2">
              <dl className="grid grid-cols-[6rem_1fr] gap-x-2 gap-y-1 text-xs">
                <dt className="text-muted-foreground">At</dt>
                <dd className="font-mono">{time(e.at)}</dd>
                <dt className="text-muted-foreground">Source</dt>
                <dd>{SOURCE_LABEL[e.source]}</dd>
                {e.state && (
                  <>
                    <dt className="text-muted-foreground">State</dt>
                    <dd>{e.state}</dd>
                  </>
                )}
                {e.task && (
                  <>
                    <dt className="text-muted-foreground">Task</dt>
                    <dd>
                      <CiteText text={e.task} />
                    </dd>
                  </>
                )}
                {e.subject_urn && (
                  <>
                    <dt className="text-muted-foreground">Subject</dt>
                    <dd>
                      <CiteText text={e.subject_urn} />
                    </dd>
                  </>
                )}
                <dt className="text-muted-foreground">Workstream</dt>
                <dd>
                  <OpsChip id={e.workstream} />
                </dd>
              </dl>
              <UnitNotesInline targetRef={noteTargetRef} compact />
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
