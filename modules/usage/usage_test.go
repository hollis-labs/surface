package usage

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hollis-labs/surface/module/moduletest"
)

const testBearerCanary = "CANARY-USAGE-KEY-9941a8bc72"

type fakeStation struct {
	mapTrendsAndInsights bool
	srv                  *httptest.Server
}

func newFakeStation(t *testing.T, mapTrendsAndInsights bool) *fakeStation {
	t.Helper()
	f := &fakeStation{mapTrendsAndInsights: mapTrendsAndInsights}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeStation) serve(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+testBearerCanary {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/accounts":
		_, _ = w.Write([]byte(`{
			"accounts": [
				{
					"id": "claude-personal",
					"provider": "anthropic",
					"kind": "subscription",
					"label": "Claude",
					"plan": "max",
					"enabled": true,
					"health": "ok",
					"stale": false,
					"last_success": {
						"sequence": 101,
						"account_id": "claude-personal",
						"source": "claude-oauth",
						"collected_at": "2026-11-23T03:50:00Z",
						"data": {
							"id": "obs_claude",
							"observed_at": "2026-11-23T03:50:00Z",
							"status": "ok",
							"plan": "max",
							"attributes": {
								"rate_limit_tier": "default_claude_max_20x",
								"reset_credits_available": "2",
								"overage_enabled": "true",
								"signin_expires_at": "2026-11-25T12:00:00Z"
							},
							"windows": [
								{
									"id": "weekly_all",
									"kind": "quota",
									"group": "weekly",
									"unit": "percent",
									"used": 100,
									"limit": 100,
									"remaining": 0,
									"window_seconds": 604800,
									"resets_at": "2026-11-30T03:00:00Z"
								},
								{
									"id": "weekly_scoped:fable",
									"kind": "quota",
									"model": "Fable",
									"group": "weekly",
									"unit": "percent",
									"used": 35,
									"limit": 100,
									"remaining": 65,
									"window_seconds": 604800,
									"resets_at": "2026-11-30T03:00:00Z"
								}
							]
						}
					}
				},
				{
					"id": "gateway-openrouter",
					"provider": "openrouter",
					"kind": "credit",
					"label": "OpenRouter",
					"enabled": true,
					"health": "stale",
					"stale": true,
					"latest_attempt": {
						"sequence": 103,
						"account_id": "gateway-openrouter",
						"source": "billing-openrouter",
						"collected_at": "2026-11-23T03:54:00Z",
						"data": {
							"id": "obs_or_err",
							"observed_at": "2026-11-23T03:54:00Z",
							"status": "error",
							"error": "upstream timeout reading key_limit"
						}
					},
					"last_success": {
						"sequence": 102,
						"account_id": "gateway-openrouter",
						"source": "billing-openrouter",
						"collected_at": "2026-11-23T03:00:00Z",
						"data": {
							"id": "obs_or",
							"observed_at": "2026-11-23T03:00:00Z",
							"status": "ok",
							"windows": [
								{
									"id": "account_balance",
									"kind": "balance",
									"unit": "usd",
									"used": null,
									"limit": 245,
									"remaining": 4.95,
									"resets_at": ""
								}
							]
						}
					}
				}
			]
		}`))
	case "/sources":
		_, _ = w.Write([]byte(`{
			"sources": [
				{
					"id": "claude-oauth",
					"account_id": "claude-personal",
					"collector": "claude",
					"stale": false
				},
				{
					"id": "billing-openrouter",
					"account_id": "gateway-openrouter",
					"collector": "billing",
					"stale": true
				}
			]
		}`))
	case "/capacity":
		_, _ = w.Write([]byte(`{
			"generated_at": "2026-11-23T03:55:00Z",
			"entries": [
				{
					"account_id": "claude-personal",
					"windows": [
						{
							"id": "weekly_scoped:fable",
							"partial_reset": true,
							"expiring": {
								"reset_kind": "partial",
								"seconds_to_reset": 3600
							}
						}
					]
				}
			],
			"use_it_or_lose_it": [
				{
					"rank": 1,
					"provider": "antigravity",
					"account_id": "agy-personal",
					"account_label": "Antigravity",
					"billing_type": "subscription",
					"model_group": "Gemini Models",
					"window_id": "g0:gemini-5h",
					"unit": "percent",
					"forfeitable_percent": 90.27,
					"resets_at": "2026-11-23T04:22:52Z",
					"seconds_to_reset": 1547,
					"reset_kind": "full",
					"urgency_score": 2256.78,
					"modalities": ["text", "image"],
					"gateway_models": ["gemini-2.5-pro"]
				}
			]
		}`))
	case "/summary":
		// Includes two different cost_kind entries plus an unpriced entry with null known_cost_usd
		// for the same account to verify cost_kinds are never summed and null stays null (not 0).
		_, _ = w.Write([]byte(`{
			"totals": [
				{
					"account_id": "claude-personal",
					"source": "claude-oauth",
					"model": "claude-opus-4-7",
					"cost_kind": "subscription_equivalent",
					"events": 120,
					"known_cost_usd": 45.50,
					"unpriced_events": 0,
					"partial_events": 0,
					"known_tokens": 1250000
				},
				{
					"account_id": "claude-personal",
					"source": "claude-oauth",
					"model": "claude-sonnet-4-5",
					"cost_kind": "api_billed",
					"events": 15,
					"known_cost_usd": 12.25,
					"unpriced_events": 0,
					"partial_events": 0,
					"known_tokens": 310000
				},
				{
					"account_id": "claude-personal",
					"source": "claude-oauth",
					"model": "claude-experimental",
					"cost_kind": "unclassified",
					"events": 4,
					"known_cost_usd": null,
					"unpriced_events": 4,
					"partial_events": 0,
					"known_tokens": 8000
				}
			]
		}`))
	case "/sources/claude-oauth/raw":
		_, _ = w.Write([]byte(`{"id":"obs_claude","status":"ok","raw_capture":true}`))
	case "/series", "/periods", "/spend", "/insights", "/resets":
		if !f.mapTrendsAndInsights {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"route_not_mapped"}`))
			return
		}
		switch r.URL.Path {
		case "/series":
			_, _ = w.Write([]byte(`{"series":[{"account_id":"claude-personal","account_label":"Claude","provider":"anthropic","window_id":"weekly_all","unit":"percent","kind":"quota","step":"1h","current_period":[{"at":"2026-11-23T03:00:00Z","used":100,"remaining":0}]}]}`))
		case "/periods":
			_, _ = w.Write([]byte(`{"periods":[{"account_id":"claude-personal","account_label":"Claude","provider":"anthropic","window_id":"weekly_all","period_start":"2026-11-16T03:00:00Z","period_end":"2026-11-23T03:00:00Z","captured_percent":100,"forfeited_percent":0}]}`))
		case "/spend":
			_, _ = w.Write([]byte(`{"spend":[{"bucket":"2026-11-23","group_key":"anthropic","provider":"anthropic","cost_kind":"api_billed","known_cost_usd":12.25,"events":15}]}`))
		case "/insights":
			_, _ = w.Write([]byte(`{"recommendations":[{"id":"live_rec_1","kind":"forfeit_soon","severity":"warning","title":"Live recommendation","summary":"From live /v1/insights","account_id":"agy-personal"}]}`))
		case "/resets":
			_, _ = w.Write([]byte(`{"calendar":[{"id":"live_cal_1","kind":"window_reset","account_id":"claude-personal","account_label":"Claude","provider":"anthropic","at":"2026-11-30T03:00:00Z","summary":"Weekly reset"}]}`))
		}
	default:
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not_found"}`))
	}
}

