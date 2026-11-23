// Package align is the Visual Alignment & Drop Pad module: architecture flow proposals,
// bucket triage boards, phased timelines, and drag-and-drop file/ref Drop Pads shared
// between the owner and the agent.
package align

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
	viewID           = "align"
	citeProposalKind = "align.proposal"
	citeDropPadKind  = "align.droppad"
)

var (
	proposalIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{1,63}$`)
	dropPadIDRe  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{1,63}$`)
)

type mod struct {
	module.Base
	h  module.Host
	st *store
}

func (m *mod) Manifest() module.Manifest {
	return module.Manifest{
		ID:      "align",
		Title:   "Align",
		Version: "0.1.0",
		About:   "Visual alignment proposals (architecture flow, bucket triage, phased timeline) and drag-and-drop Drop Pads shared with the agent.",
		Views: []module.View{{
			ID: viewID, Title: "Align", Icon: "compass", Width: 1180, Height: 760,
			Menu: true, Bare: true,
			Render:   module.Component("align/main"),
			Describe: m.describeWindow,
		}},
		Commands: []module.Command{
			{
				ID: "align.open", Title: "Align: open proposals & drop pads", Section: "Align",
				Keywords: "align proposal architecture flow triage buckets timeline droppad files",
				Run: func(ctx context.Context, h module.Host, args map[string]any) (any, error) {
					return h.Desk().Open(ctx, viewID, module.OpenOptions{Args: args})
				},
			},
		},
		Store: &module.Store{Migrations: migrations, Dir: "migrations"},
		Tools: m.tools(),
		Refs:  m.refs(),
		Slots: []module.Slot{{
			Name:  "align",
			About: "visual alignment proposals (architecture diagrams with callouts, bucket triage boards, phased timelines) and Drop Pads for drag-and-drop file/reference collections and staged file operations",
			Views: []string{viewID},
			Tools: []string{
				"align_list", "align_get", "align_propose", "align_update", "align_decide",
				"droppad_list", "droppad_get", "droppad_create", "droppad_update", "droppad_stage_files",
			},
			Where: "the module's SQLite store (align_proposals, align_proposal_revisions, align_droppads)",
			Instructions: "Use align_propose to put an architecture flow, bucket triage, or timeline proposal in front of the owner, and align_get to read the owner's edits and decisions. " +
				"Use droppad_get / droppad_list to inspect files and references the owner dropped into a Drop Pad, and droppad_stage_files to stage file copy/move operations for the owner to review and execute.",
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
		m.h.Events().Publish("align.changed", map[string]any{"what": what, "id": id})
	}
}

func (m *mod) describeWindow(ctx context.Context, _ module.Host, w module.Window) (module.WindowContext, error) {
	wc := module.WindowContext{Kind: "data", Ref: "align"}
	if id, _ := w.Args["proposal"].(string); id != "" {
		wc.Ref = "align:" + strings.TrimPrefix(id, "align:")
		if p, err := m.st.getProposal(ctx, id, false); err == nil {
			wc.Extra = map[string]any{"proposal": p.ID, "title": p.Title, "subtype": p.Subtype, "status": p.Status, "rev": p.Rev}
		}
		return wc, nil
	}
	if id, _ := w.Args["droppad"].(string); id != "" {
		wc.Ref = "droppad:" + strings.TrimPrefix(id, "droppad:")
		if dp, err := m.st.getDropPad(ctx, id); err == nil {
			wc.Extra = map[string]any{"droppad": dp.ID, "title": dp.Title, "mode": dp.Mode, "status": dp.Status, "items": len(dp.Items)}
		}
		return wc, nil
	}
	return wc, nil
}

func (m *mod) knowledge(ctx context.Context, _ module.Host, _ module.Situation) (string, error) {
	if m.st == nil {
		return "", nil
	}
	proposals, _ := m.st.listProposals(ctx, "", "")
	pads, _ := m.st.listDropPads(ctx, "", "")
	if len(proposals) == 0 && len(pads) == 0 {
		return "Align has no proposals or drop pads yet.", nil
	}
	var b strings.Builder
	if len(proposals) > 0 {
		b.WriteString("Alignment proposals:\n")
		for i, p := range proposals {
			if i >= 10 {
				break
			}
			fmt.Fprintf(&b, "- [align:%s] (%s · %s · rev %d) %s\n", p.ID, p.Subtype, p.Status, p.Rev, p.Title)
		}
	}
	if len(pads) > 0 {
		b.WriteString("Drop Pads:\n")
		for i, dp := range pads {
			if i >= 10 {
				break
			}
			fmt.Fprintf(&b, "- [droppad:%s] (%s · %s · %d items) %s\n", dp.ID, dp.Mode, dp.Status, len(dp.Items), dp.Title)
		}
	}
	return strings.TrimSpace(b.String()), nil
}

func (m *mod) routes(h module.Host) {
	h.Handle("GET /proposals", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		list, err := m.st.listProposals(r.Context(), q.Get("subtype"), q.Get("status"))
		reply(w, list, err)
	}))
	h.Handle("POST /proposals", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in proposalInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			reply(w, nil, fmt.Errorf("%w: %w", errBadInput, err))
			return
		}
		if in.Actor == "" {
			in.Actor = "owner"
		}
		p, err := m.st.createProposal(r.Context(), in)
		if err == nil {
			m.changed("proposal.created", p.ID)
		}
		reply(w, p, err)
	}))
	h.Handle("GET /proposals/{id}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, err := m.st.getProposal(r.Context(), r.PathValue("id"), true)
		reply(w, p, err)
	}))
	h.Handle("PUT /proposals/{id}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var patch proposalPatch
		if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
			reply(w, nil, fmt.Errorf("%w: %w", errBadInput, err))
			return
		}
		p, err := m.st.updateProposal(r.Context(), r.PathValue("id"), patch)
		if err == nil {
			m.changed("proposal.updated", p.ID)
		}
		reply(w, p, err)
	}))
	h.Handle("DELETE /proposals/{id}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		err := m.st.deleteProposal(r.Context(), id)
		if err == nil {
			m.changed("proposal.deleted", id)
		}
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	h.Handle("POST /proposals/{id}/decide", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in decideInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			reply(w, nil, fmt.Errorf("%w: %w", errBadInput, err))
			return
		}
		p, err := m.st.decideProposal(r.Context(), r.PathValue("id"), in)
		if err == nil {
			m.changed("proposal.decided", p.ID)
		}
		reply(w, p, err)
	}))

	h.Handle("GET /droppads", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		list, err := m.st.listDropPads(r.Context(), q.Get("mode"), q.Get("status"))
		reply(w, list, err)
	}))
	h.Handle("POST /droppads", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in dropPadInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			reply(w, nil, fmt.Errorf("%w: %w", errBadInput, err))
			return
		}
		dp, err := m.st.createDropPad(r.Context(), in)
		if err == nil {
			m.changed("droppad.created", dp.ID)
		}
		reply(w, dp, err)
	}))
	h.Handle("GET /droppads/{id}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dp, err := m.st.getDropPad(r.Context(), r.PathValue("id"))
		reply(w, dp, err)
	}))
	h.Handle("PUT /droppads/{id}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var patch dropPadPatch
		if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
			reply(w, nil, fmt.Errorf("%w: %w", errBadInput, err))
			return
		}
		dp, err := m.st.updateDropPad(r.Context(), r.PathValue("id"), patch)
		if err == nil {
			m.changed("droppad.updated", dp.ID)
		}
		reply(w, dp, err)
	}))
	h.Handle("DELETE /droppads/{id}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		err := m.st.deleteDropPad(r.Context(), id)
		if err == nil {
			m.changed("droppad.deleted", id)
		}
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	h.Handle("POST /droppads/{id}/execute", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dp, err := m.st.executeDropPad(r.Context(), r.PathValue("id"))
		if err == nil {
			m.changed("droppad.executed", dp.ID)
		}
		reply(w, dp, err)
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
			Kind: citeProposalKind, Label: "Alignment proposal", ID: proposalIDRe,
			Resolve: func(ctx context.Context, _ module.Host, ids []string) (map[string]module.RefEntity, error) {
				out := map[string]module.RefEntity{}
				for _, id := range ids {
					if p, err := m.st.getProposal(ctx, id, false); err == nil {
						out[id] = m.proposalEntity(p, false)
					}
				}
				return out, nil
			},
			Record: func(ctx context.Context, _ module.Host, id string) (module.RefEntity, error) {
				p, err := m.st.getProposal(ctx, id, true)
				if errors.Is(err, errNotFound) {
					return module.RefEntity{}, module.ErrRefNotFound
				}
				if err != nil {
					return module.RefEntity{}, err
				}
				return m.proposalEntity(p, true), nil
			},
		},
		{
			Kind: citeDropPadKind, Label: "Drop Pad", ID: dropPadIDRe,
			Resolve: func(ctx context.Context, _ module.Host, ids []string) (map[string]module.RefEntity, error) {
				out := map[string]module.RefEntity{}
				for _, id := range ids {
					if dp, err := m.st.getDropPad(ctx, id); err == nil {
						out[id] = m.dropPadEntity(dp, false)
					}
				}
				return out, nil
			},
			Record: func(ctx context.Context, _ module.Host, id string) (module.RefEntity, error) {
				dp, err := m.st.getDropPad(ctx, id)
				if errors.Is(err, errNotFound) {
					return module.RefEntity{}, module.ErrRefNotFound
				}
				if err != nil {
					return module.RefEntity{}, err
				}
				return m.dropPadEntity(dp, true), nil
			},
		},
	}
}

