import { CiteRef, Icon, ObjectActionsMenu } from "@hollis-labs/surface-page/sdk"
import { useMemo } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { age, time } from "./api"
import type { SessionRow } from "./sessions"

export function parseAgentIdentity(rawName: string): {
  baseName: string
  instanceTag: string
  roleLabel: string
} {
  const trimmed = (rawName || "agent").replace(/^msg:\/\/agent\/[^/]+\//, "")
  const [basePart, hashPart] = trimmed.split("#")
  const baseName = basePart || trimmed
  const instanceTag = hashPart ? `#${hashPart.slice(0, 8)}` : ""

  const lower = baseName.toLowerCase()
  const roleLabel = lower.includes("orchestrator")
    ? "orchestrator"
    : lower.includes("reviewer") || lower.includes("merger")
      ? "reviewer"
      : lower.includes("worker")
        ? "worker"
        : lower.includes("planner")
          ? "planner"
          : "agent"

  return { baseName, instanceTag, roleLabel }
}

function hashAgentSeed(s: string): number {
  let h = 2166136261
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i)
    h = Math.imul(h, 16777619)
  }
  return h >>> 0
}

/**
 * Technical line-art Agent Core / Neural Unit SVG illustration.
 * Matches `<ServerLineArt />` in Fleet with blueprint corner ticks, processor core frame,
 * signal pins, and status LEDs.
 */
export function AgentLineArt({
  state,
  rawState,
  stale,
  size = "sm",
}: {
  state: SessionRow["state"]
  rawState?: string
  stale?: boolean
  size?: "sm" | "lg"
}) {
  const isAwake = state === "awake"
  const isBusy = rawState === "running" || rawState === "busy" || rawState === "processing"
  const ledColor =
    state === "unknown"
      ? "var(--color-muted-foreground)"
      : stale
        ? "var(--color-warning)"
        : isAwake
          ? "var(--color-success)"
          : "var(--color-primary)"
  const coreColor = isBusy ? "var(--color-success)" : isAwake ? "var(--color-primary)" : "var(--color-muted-foreground)"

  const dims = size === "lg" ? "size-13" : "size-11"
  return (
    <div
      className={`relative flex ${dims} shrink-0 items-center justify-center rounded-sm border border-border/80 bg-muted/40 p-1.5 shadow-2xs`}
      aria-hidden="true"
    >
      <svg viewBox="0 0 48 48" fill="none" className="size-full" aria-hidden="true">
        {/* Blueprint corner ticks */}
        <path
          d="M3 7V3H7M41 3H45V7M45 41V45H41M7 45H3V41"
          stroke="currentColor"
          strokeOpacity="0.28"
          strokeWidth="1.2"
          strokeLinecap="round"
        />
        {/* Top & Bottom bus pins */}
        <line x1="15" y1="6" x2="15" y2="11" stroke="currentColor" strokeOpacity="0.5" strokeWidth="1.3" />
        <line x1="24" y1="5" x2="24" y2="11" stroke={coreColor} strokeOpacity="0.85" strokeWidth="1.4" />
        <line x1="33" y1="6" x2="33" y2="11" stroke="currentColor" strokeOpacity="0.5" strokeWidth="1.3" />
        <line x1="15" y1="37" x2="15" y2="42" stroke="currentColor" strokeOpacity="0.5" strokeWidth="1.3" />
        <line x1="24" y1="37" x2="24" y2="43" stroke={coreColor} strokeOpacity="0.85" strokeWidth="1.4" />
        <line x1="33" y1="37" x2="33" y2="42" stroke="currentColor" strokeOpacity="0.5" strokeWidth="1.3" />
        {/* Left & Right bus pins */}
        <line x1="6" y1="16" x2="11" y2="16" stroke="currentColor" strokeOpacity="0.5" strokeWidth="1.3" />
        <line x1="5" y1="24" x2="11" y2="24" stroke={coreColor} strokeOpacity="0.85" strokeWidth="1.4" />
        <line x1="6" y1="32" x2="11" y2="32" stroke="currentColor" strokeOpacity="0.5" strokeWidth="1.3" />
        <line x1="37" y1="16" x2="42" y2="16" stroke="currentColor" strokeOpacity="0.5" strokeWidth="1.3" />
        <line x1="37" y1="24" x2="43" y2="24" stroke={coreColor} strokeOpacity="0.85" strokeWidth="1.4" />
        <line x1="37" y1="32" x2="42" y2="32" stroke="currentColor" strokeOpacity="0.5" strokeWidth="1.3" />

        {/* Outer Agent Chassis */}
        <rect
          x="11"
          y="11"
          width="26"
          height="26"
          rx="3"
          stroke="currentColor"
          strokeOpacity="0.78"
          strokeWidth="1.4"
        />
        {/* Inner Neural Core Frame */}
        <rect x="16" y="16" width="16" height="16" rx="1.5" stroke={coreColor} strokeOpacity="0.75" strokeWidth="1.2" />
        {/* Eyes / Optical Sensors */}
        <circle cx="20.5" cy="22.5" r="1.7" fill={ledColor} />
        <circle cx="27.5" cy="22.5" r="1.7" fill={ledColor} />
        {/* Voice / Signal Waveform Line */}
        <path
          d="M19 27.5H21.5L22.8 25.8L24.8 29L26.2 27.5H29"
          stroke={coreColor}
          strokeWidth="1.3"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
      </svg>
    </div>
  )
}

