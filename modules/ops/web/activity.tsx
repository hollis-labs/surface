import { Icon, useEvent, type ViewProps } from "@hollis-labs/surface-page/sdk"
import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Spinner } from "@/components/ui/spinner"
import { ActivityRow, dayLabel, EpisodeHeaderBar, SOURCE_LABEL, spanBetween } from "./activity-row"
import { age, type Card, type Entry, type GlobalFeed, message, opsClient, STAGES, stageLabel } from "./api"

/** All levels of the board; the filters are desk args so the agent sees the same view. */
export function BoardActivity({
  win,
  desk,
  api,
  workstreams,
  onOpenWorkstream,
}: ViewProps & {
  workstreams: Card[]
  onOpenWorkstream?: (id: string, newWindow?: boolean) => void
}) {
  const ops = useMemo(() => opsClient(api), [api])
  const workstream = String(win.args.workstream ?? "")
  const stage = String(win.args.stage ?? "")
  const source = String(win.args.source ?? "")
  const since = String(win.args.since ?? "")
  const filters = useMemo(() => ({ workstream, stage, source, since }), [workstream, stage, source, since])
  const [feed, setFeed] = useState<GlobalFeed | null>(null)
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)
  const [open, setOpen] = useState<string | null>(null)
  const request = useRef(0)

  const byID = useMemo(() => new Map(workstreams.map((w) => [w.id, w])), [workstreams])

  const load = useCallback(
    async (refresh = false) => {
      const seq = ++request.current
      setBusy(true)
      try {
        const next = await ops.globalActivity(filters, refresh)
        if (seq === request.current) {
          setFeed(next)
          setError("")
        }
      } catch (e) {
        if (seq === request.current) setError(message(e))
      } finally {
        if (seq === request.current) setBusy(false)
      }
    },
    [ops, filters],
  )
  useEffect(() => {
    setFeed(null)
    void load()
    return () => {
      request.current++
    }
  }, [load])
  useEvent("ops.activity", () => void load())
  useEvent("ops.changed", () => void load())

  const set = (key: string, value: string) =>
    void desk.setArgs(win.id, { ...win.args, [key]: value === "all" ? "" : value })

  const activeWS = workstream ? byID.get(workstream) : undefined
  const routeLeft = activeWS ? activeWS.title : "All Workstreams"
  const routeRight = stage ? stageLabel(stage) : source ? (SOURCE_LABEL[source] ?? source) : "Live Activity"
  const inFlightCount = useMemo(() => workstreams.filter((w) => w.stage === "in_flight").length, [workstreams])

  // Group entries into day episodes for the itinerary layout
  const episodes = useMemo(() => {
    const entries = feed?.entries ?? []
    const groups: { day: string; entries: Entry[] }[] = []
    for (const e of entries) {
      const day = dayLabel(e.at)
      const last = groups[groups.length - 1]
      if (last && last.day === day) {
        last.entries.push(e)
      } else {
        groups.push({ day, entries: [e] })
      }
    }
    return groups
  }, [feed])

  return (
    <section
      aria-label="Activity across workstreams"
      className="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto p-4"
    >
      <div className="mx-auto flex w-full max-w-4xl flex-col gap-4 rounded-lg border border-border/80 bg-card/40 p-5">
        {/* Flight-Itinerary Style Header */}
        <header className="flex flex-col gap-2 border-b border-dashed border-border/80 pb-4">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div className="flex flex-wrap items-center gap-2.5">
              <h2 className="font-semibold text-foreground text-lg tracking-tight">{routeLeft}</h2>
              <Icon name="arrow-left-right" size={16} className="text-muted-foreground" />
              <span className="font-semibold text-lg text-primary tracking-tight">{routeRight}</span>
              <span className="sr-only">Activity across all workstreams</span>
            </div>
            <Button variant="outline" size="sm" onClick={() => void load(true)} disabled={busy}>
              {busy ? <Spinner /> : <Icon name="refresh-cw" size={13} />}
              Refresh activity
            </Button>
          </div>

          <div className="flex flex-wrap items-center gap-2.5 text-muted-foreground text-xs">
            <span>
              Workstreams: <strong className="font-medium text-foreground">{workstreams.length} tracked</strong> (
              {inFlightCount} in flight)
            </span>
            <span className="text-border">|</span>
            <span>
              Events: <strong className="font-medium text-foreground">{feed?.entries.length ?? 0}</strong>
            </span>
            <span className="text-border">|</span>
            <span>
              All timestamps in local time
              {feed?.fetched_at ? ` · synced ${age(feed.fetched_at)} ago` : ""}
            </span>
          </div>

          {/* Filter Bar */}
          <div className="mt-2 flex flex-wrap items-center gap-2">
            <Select value={workstream || "all"} onValueChange={(v) => set("workstream", String(v))}>
              <SelectTrigger size="sm" aria-label="Workstream">
                <SelectValue>
                  {(v: string) => (v === "all" ? "All workstreams" : (workstreams.find((w) => w.id === v)?.title ?? v))}
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All workstreams</SelectItem>
                {workstreams.map((w) => (
                  <SelectItem key={w.id} value={w.id}>
                    {w.title}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Select value={stage || "all"} onValueChange={(v) => set("stage", String(v))}>
              <SelectTrigger size="sm" aria-label="Stage">
                <SelectValue>{(v: string) => (v === "all" ? "All stages" : stageLabel(v))}</SelectValue>
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All stages</SelectItem>
                {STAGES.map((s) => (
                  <SelectItem key={s} value={s}>
                    {stageLabel(s)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Select value={source || "all"} onValueChange={(v) => set("source", String(v))}>
              <SelectTrigger size="sm" aria-label="Source">
                <SelectValue>{(v: string) => (v === "all" ? "All sources" : SOURCE_LABEL[v])}</SelectValue>
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All sources</SelectItem>
                {Object.entries(SOURCE_LABEL).map(([s, title]) => (
                  <SelectItem key={s} value={s}>
                    {title}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Label className="flex items-center gap-1.5 text-xs text-muted-foreground">
              Since (UTC)
              <Input
                type="datetime-local"
                className="h-7 w-44 text-xs"
                value={since.slice(0, 16)}
                onChange={(e) => set("since", e.target.value ? new Date(`${e.target.value}Z`).toISOString() : "")}
              />
            </Label>
          </div>
        </header>

        {/* Status / Warning Callout Banner */}
        {(error || (feed?.errors?.length ?? 0) > 0 || (feed && feed.pending > 0) || feed?.truncated) && (
          <div className="rounded-md border border-border/60 bg-muted/45 px-3.5 py-2.5 text-xs">
            <span className="font-semibold text-warning">Feed Status & Sync Notes</span>
            <ul className="mt-1 flex list-disc flex-col gap-1 pl-4 text-muted-foreground">
              {error && (
                <li role="alert" className="text-destructive">
                  {error}
                </li>
              )}
              {[...new Set(feed?.errors ?? [])].map((e) => (
                <li key={e} className="text-destructive">
                  {e}
                </li>
              ))}
              {feed && feed.pending > 0 && (
                <li>
                  {feed.pending} workstreams pending refresh. Cached rows remain visible; the next batch refreshes while
                  this window is open.
                </li>
              )}
              {feed?.truncated && <li>Showing the newest 300 matching rows. Narrow the filters for more detail.</li>}
            </ul>
          </div>
        )}

        {!feed && busy && (
          <div className="flex items-center justify-center py-8">
            <Spinner />
          </div>
        )}

        {feed?.entries.length === 0 && (
          <p className="py-8 text-center text-muted-foreground text-sm">No activity matches these filters.</p>
        )}

        {/* Episode Groups & 3-Column Itinerary Timeline */}
        <div className="flex flex-col gap-5">
          {episodes.map((ep) => {
            const uniqueWS = new Set(ep.entries.map((e) => e.workstream)).size
            const newest = ep.entries[0]?.at
            const oldest = ep.entries[ep.entries.length - 1]?.at
            const windowSpan = ep.entries.length > 1 ? spanBetween(newest, oldest) : ""
            return (
              <div key={ep.day} className="flex flex-col gap-2">
                <EpisodeHeaderBar
                  badgeText={stage ? stageLabel(stage) : "Activity"}
                  badgeTone={stage === "in_flight" || stage === "complete" ? "success" : "primary"}
                  dateText={ep.day}
                  routeText={`${uniqueWS} ${uniqueWS === 1 ? "workstream" : "workstreams"}`}
                  durationText={windowSpan || undefined}
                  countText={`${ep.entries.length} ${ep.entries.length === 1 ? "event" : "events"}`}
                />
                <ol className="flex flex-col">
                  {ep.entries.map((e, idx) => {
                    const key = JSON.stringify(e)
                    const nextEntry = ep.entries[idx + 1]
                    return (
                      <li key={key}>
                        <ActivityRow
                          e={e}
                          id=""
                          open={open === key}
                          toggle={() => setOpen(open === key ? null : key)}
                          workstreamCard={byID.get(e.workstream)}
                          nextAt={nextEntry?.at}
                          onOpenWorkstream={onOpenWorkstream}
                        />
                      </li>
                    )
                  })}
                </ol>
              </div>
            )
          })}
        </div>
      </div>
    </section>
  )
}
