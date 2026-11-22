/**
 * Full-page Follow-ups Triage & Template View (`modules/ops/web/followups.tsx`).
 *
 * Displays `caster://ops/followups` as a structured full-page card workbench:
 * - Defines a canonical Follow-up Data Template (`ops/followup/v1`) and smart parser that
 *   normalizes both structured template entries and live tracker follow-up strings (`STATE`,
 *   `INCIDENT`, `NEEDS YOU`, `OWNER STANCE`, `TO GET TO`, task follow-ups) into rich cards.
 * - Renders a filterable card grid showing category icon, kind/priority badge, timestamp,
 *   concise title, summary preview, related Cite references, and matched workstream.
 * - Clicking any card opens a rich Follow-up Detail Modal with shaded callout sections,
 *   extracted references, inline notes/marks, one-click "Attach to Workstream Notes",
 *   and the structured template representation.
 */
import { CiteRef, CiteText, Icon, ObjectActionsMenu, useMarks } from "@hollis-labs/surface-page/sdk"
import { useMemo, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { type Card, message, type OpsClient, stageLabel } from "./api"
import { OpsChip, StageBadge, UnitNotesInline } from "./parts"

export type FollowupKind = "decision" | "incident" | "backlog" | "policy" | "state" | "task"
export type FollowupPriority = "needs_you" | "high" | "normal" | "deferred"

export interface FollowupRecord {
  id: string
  index: number
  kind: FollowupKind
  priority: FollowupPriority
  timestamp?: string
  actor?: string
  title: string
  summary: string
  actionNeeded?: string
  bullets: { label?: string; text: string }[]
  refs: string[]
  taskIds: string[]
  matchedWorkstream?: Card
  matchReason?: string
  raw: string
}

export const FOLLOWUP_TEMPLATE_SPEC = `# Follow-up Entry Template (caster://ops/followups)
- [KIND] <Title> | at: <Timestamp> | priority: <needs_you|high|normal|deferred> | owner: <Owner> | workstream: <ops:id> | refs: <CW-... / caster://...> :: <Executive summary> :: Action: <Decision or next step> :: Evidence: <Links, PRs, or root-cause notes>`

export const FOLLOWUP_TEMPLATE_EXAMPLE = `- [DECISION] Catalog approval gate replacement | at: 2026-11-21 05:30Z | priority: needs_you | owner: Chrispian + Security Officer | workstream: ops:approvals-off | refs: CW-20261120-0095, CW-20261120-0098 :: Replace passphrase catalog prompt with signed-commit or delegate approval with audit trail. :: Action: Choose between file-permission gate vs delegate approver principal in morning review. :: Evidence: Designer and Security Officer plans stalled at gate; direct git commits bypassed passphrase.`

interface KindMeta {
  label: string
  icon: string
  circleClass: string
  badgeClass: string
  calloutTitleClass: string
}

export const KIND_META: Record<FollowupKind, KindMeta> = {
  decision: {
    label: "Needs You / Decision",
    icon: "user-check",
    circleClass: "bg-chart-3/15 text-chart-3 border border-chart-3/30",
    badgeClass: "bg-chart-3/15 text-chart-3 border-chart-3/30",
    calloutTitleClass: "text-chart-3",
  },
  incident: {
    label: "Incident",
    icon: "triangle-exclamation-point",
    circleClass: "bg-destructive/15 text-destructive border border-destructive/30",
    badgeClass: "bg-destructive/15 text-destructive border-destructive/30",
    calloutTitleClass: "text-destructive",
  },
  policy: {
    label: "Owner Stance / Policy",
    icon: "shield-exclamation-point",
    circleClass: "bg-primary/15 text-primary border border-primary/30",
    badgeClass: "bg-primary/15 text-primary border-primary/30",
    calloutTitleClass: "text-primary",
  },
  backlog: {
    label: "To Get To / Backlog",
    icon: "bookmark",
    circleClass: "bg-warning/15 text-warning border border-warning/30",
    badgeClass: "bg-warning/15 text-warning border-warning/30",
    calloutTitleClass: "text-warning",
  },
  state: {
    label: "State Snapshot",
    icon: "activity",
    circleClass: "bg-chart-2/15 text-chart-2 border border-chart-2/30",
    badgeClass: "bg-chart-2/15 text-chart-2 border-chart-2/30",
    calloutTitleClass: "text-chart-2",
  },
  task: {
    label: "Task Follow-up",
    icon: "list-todo",
    circleClass: "bg-success/15 text-success border border-success/30",
    badgeClass: "bg-success/15 text-success border-success/30",
    calloutTitleClass: "text-success",
  },
}

const REF_REGEX =
  /\b(?:CW-\d{8}-\d{4}(?:\.\.\d{4})?|EP-\d{8}-\d{4}|PL-\d{8}-\d{4}|PRJ-\d{8}-\d{4}|caster:\/\/[^\s,;)]+|ops:[a-z0-9-]+|cp_[a-f0-9]{8,}|01[0-9A-HJKMNP-TV-Z]{24})\b/g
