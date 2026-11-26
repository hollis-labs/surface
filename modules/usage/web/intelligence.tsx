/**
 * Usage Monitor — View 3: INTELLIGENCE (`modules/usage/web/intelligence.tsx`).
 *
 * Implements Section 5.3 of `registry:document:architecture-usage-intelligence`:
 * 1. Actionable Recommendations with expandable numeric basis & evidence drill-down
 *    (Forfeit soon, Blocked by parent, Low runway top-up, Plan fit, Human re-login, Collector broken).
 * 2. Window Burn & Exhaustion Forecasts (`projected_at_reset`, `projected_exhaustion_at`,
 *    `forfeit_forecast`, `will_run_out` flag) — all computed by the backend, never in the UI.
 * 3. Per-Subscription Value Matrix (`monthly_price_usd`, `utilization_percent`,
 *    `left_on_table_usd`, `exhaustion_share_percent`, `plan_fit`).
 * 4. Spend Anomalies & Overage Watch (daily spend spikes > max(2×median, median+3σ) and
 *    extra-usage accrued while base window was exhausted).
 */
import { CiteText, Icon } from "@hollis-labs/surface-page/sdk"
import { useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  COST_KIND_META,
  formatCountdown,
  formatLocalTimestamp,
  formatNum,
  formatPct,
  formatUSD,
  type InsightRecommendation,
  type UsageIntelligencePayload,
} from "./types"

