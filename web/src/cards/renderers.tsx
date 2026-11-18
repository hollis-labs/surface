/**
 * Which component draws each kind of card. The table is the app's: a module with a card
 * kind of its own registers its renderer here (`registerCard`), and a card nothing is
 * registered for is drawn as a plain "no renderer" card, never as nothing.
 */
import {
  type CardOutcome,
  classifyPriorResponse,
  compileFormSchema,
  createCardRegistry,
  validateForm,
} from "@surface/cards"
import { Component, type ComponentType, type ReactNode, useMemo } from "react"
import { CardApproval, type CardApprovalChange } from "@/components/ui/card-approval"
import { CardChoice, type CardChoiceOption } from "@/components/ui/card-choice"
import { CardConfirm } from "@/components/ui/card-confirm"
import { CardForm } from "@/components/ui/card-form"
import type { AnswerableCardProps } from "@/components/ui/card-frame"
import { CardMiss } from "@/components/ui/card-miss"
import { CardNotice } from "@/components/ui/card-notice"
import { CardPlan, type CardPlanStep } from "@/components/ui/card-plan"
import { CardProgress } from "@/components/ui/card-progress"
import { CardPrompt } from "@/components/ui/card-prompt"
import { GenuiCard } from "../genui/card"
import { isStale, respond, type ServerCard } from "./store"

/** What a renderer is handed: the card, and how to answer it. */
export interface CardRendererProps {
  card: ServerCard
  respond: (outcome: CardOutcome) => Promise<void>
}
export type CardRenderer = ComponentType<CardRendererProps>

const registry = createCardRegistry<CardRenderer>()
/** Registers the component that draws a kind of card at a version. */
export const registerCard = (kind: string, version: string, component: CardRenderer) =>
  registry.register(kind, version, component)

const str = (v: unknown): string | undefined => (typeof v === "string" && v !== "" ? v : undefined)
const list = <T,>(v: unknown): T[] => (Array.isArray(v) ? (v as T[]) : [])

/** A closed card's answer in a few words, for its receipt. */
export function describeOutcome(card: ServerCard): string | undefined {
  const o = card.outcome
  if (o?.status !== "submitted") return undefined
  if (o.decisions?.length) return o.decisions.map((d) => d.action).join(", ")
  if (o.answers?.length) {
    const options = list<CardChoiceOption>(card.payload?.options)
    const label = (v: unknown) => options.find((x) => x.id === v)?.label ?? String(v)
    return o.answers
      .map((a) => {
        const picked = Array.isArray(a.value) ? a.value.map(label).join(", ") : label(a.value)
        return a.note ? `${picked}: ${a.note}` : picked
      })
      .join("; ")
  }
  if (o.data) {
    const n = Object.keys(o.data).length
    return `${n} ${n === 1 ? "field" : "fields"}`
  }
  return undefined
}

const clock = new Intl.DateTimeFormat(undefined, { hour: "2-digit", minute: "2-digit" })

/** The props every answerable card shares, from the card as the server has it. */
function answerable({ card, respond: send }: CardRendererProps): AnswerableCardProps {
  const state = classifyPriorResponse(card.status)
  const at = card.expires_at ? Date.parse(card.expires_at) : Number.NaN
  return {
    title: card.title,
    state: state.kind,
    stale: isStale(card.id),
    receipt: describeOutcome(card),
    expires: state.kind === "open" && Number.isFinite(at) ? `at ${clock.format(at)}` : undefined,
    onRespond: send,
  }
}

