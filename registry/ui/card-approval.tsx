"use client"

import {
  ArrowRightIcon,
  CheckCircle2Icon,
  ChevronDownIcon,
  ShieldAlertIcon,
  ShieldCheckIcon,
  ShieldIcon,
  XCircleIcon,
} from "lucide-react"
import * as React from "react"

import { cn } from "cn"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { type AnswerableCardProps, CardBody, CardFoot, CardFrame, CardStatus, useCardRespond } from "@/components/ui/card-frame"
import { Input } from "@/components/ui/input"
import { Spinner } from "@/components/ui/spinner"

/**
 * A request to approve something consequential: what will change, how risky it is, who
 * asked. Risk reads at a glance from the edge, the icon and the badge. A high-risk approval
 * takes a second, deliberate confirm (and a typed phrase when one is given). A risk the card
 * does not know is treated as high. With several changes, the person can approve some and
 * decline the rest. Surface's own.
 */
interface CardApprovalChange {
  label: string
  before?: string
  after?: string
}

/** What was decided, for the receipt of an answered card. */
type CardApprovalDecision = "approved" | "declined" | { approved: number; of: number }

interface CardApprovalProps extends AnswerableCardProps {
  summary?: string
  risk?: string
  changes?: CardApprovalChange[]
  requestedBy?: string
  /** A phrase a high-risk approval asks the person to type at the confirm step. */
  phrase?: string
  /** Each change is approved or declined on its own (only with two or more changes). */
  perChange?: boolean
  /** What was decided; drawn as the receipt once the card is answered. */
  decision?: CardApprovalDecision
  approveLabel?: string
  rejectLabel?: string
}

type Level = "low" | "medium" | "high"

const RISK: Record<Level, { label: string; icon: typeof ShieldIcon; iconClass: string; badge: string; edge: string; tone: "default" | "warning" | "danger" }> = {
  low: {
    label: "Low risk",
    icon: ShieldCheckIcon,
    iconClass: "text-muted-foreground",
    badge: "border-border text-muted-foreground",
    edge: "before:bg-border",
    tone: "default",
  },
  medium: {
    label: "Medium risk",
    icon: ShieldIcon,
    iconClass: "text-warning",
    badge: "border-warning/40 bg-warning/10 text-warning",
    edge: "before:bg-warning",
    tone: "warning",
  },
  high: {
    label: "High risk",
    icon: ShieldAlertIcon,
    iconClass: "text-destructive",
    badge: "border-destructive/40 bg-destructive/10 text-destructive",
    edge: "before:bg-destructive",
    tone: "danger",
  },
}

/** A value is short when it fits on a line beside its label. */
const isShort = (v?: string) => v === undefined || (v.length <= 64 && !v.includes("\n"))

function ChangeShort({ change }: { change: CardApprovalChange }) {
  return (
    <dd className="flex min-w-0 flex-wrap items-center gap-1.5 font-mono text-xs">
      {change.before !== undefined && <span className="text-muted-foreground line-through decoration-destructive/60">{change.before}</span>}
      {change.before !== undefined && change.after !== undefined && <ArrowRightIcon className="size-3 shrink-0 text-muted-foreground" />}
      {change.after !== undefined && <span className="text-foreground">{change.after}</span>}
    </dd>
  )
}

/** A long value: before and after stacked as blocks, clipped until opened. */
function ChangeLong({ change }: { change: CardApprovalChange }) {
  const [open, setOpen] = React.useState(false)
  const block = "max-w-full whitespace-pre-wrap break-words rounded-sm border-l-2 px-2 py-1 font-mono text-xs"
  return (
    <dd className="flex min-w-0 flex-col gap-1">
      <div className={cn("flex flex-col gap-1", !open && "max-h-28 overflow-hidden [mask-image:linear-gradient(to_bottom,black_70%,transparent)]")}>
        {change.before !== undefined && (
          <div className={cn(block, "border-destructive/50 bg-destructive/5 text-muted-foreground")}>
            <span className="sr-only">Before: </span>
            {change.before}
          </div>
        )}
        {change.after !== undefined && (
          <div className={cn(block, "border-success/60 bg-success/5 text-foreground")}>
            <span className="sr-only">After: </span>
            {change.after}
          </div>
        )}
      </div>
      <button
        type="button"
        onClick={() => setOpen(!open)}
        className="flex items-center gap-1 self-start text-xs text-muted-foreground hover:text-foreground"
      >
        <ChevronDownIcon className={cn("size-3 transition-transform", open && "rotate-180")} />
        {open ? "Show less" : "Show all"}
      </button>
    </dd>
  )
}

/** The receipt of an answered approval: approved, declined, or some of several. */
function DecisionReceipt({ decision }: { decision: CardApprovalDecision }) {
  if (typeof decision === "object") {
    return (
      <span className="inline-flex items-center gap-1 font-medium text-foreground">
        <CheckCircle2Icon className="size-3.5 text-success" />
        Approved {decision.approved} of {decision.of}
      </span>
    )
  }
  return decision === "approved" ? (
    <span className="inline-flex items-center gap-1 font-medium text-success">
      <CheckCircle2Icon className="size-3.5" />
      Approved
    </span>
  ) : (
    <span className="inline-flex items-center gap-1 font-medium text-destructive">
      <XCircleIcon className="size-3.5" />
      Declined
    </span>
  )
}

