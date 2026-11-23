// Package trackers is the Native Trackers module: categorized work lists, taxonomy tags,
// lifecycles, and custom metadata/extras for tracking projects, checklists, and queues.
package trackers

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/hollis-labs/surface/module"
)

//go:embed migrations/*.sql
var migrations embed.FS

func init() { module.Register(&mod{}) }

const (
	viewID       = "trackers"
	citeListKind = "tracker.list"
	citeItemKind = "tracker.item"
)

var (
	listIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{1,63}$`)
	itemIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{1,63}$`)
)

type mod struct {
	module.Base
	h  module.Host
	st *store
}

func (m *mod) Manifest() module.Manifest {
	return module.Manifest{
		ID:      "trackers",
		Title:   "Trackers",
		Version: "0.1.0",
		About:   "Categorized work trackers with taxonomy tags, lifecycles, and structured metadata.",
		Views: []module.View{{
			ID: viewID, Title: "Trackers", Icon: "list-todo", Width: 1160, Height: 760,
			Menu: true, Bare: true,
			Render:   module.Component("trackers/main"),
			Describe: m.describeWindow,
		}},
		Commands: []module.Command{
			{
				ID: "trackers.open", Title: "Trackers: open", Section: "Trackers",
				Keywords: "trackers lists items todo checklist work categories tags lifecycle",
				Run: func(ctx context.Context, h module.Host, args map[string]any) (any, error) {
					return h.Desk().Open(ctx, viewID, module.OpenOptions{Args: args})
				},
			},
		},
		Store: &module.Store{Migrations: migrations, Dir: "migrations"},
		Tools: m.tools(),
		Refs:  m.refs(),
		Slots: []module.Slot{{
			Name:  "trackers",
			About: "the owner's categorized work trackers and tracker items, organized by category, taxonomy tags, and lifecycle",
			Views: []string{viewID},
			Tools: []string{
				"tracker_list", "tracker_get", "tracker_create", "tracker_update",
				"tracker_item_add", "tracker_item_update", "tracker_item_delete",
			},
			Where: "the module's SQLite store (trackers and tracker_items tables)",
			Instructions: "Read the owner's trackers with tracker_list and tracker_get before modifying items. " +
				"Reference a tracker as tracker:<id> and an item as tracker.item:<id>. Preserve the owner's taxonomy tags, metadata, and extras when updating items.",
			Knowledge: m.knowledge,
		}},
	}
}

func (m *mod) Start(_ context.Context, h module.Host) error {
	m.h = h
	m.st = &store{db: h.Store()}
	m.routes(h)
	return nil
}

func (m *mod) changed(what, id string) {
	if m.h != nil {
		m.h.Events().Publish("trackers.changed", map[string]any{"what": what, "id": id})
	}
}

func (m *mod) describeWindow(ctx context.Context, _ module.Host, w module.Window) (module.WindowContext, error) {
	wc := module.WindowContext{Kind: "data", Ref: "trackers"}
	if id, _ := w.Args["tracker"].(string); id != "" {
		if t, err := m.st.getTracker(ctx, id, false); err == nil {
			wc.Ref = "tracker:" + t.ID
			wc.Extra = map[string]any{"tracker_id": t.ID, "slug": t.Slug, "title": t.Title, "category": t.Category, "lifecycle": t.Lifecycle, "item_counts": t.ItemCounts}
		}
	}
	return wc, nil
}

func (m *mod) knowledge(ctx context.Context, _ module.Host, _ module.Situation) (string, error) {
	if m.st == nil {
		return "", nil
	}
	list, err := m.st.listTrackers(ctx, trackerFilter{})
	if err != nil {
		return "", fmt.Errorf("list trackers for knowledge: %w", err)
	}
	if len(list) == 0 {
		return "Trackers has no lists yet.", nil
	}
	var b strings.Builder
	b.WriteString("Trackers:\n")
	for i, t := range list {
		if i >= 12 {
			break
		}
		fmt.Fprintf(&b, "- [tracker:%s] (%s · %s · %d items) %s\n", t.ID, t.Category, t.Lifecycle, t.ItemCounts["total"], t.Title)
	}
	return strings.TrimSpace(b.String()), nil
}

