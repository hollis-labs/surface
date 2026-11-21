import type { ViewProps } from "@hollis-labs/surface-page/sdk"
import { useEffect, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Spinner } from "@/components/ui/spinner"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { message, time } from "./api"

type Status = "ok" | "warning" | "exhausted" | "unknown" | "error" | "gap"
interface UsageCard {
  id: string
  title: string
  kind: string
  status: Status
  used: number | null
  limit: number | null
  unit?: string
  window?: string
  reset_at?: string
  summary?: string
  gap?: string
  observed_at?: string
}
interface Totals {
  key: string
  requests: number
  successes: number
  errors: number
  input_tokens: number
  output_tokens: number
  reasoning_tokens: number
  cache_read_tokens: number
}
interface Budget {
  provider: string
  model?: string
  level?: string
  scope?: string
  window?: string
  reset_at?: string
  max_cost_usd?: number
  max_requests?: number
  max_tokens?: number
  exhausted: boolean
  error?: string
}
interface Detail {
  windows: { label: string; since: string; total: Totals }[]
  by_provider: Totals[]
  by_model: Totals[]
  budgets: Budget[]
  alerts: { at: string; provider?: string; model?: string; operation?: string; error?: string }[]
  warnings?: string[]
}
interface UsageData {
  cards: UsageCard[]
  source?: string
  detail?: Detail
  fetched_at: string
}

const STATUS: Record<Status, { label: string; dot: string }> = {
  ok: { label: "OK", dot: "bg-success" },
  warning: { label: "Warning", dot: "bg-warning" },
  exhausted: { label: "Limit reached", dot: "bg-destructive" },
  error: { label: "Unavailable", dot: "bg-destructive" },
  unknown: { label: "Unknown", dot: "bg-muted-foreground" },
  gap: { label: "Not collected", dot: "bg-muted-foreground" },
}
const n = (v?: number | null) => (v == null ? "Unknown" : v.toLocaleString())

function StatusBadge({ status }: { status: Status }) {
  const s = STATUS[status] ?? STATUS.unknown
  return (
    <Badge variant={status === "exhausted" || status === "error" ? "destructive" : "outline"}>
      <span aria-hidden className={`size-2 rounded-full ${s.dot}`} />
      {s.label}
    </Badge>
  )
}

function limitText(b: Budget) {
  const parts = []
  if (b.max_cost_usd != null) parts.push(`$${b.max_cost_usd}`)
  if (b.max_requests != null) parts.push(`${n(b.max_requests)} requests`)
  if (b.max_tokens != null) parts.push(`${n(b.max_tokens)} tokens`)
  return parts.join(", ") || "Unknown"
}

