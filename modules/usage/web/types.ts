/**
 * Data contract and shared presentation helpers for the Surface Usage Monitor module (`modules/usage`).
 *
 * Rules enforced across all four views (`Now`, `Trends`, `Intelligence`, `Sources`):
 * 1. Unknown values (`null` / `undefined`) are rendered as "Unknown", never as `0`.
 * 2. Percentages across windows are never summed together.
 * 3. Different `cost_kind` buckets (`api_billed`, `api_estimated`, `subscription_equivalent`,
 *    `local_compute`) are never summed together.
 * 4. A child window under an exhausted parent window (`parent_blocked === true`) is shaded as
 *    blocked, not shown as available capacity.
 * 5. Forecasts, recommendations, and forfeit scores come from the backend (`/api/m/usage/*`),
 *    never computed in the browser.
 */

export type AccountKind = "subscription" | "api" | "credit" | "local" | string
export type AccountHealth = "ok" | "spend_only" | "stale" | "error" | "unknown" | string
export type WindowKind = "quota" | "balance" | "spend" | string
export type CostKind =
  | "api_billed"
  | "api_estimated"
  | "subscription_equivalent"
  | "local_compute"
  | "unclassified"
  | string

export interface UsageWindowItem {
  id: string
  kind?: WindowKind
  group?: string
  model?: string
  unit?: string
  used?: number | null
  limit?: number | null
  remaining?: number | null
  window_seconds?: number | null
  resets_at?: string
  rolling?: boolean
  partial_reset?: boolean
  parent_blocked?: boolean
  blocked_by_window?: string
  severity?: string
}

export interface SpendByCostKind {
  cost_kind: CostKind
  known_cost_usd?: number | null
  events: number
  unpriced_events: number
  partial_events: number
  known_tokens: number
  models?: string[]
}

export interface UsageAccount {
  id: string
  provider: string
  kind: AccountKind
  label: string
  plan?: string
  enabled: boolean
  health: AccountHealth
  stale: boolean
  observed_at?: string
  collected_at?: string
  source_id?: string
  collector?: string
  latest_error?: string
  attributes?: Record<string, unknown>
  windows: UsageWindowItem[]
  spend_by_cost_kind?: SpendByCostKind[]
  runway_days?: number | null
  low_balance?: boolean
}

export interface UseItOrLoseItEntry {
  rank: number
  provider: string
  account_id: string
  account_label: string
  billing_type: string
  model_group: string
  window_id: string
  unit: string
  forfeitable_percent?: number | null
  resets_at?: string
  seconds_to_reset?: number | null
  reset_kind?: string
  urgency_score?: number | null
  modalities?: string[]
  gateway_models?: string[]
}

export interface NextResetItem {
  account_id: string
  account_label: string
  provider: string
  window_id: string
  model?: string
  group?: string
  unit?: string
  used?: number | null
  limit?: number | null
  remaining?: number | null
  resets_at: string
  seconds_to_reset?: number | null
  partial_reset?: boolean
  rolling?: boolean
  parent_blocked?: boolean
  reset_credits_available?: boolean
}

export interface UsageSourceItem {
  id: string
  account_id: string
  account_label?: string
  provider?: string
  collector: string
  health: AccountHealth
  stale: boolean
  observed_at?: string
  collected_at?: string
  latest_error?: string
  window_count?: number
  attributes?: Record<string, unknown>
  raw_url?: string
}

export interface UsageNowPayload {
  fetched_at: string
  live: boolean
  error?: string
  use_it_or_lose_it: UseItOrLoseItEntry[]
  next_resets: NextResetItem[]
  accounts: UsageAccount[]
  sources: UsageSourceItem[]
}

export interface SeriesPoint {
  at: string
  used?: number | null
  remaining?: number | null
  max_used?: number | null
  min_remaining?: number | null
}

export interface WindowSeries {
  account_id: string
  account_label: string
  provider: string
  window_id: string
  model?: string
  group?: string
  unit: string
  kind: WindowKind
  step: string
  current_period: SeriesPoint[]
  previous_period?: SeriesPoint[]
}

export interface CompletedPeriodRow {
  account_id: string
  account_label: string
  provider: string
  window_id: string
  model?: string
  group?: string
  period_start: string
  period_end: string
  peak_used_percent?: number | null
  used_at_reset_percent?: number | null
  captured_percent?: number | null
  forfeited_percent?: number | null
  exhausted_at?: string
  hours_exhausted?: number | null
  rolling?: boolean
  parent_blocked?: boolean
}

