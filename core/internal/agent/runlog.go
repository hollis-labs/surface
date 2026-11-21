package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"sync"
	"time"

	chatstream "github.com/hollis-labs/libs/ui/ui-go/chatstream"
)

// The run log: one row per run on the conversation wire, kept in the core database, so the way
// the agent works can be looked at and improved. Each row records who started the run (the
// owner, a module's errand, a reflex), the slot it ran in, the model, the token usage, the
// tools it called, how it ended, and how long it took. GET /api/agent/runs reads it;
// ?format=jsonl exports it.

const runLogSchema = `CREATE TABLE IF NOT EXISTS agent_runs (
	run_id TEXT PRIMARY KEY,
	started_at TEXT NOT NULL,
	ended_at TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT '',
	reason TEXT NOT NULL DEFAULT '',
	origin TEXT NOT NULL DEFAULT '',
	slot TEXT NOT NULL DEFAULT '',
	provider TEXT NOT NULL DEFAULT '',
	model TEXT NOT NULL DEFAULT '',
	uncached_input INTEGER NOT NULL DEFAULT 0,
	cache_read INTEGER NOT NULL DEFAULT 0,
	cache_write INTEGER NOT NULL DEFAULT 0,
	output INTEGER NOT NULL DEFAULT 0,
	tools TEXT NOT NULL DEFAULT '[]',
	ms INTEGER NOT NULL DEFAULT 0,
	profile TEXT NOT NULL DEFAULT '',
 agent_dir TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS agent_runs_started ON agent_runs(started_at);`

// RunRecord is one run as the log keeps it.
type RunRecord struct {
	RunID     string `json:"run_id"`
	StartedAt string `json:"started_at"`
	EndedAt   string `json:"ended_at,omitempty"`
	Status    string `json:"status,omitempty"`
	Reason    string `json:"reason,omitempty"`
	Origin    string `json:"origin,omitempty"`
	Slot      string `json:"slot,omitempty"`
	// Profile is the agent profile the run acted as: a module's id, or "default" (the seat).
	AgentDir  string   `json:"agent_dir,omitempty"`
	Profile   string   `json:"profile,omitempty"`
	Provider  string   `json:"provider,omitempty"`
	Model     string   `json:"model,omitempty"`
	Uncached  int      `json:"uncached_input"`
	CacheRead int      `json:"cache_read"`
	CacheWrit int      `json:"cache_write"`
	Output    int      `json:"output"`
	Tools     []string `json:"tools"`
	Ms        int64    `json:"ms"`
}

// runLog keeps the log; what the hook learns before a run starts waits in pending.
type runLog struct {
	db      *sql.DB
	mu      sync.Mutex
	pending map[string]runTurn
}

// runTurn is what the hook knows of a turn: who started it, the slot it runs in and the
// profile it runs as.
type runTurn struct{ origin, slot, profile, agentDir string }

func openRunLog(ctx context.Context, db *sql.DB) (*runLog, error) {
	if _, err := db.ExecContext(ctx, runLogSchema); err != nil {
		return nil, err
	}
	// A log kept before runs recorded their profile gets the column.
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('agent_runs') WHERE name = 'profile'`).Scan(&n); err != nil {
		return nil, err
	}
	if n == 0 {
		if _, err := db.ExecContext(ctx, `ALTER TABLE agent_runs ADD COLUMN profile TEXT NOT NULL DEFAULT ''`); err != nil {
			return nil, err
		}
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('agent_runs') WHERE name = 'agent_dir'`).Scan(&n); err != nil {
		return nil, err
	}
	if n == 0 {
		if _, err := db.ExecContext(ctx, `ALTER TABLE agent_runs ADD COLUMN agent_dir TEXT NOT NULL DEFAULT ''`); err != nil {
			return nil, err
		}
	}
	return &runLog{db: db, pending: map[string]runTurn{}}, nil
}

// turn is called by the hook before a run starts.
func (l *runLog) turn(runID, origin, slot, profile string, agentDir ...string) {
	if l == nil || runID == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.pending) > 256 { // turns the library dropped after the hook saw them
		l.pending = map[string]runTurn{}
	}
	dir := ""
	if len(agentDir) > 0 {
		dir = agentDir[0]
	}
	l.pending[runID] = runTurn{origin, slot, profile, dir}
}

