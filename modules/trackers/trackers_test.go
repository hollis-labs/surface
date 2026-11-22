package trackers

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/hollis-labs/surface/module/moduletest"
)

func TestTrackersAndItemsLifecycleTagsReorderAndCite(t *testing.T) {
	m := &mod{}
	h := moduletest.Start(t, m)

	// 1. Create a tracker via agent tool.
	out, err := h.Tool("tracker_create", `{
		"title": "Surface Next Launch Checklist",
		"slug": "surface-next",
		"summary": "Backend and frontend integration milestones",
		"category": "engineering",
		"tags": ["surface", "q4", "launch"],
		"lifecycle": "active",
		"metadata": {"owner": "chrispian"},
		"extras": {"priority": "p1"}
	}`)
	if err != nil {
		t.Fatalf("tracker_create: %v", err)
	}
	trk := out.(Tracker)
	if trk.ID == "" || trk.Slug != "surface-next" || trk.Category != "engineering" || len(trk.Tags) != 3 {
		t.Fatalf("unexpected tracker: %+v", trk)
	}

	// 2. Add two items via agent tool and HTTP POST /items.
	out1, err := h.Tool("tracker_item_add", `{
		"tracker_id": "`+trk.ID+`",
		"title": "Module entity cards",
		"summary": "Resolve live Cite records in card_show and card_ask",
		"lifecycle": "done",
		"category": "backend",
		"tags": ["cards", "cite"],
		"assignee": "agy-be",
		"metadata": {"task": "CW-20261122-0008"},
		"extras": {"points": 3}
	}`)
	if err != nil {
		t.Fatalf("tracker_item_add: %v", err)
	}
	it1 := out1.(TrackerItem)

	code, body := h.Do("POST", "/items", `{
		"tracker_id": "`+trk.ID+`",
		"title": "Visual alignment proposals",
		"lifecycle": "in_progress",
		"category": "backend",
		"tags": ["align", "droppad"],
		"assignee": "agy-be"
	}`)
	if code != http.StatusOK {
		t.Fatalf("POST /items: %d %s", code, body)
	}
	var it2 TrackerItem
	if err := json.Unmarshal([]byte(body), &it2); err != nil {
		t.Fatal(err)
	}
	if it1.Position != 1 || it2.Position != 2 {
		t.Fatalf("expected positions 1 and 2, got %d and %d", it1.Position, it2.Position)
	}

	// 3. Reorder items via POST /items/reorder.
	code, body = h.Do("POST", "/items/reorder", `{
		"tracker_id": "`+trk.ID+`",
		"item_ids": ["`+it2.ID+`", "`+it1.ID+`"]
	}`)
	if code != http.StatusOK {
		t.Fatalf("POST /items/reorder: %d %s", code, body)
	}
	var reordered []TrackerItem
	if err := json.Unmarshal([]byte(body), &reordered); err != nil {
		t.Fatal(err)
	}
	if len(reordered) != 2 || reordered[0].ID != it2.ID || reordered[1].ID != it1.ID {
		t.Fatalf("unexpected reorder result: %+v", reordered)
	}

	// 4. Read tracker with items and item_counts via GET /lists/{id}.
	code, body = h.Do("GET", "/lists/"+trk.ID, "")
	if code != http.StatusOK {
		t.Fatalf("GET /lists/%s: %d %s", trk.ID, code, body)
	}
	var fullTrk Tracker
	if err := json.Unmarshal([]byte(body), &fullTrk); err != nil {
		t.Fatal(err)
	}
	if len(fullTrk.Items) != 2 || fullTrk.ItemCounts["total"] != 2 || fullTrk.ItemCounts["done"] != 1 || fullTrk.ItemCounts["in_progress"] != 1 {
		t.Fatalf("unexpected tracker detail: %+v", fullTrk)
	}

	// 5. Filter items by tag and lifecycle via GET /items.
	code, body = h.Do("GET", "/items?tracker_id="+trk.ID+"&tag=cite&lifecycle=done", "")
	if code != http.StatusOK {
		t.Fatalf("GET /items: %d %s", code, body)
	}
	var filtered []TrackerItem
	if err := json.Unmarshal([]byte(body), &filtered); err != nil || len(filtered) != 1 || filtered[0].ID != it1.ID {
		t.Fatalf("unexpected filtered items: %+v err=%v", filtered, err)
	}

	// 6. Verify Cite ref resolution for tracker.list and tracker.item.
	for _, r := range m.Manifest().Refs {
		switch r.Kind {
		case "tracker.list":
			res, err := r.Resolve(context.Background(), h, []string{trk.ID})
			if err != nil || res[trk.ID].Title != "Surface Next Launch Checklist" {
				t.Fatalf("resolve tracker.list: %+v err=%v", res, err)
			}
			rec, err := r.Record(context.Background(), h, trk.ID)
			if err != nil || rec.Body == "" {
				t.Fatalf("record tracker.list: %+v err=%v", rec, err)
			}
		case "tracker.item":
			res, err := r.Resolve(context.Background(), h, []string{it1.ID})
			if err != nil || res[it1.ID].Title != "Module entity cards" {
				t.Fatalf("resolve tracker.item: %+v err=%v", res, err)
			}
			rec, err := r.Record(context.Background(), h, it1.ID)
			if err != nil || rec.Body == "" {
				t.Fatalf("record tracker.item: %+v err=%v", rec, err)
			}
		}
	}

	// 7. Delete item via tracker_item_delete tool.
	if _, err := h.Tool("tracker_item_delete", `{"id":"`+it2.ID+`"}`); err != nil {
		t.Fatalf("tracker_item_delete: %v", err)
	}
	code, _ = h.Do("GET", "/items/"+it2.ID, "")
	if code != http.StatusNotFound {
		t.Fatalf("expected 404 for deleted item, got %d", code)
	}
}
