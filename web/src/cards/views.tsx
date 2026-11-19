/** Where cards show: docked in the agent's panel, popped up over the desk, in the middle
 * of the screen when their sender is waiting, and all together in the Cards window. */
import { useCallback, useEffect, useState } from "react"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog"
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty"
import { useDeskState } from "../desk/store"
import { Icon, useEvent, type ViewProps } from "../sdk"
import { call } from "../sdk/runtime"
import { CardView } from "./renderers"
import { dismissToast, type ServerCard, startCards, useCards } from "./store"

const isOpen = (c: ServerCard) => !c.status

/** The agent's open cards, above its composer. A closed one leaves the dock: the
 * conversation carries what was answered. */
export function CardDock() {
  useEffect(startCards, [])
  const open = useCards("inline").filter((s) => isOpen(s.card))
  if (open.length === 0) return null
  return (
    <div
      data-slot="card-dock"
      className="flex max-h-[60%] shrink-0 flex-col gap-2 overflow-y-auto border-t p-2.5 *:shrink-0"
    >
      {open.map((s) => (
        <CardView key={s.card.id} card={s.card} />
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
  const front = blocking.find((s) => s.card.id !== hidden)
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

/** The Cards window: every open card, and the recent ones that are closed. */
export function CardsWindow(_: ViewProps) {
  useEffect(startCards, [])
  const [all, setAll] = useState<ServerCard[]>([])
  const refresh = useCallback(() => void call<ServerCard[]>("GET", "/api/cards?all=1").then(setAll), [])
  useEffect(refresh, [refresh])
  useEvent("cards.changed", refresh)
  const open = all.filter(isOpen)
  const closed = all.filter((c) => !isOpen(c)).reverse()
  return (
    <div className="flex flex-col gap-3 p-4">
      <p className="text-muted-foreground">
        Cards are what the app puts in front of you outside a window: a question, a request to approve, progress on
        work. The open ones wait here until they are answered.
      </p>
      {all.length === 0 && (
        <Empty>
          <EmptyHeader>
            <EmptyTitle>No cards yet</EmptyTitle>
            <EmptyDescription>The agent and the app's modules show them as they need you.</EmptyDescription>
          </EmptyHeader>
        </Empty>
      )}
      {open.map((c) => (
        <Sourced key={c.id} card={c} />
      ))}
      {closed.length > 0 && (
        <h3 className="pt-2 font-medium text-muted-foreground text-xs uppercase tracking-wider">Closed</h3>
      )}
      {closed.map((c) => (
        <Sourced key={c.id} card={c} />
      ))}
    </div>
  )
}

function Sourced({ card }: { card: ServerCard }) {
  return (
    <div className="flex flex-col gap-1">
      <span className="flex items-center gap-1.5 text-muted-foreground text-xs">
        <Icon name={card.source === "agent" ? "bot" : "puzzle"} size={12} />
        {card.source || "app"}
      </span>
      <CardView card={card} />
    </div>
  )
}