function Consumers({ title, rows }: { title: string; rows: Totals[] }) {
  return (
    <>
      <h4 className="font-medium text-sm">{title}</h4>
      {rows.length === 0 ? (
        <p className="text-muted-foreground text-sm">None recorded.</p>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Requests</TableHead>
              <TableHead>Errors</TableHead>
              <TableHead>Input tokens</TableHead>
              <TableHead>Output tokens</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((r) => (
              <TableRow key={r.key}>
                <TableCell>{r.key}</TableCell>
                <TableCell>{n(r.requests)}</TableCell>
                <TableCell>{n(r.errors)}</TableCell>
                <TableCell>{n(r.input_tokens)}</TableCell>
                <TableCell>{n(r.output_tokens)}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </>
  )
}

function RouterDetail({ d }: { d: Detail }) {
  return (
    <section aria-label="Tether AI router details" className="flex flex-col gap-3">
      {d.warnings?.map((w) => (
        <p key={w} role="status" className="text-sm">
          {w}
        </p>
      ))}
      <h4 className="font-medium text-sm">Usage by window</h4>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Window</TableHead>
            <TableHead>Requests</TableHead>
            <TableHead>Errors</TableHead>
            <TableHead>Input tokens</TableHead>
            <TableHead>Output tokens</TableHead>
            <TableHead>Reasoning tokens</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {d.windows.map((w) => (
            <TableRow key={w.label}>
              <TableCell title={`Since ${time(w.since)}`}>{w.label}</TableCell>
              <TableCell>{n(w.total.requests)}</TableCell>
              <TableCell>{n(w.total.errors)}</TableCell>
              <TableCell>{n(w.total.input_tokens)}</TableCell>
              <TableCell>{n(w.total.output_tokens)}</TableCell>
              <TableCell>{n(w.total.reasoning_tokens)}</TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
      <p className="text-muted-foreground text-xs">
        The router records requests and tokens but no cost. Daily history beyond these windows is not stored yet.
      </p>
      <Consumers title="Biggest providers · last 30 days" rows={d.by_provider} />
      <Consumers title="Biggest models · last 30 days" rows={d.by_model} />
      <h4 className="font-medium text-sm">Budgets</h4>
      {d.budgets
        .filter((b) => b.error)
        .map((b) => (
          <p key={`problem:${b.provider}:${b.model}:${b.scope}`} role="status" className="text-sm">
            <span className="font-medium">
              {b.provider}
              {b.model ? ` / ${b.model}` : ""} ({limitText(b)} per {b.window || "unknown window"}
              {b.scope ? `, scoped by ${b.scope}` : ""})
            </span>{" "}
            cannot be evaluated. Tether reports: “{b.error}”. A request this budget covers fails with that error and is
            logged as a budget rejection (below), so the limit is not being measured. The budget is set in Tether’s AI
            catalog.
          </p>
        ))}
      {d.budgets.length === 0 ? (
        <p className="text-muted-foreground text-sm">No gateway budgets are configured.</p>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Provider</TableHead>
              <TableHead>Limit</TableHead>
              <TableHead>Window</TableHead>
              <TableHead>Resets</TableHead>
              <TableHead>State</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {d.budgets.map((b) => (
              <TableRow key={`${b.provider}:${b.model}:${b.scope}`}>
                <TableCell>
                  {b.provider}
                  {b.model ? ` · ${b.model}` : ""}
                </TableCell>
                <TableCell>{limitText(b)}</TableCell>
                <TableCell>
                  {b.window || "Unknown"}
                  {b.scope ? ` · ${b.scope}` : ""}
                </TableCell>
                <TableCell>{time(b.reset_at) || "Unknown"}</TableCell>
                <TableCell>
                  {b.exhausted ? (
                    <StatusBadge status="exhausted" />
                  ) : b.error ? (
                    <StatusBadge status="warning" />
                  ) : (
                    "Within limit"
                  )}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
      <h4 className="font-medium text-sm">Budget rejections · last 7 days</h4>
      {d.alerts.length === 0 ? (
        <p className="text-muted-foreground text-sm">None.</p>
      ) : (
        <ul className="flex flex-col gap-1 text-sm">
          {d.alerts.map((a) => (
            <li key={`${a.at}:${a.provider}:${a.error}`}>
              {time(a.at)} · {a.provider || "unknown provider"}
              {a.model ? ` · ${a.model}` : ""}: {a.error}
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

export function Usage({ win, desk, api }: ViewProps) {
  const source = typeof win.args.source === "string" ? win.args.source : ""
  const [data, setData] = useState<UsageData | null>(null)
  const [error, setError] = useState("")
  useEffect(() => {
    let stopped = false
    let timer: ReturnType<typeof setTimeout>
    setData(null)
    setError("")
    const load = async () => {
      try {
        const next = await api.get<UsageData>(`usage?source=${encodeURIComponent(source)}`)
        if (!stopped) {
          setData(next)
          setError("")
        }
      } catch (e) {
        if (!stopped) setError(message(e))
      } finally {
        // One request at a time; no timer once the view unmounts.
        if (!stopped) timer = setTimeout(() => void load(), 60000)
      }
    }
    void load()
    return () => {
      stopped = true
      clearTimeout(timer)
    }
  }, [api, source])
  const selected = data?.cards.find((c) => c.id === source)
  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3 overflow-auto p-3">
      {error && (
        <p role="alert" className="text-destructive text-sm">
          {error} Displayed usage may be out of date.
        </p>
      )}
      {!data && !error && <Spinner />}
      {data && (
        <>
          <p className="text-muted-foreground text-xs">
            Observed {time(data.fetched_at)} · refreshes every minute while open
          </p>
          <div className="grid grid-cols-[repeat(auto-fill,minmax(16rem,1fr))] gap-3">
            {data.cards.map((c) => (
              <Card key={c.id} size="sm" data-state={c.id === source ? "selected" : undefined}>
                <CardHeader>
                  <CardTitle>
                    <Button
                      variant={c.id === source ? "secondary" : "ghost"}
                      size="sm"
                      onClick={() => void desk.setArgs(win.id, { ...win.args, source: c.id === source ? "" : c.id })}
                    >
                      {c.title}
                    </Button>
                  </CardTitle>
                  <CardDescription>
                    <StatusBadge status={c.status} />
                  </CardDescription>
                </CardHeader>
                <CardContent className="flex flex-col gap-1 text-sm">
                  {c.status !== "gap" && (
                    <p>
                      <span className="font-semibold">{n(c.used)}</span>
                      {c.unit ? ` ${c.unit}` : ""} · limit {c.limit == null ? "not reported" : n(c.limit)}
                    </p>
                  )}
                  {c.reset_at && (
                    <p className="text-muted-foreground text-xs">
                      Resets {time(c.reset_at)}
                      {c.window ? ` · ${c.window}` : ""}
                    </p>
                  )}
                  {c.gap && <p className="text-muted-foreground text-xs">{c.gap}</p>}
                </CardContent>
              </Card>
            ))}
          </div>
          {!source && <p className="text-muted-foreground text-sm">Select a card to see its details.</p>}
          {selected && (
            <section aria-label={`${selected.title} details`} className="flex flex-col gap-3">
              <h3 className="font-semibold">{selected.title}</h3>
              {selected.summary && <p className="text-sm">{selected.summary}</p>}
              {data.detail ? (
                <RouterDetail d={data.detail} />
              ) : (
                <p className="text-muted-foreground text-sm">{selected.gap || "No details for this source."}</p>
              )}
            </section>
          )}
        </>
      )}
    </div>
  )
}
