// Package ops is Operations: the owner's workstream board. Each workstream is one revisioned
// Caster document under the ops/v1 contract (caster://ops/ws.<id>), the Queued band's order is
// one document (caster://ops/queue), and the module's database is only a working copy read
// again every 15 seconds. Workstreams nest: a workstream is a concept of any size, with an
// optional parent. Activity and tasks are read from Torque (and the Tesseract status stream
// where a workstream names one) and rolled up from children. README.md has the format.
//
// Ops reaches Tether through its own allowlisted link. It never launches anything: no
// session, team, dispatch or claim tool is on its list, whatever is asked.
package ops

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hollis-labs/surface/module"
	"github.com/hollis-labs/surface/module/tetherlink"
)

func init() { module.Register(&mod{}) }

//go:embed migrations/*.sql
var migrations embed.FS

const (
	viewBoard  = "ops"
	viewWS     = "ops.workstream"
	viewSeed   = "ops.seed"
	readBack   = 15 * time.Second
	rollEvery  = 4  // read-backs between activity refresh batches (a minute)
	rollAtOnce = 12 // workstreams rolled up per round at most
)

// allowed are the only Tether tools Ops calls: Caster for its documents, Torque and
// Tesseract reads for activity, Tether workstream reads, and Registry reads. Nothing on it
// launches, dispatches, claims or forms a team (forbidden, checked by a test).
var allowed = tetherlink.Allowed{
	"caster_get": true, "caster_ls": true, "caster_put": true, "caster_log": true,
	"torque_task_get": true, "torque_task_list": true, "torque_epic_get": true, "torque_plan_get": true,
	"torque_plan_list_children": true, "torque_comment_list": true, "event_list": true,
	"tether_workstream_get": true, "tether_workstream_digest": true,
	"registry_record_list": true, "registry_record_get": true,
}

// forbidden are tools Ops must never reach, whatever is asked.
var forbidden = []string{
	"tether_session_launch", "tether_team_form", "tether_team_assign", "torque_session_launch",
	"torque_unit_dispatch", "torque_plan_start", "torque_sprint_start", "torque_workstream_claim_next",
}

type mod struct {
	module.Base
	h  module.Host
	st *Store
	// caller replaces the configured link (tests).
	caller tetherlink.Caller

	mu      sync.Mutex
	run     context.Context
	stop    context.CancelFunc
	done    chan struct{}
	ticks   int
	rolling bool
	bg      sync.WaitGroup
	// tasks is the Torque reads shared by every roll-up.
	tasks           *taskCache
	activityMu      sync.Mutex
	activityPending []string
	activityNext    time.Time
}

func (m *mod) Manifest() module.Manifest {
	return module.Manifest{
		ID: "ops", Title: "Operations", Version: "0.1.0",
		About: "The owner's workstream board: what is planned, queued in launch order, in flight and landing, kept in Caster (ops/v1), with activity read from Torque.",
		Views: []module.View{
			{ID: viewBoard, Title: "Operations", Icon: "radar", Width: 1280, Height: 760, Menu: true, Bare: true,
				Render: module.Component("ops/board"), Describe: m.describeBoard},
			{ID: viewWS, Title: "Workstream", Icon: "radar", Width: 900, Height: 720, Multi: true, Bare: true,
				Render: module.Component("ops/workstream"), Describe: m.describeWS},
			{ID: viewSeed, Title: "Seed Operations", Icon: "sprout", Width: 1100, Height: 760, Multi: true, Bare: true,
				Render: module.Component("ops/seed")},
		},
		Commands: []module.Command{
			{ID: "ops.open", Title: "Operations: open", Section: "Operations",
				Keywords: "operations ops workstreams board queue in flight launch order concepts",
				Run: func(ctx context.Context, h module.Host, args map[string]any) (any, error) {
					if id, _ := args["id"].(string); ValidID(id) {
						return h.Desk().Open(ctx, viewWS, module.OpenOptions{Key: id, Args: map[string]any{"id": id}, Title: "ops:" + id})
					}
					return h.Desk().Open(ctx, viewBoard, module.OpenOptions{})
				}},
			{ID: "ops.seed", Title: "Operations: seed from the tracker", Section: "Operations",
				Keywords: "operations seed import tracker preview",
				Run: func(ctx context.Context, h module.Host, args map[string]any) (any, error) {
					a := map[string]any{}
					key := "new"
					if id, _ := args["plan"].(string); id != "" {
						a["plan"], key = id, id
					}
					return h.Desk().Open(ctx, viewSeed, module.OpenOptions{Key: key, Args: a})
				}},
		},
		Settings: []module.Setting{
			{Key: "tether_command", Title: "Tether command", About: "A command that speaks MCP over stdio to Tether: Caster, Torque and Tesseract are reached through it.", Type: module.SettingString, Default: ""},
			{Key: "tether_url", Title: "Tether MCP URL", About: "Instead of a command: Tether's MCP endpoint over HTTP.", Type: module.SettingString, Default: ""},
			{Key: "tether_token_file", Title: "Tether token file", About: "With the URL: a file holding the bearer token.", Type: module.SettingString, Default: ""},
		},
		Store: &module.Store{Migrations: migrations, Dir: "migrations"},
		Tools: m.tools(),
		Refs:  m.refs(),
		Slots: []module.Slot{{
			Name:  "ops",
			About: "the owner's operations board: workstreams (concepts of any size, nested) moving through planned, parked, queued, in flight, landing and complete",
			Views: []string{viewBoard, viewWS, viewSeed},
			Tools: []string{"ops_list", "ops_get", "ops_activity", "ops_create", "ops_update", "ops_move", "ops_queue_reorder", "ops_link", "ops_unlink", "ops_seed"},
			Where: "Caster, namespace ops: caster://ops/ws.<id> per workstream (ops/v1), caster://ops/queue for the launch order; Torque for the tasks under them",
			Instructions: "The board is the owner's. Read before you write (ops_get gives rev, the base_rev for ops_update). Propose moves; make them on the owner's word, with why. " +
				"Moves are explicit: a suggested stage is a hint to put to the owner, never a move you make on your own. " +
				"Never launch, dispatch or claim anything: to start work, file a Torque task or ask the dispatcher, and say so to the owner. " +
				"Name a workstream as ops:<id> so it shows as a link. A seed is a preview the owner applies in the page; you never apply one.",
			Knowledge: m.knowledge,
		}},
	}
}

