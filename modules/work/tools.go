package work

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/hollis-labs/surface/module"
)

// The agent's Work tools, ported from Basalisk's page (frontend/src/work/agent.ts) with the
// same names, inputs, levels and answers. A draft note is the owner's to send: the agent
// never sends a round, signs off or publishes.

const (
	wsProp = `"workspace":{"type":"string","description":"The workspace's slug, as work_list gives it."}`
	idProp = `"id":{"type":"string","description":"The item's id (8 letters and digits), as work_list gives it."}`
	// change is what an item create or update may set.
	changeProps = `"title":{"type":"string","description":"Item title"},` +
		`"fields":{"type":"object","additionalProperties":true},` +
		`"sections":{"type":"object","additionalProperties":{"type":"string"},"description":"Text by section heading; omitted sections stay unchanged."},` +
		`"tags":{"type":"array","items":{"type":"string","description":"Value"}},` +
		`"links":{"type":"array","items":{"type":"object","properties":{"rel":{"type":"string","description":"Relation, e.g. relates or depends-on (contains uses parent instead)."},"to":{"type":"string","description":"Item id, torque://task/CW-…, tesseract://item/<id>, caster://…, or http(s) URL."}},"required":["rel","to"],"additionalProperties":false}}`
)

// schema is an input object: its properties (JSON, without braces) and what is required.
func schema(props string, required ...string) json.RawMessage {
	req, _ := json.Marshal(required)
	if required == nil {
		req = []byte("[]")
	}
	return json.RawMessage(`{"type":"object","properties":{` + props + `},"required":` + string(req) + `,"additionalProperties":false}`)
}

// args reads a tool's input.
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

// describeWith makes a Describe from a plain function of the input's strings.
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

// errPublish: publishing is the owner's.
var errPublish = errors.New("publishing is the owner's action")

