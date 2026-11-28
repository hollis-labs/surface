/**
 * The approvals queue: every agent's pending approvals in one modal, wherever the owner is.
 *
 * Two sources: the seat host's tool approvals (a seat asking to run a tool: GET /approvals,
 * answered with POST /approvals/answer), and the Surface app's MCP approval-bridge cards (a
 * seat asking to use one of the app's write tools: cards whose subject starts "mcp:"), which
 * this queue claims from the Cards window.
 *
 * It opens by itself when something new needs the owner (unless snoozed or switched off in
 * the settings) and closes when the queue is empty. Bulk: approve all, approve all for the
 * session, deny all, dismiss all (hide what is there now without answering: each stays in its
 * own chat).
 */
import {
  claimCards,
  Icon,
  isOwnerTyping,
  type ModuleAPI,
  type ServerCard,
  useEvent,
  useSetting,
} from "@hollis-labs/surface-page/sdk"
import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { setForwardSeats } from "./forward"

const POLL_MS = 3000
const DISMISSED_KEY = "chat.approvals.dismissed"

interface Grant {
  caller: string
  tool: string
  granted_at: string
}
const isBridge = (c: ServerCard) => !c.status && (c.subject ?? "").startsWith("mcp:")

interface SeatApproval {
  session: string
  seat: string
  seat_name: string
  title?: string
  conversation_id: string
  approval_id: string
  tool: string
  description?: string
  input?: unknown
  standing: boolean
  expires_at?: string
}

/** One row of the queue, whichever source it came from. */
interface Item {
  key: string
  from: string
  what: string
  detail?: string
  input?: unknown
  standing: boolean
  risk?: string
  seat?: SeatApproval
  card?: ServerCard
}

type Answer = "once" | "session" | "deny"

function fromSeat(a: SeatApproval, names: Record<string, string>): Item {
  const seat = names[a.seat] || a.seat_name
  return {
    key: `seat:${a.conversation_id}:${a.approval_id}`,
    from: a.title ? `${seat} · ${a.title}` : seat,
    what: a.tool || "a tool call",
    detail: a.description,
    input: a.input,
    standing: a.standing,
    seat: a,
  }
}

function fromCard(c: ServerCard): Item {
  const p = (c.payload ?? {}) as { question?: string; summary?: string; body?: string }
  return {
    key: `card:${c.id}`,
    from: c.agent || "an agent",
    what: c.title || "Use a tool of this app",
    detail: p.summary || p.question || p.body,
    standing: c.kind === "choice",
    risk: c.risk,
    card: c,
  }
}

async function answerCard(c: ServerCard, a: Answer) {
  const outcome =
    c.kind === "approval"
      ? { status: "submitted", decisions: [{ action: a === "deny" ? "reject" : "approve" }] }
      : {
          status: "submitted",
          answers: [{ questionId: "choice", value: a === "deny" ? "deny" : a === "session" ? "always" : "once" }],
        }
  await fetch(`/api/cards/${encodeURIComponent(c.id)}/response`, {
    method: "POST",
    credentials: "same-origin",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(outcome),
  })
}

