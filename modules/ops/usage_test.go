package ops

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hollis-labs/surface/module"
	"github.com/hollis-labs/surface/module/tetherlink"
)

type usageFake struct {
	mu      sync.Mutex
	calls   map[string]int
	since   []string
	fail    bool
	budgets string
}

func (f *usageFake) Call(_ context.Context, tool string, args map[string]any) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	f.calls[tool]++
	if f.fail {
		return "", errors.New("offline")
	}
	switch tool {
	case "tether_ai_usage":
		since, _ := args["since"].(string)
		f.since = append(f.since, since)
		if since == "2026-11-21T00:00:00Z" {
			return `{"ok":true,"summary":{"errors":1,"requests":3,"successes":2,"input_tokens":30,"output_tokens":9}}`, nil
		}
		return `{"ok":true,"summary":{"requests":40,"successes":39,"errors":1,"by_provider":[` +
			`{"key":"small","requests":2},{"key":"","requests":1,"errors":1},{"key":"big","requests":37,"input_tokens":900}]` +
			`,"by_model":[{"key":"m1","requests":40}]}}`, nil
	case "tether_ai_budgets":
		if f.budgets != "" {
			return f.budgets, nil
		}
		return `{"ok":true,"count":1,"budgets":[{"provider":"jev","model":"jev-1","exhausted":false,"error":"scope needs a purpose",` +
			`"window_start":"2026-11-21T00:00:00Z","usage_budget":{"level":"provider","max_cost_usd":2,"scope":"purpose","window":"day"}}]}`, nil
	case "tether_ai_budget_alerts":
		if args["limit"] != usageAlertsMax {
			return "", errors.New("unbounded alerts")
		}
		return `{"ok":true,"alerts":[{"timestamp":"2026-11-20T01:00:00Z","provider":"jev","error":"old"},{"timestamp":"2026-11-21T01:00:00Z","provider":"jev","error":"new"}]}`, nil
	}
	return "", errors.New("unexpected tool " + tool)
}

func fixedLink(f tetherlink.Caller) func() (tetherlink.Caller, error) {
	return func() (tetherlink.Caller, error) { return f, nil }
}

func TestUsageGatewayCardAndDetail(t *testing.T) {
	now := time.Date(2026, 11, 21, 14, 30, 0, 0, time.UTC)
	f := &usageFake{}
	c := &usageCache{now: func() time.Time { return now }}
	r, err := c.read(t.Context(), fixedLink(f), gatewaySource)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Cards) != 1+len(subscriptions) {
		t.Fatalf("cards: %+v", r.Cards)
	}
	g := r.Cards[0]
	if g.ID != gatewaySource || g.Used == nil || *g.Used != 3 || g.Limit != nil {
		t.Fatalf("gateway card: %+v", g)
	}
	if g.ResetAt != "2026-11-22T00:00:00Z" {
		t.Errorf("reset: %s", g.ResetAt)
	}
	if g.Status != usageWarning || g.Gap == "" {
		t.Errorf("a budget error must warn: %+v", g)
	}
	for _, s := range r.Cards[1:] {
		if s.Status != usageGap || s.Used != nil || s.Gap == "" {
			t.Errorf("subscription must be a labeled gap, not zero: %+v", s)
		}
	}
	d := r.Detail
	if d == nil || len(d.Windows) != 3 || d.Windows[0].Total.Requests != 3 || d.Windows[2].Total.Requests != 40 {
		t.Fatalf("windows: %+v", d)
	}
	if f.since[1] != "2026-11-15T00:00:00Z" || f.since[2] != "2026-10-23T00:00:00Z" {
		t.Errorf("window bounds: %v", f.since)
	}
	if d.ByProvider[0].Key != "big" || d.ByProvider[1].Key != "small" || d.ByProvider[2].Key != "(unrouted)" {
		t.Errorf("consumers: %+v", d.ByProvider)
	}
	if len(d.Budgets) != 1 || d.Budgets[0].ResetAt != "2026-11-22T00:00:00Z" || *d.Budgets[0].MaxCostUSD != 2 {
		t.Errorf("budgets: %+v", d.Budgets)
	}
	if len(d.Alerts) != 2 || d.Alerts[0].Error != "new" {
		t.Errorf("alerts newest first: %+v", d.Alerts)
	}
	// Listing has no detail.
	if r, _ := c.read(t.Context(), fixedLink(f), ""); r.Detail != nil {
		t.Error("list carries detail")
	}
}

func TestUsageExhaustedBudgetAndNoBudgets(t *testing.T) {
	now := time.Date(2026, 11, 21, 14, 30, 0, 0, time.UTC)
	f := &usageFake{budgets: `{"budgets":[{"provider":"p","exhausted":true,"window_start":"2026-11-21T14:00:00Z","usage_budget":{"window":"hour","max_requests":10}}]}`}
	c := &usageCache{now: func() time.Time { return now }}
	r, _ := c.read(t.Context(), fixedLink(f), "")
	if r.Cards[0].Status != usageExhausted || r.Cards[0].ResetAt != "2026-11-21T15:00:00Z" {
		t.Fatalf("exhausted: %+v", r.Cards[0])
	}
	f2 := &usageFake{budgets: `{"budgets":[]}`}
	c2 := &usageCache{now: func() time.Time { return now }}
	r, _ = c2.read(t.Context(), fixedLink(f2), "")
	if !strings.Contains(r.Cards[0].Gap, "No gateway budgets") {
		t.Fatalf("no-budget gap: %+v", r.Cards[0])
	}
}

func TestUsageSharedCacheIncludingFailure(t *testing.T) {
	now := time.Date(2026, 11, 21, 14, 30, 0, 0, time.UTC)
	f := &usageFake{fail: true}
	c := &usageCache{now: func() time.Time { return now }}
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			r, err := c.read(t.Context(), fixedLink(f), "")
			if err != nil || r.Cards[0].Status != usageError || !strings.Contains(r.Cards[0].Gap, "unavailable") {
				t.Errorf("failure card: %+v %v", r.Cards, err)
			}
		})
	}
	wg.Wait()
	if f.calls["tether_ai_usage"] != 1 {
		t.Fatalf("failure not cached: %v", f.calls)
	}
	now = now.Add(usageTTL)
	f.fail = false
	if r, _ := c.read(t.Context(), fixedLink(f), ""); r.Cards[0].Status == usageError {
		t.Fatal("did not refresh after TTL")
	}
	if _, err := c.read(t.Context(), fixedLink(f), "nope"); err == nil {
		t.Fatal("unknown source accepted")
	}
}

func TestUsageToolsAllowedReadOnly(t *testing.T) {
	for _, tool := range []string{"tether_ai_usage", "tether_ai_budgets", "tether_ai_budget_alerts"} {
		if !allowed[tool] {
			t.Errorf("%s not allowed", tool)
		}
	}
	m := &mod{}
	for _, tool := range m.tools() {
		if tool.Name == "ops_usage" && tool.Level != module.LevelRead {
			t.Errorf("ops_usage level %v", tool.Level)
		}
	}
}
