import { Icon, ObjectActionsMenu, type ViewProps } from "@hollis-labs/surface-page/sdk"
import { useEffect, useMemo, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Spinner } from "@/components/ui/spinner"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { age, message, time } from "./api"

type Status = "ok" | "warning" | "exhausted" | "unknown" | "error" | "gap"

interface UsageCard {
  id: string
  title: string
  kind: string
  status: Status
  used: number | null
  limit: number | null
  remaining?: number
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
  windows?: { label: string; since: string; total: Totals }[]
  by_provider?: Totals[]
  by_model?: Totals[]
  budgets?: Budget[]
  alerts?: { at: string; provider?: string; model?: string; operation?: string; error?: string }[]
  spend?: { provider: string; calls: number; failed: number; cost_usd: number }[]
  recent?: { id: string; at: string; what: string; amount?: number; unit?: string; note?: string; ok: boolean }[]
  notes?: string[]
  warnings?: string[]
}

interface UsageData {
  cards: UsageCard[]
  source?: string
  detail?: Detail
  fetched_at: string
}

const STATUS: Record<
  Status,
  {
    label: string
    dot: string
    badgeClass: string
    stripeClass: string
  }
> = {
  ok: {
    label: "OK",
    dot: "bg-success",
    badgeClass: "border-success/40 bg-success/10 text-success",
    stripeClass: "bg-success/80",
  },
  warning: {
    label: "Warning",
    dot: "bg-warning",
    badgeClass: "border-warning/45 bg-warning/10 text-warning",
    stripeClass: "bg-warning/85",
  },
  exhausted: {
    label: "Limit reached",
    dot: "bg-destructive",
    badgeClass: "border-destructive/45 bg-destructive/15 text-destructive",
    stripeClass: "bg-destructive",
  },
  error: {
    label: "Unavailable",
    dot: "bg-destructive",
    badgeClass: "border-destructive/45 bg-destructive/15 text-destructive",
    stripeClass: "bg-destructive/80",
  },
  unknown: {
    label: "Unknown",
    dot: "bg-muted-foreground",
    badgeClass: "border-border bg-muted/40 text-muted-foreground",
    stripeClass: "bg-muted-foreground/50",
  },
  gap: {
    label: "Not collected",
    dot: "bg-muted-foreground/60",
    badgeClass: "border-border/80 bg-muted/35 text-muted-foreground",
    stripeClass: "bg-muted-foreground/35",
  },
}

const n = (v?: number | null) => (v == null ? "Unknown" : v.toLocaleString())
const usd = (v?: number | null) => (v == null ? "Unknown" : `$${v.toFixed(2)}`)

