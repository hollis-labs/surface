package drafts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/hollis-labs/surface/module"
)

func raw(s string) json.RawMessage { return json.RawMessage(s) }

// draftID is the id given, or the draft in front.
func (m *mod) draftID(ctx context.Context, id string) (string, error) {
	if id != "" {
		return id, nil
	}
	if id = m.focused(ctx); id == "" {
		return "", errors.New("no draft is open; give its id (drafts_list)")
	}
	return id, nil
}

func (m *mod) tools() []module.Tool {
	return []module.Tool{
		{Name: "drafts_list", Level: module.LevelRead,
			Description: "List the drafts, most recently changed first. q filters on title and text.",
			Input:       raw(`{"type":"object","properties":{"q":{"type":"string"}}}`),
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				var a struct{ Q string }
				_ = json.Unmarshal(in, &a)
				return m.st.list(ctx, a.Q)
			}},
		{Name: "drafts_get", Level: module.LevelRead,
			Description: "Read a draft (the one in front when id is left out): its title, revision and text with line numbers, and its open marks. rev reads an older revision; from/to read only those lines.",
			Input:       raw(`{"type":"object","properties":{"id":{"type":"string"},"rev":{"type":"integer"},"from":{"type":"integer"},"to":{"type":"integer"}}}`),
			Run: func(ctx context.Context, h module.Host, in json.RawMessage) (any, error) {
				var a struct {
					ID       string
					Rev      int
					From, To int
				}
				_ = json.Unmarshal(in, &a)
				id, err := m.draftID(ctx, a.ID)
				if err != nil {
					return nil, err
				}
				d, err := m.st.get(ctx, id)
				if err != nil {
					return nil, err
				}
				text, rev := d.Body, d.Rev
				if a.Rev > 0 && a.Rev != d.Rev {
					r, err := m.st.revision(ctx, id, a.Rev)
					if err != nil {
						return nil, err
					}
					text, rev = r.Body, r.Rev
				}
				var b strings.Builder
				fmt.Fprintf(&b, "Draft %s %q (%s), revision %d of %d.\n", d.ID, d.Title, d.Lang, rev, d.Rev)
				if marks, _ := h.Marks().List(ctx, ref(id)); len(marks) > 0 {
					for _, mk := range marks {
						if mk.Status == "open" {
							fmt.Fprintf(&b, "Mark %s by %s on %q: %s\n", mk.ID, mk.Author, clip(mk.Quote, 120), mk.Body)
							for _, r := range mk.Replies {
								fmt.Fprintf(&b, "  reply by %s: %s\n", r.Author, r.Body)
							}
						}
					}
				}
				b.WriteString("\n" + numbered(text, a.From, a.To))
				return b.String(), nil
			}},
		{Name: "drafts_create", Level: module.LevelWrite,
			Description: "Start a new draft and open it on the desk. lang is markdown (default), text or a programming language.",
			Input:       raw(`{"type":"object","required":["title"],"properties":{"title":{"type":"string"},"lang":{"type":"string"},"body":{"type":"string"},"open":{"type":"boolean","description":"Open it in a window (default true)."}}}`),
			Describe: func(in json.RawMessage) string {
				var a struct{ Title string }
				_ = json.Unmarshal(in, &a)
				return "New draft “" + clip(a.Title, 60) + "”"
			},
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				var a struct {
					Title, Lang, Body string
					Open              *bool
				}
				if err := json.Unmarshal(in, &a); err != nil {
					return nil, err
				}
				d, err := m.create(ctx, a.Title, a.Lang, a.Body, "agent")
				if err != nil {
					return nil, err
				}
				if a.Open == nil || *a.Open {
					_, _ = m.open(ctx, d)
				}
				return map[string]any{"id": d.ID, "rev": d.Rev, "title": d.Title}, nil
			}},
		{Name: "drafts_edit", Level: module.LevelWrite,
			Description: "Change a passage of a draft as a new revision: find must match exactly once in the latest text, and is replaced. expect_rev is the revision you read; the edit is refused if the draft has changed since. summary says what the revision changed.",
			Input:       raw(`{"type":"object","required":["find","replace","expect_rev"],"properties":{"id":{"type":"string"},"find":{"type":"string"},"replace":{"type":"string"},"expect_rev":{"type":"integer"},"summary":{"type":"string"}}}`),
			Describe: func(in json.RawMessage) string {
				var a struct{ ID, Find, Summary string }
				_ = json.Unmarshal(in, &a)
				if a.Summary != "" {
					return "Edit draft: " + clip(a.Summary, 80)
				}
				return "Edit draft at “" + clip(a.Find, 50) + "”"
			},
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				var a struct {
					ID, Find, Replace, Summary string
					ExpectRev                  int `json:"expect_rev"`
				}
				if err := json.Unmarshal(in, &a); err != nil {
					return nil, err
				}
				id, err := m.draftID(ctx, a.ID)
				if err != nil {
					return nil, err
				}
				d, err := m.st.get(ctx, id)
				if err != nil {
					return nil, err
				}
				if a.ExpectRev != 0 && a.ExpectRev != d.Rev {
					return nil, errStale
				}
				if m.ownerWorking(ctx, id) {
					return nil, errOwnerWorking
				}
				switch n := strings.Count(d.Body, a.Find); {
				case a.Find == "":
					return nil, errors.New("find is empty")
				case n == 0:
					return nil, errors.New("find does not occur in the latest text; read the draft again")
				case n > 1:
					return nil, fmt.Errorf("find occurs %d times; include more of the surrounding text", n)
				}
				body := strings.Replace(d.Body, a.Find, a.Replace, 1)
				nd, err := m.write(ctx, id, body, "agent", a.Summary, d.Rev, nil)
				if err != nil {
					return nil, err
				}
				return map[string]any{"id": id, "rev": nd.Rev, "diff": unified(diffLines(d.Body, body))}, nil
			}},
		{Name: "drafts_write", Level: module.LevelWrite,
			Description: "Replace a draft's whole text as a new revision (prefer drafts_edit for a passage). expect_rev is the revision you read; refused if it has changed since. title renames it.",
			Input:       raw(`{"type":"object","required":["body","expect_rev"],"properties":{"id":{"type":"string"},"body":{"type":"string"},"expect_rev":{"type":"integer"},"summary":{"type":"string"},"title":{"type":"string"}}}`),
			Describe: func(in json.RawMessage) string {
				var a struct{ Summary string }
				_ = json.Unmarshal(in, &a)
				return "Rewrite draft: " + clip(a.Summary, 80)
			},
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				var a struct {
					ID, Body, Summary string
					Title             *string
					ExpectRev         int `json:"expect_rev"`
				}
				if err := json.Unmarshal(in, &a); err != nil {
					return nil, err
				}
				id, err := m.draftID(ctx, a.ID)
				if err != nil {
					return nil, err
				}
				before, err := m.st.get(ctx, id)
				if err != nil {
					return nil, err
				}
				d, err := m.write(ctx, id, a.Body, "agent", a.Summary, a.ExpectRev, a.Title)
				if err != nil {
					return nil, err
				}
				return map[string]any{"id": id, "rev": d.Rev, "diff": unified(diffLines(before.Body, a.Body))}, nil
			}},
		{Name: "drafts_revisions", Level: module.LevelRead,
			Description: "List a draft's revisions, newest first: who made each and what it changed.",
			Input:       raw(`{"type":"object","properties":{"id":{"type":"string"}}}`),
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				var a struct{ ID string }
				_ = json.Unmarshal(in, &a)
				id, err := m.draftID(ctx, a.ID)
				if err != nil {
					return nil, err
				}
				return m.st.revisions(ctx, id)
			}},
		{Name: "drafts_diff", Level: module.LevelRead,
			Description: "Compare two revisions of a draft as a unified diff. Defaults: the latest against the one before it.",
			Input:       raw(`{"type":"object","properties":{"id":{"type":"string"},"from":{"type":"integer"},"to":{"type":"integer"}}}`),
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				var a struct {
					ID       string
					From, To int
				}
				_ = json.Unmarshal(in, &a)
				id, err := m.draftID(ctx, a.ID)
				if err != nil {
					return nil, err
				}
				x, y, err := m.pair(ctx, id, a.From, a.To)
				if err != nil {
					return nil, err
				}
				return fmt.Sprintf("revision %d → %d\n%s", x.Rev, y.Rev, unified(diffLines(x.Body, y.Body))), nil
			}},
		{Name: "drafts_open", Level: module.LevelAct,
			Description: "Open a draft in a window on the desk, or bring its window forward.",
			Input:       raw(`{"type":"object","required":["id"],"properties":{"id":{"type":"string"}}}`),
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				var a struct{ ID string }
				_ = json.Unmarshal(in, &a)
				d, err := m.st.get(ctx, a.ID)
				if err != nil {
					return nil, err
				}
				return m.open(ctx, d)
			}},
		{Name: "drafts_delete", Level: module.LevelConfirm,
			Description: "Put a draft away. Its revisions are kept.",
			Input:       raw(`{"type":"object","required":["id"],"properties":{"id":{"type":"string"}}}`),
			Run: func(ctx context.Context, h module.Host, in json.RawMessage) (any, error) {
				var a struct{ ID string }
				_ = json.Unmarshal(in, &a)
				if err := m.st.remove(ctx, a.ID); err != nil {
					return nil, err
				}
				h.Events().Publish("drafts.changed", map[string]any{"id": a.ID, "removed": true})
				return "put away", nil
			}},
	}
}
