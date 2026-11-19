"use client"

import { AlertTriangleIcon, CheckCircle2Icon, ClockIcon, Link2OffIcon, PauseCircleIcon, XCircleIcon } from "lucide-react"
import * as React from "react"

import { cn } from "cn"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardAction, CardContent, CardFooter, CardHeader, CardTitle } from "@/components/ui/card"
import { Markdown } from "@/components/ui/markdown"

/**
 * The frame every card is drawn in, and the pieces the answerable ones share: the status
 * pill, the actions row, the receipt that replaces the actions once a card is closed.
 * Surface's own. The cards here only draw: what a card is, where it shows and what
 * happens to its answer are the app's.
 */

/** Where a card stands. "open" takes input; "pending" is being handled; the rest are closed. */
type CardState = "open" | "pending" | "submitted" | "canceled" | "expired" | "abandoned" | "superseded" | "failed" | "unrecognized"

/** What the person did with a card: answered it or declined. */
type CardOutcome =
  | {
      status: "submitted"
      data?: Record<string, unknown>
      answers?: { questionId: string; value: unknown; note?: string }[]
      decisions?: { itemId: string; action: string; note?: string }[]
    }
  | { status: "canceled" }

/** What every answerable card takes. */
interface AnswerableCardProps {
  title?: string
  state: CardState
  /** A submit is running. */
  busy?: boolean
  /** The card changed under the person: their input is kept and they are told. */
  stale?: boolean
  onReload?: () => void
  /** What was answered, shown once the card is closed. */
  receipt?: React.ReactNode
  /** When the card lapses, already worded ("in 4 min", "at 14:05"). */
  expires?: string
  onRespond: (outcome: CardOutcome) => void | Promise<void>
  /** What the person had entered before (a draft the host kept), to start from. */
  draft?: CardDraft
  /** Called with what the person has entered so far, as they enter it, for the host to
   * keep (a reload, a server restart). Not called for what the card started with. */
  onDraft?: (draft: CardDraft) => void
  className?: string
}

/** What a card's controls hold while it is open: its own names, its own values. */
type CardDraft = Record<string, unknown>

const STATES: Record<Exclude<CardState, "open">, { label: string; text: string; icon: typeof ClockIcon; tone: "default" | "secondary" | "destructive" | "outline" }> = {
  pending: { label: "Working", text: "Working…", icon: ClockIcon, tone: "secondary" },
  submitted: { label: "Answered", text: "Answered", icon: CheckCircle2Icon, tone: "secondary" },
  canceled: { label: "Dismissed", text: "Dismissed", icon: XCircleIcon, tone: "outline" },
  expired: { label: "Expired", text: "Expired: no longer taking an answer", icon: ClockIcon, tone: "outline" },
  abandoned: { label: "Stopped waiting", text: "Its sender stopped waiting for an answer", icon: PauseCircleIcon, tone: "outline" },
  superseded: { label: "Replaced", text: "Replaced by a newer card", icon: Link2OffIcon, tone: "outline" },
  failed: { label: "Failed", text: "Failed", icon: AlertTriangleIcon, tone: "destructive" },
  unrecognized: { label: "Unknown state", text: "In a state this app does not know", icon: AlertTriangleIcon, tone: "outline" },
}

/** The pill that says where a closed card stands. Nothing while it is open. */
function CardStatus({ state }: { state: CardState }) {
  if (state === "open") return null
  const s = STATES[state]
  return (
    <Badge data-slot="card-status" variant={s.tone}>
      <s.icon />
      {s.label}
    </Badge>
  )
}

interface CardFrameProps extends Omit<React.ComponentProps<"div">, "title"> {
  title?: React.ReactNode
  icon?: React.ReactNode
  /** Goes at the right of the header: the status pill, a risk badge. */
  meta?: React.ReactNode
  /** Marks a card that needs attention with a coloured edge. */
  tone?: "default" | "warning" | "danger" | "success"
  footer?: React.ReactNode
}

