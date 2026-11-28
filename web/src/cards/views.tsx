/** Where cards show: docked in the agent's panel, popped up over the desk, in the middle
 * of the screen when their sender is waiting, and all together in the Cards window. */
import { useCallback, useEffect, useRef, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog"
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { desk, useDeskState } from "../desk/store"
import { Icon, useEvent, useSetting, type ViewProps } from "../sdk"
import { call, setSetting } from "../sdk/runtime"
import { isOwnerTyping } from "../sdk/typing"
import { CardView } from "./renderers"
import {
  ANSWERABLE,
  dismissToast,
  holdCard,
  isClaimed,
  isLive,
  isUserInChat,
  lateOpen,
  loadDraft,
  outcomeFromDraft,
  recentlyAnsweredLate,
  respond,
  type ServerCard,
  setOpenedFromChat,
  startCards,
  useCards,
  wasOpenedFromChat,
} from "./store"

const isOpen = (c: ServerCard) => !c.status

/** Whether the active slot or display setting routes agent questions to the Cards window. */
export function useCardWindowMode(): boolean {
  const mode = (useSetting<string>("surface.card_display") ?? "inline") as string
  const slotsCSV = (useSetting<string>("surface.card_window_slots") ?? "") as string
  const d = useDeskState()
  if (mode === "window") return true
  if (!slotsCSV.trim()) return false
  const focused = d.wins.find((w) => w.id === d.focus)
  const mod = focused?.module ?? ""
  return slotsCSV
    .split(",")
    .map((s) => s.trim())
    .some((s) => s === "*" || (s !== "" && s.toLowerCase() === mod.toLowerCase()))
}

/** The agent's open cards, above its composer, and its questions it stopped waiting for,
 * which still take a late answer. A closed one leaves the dock: the conversation carries
 * what was answered. A late answer stays a moment, to show it was sent.
 * Cards that have been held for the Cards window (or when window mode is active) live in
 * the Cards window so the chat stays clear. */
export function CardDock() {
  useEffect(startCards, [])
  const windowMode = useCardWindowMode()
  const inlineCards = useCards("inline")

  // Tiny quick toasts for answers just submitted (whether in the Cards window or inline).
  const toasts = inlineCards.filter((s) => !isLive(s.card) && recentlyAnsweredLate(s.card))

  // Live cards that should render inline in the dock (not held in the Cards window).
  const open = inlineCards.filter((s) => {
    if (!isLive(s.card)) return false
    // An outside agent's (a seat's) cards are not the embedded agent's: they live in the Cards window.
    if (s.card.agent) return false
    // Held/abandoned cards or window-routed answerable cards live in the Cards window, not inline.
    if (lateOpen(s.card)) return false
    if (windowMode && ANSWERABLE.includes(s.card.kind)) return false
    return true
  })

  if (open.length === 0 && toasts.length === 0) return null
  const moveToWindow = async (id: string) => {
    setOpenedFromChat(true)
    await holdCard(id).catch(() => {})
    void desk.open("surface.cards", { args: { from_chat: true } })
  }
  return (
    <div
      data-slot="card-dock"
      className="flex max-h-[60%] shrink-0 flex-col gap-1.5 overflow-y-auto border-t border-border/70 bg-card/40 px-2.5 py-2 *:shrink-0"
    >
      {toasts.map((s) => (
        <div
          key={`toast-${s.card.id}`}
          role="status"
          data-slot="card-submit-toast"
          className="flex items-center justify-between gap-2 rounded-sm border border-success/40 bg-success/10 px-2.5 py-1 font-mono text-[11px] text-success shadow-2xs"
        >
          <span className="flex min-w-0 items-center gap-1.5 truncate">
            <Icon name="circle-check" size={12} className="shrink-0 text-success" />
            <span className="font-semibold">Answer submitted</span>
            {s.card.title && <span className="truncate text-foreground/80">· {s.card.title}</span>}
          </span>
          <span className="shrink-0 text-[10px] text-success/80">Sent to agent</span>
        </div>
      ))}
      {open.map((s) => (
        <div key={s.card.id} className="flex flex-col gap-1">
          {isOpen(s.card) && ANSWERABLE.includes(s.card.kind) && (
            <div className="flex items-center justify-end">
              <Button
                variant="ghost"
                size="xs"
                title="Move this question to the Cards window and free the chat composer"
                onClick={() => void moveToWindow(s.card.id)}
              >
                <Icon name="external-link" size={11} />
                Hold &amp; open in Cards window
              </Button>
            </div>
          )}
          <CardView card={s.card} />
        </div>
      ))}
    </div>
  )
}

/** Cards from anything but the agent: pop-ups in the corner, and the one its sender is
 * waiting on in the middle. Mounted once, over the whole page. */
export function CardHost() {
  useEffect(startCards, [])
  // No pop-up for what is already in front (desk rule 9): a notice about the window the owner is
  // looking at says nothing they cannot see. Questions still pop up.
  const desk = useDeskState()
  const shown = desk.wins.find((w) => w.id === desk.focus)
  const inFront = shown?.key ? `${shown.module}:${shown.key}` : ""
  const toasts = useCards("toast").filter(
    (s) => isOpen(s.card) && !s.view.toastDismissed && (s.view.answerable || !inFront || s.card.subject !== inFront),
  )
  const blocking = useCards("modal").filter((s) => isOpen(s.card))
  const [hidden, setHidden] = useState<string | null>(null)
  // A question that arrives while the owner is typing does not take the screen: it waits behind
  // a small prompt until they choose to answer it.
  const [deferred, setDeferred] = useState<Set<string>>(new Set())
  const seenBlocking = useRef<Set<string>>(new Set())
  const blockingIds = blocking.map((s) => s.card.id).join(",")
  // biome-ignore lint/correctness/useExhaustiveDependencies: keyed on the ids of the questions
  useEffect(() => {
    const fresh = blocking.map((s) => s.card.id).filter((id) => !seenBlocking.current.has(id))
    for (const id of fresh) seenBlocking.current.add(id)
    if (fresh.length && isOwnerTyping()) setDeferred((d) => new Set([...d, ...fresh]))
  }, [blockingIds])
  const front = blocking.find((s) => s.card.id !== hidden && !deferred.has(s.card.id))
  const waiting = blocking.filter((s) => deferred.has(s.card.id))
  return (
    <>
      {toasts.length > 0 && (
        <div
          data-slot="card-toasts"
          className="pointer-events-none fixed bottom-3 left-3 z-40 flex w-[min(24rem,calc(100vw-1.5rem))] flex-col gap-2"
        >
          {toasts.slice(-4).map((s) => (
            <div key={s.card.id} className="pointer-events-auto relative shadow-lg">
              <CardView card={s.card} />
              {s.view.answerable && (
                <Button
                  variant="ghost"
                  size="xs"
                  className="absolute -top-2 -right-2 bg-background shadow-sm"
                  title="Hide for now. It stays in the Cards window."
                  onClick={() => dismissToast(s.card.id)}
                >
                  Later
                </Button>
              )}
            </div>
          ))}
        </div>
      )}
      {waiting.length > 0 && !front && (
        <div data-slot="card-waiting" className="fixed right-4 bottom-4 z-50">
          <Button size="sm" variant="outline" className="gap-2 shadow-md" onClick={() => setDeferred(new Set())}>
            <Icon name="message-circle-question-mark" size={14} />
            {waiting.length === 1 ? "A question is waiting · Answer" : `${waiting.length} questions waiting · Answer`}
          </Button>
        </div>
      )}
      <Dialog open={!!front} onOpenChange={(open) => !open && front && setHidden(front.card.id)}>
        <DialogContent className="max-w-md gap-0 p-0 ring-0" showCloseButton={false}>
          <DialogTitle className="sr-only">{front?.card.title ?? "A question"}</DialogTitle>
          <DialogDescription className="sr-only">Something is waiting on your answer.</DialogDescription>
          {front && <CardView card={front.card} />}
        </DialogContent>
      </Dialog>
    </>
  )
}

/** The Cards window: every open card, paginated wizard-style N at a time, and the recent ones that are closed. */
export function CardsWindow({ win }: ViewProps) {
  useEffect(startCards, [])
  const [all, setAll] = useState<ServerCard[]>([])
  const [page, setPage] = useState(0)
  const [submittingAll, setSubmittingAll] = useState(false)
  const [hadOpenOnMount, setHadOpenOnMount] = useState(false)
  const [confirmClear, setConfirmClear] = useState(false)
  const [showClosed, setShowClosed] = useState(false)
  const [clearing, setClearing] = useState(false)
  const pageSizeSetting = (useSetting<number>("surface.card_page_size") ?? 3) as number
  const pageSize = Math.max(1, pageSizeSetting)
  const displayMode = (useSetting<string>("surface.card_display") ?? "inline") as string

  const isAutoFromChat = win.args?.manual !== true && (win.args?.from_chat === true || wasOpenedFromChat())

  const refresh = useCallback(() => {
    void call<ServerCard[]>("GET", "/api/cards?all=1").then((list) => {
      const liveNow = list.filter(isLive)
      setAll(list)
      if (liveNow.length > 0) {
        setHadOpenOnMount(true)
      }
    })
  }, [])
  useEffect(refresh, [refresh])
  useEvent("cards.changed", refresh)
  // Newest first, so what an agent just added is at the top. Only open cards by default: the
  // answered and dismissed ones are a click away.
  const newestFirst = (a: ServerCard, b: ServerCard) => (b.created ?? "").localeCompare(a.created ?? "")
  const open = all.filter((c) => isLive(c) && !isClaimed(c)).sort(newestFirst)
  const closed = all.filter((c) => !isLive(c)).sort(newestFirst)

  // When Cards was opened automatically while in chat (or via "Hold & open in Cards" from chat),
  // submitting the last remaining question automatically closes the Cards window and returns focus.
  useEffect(() => {
    if (hadOpenOnMount && open.length === 0 && isAutoFromChat) {
      setOpenedFromChat(false)
      const timer = window.setTimeout(() => {
        void desk.close(win.id)
      }, 180)
      return () => window.clearTimeout(timer)
    }
  }, [hadOpenOnMount, open.length, isAutoFromChat, win.id])

  // Also ensure that when new held cards arrive from the agent, Cards window takes focus.
  useEvent<ServerCard>("cards.changed", (ev) => {
    const c = ev.data
    if (c && c.source === "agent" && ANSWERABLE.includes(c.kind) && lateOpen(c)) {
      if (isUserInChat()) setOpenedFromChat(true)
      if (!win.focused) {
        void desk.focus(win.id)
      }
    }
  })

  const totalPages = Math.max(1, Math.ceil(open.length / pageSize))
  const currentPage = Math.min(page, totalPages - 1)
  const visibleOpen = open.slice(currentPage * pageSize, (currentPage + 1) * pageSize)

  const readyOutcomes = open
    .map((c) => ({ card: c, outcome: outcomeFromDraft(c, loadDraft(c.id)) }))
    .filter(
      (x): x is { card: ServerCard; outcome: NonNullable<ReturnType<typeof outcomeFromDraft>> } => x.outcome !== null,
    )

  const submitBatch = async () => {
    if (readyOutcomes.length === 0 || submittingAll) return
    const willEmptyAll = readyOutcomes.length >= open.length
    setSubmittingAll(true)
    try {
      for (const item of readyOutcomes) {
        await respond(item.card.id, item.outcome)
      }
      refresh()
      if (willEmptyAll && isAutoFromChat) {
        setOpenedFromChat(false)
        void desk.close(win.id)
      }
    } finally {
      setSubmittingAll(false)
    }
  }

  // Clear all: dismiss every open card at once. An agent's question is closed as declined
  // (canceled); one its sender stopped waiting for takes no late answer after this.
  const clearAll = async () => {
    if (clearing) return
    setClearing(true)
    try {
      for (const c of open) {
        await respond(c.id, { status: "canceled" }).catch(() => {})
      }
      refresh()
      if (isAutoFromChat) {
        setOpenedFromChat(false)
        void desk.close(win.id)
      }
    } finally {
      setClearing(false)
      setConfirmClear(false)
    }
  }

  return (
    <div className="flex flex-col gap-3 p-4">
      <div className="flex flex-wrap items-center justify-between gap-2 border-b pb-3">
        <p className="text-xs text-muted-foreground">
          Questions here stay open while you chat with the agent or inspect other windows.
        </p>
        <div className="flex items-center gap-2 text-xs">
          <span className="text-muted-foreground">Default display</span>
          <Select value={displayMode} onValueChange={(v) => v && void setSetting("surface.card_display", v)}>
            <SelectTrigger size="sm" aria-label="Default card display">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="inline">inline</SelectItem>
              <SelectItem value="window">window</SelectItem>
            </SelectContent>
          </Select>
          <span className="text-muted-foreground">Show at a time</span>
          <Select
            value={String(pageSize)}
            onValueChange={(v) => v && void setSetting("surface.card_page_size", Number.parseInt(v, 10))}
          >
            <SelectTrigger size="sm" aria-label="Cards per step">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {[1, 2, 3, 5, 10].map((n) => (
                <SelectItem key={n} value={String(n)}>
                  {n}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </div>

      {all.length === 0 && (
        <Empty>
          <EmptyHeader>
            <EmptyTitle>No cards yet</EmptyTitle>
            <EmptyDescription>The agent and the app's modules show them as they need you.</EmptyDescription>
          </EmptyHeader>
        </Empty>
      )}

      {open.length > 0 && (
        <div className="flex flex-wrap items-center justify-between gap-2 rounded-md border bg-muted/30 px-3 py-2 text-xs">
          <div className="flex items-center gap-2">
            <Badge variant="secondary">{open.length} open</Badge>
            {totalPages > 1 && (
              <span className="text-muted-foreground">
                Step {currentPage + 1} of {totalPages} (showing {currentPage * pageSize + 1}–
                {Math.min((currentPage + 1) * pageSize, open.length)})
              </span>
            )}
          </div>
          <div className="flex items-center gap-1.5">
            {totalPages > 1 && (
              <>
                <Button
                  variant="outline"
                  size="xs"
                  disabled={currentPage === 0}
                  onClick={() => setPage((p) => Math.max(0, p - 1))}
                >
                  Previous
                </Button>
                <Button
                  variant="outline"
                  size="xs"
                  disabled={currentPage >= totalPages - 1}
                  onClick={() => setPage((p) => Math.min(totalPages - 1, p + 1))}
                >
                  Next
                </Button>
              </>
            )}
            {confirmClear ? (
              <>
                <Button size="xs" variant="destructive" disabled={clearing} onClick={() => void clearAll()}>
                  Dismiss all {open.length}
                </Button>
                <Button size="xs" variant="ghost" disabled={clearing} onClick={() => setConfirmClear(false)}>
                  Keep
                </Button>
              </>
            ) : (
              <Button
                size="xs"
                variant="ghost"
                title="Dismiss every open card; an agent's questions close as declined"
                onClick={() => setConfirmClear(true)}
              >
                Clear all
              </Button>
            )}
            {open.length > 1 && (
              <Button
                size="xs"
                disabled={readyOutcomes.length === 0 || submittingAll}
                title="Submit all cards you have filled in across every step"
                onClick={() => void submitBatch()}
              >
                Submit filled ({readyOutcomes.length}/{open.length})
              </Button>
            )}
          </div>
        </div>
      )}

      {visibleOpen.map((c) => (
        <Sourced key={c.id} card={c} />
      ))}

      {open.length === 0 && all.length > 0 && <p className="text-muted-foreground text-xs">Nothing waiting for you.</p>}
      {closed.length > 0 && (
        <div className="flex items-center justify-between pt-2">
          <h3 className="font-medium text-muted-foreground text-xs uppercase tracking-wider">
            Answered and dismissed ({closed.length})
          </h3>
          <Button size="xs" variant="ghost" onClick={() => setShowClosed((v) => !v)}>
            {showClosed ? "Hide" : "Show"}
          </Button>
        </div>
      )}
      {showClosed && closed.map((c) => <Sourced key={c.id} card={c} />)}
    </div>
  )
}

/** When a card arrived, short: the time today, else the date and time. */
function arrived(created?: string): string {
  if (!created) return ""
  const d = new Date(created)
  if (Number.isNaN(d.getTime())) return ""
  const time = d.toLocaleTimeString([], { hour: "numeric", minute: "2-digit" })
  return d.toDateString() === new Date().toDateString()
    ? time
    : `${d.toLocaleDateString([], { month: "short", day: "numeric" })} ${time}`
}

function Sourced({ card }: { card: ServerCard }) {
  // Who asked: the seat by name when an outside agent asked, else the app's own agent or module.
  const from = card.agent || card.source || "app"
  return (
    <div className="flex flex-col gap-1">
      <span className="flex items-center gap-1.5 text-muted-foreground text-xs">
        <Icon name={card.source === "agent" ? "bot" : "puzzle"} size={12} />
        <span className="font-medium text-foreground/80">{from}</span>
        {arrived(card.created) && <span>· {arrived(card.created)}</span>}
      </span>
      <CardView card={card} />
    </div>
  )
}