func (m *mod) proposalEntity(p Proposal, full bool) module.RefEntity {
	e := module.RefEntity{
		Title:       p.Title,
		Subtitle:    "Align · " + p.Subtype,
		Description: p.Summary,
		UpdatedAt:   p.UpdatedAt,
		ChipLabel:   "Align: " + p.Title,
		Status:      &module.RefStatus{Label: p.Status, Tone: module.StatusTone(p.Status)},
		Open:        &module.RefOpen{Command: "align.open", Args: map[string]any{"proposal": p.ID}},
		Fields: []module.RefField{
			{Label: "ID", Value: "align:" + p.ID, Mono: true},
			{Label: "Subtype", Value: p.Subtype},
			{Label: "Revision", Value: fmt.Sprint(p.Rev)},
		},
	}
	if p.DecisionNote != "" {
		e.Fields = append(e.Fields, module.RefField{Label: "Decision note", Value: p.DecisionNote})
	}
	if full {
		raw, _ := json.MarshalIndent(p, "", "  ")
		e.Body = string(raw)
	}
	return e
}

func (m *mod) dropPadEntity(dp DropPad, full bool) module.RefEntity {
	e := module.RefEntity{
		Title:       dp.Title,
		Subtitle:    "Drop Pad · " + dp.Mode,
		Description: dp.Summary,
		UpdatedAt:   dp.UpdatedAt,
		ChipLabel:   "Drop Pad: " + dp.Title,
		Status:      &module.RefStatus{Label: dp.Status, Tone: module.StatusTone(dp.Status)},
		Open:        &module.RefOpen{Command: "align.open", Args: map[string]any{"droppad": dp.ID}},
		Fields: []module.RefField{
			{Label: "ID", Value: "droppad:" + dp.ID, Mono: true},
			{Label: "Mode", Value: dp.Mode},
			{Label: "Items", Value: fmt.Sprint(len(dp.Items))},
		},
	}
	if dp.TargetDir != "" {
		e.Fields = append(e.Fields, module.RefField{Label: "Target", Value: dp.TargetDir, Mono: true})
	}
	if full {
		raw, _ := json.MarshalIndent(dp, "", "  ")
		e.Body = string(raw)
	}
	return e
}

