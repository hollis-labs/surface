import { Icon, useEvent, type ViewProps } from "@hollis-labs/surface-page/sdk"
import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { Button } from "@/components/ui/button"
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty"
import { Spinner } from "@/components/ui/spinner"
import { message, opsClient, time } from "./api"
import { SessionRowView } from "./session-row"
import type { SessionsResult } from "./sessions"

export function BoardAgents({ api }: ViewProps) {
  const ops = useMemo(() => opsClient(api), [api])
  const [result, setResult] = useState<SessionsResult | null>(null)
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)
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
  return (
    <section aria-label="Live agents" className="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto p-3">
      <div className="flex items-center gap-2">
        <h2 className="font-medium text-sm">Live agents{result ? ` (${result.sessions.length})` : ""}</h2>
        <span className="ml-auto text-muted-foreground text-xs">
          {result ? `Observed ${time(result.fetched_at)}` : ""}
        </span>
        <Button size="sm" variant="ghost" aria-label="Refresh agents" disabled={busy} onClick={() => void load()}>
          {busy ? <Spinner /> : <Icon name="refresh-cw" />}
        </Button>
      </div>
      <p className="text-muted-foreground text-xs">
        Awake agents are working; sleeping agents are booted and idle. Refreshes while this view is open.
      </p>
      {error && (
        <p role="alert" className="text-destructive text-sm">
          {result ? "Showing the previous observation. " : ""}
          {error}
        </p>
      )}
      {result?.warning && <p className="text-muted-foreground text-xs">{result.warning}</p>}
      {result?.truncated && (
        <p role="status" className="text-sm">
          The source limit was reached; this list is incomplete.
        </p>
      )}
      {!result && busy ? (
        <Spinner />
      ) : result?.sessions.length === 0 ? (
        <Empty>
          <EmptyHeader>
            <EmptyTitle>No live agents reported</EmptyTitle>
            <EmptyDescription>No booted sessions were returned by Tether.</EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        result?.sessions.map((session) => <SessionRowView key={session.id} session={session} />)
      )}
    </section>
  )
}
