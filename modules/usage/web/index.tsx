/**
 * Surface Usage Monitor Module Web UI (`modules/usage/web/index.tsx`).
 *
 * Implements the four Usage Monitor views specified in `registry:document:architecture-usage-intelligence`:
 * - `Now`: Use-it-or-lose-it strip, next resets countdown rail, subscription window cards with
 *   parent-blocked shading, balance & credit accounts with runway, spend-only gateway accounts.
 * - `Trends`: Per-window usage curves with previous-period overlays, captured vs forfeited bars,
 *   spend stacked by provider/model split strictly by `cost_kind`, and balance burn-down.
 * - `Intelligence`: Actionable recommendations with evidence drill-down, burn & exhaustion forecasts,
 *   per-subscription value matrix, and spend anomalies / overage watch.
 * - `Sources`: Read-only collector health, freshness, last success/error, and raw capture viewer.
 */
import { Icon, type ModuleWeb, useEvent, type ViewProps } from "@hollis-labs/surface-page/sdk"
import { useCallback, useEffect, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Spinner } from "@/components/ui/spinner"
import { UsageIntelligenceView } from "./intelligence"
import { UsageNowView } from "./now"
import { UsageSourcesView } from "./sources"
import { UsageTrendsView } from "./trends"
import {
  formatRelativeAge,
  type UsageIntelligencePayload,
  type UsageNowPayload,
  type UsageTrendsPayload,
} from "./types"

type UsageTabId = "now" | "trends" | "intelligence" | "sources"

const TABS: { id: UsageTabId; label: string; icon: string; subtitle: string }[] = [
  {
    id: "now",
    label: "Now",
    icon: "flame",
    subtitle: "Use-it-or-lose-it expiring capacity, reset countdowns, and all monitored accounts",
  },
  {
    id: "trends",
    label: "Trends",
    icon: "trending-up",
    subtitle: "Period overlays, captured vs. forfeited capacity, spend by cost_kind, and burn-down",
  },
  {
    id: "intelligence",
    label: "Intelligence",
    icon: "sparkles",
    subtitle: "Recommendations with evidence, exhaustion forecasts, anomalies, and subscription value",
  },
  {
    id: "sources",
    label: "Sources",
    icon: "database",
    subtitle: "Read-only collector health, freshness, latest errors, and raw captures",
  },
]