/** Compact stepped activity bars for an agent session (Tachyon prior art). */
export function AgentActivityBars({
  session,
  width = 60,
  height = 16,
}: {
  session: SessionRow
  width?: number
  height?: number
}) {
  const isAwake = session.state === "awake"
  const isWorking =
    session.raw_state === "running" || session.raw_state === "busy" || session.raw_state === "processing"
  const series = useMemo(() => {
    const seed = hashAgentSeed(session.id || session.agent_name)
    const lastMs = session.last_activity_at
      ? Date.now() - new Date(session.last_activity_at).getTime()
      : Number.POSITIVE_INFINITY
    const recencyBoost = lastMs < 15 * 60_000 ? 35 : lastMs < 60 * 60_000 ? 20 : 8
    const base = isWorking ? 52 : isAwake ? 34 : 14
    return Array.from({ length: 12 }, (_, slot) => {
      const nib = ((seed >> (slot % 16)) & 0xf) / 15
      const tail = slot >= 9 ? recencyBoost : 0
      const v = Math.min(100, Math.max(10, Math.round(base + nib * 30 + tail)))
      return { slot, v }
    })
  }, [session.id, session.agent_name, session.last_activity_at, isAwake, isWorking])

  const color = session.stale
    ? "var(--color-warning)"
    : isWorking
      ? "var(--color-success)"
      : isAwake
        ? "var(--color-primary)"
        : "var(--color-muted-foreground)"
  const gap = 1.2
  const bw = (width - gap * (series.length - 1)) / series.length

  return (
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
        return (
          <rect
            key={`ag-bar-${session.id}-${pt.slot}`}
            x={pt.slot * (bw + gap)}
            y={height - h}
            width={bw}
            height={h}
            fill={color}
            opacity={0.35 + 0.65 * (pt.slot / series.length)}
          />
        )
      })}
    </svg>
  )
}

/**
 * Shared Agent Session Card / Row used both in the Operations `Agents` view and inside a
 * `Fleet` host modal's `Agents on Machine` tab.
 */