func (m *mod) Start(_ context.Context, h module.Host) error {
	m.h = h
	m.st = NewStore(h.Store(), GatewayCaster{Link: m.tether})
	m.tasks = newTaskCache()
	m.routes(h)
	run, cancel := context.WithCancel(context.Background())
	m.run, m.stop, m.done = run, cancel, make(chan struct{})
	go m.watch(run)
	return nil
}

func (m *mod) Stop(context.Context) error {
	if m.stop != nil {
		m.stop()
		<-m.done
		m.bg.Wait()
	}
	return nil
}

// kickRoll refreshes activity in the background, once at a time, so the board's
// cards get their task line soon after it opens rather than at the next minute.
func (m *mod) kickRoll() {
	m.mu.Lock()
	if m.rolling || m.run == nil || m.run.Err() != nil {
		m.mu.Unlock()
		return
	}
	m.rolling = true
	m.bg.Add(1)
	m.mu.Unlock()
	go func() {
		defer m.bg.Done()
		defer func() {
			m.mu.Lock()
			m.rolling = false
			m.mu.Unlock()
		}()
		ctx, cancel := context.WithTimeout(m.run, time.Minute)
		defer cancel()
		m.rollActivity(ctx)
	}()
}

var errNotConfigured = ErrNoCaster

// tether is the configured link, read from the settings at each call.
func (m *mod) tether() (tetherlink.Caller, error) {
	if m.caller != nil {
		return m.caller, nil
	}
	cfg := m.h.Config()
	c := tetherlink.New("ops", tetherlink.Config{URL: cfg.String("tether_url"), TokenFile: cfg.String("tether_token_file"), Command: cfg.String("tether_command")}, allowed)
	if c == nil {
		return nil, errNotConfigured
	}
	return c, nil
}

// changed tells the views that the board changed.
func (m *mod) changed(id, what string) {
	if m.h == nil {
		return
	}
	data := map[string]any{"what": what}
	if id != "" {
		data["id"] = id
	}
	m.h.Events().Publish("ops.changed", data)
}

// watch reads the copy back from Caster every 15 s while an Operations window is open, and
// every minute refreshes a bounded activity batch (cached; the views draw it).
func (m *mod) watch(ctx context.Context) {
	defer close(m.done)
	t := time.NewTicker(readBack)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if !m.windowOpen(ctx) {
			continue
		}
		c, cancel := context.WithTimeout(ctx, readBack*2)
		if n, err := m.st.Refresh(c, false); err == nil && n > 0 {
			m.changed("", "read-back")
		}
		cancel()
		m.mu.Lock()
		m.ticks++
		roll := m.ticks%rollEvery == 0
		m.mu.Unlock()
		if roll {
			m.kickRoll() // its own context, and the one flag the board's kick also holds
		}
	}
}

func (m *mod) windowOpen(ctx context.Context) bool {
	for _, w := range m.h.Desk().Windows(ctx) {
		if w.Module == "ops" {
			return true
		}
	}
	return false
}

// rollActivity advances the shared refresh cycle, starting with in-flight workstreams.
func (m *mod) rollActivity(ctx context.Context) {
	_, _ = m.globalActivity(ctx, activityFilter{}, false)
}

