/**
 * Usage Monitor — View 1: NOW (`modules/usage/web/now.tsx`).
 *
 * Implements Section 5.1 of `registry:document:architecture-usage-intelligence`:
 * - Top strip:
 *   1. Use-It-Or-Lose-It Ranked Capacity Strip (never forfeit paid subscription tokens).
 *   2. Next Resets Countdown Rail (exact countdowns, full vs partial resets, reset credits).
 * - Filter bar (inspired by `modules/ops/web/activity.tsx`): filter by account kind, provider, health, search.
 * - Subscription cards grouped by provider:
 *   - Every window with used/remaining bar, model label, group (`5h`, `weekly`, `monthly`),
 *     resets_at countdown, rolling vs fixed badge, parent-blocked shading (`weekly_scoped:fable`
 *     shaded blocked when `weekly_all` is 100%), available reset credits, plan & rate-limit tier.
 * - Balance & Credit accounts with remaining balance, runway days, and low-balance warning.
 * - Spend-only accounts with gateway spend strictly separated by `cost_kind` (never summed across kinds).
 * - Health & freshness badges separate from latest collector error.
 */
import { Icon } from "@hollis-labs/surface-page/sdk"
import { useMemo, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import {
  COST_KIND_META,
  formatCountdown,
  formatLocalTimestamp,
  formatNum,
  formatPct,
  formatRelativeAge,
  formatUSD,
  formatValueWithUnit,
  healthMeta,
  type UsageAccount,
  type UsageNowPayload,
  type UsageWindowItem,
} from "./types"

const FRIENDLY_WINDOW_LABELS: Record<string, string> = {
  ai_credits_monthly: "Copilot AI Credits",
  premium_requests_monthly: "Copilot Premium Requests",
  actions_minutes_monthly: "GitHub Actions Minutes",
  weekly_all: "All Models Weekly Quota",
  extra_usage: "Monthly Extra Usage",
  primary: "Primary Weekly Quota",
  credits_balance: "Credits Balance",
  characters_monthly: "Monthly Characters",
  characters_monthly_percent: "Monthly Characters (%)",
  account_balance: "Account Balance",
  month_to_date_usage: "Month-to-Date Usage",
  month_to_date_spend: "Month-to-Date Spend",
  subscription_generations: "Subscription Generations",
}

function WindowRow({ win, account }: { win: UsageWindowItem; account: UsageAccount }) {
  const isQuota = win.kind === "quota" || win.unit === "percent"
  const isBalance = win.kind === "balance"
  const isSpend = win.kind === "spend"
  const blocked = Boolean(win.parent_blocked)
  const displayTitle = win.model || FRIENDLY_WINDOW_LABELS[win.id] || win.id

  // Compute progress bar percentage only when used & limit are known
  const pct = useMemo(() => {
    if (win.unit === "percent" && win.used != null) {
      return Math.min(100, Math.max(0, win.used))
    }
    if (win.used != null && win.limit != null && win.limit > 0) {
      return Math.min(100, Math.max(0, (win.used / win.limit) * 100))
    }
    if (isBalance && win.remaining != null && win.limit != null && win.limit > 0) {
      return Math.min(100, Math.max(0, ((win.limit - win.remaining) / win.limit) * 100))
    }
    return null
  }, [win, isBalance])

  const exhausted =
    !blocked &&
    ((win.unit === "percent" && win.used != null && win.used >= 99.9) ||
      (isQuota && win.remaining != null && win.remaining <= 0))

  const barTone = blocked
    ? "bg-muted-foreground/35"
    : exhausted
      ? "bg-destructive"
      : pct != null && pct >= 80
        ? "bg-warning"
        : "bg-primary"

  return (
    <div
      className={`flex flex-col gap-1.5 rounded-md border p-2.5 text-xs transition-colors ${
        blocked
          ? "border-dashed border-warning/45 bg-muted/35 opacity-75"
          : exhausted
            ? "border-destructive/45 bg-destructive/10"
            : "border-border/75 bg-background/60"
      }`}
    >
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex min-w-0 flex-wrap items-center gap-1.5">
          {win.group && (
            <Badge variant="secondary" className="h-4.5 px-1.5 font-mono text-[10px] uppercase">
              {win.group}
            </Badge>
          )}
          <span className="font-semibold text-foreground">{displayTitle}</span>
          {(win.model || FRIENDLY_WINDOW_LABELS[win.id]) && (
            <code className="font-mono text-[10px] text-muted-foreground">({win.id})</code>
          )}
          {win.rolling ? (
            <Badge variant="outline" className="h-4 px-1 font-mono text-[9.5px] text-chart-2">
              rolling
            </Badge>
          ) : win.resets_at ? (
            <Badge variant="outline" className="h-4 px-1 font-mono text-[9.5px] text-muted-foreground">
              fixed reset
            </Badge>
          ) : null}
          {win.partial_reset && (
            <Badge
              variant="outline"
              className="h-4 border-primary/40 bg-primary/10 px-1 font-mono text-[9.5px] text-primary"
            >
              partial reset
            </Badge>
          )}
          {blocked && (
            <Badge
              variant="outline"
              className="h-4 border-warning/50 bg-warning/15 px-1.5 font-mono text-[9.5px] text-warning"
            >
              <Icon name="lock" size={10} />
              Blocked by {win.blocked_by_window || "exhausted parent"}
            </Badge>
          )}
        </div>

        {/* Right Value Readout (Unknown stays Unknown, never 0) */}
        <div className="flex items-center gap-2 font-mono text-xs">
          {isBalance ? (
            <span className="font-semibold text-foreground">
              {win.remaining != null ? `${formatValueWithUnit(win.remaining, win.unit)} left` : "Remaining: Unknown"}
              {win.limit != null ? ` / ${formatValueWithUnit(win.limit, win.unit)}` : ""}
            </span>
          ) : isSpend ? (
            <span className="font-semibold text-foreground">
              {win.used != null ? `${formatValueWithUnit(win.used, win.unit)} spent` : "Spend: Unknown"}
            </span>
          ) : (
            <span className={exhausted ? "font-bold text-destructive" : "font-semibold text-foreground"}>
              {win.used != null ? `${formatValueWithUnit(win.used, win.unit)} used` : "Used: Unknown"}
              {win.remaining != null ? ` · ${formatValueWithUnit(win.remaining, win.unit)} left` : ""}
            </span>
          )}
        </div>
      </div>

      {/* Progress Meter when percentage or limit is known */}
      {pct !== null && (
        <div className="h-1.5 w-full overflow-hidden rounded-full bg-muted/70">
          <div className={`h-full rounded-full transition-all ${barTone}`} style={{ width: `${Math.max(2, pct)}%` }} />
        </div>
      )}

      {/* Reset Countdown & Window Metadata Footer */}
      <div className="flex flex-wrap items-center justify-between gap-2 font-mono text-[10.5px] text-muted-foreground">
        <div className="flex flex-wrap items-center gap-2">
          {win.resets_at ? (
            <span className="inline-flex items-center gap-1 text-foreground/90">
              <Icon name="clock" size={11} className="text-primary" />
              Resets {formatCountdown(win.resets_at)} ({formatLocalTimestamp(win.resets_at)})
            </span>
          ) : (
            <span>No scheduled reset reported</span>
          )}
        </div>
        {blocked && (
          <span className="text-warning">
            Parent window in {account.label} is exhausted — capacity unavailable until parent resets
          </span>
        )}
      </div>
    </div>
  )
}

function AccountCard({
  account,
  onInspectSource,
}: {
  account: UsageAccount
  onInspectSource?: (sourceId: string) => void
}) {
  const hm = healthMeta(account.health, account.stale)
  const attrs = account.attributes ?? {}
  const resetCredits = attrs.reset_credits_available
  const rateLimitTier = typeof attrs.rate_limit_tier === "string" ? attrs.rate_limit_tier : undefined
  const signinExpiresAt = typeof attrs.signin_expires_at === "string" ? attrs.signin_expires_at : undefined
  const overageEnabled = attrs.overage_enabled === "true" || attrs.overage_enabled === true
  const includedAiCredits =
    typeof attrs.included_ai_credits === "string" || typeof attrs.included_ai_credits === "number"
      ? String(attrs.included_ai_credits)
      : undefined
  const overageUsd =
    typeof attrs.overage_usd === "string" || typeof attrs.overage_usd === "number"
      ? String(attrs.overage_usd)
      : undefined

  return (
    <article className="flex flex-col gap-3 rounded-lg border border-border/80 bg-card/75 p-3.5 shadow-2xs">
      {/* Account Header */}
      <div className="flex flex-wrap items-start justify-between gap-2 border-b border-border/60 pb-2.5">
        <div className="flex flex-col gap-0.5">
          <div className="flex flex-wrap items-center gap-2">
            <h3 className="font-semibold text-foreground text-sm tracking-tight">{account.label || account.id}</h3>
            <Badge variant="outline" className="h-4.5 px-1.5 font-mono text-[10px] uppercase">
              {account.provider}
            </Badge>
            <Badge variant="secondary" className="h-4.5 px-1.5 font-mono text-[10px]">
              {account.kind}
            </Badge>
            {account.plan && (
              <Badge
                variant="outline"
                className="h-4.5 border-primary/40 bg-primary/10 px-1.5 font-mono text-[10px] text-primary"
              >
                plan: {account.plan}
              </Badge>
            )}
            {rateLimitTier && (
              <Badge variant="outline" className="h-4.5 px-1.5 font-mono text-[10px] text-muted-foreground">
                tier: {rateLimitTier}
              </Badge>
            )}
          </div>
          <div className="flex flex-wrap items-center gap-2 font-mono text-[10.5px] text-muted-foreground">
            <code>{account.id}</code>
            {account.observed_at && (
              <>
                <span>·</span>
                <span>observed {formatRelativeAge(account.observed_at)}</span>
              </>
            )}
            {account.collector && (
              <>
                <span>·</span>
                <span>collector: {account.collector}</span>
              </>
            )}
          </div>
        </div>

        {/* Health & Freshness Badges (Separate from Latest Error) */}
        <div className="flex flex-wrap items-center gap-1.5">
          <Badge variant="outline" className={`h-5 gap-1.5 px-2 font-mono text-[10px] ${hm.badgeClass}`}>
            <span className={`size-1.5 rounded-full ${hm.dotClass}`} />
            {hm.label}
          </Badge>
          {account.source_id && onInspectSource && (
            <Button
              variant="ghost"
              size="xs"
              className="h-5 px-1.5 font-mono text-[10px]"
              onClick={() => account.source_id && onInspectSource(account.source_id)}
              title="Inspect source health and raw capture"
            >
              <Icon name="database" size={11} />
              Source
            </Button>
          )}
        </div>
      </div>

      {/* Latest Error Banner (Kept Separate from Staleness per Section 5.1) */}
      {account.latest_error && (
        <div
          role="alert"
          className="flex items-start gap-2 rounded-md border border-destructive/45 bg-destructive/10 px-2.5 py-1.5 text-destructive text-xs"
        >
          <Icon name="triangle-exclamation-point" size={13} className="mt-0.5 shrink-0" />
          <div className="min-w-0 flex-1">
            <span className="font-semibold">Latest collector error: </span>
            <span className="font-mono text-[11px]">{account.latest_error}</span>
          </div>
        </div>
      )}

      {/* Key Account Attributes Strip (Reset Credits, Overage, Sign-in Expiry, Included AI Credits, Runway) */}
      {(resetCredits !== undefined ||
        overageEnabled ||
        signinExpiresAt ||
        includedAiCredits !== undefined ||
        overageUsd !== undefined ||
        account.runway_days != null ||
        account.low_balance) && (
        <div className="flex flex-wrap items-center gap-1.5">
          {resetCredits !== undefined && (
            <Badge variant="outline" className="border-chart-2/40 bg-chart-2/10 font-mono text-[10px] text-chart-2">
              <Icon name="rotate-ccw" size={10} />
              Reset credits: {String(resetCredits)}
            </Badge>
          )}
          {includedAiCredits !== undefined && (
            <Badge variant="outline" className="border-primary/40 bg-primary/10 font-mono text-[10px] text-primary">
              Included AI credits: {includedAiCredits}
            </Badge>
          )}
          {overageUsd !== undefined && (
            <Badge variant="outline" className="border-border bg-muted/40 font-mono text-[10px] text-muted-foreground">
              Overage USD: ${overageUsd}
            </Badge>
          )}
          {overageEnabled && (
            <Badge variant="outline" className="border-warning/40 bg-warning/10 font-mono text-[10px] text-warning">
              Extra usage / overage enabled
            </Badge>
          )}
          {signinExpiresAt && (
            <Badge variant="outline" className="border-border bg-muted/40 font-mono text-[10px] text-muted-foreground">
              Sign-in expires: {formatLocalTimestamp(signinExpiresAt)} ({formatCountdown(signinExpiresAt)})
            </Badge>
          )}
          {account.runway_days != null && (
            <Badge
              variant="outline"
              className={`font-mono text-[10px] ${
                account.low_balance || account.runway_days < 7
                  ? "border-destructive/45 bg-destructive/15 text-destructive"
                  : "border-success/40 bg-success/10 text-success"
              }`}
            >
              Runway: ~{formatNum(account.runway_days, 1)} days
            </Badge>
          )}
          {account.low_balance && (
            <Badge variant="destructive" className="font-mono text-[10px]">
              Low Balance Floor
            </Badge>
          )}
        </div>
      )}

      {/* Windows List */}
      {account.windows.length > 0 ? (
        <div className="flex flex-col gap-2">
          {account.windows.map((win) => (
            <WindowRow key={win.id} win={win} account={account} />
          ))}
        </div>
      ) : account.health !== "spend_only" ? (
        <div className="rounded-md border border-dashed border-border/70 bg-muted/20 px-3 py-2 font-mono text-[11px] text-muted-foreground">
          No quota or balance windows reported by collector (values: Unknown)
        </div>
      ) : null}

      {/* Spend Breakdown by Cost Kind (Never summed across different cost kinds) */}
      {(account.spend_by_cost_kind?.length ?? 0) > 0 && (
        <div className="flex flex-col gap-1.5 rounded-md border border-border/70 bg-muted/25 p-2.5">
          <div className="flex items-center justify-between">
            <span className="font-mono text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
              Observed Gateway Spend (Separated by Cost Kind)
            </span>
            <span className="font-mono text-[10px] text-muted-foreground">Never summed across cost kinds</span>
          </div>
          <div className="flex flex-col gap-1.5">
            {account.spend_by_cost_kind?.map((sp) => {
              const meta = COST_KIND_META[sp.cost_kind || "unclassified"] ?? COST_KIND_META.unclassified
              return (
                <div
                  key={sp.cost_kind || "unclassified"}
                  className="flex flex-wrap items-center justify-between gap-2 rounded-sm border border-border/60 bg-card/70 px-2.5 py-1.5 text-xs"
                >
                  <div className="flex flex-wrap items-center gap-1.5">
                    <Badge variant="outline" className={`h-4 px-1.5 font-mono text-[9.5px] ${meta.badgeClass}`}>
                      {meta.shortLabel}
                    </Badge>
                    <span className="font-mono text-[11px] text-muted-foreground">
                      {sp.events.toLocaleString()} events · {sp.known_tokens.toLocaleString()} tokens
                    </span>
                    {sp.unpriced_events > 0 && (
                      <span className="font-mono text-[10px] text-warning">({sp.unpriced_events} unpriced)</span>
                    )}
                  </div>
                  <span className="font-mono font-semibold text-foreground">
                    {sp.known_cost_usd != null ? formatUSD(sp.known_cost_usd) : "Cost: Unknown"}
                  </span>
                </div>
              )
            })}
          </div>
        </div>
      )}
    </article>
  )
}

export function UsageNowView({
  data,
  onInspectSource,
}: {
  data: UsageNowPayload
  onInspectSource?: (sourceId: string) => void
}) {
  const [kindFilter, setKindFilter] = useState<string>("all")
  const [providerFilter, setProviderFilter] = useState<string>("all")
  const [healthFilter, setHealthFilter] = useState<string>("all")
  const [search, setSearch] = useState<string>("")

  const providers = useMemo(() => {
    const s = new Set<string>()
    for (const a of data.accounts) if (a.provider) s.add(a.provider)
    return [...s].sort()
  }, [data.accounts])

  const filteredAccounts = useMemo(() => {
    const q = search.trim().toLowerCase()
    return data.accounts.filter((a) => {
      if (kindFilter !== "all" && a.kind !== kindFilter) return false
      if (providerFilter !== "all" && a.provider !== providerFilter) return false
      if (healthFilter !== "all") {
        if (healthFilter === "stale" && !a.stale && a.health !== "stale") return false
        if (healthFilter !== "stale" && a.health !== healthFilter) return false
      }
      if (q) {
        const hay =
          `${a.id} ${a.label} ${a.provider} ${a.kind} ${a.plan ?? ""} ${a.windows.map((w) => `${w.id} ${w.model ?? ""}`).join(" ")}`.toLowerCase()
        if (!hay.includes(q)) return false
      }
      return true
    })
  }, [data.accounts, kindFilter, providerFilter, healthFilter, search])

  const subscriptions = filteredAccounts.filter((a) => a.kind === "subscription")
  const balancesAndCredits = filteredAccounts.filter(
    (a) => a.kind === "credit" || (a.kind === "api" && a.health !== "spend_only"),
  )
  const spendOnlyAccounts = filteredAccounts.filter((a) => a.health === "spend_only")

  return (
    <div className="flex flex-col gap-5">
      {/* ==================== TOP STRIP: USE-IT-OR-LOSE-IT & NEXT RESETS ==================== */}
      <div className="grid grid-cols-1 gap-4 xl:grid-cols-12">
        {/* Left 7 cols: Use-It-Or-Lose-It Ranked Strip */}
        <section
          aria-label="Use-it-or-lose-it expiring subscription capacity"
          className="flex flex-col gap-2.5 rounded-lg border border-primary/40 bg-card/85 p-4 xl:col-span-7"
        >
          <div className="flex flex-wrap items-center justify-between gap-2 border-b border-border/60 pb-2">
            <div className="flex items-center gap-2">
              <span className="flex size-6 items-center justify-center rounded-sm border border-primary/40 bg-primary/15 text-primary">
                <Icon name="flame" size={13} />
              </span>
              <div>
                <h3 className="font-mono text-xs font-bold uppercase tracking-wider text-foreground">
                  Use-It-Or-Lose-It · Expiring Subscription Capacity
                </h3>
                <p className="text-[11px] text-muted-foreground">
                  Ranked by forfeitable quota × plan value × reset urgency — use this capacity before it resets.
                </p>
              </div>
            </div>
            <Badge variant="outline" className="border-primary/40 bg-primary/10 font-mono text-[10px] text-primary">
              {data.use_it_or_lose_it.length} ranked windows
            </Badge>
          </div>

          {data.use_it_or_lose_it.length === 0 ? (
            <p className="py-4 text-center font-mono text-xs text-muted-foreground">
              No expiring subscription windows currently at risk of forfeiture.
            </p>
          ) : (
            <div className="flex flex-col gap-2">
              {data.use_it_or_lose_it.slice(0, 6).map((item) => (
                <div
                  key={`${item.account_id}:${item.window_id}`}
                  className="flex flex-wrap items-center justify-between gap-3 rounded-md border border-border/75 bg-background/70 px-3 py-2 text-xs"
                >
                  <div className="flex min-w-0 flex-1 items-center gap-2.5">
                    <span className="flex size-5 shrink-0 items-center justify-center rounded-full border border-primary/40 bg-primary/15 font-mono text-[10px] font-bold text-primary">
                      #{item.rank}
                    </span>
                    <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                      <div className="flex flex-wrap items-center gap-1.5">
                        <span className="font-semibold text-foreground">{item.account_label || item.account_id}</span>
                        <span className="text-muted-foreground">·</span>
                        <span className="font-medium text-primary">{item.model_group || item.window_id}</span>
                        <code className="font-mono text-[10px] text-muted-foreground">({item.window_id})</code>
                        {item.reset_kind && (
                          <Badge variant="outline" className="h-4 px-1 font-mono text-[9px] uppercase">
                            {item.reset_kind} reset
                          </Badge>
                        )}
                      </div>
                      {(item.gateway_models?.length ?? 0) > 0 && (
                        <div className="flex flex-wrap items-center gap-1 font-mono text-[10px] text-muted-foreground">
                          <span>Route work to:</span>
                          {item.gateway_models?.slice(0, 4).map((m) => (
                            <code key={m} className="rounded-xs bg-muted px-1 py-0.5 text-foreground">
                              {m}
                            </code>
                          ))}
                        </div>
                      )}
                    </div>
                  </div>

                  <div className="flex shrink-0 items-center gap-3 font-mono">
                    <div className="text-right">
                      <div className="font-bold text-sm text-success">{formatPct(item.forfeitable_percent)} unused</div>
                      <div className="text-[10px] text-muted-foreground">
                        resets {formatCountdown(item.resets_at, item.seconds_to_reset)}
                      </div>
                    </div>
                    {item.urgency_score != null && (
                      <Badge variant="secondary" className="font-mono text-[10px]">
                        score {formatNum(item.urgency_score, 0)}
                      </Badge>
                    )}
                  </div>
                </div>
              ))}
            </div>
          )}
        </section>

        {/* Right 5 cols: Next Resets Countdown Rail */}
        <section
          aria-label="Upcoming window resets"
          className="flex flex-col gap-2.5 rounded-lg border border-border/80 bg-card/75 p-4 xl:col-span-5"
        >
          <div className="flex items-center justify-between gap-2 border-b border-border/60 pb-2">
            <div className="flex items-center gap-2">
              <span className="flex size-6 items-center justify-center rounded-sm border border-chart-2/40 bg-chart-2/15 text-chart-2">
                <Icon name="timer-reset" size={13} />
              </span>
              <div>
                <h3 className="font-mono text-xs font-bold uppercase tracking-wider text-foreground">
                  Next Window Resets
                </h3>
                <p className="text-[11px] text-muted-foreground">
                  Upcoming full and partial window resets across all subscriptions.
                </p>
              </div>
            </div>
            <Badge variant="secondary" className="font-mono text-[10px]">
              {data.next_resets.length} scheduled
            </Badge>
          </div>

          {data.next_resets.length === 0 ? (
            <p className="py-4 text-center font-mono text-xs text-muted-foreground">
              No upcoming window reset timestamps reported.
            </p>
          ) : (
            <div className="flex flex-col gap-1.5">
              {data.next_resets.slice(0, 7).map((r) => (
                <div
                  key={`${r.account_id}:${r.window_id}`}
                  className="flex items-center justify-between gap-2 rounded-md border border-border/65 bg-background/60 px-2.5 py-1.5 text-xs"
                >
                  <div className="flex min-w-0 flex-1 flex-col">
                    <div className="flex flex-wrap items-center gap-1.5">
                      <span className="font-semibold text-foreground">{r.account_label || r.account_id}</span>
                      <span className="text-muted-foreground">·</span>
                      <span className="truncate text-foreground/90">{r.model || r.window_id}</span>
                      {r.group && (
                        <Badge variant="outline" className="h-4 px-1 font-mono text-[9px] uppercase">
                          {r.group}
                        </Badge>
                      )}
                      {r.partial_reset && (
                        <Badge
                          variant="outline"
                          className="h-4 border-primary/40 bg-primary/10 px-1 font-mono text-[9px] text-primary"
                        >
                          partial
                        </Badge>
                      )}
                    </div>
                    <span className="font-mono text-[10px] text-muted-foreground">
                      {formatLocalTimestamp(r.resets_at)} · used: {formatValueWithUnit(r.used, r.unit)}
                    </span>
                  </div>
                  <Badge
                    variant="outline"
                    className="shrink-0 border-chart-2/40 bg-chart-2/10 font-mono text-[10.5px] text-chart-2"
                  >
                    {formatCountdown(r.resets_at, r.seconds_to_reset)}
                  </Badge>
                </div>
              ))}
            </div>
          )}
        </section>
      </div>

      {/* ==================== FILTER BAR (OPS ACTIVITY PATTERN) ==================== */}
      <div className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-border/80 bg-card/60 px-3.5 py-2.5 text-xs">
        <div className="flex flex-wrap items-center gap-2">
          <Select value={kindFilter} onValueChange={(v) => setKindFilter(String(v))}>
            <SelectTrigger size="sm" aria-label="Account kind" className="h-7 min-w-38 text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All kinds ({data.accounts.length})</SelectItem>
              <SelectItem value="subscription">Subscriptions</SelectItem>
              <SelectItem value="credit">Credits &amp; Prepaid</SelectItem>
              <SelectItem value="api">API &amp; Spend-Only</SelectItem>
            </SelectContent>
          </Select>

          <Select value={providerFilter} onValueChange={(v) => setProviderFilter(String(v))}>
            <SelectTrigger size="sm" aria-label="Provider" className="h-7 min-w-36 text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All providers</SelectItem>
              {providers.map((p) => (
                <SelectItem key={p} value={p}>
                  {p}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>

          <Select value={healthFilter} onValueChange={(v) => setHealthFilter(String(v))}>
            <SelectTrigger size="sm" aria-label="Health" className="h-7 min-w-36 text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All health states</SelectItem>
              <SelectItem value="ok">Live · OK</SelectItem>
              <SelectItem value="spend_only">Spend Only</SelectItem>
              <SelectItem value="stale">Stale</SelectItem>
              <SelectItem value="error">Error</SelectItem>
            </SelectContent>
          </Select>
        </div>

        <div className="flex items-center gap-2">
          <Input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Filter accounts, providers, or model windows…"
            className="h-7 w-64 text-xs"
          />
          {(kindFilter !== "all" || providerFilter !== "all" || healthFilter !== "all" || search) && (
            <Button
              variant="ghost"
              size="xs"
              onClick={() => {
                setKindFilter("all")
                setProviderFilter("all")
                setHealthFilter("all")
                setSearch("")
              }}
            >
              Reset
            </Button>
          )}
        </div>
      </div>

      {/* ==================== SECTION 1: SUBSCRIPTIONS ==================== */}
      {subscriptions.length > 0 && (
        <section className="flex flex-col gap-3">
          <div className="flex items-center justify-between border-b border-border/60 pb-1.5">
            <div className="flex items-center gap-2">
              <Icon name="layers" size={14} className="text-primary" />
              <h3 className="font-mono text-xs font-bold uppercase tracking-wider text-foreground">
                Subscriptions ({subscriptions.length})
              </h3>
            </div>
            <span className="font-mono text-[11px] text-muted-foreground">
              Per-model quota windows, reset countdowns, and parent-blocked shading
            </span>
          </div>
          <div className="grid grid-cols-1 gap-3.5 lg:grid-cols-2">
            {subscriptions.map((acct) => (
              <AccountCard key={acct.id} account={acct} onInspectSource={onInspectSource} />
            ))}
          </div>
        </section>
      )}

      {/* ==================== SECTION 2: BALANCE, CREDIT & METERED API ACCOUNTS ==================== */}
      {balancesAndCredits.length > 0 && (
        <section className="flex flex-col gap-3">
          <div className="flex items-center justify-between border-b border-border/60 pb-1.5">
            <div className="flex items-center gap-2">
              <Icon name="wallet" size={14} className="text-chart-2" />
              <h3 className="font-mono text-xs font-bold uppercase tracking-wider text-foreground">
                Balances, Credits &amp; Prepaid API Accounts ({balancesAndCredits.length})
              </h3>
            </div>
            <span className="font-mono text-[11px] text-muted-foreground">
              Remaining balances, credits, and runway estimates
            </span>
          </div>
          <div className="grid grid-cols-1 gap-3.5 lg:grid-cols-2">
            {balancesAndCredits.map((acct) => (
              <AccountCard key={acct.id} account={acct} onInspectSource={onInspectSource} />
            ))}
          </div>
        </section>
      )}

      {/* ==================== SECTION 3: SPEND-ONLY GATEWAY ACCOUNTS ==================== */}
      {spendOnlyAccounts.length > 0 && (
        <section className="flex flex-col gap-3">
          <div className="flex items-center justify-between border-b border-border/60 pb-1.5">
            <div className="flex items-center gap-2">
              <Icon name="activity" size={14} className="text-success" />
              <h3 className="font-mono text-xs font-bold uppercase tracking-wider text-foreground">
                Spend-Only Gateway Accounts ({spendOnlyAccounts.length})
              </h3>
            </div>
            <span className="font-mono text-[11px] text-muted-foreground">
              Gateway-metered spend separated by cost_kind (no quota collector)
            </span>
          </div>
          <div className="grid grid-cols-1 gap-3.5 lg:grid-cols-2">
            {spendOnlyAccounts.map((acct) => (
              <AccountCard key={acct.id} account={acct} onInspectSource={onInspectSource} />
            ))}
          </div>
        </section>
      )}
    </div>
  )
}
