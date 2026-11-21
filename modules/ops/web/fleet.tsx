import type { ViewProps } from "@hollis-labs/surface-page/sdk"
import { useEffect, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Spinner } from "@/components/ui/spinner"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { message, time } from "./api"
import { SessionRowView } from "./session-row"
import type { SessionRow } from "./sessions"

interface Metrics {
  cpu_percent?: number
  memory_used?: number
  disks?: { mount: string; used: number | null; total: number | null }[]
  cpu?: number
  load1?: number
  load5?: number
  load15?: number
  memory_available?: number
  memory_total?: number
  disk_free?: number
  disk_total?: number
}
interface Host {
  name: string
  role: string
  state: string
  last_seen?: string
  probed_at?: string
  stale: boolean
  metrics: Metrics
}
interface FleetData {
  source: "snapshot" | "inventory"
  hosts: Host[]
  host?: string
  samples: (Metrics & { at: string; count: number; sample_at?: string })[]
  sessions: SessionRow[]
  fetched_at: string
  truncated: boolean
  warning?: string
  sessions_warning?: string
  sessions_truncated: boolean
  unknown_host_sessions: number
}
const number = (n?: number | null) =>
  n == null ? "Unknown" : n.toLocaleString(undefined, { maximumFractionDigits: 2 })
const bytes = (n?: number | null) => (n == null ? "Unknown" : `${number(n / 1024 ** 3)} GiB`)