func startUsageTest(t *testing.T, f *fakeStation) (*moduletest.Host, *mod) {
	t.Helper()
	keyFile := filepath.Join(t.TempDir(), "surface-usage-module.key")
	if err := os.WriteFile(keyFile, []byte(testBearerCanary+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := &mod{
		now: func() time.Time {
			tm, _ := time.Parse(time.RFC3339, "2026-11-23T03:55:00Z")
			return tm
		},
	}
	h := moduletest.Start(t, m)
	ctx := context.Background()
	_ = h.Settings.Set(ctx, "url", f.srv.URL)
	_ = h.Settings.Set(ctx, "key_file", keyFile)
	return h, m
}

func TestNowEnforcesUnknownNullParentBlockedCostKindSeparationAndErrorSeparation(t *testing.T) {
	f := newFakeStation(t, false)
	h, _ := startUsageTest(t, f)

	code, body := h.Do("GET", "/now?refresh=1", "")
	if code != http.StatusOK {
		t.Fatalf("GET /now: %d %s", code, body)
	}
	if strings.Contains(body, testBearerCanary) {
		t.Fatal("bearer key leaked in /now response")
	}

	var payload UsageNowPayload
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Live || len(payload.Accounts) != 2 || len(payload.UseItOrLoseIt) != 1 {
		t.Fatalf("unexpected /now payload: %+v", payload)
	}

	// 1. Verify parent-blocked shading on claude-personal: weekly_all is 100%, so weekly_scoped:fable
	// must be shaded parent_blocked = true with blocked_by_window = "weekly_all".
	claude := payload.Accounts[0]
	if claude.ID != "claude-personal" || len(claude.Windows) != 2 {
		t.Fatalf("unexpected claude account: %+v", claude)
	}
	fableWin := claude.Windows[1]
	if fableWin.ID != "weekly_scoped:fable" || !fableWin.ParentBlocked || fableWin.BlockedByWindow != "weekly_all" {
		t.Fatalf("expected weekly_scoped:fable to have parent_blocked=true and blocked_by_window=weekly_all, got %+v", fableWin)
	}
	if !fableWin.PartialReset {
		t.Errorf("expected weekly_scoped:fable partial_reset=true from /v1/capacity, got %+v", fableWin)
	}

	// 2. Verify cost_kind buckets on claude-personal are strictly separated and never summed together,
	// and unpriced cost_kind has KnownCostUSD == nil (JSON null, never 0).
	if len(claude.SpendByCostKind) != 3 {
		t.Fatalf("expected 3 separate cost_kind buckets, got %+v", claude.SpendByCostKind)
	}
	byKind := map[string]SpendByCostKind{}
	for _, sp := range claude.SpendByCostKind {
		byKind[sp.CostKind] = sp
	}
	if byKind["subscription_equivalent"].KnownCostUSD == nil || *byKind["subscription_equivalent"].KnownCostUSD != 45.50 {
		t.Errorf("unexpected subscription_equivalent cost: %+v", byKind["subscription_equivalent"])
	}
	if byKind["api_billed"].KnownCostUSD == nil || *byKind["api_billed"].KnownCostUSD != 12.25 {
		t.Errorf("unexpected api_billed cost: %+v", byKind["api_billed"])
	}
	if byKind["unclassified"].KnownCostUSD != nil {
		t.Errorf("expected unclassified unpriced KnownCostUSD to be nil (JSON null, not 0), got %v", *byKind["unclassified"].KnownCostUSD)
	}

	// 3. Verify gateway-openrouter balance window keeps Used as nil (JSON null, never 0),
	// and keeps latest_error separate from stale.
	orAcc := payload.Accounts[1]
	if orAcc.ID != "gateway-openrouter" || !orAcc.Stale || orAcc.LatestError != "upstream timeout reading key_limit" {
		t.Fatalf("expected gateway-openrouter stale=true and latest_error kept separate, got %+v", orAcc)
	}
	if len(orAcc.Windows) != 1 || orAcc.Windows[0].Used != nil {
		t.Fatalf("expected account_balance used to be nil (JSON null, never 0), got %+v", orAcc.Windows)
	}
	if !orAcc.LowBalance || orAcc.RunwayDays == nil {
		t.Errorf("expected low_balance=true and runway_days computed on gateway-openrouter ($4.95/$245), got %+v", orAcc)
	}
}

func TestTrendsAndIntelligenceFixtureFallbackAndLivePromotion(t *testing.T) {
	// Phase 1: Station returns 404 / route_not_mapped for /series, /periods, /spend, /insights, /resets.
	f := newFakeStation(t, false)
	h, _ := startUsageTest(t, f)

	code, body := h.Do("GET", "/trends", "")
	if code != http.StatusOK {
		t.Fatalf("GET /trends: %d %s", code, body)
	}
	var trends UsageTrendsPayload
	if err := json.Unmarshal([]byte(body), &trends); err != nil {
		t.Fatal(err)
	}
	if trends.SeriesSource != "fixture" || trends.PeriodsSource != "fixture" || trends.SpendSource != "fixture" {
		t.Fatalf("expected fixture provenance when Station returns route_not_mapped, got %+v", trends)
	}
	if len(trends.Series) == 0 || len(trends.Periods) == 0 || len(trends.Spend) == 0 {
		t.Fatalf("expected non-empty contract fixtures for series, periods, and spend, got %+v", trends)
	}

	code, body = h.Do("GET", "/intelligence", "")
	if code != http.StatusOK {
		t.Fatalf("GET /intelligence: %d %s", code, body)
	}
	var intel UsageIntelligencePayload
	if err := json.Unmarshal([]byte(body), &intel); err != nil {
		t.Fatal(err)
	}
	if intel.InsightsSource != "fixture" || intel.ResetsSource != "fixture" {
		t.Fatalf("expected fixture provenance for insights/resets, got %+v", intel)
	}
	if len(intel.Recommendations) == 0 || len(intel.Forecasts) == 0 || len(intel.Anomalies) == 0 || len(intel.SubscriptionValue) == 0 || len(intel.ResetsCalendar) == 0 {
		t.Fatalf("expected populated intelligence sections, got %+v", intel)
	}

	// Phase 2: Station maps /series, /periods, /spend, /insights, /resets -> switches to "live" seamlessly.
	f.mapTrendsAndInsights = true
	code, body = h.Do("GET", "/trends?refresh=1", "")
	if code != http.StatusOK {
		t.Fatalf("GET /trends live: %d %s", code, body)
	}
	if err := json.Unmarshal([]byte(body), &trends); err != nil {
		t.Fatal(err)
	}
	if trends.SeriesSource != "live" || trends.PeriodsSource != "live" || trends.SpendSource != "live" {
		t.Fatalf("expected live provenance after Station maps routes, got %+v", trends)
	}

	code, body = h.Do("GET", "/intelligence?refresh=1", "")
	if code != http.StatusOK {
		t.Fatalf("GET /intelligence live: %d %s", code, body)
	}
	if err := json.Unmarshal([]byte(body), &intel); err != nil {
		t.Fatal(err)
	}
	if intel.InsightsSource != "live" || intel.ResetsSource != "live" {
		t.Fatalf("expected live provenance for insights/resets, got %+v", intel)
	}
}

func TestSourcesRawProxyAndKeyIsolationOnFailure(t *testing.T) {
	f := newFakeStation(t, false)
	h, _ := startUsageTest(t, f)

	code, body := h.Do("GET", "/sources/claude-oauth/raw", "")
	if code != http.StatusOK || !strings.Contains(body, `"raw_capture":true`) {
		t.Fatalf("GET /sources/claude-oauth/raw: %d %s", code, body)
	}

	// Verify agent tools and Cite RefKind work and never leak the bearer key.
	out, err := h.Tool("usage_now", `{"refresh":true}`)
	if err != nil {
		t.Fatalf("usage_now tool: %v", err)
	}
	rawTool, _ := json.Marshal(out)
	if strings.Contains(string(rawTool), testBearerCanary) {
		t.Fatal("bearer key leaked in usage_now tool output")
	}

	for _, ref := range h.Manifest().Refs {
		if ref.Kind == "usage.account" {
			resolved, err := ref.Resolve(context.Background(), h, []string{"claude-personal"})
			if err != nil || resolved["claude-personal"].Title != "Claude" {
				t.Fatalf("resolve usage.account: %+v err=%v", resolved, err)
			}
		}
	}
}
