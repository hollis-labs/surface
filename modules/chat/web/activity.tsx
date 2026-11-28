/**
 * The agent's work, out of the thread. Tool calls (but a card_ask, which the owner answers)
 * and reasoning are folded: the thread keeps the conversation, and one activity line, pinned
 * above the composer, says what the agent is doing now. Clicking it opens the timeline: every
 * step, turn by turn, with each tool call's input and output and the reasoning.
 */
import { type ChatItem, type ChatState, type ConversationStore, useChat, useChatItem } from "@hollis-labs/surface-chat"
import { baseToolName, ChatItemView, describeToolCall, Icon } from "@hollis-labs/surface-page/sdk"
import { memo, useEffect, useRef, useState } from "react"
import { Button } from "@/components/ui/button"
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Spinner } from "@/components/ui/spinner"

/** Folded out of the thread: reasoning, and every tool call but a question for the owner. */
export function isFolded(item: ChatItem | undefined): boolean {
  if (!item) return false
  if (item.kind === "thinking") return true
  return item.kind === "tool" && baseToolName(item.name) !== "card_ask"
}

/** One line for a step. */
export function describeStep(item: ChatItem): { icon: string; text: string } {
  if (item.kind === "thinking") {
    const first = item.text.trim().split("\n")[0] ?? ""
    return { icon: "brain", text: first ? `Thinking: ${first}` : "Thinking…" }
  }
  if (item.kind === "tool") {
    const d = describeToolCall(baseToolName(item.name), item.input)
    const summary = d.summary || item.name
    return { icon: d.icon || "wrench", text: d.detail ? `${summary} · ${d.detail}` : summary }
  }
  return { icon: "dot", text: "" }
}

/** The steps since the last message of the owner: "running|count|last id". */
function currentSteps(s: ChatState): string {
  let count = 0
  let last = ""
  for (let i = s.ids.length - 1; i >= 0; i--) {
    const id = s.ids[i] as string
    const it = s.items[id]
    if (it?.kind === "user" && !it.auto) break
    if (isFolded(it)) {
      count++
      if (!last) last = id
    }
  }
  return `${s.run ? 1 : 0}|${count}|${last}`
}

/** The pinned line: what the agent is doing now, or what it did last turn. */
export const ActivityLine = memo(function ActivityLine({ store }: { store: ConversationStore }) {
  const key = useChat(store, currentSteps)
  const [running, count, lastId] = key.split("|") as [string, string, string]
  const last = useChatItem(store, lastId)
  const [open, setOpen] = useState(false)
  if (count === "0" && running === "0") return null
  const step = last ? describeStep(last) : { icon: "activity", text: "Working…" }
  const busy = running === "1"
  return (
    <>
      <Button
        type="button"
        variant="ghost"
        size="xs"
        className="h-7 w-full justify-start gap-2 rounded-none border-t border-[var(--color-border)] px-3 font-mono text-[11px] text-[var(--color-muted-foreground)]"
        title="Open the activity timeline"
        onClick={() => setOpen(true)}
      >
        {busy ? <Spinner className="size-3 shrink-0" /> : <Icon name="activity" size={12} />}
        <Icon name={step.icon} size={12} />
        <span className="min-w-0 flex-1 truncate text-left">{step.text}</span>
        <span className="shrink-0">{count === "1" ? "1 step" : `${count} steps`}</span>
        <Icon name="chevron-up" size={12} />
      </Button>
      <ActivityDialog store={store} open={open} onOpenChange={setOpen} />
    </>
  )
})

interface Turn {
  title: string
  steps: string[]
}

/** Groups the folded steps by the owner's message that started each turn. */
function turnsOf(s: ChatState): Turn[] {
  const out: Turn[] = []
  let cur: Turn = { title: "Earlier", steps: [] }
  for (const id of s.ids) {
    const it = s.items[id]
    if (it?.kind === "user" && !it.auto) {
      if (cur.steps.length) out.push(cur)
      cur = { title: it.text.split("\n")[0] ?? "", steps: [] }
    } else if (isFolded(it)) cur.steps.push(id)
  }
  if (cur.steps.length) out.push(cur)
  return out
}

function ActivityDialog({
  store,
  open,
  onOpenChange,
}: {
  store: ConversationStore
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[85vh] w-[min(56rem,95vw)] max-w-none flex-col gap-3 sm:max-w-none">
        <DialogHeader>
          <DialogTitle>Activity</DialogTitle>
          <DialogDescription>
            Every step the agent took, turn by turn: tool calls with input and output, and its reasoning.
          </DialogDescription>
        </DialogHeader>
        {open && <Timeline store={store} />}
      </DialogContent>
    </Dialog>
  )
}

function Timeline({ store }: { store: ConversationStore }) {
  const state = useChat(store, (s) => s)
  const turns = turnsOf(state)
  const end = useRef<HTMLDivElement>(null)
  useEffect(() => {
    end.current?.scrollIntoView({ block: "end" })
  }, [])
  return (
    <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto pr-1">
      {turns.length === 0 && <p className="text-xs text-[var(--color-muted-foreground)]">No activity yet.</p>}
      {turns.map((t) => (
        <section key={t.steps[0]} className="flex flex-col gap-1.5">
          <h3 className="truncate border-b border-[var(--color-border)] pb-1 text-xs font-medium">
            {t.title || "Turn"}
          </h3>
          {t.steps.map((id) => {
            const it = state.items[id]
            if (!it) return null
            if (it.kind === "thinking") return <Thinking key={id} text={it.text} streaming={it.streaming} />
            return <ChatItemView key={id} item={it} />
          })}
        </section>
      ))}
      <div ref={end} />
    </div>
  )
}

function Thinking({ text, streaming }: { text: string; streaming: boolean }) {
  return (
    <Collapsible>
      <CollapsibleTrigger className="flex w-full items-center gap-2 rounded px-1 py-1 text-left text-xs text-[var(--color-muted-foreground)] hover:bg-[var(--color-accent)]">
        {streaming ? <Spinner className="size-3" /> : <Icon name="brain" size={12} />}
        <span className="min-w-0 flex-1 truncate">Thinking{text ? `: ${text.trim().split("\n")[0]}` : "…"}</span>
        <Icon name="chevron-down" size={12} />
      </CollapsibleTrigger>
      <CollapsibleContent>
        <pre className="max-h-80 overflow-auto whitespace-pre-wrap rounded bg-[var(--color-muted)] p-2 text-[11px] leading-snug">
          {text || "(no text)"}
        </pre>
      </CollapsibleContent>
    </Collapsible>
  )
}
