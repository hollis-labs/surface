/**
 * Managing the roster: a seat's menu (pin, hide, details), its details (what it runs with,
 * its archived sessions to restore or delete), a session's menu (rename, archive), and the
 * session header's usage strip and ask-before-tools switch.
 *
 * Seats are defined in agent-system (rendered boot dirs); this only shows them. Sessions are
 * the seat host's: everything here is its routes.
 */
import { Icon, type ModuleAPI } from "@hollis-labs/surface-page/sdk"
import { type FormEvent, useCallback, useEffect, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"

export interface SessionInfo {
  id: string
  seat: string
  title?: string
  created_at: string
  state: string
  ask?: boolean
  archived?: boolean
}

export interface SeatInfo {
  seat: string
  name: string
  summary?: string
  shape: "continuity" | "concurrent"
  max_sessions: number
  adapter: string
  model?: string
  effort?: string
  version?: string
  lock?: { repo_sha?: string; [k: string]: unknown }
  mcp_servers?: string[]
  surface_tools?: string[]
  dir?: string
  archived?: number
  defined_name?: string
}

const enc = encodeURIComponent

/** The "…" menu of a seat in the roster. */
export function SeatMenu({
  seat,
  pinned,
  hidden,
  onPin,
  onHide,
  onDetails,
  onRename,
}: {
  seat: SeatInfo
  pinned: boolean
  hidden: boolean
  onPin: () => void
  onHide: () => void
  onDetails: () => void
  onRename: () => void
}) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button variant="ghost" size="icon-xs" title={`${seat.name}: pin, hide, details`}>
            <Icon name="ellipsis" size={12} />
          </Button>
        }
      />
      <DropdownMenuContent align="end">
        <DropdownMenuItem onClick={onRename}>
          <Icon name="pencil" size={13} /> Rename
        </DropdownMenuItem>
        <DropdownMenuItem onClick={onPin}>
          <Icon name={pinned ? "pin-off" : "pin"} size={13} /> {pinned ? "Unpin" : "Pin to the top"}
        </DropdownMenuItem>
        <DropdownMenuItem onClick={onHide}>
          <Icon name={hidden ? "eye" : "eye-off"} size={13} /> {hidden ? "Show in the roster" : "Hide from the roster"}
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem onClick={onDetails}>
          <Icon name="info" size={13} /> Details and archived sessions
          {seat.archived ? ` (${seat.archived})` : ""}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

/** A seat's details, and its archived sessions to restore or delete. */
export function SeatDetails({
  api,
  seat,
  open,
  onOpenChange,
  onChanged,
  onSelect,
}: {
  api: ModuleAPI
  seat: SeatInfo | null
  open: boolean
  onOpenChange: (open: boolean) => void
  onChanged: () => Promise<void>
  onSelect: (id: string) => void
}) {
  const [archived, setArchived] = useState<SessionInfo[]>([])
  const [error, setError] = useState("")
  const [confirm, setConfirm] = useState("")
  const load = useCallback(async () => {
    if (!seat) return
    const r = await api
      .get<{ sessions: SessionInfo[] }>(`h/seats/${enc(seat.seat)}/archived`)
      .catch(() => ({ sessions: [] }))
    setArchived(r.sessions)
  }, [api, seat])
  useEffect(() => {
    if (open) void load()
  }, [open, load])
  if (!seat) return null
  const act = async (fn: () => Promise<unknown>) => {
    setError("")
    try {
      await fn()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
    setConfirm("")
    await load()
    await onChanged()
  }
  const rows: [string, string | undefined][] = [
    ["Defined as", seat.defined_name ? `${seat.defined_name} (${seat.seat})` : seat.seat],
    ["Shape", seat.shape === "continuity" ? "continuity (one long-running session)" : "concurrent (several sessions)"],
    ["Runtime", [seat.adapter, seat.model, seat.effort].filter(Boolean).join(" · ")],
    ["Boot commit", seat.lock?.repo_sha ? String(seat.lock.repo_sha).slice(0, 12) : undefined],
    ["Version", seat.version],
    ["MCP servers", seat.mcp_servers?.join(", ")],
    ["Surface tools", seat.surface_tools?.join(", ")],
    ["Boot dir", seat.dir],
  ]
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[85vh] w-[min(40rem,95vw)] max-w-none flex-col gap-3 sm:max-w-none">
        <DialogHeader>
          <DialogTitle>{seat.name}</DialogTitle>
          <DialogDescription>{seat.summary || "A seat defined in agent-system."}</DialogDescription>
        </DialogHeader>
        <dl className="grid grid-cols-[8rem_1fr] gap-x-3 gap-y-1 text-xs">
          {rows
            .filter(([, v]) => v)
            .map(([k, v]) => (
              <div key={k} className="contents">
                <dt className="text-[var(--color-muted-foreground)]">{k}</dt>
                <dd className="min-w-0 break-words font-mono">{v}</dd>
              </div>
            ))}
        </dl>
        <h3 className="border-b border-[var(--color-border)] pb-1 text-xs font-medium">
          Archived sessions ({archived.length})
        </h3>
        <div className="flex min-h-0 flex-1 flex-col gap-1 overflow-y-auto">
          {archived.length === 0 && <p className="text-xs text-[var(--color-muted-foreground)]">None.</p>}
          {archived.map((s) => (
            <div
              key={s.id}
              className="flex items-center gap-2 rounded border border-[var(--color-border)] px-2 py-1 text-xs"
            >
              <span className="min-w-0 flex-1 truncate">{s.title || "Untitled session"}</span>
              <span className="shrink-0 text-[var(--color-muted-foreground)]">
                {new Date(s.created_at).toLocaleDateString()}
              </span>
              <Button
                size="xs"
                variant="outline"
                onClick={() =>
                  void act(async () => {
                    await api.post(`h/sessions/${enc(s.id)}/restore`)
                    onSelect(s.id)
                    onOpenChange(false)
                  })
                }
              >
                Restore
              </Button>
              {confirm === s.id ? (
                <Button
                  size="xs"
                  variant="destructive"
                  onClick={() => void act(() => api.del(`h/sessions/${enc(s.id)}?purge=1`))}
                >
                  Delete for good
                </Button>
              ) : (
                <Button size="xs" variant="ghost" title="Delete this session" onClick={() => setConfirm(s.id)}>
                  <Icon name="trash" size={12} />
                </Button>
              )}
            </div>
          ))}
        </div>
        {error && <p className="text-xs text-red-500">{error}</p>}
      </DialogContent>
    </Dialog>
  )
}

