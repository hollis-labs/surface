/** Pieces the Operations views share: a workstream's card, a link as a chip, a stage badge. */
import { CiteRef, CiteText, Icon } from "@hollis-labs/surface-page/sdk"
import type { ReactNode } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { age, type Card, type Link, RESOLVED_KINDS, stageLabel } from "./api"

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

/** A workstream on the board. Drag it to a lane; open it; step inside when it has children. */
export function WorkstreamCard({
  w,
  open,
  enter,
  draggable = true,
  lead,
  actions,
}: {
  w: Card
  open: (id: string) => void
  enter: (id: string) => void
  draggable?: boolean
  lead?: ReactNode
  actions?: ReactNode
}) {
  return (
    <div
      data-ops-card={w.id}
      className="flex min-w-0 cursor-pointer flex-col gap-1 rounded-md border bg-card px-2 py-1.5 text-sm hover:bg-accent"
      draggable={draggable}
      onDragStart={(e) => {
        e.dataTransfer.setData(DRAG_TYPE, w.id)
        e.dataTransfer.effectAllowed = "move"
      }}
      onClick={() => open(w.id)}
      title={`ops:${w.id}`}
    >
      <span className="flex min-w-0 items-center gap-1.5 font-medium">
        {lead}
        <span className="truncate">{w.title}</span>
        <span className="ml-auto" />
        {actions}
      </span>
      <span className="flex flex-wrap items-center gap-1.5 text-muted-foreground text-xs">
        {w.owner && <span className="truncate">{w.owner}</span>}
        {w.stage_since && (
          <span title={`In ${stageLabel(w.stage)} since ${w.stage_since}`}>{age(w.stage_since)} in stage</span>
        )}
        {w.held && (
          <Badge variant="destructive" title={w.held.reason}>
            held
          </Badge>
        )}
      </span>
      {w.held && <span className="line-clamp-2 text-destructive text-xs">{w.held.reason}</span>}
      {w.problem && <span className="text-destructive text-xs">{w.problem}</span>}
      {w.tasks && <span className="text-xs">{w.tasks}</span>}
      {w.last && (
        <span className="line-clamp-2 text-muted-foreground text-xs">
          {age(w.last.at)} ago · <CiteText text={w.last.summary} />
        </span>
      )}
      {w.suggest && (
        <span className="text-xs text-warning" title={w.suggest_why}>
          <Icon name="lightbulb" size={11} className="mr-1 inline" />
          Looks {stageLabel(w.suggest)}: {w.suggest_why}
        </span>
      )}
      {w.children.length > 0 && (
        <span className="flex">
          <Button
            size="xs"
            variant="ghost"
            onClick={(e) => {
              e.stopPropagation()
              enter(w.id)
            }}
            title="Show the workstreams inside"
          >
            <Icon name="layers" />
            {w.children.length} inside
            <Icon name="chevron-right" />
          </Button>
        </span>
      )}
    </div>
  )
}
