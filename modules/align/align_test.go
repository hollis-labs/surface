package align

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/hollis-labs/surface/module/moduletest"
)

func TestAlignmentProposalsLifecycleAndRevisions(t *testing.T) {
	m := &mod{}
	h := moduletest.Start(t, m)

	// 1. Propose an architecture flow via agent tool.
	out, err := h.Tool("align_propose", `{
		"title": "Surface Sync Architecture",
		"summary": "Local SQLite with Caster read-back",
		"subtype": "architecture",
		"payload": {
			"nodes": [{"id": "n1", "label": "Surface Store"}, {"id": "n2", "label": "Caster"}],
			"edges": [{"from": "n1", "to": "n2", "label": "sync"}],
			"groups": [{"id": "g1", "label": "Local"}],
			"callouts": [{"id": "c1", "node_id": "n1", "tone": "concern", "title": "Conflict resolution", "body": "Check base_rev before overwrite"}]
		}
	}`)
	if err != nil {
		t.Fatalf("align_propose: %v", err)
	}
	prop := out.(Proposal)
	if prop.ID == "" || prop.Subtype != "architecture" || prop.Status != "proposed" || prop.Rev != 1 {
		t.Fatalf("unexpected proposal: %+v", prop)
	}

	// 2. Propose a triage board via HTTP POST /proposals.
	code, body := h.Do("POST", "/proposals", `{
		"title": "Q4 Backlog Triage",
		"subtype": "triage",
		"payload": {
			"buckets": [{"id": "now", "title": "Now"}, {"id": "next", "title": "Next"}],
			"items": [{"id": "i1", "title": "Entity Cards", "bucket_id": "unassigned", "proposed_bucket_id": "now", "status": "active"}]
		}
	}`)
	if code != http.StatusOK {
		t.Fatalf("POST /proposals: %d %s", code, body)
	}
	var triage Proposal
	if err := json.Unmarshal([]byte(body), &triage); err != nil {
		t.Fatal(err)
	}
	if triage.Subtype != "triage" || triage.Rev != 1 {
		t.Fatalf("unexpected triage proposal: %+v", triage)
	}

	// 3. Update proposal via HTTP PUT /proposals/{id} (creates rev 2).
	code, body = h.Do("PUT", "/proposals/"+triage.ID, `{
		"title": "Q4 Backlog Triage (Updated)",
		"payload": {
			"buckets": [{"id": "now", "title": "Now"}],
			"items": [{"id": "i1", "title": "Entity Cards", "bucket_id": "now", "status": "active"}]
		},
		"note": "Moved i1 into Now"
	}`)
	if code != http.StatusOK {
		t.Fatalf("PUT /proposals/%s: %d %s", triage.ID, code, body)
	}
	if err := json.Unmarshal([]byte(body), &triage); err != nil {
		t.Fatal(err)
	}
	if triage.Rev != 2 || len(triage.Revisions) != 2 {
		t.Fatalf("expected rev 2 with 2 revisions, got rev=%d len=%d", triage.Rev, len(triage.Revisions))
	}

	// 4. Decide proposal via POST /proposals/{id}/decide (creates rev 3).
	code, body = h.Do("POST", "/proposals/"+triage.ID+"/decide", `{
		"decision": "approved",
		"note": "Looks good to ship"
	}`)
	if code != http.StatusOK {
		t.Fatalf("POST /proposals/%s/decide: %d %s", triage.ID, code, body)
	}
	if err := json.Unmarshal([]byte(body), &triage); err != nil {
		t.Fatal(err)
	}
	if triage.Status != "approved" || triage.DecisionNote != "Looks good to ship" || triage.Rev != 3 || triage.DecidedAt == "" {
		t.Fatalf("unexpected decided proposal: %+v", triage)
	}

	// 5. Filter via GET /proposals?subtype=triage&status=approved.
	code, body = h.Do("GET", "/proposals?subtype=triage&status=approved", "")
	if code != http.StatusOK {
		t.Fatalf("GET /proposals: %d %s", code, body)
	}
	var list []Proposal
	if err := json.Unmarshal([]byte(body), &list); err != nil || len(list) != 1 || list[0].ID != triage.ID {
		t.Fatalf("filtered list: %+v err=%v", list, err)
	}

	// 6. Verify Cite resolution for align.proposal.
	refs := m.Manifest().Refs
	var propRef *struct {
		found bool
	}
	for _, r := range refs {
		if r.Kind == "align.proposal" {
			resolved, err := r.Resolve(context.Background(), h, []string{prop.ID})
			if err != nil || resolved[prop.ID].Title != "Surface Sync Architecture" {
				t.Fatalf("resolve align.proposal: %+v err=%v", resolved, err)
			}
			rec, err := r.Record(context.Background(), h, prop.ID)
			if err != nil || rec.Body == "" {
				t.Fatalf("record align.proposal: %+v err=%v", rec, err)
			}
			propRef = &struct{ found bool }{true}
		}
	}
	if propRef == nil || !propRef.found {
		t.Fatal("align.proposal RefKind not registered")
	}
}

