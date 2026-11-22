/**
 * Module Entity Cards (`web/src/cards/EntityCard.tsx`).
 *
 * Allows the agent to render rich, domain-authentic cards & live data from Surface modules
 * (Ops Kanban Workstream cards, Ops Agent Session cards, Fleet Host cards, Registry Knowledge
 * Graph cards, Follow-up cards, and Tracker cards) directly inside the chatstream or Cards window
 * via `card_show` or `card_ask` (`kind: "entity"` or module-specific kinds like `ops.workstream`,
 * `ops.agent`, `ops.host`, `registry.record`, `tracker.item`).
 *
 * When invoked via `card_ask` (with `question` or `options`), the card also renders interactive
 * decision options + universal "Other: <tell me>" so the user can act on or answer about the
 * module entity right in place.
 */
import { classifyPriorResponse } from "@hollis-labs/surface-cards"
import {
  ActivityIcon,
  ArrowUpRightIcon,
  BotIcon,
  BoxesIcon,
  CheckCircle2Icon,
  CpuIcon,
  HardDriveIcon,
  LayersIcon,
  ListChecksIcon,
  SendIcon,
  ServerIcon,
} from "lucide-react"
import { useEffect, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { CardBody, CardFoot, CardFrame, CardStatus } from "@/components/ui/card-frame"
import { Input } from "@/components/ui/input"
import { CiteRef, CiteText } from "../cite/ui"
import { runCommand } from "../desk/store"
import type { CardRendererProps } from "./renderers"
import { isLive, isStale, lateOpen, loadDraft, saveDraft } from "./store"

export type ModuleEntityKind =
  | "ops.workstream"
  | "ops.agent"
  | "ops.host"
  | "ops.followup"
  | "registry.record"
  | "tracker.item"

export interface ModuleEntitySnapshot {
  id: string
  ref?: string
  entity_kind: ModuleEntityKind
  title: string
  subtitle?: string
  status?: string
  stage?: string
  summary?: string
  purpose?: string
  latest_activity?: string
  owner?: string
  host?: string
  model?: string
  task_id?: string
  progress?: { done: number; total: number }
  telemetry?: {
    cpu_pct?: number
    mem_pct?: number
    disk_pct?: number
    uptime?: string
    tokens_pct?: number
  }
  tags?: string[]
  relations?: { predicate: string; target: string }[]
  open_command?: { command: string; args?: Record<string, unknown> }
}

interface EntityOption {
  id: string
  label: string
  description?: string
}

function inferEntityKind(refOrKind?: string, cardKind?: string): ModuleEntityKind {
  const k = (refOrKind || cardKind || "").toLowerCase()
  if (k.startsWith("ops.agent") || k.startsWith("agent:") || k.includes("session")) return "ops.agent"
  if (k.startsWith("ops.host") || k.startsWith("host:") || k.startsWith("cerberus")) return "ops.host"
  if (k.startsWith("ops.followup") || k.startsWith("followup:")) return "ops.followup"
  if (k.startsWith("registry") || /^(project|agency|service|correlation|document|skill|tool|config):/.test(k))
    return "registry.record"
  if (k.startsWith("tracker")) return "tracker.item"
  return "ops.workstream"
}

function normalizeEntityFromPayload(
  raw: Record<string, unknown>,
  fallbackTitle: string,
  cardKind?: string,
): ModuleEntitySnapshot {
  const ref = typeof raw.ref === "string" ? raw.ref : typeof raw.id === "string" ? raw.id : "ops:surface-ade"
  const eKind = inferEntityKind(typeof raw.entity_kind === "string" ? raw.entity_kind : ref, cardKind)
  const data = (raw.data && typeof raw.data === "object" ? raw.data : raw) as Record<string, unknown>

  const progressObj =
    data.progress && typeof data.progress === "object" ? (data.progress as Record<string, unknown>) : undefined
  const telemetryObj =
    data.telemetry && typeof data.telemetry === "object" ? (data.telemetry as Record<string, unknown>) : undefined

  return {
    id: typeof data.id === "string" ? data.id : ref,
    ref,
    entity_kind: eKind,
    title: typeof data.title === "string" ? data.title : typeof data.name === "string" ? data.name : fallbackTitle,
    subtitle:
      typeof data.subtitle === "string" ? data.subtitle : typeof data.schema === "string" ? data.schema : undefined,
    status:
      typeof data.status === "string"
        ? data.status
        : typeof data.lifecycle === "string"
          ? data.lifecycle
          : typeof data.state === "string"
            ? data.state
            : "active",
    stage: typeof data.stage === "string" ? data.stage : undefined,
    summary: typeof data.summary === "string" ? data.summary : undefined,
    purpose: typeof data.purpose === "string" ? data.purpose : undefined,
    latest_activity: typeof data.latest_activity === "string" ? data.latest_activity : undefined,
    owner: typeof data.owner === "string" ? data.owner : undefined,
    host: typeof data.host === "string" ? data.host : undefined,
    model: typeof data.model === "string" ? data.model : undefined,
    task_id: typeof data.task_id === "string" ? data.task_id : undefined,
    progress: progressObj
      ? {
          done: typeof progressObj.done === "number" ? progressObj.done : 0,
          total: typeof progressObj.total === "number" ? progressObj.total : 1,
        }
      : undefined,
    telemetry: telemetryObj
      ? {
          cpu_pct: typeof telemetryObj.cpu_pct === "number" ? telemetryObj.cpu_pct : undefined,
          mem_pct: typeof telemetryObj.mem_pct === "number" ? telemetryObj.mem_pct : undefined,
          disk_pct: typeof telemetryObj.disk_pct === "number" ? telemetryObj.disk_pct : undefined,
          uptime: typeof telemetryObj.uptime === "string" ? telemetryObj.uptime : undefined,
          tokens_pct: typeof telemetryObj.tokens_pct === "number" ? telemetryObj.tokens_pct : undefined,
        }
      : undefined,
    tags: Array.isArray(data.tags) ? data.tags.map(String) : undefined,
    relations: Array.isArray(data.relations) ? (data.relations as { predicate: string; target: string }[]) : undefined,
  }
}

/** Visual renderer for a single Module Entity inside a card. */
export function ModuleEntityCardBody({
  entity,
  onOpenEntity,
}: {
  entity: ModuleEntitySnapshot
  onOpenEntity?: (e: ModuleEntitySnapshot) => void
}) {
  const kind = entity.entity_kind

  if (kind === "ops.agent") {
    const awake = entity.status === "awake" || entity.status === "working" || entity.status === "active"
    return (
      <div className="flex flex-col gap-2.5 rounded-md border border-border/80 bg-card/70 p-3">
        <div className="flex items-start justify-between gap-2">
          <div className="flex items-center gap-2.5">
            <div className="flex size-9 shrink-0 items-center justify-center rounded-sm border border-primary/30 bg-primary/10 text-primary">
              <BotIcon className="size-4" />
            </div>
            <div className="flex min-w-0 flex-col">
              <div className="flex items-center gap-1.5">
                <span
                  className={`size-2 rounded-full ${awake ? "bg-success shadow-[0_0_6px_var(--color-success)]" : "bg-muted-foreground"}`}
                />
                <span className="truncate font-semibold text-foreground text-xs">{entity.title}</span>
                <Badge variant="outline" className="h-4 px-1 font-mono text-[9px] uppercase">
                  {entity.status ?? "awake"}
                </Badge>
              </div>
              <span className="truncate font-mono text-[10.5px] text-muted-foreground">
                {entity.model ?? "claude-opus"} {entity.host ? `· host:${entity.host}` : ""}
              </span>
            </div>
          </div>
          {onOpenEntity && (
            <Button size="icon-xs" variant="ghost" onClick={() => onOpenEntity(entity)} title="Inspect Agent Session">
              <ArrowUpRightIcon className="size-3.5" />
            </Button>
          )}
        </div>

        {(entity.summary || entity.latest_activity) && (
          <p className="line-clamp-2 rounded-sm border-l-2 border-primary/50 bg-muted/35 px-2 py-1 text-xs text-foreground/90">
            <CiteText text={entity.latest_activity || entity.summary || ""} />
          </p>
        )}

        <div className="grid grid-cols-3 gap-1.5 font-mono text-[10.5px]">
          <div className="rounded-sm border border-border/60 bg-background/60 px-2 py-1">
            <span className="text-[9px] text-muted-foreground uppercase">Task Seat</span>
            <p className="truncate font-medium text-foreground">{entity.task_id ?? "Interactive"}</p>
          </div>
          <div className="rounded-sm border border-border/60 bg-background/60 px-2 py-1">
            <span className="text-[9px] text-muted-foreground uppercase">Placement</span>
            <p className="truncate font-medium text-foreground">{entity.host ?? "agent-os"}</p>
          </div>
          <div className="rounded-sm border border-border/60 bg-background/60 px-2 py-1">
            <span className="text-[9px] text-muted-foreground uppercase">Context Load</span>
            <p className="truncate font-medium text-primary">{entity.telemetry?.tokens_pct ?? 34}% window</p>
          </div>
        </div>
      </div>
    )
  }

  if (kind === "ops.host") {
    const cpu = entity.telemetry?.cpu_pct ?? 28
    const mem = entity.telemetry?.mem_pct ?? 54
    const disk = entity.telemetry?.disk_pct ?? 39
    return (
      <div className="flex flex-col gap-2.5 rounded-md border border-border/80 bg-card/70 p-3">
        <div className="flex items-start justify-between gap-2">
          <div className="flex items-center gap-2.5">
            <div className="flex size-9 shrink-0 items-center justify-center rounded-sm border border-primary/30 bg-primary/10 text-primary">
              <ServerIcon className="size-4" />
            </div>
            <div className="flex min-w-0 flex-col">
              <div className="flex items-center gap-1.5">
                <span className="size-2 rounded-full bg-success shadow-[0_0_6px_var(--color-success)]" />
                <span className="truncate font-mono font-semibold text-foreground text-xs">{entity.title}</span>
                <Badge variant="secondary" className="h-4 px-1 font-mono text-[9px]">
                  {entity.telemetry?.uptime ?? "UP 48d 14h"}
                </Badge>
              </div>
              <span className="truncate font-mono text-[10.5px] text-muted-foreground">
                {entity.subtitle || entity.summary || "Cerberus Fleet Compute Node"}
              </span>
            </div>
          </div>
          {onOpenEntity && (
            <Button size="icon-xs" variant="ghost" onClick={() => onOpenEntity(entity)} title="Open in Ops Fleet">
              <ArrowUpRightIcon className="size-3.5" />
            </Button>
          )}
        </div>

        <div className="grid grid-cols-3 gap-2 font-mono text-xs">
          <div className="flex flex-col gap-1 rounded-sm border border-border/70 bg-background/60 p-2">
            <div className="flex items-center justify-between text-[10px] text-muted-foreground">
              <span className="inline-flex items-center gap-1">
                <CpuIcon className="size-3 text-primary" /> CPU
              </span>
              <span className="font-semibold text-foreground">{cpu}%</span>
            </div>
            <div className="h-1.5 w-full overflow-hidden rounded-full bg-muted">
              <div className="h-full bg-primary" style={{ width: `${cpu}%` }} />
            </div>
          </div>
          <div className="flex flex-col gap-1 rounded-sm border border-border/70 bg-background/60 p-2">
            <div className="flex items-center justify-between text-[10px] text-muted-foreground">
              <span className="inline-flex items-center gap-1">
                <ActivityIcon className="size-3 text-success" /> MEM
              </span>
              <span className="font-semibold text-foreground">{mem}%</span>
            </div>
            <div className="h-1.5 w-full overflow-hidden rounded-full bg-muted">
              <div className="h-full bg-success" style={{ width: `${mem}%` }} />
            </div>
          </div>
          <div className="flex flex-col gap-1 rounded-sm border border-border/70 bg-background/60 p-2">
            <div className="flex items-center justify-between text-[10px] text-muted-foreground">
              <span className="inline-flex items-center gap-1">
                <HardDriveIcon className="size-3 text-warning" /> DISK
              </span>
              <span className="font-semibold text-foreground">{disk}%</span>
            </div>
            <div className="h-1.5 w-full overflow-hidden rounded-full bg-muted">
              <div className="h-full bg-warning" style={{ width: `${disk}%` }} />
            </div>
          </div>
        </div>
      </div>
    )
  }

  if (kind === "registry.record") {
    return (
      <div className="flex flex-col gap-2 rounded-md border border-border/80 bg-card/70 p-3">
        <div className="flex items-start justify-between gap-2">
          <div className="flex items-center gap-2.5">
            <div className="flex size-9 shrink-0 items-center justify-center rounded-sm border border-primary/30 bg-primary/10 text-primary">
              <BoxesIcon className="size-4" />
            </div>
            <div className="flex min-w-0 flex-col">
              <div className="flex flex-wrap items-center gap-1.5">
                <span className="truncate font-semibold text-foreground text-xs">{entity.title}</span>
                <Badge variant="outline" className="h-4 px-1 font-mono text-[9px]">
                  {entity.status ?? "active"}
                </Badge>
              </div>
              <span className="truncate font-mono text-[10.5px] text-muted-foreground">
                {entity.ref} {entity.owner ? `· Owner: ${entity.owner}` : ""}
              </span>
            </div>
          </div>
          {onOpenEntity && (
            <Button size="icon-xs" variant="ghost" onClick={() => onOpenEntity(entity)} title="Open in Registry Wiki">
              <ArrowUpRightIcon className="size-3.5" />
            </Button>
          )}
        </div>
        {entity.summary && <p className="text-xs text-muted-foreground leading-relaxed">{entity.summary}</p>}
        {entity.relations && entity.relations.length > 0 && (
          <div className="flex flex-wrap items-center gap-1 pt-0.5">
            {entity.relations.map((r) => (
              <Badge key={`${r.predicate}-${r.target}`} variant="outline" className="font-mono text-[10px]">
                {r.predicate}: {r.target}
              </Badge>
            ))}
          </div>
        )}
      </div>
    )
  }

  if (kind === "tracker.item") {
    const done = entity.status === "done" || entity.status === "completed"
    return (
      <div className="flex flex-col gap-2 rounded-md border border-border/80 bg-card/70 p-3">
        <div className="flex items-start justify-between gap-2">
          <div className="flex items-center gap-2">
            <div className="flex size-8 shrink-0 items-center justify-center rounded-sm border border-primary/30 bg-primary/10 text-primary">
              {done ? <CheckCircle2Icon className="size-4 text-success" /> : <ListChecksIcon className="size-4" />}
            </div>
            <div className="flex min-w-0 flex-col">
              <div className="flex items-center gap-1.5">
                <span className="truncate font-semibold text-foreground text-xs">{entity.title}</span>
                <Badge variant="outline" className="h-4 px-1 font-mono text-[9px] uppercase">
                  {entity.status ?? "in_progress"}
                </Badge>
              </div>
              {entity.subtitle && (
                <span className="truncate font-mono text-[10px] text-muted-foreground">{entity.subtitle}</span>
              )}
            </div>
          </div>
          {onOpenEntity && (
            <Button size="icon-xs" variant="ghost" onClick={() => onOpenEntity(entity)} title="Open in Trackers">
              <ArrowUpRightIcon className="size-3.5" />
            </Button>
          )}
        </div>
        {entity.summary && <p className="text-xs text-muted-foreground">{entity.summary}</p>}
        {entity.tags && entity.tags.length > 0 && (
          <div className="flex flex-wrap gap-1">
            {entity.tags.map((t) => (
              <Badge key={t} variant="secondary" className="h-4 px-1.5 font-mono text-[9.5px]">
                #{t}
              </Badge>
            ))}
          </div>
        )}
      </div>
    )
  }

  // Default: Ops Kanban Workstream Card
  const doneUnits = entity.progress?.done ?? 3
  const totalUnits = Math.max(1, entity.progress?.total ?? 5)
  const pct = Math.min(100, Math.round((doneUnits / totalUnits) * 100))

  return (
    <div className="flex flex-col gap-2.5 rounded-md border border-border/80 bg-card/75 p-3 shadow-2xs">
      <div className="flex items-start justify-between gap-2">
        <div className="flex min-w-0 flex-1 items-start gap-2">
          <div className="mt-0.5 flex size-7 shrink-0 items-center justify-center rounded-sm border border-primary/30 bg-primary/10 text-primary">
            <LayersIcon className="size-3.5" />
          </div>
          <div className="flex min-w-0 flex-1 flex-col gap-0.5">
            <div className="flex flex-wrap items-center gap-1.5">
              <span className="truncate font-semibold text-foreground text-xs">{entity.title}</span>
              <Badge
                variant="outline"
                className="h-4 border-primary/40 bg-primary/10 px-1.5 font-mono text-[9.5px] text-primary"
              >
                {entity.stage ?? entity.status ?? "in_flight"}
              </Badge>
            </div>
            {entity.ref && (
              <span className="font-mono text-[10px] text-muted-foreground">
                <CiteRef
                  refId={{ kind: "ops.workstream", canonical: entity.ref.replace(/^ops:/, "") }}
                  raw={entity.ref}
                  mono
                />
              </span>
            )}
          </div>
        </div>
        <div className="flex shrink-0 items-center gap-1">
          {onOpenEntity && (
            <Button
              size="icon-xs"
              variant="ghost"
              onClick={() => onOpenEntity(entity)}
              title="Open Workstream Detail"
              aria-label="Open Workstream Detail"
            >
              <ArrowUpRightIcon className="size-3.5" />
            </Button>
          )}
        </div>
      </div>

      {/* Progress Bar & Unit Count */}
      <div className="flex items-center gap-2">
        <div className="h-1.5 flex-1 overflow-hidden rounded-full bg-muted">
          <div className="h-full bg-primary transition-all" style={{ width: `${pct}%` }} />
        </div>
        <span className="shrink-0 font-mono text-[10px] text-muted-foreground">
          {doneUnits}/{totalUnits} units
        </span>
      </div>

      {(entity.purpose || entity.summary) && (
        <p className="line-clamp-2 text-xs text-muted-foreground leading-snug">
          <CiteText text={entity.purpose || entity.summary || ""} />
        </p>
      )}

      {entity.latest_activity && (
        <div className="flex items-center gap-1.5 rounded-sm border border-border/60 bg-muted/35 px-2 py-1 text-[11px] text-foreground/90">
          <ActivityIcon className="size-3 shrink-0 text-primary" />
          <span className="min-w-0 flex-1 truncate">
            <CiteText text={entity.latest_activity} />
          </span>
        </div>
      )}
    </div>
  )
}

/** Card renderer registered for `entity` and module card kinds (`ops.workstream`, `ops.agent`, `ops.host`, `registry.record`, `tracker.item`). */
export function EntityCardRenderer({ card, respond: send }: CardRendererProps) {
  const payload = (card.payload ?? {}) as Record<string, unknown>
  const late = lateOpen(card)
  const state = late ? "open" : classifyPriorResponse(card.status).kind
  const open = state === "open" || state === "pending"

  const rawEntities = Array.isArray(payload.entities) ? (payload.entities as Record<string, unknown>[]) : [payload]

  const [entities, setEntities] = useState<ModuleEntitySnapshot[]>(() =>
    rawEntities.map((item) => normalizeEntityFromPayload(item, card.title || "Module Object", card.kind)),
  )

  // Hydrate from /api/cite/resolve if only a ref was passed without summary/title
  useEffect(() => {
    const refsToFetch = entities
      .filter((e) => e.ref && !e.summary && !e.purpose)
      .map((e) => ({ kind: e.entity_kind, id: (e.ref ?? "").replace(/^[a-z_]+:/, "") }))
    if (refsToFetch.length === 0) return

    fetch("/api/cite/resolve", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ refs: refsToFetch }),
    })
      .then((r) => (r.ok ? r.json() : null))
      .then(
        (
          resolved: Record<
            string,
            { title?: string; subtitle?: string; summary?: string; status?: { label?: string } }
          > | null,
        ) => {
          if (!resolved) return
          setEntities((prev) =>
            prev.map((ent) => {
              const key = Object.keys(resolved).find((k) => k.endsWith(ent.id.replace(/^[a-z_]+:/, "")))
              const hit = key ? resolved[key] : undefined
              if (!hit) return ent
              return {
                ...ent,
                title: hit.title || ent.title,
                subtitle: hit.subtitle || ent.subtitle,
                summary: hit.summary || ent.summary,
                status: hit.status?.label || ent.status,
              }
            }),
          )
        },
      )
      .catch(() => {})
  }, [entities])

  const question = typeof payload.question === "string" ? payload.question : undefined
  const options = Array.isArray(payload.options) ? (payload.options as EntityOption[]) : []
  const isAnswerable = Boolean(question || options.length > 0 || card.blocking)
  const [otherText, setOtherText] = useState("")
  const [showOther, setShowOther] = useState(false)

  const handleOpenEntity = (e: ModuleEntitySnapshot) => {
    if (e.open_command) {
      void runCommand(e.open_command.command, e.open_command.args)
      return
    }
    if (e.entity_kind === "ops.workstream") {
      const wsId = (e.ref || e.id).replace(/^ops:/, "")
      void runCommand("ops.open", { id: wsId })
    } else if (e.entity_kind === "ops.agent") {
      void runCommand("ops.open", { layout: "agents" })
    } else if (e.entity_kind === "ops.host") {
      void runCommand("ops.open", { layout: "fleet", host: (e.ref || e.id).replace(/^host:/, "") })
    } else if (e.entity_kind === "registry.record") {
      void runCommand("registry.open", { id: (e.ref || e.id).replace(/^registry:/, "") })
    } else if (e.entity_kind === "tracker.item") {
      void runCommand("trackers.open", { id: (e.ref || e.id).replace(/^tracker:/, "") })
    }
  }

  return (
    <CardFrame
      data-slot="card-entity"
      title={card.title || entities[0]?.title || "Module Card"}
      meta={
        <>
          <Badge variant="outline" className="font-mono text-[10px]">
            {entities[0]?.entity_kind ?? "entity"}
          </Badge>
          <CardStatus state={state} />
        </>
      }
      footer={
        isAnswerable ? (
          <CardFoot
            state={state}
            stale={isStale(card.id)}
            allowOther={payload.allow_other !== false}
            onRespond={send}
            {...(isLive(card) ? { draft: loadDraft(card.id), onDraft: (d) => saveDraft(card.id, d) } : {})}
          >
            <div className="flex w-full flex-col gap-2">
              <div className="flex flex-wrap items-center gap-1.5">
                {(options.length > 0
                  ? options
                  : [
                      { id: "acknowledge", label: "Acknowledge" },
                      { id: "open", label: "Open in Module" },
                    ]
                ).map((opt) => (
                  <Button
                    key={opt.id}
                    size="sm"
                    variant={opt.id === "approve" || opt.id === "acknowledge" ? "default" : "outline"}
                    disabled={!open}
                    onClick={() => {
                      if (opt.id === "open" && entities[0]) handleOpenEntity(entities[0])
                      void send({
                        status: "submitted",
                        answers: [{ questionId: card.id, value: opt.id }],
                      })
                    }}
                  >
                    {opt.label}
                  </Button>
                ))}
                <Button size="sm" variant="ghost" disabled={!open} onClick={() => setShowOther((v) => !v)}>
                  Other…
                </Button>
              </div>
              {showOther && open && (
                <div className="flex items-center gap-1.5">
                  <Input
                    value={otherText}
                    onChange={(e) => setOtherText(e.target.value)}
                    placeholder="Other: tell the agent in your own words…"
                    className="h-7 text-xs"
                  />
                  <Button
                    size="xs"
                    disabled={!otherText.trim()}
                    onClick={() =>
                      void send({
                        status: "submitted",
                        answers: [{ questionId: card.id, value: "other", note: otherText.trim() }],
                      })
                    }
                  >
                    <SendIcon className="size-3" />
                    Send
                  </Button>
                </div>
              )}
            </div>
          </CardFoot>
        ) : undefined
      }
    >
      {question && <CardBody className="font-medium text-foreground">{question}</CardBody>}
      <div className="flex flex-col gap-2">
        {entities.map((ent) => (
          <ModuleEntityCardBody key={ent.id} entity={ent} onOpenEntity={handleOpenEntity} />
        ))}
      </div>
    </CardFrame>
  )
}