func (m *mod) routes(h module.Host) {
	h.Handle("GET /lists", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		list, err := m.st.listTrackers(r.Context(), trackerFilter{
			Category:  q.Get("category"),
			Lifecycle: q.Get("lifecycle"),
			Tag:       q.Get("tag"),
			Q:         q.Get("q"),
		})
		reply(w, list, err)
	}))
	h.Handle("POST /lists", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in trackerInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			reply(w, nil, fmt.Errorf("%w: %w", errBadInput, err))
			return
		}
		t, err := m.st.createTracker(r.Context(), in)
		if err == nil {
			m.changed("list.created", t.ID)
		}
		reply(w, t, err)
	}))
	h.Handle("GET /lists/{id}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t, err := m.st.getTracker(r.Context(), r.PathValue("id"), true)
		reply(w, t, err)
	}))
	h.Handle("PUT /lists/{id}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var patch trackerPatch
		if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
			reply(w, nil, fmt.Errorf("%w: %w", errBadInput, err))
			return
		}
		t, err := m.st.updateTracker(r.Context(), r.PathValue("id"), patch)
		if err == nil {
			m.changed("list.updated", t.ID)
		}
		reply(w, t, err)
	}))
	h.Handle("DELETE /lists/{id}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		err := m.st.deleteTracker(r.Context(), id)
		if err == nil {
			m.changed("list.deleted", id)
		}
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))

	h.Handle("GET /items", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		items, err := m.st.listItems(r.Context(), itemFilter{
			TrackerID: q.Get("tracker_id"),
			Lifecycle: q.Get("lifecycle"),
			Category:  q.Get("category"),
			Tag:       q.Get("tag"),
			Q:         q.Get("q"),
		})
		reply(w, items, err)
	}))
	h.Handle("POST /items", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in itemInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			reply(w, nil, fmt.Errorf("%w: %w", errBadInput, err))
			return
		}
		it, err := m.st.createItem(r.Context(), in)
		if err == nil {
			m.changed("item.created", it.ID)
		}
		reply(w, it, err)
	}))
	h.Handle("GET /items/{id}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		it, err := m.st.getItem(r.Context(), r.PathValue("id"))
		reply(w, it, err)
	}))
	h.Handle("PUT /items/{id}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var patch itemPatch
		if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
			reply(w, nil, fmt.Errorf("%w: %w", errBadInput, err))
			return
		}
		it, err := m.st.updateItem(r.Context(), r.PathValue("id"), patch)
		if err == nil {
			m.changed("item.updated", it.ID)
		}
		reply(w, it, err)
	}))
	h.Handle("DELETE /items/{id}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		err := m.st.deleteItem(r.Context(), id)
		if err == nil {
			m.changed("item.deleted", id)
		}
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	h.Handle("POST /items/reorder", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			TrackerID string   `json:"tracker_id"`
			ItemIDs   []string `json:"item_ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			reply(w, nil, fmt.Errorf("%w: %w", errBadInput, err))
			return
		}
		items, err := m.st.reorderItems(r.Context(), in.TrackerID, in.ItemIDs)
		if err == nil {
			m.changed("items.reordered", in.TrackerID)
		}
		reply(w, items, err)
	}))
}

func reply(w http.ResponseWriter, v any, err error) {
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		status := http.StatusUnprocessableEntity
		switch {
		case errors.Is(err, errNotFound):
			status = http.StatusNotFound
		case errors.Is(err, errBadInput):
			status = http.StatusBadRequest
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(v)
}

func (m *mod) refs() []module.RefKind {
	return []module.RefKind{
		{
			Kind: citeListKind, Label: "Tracker", ID: listIDRe,
			Resolve: func(ctx context.Context, _ module.Host, ids []string) (map[string]module.RefEntity, error) {
				out := map[string]module.RefEntity{}
				for _, id := range ids {
					if t, err := m.st.getTracker(ctx, id, false); err == nil {
						out[id] = m.trackerEntity(t, false)
					}
				}
				return out, nil
			},
			Record: func(ctx context.Context, _ module.Host, id string) (module.RefEntity, error) {
				t, err := m.st.getTracker(ctx, id, true)
				if errors.Is(err, errNotFound) {
					return module.RefEntity{}, module.ErrRefNotFound
				}
				if err != nil {
					return module.RefEntity{}, err
				}
				return m.trackerEntity(t, true), nil
			},
		},
		{
			Kind: citeItemKind, Label: "Tracker item", ID: itemIDRe,
			Resolve: func(ctx context.Context, _ module.Host, ids []string) (map[string]module.RefEntity, error) {
				out := map[string]module.RefEntity{}
				for _, id := range ids {
					if it, err := m.st.getItem(ctx, id); err == nil {
						out[id] = m.itemEntity(it, false)
					}
				}
				return out, nil
			},
			Record: func(ctx context.Context, _ module.Host, id string) (module.RefEntity, error) {
				it, err := m.st.getItem(ctx, id)
				if errors.Is(err, errNotFound) {
					return module.RefEntity{}, module.ErrRefNotFound
				}
				if err != nil {
					return module.RefEntity{}, err
				}
				return m.itemEntity(it, true), nil
			},
		},
	}
}

func (m *mod) trackerEntity(t Tracker, full bool) module.RefEntity {
	e := module.RefEntity{
		Title:       t.Title,
		Subtitle:    "Tracker · " + t.Category,
		Description: t.Summary,
		UpdatedAt:   t.UpdatedAt,
		ChipLabel:   "Tracker: " + t.Title,
		Status:      &module.RefStatus{Label: t.Lifecycle, Tone: module.StatusTone(t.Lifecycle)},
		Open:        &module.RefOpen{Command: "trackers.open", Args: map[string]any{"tracker": t.ID}},
		Fields: []module.RefField{
			{Label: "ID", Value: "tracker:" + t.ID, Mono: true},
			{Label: "Slug", Value: t.Slug, Mono: true},
			{Label: "Category", Value: t.Category},
			{Label: "Items", Value: fmt.Sprint(t.ItemCounts["total"])},
		},
	}
	if len(t.Tags) > 0 {
		e.Fields = append(e.Fields, module.RefField{Label: "Tags", Value: strings.Join(t.Tags, ", ")})
	}
	if full {
		raw, _ := json.MarshalIndent(t, "", "  ")
		e.Body = string(raw)
	}
	return e
}

func (m *mod) itemEntity(it TrackerItem, full bool) module.RefEntity {
	e := module.RefEntity{
		Title:       it.Title,
		Subtitle:    "Tracker item",
		Description: it.Summary,
		UpdatedAt:   it.UpdatedAt,
		ChipLabel:   "Item: " + it.Title,
		Status:      &module.RefStatus{Label: it.Lifecycle, Tone: module.StatusTone(it.Lifecycle)},
		Open:        &module.RefOpen{Command: "trackers.open", Args: map[string]any{"tracker": it.TrackerID, "item": it.ID}},
		Fields: []module.RefField{
			{Label: "ID", Value: "tracker.item:" + it.ID, Mono: true},
			{Label: "Tracker", Value: "tracker:" + it.TrackerID, Mono: true},
		},
	}
	if it.Category != "" {
		e.Fields = append(e.Fields, module.RefField{Label: "Category", Value: it.Category})
	}
	if it.Assignee != "" {
		e.Fields = append(e.Fields, module.RefField{Label: "Assignee", Value: it.Assignee})
	}
	if len(it.Tags) > 0 {
		e.Fields = append(e.Fields, module.RefField{Label: "Tags", Value: strings.Join(it.Tags, ", ")})
	}
	if full {
		raw, _ := json.MarshalIndent(it, "", "  ")
		e.Body = string(raw)
	}
	return e
}

func (m *mod) tools() []module.Tool {
	raw := func(s string) json.RawMessage { return json.RawMessage(s) }
	return []module.Tool{
		{
			Name:        "tracker_list",
			Level:       module.LevelRead,
			Description: "List trackers (categorized work lists) with item counts, optionally filtered by category, lifecycle, tag, or search query q.",
			Input:       raw(`{"type":"object","properties":{"category":{"type":"string"},"lifecycle":{"type":"string","enum":["active","paused","completed","archived"]},"tag":{"type":"string"},"q":{"type":"string"}}}`),
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				var a struct {
					Category  string `json:"category"`
					Lifecycle string `json:"lifecycle"`
					Tag       string `json:"tag"`
					Q         string `json:"q"`
				}
				if err := json.Unmarshal(in, &a); err != nil {
					return nil, err
				}
				return m.st.listTrackers(ctx, trackerFilter{Category: a.Category, Lifecycle: a.Lifecycle, Tag: a.Tag, Q: a.Q})
			},
		},
		{
			Name:        "tracker_get",
			Level:       module.LevelRead,
			Description: "Read a tracker by id or slug, including all its items, counts, tags, metadata, and extras.",
			Input:       raw(`{"type":"object","required":["id"],"properties":{"id":{"type":"string"}}}`),
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				var a struct {
					ID string `json:"id"`
				}
				if err := json.Unmarshal(in, &a); err != nil {
					return nil, err
				}
				return m.st.getTracker(ctx, a.ID, true)
			},
		},
		{
			Name:        "tracker_create",
			Level:       module.LevelWrite,
			Description: "Create a new tracker with title, summary, category, taxonomy tags, lifecycle (active|paused|completed|archived), metadata, and extras.",
			Input:       raw(`{"type":"object","required":["title"],"properties":{"id":{"type":"string"},"slug":{"type":"string"},"title":{"type":"string"},"summary":{"type":"string"},"category":{"type":"string"},"tags":{"type":"array","items":{"type":"string"}},"lifecycle":{"type":"string","enum":["active","paused","completed","archived"]},"metadata":{"type":"object"},"extras":{"type":"object"}}}`),
			Describe: func(in json.RawMessage) string {
				var a struct{ Title string }
				_ = json.Unmarshal(in, &a)
				return "Create tracker: “" + a.Title + "”"
			},
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				var a trackerInput
				if err := json.Unmarshal(in, &a); err != nil {
					return nil, err
				}
				t, err := m.st.createTracker(ctx, a)
				if err == nil {
					m.changed("list.created", t.ID)
				}
				return t, err
			},
		},
		{
			Name:        "tracker_update",
			Level:       module.LevelWrite,
			Description: "Update a tracker's title, slug, summary, category, tags, lifecycle, metadata, or extras.",
			Input:       raw(`{"type":"object","required":["id"],"properties":{"id":{"type":"string"},"slug":{"type":"string"},"title":{"type":"string"},"summary":{"type":"string"},"category":{"type":"string"},"tags":{"type":"array","items":{"type":"string"}},"lifecycle":{"type":"string","enum":["active","paused","completed","archived"]},"metadata":{"type":"object"},"extras":{"type":"object"}}}`),
			Describe: func(in json.RawMessage) string {
				var a struct{ ID string }
				_ = json.Unmarshal(in, &a)
				return "Update tracker " + a.ID
			},
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				var a struct {
					ID string `json:"id"`
					trackerPatch
				}
				if err := json.Unmarshal(in, &a); err != nil {
					return nil, err
				}
				t, err := m.st.updateTracker(ctx, a.ID, a.trackerPatch)
				if err == nil {
					m.changed("list.updated", t.ID)
				}
				return t, err
			},
		},
		{
			Name:        "tracker_item_add",
			Level:       module.LevelWrite,
			Description: "Add an item to a tracker with title, summary, lifecycle (todo|in_progress|blocked|review|done|parked), category, tags, position, assignee, metadata, and extras.",
			Input:       raw(`{"type":"object","required":["tracker_id","title"],"properties":{"id":{"type":"string"},"tracker_id":{"type":"string"},"title":{"type":"string"},"summary":{"type":"string"},"lifecycle":{"type":"string","enum":["todo","in_progress","blocked","review","done","parked"]},"category":{"type":"string"},"tags":{"type":"array","items":{"type":"string"}},"position":{"type":"integer"},"assignee":{"type":"string"},"metadata":{"type":"object"},"extras":{"type":"object"}}}`),
			Describe: func(in json.RawMessage) string {
				var a struct{ Title string }
				_ = json.Unmarshal(in, &a)
				return "Add tracker item: “" + a.Title + "”"
			},
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				var a itemInput
				if err := json.Unmarshal(in, &a); err != nil {
					return nil, err
				}
				it, err := m.st.createItem(ctx, a)
				if err == nil {
					m.changed("item.created", it.ID)
				}
				return it, err
			},
		},
		{
			Name:        "tracker_item_update",
			Level:       module.LevelWrite,
			Description: "Update a tracker item's title, summary, lifecycle (todo|in_progress|blocked|review|done|parked), category, tags, position, assignee, tracker_id, metadata, or extras.",
			Input:       raw(`{"type":"object","required":["id"],"properties":{"id":{"type":"string"},"tracker_id":{"type":"string"},"title":{"type":"string"},"summary":{"type":"string"},"lifecycle":{"type":"string","enum":["todo","in_progress","blocked","review","done","parked"]},"category":{"type":"string"},"tags":{"type":"array","items":{"type":"string"}},"position":{"type":"integer"},"assignee":{"type":"string"},"metadata":{"type":"object"},"extras":{"type":"object"}}}`),
			Describe: func(in json.RawMessage) string {
				var a struct{ ID string }
				_ = json.Unmarshal(in, &a)
				return "Update tracker item " + a.ID
			},
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				var a struct {
					ID string `json:"id"`
					itemPatch
				}
				if err := json.Unmarshal(in, &a); err != nil {
					return nil, err
				}
				it, err := m.st.updateItem(ctx, a.ID, a.itemPatch)
				if err == nil {
					m.changed("item.updated", it.ID)
				}
				return it, err
			},
		},
		{
			Name:        "tracker_item_delete",
			Level:       module.LevelConfirm,
			Description: "Delete a tracker item by id.",
			Input:       raw(`{"type":"object","required":["id"],"properties":{"id":{"type":"string"}}}`),
			Describe: func(in json.RawMessage) string {
				var a struct{ ID string }
				_ = json.Unmarshal(in, &a)
				return "Delete tracker item " + a.ID
			},
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				var a struct {
					ID string `json:"id"`
				}
				if err := json.Unmarshal(in, &a); err != nil {
					return nil, err
				}
				if err := m.st.deleteItem(ctx, a.ID); err != nil {
					return nil, err
				}
				m.changed("item.deleted", a.ID)
				return map[string]bool{"ok": true}, nil
			},
		},
	}
}