const TASK_ID_REGEX = /\bCW-\d{8}-\d{4}\b/g

/**
 * Parse a raw follow-up string (either in canonical `[KIND] Title | key: val :: ...` format
 * or natural tracker prose like `STATE 04:35Z: ...`, `INCIDENT ...`, `TO GET TO ...`) into
 * a normalized `FollowupRecord`.
 */
export function parseFollowup(rawText: string, index: number, workstreams: Card[]): FollowupRecord {
  const raw = rawText.trim()
  const refs = [...new Set(raw.match(REF_REGEX) ?? [])]
  const taskIds = [...new Set(raw.match(TASK_ID_REGEX) ?? [])]

  let kind: FollowupKind = "task"
  let priority: FollowupPriority = "normal"
  let timestamp: string | undefined
  let actor: string | undefined
  let rest = raw
  let explicitWorkstreamId: string | undefined
  let explicitAction: string | undefined

  // 1. Check for canonical template format: `[KIND] Title | k: v :: Summary :: Action: ...`
  const templateMatch = rest.match(/^\[([A-Z_]+)\]\s*(.+)$/)
  if (templateMatch) {
    const rawKind = templateMatch[1].toLowerCase()
    if (rawKind === "incident") kind = "incident"
    else if (rawKind === "decision" || rawKind === "needs_you") kind = "decision"
    else if (rawKind === "policy" || rawKind === "stance") kind = "policy"
    else if (rawKind === "backlog" || rawKind === "todo") kind = "backlog"
    else if (rawKind === "state") kind = "state"
    else kind = "task"

    const sections = templateMatch[2].split(/\s*::\s*/)
    const headerParts = sections[0].split(/\s*\|\s*/)
    const headTitle = headerParts[0]?.trim() || "Follow-up"
    for (const part of headerParts.slice(1)) {
      const [k, ...vParts] = part.split(":")
      const val = vParts.join(":").trim()
      const key = k.trim().toLowerCase()
      if (key === "at") timestamp = val
      else if (key === "priority" && (val === "needs_you" || val === "high" || val === "normal" || val === "deferred"))
        priority = val
      else if (key === "owner") actor = val
      else if (key === "workstream") explicitWorkstreamId = val.replace(/^ops:/, "")
    }
    const bodyText = sections.slice(1).join(". ") || headTitle
    for (const sec of sections.slice(1)) {
      if (/^action:/i.test(sec)) explicitAction = sec.replace(/^action:\s*/i, "").trim()
    }
    rest = `${headTitle}: ${bodyText}`
  } else {
    // 2. Parse tracker prefix conventions
    const stateMatch = rest.match(/^STATE\s+([0-9:A-Za-z.-]+)(?:\s*\(([^)]+)\))?:\s*(.+)$/)
    const incidentMatch =
      rest.match(/^INCIDENT\s+([0-9:A-Za-z.-]+)?:\s*(.+)$/) || rest.match(/^INCIDENT\s+([0-9:A-Za-z.-]+)\s+(.+)$/)
    const stanceMatch = rest.match(/^OWNER STANCE(?:\s*\(([^)]+)\))?:\s*(.+)$/)
    const toGetToMatch = rest.match(/^TO GET TO(?:\s*\(([^)]+)\))?:\s*(.+)$/)

    if (stateMatch) {
      kind = "state"
      timestamp = stateMatch[1]
      actor = stateMatch[2]
      rest = stateMatch[3]
    } else if (incidentMatch) {
      kind = "incident"
      priority = "high"
      timestamp = incidentMatch[1]
      rest = incidentMatch[2]
    } else if (stanceMatch) {
      kind = "policy"
      priority = "high"
      timestamp = stanceMatch[1]
      rest = stanceMatch[2]
    } else if (toGetToMatch) {
      kind = "backlog"
      priority = "deferred"
      const meta = toGetToMatch[1] ?? ""
      const dateMatch = meta.match(/(\d+\s+[A-Z][a-z]{2})/)
      if (dateMatch) timestamp = dateMatch[1]
      rest = toGetToMatch[2]
    }
  }

  // Upgrade kind/priority when urgent decision markers appear in the body
  if (/\bNEEDS YOU\b|owner decision|owner to schedule|verbal(?:ly)? approved/i.test(raw)) {
    if (kind !== "incident") kind = "decision"
    priority = "needs_you"
  } else if (/\bregression\b|\boutage\b|\b409\b|\blost\b/i.test(raw) && kind === "task") {
    priority = "high"
  }

  if (!timestamp) {
    const inlineTime = raw.match(/\b(\d{2}:\d{2}Z|\d{1,2}\s+(?:Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec))\b/)
    if (inlineTime) timestamp = inlineTime[1]
  }

  // Extract a clean title and summary
  const sentences = rest
    .split(/(?<=[.!?])\s+(?=[A-Z0-9"'(])|;\s+/)
    .map((s) => s.trim())
    .filter(Boolean)

  const firstSentence = sentences[0] ?? rest
  let title = firstSentence
  const colonIdx = firstSentence.indexOf(":")
  if (colonIdx > 2 && colonIdx < 58 && !firstSentence.slice(0, colonIdx).includes("//")) {
    const prefix = firstSentence.slice(0, colonIdx).trim()
    const after = firstSentence.slice(colonIdx + 1).trim()
    if (!actor && /^[A-Z][a-zA-Z0-9_-]+(?:\s+[A-Z][a-zA-Z0-9_-]+)?$/.test(prefix) && !/^CW-/.test(prefix)) {
      actor = prefix
    }
    title = after ? `${prefix}: ${after.slice(0, 68)}${after.length > 68 ? "…" : ""}` : prefix
  } else if (title.length > 82) {
    title = `${title.slice(0, 80).trim()}…`
  }

  const summary = sentences.slice(0, 2).join(" ")

  // Extract structured bullets & action Needed
  const bullets: { label?: string; text: string }[] = []
  for (const s of sentences) {
    const labeled = s.match(
      /^(NEEDS YOU|Fixes needed|Next|Phase \d+[^:]*|Running|LIVE|MERGED[^:]*|IN FLIGHT|Details|Evidence|Questions|Workaround|Owner's point|Cost of the current flow[^:]*|Phase \d+ root cause[^:]*):\s*(.+)$/i,
    )
    if (labeled) {
      const label = labeled[1].trim()
      const text = labeled[2].trim()
      bullets.push({ label, text })
      if (/^(NEEDS YOU|Fixes needed|Next|Questions)$/i.test(label) && !explicitAction) {
        explicitAction = `${label}: ${text}`
      }
    } else {
      bullets.push({ text: s })
    }
  }

  // Match to a workstream on the board
  let matchedWorkstream: Card | undefined
  let matchReason: string | undefined

  if (explicitWorkstreamId) {
    matchedWorkstream = workstreams.find((w) => w.id === explicitWorkstreamId)
    if (matchedWorkstream) matchReason = `Template workstream ops:${explicitWorkstreamId}`
  }
  if (!matchedWorkstream && taskIds.length > 0) {
    for (const tid of taskIds) {
      const found = workstreams.find(
        (w) => w.links?.some((l) => l.id === tid) || w.task_list?.some((t) => t.id === tid),
      )
      if (found) {
        matchedWorkstream = found
        matchReason = `Linked via ${tid}`
        break
      }
    }
  }
  if (!matchedWorkstream) {
    const low = raw.toLowerCase()
    for (const w of workstreams) {
      const words = w.id.split("-").filter((x) => x.length >= 4)
      const hits = words.filter((wd) => low.includes(wd))
      if (hits.length >= 2) {
        matchedWorkstream = w
        matchReason = `Matches keywords (${hits.slice(0, 2).join(", ")})`
        break
      }
    }
  }

  return {
    id: `fu-${String(index + 1).padStart(2, "0")}`,
    index,
    kind,
    priority,
    timestamp,
    actor,
    title,
    summary,
    actionNeeded: explicitAction,
    bullets,
    refs,
    taskIds,
    matchedWorkstream,
    matchReason,
    raw,
  }
}

/** Format any `FollowupRecord` into the canonical template string. */
export function formatFollowupAsTemplate(rec: FollowupRecord): string {
  const parts = [`[${rec.kind.toUpperCase()}] ${rec.title}`]
  if (rec.timestamp) parts.push(`at: ${rec.timestamp}`)
  parts.push(`priority: ${rec.priority}`)
  if (rec.actor) parts.push(`owner: ${rec.actor}`)
  if (rec.matchedWorkstream) parts.push(`workstream: ops:${rec.matchedWorkstream.id}`)
  if (rec.refs.length > 0) parts.push(`refs: ${rec.refs.join(", ")}`)

  const clauses = [parts.join(" | "), rec.summary]
  if (rec.actionNeeded) clauses.push(`Action: ${rec.actionNeeded}`)
  return `- ${clauses.join(" :: ")}`
}

function FollowupCard({
  rec,
  onSelect,
  onOpenWorkstream,
}: {
  rec: FollowupRecord
  onSelect: () => void
  onOpenWorkstream: (id: string, newWindow?: boolean) => void
}) {
  const meta = KIND_META[rec.kind]
  const markRef = `ops:followup:${rec.id}`
  const marks = useMarks(markRef)

  return (
    <div
      data-followup-card={rec.id}
      onClick={onSelect}
      className="group flex cursor-pointer flex-col justify-between gap-2.5 rounded-md border border-border/85 bg-card p-3.5 text-left shadow-2xs transition-all hover:border-primary/50 hover:bg-accent/35"
    >
      {/* Top Row: Circular Action Icon, Kind Badge, Priority & Timestamp */}
      <div className="flex flex-col gap-2">
        <div className="flex items-center gap-2">
          <div className={`flex size-7 shrink-0 items-center justify-center rounded-full ${meta.circleClass}`}>
            <Icon name={meta.icon} size={14} />
          </div>
          <Badge variant="outline" className={`h-4.5 px-1.5 font-mono text-[10px] ${meta.badgeClass}`}>
            {meta.label}
          </Badge>
          {rec.priority === "needs_you" && (
            <Badge variant="default" className="h-4.5 px-1.5 font-mono text-[10px]">
              Needs You
            </Badge>
          )}
          {rec.timestamp && (
            <span className="font-mono text-[11px] text-muted-foreground" title="Recorded timestamp">
              {rec.timestamp}
            </span>
          )}
          <span className="ml-auto flex items-center gap-0.5" onClick={(e) => e.stopPropagation()}>
            <ObjectActionsMenu
              obj={{
                kind: "ops.followup",
                id: rec.id,
                title: rec.title,
                subtitle: meta.label,
                body: formatFollowupAsTemplate(rec),
              }}
            />
            <Icon
              name="chevron-right"
              size={14}
              className="text-muted-foreground transition-transform group-hover:translate-x-0.5 group-hover:text-foreground"
            />
          </span>
        </div>

        {/* Title */}
        <h3 className="line-clamp-2 font-semibold text-foreground text-xs leading-snug">{rec.title}</h3>

        {/* Shaded Action Preview or Summary Preview */}
        {rec.actionNeeded ? (
          <div className="rounded-sm border border-border/60 bg-muted/50 px-2.5 py-1.5 text-[11px]">
            <span className={`font-semibold ${meta.calloutTitleClass}`}>Action: </span>
            <span className="line-clamp-2 text-foreground/90">{rec.actionNeeded}</span>
          </div>
        ) : (
          <p className="line-clamp-2 text-[11px] text-muted-foreground leading-relaxed">{rec.summary}</p>
        )}
      </div>

      {/* Bottom Aspect Strip */}
      <div className="flex flex-wrap items-center gap-1.5 border-t border-border/50 pt-2 text-[11px] text-muted-foreground">
        <span className="font-mono text-[10px] text-muted-foreground/80">#{rec.index + 1}</span>
        {rec.actor && (
          <>
            <span className="text-border">|</span>
            <span className="truncate font-medium text-foreground/80">{rec.actor}</span>
          </>
        )}
        {rec.matchedWorkstream && (
          <>
            <span className="text-border">|</span>
            <Button
              variant="link"
              size="xs"
              className="h-auto p-0 font-mono text-[11px] text-primary"
              onClick={(e) => {
                e.stopPropagation()
                onOpenWorkstream(rec.matchedWorkstream?.id ?? "", e.metaKey || e.ctrlKey || e.shiftKey)
              }}
              title={rec.matchReason}
            >
              ops:{rec.matchedWorkstream.id}
            </Button>
          </>
        )}
        {rec.taskIds.slice(0, 2).map((tid) => (
          <Badge key={tid} variant="secondary" className="h-4 px-1 font-mono text-[9.5px]">
            {tid}
          </Badge>
        ))}
        {rec.refs.length > 2 && <span className="font-mono text-[10px]">+{rec.refs.length - 2} refs</span>}
        {marks.length > 0 && (
          <span className="ml-auto inline-flex items-center gap-0.5 text-[10px] text-primary">
            <Icon name="message-square-text" size={10} />
            {marks.length}
          </span>
        )}
      </div>
    </div>
  )
}

export function FollowupsView({
  followups,
  workstreams,
  ops,
  onOpenWorkstream,
  onRefresh,
}: {
  followups: string[]
  workstreams: Card[]
  ops: OpsClient
  onOpenWorkstream: (id: string, newWindow?: boolean) => void
  onRefresh: () => void
}) {
  const [filterKind, setFilterKind] = useState<string>("all")
  const [search, setSearch] = useState("")
  const [selected, setSelected] = useState<FollowupRecord | null>(null)
  const [showTemplate, setShowTemplate] = useState(false)
  const [copiedTemplate, setCopiedTemplate] = useState(false)
  const [targetWs, setTargetWs] = useState<string>("")
  const [attaching, setAttaching] = useState(false)
  const [attachNotice, setAttachNotice] = useState("")

  const records = useMemo(() => followups.map((f, i) => parseFollowup(f, i, workstreams)), [followups, workstreams])

  const counts = useMemo(() => {
    const c: Record<string, number> = { all: records.length, matched: 0 }
    for (const r of records) {
      c[r.kind] = (c[r.kind] ?? 0) + 1
      if (r.matchedWorkstream) c.matched++
    }
    return c
  }, [records])

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase()
    return records.filter((r) => {
      if (filterKind === "matched" && !r.matchedWorkstream) return false
      if (filterKind !== "all" && filterKind !== "matched" && r.kind !== filterKind) return false
      if (!q) return true
      return (
        r.title.toLowerCase().includes(q) ||
        r.raw.toLowerCase().includes(q) ||
        (r.actor?.toLowerCase().includes(q) ?? false) ||
        (r.matchedWorkstream?.title.toLowerCase().includes(q) ?? false)
      )
    })
  }, [records, filterKind, search])

  const openModal = (rec: FollowupRecord) => {
    setSelected(rec)
    setTargetWs(rec.matchedWorkstream?.id ?? workstreams[0]?.id ?? "")
    setAttachNotice("")
  }

  const attachToWorkstream = async (rec: FollowupRecord) => {
    if (!targetWs) return
    setAttaching(true)
    setAttachNotice("")
    try {
      await ops.update(targetWs, {
        append_note: `Follow-up (${rec.id}): ${formatFollowupAsTemplate(rec).replace(/^- /, "")}`,
      })
      setAttachNotice(`Appended to ops:${targetWs} notes.`)
      onRefresh()
    } catch (e) {
      setAttachNotice(`Error: ${message(e)}`)
    } finally {
      setAttaching(false)
    }
  }

  return (
    <section aria-label="Follow-ups" className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto p-4">
      <div className="mx-auto flex w-full max-w-6xl flex-col gap-4">
        {/* Top Itinerary-Style Route & Summary Header */}
        <header className="flex flex-col gap-3 rounded-lg border border-border/80 bg-card/50 p-4">
          <div className="flex flex-wrap items-center justify-between gap-3 border-b border-dashed border-border/80 pb-3">
            <div className="flex flex-col gap-1">
              <div className="flex flex-wrap items-center gap-2.5">
                <h2 className="font-semibold text-foreground text-lg tracking-tight">Operations</h2>
                <Icon name="arrow-left-right" size={16} className="text-muted-foreground" />
                <span className="font-semibold text-lg text-primary tracking-tight">Follow-ups & Triage</span>
                <Badge variant="secondary" className="font-mono text-xs">
                  {records.length} items
                </Badge>
                <code className="font-mono text-muted-foreground text-xs">caster://ops/followups</code>
              </div>
              <div className="flex flex-wrap items-center gap-2 text-muted-foreground text-xs">
                <span>
                  Needs You / Decisions: <strong className="font-mono text-chart-3">{counts.decision ?? 0}</strong>
                </span>
                <span className="text-border">|</span>
                <span>
                  Incidents: <strong className="font-mono text-destructive">{counts.incident ?? 0}</strong>
                </span>
                <span className="text-border">|</span>
                <span>
                  To Get To / Backlog: <strong className="font-mono text-warning">{counts.backlog ?? 0}</strong>
                </span>
                <span className="text-border">|</span>
                <span>
                  Policy / Stance: <strong className="font-mono text-primary">{counts.policy ?? 0}</strong>
                </span>
                <span className="text-border">|</span>
                <span>
                  State Snapshots: <strong className="font-mono text-chart-2">{counts.state ?? 0}</strong>
                </span>
                <span className="text-border">|</span>
                <span>
                  Workstream-Linked: <strong className="font-mono text-foreground">{counts.matched ?? 0}</strong>
                </span>
              </div>
            </div>

            <div className="flex items-center gap-2">
              <Button
                variant={showTemplate ? "secondary" : "outline"}
                size="sm"
                onClick={() => setShowTemplate((v) => !v)}
              >
                <Icon name="file-code" size={13} />
                Data Template & Schema
              </Button>
            </div>
          </div>

          {/* Expandable Data Template Specification Card */}
          {showTemplate && (
            <div className="flex flex-col gap-2.5 rounded-md border border-border/70 bg-muted/45 p-3.5 text-xs">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <div className="flex items-center gap-2">
                  <Icon name="braces" size={14} className="text-primary" />
                  <span className="font-semibold text-foreground">
                    Canonical Follow-up Data Template (ops/followup/v1)
                  </span>
                </div>
                <Button
                  size="xs"
                  variant="outline"
                  onClick={() => {
                    void navigator.clipboard?.writeText(`${FOLLOWUP_TEMPLATE_SPEC}\n\n${FOLLOWUP_TEMPLATE_EXAMPLE}`)
                    setCopiedTemplate(true)
                    window.setTimeout(() => setCopiedTemplate(false), 1500)
                  }}
                >
                  <Icon name={copiedTemplate ? "check" : "copy"} size={11} />
                  {copiedTemplate ? "Copied template" : "Copy template"}
                </Button>
              </div>
              <p className="text-muted-foreground leading-relaxed">
                Follow-ups in <code className="font-mono">caster://ops/followups</code> are formatted with a leading{" "}
                <code className="font-mono">[KIND]</code> tag (<code className="font-mono">DECISION</code>,{" "}
                <code className="font-mono">INCIDENT</code>, <code className="font-mono">POLICY</code>,{" "}
                <code className="font-mono">BACKLOG</code>, <code className="font-mono">STATE</code>,{" "}
                <code className="font-mono">TASK</code>), pipe-separated metadata aspects (
                <code className="font-mono">at</code>, <code className="font-mono">priority</code>,{" "}
                <code className="font-mono">owner</code>, <code className="font-mono">workstream</code>,{" "}
                <code className="font-mono">refs</code>), and <code className="font-mono">::</code> clauses for{" "}
                <code className="font-mono">Summary</code>, <code className="font-mono">Action</code>, and{" "}
                <code className="font-mono">Evidence</code>. Existing tracker lines are automatically parsed into this
                template.
              </p>
              <pre className="overflow-x-auto rounded-sm border border-border/60 bg-background/80 p-2.5 font-mono text-[11px] text-foreground/90 whitespace-pre-wrap">
                {FOLLOWUP_TEMPLATE_SPEC}
                {"\n\n"}
                {FOLLOWUP_TEMPLATE_EXAMPLE}
              </pre>
            </div>
          )}

          {/* Filter Pills & Search Input */}
          <div className="flex flex-wrap items-center gap-1.5">
            {(
              [
                { id: "all", label: "All", count: counts.all },
                { id: "decision", label: "Needs You / Decisions", count: counts.decision ?? 0 },
                { id: "incident", label: "Incidents", count: counts.incident ?? 0 },
                { id: "policy", label: "Owner Stance", count: counts.policy ?? 0 },
                { id: "backlog", label: "To Get To", count: counts.backlog ?? 0 },
                { id: "task", label: "Task Follow-ups", count: counts.task ?? 0 },
                { id: "state", label: "State Snapshots", count: counts.state ?? 0 },
                { id: "matched", label: "Workstream-Linked", count: counts.matched ?? 0 },
              ] as const
            ).map((tab) => (
              <Button
                key={tab.id}
                size="xs"
                variant={filterKind === tab.id ? "secondary" : "ghost"}
                className="h-6 gap-1.5 px-2.5 text-xs"
                onClick={() => setFilterKind(tab.id)}
              >
                <span>{tab.label}</span>
                <Badge variant="outline" className="h-4 px-1 font-mono text-[10px]">
                  {tab.count}
                </Badge>
              </Button>
            ))}

            <div className="ml-auto flex items-center gap-1.5">
              <Input
                className="h-7 w-56 text-xs"
                placeholder="Filter follow-ups, CW-id, owner…"
                aria-label="Filter follow-ups"
                value={search}
                onChange={(e) => setSearch(e.target.value)}
              />
              {search && (
                <Button size="icon-xs" variant="ghost" onClick={() => setSearch("")} aria-label="Clear search">
                  <Icon name="x" size={11} />
                </Button>
              )}
            </div>
          </div>
        </header>

        {/* Cards Grid */}
        {filtered.length === 0 ? (
          <Empty className="py-12">
            <EmptyHeader>
              <EmptyTitle>No follow-ups match these filters</EmptyTitle>
              <EmptyDescription>
                Clear the search or switch category tabs to view all items in caster://ops/followups.
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        ) : (
          <div className="grid grid-cols-[repeat(auto-fill,minmax(21rem,1fr))] gap-3">
            {filtered.map((rec) => (
              <FollowupCard
                key={rec.id}
                rec={rec}
                onSelect={() => openModal(rec)}
                onOpenWorkstream={onOpenWorkstream}
              />
            ))}
          </div>
        )}
      </div>

      {/* Follow-up Rich Detail Modal */}
      <Dialog open={!!selected} onOpenChange={(open) => !open && setSelected(null)}>
        {selected && (
          <DialogContent className="flex max-h-[85vh] flex-col gap-4 sm:max-w-2xl">
            <DialogHeader className="border-b border-dashed border-border/80 pb-3">
              <div className="flex flex-wrap items-center gap-2">
                <div
                  className={`flex size-7 shrink-0 items-center justify-center rounded-full ${KIND_META[selected.kind].circleClass}`}
                >
                  <Icon name={KIND_META[selected.kind].icon} size={14} />
                </div>
                <Badge
                  variant="outline"
                  className={`h-5 px-2 font-mono text-[10.5px] ${KIND_META[selected.kind].badgeClass}`}
                >
                  {KIND_META[selected.kind].label}
                </Badge>
                <Badge
                  variant={selected.priority === "needs_you" ? "default" : "secondary"}
                  className="font-mono text-[10.5px]"
                >
                  priority: {selected.priority}
                </Badge>
                {selected.timestamp && (
                  <span className="font-mono text-muted-foreground text-xs">at {selected.timestamp}</span>
                )}
                <span className="ml-auto flex items-center gap-1">
                  <code className="font-mono text-[11px] text-muted-foreground">#{selected.index + 1}</code>
                  <ObjectActionsMenu
                    obj={{
                      kind: "ops.followup",
                      id: selected.id,
                      title: selected.title,
                      subtitle: KIND_META[selected.kind].label,
                      body: formatFollowupAsTemplate(selected),
                    }}
                  />
                </span>
              </div>
              <DialogTitle className="mt-1.5 text-base leading-snug">{selected.title}</DialogTitle>
              <DialogDescription className="flex flex-wrap items-center gap-2 text-xs">
                <span>Source: caster://ops/followups</span>
                {selected.actor && (
                  <>
                    <span>|</span>
                    <span>
                      Owner / Actor: <strong className="text-foreground">{selected.actor}</strong>
                    </span>
                  </>
                )}
                {selected.matchedWorkstream && (
                  <>
                    <span>|</span>
                    <span className="inline-flex items-center gap-1">
                      Workstream: <OpsChip id={selected.matchedWorkstream.id} />
                      <StageBadge stage={selected.matchedWorkstream.stage} />
                    </span>
                  </>
                )}
              </DialogDescription>
            </DialogHeader>

            <div className="flex min-h-0 flex-1 flex-col gap-3.5 overflow-y-auto pr-1 text-xs">
              {/* Shaded Action / Decision Required Callout */}
              {selected.actionNeeded && (
                <div className="rounded-md border border-primary/40 bg-primary/10 px-3.5 py-2.5">
                  <div className="flex items-center gap-1.5 font-semibold text-primary">
                    <Icon name="sparkles" size={13} />
                    <span>Action / Decision Required</span>
                  </div>
                  <p className="mt-1 text-foreground leading-relaxed">
                    <CiteText text={selected.actionNeeded} />
                  </p>
                </div>
              )}

              {/* Shaded Structured Breakdown & Evidence Div */}
              <div className="rounded-md border border-border/60 bg-muted/45 px-3.5 py-3">
                <span className={`font-semibold ${KIND_META[selected.kind].calloutTitleClass}`}>
                  Structured Breakdown & Details
                </span>
                <ul className="mt-1.5 flex list-disc flex-col gap-1.5 pl-4 text-foreground/90 leading-relaxed">
                  {selected.bullets.map((b) => (
                    <li key={`${b.label ?? ""}:${b.text}`}>
                      {b.label && <strong className="font-semibold text-foreground">{b.label}: </strong>}
                      <CiteText text={b.text} />
                    </li>
                  ))}
                </ul>
              </div>

              {/* Extracted References & Citations */}
              {selected.refs.length > 0 && (
                <div className="flex flex-col gap-1.5 rounded-md border border-border/70 bg-card/60 p-3">
                  <span className="font-semibold text-[11px] text-muted-foreground uppercase tracking-wider">
                    Referenced Objects ({selected.refs.length})
                  </span>
                  <div className="flex flex-wrap items-center gap-1.5">
                    {selected.refs.map((r) =>
                      /^CW-\d{8}-\d{4}$/.test(r) ? (
                        <CiteRef key={r} refId={{ kind: "torque.task", canonical: r }} />
                      ) : /^EP-\d{8}-\d{4}$/.test(r) ? (
                        <CiteRef key={r} refId={{ kind: "torque.epic", canonical: r }} />
                      ) : r.startsWith("ops:") ? (
                        <OpsChip key={r} id={r.slice(4)} />
                      ) : (
                        <Badge key={r} variant="outline" className="font-mono text-[11px]">
                          <CiteText text={r} />
                        </Badge>
                      ),
                    )}
                  </div>
                </div>
              )}

              {/* Attach / Route to Workstream */}
              <div className="flex flex-col gap-2 rounded-md border border-border/70 bg-card/60 p-3">
                <div className="flex items-center justify-between">
                  <span className="font-semibold text-[11px] text-muted-foreground uppercase tracking-wider">
                    Route / Attach to Workstream Notes
                  </span>
                  {selected.matchReason && (
                    <span className="font-mono text-[10.5px] text-primary">{selected.matchReason}</span>
                  )}
                </div>
                <div className="flex flex-wrap items-center gap-2">
                  <Select value={targetWs} onValueChange={(v) => setTargetWs(String(v))}>
                    <SelectTrigger size="sm" className="h-7 min-w-64 flex-1 text-xs" aria-label="Target workstream">
                      <SelectValue>
                        {(v: string) => {
                          const w = workstreams.find((x) => x.id === v)
                          return w ? `${w.title} (ops:${w.id} · ${stageLabel(w.stage)})` : "Choose a workstream…"
                        }}
                      </SelectValue>
                    </SelectTrigger>
                    <SelectContent>
                      {workstreams.map((w) => (
                        <SelectItem key={w.id} value={w.id}>
                          {w.title} (ops:{w.id} · {stageLabel(w.stage)})
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <Button
                    size="sm"
                    variant="secondary"
                    className="h-7 text-xs"
                    disabled={!targetWs || attaching}
                    onClick={() => void attachToWorkstream(selected)}
                  >
                    <Icon name="notebook-pen" size={12} />
                    {attaching ? "Appending…" : "Append to Workstream Notes"}
                  </Button>
                  {targetWs && (
                    <Button
                      size="sm"
                      variant="outline"
                      className="h-7 text-xs"
                      onClick={() => {
                        const wsId = targetWs
                        setSelected(null)
                        onOpenWorkstream(wsId, false)
                      }}
                    >
                      Open Workstream
                      <Icon name="arrow-right" size={12} />
                    </Button>
                  )}
                </div>
                {attachNotice && <p className="text-[11px] text-primary">{attachNotice}</p>}
              </div>

              {/* Inline Comments / Notes via Surface Marks */}
              <div className="rounded-md border border-border/70 bg-card/60 p-3">
                <span className="mb-1.5 block font-semibold text-[11px] text-muted-foreground uppercase tracking-wider">
                  Triage Notes & Comments
                </span>
                <UnitNotesInline targetRef={`ops:followup:${selected.id}`} />
              </div>

              {/* Structured Template Representation */}
              <div className="flex flex-col gap-1.5 rounded-md border border-border/70 bg-muted/30 p-3">
                <div className="flex items-center justify-between">
                  <span className="font-semibold text-[11px] text-muted-foreground uppercase tracking-wider">
                    Formatted Template Record (ops/followup/v1)
                  </span>
                  <Button
                    size="xs"
                    variant="ghost"
                    className="h-5 px-1.5 text-[10px]"
                    onClick={() => void navigator.clipboard?.writeText(formatFollowupAsTemplate(selected))}
                  >
                    <Icon name="copy" size={10} />
                    Copy formatted line
                  </Button>
                </div>
                <pre className="overflow-x-auto rounded-sm border border-border/60 bg-background/70 p-2 font-mono text-[11px] text-foreground/85 whitespace-pre-wrap">
                  {formatFollowupAsTemplate(selected)}
                </pre>
              </div>
            </div>

            <DialogFooter className="border-t border-border/60 pt-2.5">
              <Button
                variant="outline"
                size="sm"
                onClick={() => {
                  window.dispatchEvent(
                    new CustomEvent("surface:add-to-context", {
                      detail: {
                        ref: `ops:followup:${selected.id}`,
                        title: selected.title,
                      },
                    }),
                  )
                }}
              >
                <Icon name="bot" size={13} />
                Add to Agent Context
              </Button>
              <Button variant="secondary" size="sm" onClick={() => setSelected(null)}>
                Done
              </Button>
            </DialogFooter>
          </DialogContent>
        )}
      </Dialog>
    </section>
  )
}