export interface SpendGroupRow {
  bucket: string
  group_key: string
  provider?: string
  model?: string
  account_id?: string
  cost_kind: CostKind
  known_cost_usd?: number | null
  events: number
  unpriced_events?: number
  known_tokens?: number
}

export interface UsageTrendsPayload {
  fetched_at: string
  series_source: "live" | "fixture"
  periods_source: "live" | "fixture"
  spend_source: "live" | "fixture"
  series: WindowSeries[]
  periods: CompletedPeriodRow[]
  spend: SpendGroupRow[]
}

export interface InsightRecommendation {
  id: string
  kind: "forfeit_soon" | "blocked" | "top_up" | "plan_fit" | "human_action" | "collector_broken" | string
  severity: "critical" | "warning" | "info" | string
  title: string
  summary: string
  action?: string
  account_id: string
  account_label?: string
  provider?: string
  window_id?: string
  dedupe_key?: string
  basis?: Record<string, unknown>
  evidence?: string[]
}

export interface WindowForecast {
  account_id: string
  account_label: string
  provider: string
  window_id: string
  model?: string
  group?: string
  used_percent?: number | null
  burn_rate_per_hour?: number | null
  typical_burn_per_hour?: number | null
  projected_at_reset?: number | null
  projected_exhaustion_at?: string
  will_run_out?: boolean
  forfeit_forecast?: number | null
  resets_at?: string
  seconds_to_reset?: number | null
  parent_blocked?: boolean
}

export interface SpendAnomaly {
  id: string
  account_id: string
  account_label: string
  provider: string
  model?: string
  cost_kind: CostKind
  observed_at: string
  daily_spend_usd?: number | null
  trailing_median_usd?: number | null
  threshold_usd?: number | null
  kind: "spend_spike" | "overage_watch" | string
  window_caused_by?: string
  summary: string
}

export interface SubscriptionValueRow {
  account_id: string
  account_label: string
  provider: string
  plan?: string
  monthly_price_usd?: number | null
  billing_day?: number | null
  utilization_percent?: number | null
  forfeited_percent?: number | null
  left_on_table_usd?: number | null
  exhaustion_share_percent?: number | null
  hours_exhausted?: number | null
  overage_spend_usd?: number | null
  plan_fit: "well_matched" | "over_provisioned" | "under_provisioned" | "unknown" | string
  fit_reason?: string
  periods_analyzed?: number
}

export interface ResetCalendarEvent {
  id: string
  kind: "window_reset" | "partial_reset" | "reset_credits" | "signin_expiry" | string
  account_id: string
  account_label: string
  provider: string
  window_id?: string
  model?: string
  group?: string
  at: string
  seconds_until?: number | null
  summary: string
}

export interface UsageIntelligencePayload {
  fetched_at: string
  insights_source: "live" | "fixture"
  resets_source: "live" | "fixture"
  recommendations: InsightRecommendation[]
  forecasts: WindowForecast[]
  anomalies: SpendAnomaly[]
  subscription_value: SubscriptionValueRow[]
  resets_calendar: ResetCalendarEvent[]
}

export const COST_KIND_META: Record<
  string,
  { label: string; shortLabel: string; badgeClass: string; description: string }
> = {
  api_billed: {
    label: "API Billed (Provider Invoice)",
    shortLabel: "api_billed",
    badgeClass: "border-primary/40 bg-primary/15 text-primary",
    description: "Direct provider-billed API spend reported by billing ledger.",
  },
  api_estimated: {
    label: "API Estimated (Gateway Metered)",
    shortLabel: "api_estimated",
    badgeClass: "border-chart-2/40 bg-chart-2/15 text-chart-2",
    description: "Gateway-metered token spend priced from catalog rates.",
  },
  subscription_equivalent: {
    label: "Subscription Equivalent",
    shortLabel: "subscription_equivalent",
    badgeClass: "border-success/40 bg-success/15 text-success",
    description: "Imputed API value of tokens included in a flat-rate subscription.",
  },
  local_compute: {
    label: "Local Compute",
    shortLabel: "local_compute",
    badgeClass: "border-warning/40 bg-warning/15 text-warning",
    description: "Self-hosted or local inference compute cost.",
  },
  unclassified: {
    label: "Unpriced / Unclassified Events",
    shortLabel: "unpriced",
    badgeClass: "border-border bg-muted/40 text-muted-foreground",
    description: "Events without a catalog price or cost_kind classification.",
  },
}