function StatusBadge({ status }: { status: Status }) {
  const s = STATUS[status] ?? STATUS.unknown
  return (
    <Badge
      variant={status === "exhausted" || status === "error" ? "destructive" : "outline"}
      className={`h-4.5 gap-1 px-1.5 py-0 font-mono text-[10px] ${
        status !== "exhausted" && status !== "error" ? s.badgeClass : ""
      }`}
    >
      <span aria-hidden className={`size-1.5 rounded-full ${s.dot}`} />
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

/** Extract 30-day spend or request volume from the summary string for quick card display. */
function extractCardHighlight(c: UsageCard): {
  primaryLabel: string
  primaryValue: string
  secondaryLabel: string
  secondaryValue: string
  meterPct: number
} {
  if (c.status === "gap") {
    return {
      primaryLabel: "Collector Status",
      primaryValue: "Not wired",
      secondaryLabel: "Source Kind",
      secondaryValue: c.kind,
      meterPct: 0,
    }
  }
  if (c.remaining != null) {
    const spentMatch = /(\d+(?:\.\d+)?)\s+credits\s+spent/i.exec(c.summary ?? "")
    const spent = spentMatch ? Number(spentMatch[1]) : 0
    const totalPool = c.remaining + spent
    const pct = totalPool > 0 ? Math.min(100, Math.max(8, Math.round((c.remaining / totalPool) * 100))) : 50
    return {
      primaryLabel: "Remaining Balance",
      primaryValue: `${n(c.remaining)} ${c.unit ?? "credits"}`,
      secondaryLabel: "Recent Ledger",
      secondaryValue: spentMatch ? `${Number(spentMatch[1]).toLocaleString()} credits spent` : "Prepaid balance",
      meterPct: pct,
    }
  }
  const usd30Match = /\$(\d+(?:\.\d+)?)\s+over\s+the\s+last\s+30\s+days/i.exec(c.summary ?? "")
  const req30Match = /Last\s+30\s+days:\s*(\d+)\s+requests/i.exec(c.summary ?? "")
  const provMatch = /across\s+(\d+\s+providers?)/i.exec(c.summary ?? "")

  const isUSD = (c.unit ?? "").toLowerCase().includes("usd")
  const primaryValue =
    c.used != null ? (isUSD ? `$${c.used.toFixed(2)}` : `${n(c.used)} ${c.unit ?? ""}`.trim()) : "Unknown"

  const secondaryValue = usd30Match
    ? `$${usd30Match[1]} (30d${provMatch ? ` · ${provMatch[1]}` : ""})`
    : req30Match
      ? `${Number(req30Match[1]).toLocaleString()} req (30d)`
      : c.limit != null
        ? `Limit: ${n(c.limit)}`
        : "No cap configured"

  let meterPct = 18
  if (c.used != null && c.limit != null && c.limit > 0) {
    meterPct = Math.min(100, Math.max(6, Math.round((c.used / c.limit) * 100)))
  } else if (usd30Match) {
    const amt = Number(usd30Match[1])
    meterPct = Math.min(92, Math.max(12, Math.round(Math.log10(amt + 1) * 42)))
  } else if (req30Match) {
    const reqs = Number(req30Match[1])
    meterPct = Math.min(92, Math.max(15, Math.round(Math.log10(reqs + 1) * 28)))
  }

  return {
    primaryLabel: c.unit || "Today",
    primaryValue,
    secondaryLabel: c.limit != null ? "Quota Limit" : "30-Day Volume",
    secondaryValue,
    meterPct,
  }
}

/**
 * Technical line-art illustration for each usage source kind (`gateway`, `router`, `suite`, `credits`, `subscription`).
 */
function UsageLineArt({ kind, status, size = "sm" }: { kind: string; status: Status; size?: "sm" | "lg" }) {
  const isGap = status === "gap"
  const accent =
    status === "exhausted" || status === "error"
      ? "var(--color-destructive)"
      : status === "warning"
        ? "var(--color-warning)"
        : isGap
          ? "var(--color-muted-foreground)"
          : "var(--color-primary)"
  const led =
    status === "ok"
      ? "var(--color-success)"
      : status === "warning"
        ? "var(--color-warning)"
        : status === "exhausted" || status === "error"
          ? "var(--color-destructive)"
          : "var(--color-muted-foreground)"

  const dims = size === "lg" ? "size-13" : "size-11"
  return (
    <div
      className={`relative flex ${dims} shrink-0 items-center justify-center rounded-sm border border-border/80 bg-muted/40 p-1.5 shadow-2xs`}
      aria-hidden="true"
    >
      <svg viewBox="0 0 48 48" fill="none" className="size-full" aria-hidden="true">
        {/* Blueprint corner ticks */}
        <path
          d="M3 7V3H7M41 3H45V7M45 41V45H41M7 45H3V41"
          stroke="currentColor"
          strokeOpacity="0.28"
          strokeWidth="1.2"
          strokeLinecap="round"
        />
        {kind === "router" || kind === "gateway" ? (
          <>
            <rect
              x="8"
              y="12"
              width="32"
              height="24"
              rx="2.5"
              stroke="currentColor"
              strokeOpacity="0.75"
              strokeWidth="1.4"
            />
            <circle cx="15" cy="24" r="3" stroke={accent} strokeWidth="1.4" />
            <circle cx="33" cy="18" r="2.2" stroke={accent} strokeWidth="1.3" />
            <circle cx="33" cy="30" r="2.2" stroke="currentColor" strokeOpacity="0.65" strokeWidth="1.3" />
            <path d="M18 23L30.5 18.5M18 25L30.5 29.5" stroke={accent} strokeOpacity="0.8" strokeWidth="1.3" />
            <circle cx="15" cy="24" r="1.3" fill={led} />
          </>
        ) : kind === "suite" ? (
          <>
            <rect
              x="8"
              y="10"
              width="32"
              height="28"
              rx="2.5"
              stroke="currentColor"
              strokeOpacity="0.75"
              strokeWidth="1.4"
            />
            <circle cx="24" cy="24" r="8" stroke={accent} strokeOpacity="0.8" strokeWidth="1.3" />
            <path d="M13 32L19 25L24 29L30 20L35 26" stroke={accent} strokeWidth="1.4" strokeLinecap="round" />
            <circle cx="13" cy="15" r="1.5" fill={led} />
          </>
        ) : kind === "credits" ? (
          <>
            <polygon
              points="24,8 38,16 38,32 24,40 10,32 10,16"
              stroke="currentColor"
              strokeOpacity="0.75"
              strokeWidth="1.4"
            />
            <polyline points="10,16 24,24 38,16" stroke={accent} strokeOpacity="0.8" strokeWidth="1.3" />
            <line x1="24" y1="24" x2="24" y2="40" stroke={accent} strokeOpacity="0.8" strokeWidth="1.3" />
            <circle cx="24" cy="15" r="1.7" fill={led} />
          </>
        ) : (
          <>
            <rect
              x="8"
              y="13"
              width="32"
              height="22"
              rx="2.5"
              stroke="currentColor"
              strokeOpacity="0.7"
              strokeWidth="1.4"
            />
            <line x1="8" y1="20" x2="40" y2="20" stroke="currentColor" strokeOpacity="0.5" strokeWidth="1.3" />
            <rect x="13" y="25" width="9" height="5" rx="1" stroke={accent} strokeWidth="1.2" />
            <circle cx="34" cy="27.5" r="1.6" fill={led} />
          </>
        )}
      </svg>
    </div>
  )
}

/** 12-segment visual quota / activity meter bar. */
function UsageSegmentedBar({ percent, status }: { percent: number; status: Status }) {
  const segments = 12
  const filled = status === "gap" ? 0 : Math.round((Math.min(100, Math.max(0, percent)) / 100) * segments)
  const activeColor =
    status === "exhausted" || status === "error" ? "bg-destructive" : status === "warning" ? "bg-warning" : "bg-primary"
  const slots = Array.from({ length: segments }, (_, i) => i)
  return (
    <div className="grid grid-cols-12 gap-0.5" aria-hidden="true">
      {slots.map((slot) => (
        <span key={`u-seg-${slot}`} className={`h-2 rounded-[1px] ${slot < filled ? activeColor : "bg-muted/65"}`} />
      ))}
    </div>
  )
}

function Consumers({ title, rows }: { title: string; rows: Totals[] }) {
  const maxReq = Math.max(1, ...rows.map((r) => r.requests))
  return (
    <div className="flex flex-col gap-2">
      <h4 className="font-mono text-xs font-semibold uppercase tracking-wider text-foreground">{title}</h4>
      {rows.length === 0 ? (
        <p className="text-muted-foreground text-sm">None recorded.</p>
      ) : (
        <div className="overflow-hidden rounded-md border border-border/75 bg-card/50">
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
              {rows.map((r) => {
                const share = Math.min(100, Math.max(4, Math.round((r.requests / maxReq) * 100)))
                return (
                  <TableRow key={r.key}>
                    <TableCell className="font-mono text-xs font-medium">
                      <div className="flex flex-col gap-1">
                        <span>{r.key}</span>
                        <div className="h-1 w-28 overflow-hidden rounded-full bg-muted">
                          <div className="h-full bg-primary/80" style={{ width: `${share}%` }} />
                        </div>
                      </div>
                    </TableCell>
                    <TableCell className="font-mono text-xs tabular-nums">{n(r.requests)}</TableCell>
                    <TableCell
                      className={`font-mono text-xs tabular-nums ${r.errors > 0 ? "text-warning font-semibold" : ""}`}
                    >
                      {n(r.errors)}
                    </TableCell>
                    <TableCell className="font-mono text-xs tabular-nums">{n(r.input_tokens)}</TableCell>
                    <TableCell className="font-mono text-xs tabular-nums">{n(r.output_tokens)}</TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        </div>
      )}
    </div>
  )
}

function SpendDetail({ d }: { d: Detail }) {
  const maxSpend = Math.max(0.01, ...(d.spend ?? []).map((s) => s.cost_usd))
  return (
    <section aria-label="Spend details" className="flex flex-col gap-4">
      {d.warnings?.map((w) => (
        <div
          key={w}
          role="status"
          className="flex items-center gap-2 rounded-sm border border-warning/40 bg-warning/10 px-3 py-2 text-xs text-warning"
        >
          <Icon name="alert-triangle" size={14} />
          <span>{w}</span>
        </div>
      ))}

      {d.spend && (
        <div className="flex flex-col gap-2">
          <h4 className="font-mono text-xs font-semibold uppercase tracking-wider text-foreground">
            Spend by provider · last 30 days
          </h4>
          {d.spend.length === 0 ? (
            <p className="text-muted-foreground text-sm">None recorded.</p>
          ) : (
            <div className="overflow-hidden rounded-md border border-border/75 bg-card/50">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Provider</TableHead>
                    <TableHead>Spend</TableHead>
                    <TableHead>Calls</TableHead>
                    <TableHead>Failed</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {d.spend.map((r) => {
                    const pct = Math.min(100, Math.max(4, Math.round((r.cost_usd / maxSpend) * 100)))
                    return (
                      <TableRow key={r.provider}>
                        <TableCell className="font-mono text-xs font-medium">
                          <div className="flex flex-col gap-1">
                            <span>{r.provider}</span>
                            <div className="h-1 w-28 overflow-hidden rounded-full bg-muted">
                              <div className="h-full bg-primary/80" style={{ width: `${pct}%` }} />
                            </div>
                          </div>
                        </TableCell>
                        <TableCell className="font-mono text-xs font-semibold text-foreground tabular-nums">
                          {usd(r.cost_usd)}
                        </TableCell>
                        <TableCell className="font-mono text-xs tabular-nums">{n(r.calls)}</TableCell>
                        <TableCell
                          className={`font-mono text-xs tabular-nums ${r.failed > 0 ? "text-warning font-semibold" : ""}`}
                        >
                          {n(r.failed)}
                        </TableCell>
                      </TableRow>
                    )
                  })}
                </TableBody>
              </Table>
            </div>
          )}
        </div>
      )}

      {d.recent && (
        <div className="flex flex-col gap-2">
          <h4 className="font-mono text-xs font-semibold uppercase tracking-wider text-foreground">Recent charges</h4>
          {d.recent.length === 0 ? (
            <p className="text-muted-foreground text-sm">None recorded.</p>
          ) : (
            <div className="overflow-hidden rounded-md border border-border/75 bg-card/50">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Time</TableHead>
                    <TableHead>What</TableHead>
                    <TableHead>Amount</TableHead>
                    <TableHead>Note</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {d.recent.map((r) => (
                    <TableRow key={`${r.id}:${r.at}:${r.what}:${r.note}`}>
                      <TableCell className="font-mono text-xs text-muted-foreground">{time(r.at)}</TableCell>
                      <TableCell className="font-mono text-xs font-medium">{r.what}</TableCell>
                      <TableCell className="font-mono text-xs font-semibold tabular-nums">
                        {r.unit === "USD" ? usd(r.amount) : `${n(r.amount)} ${r.unit ?? ""}`}
                      </TableCell>
                      <TableCell className="text-xs">
                        {!r.ok && <StatusBadge status="error" />} {r.note}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          )}
        </div>
      )}

      {d.notes && d.notes.length > 0 && (
        <div className="flex flex-col gap-1 rounded-md border border-border/60 bg-muted/35 px-3.5 py-2.5 text-xs text-muted-foreground">
          {d.notes.map((note) => (
            <p key={note}>{note}</p>
          ))}
        </div>
      )}
    </section>
  )
}

function RouterDetail({ d }: { d: Detail }) {
  const windows = d.windows ?? []
  const budgets = d.budgets ?? []
  const alerts = d.alerts ?? []
  return (
    <section aria-label="Tether AI router details" className="flex flex-col gap-4">
      {d.warnings?.map((w) => (
        <div
          key={w}
          role="status"
          className="flex items-center gap-2 rounded-sm border border-warning/40 bg-warning/10 px-3 py-2 text-xs text-warning"
        >
          <Icon name="alert-triangle" size={14} />
          <span>{w}</span>
        </div>
      ))}

      {/* 3 Window Summary Boxes + Full Table */}
      {windows.length > 0 && (
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
          {windows.map((w) => (
            <div
              key={`box-${w.label}`}
              className="flex flex-col justify-between gap-1.5 rounded-md border border-border/80 bg-muted/20 p-3"
            >
              <div className="flex items-center justify-between">
                <span className="font-mono text-[10.5px] font-semibold uppercase tracking-wider text-muted-foreground">
                  {w.label}
                </span>
                <span className="font-mono text-sm font-bold text-primary tabular-nums">{n(w.total.requests)} req</span>
              </div>
              <div className="flex items-center justify-between font-mono text-[11px] text-muted-foreground">
                <span>In: {n(w.total.input_tokens)}</span>
                <span>Out: {n(w.total.output_tokens)}</span>
              </div>
              <div className="flex items-center justify-between font-mono text-[10.5px] text-muted-foreground">
                <span>Reasoning: {n(w.total.reasoning_tokens)}</span>
                <span className={w.total.errors > 0 ? "text-warning font-semibold" : ""}>
                  Errors: {n(w.total.errors)}
                </span>
              </div>
            </div>
          ))}
        </div>
      )}

      <div className="flex flex-col gap-2">
        <h4 className="font-mono text-xs font-semibold uppercase tracking-wider text-foreground">Usage by window</h4>
        <div className="overflow-hidden rounded-md border border-border/75 bg-card/50">
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
              {windows.map((w) => (
                <TableRow key={w.label}>
                  <TableCell className="font-mono text-xs font-medium" title={`Since ${time(w.since)}`}>
                    {w.label}
                  </TableCell>
                  <TableCell className="font-mono text-xs tabular-nums">{n(w.total.requests)}</TableCell>
                  <TableCell className="font-mono text-xs tabular-nums">{n(w.total.errors)}</TableCell>
                  <TableCell className="font-mono text-xs tabular-nums">{n(w.total.input_tokens)}</TableCell>
                  <TableCell className="font-mono text-xs tabular-nums">{n(w.total.output_tokens)}</TableCell>
                  <TableCell className="font-mono text-xs tabular-nums">{n(w.total.reasoning_tokens)}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
        <p className="text-muted-foreground text-xs">
          The router records requests and tokens but no cost. Daily history beyond these windows is not stored yet.
        </p>
      </div>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <Consumers title="Biggest providers · last 30 days" rows={d.by_provider ?? []} />
        <Consumers title="Biggest models · last 30 days" rows={d.by_model ?? []} />
      </div>

      <div className="flex flex-col gap-2">
        <h4 className="font-mono text-xs font-semibold uppercase tracking-wider text-foreground">Budgets</h4>
        {budgets
          .filter((b) => b.error)
          .map((b) => (
            <div
              key={`problem:${b.provider}:${b.model}:${b.scope}`}
              role="status"
              className="rounded-md border border-warning/40 bg-warning/10 px-3.5 py-2.5 text-xs text-foreground"
            >
              <span className="font-semibold text-warning">
                {b.provider}
                {b.model ? ` / ${b.model}` : ""} ({limitText(b)} per {b.window || "unknown window"}
                {b.scope ? `, scoped by ${b.scope}` : ""})
              </span>{" "}
              cannot be evaluated. Tether reports: “{b.error}”. A request this budget covers fails with that error and
              is logged as a budget rejection (below), so the limit is not being measured. The budget is set in Tether’s
              AI catalog.
            </div>
          ))}
        {budgets.length === 0 ? (
          <p className="text-muted-foreground text-sm">No gateway budgets are configured.</p>
        ) : (
          <div className="overflow-hidden rounded-md border border-border/75 bg-card/50">
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
                {budgets.map((b) => (
                  <TableRow key={`${b.provider}:${b.model}:${b.scope}`}>
                    <TableCell className="font-mono text-xs font-medium">
                      {b.provider}
                      {b.model ? ` · ${b.model}` : ""}
                    </TableCell>
                    <TableCell className="font-mono text-xs">{limitText(b)}</TableCell>
                    <TableCell className="font-mono text-xs">
                      {b.window || "Unknown"}
                      {b.scope ? ` · ${b.scope}` : ""}
                    </TableCell>
                    <TableCell className="font-mono text-xs">{time(b.reset_at) || "Unknown"}</TableCell>
                    <TableCell className="text-xs">
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
          </div>
        )}
      </div>

      <div className="flex flex-col gap-2">
        <h4 className="font-mono text-xs font-semibold uppercase tracking-wider text-foreground">
          Budget rejections · last 7 days
        </h4>
        {alerts.length === 0 ? (
          <p className="text-muted-foreground text-sm">None.</p>
        ) : (
          <ul className="flex flex-col gap-1.5 rounded-md border border-border/75 bg-muted/20 p-3 font-mono text-xs">
            {alerts.map((a) => (
              <li key={`${a.at}:${a.provider}:${a.error}`} className="flex flex-wrap items-center gap-1.5">
                <span className="text-muted-foreground">{time(a.at)}</span>
                <span>·</span>
                <span className="font-semibold text-warning">{a.provider || "unknown provider"}</span>
                {a.model && <span className="text-foreground/85">· {a.model}</span>}
                <span className="text-destructive">: {a.error}</span>
              </li>
            ))}
          </ul>
        )}
      </div>
    </section>
  )
}

/** Individual Usage & Quota Card in the Usage grid. */
function UsageMeterCard({ card: c, selected, onSelect }: { card: UsageCard; selected: boolean; onSelect: () => void }) {
  const st = STATUS[c.status] ?? STATUS.unknown
  const hl = useMemo(() => extractCardHighlight(c), [c])
  const isGap = c.status === "gap"

  return (
    <div
      data-slot="usage-meter-card"
      data-state={selected ? "selected" : undefined}
      className={`group relative flex min-w-0 flex-col justify-between gap-3 overflow-hidden rounded-md border bg-card/95 p-4 shadow-2xs transition-all hover:border-primary/50 hover:shadow-md ${
        selected ? "border-primary ring-1 ring-primary/40" : "border-border/85"
      }`}
    >
      {/* Top Semantic Stripe */}
      <span className={`absolute inset-x-0 top-0 h-0.5 ${st.stripeClass}`} />

      {/* Header: Line-Art Source Emblem + Title, Kind Badge & Status Badge */}
      <div className="flex min-w-0 items-start gap-3.5">
        <UsageLineArt kind={c.kind} status={c.status} />

        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <div className="flex min-w-0 items-center justify-between gap-2">
            <Button
              variant={selected ? "secondary" : "ghost"}
              size="xs"
              className="h-auto min-w-0 justify-start px-1 py-0.5 font-mono text-[14px] font-bold tracking-tight text-foreground hover:text-primary"
              onClick={onSelect}
            >
              <span className="truncate">{c.title}</span>
            </Button>

            <div className="flex shrink-0 items-center gap-1">
              <Badge
                variant="outline"
                className="h-4.5 border-border/80 bg-muted/40 px-1.5 py-0 font-mono text-[9.5px] uppercase tracking-wider text-muted-foreground"
              >
                {c.kind}
              </Badge>
              <StatusBadge status={c.status} />
            </div>
          </div>

          <div className="flex flex-wrap items-center gap-2 font-mono text-[10.5px] text-muted-foreground">
            <span>id: {c.id}</span>
            {c.window && (
              <>
                <span>·</span>
                <span>window: {c.window}</span>
              </>
            )}
            {c.observed_at && (
              <>
                <span>·</span>
                <span>{age(c.observed_at)} ago</span>
              </>
            )}
          </div>
        </div>
      </div>

      {/* Main Telemetry Readout or Gap Callout */}
      {!isGap ? (
        <div className="flex flex-col gap-2">
          <div className="grid grid-cols-2 gap-2">
            <div className="flex min-w-0 flex-col justify-between gap-0.5 rounded-sm border border-border/75 bg-muted/25 p-2.5">
              <span className="truncate font-mono text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
                {hl.primaryLabel}
              </span>
              <span className="truncate font-mono text-sm font-bold text-foreground tabular-nums">
                {hl.primaryValue}
              </span>
            </div>

            <div className="flex min-w-0 flex-col justify-between gap-0.5 rounded-sm border border-border/75 bg-muted/25 p-2.5">
              <span className="truncate font-mono text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
                {hl.secondaryLabel}
              </span>
              <span className="truncate font-mono text-xs font-semibold text-primary tabular-nums">
                {hl.secondaryValue}
              </span>
            </div>
          </div>

          <UsageSegmentedBar percent={hl.meterPct} status={c.status} />

          {/* Exact semantic lines preserved for clarity and contract parity */}
          <div className="flex flex-col gap-0.5 text-xs text-muted-foreground">
            {c.remaining != null && (
              <p className="text-foreground/90">
                <span className="font-mono font-semibold">{n(c.remaining)}</span> {c.unit} remaining
              </p>
            )}
            {c.status !== "error" && c.remaining == null && (
              <p className="text-foreground/90">
                <span className="font-mono font-semibold">{n(c.used)}</span>
                {c.unit ? ` ${c.unit}` : ""} · limit {c.limit == null ? "not reported" : n(c.limit)}
              </p>
            )}
            {c.reset_at && (
              <p className="font-mono text-[11px] text-muted-foreground">
                Resets {time(c.reset_at)}
                {c.window ? ` · ${c.window}` : ""}
              </p>
            )}
            {c.gap && <p className="line-clamp-2 text-[11px] text-muted-foreground">{c.gap}</p>}
          </div>
        </div>
      ) : (
        <div className="flex flex-col gap-2">
          <div className="rounded-sm border border-border/65 bg-muted/30 px-3 py-2.5 text-xs text-muted-foreground leading-relaxed">
            <div className="mb-1 flex items-center gap-1.5 font-mono text-[10.5px] font-semibold uppercase tracking-wider text-foreground/80">
              <Icon name="plug-zap" size={11} className="text-muted-foreground" />
              <span>Collection Gap · {c.kind}</span>
            </div>
            <p>{c.gap}</p>
          </div>
        </div>
      )}

      {/* Footer Line */}
      <div className="flex items-center justify-between gap-2 border-t border-border/60 pt-2">
        <span className="truncate font-mono text-[10.5px] text-muted-foreground">
          {isGap ? "Unmetered source" : c.summary ? `${c.summary.slice(0, 56)}…` : "Metered via Tether"}
        </span>
        <Button
          variant="ghost"
          size="xs"
          className="h-6 shrink-0 gap-1 px-2 font-mono text-[11px] text-primary hover:bg-primary/12"
          onClick={onSelect}
        >
          Inspect
          <Icon name="chevron-right" size={12} />
        </Button>
      </div>
    </div>
  )
}

export function Usage({ win, desk, api }: ViewProps) {
  const source = typeof win.args.source === "string" ? win.args.source : ""
  const [data, setData] = useState<UsageData | null>(null)
  const [error, setError] = useState("")
  const [kindFilter, setKindFilter] = useState<string>("all")
  const [search, setSearch] = useState("")

  useEffect(() => {
    let stopped = false
    let timer: ReturnType<typeof setTimeout>
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

  const selectSource = (nextSource: string) => {
    const nextArgs = { ...win.args }
    if (nextSource) {
      nextArgs.source = nextSource
    } else {
      delete nextArgs.source
    }
    void desk.setArgs(win.id, nextArgs)
  }

  const cards = data?.cards ?? []
  const selected = cards.find((c) => c.id === source)

  const summary = useMemo(() => {
    let activeCount = 0
    let gapCount = 0
    let warningCount = 0
    let total30dSpend = 0
    for (const c of cards) {
      if (c.status === "gap") gapCount++
      else activeCount++
      if (c.status === "warning" || c.status === "exhausted" || c.status === "error") warningCount++
      const m = /\$(\d+(?:\.\d+)?)\s+over\s+the\s+last\s+30\s+days/i.exec(c.summary ?? "")
      if (m) total30dSpend += Number(m[1])
    }
    return {
      total: cards.length,
      activeCount,
      gapCount,
      warningCount,
      total30dSpend,
    }
  }, [cards])

  const filteredCards = useMemo(() => {
    const q = search.trim().toLowerCase()
    return cards.filter((c) => {
      if (kindFilter === "active" && c.status === "gap") return false
      if (kindFilter === "gap" && c.status !== "gap") return false
      if (kindFilter !== "all" && kindFilter !== "active" && kindFilter !== "gap" && c.kind !== kindFilter) {
        return false
      }
      if (!q) return true
      return (
        c.title.toLowerCase().includes(q) ||
        c.id.toLowerCase().includes(q) ||
        c.kind.toLowerCase().includes(q) ||
        (c.summary ?? "").toLowerCase().includes(q) ||
        (c.gap ?? "").toLowerCase().includes(q)
      )
    })
  }, [cards, kindFilter, search])

  return (
    <div className="flex min-h-0 min-w-0 flex-1 flex-col gap-4 overflow-x-hidden overflow-y-auto overscroll-x-none p-4">
      {error && (
        <div
          role="alert"
          className="flex items-center gap-2 rounded-sm border border-destructive/40 bg-destructive/10 px-3 py-2 text-xs text-destructive"
        >
          <Icon name="alert-triangle" size={14} />
          <span>{error} Displayed usage may be out of date.</span>
        </div>
      )}

      {!data && !error && (
        <div className="flex flex-1 items-center justify-center py-12">
          <Spinner />
        </div>
      )}

      {data && (
        <>
          {/* Top Telemetry Summary Header & Filter Strip */}
          <div className="flex flex-wrap items-center justify-between gap-3 rounded-md border border-border/80 bg-card/80 px-3.5 py-2.5">
            <div className="flex flex-wrap items-center gap-4">
              <div className="flex items-center gap-2.5">
                <span className="flex size-7 items-center justify-center rounded-sm border border-primary/30 bg-primary/10 text-primary">
                  <Icon name="gauge" size={15} />
                </span>
                <div>
                  <div className="flex flex-wrap items-center gap-2">
                    <h2 className="font-mono text-xs font-bold uppercase tracking-wider text-foreground">
                      Usage, Quotas &amp; Spend
                    </h2>
                    <Badge
                      variant="outline"
                      className="h-4 border-success/40 bg-success/10 px-1.5 font-mono text-[10px] text-success"
                    >
                      {summary.activeCount} metered
                    </Badge>
                    <Badge variant="secondary" className="h-4 px-1.5 font-mono text-[10px]">
                      {summary.gapCount} gaps
                    </Badge>
                  </div>
                  <p className="font-mono text-[11px] text-muted-foreground">
                    Observed {time(data.fetched_at)} · refreshes every minute while open
                  </p>
                </div>
              </div>

              <div className="hidden h-7 w-px bg-border/70 lg:block" />

              <div className="hidden flex-wrap items-center gap-4 font-mono text-xs lg:flex">
                <div>
                  <span className="text-[10px] uppercase text-muted-foreground">30d Media Suites</span>
                  <p className="font-semibold text-foreground">${summary.total30dSpend.toFixed(2)} est.</p>
                </div>
                <div>
                  <span className="text-[10px] uppercase text-muted-foreground">Alerts / Warnings</span>
                  <p className={`font-semibold ${summary.warningCount > 0 ? "text-warning" : "text-foreground"}`}>
                    {summary.warningCount === 0 ? "All within limits" : `${summary.warningCount} flagged`}
                  </p>
                </div>
              </div>
            </div>

            <div className="flex flex-wrap items-center gap-2">
              <div className="flex flex-wrap items-center gap-1 rounded-sm border border-border/75 bg-muted/30 p-0.5">
                {(
                  [
                    { id: "all", label: `All (${summary.total})` },
                    { id: "active", label: `Metered (${summary.activeCount})` },
                    { id: "suite", label: "Suites" },
                    { id: "credits", label: "Credits" },
                    { id: "subscription", label: "Subscriptions" },
                    { id: "gap", label: `Gaps (${summary.gapCount})` },
                  ] as const
                ).map((tab) => (
                  <Button
                    key={tab.id}
                    size="xs"
                    variant={kindFilter === tab.id ? "secondary" : "ghost"}
                    className="h-6 px-2 font-mono text-[11px]"
                    onClick={() => setKindFilter(tab.id)}
                  >
                    {tab.label}
                  </Button>
                ))}
              </div>

              <Input
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                placeholder="Filter provider, suite, kind…"
                className="h-7 w-48 font-mono text-xs"
                aria-label="Filter usage cards"
              />
            </div>
          </div>

          {!source && (
            <p className="font-mono text-xs text-muted-foreground">
              Select a card to inspect its 30-day provider breakdown, recent ledger charges, and configured budgets.
            </p>
          )}

          {/* Grid of Usage & Quota Cards */}
          <div className="grid grid-cols-[repeat(auto-fill,minmax(24rem,1fr))] gap-4">
            {filteredCards.map((c) => (
              <UsageMeterCard
                key={c.id}
                card={c}
                selected={c.id === source}
                onSelect={() => selectSource(c.id === source ? "" : c.id)}
              />
            ))}
          </div>

          {/* Click-Into Usage Source Detail Modal */}
          <Dialog open={!!selected} onOpenChange={(open) => !open && selectSource("")}>
            <DialogContent className="flex max-h-[88vh] w-[min(60rem,calc(100vw-2rem))] max-w-[calc(100vw-2rem)] flex-col gap-0 overflow-hidden overscroll-x-none p-0 sm:max-w-4xl lg:max-w-5xl">
              {selected && (
                <>
                  <DialogHeader className="shrink-0 border-b border-border/80 bg-muted/30 px-5 py-3.5 pr-10">
                    <div className="flex flex-wrap items-start justify-between gap-3">
                      <div className="flex items-start gap-3.5">
                        <UsageLineArt kind={selected.kind} status={selected.status} size="lg" />
                        <div className="flex flex-col gap-1">
                          <div className="flex flex-wrap items-center gap-2">
                            <DialogTitle className="font-mono text-base font-bold tracking-tight">
                              {selected.title}
                            </DialogTitle>
                            <Badge variant="secondary" className="font-mono text-[10.5px] uppercase">
                              {selected.kind}
                            </Badge>
                            <StatusBadge status={selected.status} />
                          </div>
                          <DialogDescription className="text-xs text-muted-foreground">
                            {selected.summary || selected.gap || `Usage telemetry for ${selected.title}`}
                          </DialogDescription>
                        </div>
                      </div>
                      <ObjectActionsMenu
                        object={{
                          kind: "ops.usage",
                          id: selected.id,
                          title: selected.title,
                          subtitle: `${selected.kind} · ${STATUS[selected.status]?.label ?? selected.status}`,
                        }}
                      />
                    </div>
                  </DialogHeader>

                  <div className="mx-auto flex min-h-0 w-[90%] flex-1 flex-col gap-4 overflow-x-hidden overflow-y-auto overscroll-x-none py-4 text-sm">
                    <section aria-label={`${selected.title} details`} className="flex flex-col gap-4">
                      {selected.summary && (
                        <div className="rounded-md border border-border/70 bg-muted/30 px-3.5 py-2.5 text-xs text-foreground/90 leading-relaxed">
                          {selected.summary}
                        </div>
                      )}
                      {data.detail?.windows ? (
                        <RouterDetail d={data.detail} />
                      ) : data.detail && (data.detail.spend || data.detail.recent || data.detail.notes) ? (
                        <SpendDetail d={data.detail} />
                      ) : (
                        <div className="rounded-md border border-border/70 bg-muted/25 p-4 text-xs text-muted-foreground leading-relaxed">
                          {selected.gap || "No details for this source."}
                        </div>
                      )}
                    </section>
                  </div>
                </>
              )}
            </DialogContent>
          </Dialog>
        </>
      )}
    </div>
  )
}
