package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/hollis-labs/surface/module"
)

// The agent's Ops tools. Reads never ask; writes are LevelWrite, so the owner approves each
// (and an approval can stand). Nothing here launches: there is no tool for it.

func schema(props string, required ...string) json.RawMessage {
	req, _ := json.Marshal(required)
	if required == nil {
		req = []byte("[]")
	}
	return json.RawMessage(`{"type":"object","properties":{` + props + `},"required":` + string(req) + `,"additionalProperties":false}`)
}

func args[T any](in json.RawMessage) (T, error) {
	var v T
	if len(in) == 0 {
		return v, nil
	}
	if err := json.Unmarshal(in, &v); err != nil {
		return v, fmt.Errorf("the input could not be read: %w", err)
	}
	return v, nil
}

func describeWith(fn func(a map[string]any) string) func(json.RawMessage) string {
	return func(in json.RawMessage) string {
		a := map[string]any{}
		_ = json.Unmarshal(in, &a)
		return fn(a)
	}
}

func str(a map[string]any, k string) string {
	if v, ok := a[k]; ok && v != nil {
		return fmt.Sprint(v)
	}
	return ""
}

const (
	idProp     = `"id":{"type":"string","description":"The workstream's Ops id (a slug, as ops_list gives it; written ops:<id> in text)."}`
	stageEnum  = `"enum":["planned","parked","queued","in_flight","landing","complete","dropped"]`
	linkSchema = `{"type":"object","properties":{"kind":{"type":"string","description":"A Cite kind: torque.task, torque.epic, torque.plan, torque.project, work.item, registry.record, caster.doc, tesseract.item, tether.agent, tether.message, tether.team, ops.workstream, url."},` +
		`"id":{"type":"string","description":"The id in that kind's form (CW-…, EP-…, <workspace>/<item>, workstream:<ULID>, caster://…)."},` +
		`"role":{"type":"string","description":"What it is to the workstream: origin, scope, identity, architecture, design, task, team, thread…"},` +
		`"label":{"type":"string","description":"A short label to show until it resolves."}},"required":["kind","id"],"additionalProperties":false}`
	checkSchema = `{"type":"object","properties":{"text":{"type":"string"},"done":{"type":"boolean"},"ref":{"type":"string"}},"required":["text"],"additionalProperties":false}`
)