export function ApprovalsOverlay({ api }: { api: ModuleAPI }) {
  const autoOpen = useSetting<boolean>("chat.approvals_auto_open") ?? true
  const snoozeMin = useSetting<number>("chat.approvals_snooze_minutes") ?? 10
  const names = (useSetting<Record<string, string>>("chat.seat_names") ?? {}) as Record<string, string>
  const namesRef = useRef(names)
  namesRef.current = names
  const [seatItems, setSeatItems] = useState<SeatApproval[]>([])
  const [cards, setCards] = useState<ServerCard[]>([])
  const [open, setOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  // Dismissed rows stay hidden across reloads (this browser), until answered elsewhere.
  const [dismissed, setDismissedState] = useState<Set<string>>(() => {
    try {
      return new Set(JSON.parse(localStorage.getItem(DISMISSED_KEY) ?? "[]") as string[])
    } catch {
      return new Set()
    }
  })
  const setDismissed = (fn: (d: Set<string>) => Set<string>) =>
    setDismissedState((d) => {
      const next = fn(d)
      try {
        localStorage.setItem(DISMISSED_KEY, JSON.stringify([...next].slice(-500)))
      } catch {
        // storage full or off: dismissing still works for this page
      }
      return next
    })
  const [grants, setGrants] = useState<Grant[]>([])
  const [showGrants, setShowGrants] = useState(false)
  const loadGrants = useCallback(async () => {
    const r = await fetch("/api/mcp/grants", { credentials: "same-origin" }).catch(() => null)
    if (r?.ok) setGrants((await r.json()) as Grant[])
  }, [])
  const revoke = async (g: Grant) => {
    await fetch(`/api/mcp/grants?caller=${encodeURIComponent(g.caller)}&tool=${encodeURIComponent(g.tool)}`, {
      method: "DELETE",
      credentials: "same-origin",
    }).catch(() => {})
    await loadGrants()
  }
  const snoozedUntil = useRef(0)
  const seen = useRef<Set<string>>(new Set())

  useEffect(() => claimCards("chat.approvals", isBridge), [])

  const loadSeats = useCallback(async () => {
    try {
      const r = await api.get<{ approvals: SeatApproval[] }>("h/approvals")
      setSeatItems(r.approvals ?? [])
    } catch {
      // The seat host is away: the queue shows what it has.
    }
  }, [api])
  const loadCards = useCallback(async () => {
    const r = await fetch("/api/cards", { credentials: "same-origin" }).catch(() => null)
    if (r?.ok) setCards(((await r.json()) as ServerCard[]).filter(isBridge))
  }, [])

  useEffect(() => {
    void loadSeats()
    void loadCards()
    const t = setInterval(() => {
      if (document.visibilityState === "visible") void loadSeats()
    }, POLL_MS)
    return () => clearInterval(t)
  }, [loadSeats, loadCards])
  useEvent("cards.changed", () => void loadCards())

  // The seats Forward offers, kept fresh from here: this overlay is on every page.
  useEffect(() => {
    const read = () =>
      void api
        .get<{ seats: { seat: string; name: string }[] }>("h/seats")
        .then((r) =>
          setForwardSeats((r.seats ?? []).map((s) => ({ seat: s.seat, name: namesRef.current[s.seat] || s.name }))),
        )
        .catch(() => {})
    read()
    const t = setInterval(read, 30_000)
    return () => clearInterval(t)
  }, [api])

  const items = useMemo(
    () => [...seatItems.map((a) => fromSeat(a, names)), ...cards.map(fromCard)].filter((i) => !dismissed.has(i.key)),
    [seatItems, cards, dismissed, names],
  )

  // Open for something new; close when nothing is left.
  useEffect(() => {
    const fresh = items.some((i) => !seen.current.has(i.key))
    for (const i of items) seen.current.add(i.key)
    if (items.length === 0) {
      setOpen(false)
      return
    }
    // Not over what the owner is typing: then the button below asks instead.
    if (fresh && autoOpen && Date.now() >= snoozedUntil.current && !isOwnerTyping()) setOpen(true)
  }, [items, autoOpen])

  const answer = useCallback(
    async (list: Item[], a: Answer) => {
      setBusy(true)
      setError("")
      try {
        const seat = list.filter((i) => i.seat).map((i) => i.seat as SeatApproval)
        if (seat.length) {
          const r = await api.post<{ results: { approval_id: string; error?: string }[] }>("h/approvals/answer", {
            approvals: seat.map((s) => ({ conversation_id: s.conversation_id, approval_id: s.approval_id })),
            decision: a === "deny" ? "deny" : "allow",
            scope: a === "session" ? "conversation" : "once",
          })
          const failed = r.results.filter((x) => x.error)
          if (failed.length) setError(failed.map((x) => x.error).join("; "))
        }
        for (const i of list) if (i.card) await answerCard(i.card, a)
      } catch (err) {
        setError(err instanceof Error ? err.message : "The answer did not reach the agent.")
      } finally {
        setBusy(false)
        await Promise.all([loadSeats(), loadCards()])
      }
    },
    [api, loadSeats, loadCards],
  )

  const snooze = () => {
    snoozedUntil.current = Date.now() + Math.max(1, snoozeMin) * 60_000
    setOpen(false)
  }

  if (items.length === 0) return null
  return (
    <>
      {!open && (
        // In the empty middle of the top bar: clear of the windows' title bars and the composers.
        <div className="fixed top-1 left-1/2 z-50 -translate-x-[calc(100%+0.25rem)]">
          <Button size="xs" variant="outline" className="h-6 gap-1.5 bg-card" onClick={() => setOpen(true)}>
            <Icon name="shield-question-mark" size={12} />
            {items.length === 1 ? "1 approval waiting" : `${items.length} approvals waiting`}
          </Button>
        </div>
      )}
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="flex max-h-[85vh] w-[min(44rem,95vw)] max-w-none flex-col gap-3 sm:max-w-none">
          <DialogHeader>
            <DialogTitle>Approvals ({items.length})</DialogTitle>
            <DialogDescription>
              Agents waiting for your go-ahead. Answer one at a time or all at once.
            </DialogDescription>
          </DialogHeader>
          <div className="flex min-h-0 flex-1 flex-col gap-2 overflow-y-auto pr-1">
            {items.map((i) => (
              <Row key={i.key} item={i} busy={busy} onAnswer={(a) => void answer([i], a)} />
            ))}
          </div>
          {error && <p className="text-xs text-red-500">{error}</p>}
          <div className="flex flex-col gap-1 border-t border-[var(--color-border)] pt-2">
            <Button
              size="xs"
              variant="ghost"
              className="self-start"
              onClick={() => {
                if (!showGrants) void loadGrants()
                setShowGrants((v) => !v)
              }}
            >
              <Icon name={showGrants ? "chevron-down" : "chevron-right"} size={12} /> Always allowed
            </Button>
            {showGrants && grants.length === 0 && (
              <p className="px-2 text-xs text-[var(--color-muted-foreground)]">No standing approvals.</p>
            )}
            {showGrants &&
              grants.map((g) => (
                <div key={`${g.caller}/${g.tool}`} className="flex items-center gap-2 px-2 text-xs">
                  <span className="min-w-0 flex-1 truncate">
                    <span className="font-medium">{g.caller}</span> · <span className="font-mono">{g.tool}</span>
                  </span>
                  <Button size="xs" variant="ghost" onClick={() => void revoke(g)}>
                    Revoke
                  </Button>
                </div>
              ))}
          </div>
          <DialogFooter className="flex flex-wrap items-center gap-1.5 sm:justify-between">
            <div className="flex gap-1.5">
              <Button
                size="sm"
                variant="ghost"
                disabled={busy}
                onClick={snooze}
                title={`Close and stay closed for ${snoozeMin} minutes`}
              >
                <Icon name="alarm-clock" size={13} /> Snooze {snoozeMin}m
              </Button>
              <Button
                size="sm"
                variant="ghost"
                disabled={busy}
                title="Hide these without answering; each stays in its own chat"
                onClick={() => setDismissed((d) => new Set([...d, ...items.map((i) => i.key)]))}
              >
                Dismiss all
              </Button>
            </div>
            <div className="flex gap-1.5">
              <Button size="sm" variant="outline" disabled={busy} onClick={() => void answer(items, "deny")}>
                Deny all
              </Button>
              <Button size="sm" variant="outline" disabled={busy} onClick={() => void answer(items, "session")}>
                Approve all for session
              </Button>
              <Button size="sm" disabled={busy} onClick={() => void answer(items, "once")}>
                Approve all
              </Button>
            </div>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}

function Row({ item, busy, onAnswer }: { item: Item; busy: boolean; onAnswer: (a: Answer) => void }) {
  const [show, setShow] = useState(false)
  const input = item.input === undefined ? "" : JSON.stringify(item.input, null, 2)
  return (
    <div className="flex flex-col gap-1.5 rounded border border-[var(--color-border)] p-2 text-sm">
      <div className="flex items-start gap-2">
        <Icon name="bot" size={14} className="mt-0.5 shrink-0" />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-1.5">
            <span className="font-medium">{item.from}</span>
            <span className="text-[var(--color-muted-foreground)]">wants</span>
            <span className="font-mono text-xs">{item.what}</span>
            {item.risk && (
              <Badge variant={item.risk === "high" ? "destructive" : "outline"} className="text-[10px]">
                {item.risk} risk
              </Badge>
            )}
          </div>
          {item.detail && <p className="mt-0.5 text-xs text-[var(--color-muted-foreground)]">{item.detail}</p>}
        </div>
      </div>
      {show && input && (
        <pre className="max-h-48 overflow-auto rounded bg-[var(--color-muted)] p-2 text-[11px] leading-snug">
          {input}
        </pre>
      )}
      <div className="flex flex-wrap items-center justify-end gap-1.5">
        {input && (
          <Button size="xs" variant="ghost" onClick={() => setShow((v) => !v)}>
            {show ? "Hide input" : "Show input"}
          </Button>
        )}
        <Button size="xs" variant="ghost" disabled={busy} onClick={() => onAnswer("deny")}>
          Deny
        </Button>
        {item.standing && (
          <Button size="xs" variant="outline" disabled={busy} onClick={() => onAnswer("session")}>
            Allow for session
          </Button>
        )}
        <Button size="xs" disabled={busy} onClick={() => onAnswer("once")}>
          Allow
        </Button>
      </div>
    </div>
  )
}