func (m *mod) tools() []module.Tool {
	raw := func(s string) json.RawMessage { return json.RawMessage(s) }
	return []module.Tool{
		{
			Name:        "align_list",
			Level:       module.LevelRead,
			Description: "List visual alignment proposals (architecture flow, bucket triage, timeline), optionally filtered by subtype or status.",
			Input:       raw(`{"type":"object","properties":{"subtype":{"type":"string","enum":["architecture","triage","timeline"]},"status":{"type":"string","enum":["draft","proposed","approved","changes_requested","parked"]}}}`),
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				var a struct {
					Subtype string `json:"subtype"`
					Status  string `json:"status"`
				}
				if err := json.Unmarshal(in, &a); err != nil {
					return nil, err
				}
				return m.st.listProposals(ctx, a.Subtype, a.Status)
			},
		},
		{
			Name:        "align_get",
			Level:       module.LevelRead,
			Description: "Read an alignment proposal by id, including its current payload, status, decision note, and revision history.",
			Input:       raw(`{"type":"object","required":["id"],"properties":{"id":{"type":"string"}}}`),
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				var a struct {
					ID string `json:"id"`
				}
				if err := json.Unmarshal(in, &a); err != nil {
					return nil, err
				}
				return m.st.getProposal(ctx, a.ID, true)
			},
		},
		{
			Name:        "align_propose",
			Level:       module.LevelWrite,
			Description: "Create a visual alignment proposal for the owner: architecture ({nodes, edges, groups, callouts, notes?}), triage ({buckets, items}), or timeline ({phases, items}). Set open: true to open it in the Align window.",
			Input:       raw(`{"type":"object","required":["title","payload"],"properties":{"id":{"type":"string"},"title":{"type":"string"},"summary":{"type":"string"},"subtype":{"type":"string","enum":["architecture","triage","timeline"]},"status":{"type":"string","enum":["draft","proposed"]},"payload":{"type":"object"},"note":{"type":"string"},"open":{"type":"boolean"}}}`),
			Describe: func(in json.RawMessage) string {
				var a struct{ Title string }
				_ = json.Unmarshal(in, &a)
				return "Propose alignment: “" + a.Title + "”"
			},
			Run: func(ctx context.Context, h module.Host, in json.RawMessage) (any, error) {
				var a struct {
					proposalInput
					Open bool `json:"open"`
				}
				if err := json.Unmarshal(in, &a); err != nil {
					return nil, err
				}
				a.Actor = "agent"
				p, err := m.st.createProposal(ctx, a.proposalInput)
				if err != nil {
					return nil, err
				}
				m.changed("proposal.created", p.ID)
				if a.Open {
					_, _ = h.Desk().Open(ctx, viewID, module.OpenOptions{Args: map[string]any{"proposal": p.ID}, Reason: "Alignment proposal: " + p.Title})
				}
				return p, nil
			},
		},
		{
			Name:        "align_update",
			Level:       module.LevelWrite,
			Description: "Update an existing alignment proposal's payload, title, summary, or status, saving a new revision.",
			Input:       raw(`{"type":"object","required":["id"],"properties":{"id":{"type":"string"},"title":{"type":"string"},"summary":{"type":"string"},"subtype":{"type":"string","enum":["architecture","triage","timeline"]},"status":{"type":"string","enum":["draft","proposed","approved","changes_requested","parked"]},"payload":{"type":"object"},"note":{"type":"string"}}}`),
			Describe: func(in json.RawMessage) string {
				var a struct{ ID string }
				_ = json.Unmarshal(in, &a)
				return "Update alignment proposal " + a.ID
			},
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				var a struct {
					ID string `json:"id"`
					proposalPatch
				}
				if err := json.Unmarshal(in, &a); err != nil {
					return nil, err
				}
				a.Actor = "agent"
				p, err := m.st.updateProposal(ctx, a.ID, a.proposalPatch)
				if err == nil {
					m.changed("proposal.updated", p.ID)
				}
				return p, err
			},
		},
		{
			Name:        "align_decide",
			Level:       module.LevelWrite,
			Description: "Record a decision (approved, changes_requested, parked) on an alignment proposal, optionally saving an updated payload and decision note.",
			Input:       raw(`{"type":"object","required":["id","decision"],"properties":{"id":{"type":"string"},"decision":{"type":"string","enum":["approved","changes_requested","parked"]},"note":{"type":"string"},"payload":{"type":"object"}}}`),
			Describe: func(in json.RawMessage) string {
				var a struct{ ID, Decision string }
				_ = json.Unmarshal(in, &a)
				return "Mark alignment proposal " + a.ID + " as " + a.Decision
			},
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				var a struct {
					ID string `json:"id"`
					decideInput
				}
				if err := json.Unmarshal(in, &a); err != nil {
					return nil, err
				}
				if a.Actor == "" {
					a.Actor = "agent"
				}
				p, err := m.st.decideProposal(ctx, a.ID, a.decideInput)
				if err == nil {
					m.changed("proposal.decided", p.ID)
				}
				return p, err
			},
		},
		{
			Name:        "droppad_list",
			Level:       module.LevelRead,
			Description: "List Drop Pads (drag-and-drop file/reference collections and staged file operation pads).",
			Input:       raw(`{"type":"object","properties":{"mode":{"type":"string","enum":["collection","file_ops"]},"status":{"type":"string","enum":["active","executed","archived"]}}}`),
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				var a struct {
					Mode   string `json:"mode"`
					Status string `json:"status"`
				}
				if err := json.Unmarshal(in, &a); err != nil {
					return nil, err
				}
				return m.st.listDropPads(ctx, a.Mode, a.Status)
			},
		},
		{
			Name:        "droppad_get",
			Level:       module.LevelRead,
			Description: "Read a Drop Pad and all its staged items (files, references, notes).",
			Input:       raw(`{"type":"object","required":["id"],"properties":{"id":{"type":"string"}}}`),
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				var a struct {
					ID string `json:"id"`
				}
				if err := json.Unmarshal(in, &a); err != nil {
					return nil, err
				}
				return m.st.getDropPad(ctx, a.ID)
			},
		},
		{
			Name:        "droppad_create",
			Level:       module.LevelWrite,
			Description: "Create a new Drop Pad for collecting files/refs or staging file copy/move operations.",
			Input:       raw(`{"type":"object","required":["title"],"properties":{"id":{"type":"string"},"title":{"type":"string"},"summary":{"type":"string"},"mode":{"type":"string","enum":["collection","file_ops"]},"target_dir":{"type":"string"},"operation":{"type":"string","enum":["copy","move","reference"]},"items":{"type":"array","items":{"type":"object","properties":{"id":{"type":"string"},"kind":{"type":"string","enum":["file","ref","note"]},"ref_or_path":{"type":"string"},"label":{"type":"string"},"dest_path":{"type":"string"},"status":{"type":"string","enum":["staged","done","skipped"]},"metadata":{"type":"object"}}}}}}`),
			Describe: func(in json.RawMessage) string {
				var a struct{ Title string }
				_ = json.Unmarshal(in, &a)
				return "Create Drop Pad: “" + a.Title + "”"
			},
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				var a dropPadInput
				if err := json.Unmarshal(in, &a); err != nil {
					return nil, err
				}
				dp, err := m.st.createDropPad(ctx, a)
				if err == nil {
					m.changed("droppad.created", dp.ID)
				}
				return dp, err
			},
		},
		{
			Name:        "droppad_update",
			Level:       module.LevelWrite,
			Description: "Update a Drop Pad's title, summary, mode, target_dir, operation, status, or items list.",
			Input:       raw(`{"type":"object","required":["id"],"properties":{"id":{"type":"string"},"title":{"type":"string"},"summary":{"type":"string"},"mode":{"type":"string","enum":["collection","file_ops"]},"target_dir":{"type":"string"},"operation":{"type":"string","enum":["copy","move","reference"]},"status":{"type":"string","enum":["active","executed","archived"]},"items":{"type":"array","items":{"type":"object","properties":{"id":{"type":"string"},"kind":{"type":"string","enum":["file","ref","note"]},"ref_or_path":{"type":"string"},"label":{"type":"string"},"dest_path":{"type":"string"},"status":{"type":"string","enum":["staged","done","skipped"]},"metadata":{"type":"object"}}}}}}`),
			Describe: func(in json.RawMessage) string {
				var a struct{ ID string }
				_ = json.Unmarshal(in, &a)
				return "Update Drop Pad " + a.ID
			},
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				var a struct {
					ID string `json:"id"`
					dropPadPatch
				}
				if err := json.Unmarshal(in, &a); err != nil {
					return nil, err
				}
				dp, err := m.st.updateDropPad(ctx, a.ID, a.dropPadPatch)
				if err == nil {
					m.changed("droppad.updated", dp.ID)
				}
				return dp, err
			},
		},
		{
			Name:        "droppad_stage_files",
			Level:       module.LevelWrite,
			Description: "Stage file copy/move/reference items into a Drop Pad (creating or updating items for the owner to review and execute).",
			Input:       raw(`{"type":"object","required":["id","items"],"properties":{"id":{"type":"string"},"target_dir":{"type":"string"},"operation":{"type":"string","enum":["copy","move","reference"]},"items":{"type":"array","items":{"type":"object","required":["ref_or_path"],"properties":{"id":{"type":"string"},"kind":{"type":"string","enum":["file","ref","note"]},"ref_or_path":{"type":"string"},"label":{"type":"string"},"dest_path":{"type":"string"},"status":{"type":"string","enum":["staged","done","skipped"]},"metadata":{"type":"object"}}}}}}`),
			Describe: func(in json.RawMessage) string {
				var a struct{ ID string }
				_ = json.Unmarshal(in, &a)
				return "Stage files in Drop Pad " + a.ID
			},
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				var a struct {
					ID        string        `json:"id"`
					TargetDir string        `json:"target_dir"`
					Operation string        `json:"operation"`
					Items     []DropPadItem `json:"items"`
				}
				if err := json.Unmarshal(in, &a); err != nil {
					return nil, err
				}
				dp, err := m.st.stageDropPadItems(ctx, a.ID, a.TargetDir, a.Operation, a.Items)
				if err == nil {
					m.changed("droppad.staged", dp.ID)
				}
				return dp, err
			},
		},
	}
}