func (m *mod) tools() []module.Tool {
	return []module.Tool{
		{Name: "work_list", Level: module.LevelRead,
			Description: "List Work: without workspace, the workspaces in Content (articles, videos, posts), Planner (projects, features, tasks) and Writer (books, chapters, settings), each with its slug, app, title and item count. With workspace, its items: id, kind, title, stage, parent, a first line, and whether it is signed off. kind and stage narrow the items.",
			Input: schema(wsProp + `,"app":{"type":"string","enum":["content","planner","writer"]},` +
				`"kind":{"type":"string","description":"Only items of this kind (video, chapter, task…)."},` +
				`"stage":{"type":"string","description":"Only items at this stage."},` +
				`"include_cut":{"type":"boolean","description":"Include cut items for restoration"}`),
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				a, err := args[struct {
					Workspace, App, Kind, Stage string
					IncludeCut                  bool `json:"include_cut"`
				}](in)
				if err != nil {
					return nil, err
				}
				return m.toolList(ctx, a.Workspace, a.App, a.Kind, a.Stage, a.IncludeCut)
			}},
		{Name: "work_get", Level: module.LevelRead,
			Description: "Read a Work item: its kind, stage, fields, tags and links, the whole text as Markdown under its section headings, the owner's draft notes not yet sent, the last round's notes with the team's replies, and its sign-off (and whether its text has changed since).",
			Input:       schema(wsProp+","+idProp, "workspace", "id"),
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				a, err := args[struct{ Workspace, ID string }](in)
				if err != nil {
					return nil, err
				}
				return m.toolGet(ctx, a.Workspace, a.ID)
			}},
		{Name: "work_note", Level: module.LevelWrite,
			Description: "Add a draft note to a Work item, for the owner to look over and send with the next round: a comment (on the whole item, or on one section: give section), or a suggestion (section, quote: the exact passage in that section as it is now, replace: what to put instead). This only drafts: sending the round, signing off and publishing are the owner's.",
			Input: schema(wsProp+","+idProp+`,"kind":{"type":"string","enum":["comment","suggestion"]},`+
				`"section":{"type":"string","description":"The section's heading, as work_get lists them. Leave out for a comment on the whole item."},`+
				`"quote":{"type":"string","description":"For a suggestion: the exact passage to replace."},`+
				`"replace":{"type":"string","description":"For a suggestion: what to put instead."},`+
				`"text":{"type":"string","description":"The note (for a suggestion, why; optional)."}`, "workspace", "id", "kind"),
			Describe: describeWith(func(a map[string]any) string {
				s := fmt.Sprintf("Draft a %s on %s/%s", str(a, "kind"), str(a, "workspace"), str(a, "id"))
				if sec := str(a, "section"); sec != "" {
					s += " (" + sec + ")"
				}
				return s
			}),
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				a, err := args[struct {
					Workspace, ID, Kind, Section, Quote, Text string
					Replace                                   *string
				}](in)
				if err != nil {
					return nil, err
				}
				if a.Kind == "suggestion" && (a.Section == "" || a.Quote == "" || a.Replace == nil) {
					return nil, errors.New("a suggestion needs section, quote and replace")
				}
				if a.Kind == "comment" && strings.TrimSpace(a.Text) == "" {
					return nil, errors.New("a comment needs text")
				}
				_ = m.sync(ctx, a.Workspace, false)
				n := Note{Kind: a.Kind, Section: a.Section, Quote: a.Quote, Text: a.Text}
				if a.Replace != nil {
					n.Replace = *a.Replace
				}
				note, err := m.st.AddNote(ctx, a.Workspace, a.ID, n)
				if err != nil {
					return nil, err
				}
				m.changed(a.Workspace, a.ID, "note")
				return map[string]any{"drafted": note.ID, "note": "It is in the item's draft round; the owner sends it."}, nil
			}},
		{Name: "work_space_create", Level: module.LevelWrite,
			Description: "Create a Work workspace. Use app planner to organize plans and child items; content for publication drafts; writer for manuscripts. Does not send, sign off or publish.",
			Input: schema(`"app":{"type":"string","enum":["planner","content","writer"]},`+
				`"slug":{"type":"string","description":"Unique lowercase letters, digits and hyphens."},`+
				`"title":{"type":"string","description":"Workspace title"},`+
				`"about":{"type":"string","description":"Workspace description"}`, "app", "slug", "title"),
			Describe: describeWith(func(a map[string]any) string {
				return fmt.Sprintf("Create %s workspace “%s”", str(a, "app"), str(a, "title"))
			}),
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				a, err := args[struct{ App, Slug, Title, About string }](in)
				if err != nil {
					return nil, err
				}
				slug, err := m.st.NewWorkspace(ctx, a.Slug, a.Title, a.App, a.About)
				if err != nil {
					return nil, err
				}
				m.changed(slug, "", "workspace")
				return map[string]any{"workspace": slug}, nil
			}},
		{Name: "work_item_create", Level: module.LevelWrite,
			Description: "Create a Work item, optionally inside a parent, with text by section, fields, tags and links. Read work_list for kinds and stages. Publishing is reserved for the owner.",
			Input: schema(wsProp+`,"kind":{"type":"string","description":"Kind from work_list"},`+changeProps+
				`,"parent":{"type":"string","description":"The item's id (8 letters and digits), as work_list gives it."},`+
				`"stage":{"type":"string","description":"Stage from work_list"}`, "workspace", "kind", "title"),
			Describe: describeWith(func(a map[string]any) string {
				return fmt.Sprintf("Create %s “%s” in %s", str(a, "kind"), str(a, "title"), str(a, "workspace"))
			}),
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				a, err := args[struct {
					Workspace string `json:"workspace"`
					NewItem
				}](in)
				if err != nil {
					return nil, err
				}
				if a.Stage == StagePublished {
					return nil, errPublish
				}
				_ = m.sync(ctx, a.Workspace, false)
				id, err := m.st.MakeItem(ctx, a.Workspace, a.NewItem)
				if err != nil {
					return nil, err
				}
				m.changed(a.Workspace, id, "item")
				return map[string]any{"id": id}, nil
			}},
		{Name: "work_item_update", Level: module.LevelWrite,
			Description: "Edit a Work item's title, text by section, fields, tags or links. Give base_rev from work_get: a concurrent change becomes suggestions rather than being overwritten. tags and links replace their full lists. Use work_item_place for parent and order.",
			Input:       schema(wsProp+","+idProp+`,"base_rev":{"type":"integer","minimum":1},`+changeProps, "workspace", "id", "base_rev"),
			Describe: describeWith(func(a map[string]any) string {
				return fmt.Sprintf("Edit %s/%s", str(a, "workspace"), str(a, "id"))
			}),
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				a, err := args[struct {
					Workspace, ID string
					BaseRev       int `json:"base_rev"`
					Title         *string
					Fields        map[string]any
					Sections      map[string]string
					Tags          *[]string
					Links         *[]Link
				}](in)
				if err != nil {
					return nil, err
				}
				_ = m.sync(ctx, a.Workspace, false)
				rev, err := m.st.EditItem(ctx, a.Workspace, a.ID, Edit{BaseRev: a.BaseRev, Title: a.Title, Fields: a.Fields, Sections: a.Sections, Tags: a.Tags, Links: a.Links})
				var c *Conflict
				if errors.As(err, &c) {
					m.changed(a.Workspace, a.ID, "note")
				}
				if err != nil {
					return nil, err
				}
				m.changed(a.Workspace, a.ID, "item")
				return map[string]any{"rev": rev}, nil
			}},
		{Name: "work_item_place", Level: module.LevelWrite,
			Description: "Change a Work item's parent and sibling order. Empty parent means top level; empty before means last. Give the outline view id to persist top-level order.",
			Input: schema(wsProp+","+idProp+`,"parent":{"type":"string","description":"Parent id or empty for top level"},`+
				`"before":{"type":"string","description":"Sibling id or empty for last"},`+
				`"view":{"type":"string","description":"Outline view id"}`, "workspace", "id", "parent"),
			Describe: describeWith(func(a map[string]any) string {
				p := str(a, "parent")
				if p == "" {
					p = "the workspace"
				}
				return fmt.Sprintf("Move %s under %s", str(a, "id"), p)
			}),
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				a, err := args[struct{ Workspace, ID, Parent, Before, View string }](in)
				if err != nil {
					return nil, err
				}
				_ = m.sync(ctx, a.Workspace, false)
				if err := m.st.Place(ctx, a.Workspace, a.ID, a.Parent, a.Before, a.View); err != nil {
					return nil, err
				}
				m.changed(a.Workspace, a.ID, "item")
				return map[string]any{"ok": true}, nil
			}},
		{Name: "work_item_stage", Level: module.LevelWrite,
			Description: "Move a Work item between its kind's stages. Cannot publish; that remains the owner's action.",
			Input:       schema(wsProp+","+idProp+`,"stage":{"type":"string","description":"Stage from work_list"}`, "workspace", "id", "stage"),
			Describe: describeWith(func(a map[string]any) string {
				return fmt.Sprintf("Move %s to %s", str(a, "id"), str(a, "stage"))
			}),
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				a, err := args[struct{ Workspace, ID, Stage string }](in)
				if err != nil {
					return nil, err
				}
				if a.Stage == StagePublished {
					return nil, errPublish
				}
				_ = m.sync(ctx, a.Workspace, false)
				rev, err := m.st.Move(ctx, a.Workspace, a.ID, "stage", a.Stage)
				if err != nil {
					return nil, err
				}
				m.changed(a.Workspace, a.ID, "item")
				return map[string]any{"rev": rev}, nil
			}},
		{Name: "work_item_delete", Level: module.LevelConfirm,
			Description: "Remove a Work item from views (recoverable cut, keeps Caster history). Always asks the owner.",
			Input:       schema(wsProp+","+idProp, "workspace", "id"),
			Describe: describeWith(func(a map[string]any) string {
				return fmt.Sprintf("Remove %s/%s from Work", str(a, "workspace"), str(a, "id"))
			}),
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				a, err := args[struct{ Workspace, ID string }](in)
				if err != nil {
					return nil, err
				}
				if err := m.st.Cut(ctx, a.Workspace, a.ID); err != nil {
					return nil, err
				}
				m.changed(a.Workspace, a.ID, "item")
				return map[string]any{"ok": true}, nil
			}},
		{Name: "work_space_update", Level: module.LevelWrite,
			Description: "Rename a workspace's title or mark it archived (or unarchive it). Its slug and references stay stable.",
			Input:       schema(wsProp+`,"title":{"type":"string","description":"New title"},"archived":{"type":"boolean"}`, "workspace"),
			Describe: describeWith(func(a map[string]any) string {
				return fmt.Sprintf("Update workspace %s", str(a, "workspace"))
			}),
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				a, err := args[struct {
					Workspace string
					Title     *string
					Archived  *bool
				}](in)
				if err != nil {
					return nil, err
				}
				if err := m.st.UpdateWorkspace(ctx, a.Workspace, a.Title, a.Archived, nil); err != nil {
					return nil, err
				}
				m.changed(a.Workspace, "", "workspace")
				return map[string]bool{"updated": true}, nil
			}},
		{Name: "work_space_delete", Level: module.LevelConfirm,
			Description: "Remove a workspace from the workspace list, retaining its Caster data and history. Always asks.",
			Input:       schema(wsProp, "workspace"),
			Describe: describeWith(func(a map[string]any) string {
				return fmt.Sprintf("Remove workspace %s", str(a, "workspace"))
			}),
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				a, err := args[struct{ Workspace string }](in)
				if err != nil {
					return nil, err
				}
				deleted := true
				if err := m.st.UpdateWorkspace(ctx, a.Workspace, nil, nil, &deleted); err != nil {
					return nil, err
				}
				m.changed(a.Workspace, "", "workspace")
				return map[string]bool{"deleted": true}, nil
			}},
		{Name: "work_item_restore", Level: module.LevelWrite,
			Description: "Restore a cut item. Use work_list with include_cut to find it.",
			Input:       schema(wsProp+","+idProp, "workspace", "id"),
			Describe: describeWith(func(a map[string]any) string {
				return fmt.Sprintf("Restore %s/%s", str(a, "workspace"), str(a, "id"))
			}),
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				a, err := args[struct{ Workspace, ID string }](in)
				if err != nil {
					return nil, err
				}
				if err := m.st.Restore(ctx, a.Workspace, a.ID); err != nil {
					return nil, err
				}
				m.changed(a.Workspace, a.ID, "item")
				return map[string]any{"ok": true}, nil
			}},
		{Name: "work_note_update", Level: module.LevelWrite,
			Description: "Edit a draft review note's text or replacement; never sends the round.",
			Input: schema(wsProp+`,"id":{"type":"string","description":"Draft note ID"},`+
				`"text":{"type":"string","description":"Text"},"replace":{"type":"string","description":"Replacement"}`, "workspace", "id"),
			Describe: describeWith(func(a map[string]any) string {
				return fmt.Sprintf("Edit draft note %s in %s", str(a, "id"), str(a, "workspace"))
			}),
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				a, err := args[struct {
					Workspace, ID string
					Text, Replace *string
				}](in)
				if err != nil {
					return nil, err
				}
				if err := m.st.EditNote(ctx, a.Workspace, a.ID, a.Text, a.Replace); err != nil {
					return nil, err
				}
				m.changed(a.Workspace, "", "note")
				return map[string]any{"ok": true}, nil
			}},
		{Name: "work_note_delete", Level: module.LevelConfirm,
			Description: "Delete a draft review note. Always asks.",
			Input:       schema(wsProp+`,"id":{"type":"string","description":"Draft note ID"}`, "workspace", "id"),
			Describe: describeWith(func(a map[string]any) string {
				return fmt.Sprintf("Delete draft note %s in %s", str(a, "id"), str(a, "workspace"))
			}),
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				a, err := args[struct{ Workspace, ID string }](in)
				if err != nil {
					return nil, err
				}
				if err := m.st.DropNote(ctx, a.Workspace, a.ID); err != nil {
					return nil, err
				}
				m.changed(a.Workspace, "", "note")
				return map[string]any{"ok": true}, nil
			}},
		{Name: "work_history", Level: module.LevelRead,
			Description: "Read a Work item's revision history or a specific historical revision. name is its Caster name from work_get.",
			Input:       schema(wsProp+`,"name":{"type":"string","description":"Caster item name"},"rev":{"type":"integer","minimum":1}`, "workspace", "name"),
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				a, err := args[struct {
					Workspace, Name string
					Rev             *int
				}](in)
				if err != nil {
					return nil, err
				}
				if k, _ := ParseName(a.Name); k == NameOther {
					return nil, errors.New("that is not a work item")
				}
				if a.Rev == nil {
					revs, lerr := m.st.Log(ctx, a.Workspace, a.Name)
					if lerr != nil {
						return nil, lerr
					}
					return map[string]any{"revisions": revs}, nil
				}
				it, ok, err := m.st.At(ctx, a.Workspace, a.Name, *a.Rev)
				if err != nil {
					return nil, err
				}
				if !ok {
					return nil, errors.New("there is no such revision")
				}
				return atResult(it), nil
			}},
		{Name: "work_view_save", Level: module.LevelWrite,
			Description: "Create or edit a saved Work view (board, calendar, outline, canvas or flow). Omit id to create. Only supplied settings change; layout content is revisioned in Caster.",
			Input: schema(wsProp+`,"id":{"type":"string","description":"Existing view ID, omitted for a new view"},`+
				`"view":{"type":"string","enum":["board","calendar","outline","canvas","flow"]},`+
				`"title":{"type":"string","description":"Title"},"kinds":{"type":"array","items":{"type":"string"}},`+
				`"group_by":{"type":"string","description":"Grouping field"},"date_field":{"type":"string","description":"Date field"},`+
				`"root":{"type":"string","description":"Outline root item"},"rel":{"type":"array","items":{"type":"string"}},`+
				`"filter":{"type":"object"}`, "workspace"),
			Describe: describeWith(func(a map[string]any) string {
				if id := str(a, "id"); id != "" {
					return fmt.Sprintf("Change view %s in %s", id, str(a, "workspace"))
				}
				return fmt.Sprintf("Make a %s view in %s", str(a, "view"), str(a, "workspace"))
			}),
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				a, err := args[map[string]any](in)
				if err != nil {
					return nil, err
				}
				ws, _ := a["workspace"].(string)
				id, _ := a["id"].(string)
				patch := map[string]any{}
				for _, k := range []string{"view", "title", "kinds", "group_by", "date_field", "root", "rel", "filter"} {
					if v, ok := a[k]; ok {
						patch[k] = v
					}
				}
				if id == "" {
					id = NewID()
				}
				rev, err := m.st.SaveView(ctx, ws, id, patch)
				if err != nil {
					return nil, err
				}
				m.changed(ws, id, "view")
				return map[string]any{"id": id, "rev": rev}, nil
			}},
	}
}