func (m *mod) tools() []module.Tool {
	return []module.Tool{
		{Name: "ops_agents", Level: module.LevelRead,
			Description: "Live agents from Tether: awake, sleeping or unknown, host, raw state, observed last activity and workstream. Read-only; shares the Agents/Fleet refresh cache. Unknown or stale observations do not imply sleeping.",
			Input:       schema(""),
			Run:         func(ctx context.Context, _ module.Host, _ json.RawMessage) (any, error) { return m.sessions(ctx) }},
		{Name: "ops_settle", Level: module.LevelWrite,
			Description: "Settle a complete workstream: preserve its final tracking snapshot and hide it from the board by default. History and Cite references remain. The owner approves this action.",
			Input:       schema(idProp+`,"base_rev":{"type":"integer","minimum":1}`, "id", "base_rev"),
			Describe:    describeWith(func(a map[string]any) string { return "Settle " + Ref(str(a, "id")) + " and preserve final tracking" }),
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				a, err := args[struct {
					ID      string `json:"id"`
					BaseRev int    `json:"base_rev"`
				}](in)
				if err != nil {
					return nil, err
				}
				return m.settle(ctx, a.ID, a.BaseRev)
			}},
		{Name: "ops_fleet", Level: module.LevelRead,
			Description: "Fleet machines and their latest observed Cerberus health. With host, include its recent 5-minute health samples and shared Tether agent rows. CPU percent is utilization when available; cpu is logical core count in the inventory fallback. Load is not utilization. Reads are cached for 30 seconds. Missing observations remain unknown.",
			Input:       schema(`"host":{"type":"string","description":"A machine name from the fleet list; omitted lists machines."}`),
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				a, err := args[struct{ Host string }](in)
				if err != nil {
					return nil, err
				}
				return m.fleet(ctx, a.Host)
			}},
		{Name: "ops_usage", Level: module.LevelRead,
			Description: "Usage against limits for what we pay for or are rate-limited on: one card per source with status, usage, limit and reset time. tether-ai is Tether's own AI router (calls Tether makes itself), not llm-gateway; llm-gateway, which carries Basalisk and Studio turns, is a gap until wired. With source=tether-ai, include its details: request and token totals since 00:00 UTC, 7 and 30 days, the biggest providers and models, budgets and recent budget alerts. Subscriptions not yet collected are gaps, not zero. Read-only; cached for a minute.",
			Input:       schema(`"source":{"type":"string","description":"A card id from the list (llm-gateway, tether-ai, claude, codex, agy, opencode); omitted lists cards."}`),
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				a, err := args[struct{ Source string }](in)
				if err != nil {
					return nil, err
				}
				return m.usage(ctx, a.Source)
			}},
		{Name: "ops_list", Level: module.LevelRead,
			Description: "The Operations board: workstreams by lane (stage), the Queued band in launch order, and for each its owner, held reason, parent and children, Torque roll-up and last activity (from the cache). Top-level concepts by default; parent lists one concept's children; all lists every level. stage, owner and held narrow it.",
			Input: schema(`"stage":{"type":"string",` + stageEnum + `},"owner":{"type":"string"},"held":{"type":"boolean"},` +
				`"show_settled":{"type":"boolean"},"parent":{"type":"string","description":"List the children of this workstream."},"all":{"type":"boolean","description":"Every level, not just the top."}`),
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				a, err := args[struct {
					Stage, Owner, Parent string
					Held                 *bool
					All                  bool
					ShowSettled          bool `json:"show_settled"`
				}](in)
				if err != nil {
					return nil, err
				}
				return m.toolList(ctx, a.Stage, a.Owner, a.Parent, a.Held, a.All, a.ShowSettled)
			}},
		{Name: "ops_get", Level: module.LevelRead,
			Description: "One workstream: its document (title, owner, stage and since, held, parent, children, active-when list, links, notes, rev for ops_update's base_rev), its Torque roll-up and the last 10 activity rows (rolled up from its children).",
			Input:       schema(idProp, "id"),
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				a, err := args[struct{ ID string }](in)
				if err != nil {
					return nil, err
				}
				return m.toolGet(ctx, a.ID)
			}},
		{Name: "ops_activity", Level: module.LevelRead,
			Description: "Activity across all workstreams when id is omitted; with id, that workstream and its children. Newest first: Torque task states and STATUS comments, status-stream entries and Ops moves. since (RFC 3339), source (torque, comment, status, ops) and task narrow it; workstream and stage also filter the global feed. refresh reads another bounded batch now.",
			Input:       schema(idProp + `,"since":{"type":"string"},"source":{"type":"string","enum":["torque","comment","status","ops"]},"task":{"type":"string"},"refresh":{"type":"boolean"},"limit":{"type":"integer","minimum":1,"maximum":300},"workstream":{"type":"string"},"stage":{"type":"string",` + stageEnum + `}`),
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				a, err := args[struct {
					ID, Since, Source, Task, Workstream, Stage string
					Refresh                                    bool
					Limit                                      int
				}](in)
				if err != nil {
					return nil, err
				}
				if a.ID == "" {
					f := activityFilter{Workstream: a.Workstream, Stage: a.Stage, Source: a.Source, Since: a.Since, Task: a.Task, Limit: a.Limit}
					if inputErr := f.validate(); inputErr != nil {
						return nil, inputErr
					}
					_ = m.sync(ctx, false)
					return m.globalActivity(ctx, f, a.Refresh)
				}
				_ = m.sync(ctx, false)
				r, err := m.rollup(ctx, a.ID, a.Refresh)
				if err != nil {
					return nil, err
				}
				var since time.Time
				if a.Since != "" {
					if since = When(a.Since); since.IsZero() {
						return nil, fmt.Errorf("since %q is not a time (RFC 3339)", a.Since)
					}
				}
				es := slices.DeleteFunc(slices.Clone(r.Entries), func(e Entry) bool {
					return (!since.IsZero() && When(e.At).Before(since)) || (a.Source != "" && e.Source != a.Source) || (a.Task != "" && e.Task != a.Task)
				})
				if a.Limit <= 0 || a.Limit > 300 {
					a.Limit = 60
				}
				if len(es) > a.Limit {
					es = es[:a.Limit]
				}
				return map[string]any{"workstream": a.ID, "entries": es, "errors": r.Errors, "fetched_at": r.FetchedAt}, nil
			}},
		{Name: "ops_create", Level: module.LevelWrite,
			Description: "Create a workstream: a concept of any size, from one task to a mesh-wide change. id is a slug chosen now and never changed (it is the Registry workstream key). Stage defaults to planned; parent nests it under another workstream. Links are typed (Cite kinds).",
			Input: schema(idProp+`,"title":{"type":"string"},"owner":{"type":"string"},"project":{"type":"string","description":"Torque project (PRJ-…)."},`+
				`"status_ns":{"type":"string","description":"Where its status entries are read, e.g. project/<slug>/event/status."},`+
				`"stage":{"type":"string",`+stageEnum+`},"parent":{"type":"string"},"links":{"type":"array","items":`+linkSchema+`},`+
				`"active_when":{"type":"array","items":`+checkSchema+`},"purpose":{"type":"string"},"notes":{"type":"string"},"labels":{"type":"array","items":{"type":"string"},"description":"Free-text unit_plan.workstream_id labels it answers to."}`, "id", "title"),
			Describe: describeWith(func(a map[string]any) string {
				s := fmt.Sprintf("Create workstream ops:%s (%s)", str(a, "id"), str(a, "title"))
				if p := str(a, "parent"); p != "" {
					s += " under ops:" + p
				}
				return s
			}),
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				a, err := args[struct {
					NewWorkstream
					Labels []string `json:"labels"`
				}](in)
				if err != nil {
					return nil, err
				}
				a.Correlation.Labels = a.Labels
				got, err := m.st.Create(ctx, a.NewWorkstream, actor(ctx))
				if err != nil {
					return nil, err
				}
				m.changed(got.ID, "created")
				return map[string]any{"created": Ref(got.ID), "stage": got.Stage, "rev": got.Rev}, nil
			}},
		{Name: "ops_update", Level: module.LevelWrite,
			Description: "Change a workstream: title, owner, project, status_ns, parent (\"\" makes it top level), purpose, notes (replace) or append_note, held (a reason; \"\" clears), active_when (replace), tick (one line's done by index) or add_check. base_rev is the rev you read (ops_get): it is required with notes, purpose, active_means, active_when, labels and tick, which replace what you read, and a workstream changed since is then not changed (conflict; read it again).",
			Input: schema(idProp+`,"base_rev":{"type":"integer"},"title":{"type":"string"},"owner":{"type":"string"},"project":{"type":"string"},"status_ns":{"type":"string"},"parent":{"type":"string"},`+
				`"purpose":{"type":"string"},"active_means":{"type":"string"},"notes":{"type":"string"},"append_note":{"type":"string"},"held":{"type":"string"},`+
				`"active_when":{"type":"array","items":`+checkSchema+`},"tick":{"type":"object","properties":{"index":{"type":"integer"},"done":{"type":"boolean"}},"required":["index","done"],"additionalProperties":false},`+
				`"add_check":`+checkSchema+`,"labels":{"type":"array","items":{"type":"string"}}`, "id"),
			Describe: describeWith(func(a map[string]any) string {
				var keys []string
				for k := range a {
					if k != "id" && k != "base_rev" {
						keys = append(keys, k)
					}
				}
				slices.Sort(keys)
				return fmt.Sprintf("Update ops:%s (%s)", str(a, "id"), strings.Join(keys, ", "))
			}),
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				a, err := args[struct {
					ID      string `json:"id"`
					BaseRev int    `json:"base_rev"`
					Patch
				}](in)
				if err != nil {
					return nil, err
				}
				got, err := m.st.Update(ctx, a.ID, a.BaseRev, a.Patch)
				if err != nil {
					return nil, err
				}
				m.changed(a.ID, "updated")
				return map[string]any{"updated": Ref(a.ID), "rev": got.Rev}, nil
			}},
		{Name: "ops_move", Level: module.LevelWrite,
			Description: "Move a workstream to another stage, saying why; the move is recorded. Into queued puts it at the end of the Queued band; out of queued takes it off. Moves are the owner's decision: make one on their word.",
			Input:       schema(idProp+`,"stage":{"type":"string",`+stageEnum+`},"why":{"type":"string"}`, "id", "stage", "why"),
			Describe: describeWith(func(a map[string]any) string {
				return fmt.Sprintf("Move ops:%s to %s: %s", str(a, "id"), label(str(a, "stage")), str(a, "why"))
			}),
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				a, err := args[struct{ ID, Stage, Why string }](in)
				if err != nil {
					return nil, err
				}
				if strings.TrimSpace(a.Why) == "" {
					return nil, errors.New("say why")
				}
				got, err := m.st.Move(ctx, a.ID, a.Stage, a.Why, actor(ctx))
				if got.ID != "" {
					m.changed(a.ID, "moved")
				}
				if err != nil {
					return nil, err
				}
				return map[string]any{"moved": Ref(a.ID), "stage": got.Stage}, nil
			}},
		{Name: "ops_queue_reorder", Level: module.LevelWrite,
			Description: "Reorder the Queued band: move one queued workstream before or after another queued sibling (same parent), or one place up or down. The order is advisory: it does not drive dispatch.",
			Input:       schema(idProp+`,"before":{"type":"string"},"after":{"type":"string"},"dir":{"type":"string","enum":["up","down"]}`, "id"),
			Describe: describeWith(func(a map[string]any) string {
				switch {
				case str(a, "before") != "":
					return fmt.Sprintf("Queue ops:%s before ops:%s", str(a, "id"), str(a, "before"))
				case str(a, "after") != "":
					return fmt.Sprintf("Queue ops:%s after ops:%s", str(a, "id"), str(a, "after"))
				}
				return fmt.Sprintf("Move ops:%s %s in the queue", str(a, "id"), str(a, "dir"))
			}),
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				a, err := args[struct{ ID, Before, After, Dir string }](in)
				if err != nil {
					return nil, err
				}
				order, err := m.st.Reorder(ctx, a.ID, a.Before, a.After, a.Dir)
				if err != nil {
					return nil, err
				}
				m.changed(a.ID, "reordered")
				return map[string]any{"queue": order}, nil
			}},
		{Name: "ops_link", Level: module.LevelWrite,
			Description: "Add a typed link to a workstream (a Cite kind, an id and a role): a Torque task, epic or plan, a Planner item (origin), a Registry record (identity), a design in Tesseract or Caster, a thread.",
			Input:       schema(idProp+`,"link":`+linkSchema, "id", "link"),
			Describe: describeWith(func(a map[string]any) string {
				l, _ := a["link"].(map[string]any)
				return fmt.Sprintf("Link %s %s to ops:%s", str(l, "kind"), str(l, "id"), str(a, "id"))
			}),
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				a, err := args[struct {
					ID   string
					Link Link
				}](in)
				if err != nil {
					return nil, err
				}
				got, err := m.st.Link(ctx, a.ID, a.Link)
				if err != nil {
					return nil, err
				}
				m.changed(a.ID, "linked")
				return map[string]any{"linked": a.Link, "links": len(got.Links)}, nil
			}},
		{Name: "ops_unlink", Level: module.LevelWrite,
			Description: "Take a link off a workstream, by its kind and id.",
			Input:       schema(idProp+`,"kind":{"type":"string"},"ref":{"type":"string","description":"The linked id."}`, "id", "kind", "ref"),
			Describe: describeWith(func(a map[string]any) string {
				return fmt.Sprintf("Unlink %s %s from ops:%s", str(a, "kind"), str(a, "ref"), str(a, "id"))
			}),
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				a, err := args[struct{ ID, Kind, Ref string }](in)
				if err != nil {
					return nil, err
				}
				got, err := m.st.Unlink(ctx, a.ID, a.Kind, a.Ref)
				if err != nil {
					return nil, err
				}
				m.changed(a.ID, "unlinked")
				return map[string]any{"unlinked": a.Ref, "links": len(got.Links)}, nil
			}},
		{Name: "ops_seed", Level: module.LevelAct,
			Description: "Preview seeding Operations from the owner's workstream tracker, given as its JSON: how each thread maps to a workstream, the proposed concept groupings, and where each follow-up goes. Nothing is written: the preview opens in a window, where the owner edits it and applies it. You never apply a seed. A tracker on disk is read only from the owner's page (Seed, then its path).",
			Input:       schema(`"json":{"type":"string","description":"The tracker's JSON text."}`, "json"),
			Describe: describeWith(func(a map[string]any) string {
				return "Preview an Operations seed (writes nothing)"
			}),
			Run: func(ctx context.Context, h module.Host, in json.RawMessage) (any, error) {
				a, err := args[struct{ JSON string }](in)
				if err != nil {
					return nil, err
				}
				if strings.TrimSpace(a.JSON) == "" {
					return nil, errors.New("give the tracker's JSON; a tracker on disk is read only from the owner's page")
				}
				p, err := m.preview(ctx, "", a.JSON)
				if err != nil {
					return nil, err
				}
				if _, err := h.Desk().Open(ctx, viewSeed, module.OpenOptions{Key: p.ID, Args: map[string]any{"plan": p.ID}, Reason: "the seed preview, for the owner to apply"}); err != nil {
					return nil, err
				}
				inc, groups := 0, []string{}
				for _, w := range p.Workstreams {
					if w.Include {
						inc++
					}
				}
				for _, c := range p.Concepts {
					groups = append(groups, fmt.Sprintf("%s (%s): %s", c.ID, c.Title, strings.Join(c.Members, ", ")))
				}
				owner := 0
				for _, f := range p.Followups {
					if f.Workstream == "" {
						owner++
					}
				}
				return map[string]any{"preview": p.ID, "workstreams": inc, "proposed_concepts": groups,
					"followups": len(p.Followups), "followups_for_owner": owner,
					"note": "The preview is open for the owner. Applying is theirs, in the window."}, nil
			}},
	}
}