function RecommendationCard({ rec }: { rec: InsightRecommendation }) {
  const [openEvidence, setOpenEvidence] = useState(false)

  const sevTone =
    rec.severity === "critical"
      ? {
          border: "border-destructive/50 bg-destructive/10",
          badge: "border-destructive/50 bg-destructive/15 text-destructive",
          icon: "octagon-pause",
        }
      : rec.severity === "warning"
        ? {
            border: "border-warning/45 bg-warning/10",
            badge: "border-warning/45 bg-warning/15 text-warning",
            icon: "triangle-exclamation-point",
          }
        : {
            border: "border-primary/40 bg-primary/10",
            badge: "border-primary/40 bg-primary/15 text-primary",
            icon: "sparkles",
          }

  return (
    <article className={`flex flex-col gap-2 rounded-lg border p-3.5 text-xs ${sevTone.border}`}>
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div className="flex items-start gap-2.5">
          <span
            className={`mt-0.5 flex size-6 shrink-0 items-center justify-center rounded-sm border ${sevTone.badge}`}
          >
            <Icon name={sevTone.icon} size={13} />
          </span>
          <div className="flex flex-col gap-0.5">
            <div className="flex flex-wrap items-center gap-1.5">
              <Badge variant="outline" className={`h-4.5 px-1.5 font-mono text-[9.5px] uppercase ${sevTone.badge}`}>
                {rec.kind.replace(/_/g, " ")}
              </Badge>
              {rec.account_label && (
                <Badge variant="secondary" className="h-4.5 px-1.5 font-mono text-[10px]">
                  {rec.account_label}
                </Badge>
              )}
              {rec.window_id && <code className="font-mono text-[10px] text-muted-foreground">({rec.window_id})</code>}
            </div>
            <h4 className="font-semibold text-foreground text-sm">{rec.title}</h4>
            <p className="text-foreground/90 text-xs leading-relaxed">
              <CiteText text={rec.summary} />
            </p>
          </div>
        </div>

        {((rec.evidence?.length ?? 0) > 0 || (rec.basis && Object.keys(rec.basis).length > 0)) && (
          <Button
            variant="outline"
            size="xs"
            className="h-6 shrink-0 font-mono text-[10.5px]"
            onClick={() => setOpenEvidence((o) => !o)}
          >
            <Icon name="eye" size={11} />
            {openEvidence ? "Hide Evidence" : "Evidence & Basis"}
          </Button>
        )}
      </div>

      {rec.action && (
        <div className="flex items-center gap-2 rounded-sm border border-border/70 bg-background/75 px-2.5 py-1.5 font-mono text-[11px] text-primary">
          <Icon name="arrow-up-right" size={12} className="shrink-0" />
          <span>{rec.action}</span>
        </div>
      )}

      {openEvidence && (
        <div className="flex flex-col gap-2 rounded-md border border-border/75 bg-background/85 p-2.5 font-mono text-[11px]">
          {rec.basis && Object.keys(rec.basis).length > 0 && (
            <div className="flex flex-wrap items-center gap-2">
              <span className="font-semibold text-muted-foreground uppercase">Numeric basis:</span>
              {Object.entries(rec.basis).map(([k, v]) => (
                <Badge key={k} variant="outline" className="h-4.5 px-1.5 font-mono text-[10px]">
                  {k}: {v === null || v === undefined ? "Unknown" : String(v)}
                </Badge>
              ))}
            </div>
          )}
          {(rec.evidence?.length ?? 0) > 0 && (
            <ul className="flex list-disc flex-col gap-1 pl-4 text-muted-foreground">
              {rec.evidence?.map((ev) => (
                <li key={ev}>
                  <CiteText text={ev} />
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
    </article>
  )
}

export function UsageIntelligenceView({ data }: { data: UsageIntelligencePayload }) {
  return (
    <div className="flex flex-col gap-5">
      {/* Top Provenance Banner */}
      <div className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-border/80 bg-card/65 px-3.5 py-2 text-xs">
        <span className="text-muted-foreground">
          All forecasts, forfeit percentages, runway estimates, and recommendations are computed by the usage-monitor
          intelligence engine so UI, alerts, and the model broker always agree.
        </span>
        <div className="flex items-center gap-1.5 font-mono text-[10px]">
          <Badge
            variant="outline"
            className={
              data.insights_source === "live"
                ? "border-success/40 bg-success/10 text-success"
                : "border-chart-2/40 bg-chart-2/10 text-chart-2"
            }
          >
            /v1/insights: {data.insights_source}
          </Badge>
          <Badge
            variant="outline"
            className={
              data.resets_source === "live"
                ? "border-success/40 bg-success/10 text-success"
                : "border-chart-2/40 bg-chart-2/10 text-chart-2"
            }
          >
            /v1/resets: {data.resets_source}
          </Badge>
        </div>
      </div>

      {/* ==================== 1. RECOMMENDATIONS WITH EVIDENCE ==================== */}
      <section className="flex flex-col gap-3">
        <div className="flex items-center justify-between border-b border-border/60 pb-1.5">
          <div className="flex items-center gap-2">
            <Icon name="sparkles" size={14} className="text-primary" />
            <h3 className="font-mono text-xs font-bold uppercase tracking-wider text-foreground">
              Recommendations &amp; Actionable Intelligence ({data.recommendations.length})
            </h3>
          </div>
          <span className="font-mono text-[11px] text-muted-foreground">
            Rule-based guidance backed by window observations and historical rollups
          </span>
        </div>
        <div className="grid grid-cols-1 gap-3 lg:grid-cols-2">
          {data.recommendations.map((rec) => (
            <RecommendationCard key={rec.id} rec={rec} />
          ))}
        </div>
      </section>

      {/* ==================== 2. PER-SUBSCRIPTION VALUE & PLAN FIT ==================== */}
      <section className="flex flex-col gap-3">
        <div className="flex items-center justify-between border-b border-border/60 pb-1.5">
          <div className="flex items-center gap-2">
            <Icon name="wallet" size={14} className="text-success" />
            <h3 className="font-mono text-xs font-bold uppercase tracking-wider text-foreground">
              Per-Subscription Value, Utilization &amp; Plan Fit ({data.subscription_value.length})
            </h3>
          </div>
          <span className="font-mono text-[11px] text-muted-foreground">
            Left on the table = monthly price × mean forfeited % across completed periods
          </span>
        </div>

        <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3">
          {data.subscription_value.map((sub) => {
            const fitBadge =
              sub.plan_fit === "well_matched"
                ? "border-success/40 bg-success/15 text-success"
                : sub.plan_fit === "over_provisioned"
                  ? "border-warning/45 bg-warning/15 text-warning"
                  : sub.plan_fit === "under_provisioned"
                    ? "border-destructive/45 bg-destructive/15 text-destructive"
                    : "border-border bg-muted/40 text-muted-foreground"

            return (
              <div
                key={sub.account_id}
                className="flex flex-col gap-2.5 rounded-lg border border-border/80 bg-card/75 p-3.5 text-xs"
              >
                <div className="flex items-start justify-between gap-2 border-b border-border/60 pb-2">
                  <div>
                    <div className="flex items-center gap-1.5">
                      <span className="font-semibold text-foreground text-sm">{sub.account_label}</span>
                      {sub.plan && (
                        <Badge variant="secondary" className="h-4 px-1.5 font-mono text-[9.5px] uppercase">
                          {sub.plan}
                        </Badge>
                      )}
                    </div>
                    <code className="font-mono text-[10px] text-muted-foreground">
                      {sub.provider} · {sub.account_id}
                    </code>
                  </div>
                  <Badge variant="outline" className={`h-5 px-2 font-mono text-[10px] uppercase ${fitBadge}`}>
                    {sub.plan_fit.replace(/_/g, " ")}
                  </Badge>
                </div>

                <div className="grid grid-cols-3 gap-2 font-mono">
                  <div className="rounded-sm border border-border/60 bg-background/60 p-2">
                    <div className="text-[9.5px] text-muted-foreground uppercase">Monthly Price</div>
                    <div className="font-bold text-foreground text-xs">{formatUSD(sub.monthly_price_usd)}</div>
                  </div>
                  <div className="rounded-sm border border-border/60 bg-background/60 p-2">
                    <div className="text-[9.5px] text-muted-foreground uppercase">Utilization</div>
                    <div className="font-bold text-success text-xs">{formatPct(sub.utilization_percent)}</div>
                  </div>
                  <div className="rounded-sm border border-border/60 bg-background/60 p-2">
                    <div className="text-[9.5px] text-muted-foreground uppercase">Left on Table</div>
                    <div
                      className={`font-bold text-xs ${
                        sub.left_on_table_usd != null && sub.left_on_table_usd > 20 ? "text-warning" : "text-foreground"
                      }`}
                    >
                      {formatUSD(sub.left_on_table_usd)}
                    </div>
                  </div>
                </div>

                {sub.fit_reason && <p className="text-[11px] text-muted-foreground leading-snug">{sub.fit_reason}</p>}

                <div className="mt-auto flex flex-wrap items-center justify-between gap-2 border-t border-border/50 pt-1.5 font-mono text-[10px] text-muted-foreground">
                  <span>Forfeited avg: {formatPct(sub.forfeited_percent)}</span>
                  {sub.overage_spend_usd != null && sub.overage_spend_usd > 0 && (
                    <span className="text-warning">Extra usage: {formatUSD(sub.overage_spend_usd)}</span>
                  )}
                  {sub.periods_analyzed != null && <span>{sub.periods_analyzed} periods</span>}
                </div>
              </div>
            )
          })}
        </div>
      </section>

      {/* ==================== 3. WINDOW FORECASTS (PROJECTED AT RESET & EXHAUSTION) ==================== */}
      <section className="flex flex-col gap-3">
        <div className="flex items-center justify-between border-b border-border/60 pb-1.5">
          <div className="flex items-center gap-2">
            <Icon name="trending-up" size={14} className="text-chart-2" />
            <h3 className="font-mono text-xs font-bold uppercase tracking-wider text-foreground">
              Window Burn Rates, Projected Reset Usage &amp; Exhaustion Forecasts ({data.forecasts.length})
            </h3>
          </div>
          <span className="font-mono text-[11px] text-muted-foreground">
            Lookback L = clamp(25% of window_seconds, 1h, 24h)
          </span>
        </div>

        <div className="overflow-x-auto rounded-lg border border-border/80 bg-card/75">
          <table className="w-full border-collapse text-xs">
            <thead className="border-b border-border bg-muted/50 font-mono text-[10px] text-muted-foreground uppercase">
              <tr>
                <th className="px-3 py-2 text-left">Account / Window</th>
                <th className="px-3 py-2 text-right">Current Used</th>
                <th className="px-3 py-2 text-right">Burn / hr (7d Typical)</th>
                <th className="px-3 py-2 text-right">Projected at Reset</th>
                <th className="px-3 py-2 text-right">Forfeit Forecast</th>
                <th className="px-3 py-2 text-right">Exhaustion / Reset</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border/60 font-mono">
              {data.forecasts.map((f) => (
                <tr key={`${f.account_id}:${f.window_id}`} className="hover:bg-muted/25">
                  <td className="px-3 py-2 font-sans">
                    <div className="flex flex-wrap items-center gap-1.5">
                      <span className="font-semibold text-foreground">{f.account_label || f.account_id}</span>
                      <span className="text-muted-foreground">·</span>
                      <span className="text-primary">{f.model || f.window_id}</span>
                      {f.group && (
                        <Badge variant="outline" className="h-4 px-1 font-mono text-[9px] uppercase">
                          {f.group}
                        </Badge>
                      )}
                      {f.parent_blocked && (
                        <Badge
                          variant="outline"
                          className="h-4 border-warning/40 bg-warning/10 px-1 font-mono text-[9px] text-warning"
                        >
                          parent-blocked
                        </Badge>
                      )}
                    </div>
                  </td>
                  <td className="px-3 py-2 text-right">{formatPct(f.used_percent)}</td>
                  <td className="px-3 py-2 text-right text-muted-foreground">
                    {f.burn_rate_per_hour != null ? `${formatNum(f.burn_rate_per_hour, 2)}%/h` : "Unknown"}
                    {f.typical_burn_per_hour != null ? ` (${formatNum(f.typical_burn_per_hour, 2)}%/h)` : ""}
                  </td>
                  <td className="px-3 py-2 text-right font-semibold text-foreground">
                    {formatPct(f.projected_at_reset)}
                  </td>
                  <td
                    className={`px-3 py-2 text-right font-semibold ${
                      f.forfeit_forecast != null && f.forfeit_forecast >= 30 ? "text-warning" : "text-success"
                    }`}
                  >
                    {formatPct(f.forfeit_forecast)}
                  </td>
                  <td className="px-3 py-2 text-right">
                    {f.will_run_out ? (
                      <Badge variant="destructive" className="font-mono text-[10px]">
                        Will run out {f.projected_exhaustion_at ? formatCountdown(f.projected_exhaustion_at) : "early"}
                      </Badge>
                    ) : (
                      <span className="text-muted-foreground">
                        Resets {formatCountdown(f.resets_at, f.seconds_to_reset)}
                      </span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>

      {/* ==================== 4. SPEND ANOMALIES & OVERAGE WATCH ==================== */}
      <section className="flex flex-col gap-3">
        <div className="flex items-center justify-between border-b border-border/60 pb-1.5">
          <div className="flex items-center gap-2">
            <Icon name="triangle-exclamation-point" size={14} className="text-warning" />
            <h3 className="font-mono text-xs font-bold uppercase tracking-wider text-foreground">
              Spend Anomalies &amp; Extra-Usage Overage Watch ({data.anomalies.length})
            </h3>
          </div>
          <span className="font-mono text-[11px] text-muted-foreground">
            Daily spend &gt; max(2× trailing 28d median, median + 3σ) or extra_usage while base window exhausted
          </span>
        </div>

        <div className="grid grid-cols-1 gap-3 lg:grid-cols-2">
          {data.anomalies.map((an) => {
            const ckMeta = COST_KIND_META[an.cost_kind || "unclassified"] ?? COST_KIND_META.unclassified
            return (
              <div
                key={an.id}
                className="flex flex-col gap-2 rounded-lg border border-warning/45 bg-warning/10 p-3.5 text-xs"
              >
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <div className="flex flex-wrap items-center gap-1.5">
                    <Badge
                      variant="outline"
                      className="border-warning/50 bg-warning/15 font-mono text-[10px] text-warning uppercase"
                    >
                      {an.kind.replace(/_/g, " ")}
                    </Badge>
                    <span className="font-semibold text-foreground">{an.account_label || an.account_id}</span>
                    {an.model && <code className="font-mono text-[10.5px] text-primary">{an.model}</code>}
                    <Badge variant="outline" className={`h-4 px-1.5 font-mono text-[9.5px] ${ckMeta.badgeClass}`}>
                      {ckMeta.shortLabel}
                    </Badge>
                  </div>
                  <span className="font-mono text-[10.5px] text-muted-foreground">
                    {formatLocalTimestamp(an.observed_at)}
                  </span>
                </div>
                <p className="text-foreground/90 leading-snug">{an.summary}</p>
                <div className="flex flex-wrap items-center gap-3 font-mono text-[11px] text-muted-foreground">
                  <span>
                    Observed: <strong className="text-foreground">{formatUSD(an.daily_spend_usd)}</strong>
                  </span>
                  {an.trailing_median_usd != null && <span>28d median: {formatUSD(an.trailing_median_usd)}</span>}
                  {an.threshold_usd != null && <span>Threshold: {formatUSD(an.threshold_usd)}</span>}
                  {an.window_caused_by && (
                    <span>
                      Caused by window: <code>{an.window_caused_by}</code>
                    </span>
                  )}
                </div>
              </div>
            )
          })}
        </div>
      </section>
    </div>
  )
}