// toolList is work_list: the workspaces, or one workspace's kinds, views and items.
func (m *mod) toolList(ctx context.Context, ws, app, kind, stage string, includeCut bool) (any, error) {
	if ws == "" {
		_, rerr := m.st.Refresh(ctx, IndexNS, false)
		list, err := m.st.Workspaces(ctx, app)
		if err != nil {
			return nil, err
		}
		if len(list) == 0 && rerr != nil {
			return nil, rerr
		}
		out := make([]map[string]any, 0, len(list))
		for _, w := range list {
			out = append(out, map[string]any{"workspace": w.Slug, "app": w.App, "title": w.Title, "items": w.Items, "archived": w.Archived})
		}
		return out, nil
	}
	rerr := m.sync(ctx, ws, false)
	sp, ok, err := m.st.Open(ctx, ws)
	if err != nil {
		return nil, err
	}
	if !ok {
		if rerr != nil {
			return nil, rerr
		}
		return nil, errors.New("there is no such workspace")
	}
	kinds := make([]map[string]any, 0, len(sp.Kinds))
	for _, k := range sp.Kinds {
		kinds = append(kinds, map[string]any{"kind": k.Kind, "label": k.Label, "stages": k.Stages, "fields": k.Fields, "sections": k.Sections, "children": k.Children})
	}
	items := []map[string]any{}
	for _, c := range sp.Items {
		if (!includeCut && c.Cut) || (kind != "" && c.Kind != kind) || (stage != "" && c.Stage != stage) {
			continue
		}
		it := map[string]any{"id": c.ID, "kind": c.Kind, "title": c.Title, "stage": c.Stage, "excerpt": c.Excerpt, "words": c.Words}
		if c.Cut {
			it["cut"] = true
		}
		if c.Parent != "" {
			it["parent"] = c.Parent
		}
		if c.Excerpt == "" {
			delete(it, "excerpt")
		}
		if c.Signed {
			if c.StaleSign {
				it["signed_off"] = "changed since"
			} else {
				it["signed_off"] = true
			}
		}
		items = append(items, it)
	}
	return map[string]any{"workspace": sp.Workspace.Slug, "title": sp.Workspace.Title, "app": sp.Workspace.App, "kinds": kinds, "views": sp.Views, "items": items}, nil
}

