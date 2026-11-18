package notes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/hollis-labs/surface/module"
)

func in[T any](raw json.RawMessage) (T, error) {
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		return v, fmt.Errorf("bad input: %w", err)
	}
	return v, nil
}

// pathOr is the path a tool was given, or the note in front when it was given none.
func (m *mod) pathOr(ctx context.Context, p string) (string, error) {
	if strings.TrimSpace(p) != "" {
		return p, nil
	}
	if front := m.front(ctx); front != "" {
		return front, nil
	}
	return "", errors.New("no path given and no note is open: name the note (notes_list shows them)")
}

func numbered(text string, from, count int) (string, int) {
	lines := strings.Split(text, "\n")
	total := len(lines)
	if from < 1 {
		from = 1
	}
	if count <= 0 || count > 2000 {
		count = 2000
	}
	var b strings.Builder
	for i := from; i <= total && i < from+count; i++ {
		fmt.Fprintf(&b, "%4d  %s\n", i, lines[i-1])
	}
	return b.String(), total
}

func (m *mod) tools() []module.Tool {
	raw := func(s string) json.RawMessage { return json.RawMessage(s) }
	return []module.Tool{
		{Name: "notes_list", Level: module.LevelRead,
			Description: "List the owner's notes (path, title, when last changed), most recently changed first. q keeps the ones whose path or title contains it.",
			Input:       raw(`{"type":"object","properties":{"q":{"type":"string"},"limit":{"type":"integer"}}}`),
			Run: func(_ context.Context, _ module.Host, r json.RawMessage) (any, error) {
				a, err := in[struct {
					Q     string
					Limit int
				}](r)
				if err != nil {
					return nil, err
				}
				notes, err := m.st.list()
				if err != nil {
					return nil, err
				}
				q := strings.ToLower(a.Q)
				out := []Note{}
				for _, n := range notes {
					if q == "" || strings.Contains(strings.ToLower(n.Path), q) || strings.Contains(strings.ToLower(n.Title), q) {
						out = append(out, n)
					}
				}
				if a.Limit <= 0 || a.Limit > 300 {
					a.Limit = 100
				}
				total := len(out)
				if total > a.Limit {
					out = out[:a.Limit]
				}
				return map[string]any{"notes": out, "total": total}, nil
			}},
		{Name: "notes_read", Level: module.LevelRead,
			Description: "Read a note: its text with line numbers, its rev (pass it to notes_edit or notes_write so a change made meanwhile is not overwritten), and the notes that link to it. With no path: the note in front. For a long note pass from and count (lines).",
			Input:       raw(`{"type":"object","properties":{"path":{"type":"string","description":"As notes_list gives it. Omit for the note in front."},"from":{"type":"integer"},"count":{"type":"integer"}}}`),
			Run: func(ctx context.Context, _ module.Host, r json.RawMessage) (any, error) {
				a, err := in[struct {
					Path        string
					From, Count int
				}](r)
				if err != nil {
					return nil, err
				}
				p, err := m.pathOr(ctx, a.Path)
				if err != nil {
					return nil, err
				}
				n, err := m.st.read(p)
				if err != nil {
					return nil, err
				}
				text, total := numbered(n.Text, a.From, a.Count)
				back, _ := m.st.backlinks(n.Path)
				var from []string
				for _, b := range back {
					from = append(from, b.Path)
				}
				return map[string]any{"path": n.Path, "title": n.Title, "rev": n.Rev, "ref": "notes:" + n.Path, "lines": total, "text": text, "linked_from": from}, nil
			}},
		{Name: "notes_search", Level: module.LevelRead,
			Description: "Find the lines in the owner's notes that contain some text (case does not matter). Returns path, line number and the line.",
			Input:       raw(`{"type":"object","required":["q"],"properties":{"q":{"type":"string"},"limit":{"type":"integer"}}}`),
			Run: func(_ context.Context, _ module.Host, r json.RawMessage) (any, error) {
				a, err := in[struct {
					Q     string
					Limit int
				}](r)
				if err != nil {
					return nil, err
				}
				return m.st.search(a.Q, a.Limit)
			}},
		{Name: "notes_edit", Level: module.LevelWrite,
			Description: "Change one passage of a note: find is the exact text to replace (it must occur once; quote enough to be unique), replace is what goes there. Prefer this to notes_write for a change to an existing note. With no path: the note in front.",
			Input:       raw(`{"type":"object","required":["find","replace"],"properties":{"path":{"type":"string"},"find":{"type":"string"},"replace":{"type":"string"},"rev":{"type":"string","description":"The rev you read; the edit is refused if the note changed since."}}}`),
			Describe: func(r json.RawMessage) string {
				a, _ := in[struct{ Path, Find string }](r)
				return fmt.Sprintf("Edit the note %s: replace “%s”", orFront(a.Path), clip(strings.ReplaceAll(a.Find, "\n", " "), 60))
			},
			Run: func(ctx context.Context, _ module.Host, r json.RawMessage) (any, error) {
				a, err := in[struct{ Path, Find, Replace, Rev string }](r)
				if err != nil {
					return nil, err
				}
				p, err := m.pathOr(ctx, a.Path)
				if err != nil {
					return nil, err
				}
				n, err := m.st.edit(p, a.Find, a.Replace, a.Rev)
				if err != nil {
					return nil, err
				}
				m.changed(n.Path, "saved")
				return map[string]any{"path": n.Path, "rev": n.Rev, "saved": true}, nil
			}},
		{Name: "notes_write", Level: module.LevelWrite,
			Description: "Write a whole note. For a new note give path (folders are made as needed; .md is added) and text, and it is refused if the note exists. To replace an existing note's text pass replace: true and the rev you read.",
			Input:       raw(`{"type":"object","required":["path","text"],"properties":{"path":{"type":"string"},"text":{"type":"string"},"replace":{"type":"boolean"},"rev":{"type":"string"},"open":{"type":"boolean","description":"Open the note in a window afterwards."}}}`),
			Describe: func(r json.RawMessage) string {
				a, _ := in[struct {
					Path    string
					Replace bool
				}](r)
				if a.Replace {
					return "Replace the text of the note " + a.Path
				}
				return "Create the note " + a.Path
			},
			Run: func(ctx context.Context, _ module.Host, r json.RawMessage) (any, error) {
				a, err := in[struct {
					Path, Text, Rev string
					Replace, Open   bool
				}](r)
				if err != nil {
					return nil, err
				}
				n, err := m.st.write(a.Path, a.Text, a.Rev, !a.Replace)
				if err != nil {
					return nil, err
				}
				what := "created"
				if a.Replace {
					what = "saved"
				}
				m.changed(n.Path, what)
				if a.Open {
					if _, err := m.open(ctx, n.Path); err != nil {
						return nil, err
					}
				}
				return map[string]any{"path": n.Path, "rev": n.Rev, "ref": "notes:" + n.Path, what: true}, nil
			}},
		{Name: "notes_open", Level: module.LevelAct,
			Description: "Show a note to the owner in the Notes window.",
			Input:       raw(`{"type":"object","required":["path"],"properties":{"path":{"type":"string"}}}`),
			Run: func(ctx context.Context, _ module.Host, r json.RawMessage) (any, error) {
				a, err := in[struct{ Path string }](r)
				if err != nil {
					return nil, err
				}
				if _, err := m.st.read(a.Path); err != nil {
					return nil, err
				}
				return m.open(ctx, a.Path)
			}},
		{Name: "notes_rename", Level: module.LevelConfirm,
			Description: "Rename or move a note. Links to it written as [[name]] are not rewritten: say so, and offer to fix them with notes_edit.",
			Input:       raw(`{"type":"object","required":["from","to"],"properties":{"from":{"type":"string"},"to":{"type":"string"}}}`),
			Describe: func(r json.RawMessage) string {
				a, _ := in[struct{ From, To string }](r)
				return "Rename the note " + a.From + " to " + a.To
			},
			Run: func(_ context.Context, _ module.Host, r json.RawMessage) (any, error) {
				a, err := in[struct{ From, To string }](r)
				if err != nil {
					return nil, err
				}
				n, err := m.st.rename(a.From, a.To)
				if err != nil {
					return nil, err
				}
				m.changed(n.Path, "renamed")
				return map[string]any{"path": n.Path, "renamed": true}, nil
			}},
		{Name: "notes_delete", Level: module.LevelConfirm,
			Description: "Delete a note. It is moved to the notes folder's .trash, where the owner can get it back.",
			Input:       raw(`{"type":"object","required":["path"],"properties":{"path":{"type":"string"}}}`),
			Describe: func(r json.RawMessage) string {
				a, _ := in[struct{ Path string }](r)
				return "Delete the note " + a.Path
			},
			Run: func(_ context.Context, _ module.Host, r json.RawMessage) (any, error) {
				a, err := in[struct{ Path string }](r)
				if err != nil {
					return nil, err
				}
				kept, err := m.st.remove(a.Path)
				if err != nil {
					return nil, err
				}
				m.changed(a.Path, "deleted")
				return map[string]any{"deleted": true, "kept_in": kept}, nil
			}},
	}
}

func orFront(p string) string {
	if p == "" {
		return "in front"
	}
	return p
}
