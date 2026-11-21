import { CiteRef } from "@hollis-labs/surface-page/sdk"
import { Badge } from "@/components/ui/badge"
import { age, time } from "./api"
import type { SessionRow } from "./sessions"

/** The same observation is drawn in Agents and beneath a Fleet host. */
export function SessionRowView({ session: s }: { session: SessionRow }) {
  return (
    <article className="flex flex-wrap items-start gap-3 rounded-md border p-3" aria-label={s.agent_name}>
      <div className="min-w-40 flex-1">
        {s.agent_ref ? (
          <CiteRef refId={{ kind: "tether.agent", canonical: s.agent_ref }} label={s.agent_name} />
        ) : (
          <span className="font-medium text-sm">{s.agent_name}</span>
        )}
        <p className="text-muted-foreground text-xs">
          {s.kind} · {s.id}
        </p>
      </div>
      <div className="min-w-28 text-sm">{s.host || "Host unknown"}</div>
      <div className="flex min-w-32 flex-col items-start gap-1">
        <Badge variant={s.state === "awake" ? "default" : "secondary"}>
          {s.state === "awake" ? "Awake" : s.state === "sleeping" ? "Sleeping" : "State unknown"}
        </Badge>
        <span className="text-muted-foreground text-xs">{s.raw_state}</span>
        {s.stale && <Badge variant="outline">Stale observation</Badge>}
      </div>
      <div className="min-w-40 flex-1 text-xs">
        <p title={time(s.last_activity_at)}>
          {s.last_activity_at
            ? `${age(s.last_activity_at)} ago · ${time(s.last_activity_at)}`
            : "Last activity unknown"}
        </p>
        {s.last_activity && <p>{s.last_activity}</p>}
        {s.warning && <p className="text-muted-foreground">{s.warning}</p>}
      </div>
      {s.workstream_id && (
        <CiteRef
          refId={{
            kind: "registry.record",
            canonical: s.workstream_id.startsWith("workstream:") ? s.workstream_id : `workstream:${s.workstream_id}`,
          }}
          label="Workstream"
        />
      )}
    </article>
  )
}