// draftNote is a draft note as work_get gives it.
type draftNote struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Section string `json:"section,omitempty"`
	Quote   string `json:"quote,omitempty"`
	Replace string `json:"replace,omitempty"`
	Text    string `json:"text,omitempty"`
}

// toolGet is work_get: one item whole.
func (m *mod) toolGet(ctx context.Context, ws, id string) (any, error) {
	rerr := m.sync(ctx, ws, false)
	d, ok, err := m.st.Detail(ctx, ws, id)
	if err != nil {
		return nil, err
	}
	if !ok {
		if rerr != nil {
			return nil, rerr
		}
		return nil, errors.New("there is no such item")
	}
	parts := []string{}
	if d.Lead != "" {
		parts = append(parts, d.Lead)
	}
	for _, s := range d.Order {
		parts = append(parts, "## "+s+"\n\n"+strings.TrimSpace(d.Sections[s]))
	}
	notes := make([]draftNote, 0, len(d.Notes))
	for _, n := range d.Notes {
		notes = append(notes, draftNote{ID: n.ID, Kind: n.Kind, Section: n.Section, Quote: n.Quote, Replace: n.Replace, Text: n.Text})
	}
	out := map[string]any{
		"id": d.ID, "ref": d.Ref, "name": ItemName(d.ID), "kind": d.Kind, "title": d.Title, "stage": d.Stage, "rev": d.Rev,
		"fields": d.Fields, "tags": d.Tags, "links": d.Links, "children": d.Children, "sections": d.Order,
		"text": strings.Join(parts, "\n\n"), "draft_notes": notes, "changed_since_round": d.Changed,
		"last_round": nil, "signed_off": nil,
	}
	if d.Parent != "" {
		out["parent"] = d.Parent
	}
	if len(d.Rounds) > 0 {
		last := d.Rounds[0]
		out["last_round"] = map[string]any{"round": last.Round, "sent_at": last.SentAt, "open": last.Open, "notes": last.Notes}
	}
	if d.SignOff != nil {
		out["signed_off"] = map[string]any{"at": d.SignOff.At, "rev": d.SignOff.Rev, "changed_since": d.StaleSign}
	}
	return out, nil
}