registerCard("info", "1", ({ card, respond: send }) => (
  <CardNotice
    title={card.title}
    body={str(card.payload?.body)}
    tone={(str(card.payload?.tone) as "info" | "success" | "warning" | undefined) ?? "info"}
    state={classifyPriorResponse(card.status).kind}
    onDismiss={() => void send({ status: "canceled" })}
  />
))
registerCard("error", "1", ({ card, respond: send }) => (
  <CardNotice
    title={card.title}
    body={str(card.payload?.body)}
    detail={str(card.payload?.detail)}
    tone="error"
    state={classifyPriorResponse(card.status).kind}
    onDismiss={() => void send({ status: "canceled" })}
  />
))
registerCard("confirm", "1", (p) => (
  <CardConfirm
    {...answerable(p)}
    body={str(p.card.payload?.body)}
    confirmLabel={str(p.card.payload?.confirm_label)}
    cancelLabel={str(p.card.payload?.cancel_label)}
    danger={p.card.payload?.danger === true}
  />
))
registerCard("prompt", "1", (p) => (
  <CardPrompt
    {...answerable(p)}
    question={str(p.card.payload?.question) ?? ""}
    body={str(p.card.payload?.body)}
    placeholder={str(p.card.payload?.placeholder)}
    multiline={p.card.payload?.multiline === true}
  />
))
registerCard("choice", "1", (p) => (
  <CardChoice
    {...answerable(p)}
    question={str(p.card.payload?.question) ?? ""}
    body={str(p.card.payload?.body)}
    options={list<CardChoiceOption>(p.card.payload?.options)}
    multiple={p.card.payload?.multiple === true}
    allowOther={p.card.payload?.allow_other === true}
  />
))
registerCard("form", "1", function FormRenderer(p) {
  const schema = p.card.payload?.schema
  const form = useMemo(() => compileFormSchema(schema), [schema])
  return (
    <CardForm
      {...answerable(p)}
      body={str(p.card.payload?.body)}
      fields={form.fields}
      issues={form.issues}
      values={p.card.payload?.values as Record<string, unknown> | undefined}
      validate={(values) => ({ ...validateForm(form.fields, values) })}
      submitLabel={str(p.card.payload?.submit_label)}
    />
  )
})
registerCard("approval", "1", (p) => (
  <CardApproval
    {...answerable(p)}
    summary={str(p.card.payload?.summary)}
    risk={p.card.risk || "low"}
    changes={list<CardApprovalChange>(p.card.payload?.changes)}
    requestedBy={str(p.card.payload?.requested_by)}
    phrase={str(p.card.payload?.phrase)}
  />
))
registerCard("progress", "1", ({ card }) => (
  <CardProgress
    title={card.title}
    label={str(card.payload?.label)}
    value={typeof card.payload?.value === "number" ? card.payload.value : undefined}
    max={typeof card.payload?.max === "number" ? card.payload.max : undefined}
    status={
      (str(card.payload?.status) as "running" | "done" | "failed" | undefined) ?? (card.status ? "done" : "running")
    }
    detail={str(card.payload?.detail)}
  />
))
registerCard("plan", "1", ({ card }) => <CardPlan title={card.title} steps={list<CardPlanStep>(card.payload?.steps)} />)

// A generated surface is a card too: decoded, checked and driven by @surface/genui.
registerCard("genui", "1", GenuiCard)

/** A renderer that throws takes down its own card, not what is around it. */
class Boundary extends Component<{ card: ServerCard; children: ReactNode }, { error: Error | null }> {
  state = { error: null as Error | null }
  static getDerivedStateFromError(error: Error) {
    return { error }
  }
  render() {
    const { card } = this.props
    if (this.state.error) {
      return (
        <CardMiss
          kind={card.kind}
          version={card.version}
          reason="failed"
          title={card.title}
          detail={this.state.error.message}
          payload={card.payload}
        />
      )
    }
    return this.props.children
  }
}

/** Draws one card with the renderer registered for its kind. */
export function CardView({ card }: { card: ServerCard }) {
  const found = registry.resolve(card.kind, card.version || "1")
  if (!found.ok) {
    return (
      <CardMiss
        kind={card.kind}
        version={card.version}
        reason={found.miss.code}
        title={card.title}
        payload={card.payload}
      />
    )
  }
  const Draw = found.component
  return (
    <Boundary card={card} key={`${card.kind}@${card.version ?? "1"}`}>
      <Draw card={card} respond={(outcome) => respond(card.id, outcome)} />
    </Boundary>
  )
}