/** A session's "…" menu: rename, and archive (concurrent seats; a continuity seat resets). */
export function SessionMenu({
  canArchive,
  onRename,
  onArchive,
}: {
  canArchive: boolean
  onRename: () => void
  onArchive: () => void
}) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button variant="ghost" size="icon-xs" title="Rename or archive">
            <Icon name="ellipsis" size={12} />
          </Button>
        }
      />
      <DropdownMenuContent align="end">
        <DropdownMenuItem onClick={onRename}>
          <Icon name="pencil" size={13} /> Rename
        </DropdownMenuItem>
        {canArchive && (
          <DropdownMenuItem onClick={onArchive}>
            <Icon name="archive" size={13} /> Archive
          </DropdownMenuItem>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

/** An inline title editor: Enter saves, Escape cancels. */
export function RenameField({
  value,
  onSave,
  onCancel,
}: {
  value: string
  onSave: (title: string) => void
  onCancel: () => void
}) {
  const [draft, setDraft] = useState(value)
  const submit = (e: FormEvent) => {
    e.preventDefault()
    onSave(draft.trim())
  }
  return (
    <form className="flex min-w-0 flex-1 items-center gap-1" onSubmit={submit}>
      <Input
        autoFocus
        value={draft}
        aria-label="Session title"
        className="h-6 text-xs"
        onChange={(e) => setDraft(e.target.value)}
        onKeyDown={(e) => e.key === "Escape" && onCancel()}
        onBlur={onCancel}
      />
    </form>
  )
}

interface Usage {
  turns: number
  cost: number | null
  turn_cost: number | null
  currency?: string
  context_used?: number
  context_size?: number
  input_tokens: number
  output_tokens: number
  cache_read_tokens: number
  cache_write_tokens: number
}

const money = (n: number | null | undefined) => (n == null ? "–" : `$${n < 1 ? n.toFixed(3) : n.toFixed(2)}`)
const tokens = (n: number) => (n >= 1e6 ? `${(n / 1e6).toFixed(1)}M` : n >= 1e3 ? `${Math.round(n / 1e3)}k` : String(n))

/** What the session has cost and how full its context is, refreshed after each turn. */
export function UsageStrip({ api, session, refresh }: { api: ModuleAPI; session: string; refresh: number }) {
  const [u, setU] = useState<Usage | null>(null)
  // biome-ignore lint/correctness/useExhaustiveDependencies: refresh is the signal to read again
  useEffect(() => {
    void api
      .get<Usage>(`h/sessions/${enc(session)}/usage`)
      .then(setU)
      .catch(() => setU(null))
  }, [api, session, refresh])
  if (!u || u.turns === 0) return null
  const pct = u.context_size ? Math.round(((u.context_used ?? 0) / u.context_size) * 100) : null
  return (
    <div
      className="flex flex-wrap items-center gap-x-3 gap-y-0.5 text-[11px] text-[var(--color-muted-foreground)]"
      title={`Tokens over ${u.turns} turns: ${tokens(u.input_tokens)} in, ${tokens(u.output_tokens)} out, ${tokens(u.cache_read_tokens)} cache read, ${tokens(u.cache_write_tokens)} cache write`}
    >
      <span>
        <Icon name="coins" size={11} /> {money(u.cost)} session
      </span>
      <span>last turn {money(u.turn_cost)}</span>
      {pct !== null && (
        <span>
          context {pct}% ({tokens(u.context_used ?? 0)}/{tokens(u.context_size ?? 0)})
        </span>
      )}
      <span>{u.turns} turns</span>
    </div>
  )
}

/** Ask before tools: the agent asks through the approvals queue instead of running freely. */
export function AskSwitch({
  api,
  session,
  ask,
  onChanged,
}: {
  api: ModuleAPI
  session: string
  ask: boolean
  onChanged: () => Promise<void>
}) {
  const [busy, setBusy] = useState(false)
  return (
    <span
      className="flex items-center gap-1.5 text-xs"
      title="Ask before using tools (answered in the approvals queue)"
    >
      <Switch
        checked={ask}
        disabled={busy}
        aria-label="Ask before tools"
        onCheckedChange={(v) => {
          setBusy(true)
          void fetchPatch(api, session, { ask: v }).finally(async () => {
            setBusy(false)
            await onChanged()
          })
        }}
      />
      <Badge variant={ask ? "secondary" : "outline"} className="text-[10px]">
        {ask ? "asks" : "auto"}
      </Badge>
    </span>
  )
}

/** PATCH a session (the module API has no patch verb: the URL is the module's own route). */
export async function fetchPatch(api: ModuleAPI, session: string, body: Record<string, unknown>) {
  const res = await fetch(api.url(`h/sessions/${enc(session)}`), {
    method: "PATCH",
    credentials: "same-origin",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  })
  if (!res.ok) {
    const b = (await res.json().catch(() => ({}))) as { message?: string }
    throw new Error(b.message || `The seat host answered ${res.status}.`)
  }
}
