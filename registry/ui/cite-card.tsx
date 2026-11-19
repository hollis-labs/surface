import * as React from "react"
import { Badge } from "@/components/ui/badge"
import { Skeleton } from "@/components/ui/skeleton"
import { cn } from "cn"

/**
 * What a reference stands for, as a hover card or a record's header: the kind, a title, its
 * status, a short preview and a few fields, then the actions. Every kind of reference is drawn
 * with this one card; the caller maps its record onto the props.
 */

type CiteTone = "neutral" | "info" | "success" | "warning" | "danger"

const toneClass: Record<CiteTone, string> = {
  neutral: "bg-muted text-muted-foreground",
  info: "bg-primary/10 text-primary",
  success: "bg-success/10 text-success",
  warning: "bg-warning/10 text-warning",
  danger: "bg-destructive/10 text-destructive",
}

function CiteStatus({ label, tone = "neutral", className }: { label: string; tone?: CiteTone; className?: string }) {
  return (
    <Badge variant="secondary" data-slot="cite-status" className={cn(toneClass[tone] ?? toneClass.neutral, className)}>
      {label}
    </Badge>
  )
}

interface CiteCardField {
  label: string
  value: string
  mono?: boolean
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
  /** The reference as written: an id, a path, a URL. */
  canonical: string
  title: string
  subtitle?: string
  description?: string
  status?: { label: string; tone?: CiteTone }
  fields?: CiteCardField[]
  /** When it last changed, already put in words ("3m ago"). */
  updated?: string
  error?: string
  /** Buttons along the bottom (Open, Copy). */
  actions?: React.ReactNode
}) {
  return (
    <div data-slot="cite-card" data-phase={phase} className={cn("flex min-w-0 flex-col gap-2 text-sm", className)} {...props}>
      <div className="flex items-center gap-1.5 text-xs text-muted-foreground [&>svg]:size-3.5 [&>svg]:shrink-0">
        {icon}
        <span className="truncate">{kindLabel}</span>
        {status ? <CiteStatus className="ml-auto" label={status.label} tone={status.tone} /> : null}
      </div>
      <div className="flex min-w-0 flex-col gap-0.5">
        <div className="line-clamp-2 font-medium break-words text-foreground">{title}</div>
        {subtitle ? <div className="truncate text-xs text-muted-foreground">{subtitle}</div> : null}
      </div>
      {phase === "loading" && !description ? (
        <div className="flex flex-col gap-1">
          <Skeleton className="h-3 w-full" />
          <Skeleton className="h-3 w-2/3" />
        </div>
      ) : null}
      {description ? <p className="line-clamp-4 text-xs leading-relaxed whitespace-pre-line text-muted-foreground">{description}</p> : null}
      {error ? <p className="text-xs text-destructive">{error}</p> : null}
      {fields.length > 0 ? (
        <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-0.5 text-xs">
          {fields.map((f) => (
            <React.Fragment key={f.label}>
              <dt className="text-muted-foreground">{f.label}</dt>
              <dd className={cn("truncate text-foreground", f.mono && "font-mono")} title={f.value}>
                {f.value}
              </dd>
            </React.Fragment>
          ))}
        </dl>
      ) : null}
      <div className="flex items-center gap-2 border-t border-border pt-2">
        <code className="min-w-0 flex-1 truncate font-mono text-[0.7rem] text-muted-foreground" title={canonical}>
          {canonical}
        </code>
        {updated ? <span className="shrink-0 text-[0.7rem] text-muted-foreground">{updated}</span> : null}
      </div>
      {actions ? <div className="flex items-center justify-end gap-1">{actions}</div> : null}
    </div>
  )
}

export { CiteCard, CiteStatus, type CiteCardField, type CiteTone }
