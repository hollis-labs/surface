import { Icon, useEvent, type ViewProps } from "@hollis-labs/surface-page/sdk"
import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Spinner } from "@/components/ui/spinner"
import { ActivityRow, SOURCE_LABEL } from "./activity-row"
import { type Card, type GlobalFeed, message, opsClient, STAGES, stageLabel } from "./api"

/** All levels of the board; the filters are desk args so the agent sees the same view. */
export function BoardActivity({ win, desk, api, workstreams }: ViewProps & { workstreams: Card[] }) {
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
  return (
    <section
      aria-label="Activity across workstreams"
      className="flex min-h-0 flex-1 flex-col gap-2 overflow-y-auto p-3"
    >
      <h2 className="text-sm font-medium">Activity across all workstreams</h2>
      <div className="flex flex-wrap items-center gap-2">
        <Select value={workstream || "all"} onValueChange={(v) => set("workstream", String(v))}>
          <SelectTrigger aria-label="Workstream">
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
          <SelectTrigger aria-label="Stage">
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
          <SelectTrigger aria-label="Source">
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
        <Label className="flex items-center gap-2">
          Since (UTC)
          <Input
            type="datetime-local"
            value={since.slice(0, 16)}
            onChange={(e) => set("since", e.target.value ? new Date(`${e.target.value}Z`).toISOString() : "")}
          />
        </Label>
        <Button variant="ghost" size="sm" onClick={() => void load(true)} disabled={busy}>
          {busy ? <Spinner /> : <Icon name="refresh-cw" />}Refresh activity
        </Button>
      </div>
      {error && (
        <p role="alert" className="text-destructive text-sm">
          {error}
        </p>
      )}
      {[...new Set(feed?.errors ?? [])].map((e) => (
        <p key={e} className="text-destructive text-xs">
          {e}
        </p>
      ))}
      {feed && feed.pending > 0 && (
        <p className="text-muted-foreground text-xs">
          {feed.pending} workstreams pending refresh. Cached rows remain visible; the next batch refreshes while this
          window is open.
        </p>
      )}
      {feed?.truncated && (
        <p className="text-muted-foreground text-xs">
          Showing the newest 300 matching rows. Narrow the filters for more detail.
        </p>
      )}
      {!feed && busy && <Spinner />}
      {feed?.entries.length === 0 && (
        <p className="text-muted-foreground text-sm">No activity matches these filters.</p>
      )}
      <ol className="flex flex-col">
        {feed?.entries.map((e) => {
          const key = JSON.stringify(e)
          return (
            <li key={key} className="border-b py-1 last:border-0">
              <ActivityRow e={e} id="" open={open === key} toggle={() => setOpen(open === key ? null : key)} />
            </li>
          )
        })}
      </ol>
    </section>
  )
}
