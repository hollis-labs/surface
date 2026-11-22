/** Shared Agents/Fleet rows; unknown observations never imply sleeping. */
export interface SessionRow {
  id: string
  kind: string
  agent_id: string
  agent_name: string
  agent_ref?: string
  host: string
  state: "awake" | "sleeping" | "unknown"
  raw_state: string
  last_activity_at?: string
  last_activity?: string
  workstream_id?: string
  task_id?: string
  stale: boolean
  warning?: string
}

export interface SessionsResult {
  sessions: SessionRow[]
  fetched_at: string
  truncated: boolean
  warning?: string
}
