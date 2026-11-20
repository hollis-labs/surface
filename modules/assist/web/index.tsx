/**
 * Done for you: what the agent did in the owner's name (receipts, replies, sends, settled
 * mail, the owner's own answers on cards, rules), by day; and how the owner's mail is sorted
 * now (what waits on them, what was handed to the agent, what is held). Rules can be removed.
 */
import { CiteRef, Icon, type ModuleWeb, useEvent, type ViewProps } from "@hollis-labs/surface-page/sdk"
import { useCallback, useEffect, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty"
import { Item, ItemContent, ItemDescription, ItemTitle } from "@/components/ui/item"
import { Markdown } from "@/components/ui/markdown"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"

interface Entry {
  id: number
  at: string
  kind: string
  summary: string
  with?: string
  asked: boolean
}

interface Mail {
  id: string
  from: string
  from_name?: string
  kind: string
  subject: string
  at: string
  why?: string
}

interface Rule {
  id: number
  from?: string
  kind?: string
  subject_has?: string
  class: string
  note?: string
}

interface SentMessage {
  id: string
  kind: string
  subject: string
  to: string
  at: string
  task_id?: string
  delivery: string
  request?: string
  alert?: string
  events: { id: string; at: string; kind: string; state?: string; detail?: string; task_id?: string }[]
}
interface SentHistory {
  messages: SentMessage[]
  warning: string
}

type Tab = "done" | "sent" | "mail" | "rules"

const receiptLabel = (kind: string, state?: string) => {
  if (kind === "request.unacknowledged_alert") return "Not acknowledged"
  if (kind === "routing.reply_undeliverable") return "Reply delivery failed"
  if (state === "consumed") return "Taken from mailbox"
  return state || "Delivery update"
}

const day = (at: string) =>
  new Date(at).toLocaleDateString(undefined, { weekday: "long", month: "short", day: "numeric" })
const time = (at: string) => new Date(at).toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" })

const classes: [string, string][] = [
  ["needs", "Waiting on you"],
  ["answer", "Answers that came back"],
  ["bas", "Handed to the agent"],
  ["digest", "Held for you"],
]

function DoneForYou({ win, desk, api }: ViewProps) {
  const [tab, setTab] = useState<Tab>("done")
  const [entries, setEntries] = useState<Entry[] | null>(null)
  const [mail, setMail] = useState<Record<string, Mail[]> | null>(null)
  const [rules, setRules] = useState<Rule[] | null>(null)
  const [sent, setSent] = useState<SentHistory | null>(null)
  const [error, setError] = useState("")

  const load = useCallback(() => {
    const fail = (e: Error) => setError(e.message)
    void api.get<Entry[]>("activity").then(setEntries).catch(fail)
    void api.get<SentHistory>("sent").then(setSent).catch(fail)
    void api.get<Record<string, Mail[]>>("mail").then(setMail).catch(fail)
    void api.get<Rule[]>("rules").then(setRules).catch(fail)
  }, [api])
  useEffect(load, [load])
  useEvent("assist.", load)
  useEvent("cards.changed", load)
  useEffect(() => desk.report(win.id, { kind: "data", ref: "assist:activity" }), [desk, win.id])

  const drop = (id: number) =>
    void api
      .del(`rules/${id}`)
      .then(load)
      .catch((e: Error) => setError(e.message))

  let lastDay = ""
  return (
    <div className="flex h-full min-h-0 flex-col bg-background">
      <div className="flex shrink-0 items-center gap-2 border-b px-2 py-1">
        <ToggleGroup size="sm" spacing={0} value={[tab]} onValueChange={(v: string[]) => v[0] && setTab(v[0] as Tab)}>
          <ToggleGroupItem value="done">Done for you</ToggleGroupItem>
          <ToggleGroupItem value="sent">
            Sent {sent?.messages.some((m) => m.alert) && "· needs attention"}
          </ToggleGroupItem>
          <ToggleGroupItem value="mail">Your mail</ToggleGroupItem>
          <ToggleGroupItem value="rules">Rules</ToggleGroupItem>
        </ToggleGroup>
        {error && <span className="text-xs text-destructive">{error}</span>}
        <Button className="ml-auto" size="sm" variant="ghost" aria-label="Refresh" title="Refresh" onClick={load}>
          <Icon name="refresh-cw" />
        </Button>
      </div>
      <div className="min-h-0 flex-1 overflow-auto p-2">
        {tab === "done" &&
          (entries?.length === 0 ? (
            <Empty className="h-full">
              <EmptyHeader>
                <EmptyTitle>Nothing done for you yet</EmptyTitle>
                <EmptyDescription>
                  What the agent sends, answers or settles in your name is listed here.
                </EmptyDescription>
              </EmptyHeader>
            </Empty>
          ) : (
            (entries ?? []).map((e) => {
              const d = day(e.at)
              const head = d !== lastDay
              lastDay = d
              return (
                <div key={e.id}>
                  {head && <div className="px-1 pt-3 pb-1 text-xs font-medium text-muted-foreground">{d}</div>}
                  <Item size="sm">
                    <ItemContent>
                      {/* The agent writes its summaries in Markdown. */}
                      <Markdown className="w-full font-medium">{e.summary}</Markdown>
                      <ItemDescription>
                        {time(e.at)} · {e.kind}
                        {e.asked ? " · you were asked" : ""}
                      </ItemDescription>
                    </ItemContent>
                  </Item>
                </div>
              )
            })
          ))}
        {tab === "sent" && (
          <div className="flex flex-col gap-2">
            {sent?.warning && (
              <Item role="status">
                <ItemContent>
                  <ItemDescription>{sent.warning}</ItemDescription>
                </ItemContent>
              </Item>
            )}
            {sent?.messages.length === 0 && (
              <Empty>
                <EmptyHeader>
                  <EmptyTitle>No sent messages yet</EmptyTitle>
                  <EmptyDescription>
                    Messages sent in your name and their delivery receipts appear here. Request progress appears when
                    the recipient reports it.
                  </EmptyDescription>
                </EmptyHeader>
              </Empty>
            )}
            {(sent?.messages ?? []).map((m) => (
              <Item key={m.id} variant="outline">
                <ItemContent>
                  <ItemTitle>{m.subject || "Message"}</ItemTitle>
                  <ItemDescription>
                    To {m.to} · {day(m.at)} {time(m.at)}
                  </ItemDescription>
                  <div className="flex flex-wrap items-center gap-2">
                    <Badge variant="secondary">{receiptLabel("", m.delivery)}</Badge>
                    {m.request && <Badge variant="outline">Request: {m.request}</Badge>}
                    {m.task_id && <CiteRef refId={{ kind: "torque.task", canonical: m.task_id }} />}
                  </div>
                  {m.alert && <ItemDescription role="alert">{m.alert}</ItemDescription>}
                  <details>
                    <summary>Timeline ({m.events.length})</summary>
                    {m.events.length === 0 && (
                      <ItemDescription>Predates delivery tracking; no receipts recorded.</ItemDescription>
                    )}
                    {m.events.map((e) => (
                      <Item key={e.id} size="sm">
                        <ItemContent>
                          <ItemTitle>{receiptLabel(e.kind, e.state)}</ItemTitle>
                          <ItemDescription>
                            <time dateTime={e.at} title={e.at}>
                              {new Date(e.at).toLocaleString()}
                            </time>
                            {e.detail ? ` · ${e.detail}` : ""}
                          </ItemDescription>
                        </ItemContent>
                      </Item>
                    ))}
                  </details>
                </ItemContent>
              </Item>
            ))}
          </div>
        )}
        {tab === "mail" &&
          classes.map(([key, title]) => (
            <div key={key} className="pb-2">
              <div className="flex items-center gap-2 px-1 pt-2 pb-1 text-xs font-medium text-muted-foreground">
                {title}
                <Badge variant="secondary">{mail?.[key]?.length ?? 0}</Badge>
              </div>
              {(mail?.[key] ?? []).map((m) => (
                <Item key={m.id} size="sm">
                  <ItemContent>
                    <ItemTitle>{m.subject}</ItemTitle>
                    <ItemDescription>
                      {m.from_name || m.from} · {m.kind} · {day(m.at)} {time(m.at)}
                      {m.why ? ` · ${m.why}` : ""}
                    </ItemDescription>
                  </ItemContent>
                </Item>
              ))}
            </div>
          ))}
        {tab === "rules" &&
          (rules?.length === 0 ? (
            <Empty className="h-full">
              <EmptyHeader>
                <EmptyTitle>No rules</EmptyTitle>
                <EmptyDescription>“This is noise” and “Always tell me” on a card make one.</EmptyDescription>
              </EmptyHeader>
            </Empty>
          ) : (
            (rules ?? []).map((r) => (
              <Item key={r.id} size="sm">
                <ItemContent>
                  <ItemTitle>
                    {[r.from && `from ${r.from}`, r.kind && `kind ${r.kind}`, r.subject_has && `“${r.subject_has}”`]
                      .filter(Boolean)
                      .join(", ")}{" "}
                    → {r.class}
                  </ItemTitle>
                  {r.note && <ItemDescription>{r.note}</ItemDescription>}
                </ItemContent>
                <Button
                  size="icon-xs"
                  variant="ghost"
                  aria-label="Remove rule"
                  title="Remove rule"
                  onClick={() => drop(r.id)}
                >
                  <Icon name="x" />
                </Button>
              </Item>
            ))
          ))}
      </div>
    </div>
  )
}

export default { views: { "assist/main": DoneForYou } } satisfies ModuleWeb
