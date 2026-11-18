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

type Of<K extends ChatItem["kind"]> = Extract<ChatItem, { kind: K }>
export type ToolItem = Of<"tool">

/** Tool name → the card that draws its calls. Empty until the cards step. */
export const toolCards: Record<string, ComponentType<{ item: ToolItem }>> = {}

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