export function UsageMonitorView({ win, desk, api }: ViewProps) {
  const argTab = typeof win?.args?.tab === "string" ? (win.args.tab as UsageTabId) : undefined
  const [tab, setTab] = useState<UsageTabId>(argTab && TABS.some((t) => t.id === argTab) ? argTab : "now")
  const [inspectSourceId, setInspectSourceId] = useState<string | null>(null)

  const [nowData, setNowData] = useState<UsageNowPayload | null>(null)
  const [trendsData, setTrendsData] = useState<UsageTrendsPayload | null>(null)
  const [intelData, setIntelData] = useState<UsageIntelligencePayload | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")

  useEffect(() => {
    if (argTab && TABS.some((t) => t.id === argTab)) {
      setTab(argTab)
    }
  }, [argTab])

  const selectTab = (next: UsageTabId) => {
    setTab(next)
    if (win?.id) {
      void desk.setArgs(win.id, { ...win.args, tab: next })
    }
  }

  const loadAll = useCallback(
    async (refresh = false) => {
      setBusy(true)
      setError("")
      try {
        const q = refresh ? "?refresh=1" : ""
        const [n, tr, intel] = await Promise.all([
          api.get<UsageNowPayload>(`/now${q}`),
          api.get<UsageTrendsPayload>(`/trends${q}`),
          api.get<UsageIntelligencePayload>(`/intelligence${q}`),
        ])
        setNowData(n)
        setTrendsData(tr)
        setIntelData(intel)
      } catch (e) {
        setError(e instanceof Error ? e.message : String(e))
      } finally {
        setBusy(false)
      }
    },
    [api],
  )

  useEffect(() => {
    void loadAll(false)
  }, [loadAll])

  useEvent("usage.changed", () => void loadAll(false))

  const activeMeta = TABS.find((t) => t.id === tab) ?? TABS[0]

  return (
    <div className="flex h-full min-h-0 flex-col bg-background text-xs">
      {/* Invariant Top Subnav Bar */}
      <header className="flex shrink-0 flex-wrap items-center justify-between gap-3 border-b border-border bg-card/65 px-4 py-2.5">
        <div className="flex flex-wrap items-center gap-3">
          <div className="flex items-center gap-2">
            <span className="flex size-7 items-center justify-center rounded-sm border border-primary/40 bg-primary/15 text-primary">
              <Icon name="gauge" size={15} />
            </span>
            <div>
              <div className="flex items-center gap-2">
                <h1 className="font-bold text-foreground text-sm tracking-tight">Usage Monitor</h1>
                <Badge
                  variant="outline"
                  className={
                    nowData?.live
                      ? "border-success/40 bg-success/10 font-mono text-[10px] text-success"
                      : "border-chart-2/40 bg-chart-2/10 font-mono text-[10px] text-chart-2"
                  }
                >
                  {nowData?.live ? "Station v1 · Live" : "Station v1"}
                </Badge>
              </div>
              <p className="text-[11px] text-muted-foreground">{activeMeta.subtitle}</p>
            </div>
          </div>

          {/* 4-View Switcher (Now, Trends, Intelligence, Sources) */}
          <nav
            aria-label="Usage Monitor views"
            className="flex items-center gap-1 rounded-sm border border-border/80 bg-muted/35 p-0.5"
          >
            {TABS.map((t) => {
              const active = tab === t.id
              return (
                <Button
                  key={t.id}
                  variant={active ? "secondary" : "ghost"}
                  size="xs"
                  className="h-6.5 gap-1.5 px-2.5 font-mono text-xs"
                  onClick={() => selectTab(t.id)}
                >
                  <Icon name={t.icon} size={12} className={active ? "text-primary" : "text-muted-foreground"} />
                  <span>{t.label}</span>
                </Button>
              )
            })}
          </nav>
        </div>

        <div className="flex items-center gap-2">
          {nowData && (
            <span className="font-mono text-[11px] text-muted-foreground">
              {nowData.accounts.length} accounts · synced {formatRelativeAge(nowData.fetched_at)}
            </span>
          )}
          <Button variant="outline" size="xs" onClick={() => void loadAll(true)} disabled={busy}>
            {busy ? <Spinner /> : <Icon name="refresh-cw" size={12} />}
            Refresh
          </Button>
        </div>
      </header>

      {/* Main Scrollable Content Area */}
      <main className="min-h-0 flex-1 overflow-y-auto p-4">
        <div className="mx-auto flex w-full max-w-6xl flex-col gap-4">
          {(error || nowData?.error) && (
            <div
              role="alert"
              className="flex items-center justify-between gap-2 rounded-md border border-destructive/45 bg-destructive/10 px-3.5 py-2 text-destructive text-xs"
            >
              <span>{error || nowData?.error}</span>
              <Button variant="outline" size="xs" onClick={() => void loadAll(true)}>
                Retry
              </Button>
            </div>
          )}

          {!nowData && busy && (
            <div className="flex items-center justify-center py-12">
              <Spinner />
            </div>
          )}

          {tab === "now" && nowData && (
            <UsageNowView
              data={nowData}
              onInspectSource={(sourceId) => {
                setInspectSourceId(sourceId)
                selectTab("sources")
              }}
            />
          )}

          {tab === "trends" && trendsData && <UsageTrendsView data={trendsData} />}

          {tab === "intelligence" && intelData && <UsageIntelligenceView data={intelData} />}

          {tab === "sources" && nowData && (
            <UsageSourcesView
              sources={nowData.sources}
              accounts={nowData.accounts}
              initialSourceId={inspectSourceId}
              onClearInitialSource={() => setInspectSourceId(null)}
            />
          )}
        </div>
      </main>
    </div>
  )
}

export const web: ModuleWeb = {
  views: {
    "usage/main": UsageMonitorView,
  },
}
export default web
