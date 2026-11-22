import { CiteRef, Icon, ObjectActionsMenu, useEvent, type ViewProps } from "@hollis-labs/surface-page/sdk"
import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty"
import { Input } from "@/components/ui/input"
import { Spinner } from "@/components/ui/spinner"
import { age, message, opsClient, time } from "./api"
import { UnitNotesInline } from "./parts"
import { AgentActivityBars, AgentLineArt, parseAgentIdentity, SessionRowView } from "./session-row"
import type { SessionRow, SessionsResult } from "./sessions"

export function BoardAgents({ win, desk, api }: ViewProps) {
  const ops = useMemo(() => opsClient(api), [api])
  const [result, setResult] = useState<SessionsResult | null>(null)
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)
  const [stateFilter, setStateFilter] = useState<"all" | "awake" | "sleeping" | "stale">("all")
  const [hostFilter, setHostFilter] = useState<string>("all")
  const [search, setSearch] = useState("")
  const [selected, setSelected] = useState<SessionRow | null>(null)
  const request = useRef(0)

  const load = useCallback(async () => {
    const seq = ++request.current
    setBusy(true)
    try {
      const next = await ops.agents()
      if (seq === request.current) {
        setResult(next)
        setError("")
      }
    } catch (e) {
      if (seq === request.current) setError(message(e))
    } finally {
      if (seq === request.current) setBusy(false)
    }
  }, [ops])

  useEffect(() => {
    void load()
    return () => {
      request.current++
    }
  }, [load])
  useEvent("ops.agents", () => void load())

  const sessions = result?.sessions ?? []

  const summary = useMemo(() => {
    let awake = 0
    let sleeping = 0
    let unknown = 0
    let stale = 0
    const hosts = new Set<string>()
    let assignedTasks = 0
    for (const s of sessions) {
      if (s.state === "awake") awake++
      else if (s.state === "sleeping") sleeping++
      else unknown++
      if (s.stale) stale++
      if (s.host) hosts.add(s.host)
      if (s.task_id) assignedTasks++
    }
    return {
      total: sessions.length,
      awake,
      sleeping,
      unknown,
      stale,
      hostCount: hosts.size,
      hosts: ["all", ...Array.from(hosts).sort()],
      assignedTasks,
    }
  }, [sessions])

  const filteredSessions = useMemo(() => {
    const q = search.trim().toLowerCase()
    return sessions.filter((s) => {
      if (stateFilter === "awake" && s.state !== "awake") return false
      if (stateFilter === "sleeping" && s.state !== "sleeping") return false
      if (stateFilter === "stale" && !s.stale) return false
      if (hostFilter !== "all" && (s.host || "unknown") !== hostFilter) return false
      if (!q) return true
      return (
        s.agent_name.toLowerCase().includes(q) ||
        s.id.toLowerCase().includes(q) ||
        (s.host || "").toLowerCase().includes(q) ||
        (s.task_id || "").toLowerCase().includes(q) ||
        (s.workstream_id || "").toLowerCase().includes(q) ||
        (s.raw_state || "").toLowerCase().includes(q)
      )
    })
  }, [sessions, stateFilter, hostFilter, search])

  const openHostInFleet = (targetHost: string) => {
    setSelected(null)
    void desk.setArgs(win.id, { ...win.args, layout: "fleet", host: targetHost })
  }

  return (
    <section
      aria-label="Live agents"
      className="flex min-h-0 min-w-0 flex-1 flex-col gap-4 overflow-x-hidden overflow-y-auto overscroll-x-none p-4"
    >
      {/* Top Telemetry Summary Header & Filter Workbench */}
      <div className="flex flex-wrap items-center justify-between gap-3 rounded-md border border-border/80 bg-card/80 px-3.5 py-2.5">
        <div className="flex flex-wrap items-center gap-4">
          <div className="flex items-center gap-2.5">
            <span className="flex size-7 items-center justify-center rounded-sm border border-primary/30 bg-primary/10 text-primary">
              <Icon name="bot" size={15} />
            </span>
            <div>
              <div className="flex flex-wrap items-center gap-2">
                <h2 className="font-mono text-xs font-bold uppercase tracking-wider text-foreground">
                  Live Agents{result ? ` (${result.sessions.length})` : ""}
                </h2>
                <Badge
                  variant="outline"
                  className="h-4 border-success/40 bg-success/10 px-1.5 font-mono text-[10px] text-success"
                >
                  {summary.awake} awake
                </Badge>
                {summary.sleeping > 0 && (
                  <Badge variant="secondary" className="h-4 px-1.5 font-mono text-[10px]">
                    {summary.sleeping} sleeping
                  </Badge>
                )}
              </div>
              <p className="font-mono text-[11px] text-muted-foreground">
                {result ? `Observed ${time(result.fetched_at)} · ` : ""}
                Awake agents are working; sleeping agents are booted and idle.
              </p>
            </div>
          </div>

          <div className="hidden h-7 w-px bg-border/70 lg:block" />

          <div className="hidden flex-wrap items-center gap-4 font-mono text-xs lg:flex">
            <div>
              <span className="text-[10px] uppercase text-muted-foreground">Placement Hosts</span>
              <p className="font-semibold text-foreground">{summary.hostCount} active</p>
            </div>
            <div>
              <span className="text-[10px] uppercase text-muted-foreground">Task Seats</span>
              <p className="font-semibold text-foreground">
                {summary.assignedTasks}/{summary.total} linked
              </p>
            </div>
            <div>
              <span className="text-[10px] uppercase text-muted-foreground">Freshness</span>
              <p className="font-semibold text-foreground">
                {summary.stale === 0 ? "All current" : `${summary.stale} stale`}
              </p>
            </div>
          </div>
        </div>

        {/* Filter Controls & Refresh */}
        <div className="flex flex-wrap items-center gap-2">
          <div className="flex items-center gap-1 rounded-sm border border-border/75 bg-muted/30 p-0.5">
            {(
              [
                { id: "all", label: `All (${summary.total})` },
                { id: "awake", label: `Awake (${summary.awake})` },
                { id: "sleeping", label: `Sleeping (${summary.sleeping})` },
                ...(summary.stale > 0 ? [{ id: "stale", label: `Stale (${summary.stale})` }] : []),
              ] as const
            ).map((f) => (
              <Button
                key={f.id}
                size="xs"
                variant={stateFilter === f.id ? "secondary" : "ghost"}
                className="h-6 px-2 font-mono text-[11px]"
                onClick={() => setStateFilter(f.id as "all" | "awake" | "sleeping" | "stale")}
              >
                {f.label}
              </Button>
            ))}
          </div>

          {summary.hosts.length > 2 && (
            <div className="flex items-center gap-1 rounded-sm border border-border/75 bg-muted/30 p-0.5">
              {summary.hosts.map((h) => (
                <Button
                  key={h}
                  size="xs"
                  variant={hostFilter === h ? "secondary" : "ghost"}
                  className="h-6 px-2 font-mono text-[11px]"
                  onClick={() => setHostFilter(h)}
                >
                  {h === "all" ? "All hosts" : h}
                </Button>
              ))}
            </div>
          )}

          <Input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Filter agent, CW-task, host…"
            className="h-7 w-48 font-mono text-xs"
            aria-label="Filter agents"
          />

          <Button
            size="sm"
            variant="outline"
            className="h-7 gap-1 px-2 font-mono text-xs"
            aria-label="Refresh agents"
            disabled={busy}
            onClick={() => void load()}
          >
            {busy ? <Spinner /> : <Icon name="refresh-cw" size={12} />}
            Refresh
          </Button>
        </div>
      </div>

      {error && (
        <div
          role="alert"
          className="flex items-center gap-2 rounded-sm border border-destructive/40 bg-destructive/10 px-3 py-2 text-xs text-destructive"
        >
          <Icon name="triangle-exclamation-point" size={14} />
          <span>
            {result ? "Showing the previous observation. " : ""}
            {error}
          </span>
        </div>
      )}

      {result?.warning && (
        <div className="flex items-center gap-2 rounded-sm border border-border/70 bg-muted/35 px-3 py-2 text-xs text-muted-foreground">
          <Icon name="info" size={13} className="shrink-0 text-primary" />
          <span>{result.warning}</span>
        </div>
      )}

      {result?.truncated && (
        <p role="status" className="font-mono text-xs text-warning">
          The source limit was reached; this list is incomplete.
        </p>
      )}

      {!result && busy ? (
        <div className="flex flex-1 items-center justify-center py-12">
          <Spinner />
        </div>
      ) : result?.sessions.length === 0 ? (
        <Empty className="py-12">
          <EmptyHeader>
            <EmptyTitle>No live agents reported</EmptyTitle>
            <EmptyDescription>No booted sessions were returned by Tether.</EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : filteredSessions.length === 0 ? (
        <Empty className="py-12">
          <EmptyHeader>
            <EmptyTitle>No agents match these filters</EmptyTitle>
            <EmptyDescription>Clear the search query or switch state tabs to inspect all sessions.</EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <div className="grid grid-cols-[repeat(auto-fill,minmax(26rem,1fr))] gap-4">
          {filteredSessions.map((session) => (
            <SessionRowView key={session.id} session={session} onSelect={setSelected} onOpenHost={openHostInFleet} />
          ))}
        </div>
      )}

      {/* Click-Into Agent Session Inspector Modal */}
      <Dialog open={!!selected} onOpenChange={(open) => !open && setSelected(null)}>
        <DialogContent className="flex max-h-[86vh] w-[min(52rem,calc(100vw-2rem))] max-w-[calc(100vw-2rem)] flex-col gap-0 overflow-hidden overscroll-x-none p-0 sm:max-w-3xl">
          {selected && (
            <>
              <DialogHeader className="shrink-0 border-b border-border/80 bg-muted/30 px-5 py-4 pr-10">
                <div className="flex flex-wrap items-start justify-between gap-3">
                  <div className="flex items-start gap-3.5">
                    <AgentLineArt
                      state={selected.state}
                      rawState={selected.raw_state}
                      stale={selected.stale}
                      size="lg"
                    />
                    <div className="flex flex-col gap-1">
                      <div className="flex flex-wrap items-center gap-2">
                        <DialogTitle className="font-mono text-base font-bold tracking-tight">
                          {parseAgentIdentity(selected.agent_name).baseName}
                        </DialogTitle>
                        {parseAgentIdentity(selected.agent_name).instanceTag && (
                          <Badge variant="outline" className="font-mono text-[10.5px]">
                            {parseAgentIdentity(selected.agent_name).instanceTag}
                          </Badge>
                        )}
                        <Badge
                          variant={selected.state === "awake" ? "default" : "secondary"}
                          className="font-mono text-[10.5px]"
                        >
                          {selected.state === "awake"
                            ? "Awake"
                            : selected.state === "sleeping"
                              ? "Sleeping"
                              : "State unknown"}
                        </Badge>
                        <Badge variant="outline" className="font-mono text-[10.5px]">
                          raw: {selected.raw_state || "unknown"}
                        </Badge>
                      </div>
                      <DialogDescription className="font-mono text-xs text-muted-foreground">
                        {selected.agent_ref || selected.agent_id} · {selected.id}
                      </DialogDescription>
                    </div>
                  </div>
                  <div className="flex items-center gap-1.5">
                    <AgentActivityBars session={selected} width={76} height={20} />
                    <ObjectActionsMenu
                      object={{
                        kind: "tether.agent",
                        id: selected.agent_ref || selected.agent_id || selected.id,
                        title: selected.agent_name,
                        subtitle: `${selected.state} on ${selected.host || "unknown host"}`,
                      }}
                    />
                  </div>
                </div>
              </DialogHeader>

              <div className="mx-auto flex min-h-0 w-[92%] flex-1 flex-col gap-4 overflow-x-hidden overflow-y-auto overscroll-x-none py-4 text-xs">
                {/* 3 Telemetry Summary Boxes */}
                <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
                  <div className="flex flex-col gap-1 rounded-md border border-border/80 bg-muted/20 p-3">
                    <span className="font-mono text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
                      Host Placement
                    </span>
                    <span className="font-mono text-sm font-bold text-foreground">
                      {selected.host || "Host unknown"}
                    </span>
                    {selected.host && (
                      <Button
                        variant="link"
                        size="xs"
                        className="h-auto justify-start p-0 font-mono text-[11px] text-primary"
                        onClick={() => openHostInFleet(selected.host)}
                      >
                        Inspect host in Fleet →
                      </Button>
                    )}
                  </div>

                  <div className="flex flex-col gap-1 rounded-md border border-border/80 bg-muted/20 p-3">
                    <span className="font-mono text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
                      Last Observed Turn
                    </span>
                    <span className="font-mono text-sm font-bold text-foreground">
                      {selected.last_activity_at ? `${age(selected.last_activity_at)} ago` : "Unknown"}
                    </span>
                    <span className="font-mono text-[11px] text-muted-foreground">
                      {selected.last_activity_at ? time(selected.last_activity_at) : "No timestamp"}
                    </span>
                  </div>

                  <div className="flex flex-col gap-1 rounded-md border border-border/80 bg-muted/20 p-3">
                    <span className="font-mono text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
                      Observation Freshness
                    </span>
                    <span className={`font-mono text-sm font-bold ${selected.stale ? "text-warning" : "text-success"}`}>
                      {selected.stale ? "Stale observation" : "Live & current"}
                    </span>
                    <span className="font-mono text-[11px] text-muted-foreground">Kind: {selected.kind}</span>
                  </div>
                </div>

                {/* Linked References & Assignments */}
                <div className="flex flex-col gap-2 rounded-md border border-border/75 bg-muted/20 p-3.5">
                  <span className="font-mono text-[11px] font-semibold uppercase tracking-wider text-foreground">
                    Linked References &amp; Assignments
                  </span>
                  <dl className="grid grid-cols-[8rem_1fr] items-center gap-x-3 gap-y-2 text-xs">
                    <dt className="text-muted-foreground">Tether Agent</dt>
                    <dd>
                      {selected.agent_ref ? (
                        <CiteRef
                          refId={{ kind: "tether.agent", canonical: selected.agent_ref }}
                          label={selected.agent_name}
                        />
                      ) : (
                        <span className="font-mono">{selected.agent_name}</span>
                      )}
                    </dd>
                    <dt className="text-muted-foreground">Active Task</dt>
                    <dd>
                      {selected.task_id ? (
                        <CiteRef refId={{ kind: "torque.task", canonical: selected.task_id }} />
                      ) : (
                        <span className="text-muted-foreground">None reported</span>
                      )}
                    </dd>
                    <dt className="text-muted-foreground">Workstream</dt>
                    <dd>
                      {selected.workstream_id ? (
                        <CiteRef
                          refId={{
                            kind: "registry.record",
                            canonical: selected.workstream_id.startsWith("workstream:")
                              ? selected.workstream_id
                              : `workstream:${selected.workstream_id}`,
                          }}
                          label={selected.workstream_id}
                        />
                      ) : (
                        <span className="text-muted-foreground">None reported</span>
                      )}
                    </dd>
                    <dt className="text-muted-foreground">Mailbox / Wire URN</dt>
                    <dd className="flex items-center gap-2 font-mono text-[11px]">
                      <span className="truncate">{selected.agent_ref || selected.agent_id}</span>
                      <Button
                        size="xs"
                        variant="ghost"
                        className="h-5 px-1.5 text-[10px]"
                        onClick={() =>
                          void navigator.clipboard?.writeText(selected.agent_ref || selected.agent_id || selected.id)
                        }
                      >
                        <Icon name="copy" size={10} />
                        Copy
                      </Button>
                    </dd>
                  </dl>
                </div>

                {/* Operator Notes via Surface Marks */}
                <div className="flex flex-col gap-1.5 rounded-md border border-border/75 bg-muted/20 p-3.5">
                  <span className="font-mono text-[11px] font-semibold uppercase tracking-wider text-foreground">
                    Session Notes &amp; Marks
                  </span>
                  <UnitNotesInline targetRef={selected.agent_ref || selected.id} />
                </div>
              </div>
            </>
          )}
        </DialogContent>
      </Dialog>
    </section>
  )
}
