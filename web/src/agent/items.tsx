/**
 * How each item of the conversation is drawn. The table is the seam for cards: a tool with a
 * card of its own (an approval, a plan, progress) is drawn by the component `toolCards` names
 * for it, and every other tool by the plain row.
 */
import type { ChatItem } from "@hollis-labs/surface-chat"
import { CheckCircle2Icon, CheckIcon, CopyIcon, ExternalLinkIcon, ListChecksIcon } from "lucide-react"
import { type ComponentType, useCallback, useEffect, useMemo, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Markdown } from "@/components/ui/markdown"
import { Message, MessageContent, MessageNotice } from "@/components/ui/message"
import { Spinner } from "@/components/ui/spinner"
import { Tool, ToolContent, ToolHeader, ToolInput, ToolOutput } from "@/components/ui/tool"
import { isLive, setOpenedFromChat, useCard, useCardsByIds } from "../cards/store"
import { desk } from "../desk/store"
import { useEvent } from "../sdk"
import { baseToolName } from "./toolname.ts"

export { baseToolName }

type Of<K extends ChatItem["kind"]> = Extract<ChatItem, { kind: K }>

export type ToolItem = Of<"tool">

/**
 * A card (or batch of cards) the agent raised in the conversation.
 * Renders as a compact, fixed-height single-line receipt/status bar inside the chat stream
 * so it never splits the conversation or causes layout jumps when cards open or close.
 */
function AskedCard({ item }: { item: ToolItem }) {
  const input = (item.input ?? {}) as {
    kind?: string
    title?: string
    risk?: string
    payload?: Record<string, unknown>
    questions?: { kind?: string; title?: string; payload?: Record<string, unknown> }[]
  }
  let result: {
    id?: string
    ids?: string[]
    status?: string
    data?: Record<string, unknown>
    answers?: { questionId?: string; value?: unknown; note?: string }[]
    decisions?: { itemId?: string; action?: string; note?: string }[]
  } = {}
  try {
    if (item.result) result = JSON.parse(item.result)
  } catch {
    // The result is not the tool's JSON (an error message): the plain row shows it.
  }

  const cardIds = useMemo(() => {
    if (Array.isArray(result.ids) && result.ids.length > 0) return result.ids
    if (typeof result.id === "string" && result.id) return [result.id]
    return []
  }, [result.id, result.ids])

  const liveCards = useCardsByIds(cardIds)
  if (item.status === "error") return <ToolRow item={item} />

  const questionList = Array.isArray(input.questions) ? input.questions : []
  const totalCount = Math.max(cardIds.length, questionList.length, 1)
  const primaryTitle =
    input.title ||
    questionList[0]?.title ||
    (typeof input.payload?.question === "string" ? input.payload.question : "") ||
    (totalCount > 1 ? `${totalCount} questions` : "Question")

  const openCards = liveCards.filter(isLive)
  const stillWaiting = item.status === "running" || (cardIds.length > 0 && openCards.length > 0)

  const openInCardsWindow = () => {
    setOpenedFromChat(true)
    void desk.open("surface.cards", { args: { from_chat: true } })
  }

  if (stillWaiting) {
    const waitingCount = openCards.length > 0 ? openCards.length : totalCount
    return (
      <div
        data-slot="chat-card-status"
        className="my-1 flex items-center justify-between gap-2 rounded-sm border border-primary/35 bg-primary/8 px-2.5 py-1.5 text-xs"
      >
        <div className="flex min-w-0 items-center gap-2">
          <Spinner className="size-3.5 shrink-0 text-primary" />
          <span className="font-mono text-[11px] font-semibold text-primary">
            {waitingCount > 1 ? `${waitingCount} questions waiting` : "Waiting for answer"}
          </span>
          <span className="truncate text-foreground/85">{primaryTitle}</span>
        </div>
        <Button
          variant="outline"
          size="xs"
          className="h-5.5 shrink-0 gap-1 border-primary/35 bg-background/80 px-2 font-mono text-[10.5px] text-primary hover:bg-primary/15"
          onClick={openInCardsWindow}
        >
          <ExternalLinkIcon className="size-3" />
          Answer in Cards
        </Button>
      </div>
    )
  }

  // Answered / completed receipt: compact 1-line pill that never splits the chat stream.
  const answeredCount =
    liveCards.length > 0 ? liveCards.filter((c) => c.status === "submitted").length || totalCount : totalCount
  return (
    <div
      data-slot="chat-card-receipt"
      className="my-1 flex items-center justify-between gap-2 rounded-sm border border-border/70 bg-muted/30 px-2.5 py-1.5 text-xs"
    >
      <div className="flex min-w-0 items-center gap-2">
        <CheckCircle2Icon className="size-3.5 shrink-0 text-success" />
        <span className="font-mono text-[11px] font-medium text-success">
          {totalCount > 1 ? `Answered ${answeredCount} of ${totalCount}` : "Answered"}
        </span>
        <span className="truncate text-muted-foreground">{primaryTitle}</span>
      </div>
      <Button
        variant="ghost"
        size="xs"
        className="h-5 shrink-0 gap-1 px-1.5 font-mono text-[10.5px] text-muted-foreground hover:text-foreground"
        onClick={() => {
          setOpenedFromChat(false)
          void desk.open("surface.cards", { args: { manual: true } })
        }}
      >
        <ListChecksIcon className="size-3" />
        View
      </Button>
    </div>
  )
}