export function Fleet({ win, desk, api }: ViewProps) {
  const host = typeof win.args.host === "string" ? win.args.host : ""
  const [data, setData] = useState<FleetData | null>(null)
  const [error, setError] = useState("")
  useEffect(() => {
    let stopped = false
    let timer: ReturnType<typeof setTimeout>
    setData(null)
    setError("")
    const load = async () => {
      try {
        const next = await api.get<FleetData>(`fleet?host=${encodeURIComponent(host)}`)
        if (!stopped) {
          setData(next)
          setError("")
        }
      } catch (e) {
        if (!stopped) setError(message(e))
      } finally {
        // No overlapping requests, and no timer once the Fleet view unmounts.
        if (!stopped) timer = setTimeout(() => void load(), 30000)
      }
    }
    void load()
    return () => {
      stopped = true
      clearTimeout(timer)
    }
  }, [api, host])
  const selected = data?.hosts.find((h) => h.name === host)
  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3 overflow-auto p-3">
      {error && (
        <p role="alert" className="text-destructive text-sm">
          {error} Displayed observations may be out of date.
        </p>
      )}
      {!data && !error && <Spinner />}
      {data && (
        <>
          <p className="text-muted-foreground text-xs">
            Observed {time(data.fetched_at)} · refreshes every 30 seconds while open
          </p>
          {data.truncated && <p className="text-sm">Inventory is limited to 100 machines.</p>}
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Machine</TableHead>
                <TableHead>Role</TableHead>
                <TableHead>Health</TableHead>
                <TableHead>1m load</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {data.hosts.map((h) => (
                <TableRow key={h.name} data-state={h.name === host ? "selected" : undefined}>
                  <TableCell>
                    <Button
                      variant={h.name === host ? "secondary" : "ghost"}
                      size="sm"
                      onClick={() => void desk.setArgs(win.id, { ...win.args, host: h.name })}
                    >
                      {h.name}
                    </Button>
                  </TableCell>
                  <TableCell>{h.role || "Unknown"}</TableCell>
                  <TableCell>
                    <Badge variant={h.state === "online" ? "default" : "secondary"}>{h.state}</Badge>{" "}
                    {h.stale && <Badge variant="outline">Stale</Badge>}
                  </TableCell>
                  <TableCell>{number(h.metrics.load1)}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
          {data.hosts.length === 0 && <p>No machines reported by Cerberus.</p>}
          {!host && data.hosts.length > 0 && (
            <p className="text-muted-foreground text-sm">Select a machine to see its agents and recent health.</p>
          )}
          {selected && (
            <section aria-label={`${host} details`} className="flex flex-col gap-3">
              <h3 className="font-semibold">{selected.name}</h3>
              <p className="text-muted-foreground text-xs">
                Last seen {time(selected.last_seen) || "unknown"} · health observed{" "}
                {time(selected.probed_at) || "unknown"}
                {selected.stale ? " · stale observation" : ""}
              </p>
              <dl className="grid grid-cols-2 gap-3 md:grid-cols-4">
                <div>
                  <dt>CPU utilization</dt>
                  <dd>
                    {selected.metrics.cpu_percent == null ? "Unknown" : `${number(selected.metrics.cpu_percent)}%`}
                  </dd>
                  <dt>CPU cores</dt>
                  <dd>{number(selected.metrics.cpu)}</dd>
                </div>
                <div>
                  <dt>Load 1 / 5 / 15m</dt>
                  <dd>
                    {number(selected.metrics.load1)} / {number(selected.metrics.load5)} /{" "}
                    {number(selected.metrics.load15)}
                  </dd>
                </div>
                <div>
                  <dt>Memory {data.source === "snapshot" ? "used" : "available"} / total</dt>
                  <dd>
                    {bytes(
                      data.source === "snapshot" ? selected.metrics.memory_used : selected.metrics.memory_available,
                    )}{" "}
                    / {bytes(selected.metrics.memory_total)}
                  </dd>
                </div>
                <div>
                  <dt>Disk usage</dt>
                  <dd>
                    {selected.metrics.disks?.length
                      ? selected.metrics.disks.map((d) => (
                          <p key={d.mount}>
                            {d.mount}: {bytes(d.used)} / {bytes(d.total)} used
                          </p>
                        ))
                      : data.source === "inventory"
                        ? `${bytes(selected.metrics.disk_free)} / ${bytes(selected.metrics.disk_total)} free`
                        : "Unknown"}
                  </dd>
                </div>
              </dl>
              <p className="text-muted-foreground text-xs">
                Missing observations are unknown. Load is the queue average, not a percentage.
              </p>
              <h4 className="font-medium text-sm">
                Recent health ·{" "}
                {data.source === "snapshot" ? "latest observation per 5-minute bucket" : "5-minute averages"} in the
                last hour
              </h4>
              {data.warning && <p role="status">{data.warning}</p>}
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Time</TableHead>
                    <TableHead>Samples</TableHead>
                    <TableHead>CPU utilization</TableHead>
                    <TableHead>1m load</TableHead>
                    <TableHead>Memory {data.source === "snapshot" ? "used" : "available"}</TableHead>
                    <TableHead>Disk {data.source === "snapshot" ? "used / total" : "free"}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {data.samples.map((s) => (
                    <TableRow key={s.at}>
                      <TableCell title={s.sample_at ? `Observed ${time(s.sample_at)}` : undefined}>
                        {time(s.at)}
                      </TableCell>
                      <TableCell>{s.count}</TableCell>
                      <TableCell>{s.cpu_percent == null ? "Unknown" : `${number(s.cpu_percent)}%`}</TableCell>
                      <TableCell>{number(s.load1)}</TableCell>
                      <TableCell>{bytes(data.source === "snapshot" ? s.memory_used : s.memory_available)}</TableCell>
                      <TableCell>
                        {data.source === "snapshot"
                          ? s.disks?.length
                            ? s.disks.map((d) => (
                                <p key={d.mount}>
                                  {d.mount}: {bytes(d.used)} / {bytes(d.total)}
                                </p>
                              ))
                            : "Unknown"
                          : bytes(s.disk_free)}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
              {data.samples.length === 0 && (
                <p className="text-muted-foreground text-sm">No recent health samples available.</p>
              )}
              <h4 className="font-medium text-sm">Agents on this machine</h4>
              {data.sessions_warning && (
                <p role="status" className="text-sm">
                  {data.sessions_warning}
                </p>
              )}
              {data.sessions_truncated && <p className="text-sm">Agent observations are truncated.</p>}
              {data.unknown_host_sessions > 0 && (
                <p className="text-sm">{data.unknown_host_sessions} agent observations have unknown placement.</p>
              )}
              {data.sessions.map((s) => (
                <SessionRowView key={`${s.kind}:${s.id}`} session={s} />
              ))}
              {data.sessions.length === 0 && (
                <p className="text-muted-foreground text-sm">
                  No agents with a known placement on this machine were reported.
                </p>
              )}
            </section>
          )}
        </>
      )}
    </div>
  )
}
