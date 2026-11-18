/**
 * How each item of the conversation is drawn. The table is the seam for cards: a tool with a
 * card of its own (an approval, a plan, progress) is drawn by the component `toolCards` names
 * for it, and every other tool by the plain row.
 */
import type { ChatItem } from "@surface/chat"
import type { ComponentType } from "react"
import { Markdown } from "@/components/ui/markdown"
import { Message, MessageContent, MessageNotice } from "@/components/ui/message"
import { Spinner } from "@/components/ui/spinner"
import { Tool, ToolContent, ToolHeader, ToolInput, ToolOutput } from "@/components/ui/tool"
import { CardView } from "../cards/renderers"
import { type ServerCard, useCard } from "../cards/store"

type Of<K extends ChatItem["kind"]> = Extract<ChatItem, { kind: K }>
export type ToolItem = Of<"tool">

/**
 * A card the agent raised, in the conversation. While the agent waits, the card itself is
 * docked above the composer and the conversation only says a question is up; once it is
 * answered the card is drawn here, closed, with what was answered.
 */
function AskedCard({ item }: { item: ToolItem }) {
  const input = (item.input ?? {}) as {
    kind?: string
    title?: string
    risk?: string
    payload?: Record<string, unknown>
  }
  let result: { id?: string; status?: string; data?: Record<string, unknown>; answers?: never[]; decisions?: never[] } =
    {}
  try {
    if (item.result) result = JSON.parse(item.result)
  } catch {
    // The result is not the tool's JSON (an error message): the plain row shows it.
  }
  const live = useCard(result.id ?? "")
  if (item.status === "error") return <ToolRow item={item} />
  if (item.status === "running" || !input.kind) {
    return (
      <MessageNotice className="flex items-center gap-1.5">
        <Spinner className="size-3" /> Waiting on your answer{input.title ? `: ${input.title}` : ""}
      </MessageNotice>
    )
  }
  const card: ServerCard = live ?? {
    id: result.id ?? item.id,
    kind: input.kind,
    title: input.title,
    risk: input.risk,
    payload: input.payload,
    status: result.status || "submitted",
    outcome:
      result.status === "submitted"
        ? { status: "submitted", data: result.data, answers: result.answers, decisions: result.decisions }
        : undefined,
  }
  return <CardView card={card} />
}

/** A card the agent showed or changed without waiting: a quiet line; the card is in the dock. */
function ShownCard({ item }: { item: ToolItem }) {
  const input = (item.input ?? {}) as { kind?: string; title?: string; end?: boolean }
  if (item.status === "error") return <ToolRow item={item} />
  const what =
    item.name === "card_update" ? (input.end ? "Closed a card" : "Updated a card") : `Showed a ${input.kind ?? ""} card`
  return (
    <MessageNotice>
      {what}
      {input.title ? `: ${input.title}` : ""}
    </MessageNotice>
  )
}

/**
 * A generated surface the agent showed. While it is up it is docked above the composer;
 * afterwards it is drawn here, closed, when this page still holds it, and otherwise as a
 * line saying what was shown and what came of it.
 */
function GeneratedSurface({ item }: { item: ToolItem }) {
  const input = (item.input ?? {}) as { pattern?: string; params?: { title?: string } }
  let result: { id?: string; status?: string } = {}
  try {
    if (item.result) result = JSON.parse(item.result)
  } catch {
    // Not the tool's JSON (an error message): the plain row shows it.
  }
  const live = useCard(result.id ?? "")
  if (item.status === "error") return <ToolRow item={item} />
  const title = input.params?.title
  if (item.status === "running") {
    return (
      <MessageNotice className="flex items-center gap-1.5">
        <Spinner className="size-3" /> Showing you{title ? `: ${title}` : " something"}
      </MessageNotice>
    )
  }
  if (live) return <CardView card={live} />
  return (
    <MessageNotice>
      Showed you{title ? `: ${title}` : " a generated surface"}
      {result.status ? ` (${result.status})` : ""}
    </MessageNotice>
  )
}

/** Tool name → the card that draws its calls. */
export const toolCards: Record<string, ComponentType<{ item: ToolItem }>> = {
  card_ask: AskedCard,
  card_show: ShownCard,
  card_update: ShownCard,
  genui_show: GeneratedSurface,
}

function ToolRow({ item }: { item: ToolItem }) {
  return (
    <Tool>
      <ToolHeader title={item.name} status={item.status} />
      <ToolContent>
        <ToolInput input={item.input} />
        <ToolOutput output={item.result} error={item.status === "error"} />
      </ToolContent>
    </Tool>
  )
}

/** One kind → how it is drawn. */
export const renderers: { [K in ChatItem["kind"]]: ComponentType<{ item: Of<K> }> } = {
  user: ({ item }) =>
    item.auto ? (
      <MessageNotice>{item.auto}</MessageNotice>
    ) : (
      <Message from="user">
        <MessageContent>{item.text}</MessageContent>
      </Message>
    ),
  text: ({ item }) =>
    item.text ? (
      <Message from="assistant">
        <MessageContent>
          <Markdown streaming={item.streaming}>{item.text}</Markdown>
        </MessageContent>
      </Message>
    ) : item.streaming ? (
      <MessageNotice className="flex items-center gap-1.5">
        <Spinner className="size-3" /> Writing…
      </MessageNotice>
    ) : null,
  tool: ({ item }) => {
    const Card = toolCards[item.name] ?? ToolRow
    return <Card item={item} />
  },
  notice: ({ item }) => <MessageNotice tone={item.tone}>{item.text}</MessageNotice>,
}

export function ItemView({ item }: { item: ChatItem }) {
  const Draw = renderers[item.kind] as ComponentType<{ item: ChatItem }>
  return <Draw item={item} />
}