function CardFrame({ title, icon, meta, tone = "default", footer, className, children, ...props }: CardFrameProps) {
  return (
    <Card
      size="sm"
      data-slot="card-frame"
      data-tone={tone}
      className={cn(
        "w-full data-[tone=danger]:ring-destructive/40 data-[tone=success]:ring-success/40 data-[tone=warning]:ring-warning/50",
        className,
      )}
      {...props}
    >
      {(title || meta) && (
        <CardHeader>
          <CardTitle className="flex min-w-0 items-center gap-2 text-sm [&_svg]:size-4 [&_svg]:shrink-0">
            {icon}
            <span className="min-w-0 truncate">{title}</span>
          </CardTitle>
          {meta && <CardAction className="flex items-center gap-1.5">{meta}</CardAction>}
        </CardHeader>
      )}
      {children && <CardContent className="flex flex-col gap-3">{children}</CardContent>}
      {footer && <CardFooter className="flex-col items-stretch gap-2 border-t py-(--card-spacing)">{footer}</CardFooter>}
    </Card>
  )
}

/** A card's prose, as Markdown. */
function CardBody({ children, className }: { children?: string; className?: string }) {
  if (!children) return null
  return <Markdown className={cn("text-muted-foreground", className)}>{children}</Markdown>
}

/** The row of buttons at the foot of an open card. */
function CardActions({ className, ...props }: React.ComponentProps<"div">) {
  return <div data-slot="card-actions" className={cn("flex flex-wrap items-center justify-end gap-2", className)} {...props} />
}

/** What stands where the actions were once a card is closed: what happened, and the answer. */
function CardReceipt({ state, children }: { state: CardState; children?: React.ReactNode }) {
  if (state === "open") return null
  const s = STATES[state]
  return (
    <div data-slot="card-receipt" role="status" className="flex min-w-0 items-start gap-2 text-xs text-muted-foreground">
      <s.icon className="mt-0.5 size-3.5 shrink-0" />
      <span className="min-w-0">
        {s.text}
        {children ? <span className="text-foreground"> · {children}</span> : null}
      </span>
    </div>
  )
}

/** The foot of an answerable card: its actions while open, a notice when it went stale,
 * the receipt once closed. */
function CardFoot({
  state,
  stale,
  onReload,
  receipt,
  expires,
  children,
}: Pick<AnswerableCardProps, "state" | "stale" | "onReload" | "receipt" | "expires"> & { children: React.ReactNode }) {
  if (state !== "open" && state !== "pending") return <CardReceipt state={state}>{receipt}</CardReceipt>
  return (
    <>
      {stale && (
        <div role="alert" className="flex items-center justify-between gap-2 text-xs text-warning">
          <span>This changed while you were answering. What you entered is kept.</span>
          {onReload && (
            <Button variant="outline" size="xs" onClick={onReload}>
              Reload
            </Button>
          )}
        </div>
      )}
      <div className="flex flex-wrap items-center justify-between gap-2">
        <span className="text-xs text-muted-foreground">{expires ? `Expires ${expires}` : ""}</span>
        <CardActions>{children}</CardActions>
      </div>
    </>
  )
}

/** Reports a card's draft to `onDraft` each time it changes, but not the value it started
 * with, so a card that is only looked at saves nothing. */
function useCardDraft(draft: CardDraft, onDraft: AnswerableCardProps["onDraft"]) {
  const first = React.useRef(true)
  const report = React.useRef(onDraft)
  report.current = onDraft
  const key = JSON.stringify(draft)
  // biome-ignore lint/correctness/useExhaustiveDependencies: the serialised draft is the change
  React.useEffect(() => {
    if (first.current) {
      first.current = false
      return
    }
    report.current?.(JSON.parse(key) as CardDraft)
  }, [key])
}

/** Runs a card's responder with a busy flag around it, and says whether its controls are off. */
function useCardRespond({ state, busy, stale, onRespond }: Pick<AnswerableCardProps, "state" | "busy" | "stale" | "onRespond">) {
  const [sending, setSending] = React.useState(false)
  const working = sending || busy === true || state === "pending"
  const disabled = state !== "open" || working || stale === true
  const respond = async (outcome: CardOutcome) => {
    if (disabled) return
    setSending(true)
    try {
      await onRespond(outcome)
    } finally {
      setSending(false)
    }
  }
  return { disabled, working, respond }
}

export { CardActions, CardBody, CardFoot, CardFrame, CardReceipt, CardStatus, useCardDraft, useCardRespond }
export type { AnswerableCardProps, CardDraft, CardFrameProps, CardOutcome, CardState }