// collector follows one run's events.
type collector struct {
	rec   RunRecord
	start time.Time
}

func (l *runLog) begin(runID, model string) *collector {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	t := l.pending[runID]
	delete(l.pending, runID)
	l.mu.Unlock()
	if t.origin == "" {
		t.origin = "owner"
	}
	// The model the app's config names; a run.start that names one wins.
	c := &collector{start: time.Now(), rec: RunRecord{RunID: runID, Origin: t.origin, Slot: t.slot, Profile: t.profile, AgentDir: t.agentDir, Model: model, Tools: []string{}}}
	c.rec.StartedAt = c.start.UTC().Format(time.RFC3339)
	_, _ = l.db.ExecContext(context.Background(), `INSERT OR IGNORE INTO agent_runs(run_id, started_at, origin, slot, profile, agent_dir) VALUES(?,?,?,?,?,?)`,
		runID, c.rec.StartedAt, t.origin, t.slot, t.profile, t.agentDir)
	return c
}

func (c *collector) see(ev chatstream.Event) {
	if c == nil {
		return
	}
	if ev.Verb == chatstream.VerbRunStart {
		c.rec.Provider = firstOf(ev.Provider, c.rec.Provider)
		c.rec.Model = firstOf(ev.Model, c.rec.Model)
	}
	if ev.Verb == chatstream.VerbPartStart && ev.Kind == "tool_call" && len(c.rec.Tools) < 200 {
		// The tool's name is in the part's meta (Nanite), or on the event.
		name := ev.Name
		if raw, ok := ev.Meta["name"]; ok && name == "" {
			_ = json.Unmarshal(raw, &name)
		}
		if name != "" {
			c.rec.Tools = append(c.rec.Tools, name)
		}
	}
	if u := ev.Usage; u != nil {
		switch u.Scope {
		case chatstream.UsageDelta:
			c.rec.Uncached += u.UncachedInput
			c.rec.CacheRead += u.CacheRead
			c.rec.CacheWrit += u.CacheWrite
			c.rec.Output += u.Output
		default: // cumulative and final carry the whole so far
			c.rec.Uncached, c.rec.CacheRead, c.rec.CacheWrit, c.rec.Output = u.UncachedInput, u.CacheRead, u.CacheWrite, u.Output
		}
	}
}

func (l *runLog) end(c *collector, status, reason string) {
	if l == nil || c == nil {
		return
	}
	tools, _ := json.Marshal(c.rec.Tools)
	_, _ = l.db.ExecContext(context.Background(), `UPDATE agent_runs SET ended_at=?, status=?, reason=?, provider=?, model=?,
		uncached_input=?, cache_read=?, cache_write=?, output=?, tools=?, ms=? WHERE run_id=?`,
		time.Now().UTC().Format(time.RFC3339), status, clipText(reason, 500), c.rec.Provider, c.rec.Model,
		c.rec.Uncached, c.rec.CacheRead, c.rec.CacheWrit, c.rec.Output, string(tools), time.Since(c.start).Milliseconds(), c.rec.RunID)
}

// recent is the newest runs, newest first.
func (l *runLog) recent(ctx context.Context, limit int) ([]RunRecord, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := l.db.QueryContext(ctx, `SELECT run_id, started_at, ended_at, status, reason, origin, slot, provider, model,
		uncached_input, cache_read, cache_write, output, tools, ms, profile, agent_dir FROM agent_runs ORDER BY started_at DESC, rowid DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []RunRecord{}
	for rows.Next() {
		var r RunRecord
		var tools string
		if err := rows.Scan(&r.RunID, &r.StartedAt, &r.EndedAt, &r.Status, &r.Reason, &r.Origin, &r.Slot, &r.Provider, &r.Model,
			&r.Uncached, &r.CacheRead, &r.CacheWrit, &r.Output, &tools, &r.Ms, &r.Profile, &r.AgentDir); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(tools), &r.Tools)
		out = append(out, r)
	}
	return out, rows.Err()
}

func clipText(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

func atoi(s string, def int) int {
	if n, err := strconv.Atoi(s); err == nil && n > 0 {
		return n
	}
	return def
}