/** A card the agent showed or changed without waiting: a quiet line; the card is in the dock. */
function ShownCard({ item }: { item: ToolItem }) {
  const input = (item.input ?? {}) as { kind?: string; title?: string; end?: boolean }
  if (item.status === "error") return <ToolRow item={item} />
  const what =
    baseToolName(item.name) === "card_update"
      ? input.end
        ? "Closed a card"
        : "Updated a card"
      : `Showed a ${input.kind ?? ""} card`
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
  if (live) {
    return (
      <div className="my-1 flex items-center justify-between gap-2 rounded-sm border border-border/70 bg-muted/30 px-2.5 py-1.5 text-xs">
        <span className="truncate text-muted-foreground">
          Showed you{title ? `: ${title}` : " a generated surface"}
        </span>
        <Button
          variant="ghost"
          size="xs"
          className="h-5 shrink-0 px-1.5 font-mono text-[10.5px]"
          onClick={() => void desk.open("surface.cards", { args: { manual: true } })}
        >
          View
        </Button>
      </div>
    )
  }
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

function DeskPlan({ item }: { item: Of<"plan"> }) {
  const [status, setStatus] = useState("")
  const [loaded, setLoaded] = useState(item.disposition !== "suggest")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const refresh = useCallback(async () => {
    if (item.disposition !== "suggest") return
    try {
      const r = await fetch(`/api/agent/front-desk/plans/${encodeURIComponent(item.planID)}`)
      const body = await r.json()
      if (!r.ok) throw new Error(body.message || "Could not load the plan decision.")
      setStatus((current) => (body.status === "accept" ? "Started" : body.status === "decline" ? "Declined" : current))
      setLoaded(true)
      setError("")
    } catch (e) {
      setError((e as Error).message)
    }
  }, [item.planID, item.disposition])
  useEffect(() => {
    void refresh()
  }, [refresh])
  useEvent<{ plan: string }>("agent.plan.decision", (ev) => {
    if (ev.data.plan === item.planID) void refresh()
  })
  const answer = async (action: "accept" | "decline") => {
    setBusy(true)
    setError("")
    try {
      const r = await fetch(`/api/agent/front-desk/plans/${encodeURIComponent(item.planID)}/${action}`, {
        method: "POST",
      })
      const body = await r.json()
      if (!r.ok) throw new Error(body.message || "This plan could not be answered.")
      setStatus(action === "accept" ? "Started" : "Declined")
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }
  return (
    <div className="flex flex-col gap-2 p-2">
      <MessageNotice>
        {status || (item.disposition === "dispatch" ? "Background plan started" : "Suggested background plan")}
      </MessageNotice>
      <Markdown>{item.text}</Markdown>
      {item.disposition === "suggest" && loaded && !status && (
        <div className="flex gap-2">
          <Button size="sm" disabled={busy} onClick={() => void answer("accept")}>
            Start plan
          </Button>
          <Button size="sm" variant="outline" disabled={busy} onClick={() => void answer("decline")}>
            Decline
          </Button>
        </div>
      )}
      {error && <MessageNotice tone="error">{error}</MessageNotice>}
    </div>
  )
}

/** Assistant turn with clear separation, topic/thread badge, and hover copy action. */
function AssistantMessage({ item, topic, slot }: { item: Of<"text">; topic?: string; slot?: string }) {
  const [copied, setCopied] = useState(false)
  if (!item.text) {
    return item.streaming ? (
      <MessageNotice className="flex items-center gap-1.5">
        <Spinner className="size-3" /> Writing…
      </MessageNotice>
    ) : null
  }
  const copy = () => {
    void navigator.clipboard?.writeText(item.text).then(() => {
      setCopied(true)
      window.setTimeout(() => setCopied(false), 1400)
    })
  }
  const activeTopic = topic || slot || "general"
  return (
    <Message from="assistant" className="my-1">
      <MessageContent>
        <div className="mb-1 flex items-center gap-1.5 border-b border-border/60 pb-1.5">
          <span className="size-1.5 rounded-full bg-primary" />
          <span className="font-mono text-[11px] font-semibold text-primary">Agent</span>
          <Badge
            variant="outline"
            className="h-4 border-chart-2/30 bg-chart-2/10 px-1.5 py-0 font-mono text-[10px] text-chart-2"
            title={`Tracking topic: #${activeTopic}`}
          >
            #{activeTopic}
          </Badge>
          {slot && topic && slot !== topic && (
            <Badge
              variant="secondary"
              className="h-4 px-1.5 py-0 font-mono text-[10px] text-muted-foreground"
              title={`Active context slot: ${slot}`}
            >
              {slot}
            </Badge>
          )}
          {item.streaming && (
            <span className="flex items-center gap-1 font-mono text-[10.5px] text-primary">
              <Spinner className="size-2.5" /> streaming
            </span>
          )}
          <Button
            variant="ghost"
            size="icon-xs"
            className="ml-auto size-5 opacity-0 transition-opacity group-hover/message:opacity-100 focus-visible:opacity-100"
            title={copied ? "Copied" : "Copy response"}
            aria-label={copied ? "Copied" : "Copy response"}
            onClick={copy}
          >
            {copied ? <CheckIcon className="size-3 text-success" /> : <CopyIcon className="size-3" />}
          </Button>
        </div>
        <Markdown streaming={item.streaming}>{item.text}</Markdown>
      </MessageContent>
    </Message>
  )
}

/** One kind → how it is drawn. */
export const renderers: { [K in ChatItem["kind"]]: ComponentType<{ item: Of<K> }> } = {
  user: ({ item }) =>
    item.auto ? (
      <MessageNotice>{item.auto}</MessageNotice>
    ) : (
      <Message from="user" className="mt-2 first:mt-0">
        <MessageContent>{item.text}</MessageContent>
      </Message>
    ),
  text: ({ item }) => <AssistantMessage item={item} />,
  tool: ({ item }) => {
    const Card = toolCards[baseToolName(item.name)] ?? ToolRow
    return <Card item={item} />
  },
  plan: DeskPlan,
  notice: ({ item }) => <MessageNotice tone={item.tone}>{item.text}</MessageNotice>,
}

export function ItemView({ item, topic, slot }: { item: ChatItem; topic?: string; slot?: string }) {
  if (item.kind === "text") {
    return <AssistantMessage item={item} topic={topic} slot={slot} />
  }
  const Draw = renderers[item.kind] as ComponentType<{ item: ChatItem }>
  return <Draw item={item} />
}