export function formatNum(v?: number | null, digits = 1): string {
  if (v === null || v === undefined || Number.isNaN(v)) return "Unknown"
  if (Math.abs(v - Math.round(v)) < 0.005) return Math.round(v).toLocaleString()
  return v.toLocaleString(undefined, { minimumFractionDigits: 0, maximumFractionDigits: digits })
}

export function formatPct(v?: number | null, digits = 1): string {
  if (v === null || v === undefined || Number.isNaN(v)) return "Unknown"
  return `${formatNum(v, digits)}%`
}

export function formatUSD(v?: number | null): string {
  if (v === null || v === undefined || Number.isNaN(v)) return "Unknown"
  if (v > 0 && v < 0.01) return `$${v.toFixed(4)}`
  return `$${v.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`
}

export function formatValueWithUnit(v?: number | null, unit?: string): string {
  if (v === null || v === undefined || Number.isNaN(v)) return "Unknown"
  const u = (unit ?? "").toLowerCase()
  if (u === "percent" || u === "%") return formatPct(v)
  if (u === "usd" || u === "$") return formatUSD(v)
  if (u === "credits") return `${formatNum(v, 1)} credits`
  if (u === "requests") return `${formatNum(v, 0)} req`
  if (u === "tokens") return `${formatNum(v, 0)} tok`
  return unit ? `${formatNum(v, 1)} ${unit}` : formatNum(v, 1)
}

export function formatCountdown(resetsAt?: string, secondsToReset?: number | null): string {
  let sec = secondsToReset
  if ((sec === null || sec === undefined) && resetsAt) {
    const ms = new Date(resetsAt).getTime() - Date.now()
    if (!Number.isNaN(ms)) sec = Math.max(0, Math.round(ms / 1000))
  }
  if (sec === null || sec === undefined || Number.isNaN(sec)) return "Unknown"
  if (sec <= 0) return "resetting now"
  const m = Math.floor(sec / 60)
  if (m < 60) return `in ${Math.max(1, m)}m`
  const h = Math.floor(m / 60)
  const remM = m % 60
  if (h < 48) return remM > 0 ? `in ${h}h ${remM}m` : `in ${h}h`
  const d = Math.floor(h / 24)
  const remH = h % 24
  return remH > 0 ? `in ${d}d ${remH}h` : `in ${d}d`
}

export function formatRelativeAge(iso?: string): string {
  if (!iso) return "Unknown"
  const ms = Date.now() - new Date(iso).getTime()
  if (Number.isNaN(ms)) return "Unknown"
  const s = Math.max(0, Math.round(ms / 1000))
  if (s < 60) return `${s}s ago`
  const m = Math.round(s / 60)
  if (m < 60) return `${m}m ago`
  const h = Math.round(m / 60)
  if (h < 48) return `${h}h ago`
  return `${Math.round(h / 24)}d ago`
}

export function formatLocalTimestamp(iso?: string): string {
  if (!iso) return "Unknown"
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return "Unknown"
  return d.toLocaleString([], {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  })
}

export function healthMeta(
  health?: AccountHealth,
  stale?: boolean,
): {
  label: string
  dotClass: string
  badgeClass: string
} {
  const h = (health ?? "unknown").toLowerCase()
  if (h === "error") {
    return {
      label: "Collector Error",
      dotClass: "bg-destructive",
      badgeClass: "border-destructive/45 bg-destructive/15 text-destructive",
    }
  }
  if (stale || h === "stale") {
    return {
      label: "Stale Reading",
      dotClass: "bg-warning",
      badgeClass: "border-warning/45 bg-warning/15 text-warning",
    }
  }
  if (h === "spend_only") {
    return {
      label: "Spend Only (Gateway)",
      dotClass: "bg-chart-2",
      badgeClass: "border-chart-2/40 bg-chart-2/15 text-chart-2",
    }
  }
  if (h === "ok") {
    return {
      label: "Live · OK",
      dotClass: "bg-success",
      badgeClass: "border-success/40 bg-success/15 text-success",
    }
  }
  return {
    label: "Unknown",
    dotClass: "bg-muted-foreground",
    badgeClass: "border-border bg-muted/40 text-muted-foreground",
  }
}