func (m *mod) toolList(ctx context.Context, stage, owner, parent string, held *bool, all bool, showSettled ...bool) (any, error) {
	syncErr := m.sync(ctx, false)
	b, err := m.st.All(ctx)
	if err != nil {
		return nil, err
	}
	if len(showSettled) == 0 || !showSettled[0] {
		b = b.unsettled()
	}
	cached := m.st.Cached(ctx)
	lanes := map[string][]map[string]any{}
	for _, w := range b.Workstreams {
		if !all && w.Parent != parent {
			continue
		}
		if (stage != "" && w.Stage != stage) || (owner != "" && !strings.EqualFold(w.Owner, owner)) || (held != nil && (w.Held != nil) != *held) {
			continue
		}
		r := map[string]any{"id": w.ID, "title": w.Title, "owner": w.Owner, "stage_since": w.StageSince}
		if w.Parent != "" {
			r["parent"] = w.Parent
		}
		if len(w.Children) > 0 {
			r["children"] = w.Children
		}
		if w.Held != nil {
			r["held"] = w.Held.Reason
		}
		if c, ok := cached[w.ID]; ok {
			if t := CountsText(c.Counts); t != "" {
				r["tasks"] = t
			}
			if c.Last != nil {
				r["last_activity"] = map[string]any{"at": c.Last.At, "summary": c.Last.Summary}
			}
			if c.Suggest != "" {
				r["suggested_stage"] = c.Suggest + ": " + c.SuggestWhy
			}
		}
		lanes[w.Stage] = append(lanes[w.Stage], r)
	}
	queue := []string{}
	for _, id := range b.Queue {
		if w, ok := b.Find(id); ok && (all || w.Parent == parent) {
			queue = append(queue, id)
		}
	}
	out := map[string]any{"lanes": lanes, "queue": queue, "level": orTop(parent)}
	if syncErr != nil {
		out["problem"] = syncErr.Error()
	}
	return out, nil
}

func orTop(p string) string {
	if p == "" {
		return "top level"
	}
	return Ref(p)
}

func (m *mod) toolGet(ctx context.Context, id string) (any, error) {
	_ = m.sync(ctx, false)
	w, ok, err := m.st.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotFound
	}
	out := map[string]any{"workstream": w, "ref": Ref(id), "caster": DocRef(WSName(id))}
	if r, err := m.rollup(ctx, id, false); err == nil {
		last := r.Entries
		if len(last) > 10 {
			last = last[:10]
		}
		out["tasks"] = map[string]any{"counts": r.Counts, "text": CountsText(r.Counts), "list": r.Tasks}
		out["plans"] = r.Plans
		out["activity"] = last
		if r.Suggest != "" {
			out["suggested_stage"] = r.Suggest + ": " + r.SuggestWhy
		}
		if len(r.Errors) > 0 {
			out["problems"] = r.Errors
		}
	}
	return out, nil
}
