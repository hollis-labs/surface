// Package usage is the Usage Monitor module: a four-view operational cockpit (Now, Trends,
// Intelligence, Sources) over the Station Usage Monitor v1 API, enforcing strict separation
// of cost_kind buckets, unknown values as nil (never 0), parent-blocked quota window shading,
// and server-side key isolation.
package usage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/hollis-labs/surface/module"
)

func init() { module.Register(&mod{}) }

const (
	viewID   = "usage"
	citeKind = "usage.account"
)

var accountIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{1,63}$`)

type mod struct {
	module.Base
	h   module.Host
	now func() time.Time

	mu       sync.Mutex
	cachedAt time.Time
	cached   UsageNowPayload
}

func (m *mod) clock() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}

func (m *mod) Manifest() module.Manifest {
	return module.Manifest{
		ID:      "usage",
		Title:   "Usage Monitor",
		Version: "0.1.0",
		About:   "Subscription capacity, use-it-or-lose-it forfeiture prevention, reset countdowns, cost_kind spend trends, and usage intelligence from Station Usage Monitor v1.",
		Views: []module.View{{
			ID: viewID, Title: "Usage Monitor", Icon: "gauge", Width: 1220, Height: 780,
			Menu: true, Bare: true,
			Render:   module.Component("usage/main"),
			Describe: m.describeWindow,
		}},
		Commands: []module.Command{{
			ID:       "usage.open",
			Title:    "Usage Monitor: open",
			Section:  "Usage",
			Keywords: "usage monitor quota resets trends intelligence spend subscriptions capacity",
			Run: func(ctx context.Context, h module.Host, args map[string]any) (any, error) {
				return h.Desk().Open(ctx, viewID, module.OpenOptions{Args: args})
			},
		}},
		Settings: []module.Setting{
			{
				Key:     "url",
				Title:   "Usage monitor URL",
				About:   "Station Usage Monitor v1 API base URL (https, or http on loopback).",
				Type:    module.SettingString,
				Default: defaultURL,
				Owner:   true,
			},
			{
				Key:     "key_file",
				Title:   "Usage monitor key file",
				About:   "Bearer key file (scope usage-monitor.read). Never sent to the browser.",
				Type:    module.SettingString,
				Default: defaultKeyFile,
				Owner:   true,
			},
		},
		Tools: m.tools(),
		Refs:  m.refs(),
		Slots: []module.Slot{{
			Name:  "usage",
			About: "subscription quota windows, use-it-or-lose-it expiring capacity, reset countdowns, gateway spend separated by cost_kind, and usage recommendations",
			Views: []string{viewID},
			Tools: []string{"usage_now", "usage_trends", "usage_intelligence", "usage_sources"},
			Where: "Station Usage Monitor v1 (/accounts, /sources, /capacity, /summary, /series, /periods, /spend, /insights, /resets)",
			Instructions: "Never sum different cost_kind values (api_billed, api_estimated, subscription_equivalent, local_compute). " +
				"Report nil/null values as Unknown, never 0. Prioritize expiring subscription capacity in use_it_or_lose_it before it resets.",
			Knowledge: m.knowledge,
		}},
	}
}

func (m *mod) Start(_ context.Context, h module.Host) error {
	m.h = h
	m.routes(h)
	return nil
}

func (m *mod) client() (*stationClient, error) {
	if m.h == nil {
		return nil, errNotConfigured
	}
	cfg := m.h.Config()
	return newStationClient(cfg.String("url"), cfg.String("key_file"))
}

func (m *mod) loadNow(ctx context.Context, refresh bool) (UsageNowPayload, error) {
	now := m.clock()
	m.mu.Lock()
	if !refresh && !m.cachedAt.IsZero() && now.Sub(m.cachedAt) < cacheTTL && len(m.cached.Accounts) > 0 {
		out := m.cached
		m.mu.Unlock()
		return out, nil
	}
	m.mu.Unlock()

	cl, err := m.client()
	if err != nil {
		return UsageNowPayload{
			FetchedAt:     now.UTC().Format(time.RFC3339),
			Live:          false,
			Error:         err.Error(),
			UseItOrLoseIt: []UseItOrLoseItEntry{},
			NextResets:    []NextResetItem{},
			Accounts:      []UsageAccount{},
			Sources:       []UsageSourceItem{},
		}, err
	}

	payload, err := cl.fetchNow(ctx, now)
	if err != nil {
		return UsageNowPayload{
			FetchedAt:     now.UTC().Format(time.RFC3339),
			Live:          false,
			Error:         err.Error(),
			UseItOrLoseIt: []UseItOrLoseItEntry{},
			NextResets:    []NextResetItem{},
			Accounts:      []UsageAccount{},
			Sources:       []UsageSourceItem{},
		}, err
	}

	m.mu.Lock()
	m.cached = payload
	m.cachedAt = now
	m.mu.Unlock()
	return payload, nil
}

func (m *mod) loadTrends(ctx context.Context, refresh bool, accountFilter, windowID, step, groupBy, costKind string) (UsageTrendsPayload, error) {
	now := m.clock()
	nowPayload, _ := m.loadNow(ctx, refresh)
	cl, err := m.client()

	out := UsageTrendsPayload{
		FetchedAt:     now.UTC().Format(time.RFC3339),
		SeriesSource:  "fixture",
		PeriodsSource: "fixture",
		SpendSource:   "fixture",
	}

	if err == nil {
		q := url.Values{}
		if accountFilter != "" && accountFilter != "all" {
			q.Set("account", accountFilter)
		}
		if windowID != "" {
			q.Set("window", windowID)
		}
		if step != "" {
			q.Set("step", step)
		}
		if groupBy != "" {
			q.Set("group_by", groupBy)
		}
		if costKind != "" {
			q.Set("cost_kind", costKind)
		}
		qs := ""
		if len(q) > 0 {
			qs = "?" + q.Encode()
		}

		var liveSeries struct {
			Series []WindowSeries `json:"series"`
		}
		if serr := cl.getJSON(ctx, "/series"+qs, &liveSeries); serr == nil && liveSeries.Series != nil {
			out.Series = liveSeries.Series
			out.SeriesSource = "live"
		}

		var livePeriods struct {
			Periods []CompletedPeriodRow `json:"periods"`
		}
		if perr := cl.getJSON(ctx, "/periods"+qs, &livePeriods); perr == nil && livePeriods.Periods != nil {
			out.Periods = livePeriods.Periods
			out.PeriodsSource = "live"
		}

		var liveSpend struct {
			Spend []SpendGroupRow `json:"spend"`
		}
		if sperr := cl.getJSON(ctx, "/spend"+qs, &liveSpend); sperr == nil && liveSpend.Spend != nil {
			out.Spend = liveSpend.Spend
			out.SpendSource = "live"
		}
	}

	if out.SeriesSource == "fixture" {
		out.Series = buildFixtureSeries(now, nowPayload.Accounts, accountFilter, step)
	}
	if out.PeriodsSource == "fixture" {
		out.Periods = buildFixturePeriods(now, accountFilter)
	}
	if out.SpendSource == "fixture" {
		out.Spend = buildFixtureSpend(now, nowPayload.Accounts, costKind)
	}
	return out, nil
}

func (m *mod) loadIntelligence(ctx context.Context, refresh bool) (UsageIntelligencePayload, error) {
	now := m.clock()
	nowPayload, _ := m.loadNow(ctx, refresh)
	out := buildIntelligence(now, nowPayload)

	cl, err := m.client()
	if err != nil {
		return out, nil
	}

	var liveInsights struct {
		Recommendations   []InsightRecommendation `json:"recommendations"`
		Forecasts         []WindowForecast        `json:"forecasts"`
		Anomalies         []SpendAnomaly          `json:"anomalies"`
		SubscriptionValue []SubscriptionValueRow  `json:"subscription_value"`
	}
	if ierr := cl.getJSON(ctx, "/insights", &liveInsights); ierr == nil {
		out.InsightsSource = "live"
		if len(liveInsights.Recommendations) > 0 {
			// Merge live recommendations with any live capacity/account signals without duplicates.
			seen := map[string]bool{}
			var merged []InsightRecommendation
			for _, r := range liveInsights.Recommendations {
				k := r.DedupeKey
				if k == "" {
					k = r.ID
				}
				seen[k] = true
				merged = append(merged, r)
			}
			for _, r := range out.Recommendations {
				k := r.DedupeKey
				if k == "" {
					k = r.ID
				}
				if !seen[k] {
					merged = append(merged, r)
				}
			}
			out.Recommendations = merged
		}
		if len(liveInsights.Forecasts) > 0 {
			out.Forecasts = liveInsights.Forecasts
		}
		if len(liveInsights.Anomalies) > 0 {
			out.Anomalies = liveInsights.Anomalies
		}
		if len(liveInsights.SubscriptionValue) > 0 {
			out.SubscriptionValue = liveInsights.SubscriptionValue
		}
	}

	var liveResets struct {
		Resets   []ResetCalendarEvent `json:"resets"`
		Calendar []ResetCalendarEvent `json:"calendar"`
	}
	if rerr := cl.getJSON(ctx, "/resets", &liveResets); rerr == nil {
		out.ResetsSource = "live"
		if len(liveResets.Calendar) > 0 {
			out.ResetsCalendar = liveResets.Calendar
		} else if len(liveResets.Resets) > 0 {
			out.ResetsCalendar = liveResets.Resets
		}
	}

	return out, nil
}

func (m *mod) routes(h module.Host) {
	h.Handle("GET /now", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refresh := r.URL.Query().Get("refresh") == "1" || r.URL.Query().Get("refresh") == "true"
		payload, _ := m.loadNow(r.Context(), refresh)
		writeJSON(w, http.StatusOK, payload)
	}))

	h.Handle("GET /trends", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		refresh := q.Get("refresh") == "1" || q.Get("refresh") == "true"
		payload, _ := m.loadTrends(
			r.Context(),
			refresh,
			q.Get("account"),
			q.Get("window"),
			q.Get("step"),
			q.Get("group_by"),
			q.Get("cost_kind"),
		)
		writeJSON(w, http.StatusOK, payload)
	}))

	h.Handle("GET /intelligence", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refresh := r.URL.Query().Get("refresh") == "1" || r.URL.Query().Get("refresh") == "true"
		payload, _ := m.loadIntelligence(r.Context(), refresh)
		writeJSON(w, http.StatusOK, payload)
	}))

	h.Handle("GET /sources", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refresh := r.URL.Query().Get("refresh") == "1" || r.URL.Query().Get("refresh") == "true"
		nowPayload, _ := m.loadNow(r.Context(), refresh)
		writeJSON(w, http.StatusOK, UsageSourcesPayload{
			FetchedAt: nowPayload.FetchedAt,
			Live:      nowPayload.Live,
			Sources:   nowPayload.Sources,
			Accounts:  nowPayload.Accounts,
		})
	}))

	h.Handle("GET /sources/{id}/raw", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sourceID := strings.TrimSpace(r.PathValue("id"))
		if sourceID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing source id"})
			return
		}
		cl, err := m.client()
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
			return
		}
		raw, err := cl.getRaw(r.Context(), "/sources/"+url.PathEscape(sourceID)+"/raw")
		if err != nil {
			status := http.StatusBadGateway
			if errors.Is(err, errRouteNotMapped) {
				status = http.StatusNotFound
			}
			writeJSON(w, status, map[string]string{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(raw)
	}))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (m *mod) describeWindow(_ context.Context, _ module.Host, w module.Window) (module.WindowContext, error) {
	tab, _ := w.Args["tab"].(string)
	if tab == "" {
		tab = "now"
	}
	return module.WindowContext{
		Kind:  "data",
		Ref:   "usage",
		Extra: map[string]any{"tab": tab},
	}, nil
}

func (m *mod) knowledge(ctx context.Context, _ module.Host, _ module.Situation) (string, error) {
	p, err := m.loadNow(ctx, false)
	if err != nil && len(p.Accounts) == 0 {
		return "Usage Monitor: " + err.Error(), nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Usage Monitor (%d accounts, %d expiring windows in use_it_or_lose_it):\n", len(p.Accounts), len(p.UseItOrLoseIt))
	for i, u := range p.UseItOrLoseIt {
		if i >= 5 {
			break
		}
		pct := "Unknown"
		if u.ForfeitablePercent != nil {
			pct = fmt.Sprintf("%.1f%%", *u.ForfeitablePercent)
		}
		fmt.Fprintf(&b, "- #%d %s (%s / %s): %s unused, resets at %s\n", u.Rank, u.AccountLabel, u.ModelGroup, u.WindowID, pct, u.ResetsAt)
	}
	return strings.TrimSpace(b.String()), nil
}

func (m *mod) refs() []module.RefKind {
	return []module.RefKind{{
		Kind: citeKind, Label: "Usage account", ID: accountIDRe,
		Resolve: func(ctx context.Context, _ module.Host, ids []string) (map[string]module.RefEntity, error) {
			p, _ := m.loadNow(ctx, false)
			byID := map[string]UsageAccount{}
			for _, a := range p.Accounts {
				byID[a.ID] = a
			}
			out := map[string]module.RefEntity{}
			for _, id := range ids {
				if a, ok := byID[id]; ok {
					out[id] = m.accountEntity(a, false)
				}
			}
			return out, nil
		},
		Record: func(ctx context.Context, _ module.Host, id string) (module.RefEntity, error) {
			p, _ := m.loadNow(ctx, false)
			for _, a := range p.Accounts {
				if a.ID == id {
					return m.accountEntity(a, true), nil
				}
			}
			return module.RefEntity{}, module.ErrRefNotFound
		},
	}}
}

func (m *mod) accountEntity(a UsageAccount, full bool) module.RefEntity {
	e := module.RefEntity{
		Title:       a.Label,
		Subtitle:    fmt.Sprintf("Usage · %s · %s", a.Provider, a.Kind),
		Description: fmt.Sprintf("%d quota/balance windows · health: %s", len(a.Windows), a.Health),
		UpdatedAt:   a.ObservedAt,
		ChipLabel:   "Usage: " + a.Label,
		Status:      &module.RefStatus{Label: a.Health, Tone: module.StatusTone(a.Health)},
		Open:        &module.RefOpen{Command: "usage.open", Args: map[string]any{"tab": "now", "account": a.ID}},
		Fields: []module.RefField{
			{Label: "Account", Value: a.ID, Mono: true},
			{Label: "Provider", Value: a.Provider},
			{Label: "Kind", Value: a.Kind},
		},
	}
	if a.Plan != "" {
		e.Fields = append(e.Fields, module.RefField{Label: "Plan", Value: a.Plan})
	}
	if full {
		raw, _ := json.MarshalIndent(a, "", "  ")
		e.Body = string(raw)
	}
	return e
}

func (m *mod) tools() []module.Tool {
	raw := func(s string) json.RawMessage { return json.RawMessage(s) }
	return []module.Tool{
		{
			Name:        "usage_now",
			Level:       module.LevelRead,
			Description: "Read live subscription capacity, ranked use-it-or-lose-it expiring windows, next reset countdowns, and all accounts from Station Usage Monitor.",
			Input:       raw(`{"type":"object","properties":{"refresh":{"type":"boolean"}}}`),
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				var a struct {
					Refresh bool `json:"refresh"`
				}
				_ = json.Unmarshal(in, &a)
				return m.loadNow(ctx, a.Refresh)
			},
		},
		{
			Name:        "usage_trends",
			Level:       module.LevelRead,
			Description: "Read per-window usage curves across periods, captured vs forfeited capacity by completed period, and spend separated strictly by cost_kind.",
			Input:       raw(`{"type":"object","properties":{"account":{"type":"string"},"window":{"type":"string"},"step":{"type":"string"},"group_by":{"type":"string","enum":["provider","model"]},"cost_kind":{"type":"string","enum":["api_billed","api_estimated","subscription_equivalent","local_compute","unclassified"]},"refresh":{"type":"boolean"}}}`),
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				var a struct {
					Account  string `json:"account"`
					Window   string `json:"window"`
					Step     string `json:"step"`
					GroupBy  string `json:"group_by"`
					CostKind string `json:"cost_kind"`
					Refresh  bool   `json:"refresh"`
				}
				_ = json.Unmarshal(in, &a)
				return m.loadTrends(ctx, a.Refresh, a.Account, a.Window, a.Step, a.GroupBy, a.CostKind)
			},
		},
		{
			Name:        "usage_intelligence",
			Level:       module.LevelRead,
			Description: "Read usage recommendations with evidence, window exhaustion forecasts, per-subscription value and plan fit, spend anomalies, and the reset calendar.",
			Input:       raw(`{"type":"object","properties":{"refresh":{"type":"boolean"}}}`),
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				var a struct {
					Refresh bool `json:"refresh"`
				}
				_ = json.Unmarshal(in, &a)
				return m.loadIntelligence(ctx, a.Refresh)
			},
		},
		{
			Name:        "usage_sources",
			Level:       module.LevelRead,
			Description: "List collector sources, their health, staleness, last observation timestamp, and latest collector error.",
			Input:       raw(`{"type":"object","properties":{"refresh":{"type":"boolean"}}}`),
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				var a struct {
					Refresh bool `json:"refresh"`
				}
				_ = json.Unmarshal(in, &a)
				p, err := m.loadNow(ctx, a.Refresh)
				return UsageSourcesPayload{
					FetchedAt: p.FetchedAt,
					Live:      p.Live,
					Sources:   p.Sources,
					Accounts:  p.Accounts,
				}, err
			},
		},
	}
}