func TestDropPadStagingAndSafeFileExecution(t *testing.T) {
	m := &mod{}
	h := moduletest.Start(t, m)

	srcDir := t.TempDir()
	dstDir := filepath.Join(t.TempDir(), "organized")
	srcFile := filepath.Join(srcDir, "design-notes.md")
	if err := os.WriteFile(srcFile, []byte("# Design Notes\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// 1. Create Drop Pad via tool.
	out, err := h.Tool("droppad_create", `{
		"title": "Design Assets",
		"summary": "Files to copy into organized folder",
		"mode": "file_ops",
		"operation": "copy"
	}`)
	if err != nil {
		t.Fatalf("droppad_create: %v", err)
	}
	dp := out.(DropPad)

	// 2. Stage file via droppad_stage_files tool.
	stageIn, _ := json.Marshal(map[string]any{
		"id":         dp.ID,
		"target_dir": dstDir,
		"operation":  "copy",
		"items": []map[string]any{
			{"kind": "file", "ref_or_path": srcFile, "label": "design-notes.md"},
			{"kind": "ref", "ref_or_path": "ops:phase-1", "label": "Phase 1 Workstream"},
		},
	})
	out, err = h.Tool("droppad_stage_files", string(stageIn))
	if err != nil {
		t.Fatalf("droppad_stage_files: %v", err)
	}
	dp = out.(DropPad)
	if len(dp.Items) != 2 || dp.Items[0].Status != "staged" {
		t.Fatalf("staged items: %+v", dp.Items)
	}

	// 3. Execute file_ops via HTTP POST /droppads/{id}/execute.
	code, body := h.Do("POST", "/droppads/"+dp.ID+"/execute", `{}`)
	if code != http.StatusOK {
		t.Fatalf("POST /droppads/%s/execute: %d %s", dp.ID, code, body)
	}
	if err := json.Unmarshal([]byte(body), &dp); err != nil {
		t.Fatal(err)
	}
	if dp.Status != "executed" || dp.Items[0].Status != "done" {
		t.Fatalf("executed droppad: %+v", dp)
	}
	copiedBytes, err := os.ReadFile(filepath.Join(dstDir, "design-notes.md"))
	if err != nil || string(copiedBytes) != "# Design Notes\n" {
		t.Fatalf("copied file mismatch: %q err=%v", string(copiedBytes), err)
	}

	// 4. Delete Drop Pad via HTTP DELETE /droppads/{id}.
	code, _ = h.Do("DELETE", "/droppads/"+dp.ID, "")
	if code != http.StatusOK {
		t.Fatalf("DELETE /droppads/%s: %d", dp.ID, code)
	}
	code, _ = h.Do("GET", "/droppads/"+dp.ID, "")
	if code != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", code)
	}
}
