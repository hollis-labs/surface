/**
 * Usage Monitor — View 2: TRENDS (`modules/usage/web/trends.tsx`).
 *
 * Implements Section 5.2 of `registry:document:architecture-usage-intelligence`:
 * 1. Per-window usage curves across periods (with previous-period overlay SVG chart).
 * 2. Captured vs. Forfeited by completed period (never forfeit subscription capacity).
 * 3. Spend over time stacked by provider or model, strictly split by `cost_kind`
 *    (`api_billed`, `api_estimated`, `subscription_equivalent`, `local_compute`).
 * 4. Balance burn-down curves for credit and prepaid API accounts.
 */
import { Icon } from "@hollis-labs/surface-page/sdk"
import { useMemo, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import {
  COST_KIND_META,
  formatLocalTimestamp,
  formatNum,
  formatPct,
  formatUSD,
  formatValueWithUnit,
  type SeriesPoint,
  type UsageTrendsPayload,
  type WindowSeries,
} from "./types"

function SeriesCurveChart({ series, showOverlay }: { series: WindowSeries; showOverlay: boolean }) {
  const cur = series.current_period ?? []
  const prev = series.previous_period ?? []
  const isBalance = series.kind === "balance"

  const maxVal = useMemo(() => {
    if (series.unit === "percent") return 100
    let m = 1
    for (const p of [...cur, ...(showOverlay ? prev : [])]) {
      const v = isBalance ? p.remaining : p.used
      if (v != null && v > m) m = v
    }
    return m
  }, [cur, prev, showOverlay, series.unit, isBalance])

  const toPolyline = (pts: SeriesPoint[]) => {
    if (pts.length === 0) return ""
    const w = 520
    const h = 110
    const valid: string[] = []
    pts.forEach((p, idx) => {
      const val = isBalance ? p.remaining : p.used
      if (val == null) return
      const x = pts.length === 1 ? w / 2 : (idx / (pts.length - 1)) * (w - 16) + 8
      const y = h - 10 - Math.min(1, Math.max(0, val / maxVal)) * (h - 22)
      valid.push(`${x.toFixed(1)},${y.toFixed(1)}`)
    })
    return valid.join(" ")
  }

  const curLine = toPolyline(cur)
  const prevLine = showOverlay ? toPolyline(prev) : ""
  const latest = cur[cur.length - 1]
  const latestVal = isBalance ? latest?.remaining : latest?.used

  return (
    <div className="flex flex-col gap-2 rounded-lg border border-border/80 bg-card/75 p-3.5 text-xs">
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div>
          <div className="flex flex-wrap items-center gap-1.5">
            <span className="font-semibold text-foreground">{series.account_label || series.account_id}</span>
            <span className="text-muted-foreground">·</span>
            <span className="font-medium text-primary">{series.model || series.window_id}</span>
            {series.group && (
              <Badge variant="secondary" className="h-4 px-1.5 font-mono text-[9.5px] uppercase">
                {series.group}
              </Badge>
            )}
            <Badge variant="outline" className="h-4 px-1.5 font-mono text-[9.5px]">
              {series.kind} · step {series.step}
            </Badge>
          </div>
          <code className="font-mono text-[10px] text-muted-foreground">
            {series.account_id} / {series.window_id}
          </code>
        </div>
        <div className="text-right font-mono">
          <div className="font-bold text-foreground text-sm">
            {formatValueWithUnit(latestVal, series.unit)} {isBalance ? "remaining" : "used"}
          </div>
          <div className="text-[10px] text-muted-foreground">
            {cur.length} points{showOverlay && prev.length > 0 ? ` · ${prev.length} prior overlay` : ""}
          </div>
        </div>
      </div>

      {/* SVG Curve Canvas */}
      <div className="relative h-28 w-full overflow-hidden rounded-md border border-border/60 bg-background/70 p-1">
        <svg viewBox="0 0 520 110" className="size-full" aria-label={`${series.window_id} trend curve`}>
          {/* Horizontal reference grid lines */}
          {[0.25, 0.5, 0.75, 1].map((f) => {
            const y = 100 - f * 78
            return (
              <line
                key={f}
                x1="8"
                y1={y}
                x2="512"
                y2={y}
                stroke="currentColor"
                strokeOpacity="0.14"
                strokeDasharray="3 3"
              />
            )
          })}
          {prevLine && (
            <polyline
              fill="none"
              stroke="var(--color-muted-foreground)"
              strokeWidth="1.6"
              strokeDasharray="4 3"
              strokeOpacity="0.65"
              points={prevLine}
            />
          )}
          {curLine && (
            <polyline
              fill="none"
              stroke={isBalance ? "var(--color-chart-2)" : "var(--color-primary)"}
              strokeWidth="2.2"
              strokeLinecap="round"
              strokeLinejoin="round"
              points={curLine}
            />
          )}
        </svg>
      </div>

      <div className="flex flex-wrap items-center justify-between gap-2 font-mono text-[10px] text-muted-foreground">
        <div className="flex items-center gap-3">
          <span className="inline-flex items-center gap-1">
            <span className={`inline-block h-0.5 w-3 ${isBalance ? "bg-chart-2" : "bg-primary"}`} />
            Current period
          </span>
          {showOverlay && prev.length > 0 && (
            <span className="inline-flex items-center gap-1">
              <span className="inline-block h-0.5 w-3 border-t border-dashed border-muted-foreground" />
              Previous period overlay
            </span>
          )}
        </div>
        {cur.length > 0 && (
          <span>
            {formatLocalTimestamp(cur[0]?.at)} → {formatLocalTimestamp(cur[cur.length - 1]?.at)}
          </span>
        )}
      </div>
    </div>
  )
}

export function UsageTrendsView({ data }: { data: UsageTrendsPayload }) {
  const [accountFilter, setAccountFilter] = useState<string>("all")
  const [costKindFilter, setCostKindFilter] = useState<string>("api_estimated")
  const [spendGroupBy, setSpendGroupBy] = useState<"provider" | "model">("provider")
  const [showOverlay, setShowOverlay] = useState<boolean>(true)

  const accounts = useMemo(() => {
    const map = new Map<string, string>()
    for (const s of data.series) map.set(s.account_id, s.account_label || s.account_id)
    for (const p of data.periods) map.set(p.account_id, p.account_label || p.account_id)
    return [...map.entries()]
  }, [data.series, data.periods])

  const quotaSeries = useMemo(
    () =>
      data.series.filter((s) => s.kind !== "balance" && (accountFilter === "all" || s.account_id === accountFilter)),
    [data.series, accountFilter],
  )

  const balanceSeries = useMemo(
    () =>
      data.series.filter((s) => s.kind === "balance" && (accountFilter === "all" || s.account_id === accountFilter)),
    [data.series, accountFilter],
  )

  const filteredPeriods = useMemo(
    () => data.periods.filter((p) => accountFilter === "all" || p.account_id === accountFilter),
    [data.periods, accountFilter],
  )

  // Filter spend strictly by the selected single cost_kind so different cost_kinds are NEVER summed together
  const availableCostKinds = useMemo(() => {
    const s = new Set<string>()
    for (const r of data.spend) if (r.cost_kind) s.add(r.cost_kind)
    if (s.size === 0) s.add("api_estimated")
    return [...s]
  }, [data.spend])

  const activeCostKind = availableCostKinds.includes(costKindFilter)
    ? costKindFilter
    : (availableCostKinds[0] ?? "api_estimated")

  const spendRowsForKind = useMemo(() => {
    return data.spend.filter((r) => (r.cost_kind || "unclassified") === activeCostKind)
  }, [data.spend, activeCostKind])

  // Group spend rows within the single selected cost_kind by provider or model
  const spendAggregated = useMemo(() => {
    const map = new Map<
      string,
      { key: string; known_cost_usd: number | null; events: number; tokens: number; buckets: number }
    >()
    for (const r of spendRowsForKind) {
      const k =
        spendGroupBy === "model"
          ? r.model || r.group_key || "unknown-model"
          : r.provider || r.account_id || r.group_key || "unknown-provider"
      const cur = map.get(k) ?? { key: k, known_cost_usd: null, events: 0, tokens: 0, buckets: 0 }
      if (r.known_cost_usd != null) {
        cur.known_cost_usd = (cur.known_cost_usd ?? 0) + r.known_cost_usd
      }
      cur.events += r.events || 0
      cur.tokens += r.known_tokens || 0
      cur.buckets += 1
      map.set(k, cur)
    }
    return [...map.values()].sort((a, b) => (b.known_cost_usd ?? -1) - (a.known_cost_usd ?? -1))
  }, [spendRowsForKind, spendGroupBy])

  const maxSpendInKind = useMemo(() => {
    let m = 0.01
    for (const r of spendAggregated) {
      if (r.known_cost_usd != null && r.known_cost_usd > m) m = r.known_cost_usd
    }
    return m
  }, [spendAggregated])

  return (
    <div className="flex flex-col gap-5">
      {/* Top Filter & Endpoint Source Strip */}
      <div className="flex flex-wrap items-center justify-between gap-3 rounded-md border border-border/80 bg-card/65 px-3.5 py-2.5 text-xs">
        <div className="flex flex-wrap items-center gap-2">
          <Select value={accountFilter} onValueChange={(v) => setAccountFilter(String(v))}>
            <SelectTrigger size="sm" aria-label="Account filter" className="h-7 min-w-44 text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All accounts ({accounts.length})</SelectItem>
              {accounts.map(([id, label]) => (
                <SelectItem key={id} value={id}>
                  {label} ({id})
                </SelectItem>
              ))}
            </SelectContent>
          </Select>

          <Select value={showOverlay ? "overlay" : "current"} onValueChange={(v) => setShowOverlay(v === "overlay")}>
            <SelectTrigger size="sm" aria-label="Period overlay" className="h-7 min-w-44 text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="overlay">Overlay previous period</SelectItem>
              <SelectItem value="current">Current period only</SelectItem>
            </SelectContent>
          </Select>
        </div>

        {/* Endpoint Source Provenance Badges */}
        <div className="flex flex-wrap items-center gap-1.5 font-mono text-[10px]">
          <Badge
            variant="outline"
            className={
              data.series_source === "live"
                ? "border-success/40 bg-success/10 text-success"
                : "border-chart-2/40 bg-chart-2/10 text-chart-2"
            }
          >
            /v1/series: {data.series_source}
          </Badge>
          <Badge
            variant="outline"
            className={
              data.periods_source === "live"
                ? "border-success/40 bg-success/10 text-success"
                : "border-chart-2/40 bg-chart-2/10 text-chart-2"
            }
          >
            /v1/periods: {data.periods_source}
          </Badge>
          <Badge
            variant="outline"
            className={
              data.spend_source === "live"
                ? "border-success/40 bg-success/10 text-success"
                : "border-chart-2/40 bg-chart-2/10 text-chart-2"
            }
          >
            /v1/spend: {data.spend_source}
          </Badge>
        </div>
      </div>

      {/* ==================== 1. PER-WINDOW USAGE CURVES (WITH PERIOD OVERLAY) ==================== */}
      <section className="flex flex-col gap-3">
        <div className="flex items-center justify-between border-b border-border/60 pb-1.5">
          <div className="flex items-center gap-2">
            <Icon name="trending-up" size={14} className="text-primary" />
            <h3 className="font-mono text-xs font-bold uppercase tracking-wider text-foreground">
              Per-Window Usage Curves Across Periods ({quotaSeries.length})
            </h3>
          </div>
          <span className="font-mono text-[11px] text-muted-foreground">
            Current window trajectory overlaid against prior completed period
          </span>
        </div>
        <div className="grid grid-cols-1 gap-3.5 lg:grid-cols-2">
          {quotaSeries.map((s) => (
            <SeriesCurveChart key={`${s.account_id}:${s.window_id}`} series={s} showOverlay={showOverlay} />
          ))}
        </div>
      </section>

      {/* ==================== 2. CAPTURED VS FORFEITED BY COMPLETED PERIOD ==================== */}
      <section className="flex flex-col gap-3">
        <div className="flex items-center justify-between border-b border-border/60 pb-1.5">
          <div className="flex items-center gap-2">
            <Icon name="layers" size={14} className="text-success" />
            <h3 className="font-mono text-xs font-bold uppercase tracking-wider text-foreground">
              Captured vs. Forfeited Subscription Capacity by Period ({filteredPeriods.length})
            </h3>
          </div>
          <span className="font-mono text-[11px] text-muted-foreground">
            Forfeited = 100% − used at reset (0% for rolling or parent-blocked windows)
          </span>
        </div>

        <div className="flex flex-col gap-2 rounded-lg border border-border/80 bg-card/75 p-3.5">
          {filteredPeriods.map((p) => {
            const cap = p.captured_percent
            const forf = p.forfeited_percent
            return (
              <div
                key={`${p.account_id}:${p.window_id}:${p.period_start}:${p.period_end}`}
                className="flex flex-col gap-1.5 rounded-md border border-border/70 bg-background/60 p-2.5 text-xs"
              >
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <div className="flex flex-wrap items-center gap-1.5">
                    <span className="font-semibold text-foreground">{p.account_label || p.account_id}</span>
                    <span className="text-muted-foreground">·</span>
                    <span className="font-medium text-primary">{p.model || p.window_id}</span>
                    {p.group && (
                      <Badge variant="secondary" className="h-4 px-1.5 font-mono text-[9.5px] uppercase">
                        {p.group}
                      </Badge>
                    )}
                    <span className="font-mono text-[10.5px] text-muted-foreground">
                      {formatLocalTimestamp(p.period_start)} → {formatLocalTimestamp(p.period_end)}
                    </span>
                    {p.rolling && (
                      <Badge variant="outline" className="h-4 px-1 font-mono text-[9px] text-chart-2">
                        rolling (0% forfeited)
                      </Badge>
                    )}
                    {p.parent_blocked && (
                      <Badge
                        variant="outline"
                        className="h-4 border-warning/40 bg-warning/10 px-1 font-mono text-[9px] text-warning"
                      >
                        parent-blocked
                      </Badge>
                    )}
                  </div>

                  <div className="flex items-center gap-3 font-mono text-xs">
                    <span className="text-success">Captured: {formatPct(cap)}</span>
                    <span
                      className={forf != null && forf >= 30 ? "font-semibold text-warning" : "text-muted-foreground"}
                    >
                      Forfeited: {formatPct(forf)}
                    </span>
                    {p.hours_exhausted != null && p.hours_exhausted > 0 && (
                      <Badge variant="destructive" className="h-4.5 px-1.5 font-mono text-[9.5px]">
                        Exhausted early ({formatNum(p.hours_exhausted, 1)}h blocked)
                      </Badge>
                    )}
                  </div>
                </div>

                {/* Stacked Captured vs Forfeited Bar */}
                {cap != null && (
                  <div className="flex h-2 w-full overflow-hidden rounded-full bg-muted/60">
                    <div
                      className="h-full bg-success"
                      style={{ width: `${Math.min(100, Math.max(0, cap))}%` }}
                      title={`Captured: ${formatPct(cap)}`}
                    />
                    {forf != null && forf > 0 && (
                      <div
                        className="h-full bg-warning/75"
                        style={{ width: `${Math.min(100 - cap, Math.max(0, forf))}%` }}
                        title={`Forfeited: ${formatPct(forf)}`}
                      />
                    )}
                  </div>
                )}
              </div>
            )
          })}
        </div>
      </section>

      {/* ==================== 3. SPEND BY PROVIDER / MODEL (SPLIT BY COST_KIND) ==================== */}
      <section className="flex flex-col gap-3">
        <div className="flex flex-wrap items-center justify-between gap-2 border-b border-border/60 pb-1.5">
          <div className="flex items-center gap-2">
            <Icon name="activity" size={14} className="text-chart-2" />
            <h3 className="font-mono text-xs font-bold uppercase tracking-wider text-foreground">
              Spend Breakdown Split Strictly by Cost Kind
            </h3>
          </div>

          <div className="flex flex-wrap items-center gap-2">
            <Select value={activeCostKind} onValueChange={(v) => setCostKindFilter(String(v))}>
              <SelectTrigger size="sm" aria-label="Cost kind" className="h-7 min-w-52 font-mono text-xs">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {availableCostKinds.map((ck) => (
                  <SelectItem key={ck} value={ck}>
                    {COST_KIND_META[ck]?.label ?? ck}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>

            <Select value={spendGroupBy} onValueChange={(v) => setSpendGroupBy(v as "provider" | "model")}>
              <SelectTrigger size="sm" aria-label="Group spend by" className="h-7 min-w-36 text-xs">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="provider">Group by Provider</SelectItem>
                <SelectItem value="model">Group by Model</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </div>

        <div className="flex flex-col gap-2 rounded-lg border border-border/80 bg-card/75 p-3.5">
          <div className="flex items-center justify-between border-b border-border/50 pb-2 text-xs">
            <div className="flex items-center gap-2">
              <Badge
                variant="outline"
                className={`font-mono text-[10px] ${(COST_KIND_META[activeCostKind] ?? COST_KIND_META.unclassified).badgeClass}`}
              >
                {activeCostKind}
              </Badge>
              <span className="text-muted-foreground">
                {(COST_KIND_META[activeCostKind] ?? COST_KIND_META.unclassified).description}
              </span>
            </div>
            <span className="font-mono text-[10.5px] text-muted-foreground">
              Isolated cost_kind view — never combined with other cost kinds
            </span>
          </div>

          <div className="flex flex-col gap-2 pt-1">
            {spendAggregated.map((row) => {
              const pct =
                row.known_cost_usd != null ? Math.min(100, Math.max(3, (row.known_cost_usd / maxSpendInKind) * 100)) : 0
              return (
                <div
                  key={row.key}
                  className="flex flex-col gap-1 rounded-md border border-border/65 bg-background/60 p-2.5 text-xs"
                >
                  <div className="flex items-center justify-between gap-2 font-mono">
                    <div className="flex items-center gap-2">
                      <span className="font-semibold text-foreground">{row.key}</span>
                      <span className="text-[10.5px] text-muted-foreground">
                        {row.events.toLocaleString()} events · {row.tokens.toLocaleString()} tokens
                      </span>
                    </div>
                    <span className="font-bold text-foreground">
                      {row.known_cost_usd != null ? formatUSD(row.known_cost_usd) : "Unknown"}
                    </span>
                  </div>
                  {row.known_cost_usd != null && (
                    <div className="h-1.5 w-full overflow-hidden rounded-full bg-muted/60">
                      <div className="h-full rounded-full bg-chart-2" style={{ width: `${pct}%` }} />
                    </div>
                  )}
                </div>
              )
            })}
          </div>
        </div>
      </section>

      {/* ==================== 4. BALANCE BURN-DOWN ==================== */}
      {balanceSeries.length > 0 && (
        <section className="flex flex-col gap-3">
          <div className="flex items-center justify-between border-b border-border/60 pb-1.5">
            <div className="flex items-center gap-2">
              <Icon name="wallet" size={14} className="text-chart-2" />
              <h3 className="font-mono text-xs font-bold uppercase tracking-wider text-foreground">
                Prepaid &amp; Credit Balance Burn-Down ({balanceSeries.length})
              </h3>
            </div>
            <span className="font-mono text-[11px] text-muted-foreground">
              Remaining balance trajectory across observation buckets
            </span>
          </div>
          <div className="grid grid-cols-1 gap-3.5 lg:grid-cols-2">
            {balanceSeries.map((s) => (
              <SeriesCurveChart key={`${s.account_id}:${s.window_id}`} series={s} showOverlay={showOverlay} />
            ))}
          </div>
        </section>
      )}
    </div>
  )
}
