/**
 * Usage Monitor — View 4: SOURCES (`modules/usage/web/sources.tsx`).
 *
 * Implements Section 5.4 of `registry:document:architecture-usage-intelligence`:
 * - Read-only list of all accounts and collector sources, showing:
 *   - Collector type (`agy`, `claude`, `codex`, `billing`, `opencode`, `push`, `gateway`)
 *   - Health & freshness status (`ok`, `spend_only`, `stale`, `error`), with latest error separate from staleness
 *   - Last success / observed timestamp and collection sequence
 *   - Raw capture inspection modal (fetches `/api/m/usage/sources/{id}/raw` server-side)
 */
import { Icon } from "@hollis-labs/surface-page/sdk"
import { useCallback, useEffect, useMemo, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Spinner } from "@/components/ui/spinner"
import { formatLocalTimestamp, formatRelativeAge, healthMeta, type UsageAccount, type UsageSourceItem } from "./types"

export function UsageSourcesView({
  sources,
  accounts,
  initialSourceId,
  onClearInitialSource,
}: {
  sources: UsageSourceItem[]
  accounts: UsageAccount[]
  initialSourceId?: string | null
  onClearInitialSource?: () => void
}) {
  const [search, setSearch] = useState("")
  const [rawSource, setRawSource] = useState<UsageSourceItem | null>(null)
  const [rawJson, setRawJson] = useState<string>("")
  const [rawLoading, setRawLoading] = useState(false)
  const [rawError, setRawError] = useState("")

  const inspectRaw = useCallback(async (src: UsageSourceItem) => {
    setRawSource(src)
    setRawLoading(true)
    setRawError("")
    setRawJson("")
    try {
      const resp = await fetch(src.raw_url || `/api/m/usage/sources/${encodeURIComponent(src.id)}/raw`)
      const text = await resp.text()
      if (!resp.ok) {
        setRawError(`HTTP ${resp.status}: ${text}`)
      } else {
        try {
          setRawJson(JSON.stringify(JSON.parse(text), null, 2))
        } catch {
          setRawJson(text)
        }
      }
    } catch (e) {
      setRawError(e instanceof Error ? e.message : String(e))
    } finally {
      setRawLoading(false)
    }
  }, [])

  useEffect(() => {
    if (!initialSourceId) return
    const match = sources.find((s) => s.id === initialSourceId || s.account_id === initialSourceId)
    if (match) {
      void inspectRaw(match)
      onClearInitialSource?.()
    }
  }, [initialSourceId, sources, onClearInitialSource, inspectRaw])

  const filteredSources = useMemo(() => {
    const q = search.trim().toLowerCase()
    if (!q) return sources
    return sources.filter((s) =>
      `${s.id} ${s.account_id} ${s.account_label ?? ""} ${s.provider ?? ""} ${s.collector} ${s.health}`
        .toLowerCase()
        .includes(q),
    )
  }, [sources, search])

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-border/80 bg-card/65 px-3.5 py-2.5 text-xs">
        <div className="flex items-center gap-2">
          <Icon name="database" size={14} className="text-primary" />
          <span className="font-semibold text-foreground">
            Station Usage Monitor Sources ({sources.length}) &amp; Accounts ({accounts.length})
          </span>
          <span className="text-muted-foreground">
            · Read-only collector health, freshness, and raw payload inspection
          </span>
        </div>
        <Input
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="Filter sources by id, account, collector, or health…"
          className="h-7 w-72 text-xs"
        />
      </div>

      <div className="overflow-x-auto rounded-lg border border-border/80 bg-card/75">
        <table className="w-full border-collapse text-xs">
          <thead className="border-b border-border bg-muted/50 font-mono text-[10px] text-muted-foreground uppercase">
            <tr>
              <th className="px-3 py-2 text-left">Source ID</th>
              <th className="px-3 py-2 text-left">Account</th>
              <th className="px-3 py-2 text-left">Collector</th>
              <th className="px-3 py-2 text-left">Health &amp; Freshness</th>
              <th className="px-3 py-2 text-left">Last Observation</th>
              <th className="px-3 py-2 text-right">Windows</th>
              <th className="px-3 py-2 text-right">Raw Capture</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-border/60">
            {filteredSources.map((src) => {
              const hm = healthMeta(src.health, src.stale)
              return (
                <tr key={src.id} className="hover:bg-muted/25">
                  <td className="px-3 py-2 font-mono font-semibold text-foreground">{src.id}</td>
                  <td className="px-3 py-2">
                    <div className="flex flex-wrap items-center gap-1.5">
                      <span className="font-medium text-foreground">{src.account_label || src.account_id}</span>
                      <code className="font-mono text-[10px] text-muted-foreground">({src.account_id})</code>
                    </div>
                  </td>
                  <td className="px-3 py-2">
                    <Badge variant="secondary" className="h-4.5 px-1.5 font-mono text-[10px]">
                      {src.collector || "push"}
                    </Badge>
                  </td>
                  <td className="px-3 py-2">
                    <div className="flex flex-col gap-1">
                      <Badge
                        variant="outline"
                        className={`h-5 w-fit gap-1.5 px-2 font-mono text-[10px] ${hm.badgeClass}`}
                      >
                        <span className={`size-1.5 rounded-full ${hm.dotClass}`} />
                        {hm.label}
                      </Badge>
                      {src.latest_error && (
                        <span className="font-mono text-[10.5px] text-destructive">Error: {src.latest_error}</span>
                      )}
                    </div>
                  </td>
                  <td className="px-3 py-2 font-mono text-[11px] text-muted-foreground">
                    {src.observed_at ? (
                      <span>
                        {formatRelativeAge(src.observed_at)} ({formatLocalTimestamp(src.observed_at)})
                      </span>
                    ) : (
                      "Unknown"
                    )}
                  </td>
                  <td className="px-3 py-2 text-right font-mono">
                    {src.window_count != null ? src.window_count : "Unknown"}
                  </td>
                  <td className="px-3 py-2 text-right">
                    <Button
                      variant="outline"
                      size="xs"
                      className="h-6 font-mono text-[10.5px]"
                      onClick={() => void inspectRaw(src)}
                    >
                      <Icon name="eye" size={11} />
                      View Raw
                    </Button>
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>

      {/* Raw Source Capture Inspector Modal */}
      <Dialog open={rawSource !== null} onOpenChange={(open) => !open && setRawSource(null)}>
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle className="font-mono text-sm">
              Raw Collector Capture · {rawSource?.id} ({rawSource?.account_id})
            </DialogTitle>
            <DialogDescription className="font-mono text-xs">
              Proxied server-side from /v1/sources/{rawSource?.id}/raw on Station Usage Monitor
            </DialogDescription>
          </DialogHeader>
          {rawLoading ? (
            <div className="flex items-center justify-center py-8">
              <Spinner />
            </div>
          ) : rawError ? (
            <div className="rounded-md border border-destructive/45 bg-destructive/10 p-3 font-mono text-destructive text-xs">
              {rawError}
            </div>
          ) : (
            <pre className="max-h-96 overflow-auto rounded-md border border-border bg-muted/40 p-3 font-mono text-[11px] text-foreground">
              {rawJson || "No raw capture payload available."}
            </pre>
          )}
        </DialogContent>
      </Dialog>
    </div>
  )
}
