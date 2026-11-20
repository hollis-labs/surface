import * as React from "react"
import { Badge } from "@/components/ui/badge"
import { Skeleton } from "@/components/ui/skeleton"
import { cn } from "cn"

/**
 * What a reference stands for, as a hover card (CiteCard) or the head of its record
 * (CiteRecordHeader): the kind, a title, the reference as written, its status, a short preview
 * and a few fields, then when it changed and the actions. Every kind of reference is drawn
 * with these; the caller maps its record onto the props.
 */

type CiteTone = "neutral" | "info" | "success" | "warning" | "danger"

const toneClass: Record<CiteTone, string> = {
  neutral: "text-muted-foreground",
  info: "text-primary",
  success: "text-success",
  warning: "text-warning",
  danger: "text-destructive",
}

/** A status as a pill outlined in its tone. */
function CiteStatus({ label, tone = "neutral", className }: { label: string; tone?: CiteTone; className?: string }) {
  return (
    <Badge
      variant="outline"
      data-slot="cite-status"
      data-tone={tone}
      className={cn("h-4.5 border-current px-1.5 text-[0.7rem] font-normal tracking-normal normal-case", toneClass[tone] ?? toneClass.neutral, className)}
    >
      {label}
    </Badge>
  )
}

interface CiteCardField {
  label: string
  value: string
  mono?: boolean
}

/** The kind, as the card's eyebrow: its icon, its name, and the status at the far end. */
function CiteKind({ icon, kindLabel, status }: { icon?: React.ReactNode; kindLabel: string; status?: { label: string; tone?: CiteTone } }) {
  return (
    <div className="flex items-center gap-1.5 text-[0.7rem] font-medium tracking-wider text-muted-foreground uppercase [&>svg]:size-3.5 [&>svg]:shrink-0 [&>svg]:text-primary">
      {icon}
      <span className="min-w-0 flex-1 truncate">{kindLabel}</span>
      {status ? <CiteStatus label={status.label} tone={status.tone} /> : null}
    </div>
  )
}

function CiteCard({
  className,
  phase = "ready",
  icon,
  kindLabel,
  canonical,
  title,
  subtitle,
  description,
  note,
  status,
  fields = [],
  updated,
  error,
  actions,
  ...props
}: Omit<React.ComponentProps<"div">, "title"> & {
  /** loading draws a placeholder for what is still coming. */
  phase?: "loading" | "ready" | "not-found" | "error" | "unresolved"
  icon?: React.ReactNode
  kindLabel: string
  /** The reference as written: an id, a path, a URL. Shown under the title. */
  canonical: string
  title: string
  subtitle?: string
  description?: string
  /** Why there is nothing more to show ("It may have been deleted…"). */
  note?: string
  status?: { label: string; tone?: CiteTone }
  fields?: CiteCardField[]
  /** When it last changed, already put in words ("updated 3m ago"). */
  updated?: string
  error?: string
  /** Buttons at the foot, after the time (Copy id, Open). */
  actions?: React.ReactNode
}) {
  return (
    <div data-slot="cite-card" data-phase={phase} className={cn("flex min-w-0 flex-col gap-2 text-sm", className)} {...props}>
      <CiteKind icon={icon} kindLabel={kindLabel} status={status} />
      <div className="flex min-w-0 flex-col gap-0.5">
        <div className="line-clamp-2 font-semibold break-words text-foreground">{title}</div>
        {subtitle ? <div className="truncate text-xs text-muted-foreground">{subtitle}</div> : null}
        <code className="font-mono text-[0.7rem] break-all text-muted-foreground" title={canonical}>
          {canonical}
        </code>
      </div>
      {phase === "loading" && !description ? (
        <div className="flex flex-col gap-1">
          <Skeleton className="h-3 w-full" />
          <Skeleton className="h-3 w-2/3" />
        </div>
      ) : null}
      {note ? <p className="line-clamp-4 text-xs leading-relaxed text-muted-foreground">{note}</p> : null}
      {error ? <p className="text-xs text-destructive">{error}</p> : null}
      {description ? <p className="line-clamp-4 text-xs leading-relaxed whitespace-pre-line text-foreground/80">{description}</p> : null}
      {fields.length > 0 ? (
        <dl className="grid grid-cols-[5.5rem_minmax(0,1fr)] gap-x-2 gap-y-0.5 border-t border-border pt-1.5 text-xs">
          {fields.map((f) => (
            <React.Fragment key={f.label}>
              <dt className="truncate text-muted-foreground">{f.label}</dt>
              <dd className={cn("truncate text-foreground", f.mono && "font-mono text-[0.7rem]")} title={f.value}>
                {f.value}
              </dd>
            </React.Fragment>
          ))}
        </dl>
      ) : null}
      {updated || actions ? (
        <div className="flex flex-wrap items-center gap-1 border-t border-border pt-2">
          <span className="min-w-0 flex-1 truncate text-[0.7rem] text-muted-foreground">{updated}</span>
          {actions}
        </div>
      ) : null}
    </div>
  )
}

/**
 * The head of a record read whole: the kind, a large title, the reference, then its status and
 * fields on one wrapping line, and the actions. `renderValue` draws a field's value (a reference
 * in it as a chip, say); by default the text.
 */
function CiteRecordHeader({
  className,
  icon,
  kindLabel,
  canonical,
  title,
  subtitle,
  status,
  fields = [],
  updated,
  error,
  actions,
  renderValue,
  ...props
}: Omit<React.ComponentProps<"div">, "title"> & {
  icon?: React.ReactNode
  kindLabel: string
  canonical: string
  title: string
  subtitle?: string
  status?: { label: string; tone?: CiteTone }
  fields?: CiteCardField[]
  updated?: string
  error?: string
  actions?: React.ReactNode
  renderValue?: (field: CiteCardField) => React.ReactNode
}) {
  return (
    <div data-slot="cite-record-header" className={cn("flex min-w-0 flex-col gap-2", className)} {...props}>
      <CiteKind icon={icon} kindLabel={kindLabel} />
      <div className="flex min-w-0 flex-col gap-1">
        <h1 className="text-2xl leading-tight font-semibold tracking-tight break-words text-foreground">{title}</h1>
        {subtitle ? <div className="text-sm text-muted-foreground">{subtitle}</div> : null}
        <code className="font-mono text-xs break-all text-muted-foreground">{canonical}</code>
      </div>
      {status || fields.length > 0 || updated ? (
        <div className="flex flex-wrap items-center gap-x-3.5 gap-y-1.5 text-xs">
          {status ? <CiteStatus label={status.label} tone={status.tone} /> : null}
          {fields.map((f) => (
            <span key={f.label} className="inline-flex min-w-0 max-w-full items-baseline gap-1.5">
              <span className="text-muted-foreground">{f.label}</span>
              <span className={cn("min-w-0 truncate text-foreground", f.mono && "font-mono text-[0.7rem]")} title={f.value}>
                {renderValue ? renderValue(f) : f.value}
              </span>
            </span>
          ))}
          {updated ? <span className="text-muted-foreground">{updated}</span> : null}
        </div>
      ) : null}
      {error ? <p className="text-xs text-destructive">{error}</p> : null}
      {actions ? <div className="flex flex-wrap items-center gap-1">{actions}</div> : null}
    </div>
  )
}

export { CiteCard, CiteKind, CiteRecordHeader, CiteStatus, type CiteCardField, type CiteTone }