export function SessionRowView({
  session: s,
  onSelect,
  onOpenHost,
}: {
  session: SessionRow
  onSelect?: (s: SessionRow) => void
  onOpenHost?: (host: string) => void
}) {
  const { baseName, instanceTag, roleLabel } = useMemo(() => parseAgentIdentity(s.agent_name), [s.agent_name])
  const isAwake = s.state === "awake"
  const isSleeping = s.state === "sleeping"
  const canonicalWorkstream = s.workstream_id
    ? s.workstream_id.startsWith("workstream:")
      ? s.workstream_id
      : `workstream:${s.workstream_id}`
    : ""

  return (
    <article
      aria-label={s.agent_name}
      data-slot="agent-session-card"
      className="group relative flex min-w-0 flex-col justify-between gap-3 overflow-hidden rounded-md border border-border/85 bg-card/95 p-3.5 shadow-2xs transition-all hover:border-primary/50 hover:shadow-md"
    >
      {/* Top Semantic Accent Bar */}
      <span
        className={`absolute inset-x-0 top-0 h-0.5 ${
          s.stale
            ? "bg-warning/80"
            : isAwake
              ? "bg-success/80"
              : isSleeping
                ? "bg-primary/70"
                : "bg-muted-foreground/50"
        }`}
      />

      {/* Card Header: Line-Art Agent Core + Name, Instance Tag, Role & Live State */}
      <div className="flex min-w-0 items-start gap-3">
        <AgentLineArt state={s.state} rawState={s.raw_state} stale={s.stale} />

        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <div className="flex min-w-0 items-center justify-between gap-2">
            <div className="flex min-w-0 items-center gap-1.5">
              {s.agent_ref ? (
                <span onClick={(e) => e.stopPropagation()} className="truncate font-mono text-sm font-bold">
                  <CiteRef refId={{ kind: "tether.agent", canonical: s.agent_ref }} label={baseName} />
                </span>
              ) : (
                <span className="truncate font-mono text-sm font-bold text-foreground">{baseName}</span>
              )}
              {instanceTag && (
                <span className="shrink-0 rounded-xs bg-muted/60 px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground">
                  {instanceTag}
                </span>
              )}
              <Badge
                variant="outline"
                className="h-4.5 shrink-0 border-border/80 bg-muted/40 px-1.5 py-0 font-mono text-[10px] uppercase tracking-wider text-muted-foreground"
              >
                {roleLabel}
              </Badge>
            </div>

            <div className="flex shrink-0 items-center gap-1">
              {s.stale && (
                <Badge
                  variant="outline"
                  className="h-4.5 border-warning/45 bg-warning/10 px-1.5 py-0 font-mono text-[10px] text-warning"
                >
                  Stale observation
                </Badge>
              )}
              <Badge
                variant={isAwake ? "default" : "secondary"}
                className={`h-4.5 gap-1 px-1.5 py-0 font-mono text-[10px] ${
                  isAwake
                    ? "border-success/40 bg-success/15 text-success hover:bg-success/20"
                    : isSleeping
                      ? "border-primary/35 bg-primary/10 text-primary"
                      : ""
                }`}
              >
                <span
                  className={`size-1.5 rounded-full ${
                    isAwake ? "animate-pulse bg-success" : isSleeping ? "bg-primary" : "bg-muted-foreground"
                  }`}
                />
                {isAwake ? "Awake" : isSleeping ? "Sleeping" : "State unknown"}
              </Badge>
              <ObjectActionsMenu
                object={{
                  kind: "tether.agent",
                  id: s.agent_ref || s.agent_id || s.id,
                  title: s.agent_name,
                  subtitle: `${s.state} (${s.raw_state}) on ${s.host || "unknown host"}`,
                }}
              />
            </div>
          </div>

          {/* Subheader: Session ID + Activity Pulse Bar + Raw State */}
          <div className="flex flex-wrap items-center justify-between gap-2 pt-0.5">
            <span className="truncate font-mono text-[11px] text-muted-foreground">
              {s.kind} · {s.id}
            </span>
            <div className="flex items-center gap-2">
              <span className="rounded-xs border border-border/70 bg-muted/40 px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground">
                raw: <strong className="text-foreground/90">{s.raw_state || "unknown"}</strong>
              </span>
              <AgentActivityBars session={s} />
            </div>
          </div>
        </div>
      </div>

      {/* 3 Compact Telemetry & Context Boxes: Host Placement | Last Activity | Linked Workstream & Task */}
      <div className="grid grid-cols-1 gap-2 sm:grid-cols-3">
        {/* Box 1: Host Placement */}
        <div className="flex min-w-0 flex-col justify-between gap-1 rounded-sm border border-border/75 bg-muted/25 p-2">
          <div className="flex items-center justify-between gap-1">
            <span className="font-mono text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
              Host Node
            </span>
            <Icon name="server" size={11} className="text-primary/80" />
          </div>
          {s.host ? (
            onOpenHost ? (
              <Button
                variant="link"
                size="xs"
                className="h-auto justify-start p-0 font-mono text-xs font-semibold text-primary"
                onClick={(e) => {
                  e.stopPropagation()
                  onOpenHost(s.host)
                }}
              >
                {s.host}
              </Button>
            ) : (
              <span className="truncate font-mono text-xs font-semibold text-foreground">{s.host}</span>
            )
          ) : (
            <span className="truncate font-mono text-xs text-muted-foreground">Host unknown</span>
          )}
          <span className="truncate font-mono text-[10px] text-muted-foreground">Tether runtime seat</span>
        </div>

        {/* Box 2: Last Activity */}
        <div className="flex min-w-0 flex-col justify-between gap-1 rounded-sm border border-border/75 bg-muted/25 p-2">
          <div className="flex items-center justify-between gap-1">
            <span className="font-mono text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
              Last Activity
            </span>
            <Icon name="clock" size={11} className="text-primary/80" />
          </div>
          <span className="truncate font-mono text-xs font-semibold text-foreground" title={time(s.last_activity_at)}>
            {s.last_activity_at ? `${age(s.last_activity_at)} ago` : "Last activity unknown"}
          </span>
          <span className="truncate font-mono text-[10px] text-muted-foreground">
            {s.last_activity || (s.last_activity_at ? time(s.last_activity_at) : "No recent turn timestamp")}
          </span>
        </div>

        {/* Box 3: Assignment (Workstream & Task) */}
        <div
          className="flex min-w-0 flex-col justify-between gap-1 rounded-sm border border-border/75 bg-muted/25 p-2"
          onClick={(e) => e.stopPropagation()}
        >
          <div className="flex items-center justify-between gap-1">
            <span className="font-mono text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
              Assignment
            </span>
            <Icon name="radar" size={11} className="text-primary/80" />
          </div>
          <div className="flex flex-wrap items-center gap-1.5">
            {s.task_id && <CiteRef refId={{ kind: "torque.task", canonical: s.task_id }} />}
            {canonicalWorkstream && (
              <CiteRef
                refId={{
                  kind: "registry.record",
                  canonical: canonicalWorkstream,
                }}
                label="Workstream"
              />
            )}
            {!s.task_id && !canonicalWorkstream && (
              <span className="font-mono text-xs text-muted-foreground">Unassigned</span>
            )}
          </div>
          <span className="truncate font-mono text-[10px] text-muted-foreground">
            {s.workstream_id ? s.workstream_id : "Standing agent session"}
          </span>
        </div>
      </div>

      {s.warning && (
        <div className="flex items-center gap-1.5 rounded-xs border border-warning/35 bg-warning/10 px-2.5 py-1 text-xs text-warning">
          <Icon name="alert-triangle" size={12} className="shrink-0" />
          <span>{s.warning}</span>
        </div>
      )}

      {onSelect && (
        <div className="flex items-center justify-between gap-2 border-t border-border/60 pt-2">
          <span className="truncate font-mono text-[10.5px] text-muted-foreground" title={s.agent_ref || s.agent_id}>
            {s.agent_ref || s.agent_id}
          </span>
          <Button
            variant="ghost"
            size="xs"
            className="h-6 shrink-0 gap-1 px-2 font-mono text-[11px] text-primary hover:bg-primary/12"
            onClick={() => onSelect(s)}
          >
            Inspect session
            <Icon name="chevron-right" size={12} />
          </Button>
        </div>
      )}
    </article>
  )
}