function CardApproval({
  title,
  summary,
  risk = "low",
  changes = [],
  requestedBy,
  phrase,
  perChange = false,
  decision,
  approveLabel = "Approve",
  rejectLabel = "Decline",
  allowOther = true,
  className,
  receipt,
  ...a
}: CardApprovalProps) {
  const { disabled, working, respond } = useCardRespond(a)
  const level: Level = risk === "low" || risk === "medium" ? risk : "high"
  const r = RISK[level]
  const split = perChange && changes.length > 1
  const [included, setIncluded] = React.useState<boolean[]>(() => changes.map(() => true))
  const count = included.filter(Boolean).length
  const [confirming, setConfirming] = React.useState(false)
  const [typed, setTyped] = React.useState("")
  const open = a.state === "open" || a.state === "pending"
  const typedOk = !phrase || typed.trim().toLowerCase() === phrase.toLowerCase()

  const decide = (action: "approve" | "reject") => {
    if (!split || action === "reject") {
      void respond({ status: "submitted", decisions: [{ itemId: "approval", action }] })
      return
    }
    void respond({
      status: "submitted",
      decisions: changes.map((c, i) => ({ itemId: `change:${i}`, action: included[i] ? "approve" : "reject", note: c.label })),
    })
  }
  const approveText = split && count < changes.length ? `${approveLabel} ${count} of ${changes.length}` : approveLabel

  return (
    <CardFrame
      data-slot="card-approval"
      data-risk={level}
      title={title ?? "Approval needed"}
      icon={<r.icon className={r.iconClass} />}
      tone={open ? r.tone : "default"}
      meta={
        <>
          <Badge variant="outline" className={cn("gap-1", r.badge)}>
            {r.label}
          </Badge>
          <CardStatus state={a.state} />
        </>
      }
      className={cn(
        "relative overflow-hidden before:absolute before:inset-y-0 before:left-0 before:w-1",
        open ? r.edge : "before:bg-transparent",
        !open && "opacity-80",
        className,
      )}
      footer={
        <CardFoot {...a} allowOther={allowOther} receipt={decision ? <DecisionReceipt decision={decision} /> : receipt}>
          {confirming ? (
            <div className="flex w-full flex-col gap-2 rounded-md border border-destructive/40 bg-destructive/5 p-2" role="alertdialog" aria-label="Confirm a high-risk approval">
              <p className="text-xs text-foreground">
                <span className="font-medium text-destructive">High risk.</span> This cannot be taken back from here. Approve {split ? `${count} of ${changes.length} changes` : "it"}?
              </p>
              {phrase && (
                <label className="flex flex-col gap-1 text-xs text-muted-foreground">
                  <span>
                    Type <span className="font-mono text-foreground">{phrase}</span> to confirm.
                  </span>
                  <Input value={typed} disabled={disabled} autoComplete="off" onChange={(e) => setTyped(e.target.value)} />
                </label>
              )}
              <div className="flex justify-end gap-2">
                <Button variant="ghost" size="sm" disabled={disabled} onClick={() => setConfirming(false)}>
                  Back
                </Button>
                <Button variant="destructive" size="sm" disabled={disabled || !typedOk || count === 0} onClick={() => decide("approve")}>
                  {working && <Spinner />}
                  Confirm approval
                </Button>
              </div>
            </div>
          ) : (
            <>
              <Button variant="outline" size="sm" disabled={disabled} onClick={() => decide("reject")}>
                {rejectLabel}
              </Button>
              <Button
                variant={level === "high" ? "destructive" : "default"}
                size="sm"
                disabled={disabled || count === 0}
                onClick={() => (level === "high" ? setConfirming(true) : decide("approve"))}
              >
                {working && <Spinner />}
                {level === "high" ? `${approveText}…` : approveText}
              </Button>
            </>
          )}
        </CardFoot>
      }
    >
      <CardBody className="text-foreground">{summary}</CardBody>
      {changes.length > 0 && (
        <dl className="flex flex-col divide-y rounded-md border text-xs">
          {changes.map((c, i) => (
            <div
              key={`${c.label}-${i}`}
              className={cn("flex flex-col gap-1 px-2 py-1.5", split && !included[i] && "opacity-50")}
            >
              <dt className="flex items-center justify-between gap-2 text-muted-foreground">
                <span>{c.label}</span>
                {split && open && (
                  <label className="flex items-center gap-1.5 text-xs">
                    <input
                      type="checkbox"
                      className="size-3.5 accent-primary"
                      checked={included[i]}
                      disabled={disabled || confirming}
                      onChange={(e) => setIncluded(included.map((v, j) => (j === i ? e.target.checked : v)))}
                    />
                    {included[i] ? "Approve" : "Decline"}
                  </label>
                )}
              </dt>
              {isShort(c.before) && isShort(c.after) ? <ChangeShort change={c} /> : <ChangeLong change={c} />}
            </div>
          ))}
        </dl>
      )}
      {requestedBy && <p className="text-xs text-muted-foreground">Asked by {requestedBy}</p>}
    </CardFrame>
  )
}

export { CardApproval }
export type { CardApprovalChange, CardApprovalDecision, CardApprovalProps }
