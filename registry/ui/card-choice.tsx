"use client"

import { CheckIcon, ListChecksIcon } from "lucide-react"
import * as React from "react"

import { cn } from "cn"
import { Button } from "@/components/ui/button"
import { type AnswerableCardProps, CardBody, CardFoot, CardFrame, CardStatus, useCardDraft, useCardRespond } from "@/components/ui/card-frame"
import { Input } from "@/components/ui/input"
import { Spinner } from "@/components/ui/spinner"

/** Pick one of a few options, or several, or write another. Surface's own. */
interface CardChoiceOption {
  id: string
  label: string
  description?: string
}

interface CardChoiceProps extends AnswerableCardProps {
  question: string
  body?: string
  options: CardChoiceOption[]
  /** More than one may be picked. */
  multiple?: boolean
  /** The person may write an answer that is not on the list. */
  allowOther?: boolean
}

function CardChoice({ title, question, body, options, multiple, allowOther, draft, onDraft, className, ...a }: CardChoiceProps) {
  const { disabled, working, respond } = useCardRespond(a)
  const [picked, setPicked] = React.useState<string[]>(() =>
    Array.isArray(draft?.picked) ? draft.picked.filter((x): x is string => typeof x === "string") : [],
  )
  const [other, setOther] = React.useState(() => (typeof draft?.other === "string" ? draft.other : ""))
  useCardDraft({ picked, other }, onDraft)
  const toggle = (id: string) =>
    setPicked((was) => (multiple ? (was.includes(id) ? was.filter((x) => x !== id) : [...was, id]) : was.includes(id) ? [] : [id]))
  const otherOn = picked.includes("other")
  const ready = picked.length > 0 && (!otherOn || other.trim() !== "")
  const send = () =>
    void respond({
      status: "submitted",
      answers: [{ questionId: "choice", value: multiple ? picked : picked[0], ...(otherOn ? { note: other.trim() } : {}) }],
    })
  const open = a.state === "open" || a.state === "pending"
  const all = allowOther ? [...options, { id: "other", label: "Something else" }] : options
  return (
    <CardFrame
      data-slot="card-choice"
      title={title ?? "A choice"}
      icon={<ListChecksIcon />}
      meta={<CardStatus state={a.state} />}
      className={className}
      footer={
        <CardFoot {...a}>
          <Button variant="outline" size="sm" disabled={disabled} onClick={() => void respond({ status: "canceled" })}>
            Skip
          </Button>
          <Button size="sm" disabled={disabled || !ready} onClick={send}>
            {working && <Spinner />}
            {multiple ? "Choose these" : "Choose"}
          </Button>
        </CardFoot>
      }
    >
      <CardBody>{body}</CardBody>
      <p className="text-sm font-medium">{question}</p>
      {open && (
        <fieldset className="flex flex-col gap-1.5" aria-label={question}>
          {all.map((o) => {
            const on = picked.includes(o.id)
            return (
              <Button
                key={o.id}
                variant="outline"
                role={multiple ? "checkbox" : "radio"}
                aria-checked={on}
                disabled={disabled}
                className={cn("h-auto justify-start gap-2 px-2.5 py-1.5 text-left whitespace-normal", on && "border-ring bg-muted")}
                onClick={() => toggle(o.id)}
              >
                <span className={cn("flex size-4 shrink-0 items-center justify-center rounded-full border", multiple && "rounded-[4px]", on && "border-primary bg-primary text-primary-foreground")}>
                  {on && <CheckIcon className="size-3" />}
                </span>
                <span className="flex min-w-0 flex-col">
                  <span>{o.label}</span>
                  {o.description && <span className="text-xs font-normal text-muted-foreground">{o.description}</span>}
                </span>
              </Button>
            )
          })}
          {otherOn && (
            <Input autoFocus value={other} placeholder="Say what" disabled={disabled} aria-label="Something else" onChange={(e) => setOther(e.target.value)} />
          )}
        </fieldset>
      )}
    </CardFrame>
  )
}

export { CardChoice }
export type { CardChoiceOption, CardChoiceProps }