// sync brings the copy up to date (at most every 15 s unless forced). The error is a
// problem to report; the copy is still read.
func (m *mod) sync(ctx context.Context, force bool) error {
	_, err := m.st.Refresh(ctx, force)
	return err
}

// rollup is a workstream's roll-up: the cache when it is fresh, else read now.
func (m *mod) rollup(ctx context.Context, id string, force bool) (Rollup, error) {
	if !force {
		if r, at, ok := m.st.cachedRollup(ctx, id); ok && time.Since(at) < activityFresh {
			return r, nil
		}
	}
	b, err := m.st.All(ctx)
	if err != nil {
		return Rollup{}, err
	}
	w, ok := b.Find(id)
	if !ok {
		return Rollup{}, ErrNotFound
	}
	r := reader{link: m.tether, cache: m.tasks}.Roll(ctx, w, b.Descendants(id))
	m.st.keepRollup(ctx, r)
	return r, nil
}

// ---- What the agent is told ----

func (m *mod) describeBoard(_ context.Context, _ module.Host, w module.Window) (module.WindowContext, error) {
	wc := module.WindowContext{Kind: "data", Ref: "ops", Extra: map[string]any{"layout": w.Args["layout"]}}
	if w.Args["layout"] == "activity" {
		for _, k := range []string{"workstream", "stage", "source", "since"} {
			wc.Extra[k] = w.Args[k]
		}
		return wc, nil
	}
	if p, _ := w.Args["parent"].(string); ValidID(p) {
		wc.Ref = Ref(p)
		wc.Extra["inside"] = p
	}
	return wc, nil
}

func (m *mod) describeWS(_ context.Context, _ module.Host, w module.Window) (module.WindowContext, error) {
	id, _ := w.Args["id"].(string)
	if !ValidID(id) {
		return module.WindowContext{Kind: "data", Ref: "ops"}, nil
	}
	return module.WindowContext{Kind: "workstream", Ref: Ref(id), Extra: map[string]any{"workstream": id}}, nil
}

// knowledge is the slot's: lane counts, the queue head, what is in flight (held reasons and
// the age of its last activity), and the workstream in front with its links. Copy only.
func (m *mod) knowledge(ctx context.Context, _ module.Host, s module.Situation) (string, error) {
	if m.st == nil {
		return "", nil
	}
	b, err := m.st.All(ctx)
	if err != nil {
		return "", err
	}
	if len(b.Workstreams) == 0 {
		return "Operations has no workstreams in its working copy yet.", nil
	}
	cached := m.st.Cached(ctx)
	lanes := map[string]int{}
	for _, w := range b.Workstreams {
		lanes[w.Stage]++
	}
	var head []map[string]any
	for _, id := range b.Queue {
		if w, ok := b.Find(id); ok && w.Parent == "" {
			head = append(head, map[string]any{"id": id, "title": w.Title})
			if len(head) == 3 {
				break
			}
		}
	}
	var flying []map[string]any
	for _, w := range b.Workstreams {
		if w.Stage != StageInFlight {
			continue
		}
		r := map[string]any{"id": w.ID, "title": w.Title, "parent": w.Parent}
		if w.Held != nil {
			r["held"] = w.Held.Reason
		}
		if c, ok := cached[w.ID]; ok {
			r["tasks"] = CountsText(c.Counts)
			if c.Last != nil {
				r["last_activity"] = c.Last.At
			}
			if c.Suggest != "" {
				r["suggested_stage"] = c.Suggest
			}
		}
		flying = append(flying, r)
	}
	sort.Slice(flying, func(i, j int) bool { return fmt.Sprint(flying[i]["id"]) < fmt.Sprint(flying[j]["id"]) })
	out := map[string]any{"lanes": lanes, "queue_head": head, "in_flight": flying}
	if s.Focused != nil && strings.HasPrefix(s.Focused.Ref, "ops:") {
		if w, ok := b.Find(strings.TrimPrefix(s.Focused.Ref, "ops:")); ok {
			out["in_front"] = map[string]any{"id": w.ID, "title": w.Title, "stage": w.Stage, "rev": w.Rev, "parent": w.Parent,
				"children": w.Children, "links": w.Links, "active_when": w.ActiveWhen}
			if c, ok := cached[w.ID]; ok {
				front := out["in_front"].(map[string]any)
				front["task_counts"], front["plans"] = c.Counts, c.Plans
			}
		}
	}
	j, _ := json.Marshal(out)
	return "Operations (from the working copy):\n" + string(j) + "\n" + latestActivityLine(b, cached), nil
}

// actor is who a write is recorded as.
func actor(ctx context.Context) string {
	switch a := module.ActorOf(ctx); a {
	case "", module.ActorOwner:
		return "owner"
	default:
		return string(a)
	}
}

// isInput says whether err is the caller's (a 400), not the service's.
func isInput(err error) bool {
	var ie *InputError
	return errors.As(err, &ie)
}
