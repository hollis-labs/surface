"use client"

import { ArrowRightIcon, ShieldAlertIcon, ShieldCheckIcon } from "lucide-react"
import * as React from "react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { type AnswerableCardProps, CardBody, CardFoot, CardFrame, CardStatus, useCardRespond } from "@/components/ui/card-frame"
import { Input } from "@/components/ui/input"
import { Spinner } from "@/components/ui/spinner"

/**
 * A request to approve something consequential: what will change, how risky it is, who
 * asked. A high-risk approval makes the person type a phrase first. A risk the card does
 * not know is treated as high. Surface's own.
 */
interface CardApprovalChange {
  label: string
  before?: string
  after?: string
}

interface CardApprovalProps extends AnswerableCardProps {
  summary?: string
  risk?: string
  changes?: CardApprovalChange[]
  requestedBy?: string
  /** The phrase a high-risk approval asks the person to type. */
  phrase?: string
  approveLabel?: string
  rejectLabel?: string
}

function CardApproval({ title, summary, risk = "low", changes = [], requestedBy, phrase = "approve", approveLabel = "Approve", rejectLabel = "Reject", className, ...a }: CardApprovalProps) {
  const { disabled, working, respond } = useCardRespond(a)
  const [typed, setTyped] = React.useState("")
  const level = risk === "low" || risk === "medium" ? risk : "high"
  const armed = level !== "high" || typed.trim().toLowerCase() === phrase.toLowerCase()
  const decide = (action: "approve" | "reject") => void respond({ status: "submitted", decisions: [{ itemId: "approval", action }] })
  const open = a.state === "open" || a.state === "pending"
  return (
    <CardFrame
      data-slot="card-approval"
      title={title ?? "Approval needed"}
      icon={level === "low" ? <ShieldCheckIcon /> : <ShieldAlertIcon className={level === "high" ? "text-destructive" : "text-warning"} />}
      tone={level === "high" ? "danger" : level === "medium" ? "warning" : "default"}
      meta={
        <>
          <Badge variant={level === "high" ? "destructive" : "outline"}>{level} risk</Badge>
          <CardStatus state={a.state} />
        </>
      }
      className={className}
      footer={
        <CardFoot {...a}>
          <Button variant="outline" size="sm" disabled={disabled} onClick={() => decide("reject")}>
            {rejectLabel}
          </Button>
          <Button variant={level === "high" ? "destructive" : "default"} size="sm" disabled={disabled || !armed} onClick={() => decide("approve")}>
            {working && <Spinner />}
            {approveLabel}
          </Button>
        </CardFoot>
      }
    >
      <CardBody className="text-foreground">{summary}</CardBody>
      {changes.length > 0 && (
        <dl className="flex flex-col gap-1.5 rounded-md border p-2 text-xs">
          {changes.map((c) => (
            <div key={c.label} className="flex flex-col gap-0.5">
              <dt className="text-muted-foreground">{c.label}</dt>
              <dd className="flex min-w-0 flex-wrap items-center gap-1.5 font-mono">
                {c.before !== undefined && <span className="text-muted-foreground line-through">{c.before}</span>}
                {c.before !== undefined && c.after !== undefined && <ArrowRightIcon className="size-3 shrink-0 text-muted-foreground" />}
                {c.after !== undefined && <span>{c.after}</span>}
              </dd>
            </div>
          ))}
        </dl>
      )}
      {requestedBy && <p className="text-xs text-muted-foreground">Asked by {requestedBy}</p>}
      {open && level === "high" && (
        <label className="flex flex-col gap-1.5 text-xs text-muted-foreground">
          <span>
            Type <span className="font-mono text-foreground">{phrase}</span> to approve.
          </span>
          <Input value={typed} disabled={disabled} autoComplete="off" onChange={(e) => setTyped(e.target.value)} />
        </label>
      )}
    </CardFrame>
  )
}

export { CardApproval }
export type { CardApprovalChange, CardApprovalProps }
