/**
 * The Context window: why the agent got the context it got (docs/agent.md, "The context trace").
 * Now: what a message sent this moment would carry. History: every turn the trace kept, with the
 * window in front, the slots chosen and why, the tools brought to mind, the routed rule and
 * reflexes, what a budget cut, and what followed in the next 15 minutes. An owner's re-pin or move
 * soon after a turn is a correction: the guess was off.
 */
import { useCallback, useEffect, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty"
import { Item, ItemContent, ItemDescription, ItemTitle } from "@/components/ui/item"
import { Switch } from "@/components/ui/switch"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { Icon, useEvent, type ViewProps } from "../sdk"
import { call, setSetting } from "../sdk/runtime"

interface Signal {
  at: string
  kind: "pin" | "unpin" | "focus" | string
  actor?: string
  detail?: Record<string, unknown>
}
interface Turn {
  id: number
  at: string
  run_id?: string
  text: string
  view?: string
  module?: string
  mode?: string
  layout?: string
  open_views: string[]
  slot: string
  slots: string[]
  why?: string
  top_of_mind: string[]
  routed?: string
  fired: string[]
  halted?: boolean
  block_chars: number
  trimmed: string[]
  signals?: Signal[]
}
interface Preview {
  slot: string
  slots: { name: string; module?: string; about?: string }[]
  why: string
  top_of_mind: string[]
  focused?: { window?: string; view?: string; module?: string; title?: string; kind?: string; ref?: string }
  block: string
}

/** How a slot came to be chosen, in words. */
const WHY: Record<string, string> = {
  focus: "the window in front belongs to it",
  kept: "it stayed from before: the window in front has no slot of its own",
  pinned: "it was pinned",
  mode: "your mode chose it",
  default: "nothing else applied (the default)",
}
const when = (iso: string) => {
  const d = new Date(iso)
  return Number.isNaN(d.getTime())
    ? iso
    : d.toLocaleString([], { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit", second: "2-digit" })
}
/** The owner's own signal (not the agent's). */
const byOwner = (s: Signal) => s.actor === "owner" || s.actor === "" || s.actor === undefined
/** A correction: the owner re-pinned or unpinned a slot soon after the turn, so the slot the agent
 * chose was not the one they meant. A move to another window is shown but is weaker evidence (they
 * may only have gone to look at something). */
const correction = (s: Signal) => byOwner(s) && (s.kind === "pin" || s.kind === "unpin")

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="grid grid-cols-[9rem_1fr] gap-3 py-1.5 text-sm">
      <span className="text-muted-foreground text-xs">{label}</span>
      <div className="flex min-w-0 flex-wrap items-center gap-1">{children}</div>
    </div>
  )
}
const chips = (list: string[] | undefined, empty = "none") =>
  list && list.length > 0 ? (
    list.map((x) => (
      <Badge key={x} variant="outline" className="font-mono">
        {x}
      </Badge>
    ))
  ) : (
    <span className="text-muted-foreground text-xs">{empty}</span>
  )

function TurnDetail({ t }: { t: Turn }) {
  const corrections = (t.signals ?? []).filter(correction)
  return (
    <div className="flex flex-col gap-1 p-4">
      <p className="text-sm whitespace-pre-wrap">{t.text}</p>
      <span className="mb-2 text-xs text-muted-foreground">
        {when(t.at)}
        {t.run_id ? ` · run ${t.run_id}` : ""}
      </span>
      <Row label="In front">
        {t.view ? (
          <Badge variant="secondary">{t.view}</Badge>
        ) : (
          <span className="text-xs text-muted-foreground">nothing</span>
        )}
        {t.mode && <Badge variant="outline">mode {t.mode}</Badge>}
        {t.layout && <Badge variant="outline">{t.layout}</Badge>}
      </Row>
      <Row label="Open">{chips(t.open_views)}</Row>
      <Row label="Slot">
        <Badge>{t.slot || "none"}</Badge>
        <span className="text-xs text-muted-foreground">because {WHY[t.why ?? ""] ?? t.why ?? "?"}</span>
      </Row>
      <Row label="Slots offered">{chips(t.slots)}</Row>
      <Row label="Tools to mind">{chips(t.top_of_mind)}</Row>
      <Row label="Routed">
        {t.routed ? (
          <Badge variant="outline">{t.routed}</Badge>
        ) : (
          <span className="text-xs text-muted-foreground">no rule</span>
        )}
      </Row>
      <Row label="Reflexes">
        {chips(t.fired)}
        {t.halted && <Badge variant="destructive">halted the turn</Badge>}
      </Row>
      <Row label="Context size">
        <span className="font-mono text-xs">{t.block_chars.toLocaleString()} characters</span>
      </Row>
      <Row label="Budget cut">{chips(t.trimmed, "nothing cut")}</Row>
      <Row label="What followed">
        {(t.signals ?? []).length === 0 ? (
          <span className="text-xs text-muted-foreground">nothing in the next 15 minutes</span>
        ) : (
          <ul className="flex w-full flex-col gap-1">
            {(t.signals ?? []).map((s) => (
              <li
                key={`${s.at}-${s.kind}-${s.actor ?? ""}-${JSON.stringify(s.detail ?? {})}`}
                className="flex flex-wrap items-center gap-1.5 text-xs"
              >
                <span className="text-muted-foreground">{when(s.at)}</span>
                <Badge variant={correction(s) ? "secondary" : "outline"}>{s.kind}</Badge>
                <span className="text-muted-foreground">by {s.actor || "owner"}</span>
                {s.detail && <code className="font-mono text-muted-foreground">{JSON.stringify(s.detail)}</code>}
              </li>
            ))}
          </ul>
        )}
      </Row>
      {corrections.length > 0 && (
        <p className="mt-2 rounded-md border border-warning/40 bg-warning/10 px-3 py-2 text-xs">
          You re-pinned the context soon after this turn ({corrections.map((c) => c.kind).join(", ")}): the slot the
          agent chose was probably not the one you meant.
        </p>
      )}
    </div>
  )
}

function Now() {
  const [p, setP] = useState<Preview | null>(null)
  const [error, setError] = useState("")
  const load = useCallback(() => {
    call<Preview>("GET", "/api/agent/context")
      .then((x) => {
        setP(x)
        setError("")
      })
      .catch((e: Error) => setError(e.message))
  }, [])
  useEffect(load, [load])
  useEvent("desk.changed", load)
  if (error) return <p className="p-4 text-xs text-destructive">{error}</p>
  if (!p) return <p className="p-4 text-xs text-muted-foreground">Reading…</p>
  return (
    <div className="flex flex-col gap-1 p-4">
      <p className="text-xs text-muted-foreground">
        What a message sent now would carry. It follows the window in front.
      </p>
      <Row label="In front">
        {p.focused?.view ? (
          <>
            <Badge variant="secondary">{p.focused.view}</Badge>
            <span className="truncate text-xs">{p.focused.title}</span>
            {p.focused.ref && (
              <Badge variant="outline" className="font-mono">
                {p.focused.ref}
              </Badge>
            )}
          </>
        ) : (
          <span className="text-xs text-muted-foreground">nothing</span>
        )}
      </Row>
      <Row label="Slot">
        <Badge>{p.slot || "none"}</Badge>
        <span className="text-xs text-muted-foreground">because {WHY[p.why] ?? p.why}</span>
      </Row>
      <Row label="Slots offered">{chips(p.slots.map((s) => s.name))}</Row>
      <Row label="Tools to mind">{chips(p.top_of_mind)}</Row>
      <Row label="The block">
        <span className="font-mono text-xs">{p.block.length.toLocaleString()} characters</span>
      </Row>
      <pre className="mt-2 max-h-[50vh] overflow-auto rounded-md border bg-muted/40 p-3 font-mono text-xs whitespace-pre-wrap">
        {p.block}
      </pre>
    </div>
  )
}

function History() {
  const [turns, setTurns] = useState<Turn[] | null>(null)
  const [on, setOn] = useState(true)
  const [open, setOpen] = useState<number | null>(null)
  const [error, setError] = useState("")
  const load = useCallback(() => {
    call<{ on: boolean; turns: Turn[] }>("GET", "/api/agent/trace?limit=200")
      .then((r) => {
        setTurns(r.turns ?? [])
        setOn(r.on)
        setError("")
      })
      .catch((e: Error) => setError(e.message))
  }, [])
  useEffect(load, [load])
  const shown = turns?.find((t) => t.id === open) ?? turns?.[0]
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex shrink-0 flex-wrap items-center gap-3 border-b px-3 py-2 text-xs">
        <span className="flex items-center gap-2">
          <Switch
            checked={on}
            onCheckedChange={(v) =>
              void setSetting("surface.agent_trace", v)
                .then(() => setOn(v))
                .catch((e: Error) => setError(e.message))
            }
            aria-label="Keep the trace"
          />
          Keep the trace
        </span>
        <span className="text-muted-foreground">On this machine only; the newest 50,000 turns.</span>
        <div className="ml-auto flex items-center gap-1">
          <Button size="sm" variant="ghost" aria-label="Read the trace again" onClick={load}>
            <Icon name="refresh-cw" />
          </Button>
          <Button
            size="sm"
            variant="outline"
            render={<a href="/api/agent/trace/export" download="context-trace.jsonl" />}
          >
            <Icon name="download" />
            Export
          </Button>
        </div>
      </div>
      {error && <p className="border-b px-3 py-1 text-xs text-destructive">{error}</p>}
      {turns?.length === 0 ? (
        <Empty className="h-full">
          <EmptyHeader>
            <EmptyTitle>No turns yet</EmptyTitle>
            <EmptyDescription>Each message to the agent leaves a record here.</EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <div className="flex min-h-0 flex-1">
          <div className="flex w-80 shrink-0 flex-col gap-0.5 overflow-auto border-r p-1.5 @max-3xl:w-56">
            {turns?.map((t) => {
              const fixed = (t.signals ?? []).some(correction)
              return (
                <Item
                  key={t.id}
                  size="sm"
                  variant={shown?.id === t.id ? "muted" : "default"}
                  render={<a href={`#turn-${t.id}`} />}
                  onClick={(e) => {
                    e.preventDefault()
                    setOpen(t.id)
                  }}
                >
                  <ItemContent>
                    <ItemTitle className="w-full">
                      <Badge variant="outline" className="font-mono">
                        {t.slot || "none"}
                      </Badge>
                      {fixed && (
                        <span title="You re-pinned the context soon after this turn">
                          <Icon name="corner-down-left" size={12} />
                        </span>
                      )}
                      {t.halted && <Badge variant="destructive">halted</Badge>}
                      <span className="ml-auto shrink-0 font-normal text-muted-foreground text-xs">{when(t.at)}</span>
                    </ItemTitle>
                    <ItemDescription className="truncate">{t.text}</ItemDescription>
                  </ItemContent>
                </Item>
              )
            })}
          </div>
          <div className="min-w-0 flex-1 overflow-auto">{shown && <TurnDetail t={shown} />}</div>
        </div>
      )}
    </div>
  )
}

export function ContextInspector(_: ViewProps) {
  const [tab, setTab] = useState<"now" | "history">("now")
  return (
    <div className="@container flex h-full min-h-0 flex-col bg-background">
      <div className="flex shrink-0 items-center gap-2 border-b px-3 py-2">
        <span className="text-sm font-semibold">Context</span>
        <ToggleGroup
          size="sm"
          spacing={0}
          value={[tab]}
          onValueChange={(v: string[]) => v[0] && setTab(v[0] as "now" | "history")}
        >
          <ToggleGroupItem value="now">Now</ToggleGroupItem>
          <ToggleGroupItem value="history">History</ToggleGroupItem>
        </ToggleGroup>
        <span className="text-xs text-muted-foreground">Why the agent gets the context it gets.</span>
      </div>
      {tab === "now" ? (
        <div className="min-h-0 flex-1 overflow-auto">
          <Now />
        </div>
      ) : (
        <History />
      )}
    </div>
  )
}
