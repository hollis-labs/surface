package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/hollis-labs/surface/module/tetherlink"
)

// Usage is one card per thing we pay for or are rate-limited on: usage against a limit,
// when the limit resets, and a status. Sources are read-only; a source not collected yet is
// a card that says so, never a zero.

// Usage statuses. gap means no collector reads this source yet; unknown means a collector
// ran but the source did not report enough to judge.
const (
	usageOK        = "ok"
	usageWarning   = "warning"
	usageExhausted = "exhausted"
	usageUnknown   = "unknown"
	usageError     = "error"
	usageGap       = "gap"
)

const gatewaySource = "llm-gateway"

type UsageCard struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Kind   string `json:"kind"` // gateway or subscription
	Status string `json:"status"`
	// Used and Limit share Unit; either is nil when the source does not report it.
	Used     *float64 `json:"used"`
	Limit    *float64 `json:"limit"`
	Unit     string   `json:"unit,omitempty"`
	Window   string   `json:"window,omitempty"`
	ResetAt  string   `json:"reset_at,omitempty"`
	Summary  string   `json:"summary,omitempty"`
	Gap      string   `json:"gap,omitempty"`
	Observed string   `json:"observed_at,omitempty"`
}

// UsageTotals is one aggregate row from the gateway: a provider, a model or a whole window.
type UsageTotals struct {
	Key             string `json:"key"`
	Requests        int64  `json:"requests"`
	Successes       int64  `json:"successes"`
	Errors          int64  `json:"errors"`
	InputTokens     int64  `json:"input_tokens"`
	OutputTokens    int64  `json:"output_tokens"`
	ReasoningTokens int64  `json:"reasoning_tokens"`
	CacheReadTokens int64  `json:"cache_read_tokens"`
}

type UsageWindow struct {
	Label string      `json:"label"`
	Since string      `json:"since"`
	Total UsageTotals `json:"total"`
}

type UsageBudget struct {
	Provider    string   `json:"provider"`
	Model       string   `json:"model,omitempty"`
	Level       string   `json:"level,omitempty"`
	Scope       string   `json:"scope,omitempty"`
	Window      string   `json:"window,omitempty"`
	WindowStart string   `json:"window_start,omitempty"`
	ResetAt     string   `json:"reset_at,omitempty"`
	MaxCostUSD  *float64 `json:"max_cost_usd,omitempty"`
	MaxRequests *float64 `json:"max_requests,omitempty"`
	MaxTokens   *float64 `json:"max_tokens,omitempty"`
	Exhausted   bool     `json:"exhausted"`
	Error       string   `json:"error,omitempty"`
}

type UsageAlert struct {
	At        string `json:"at"`
	Provider  string `json:"provider,omitempty"`
	Model     string `json:"model,omitempty"`
	Operation string `json:"operation,omitempty"`
	Error     string `json:"error,omitempty"`
}

// UsageDetail is a card's details. Only the gateway has them so far.
type UsageDetail struct {
	Windows    []UsageWindow `json:"windows"`
	ByProvider []UsageTotals `json:"by_provider"`
	ByModel    []UsageTotals `json:"by_model"`
	Budgets    []UsageBudget `json:"budgets"`
	Alerts     []UsageAlert  `json:"alerts"`
	Warnings   []string      `json:"warnings,omitempty"`
}

type UsageResult struct {
	Cards     []UsageCard  `json:"cards"`
	Source    string       `json:"source,omitempty"`
	Detail    *UsageDetail `json:"detail,omitempty"`
	FetchedAt string       `json:"fetched_at"`
}

// subscriptions are the seats we pay for per account. No collector reads them yet; they
// stay visible as gaps so a missing number is never mistaken for zero use.
var subscriptions = []struct{ id, title string }{
	{"claude", "Claude (Claude Code)"},
	{"codex", "Codex"},
	{"agy", "agy"},
	{"opencode", "opencode"},
}

const (
	usageTTL       = time.Minute
	usageTopN      = 10
	usageAlertsMax = 20
)

type gatewayRead struct {
	card   UsageCard
	detail UsageDetail
	err    error
	at     time.Time
}

// usageCache serializes and caches gateway reads, failures included, so open windows and
// the tool share one bounded refresh.
type usageCache struct {
	mu      sync.Mutex
	gateway gatewayRead
	now     func() time.Time
}

func (c *usageCache) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

func (c *usageCache) read(ctx context.Context, link func() (tetherlink.Caller, error), source string) (UsageResult, error) {
	if source != "" && source != gatewaySource && !isSubscription(source) {
		return UsageResult{}, fmt.Errorf("unknown usage source %q", source)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.clock()
	if c.gateway.at.IsZero() || now.Sub(c.gateway.at) >= usageTTL {
		var g gatewayRead
		if l, err := link(); err != nil {
			g.err = err
		} else {
			g.card, g.detail, g.err = readGateway(ctx, l, now)
		}
		g.at = now
		c.gateway = g
	}
	g := c.gateway
	card := g.card
	if g.err != nil {
		card = UsageCard{ID: gatewaySource, Title: "LLM gateway", Kind: "gateway", Status: usageError,
			Gap: "Gateway usage unavailable: " + g.err.Error(), Observed: g.at.UTC().Format(time.RFC3339)}
	}
	out := UsageResult{Cards: []UsageCard{card}, Source: source, FetchedAt: g.at.UTC().Format(time.RFC3339)}
	for _, s := range subscriptions {
		out.Cards = append(out.Cards, UsageCard{ID: s.id, Title: s.title, Kind: "subscription", Status: usageGap,
			Gap: "Not collected yet. A read-only collector for this subscription's CLI is pending."})
	}
	if source == gatewaySource && g.err == nil {
		d := g.detail
		out.Detail = &d
	}
	return out, nil
}

func isSubscription(id string) bool {
	for _, s := range subscriptions {
		if s.id == id {
			return true
		}
	}
	return false
}

// readGateway makes five bounded reads: budgets, usage for three windows, recent alerts.
func readGateway(ctx context.Context, link tetherlink.Caller, now time.Time) (UsageCard, UsageDetail, error) {
	now = now.UTC()
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	detail := UsageDetail{Windows: []UsageWindow{}, ByProvider: []UsageTotals{}, ByModel: []UsageTotals{}, Budgets: []UsageBudget{}, Alerts: []UsageAlert{}}
	windows := []struct {
		label string
		since time.Time
	}{{"Today (UTC)", day}, {"Last 7 days", day.AddDate(0, 0, -6)}, {"Last 30 days", day.AddDate(0, 0, -29)}}
	var month usageSummary
	for i, w := range windows {
		s, err := readGatewayUsage(ctx, link, w.since)
		if err != nil {
			return UsageCard{}, detail, err
		}
		s.Total.Key = w.label
		detail.Windows = append(detail.Windows, UsageWindow{Label: w.label, Since: w.since.Format(time.RFC3339), Total: s.Total})
		if i == len(windows)-1 {
			month = s
		}
	}
	detail.ByProvider = top(month.ByProvider)
	detail.ByModel = top(month.ByModel)

	budgets, err := readGatewayBudgets(ctx, link)
	if err != nil {
		detail.Warnings = append(detail.Warnings, "Budgets unavailable: "+err.Error())
	} else {
		detail.Budgets = budgets
	}
	alerts, err := readGatewayAlerts(ctx, link, day.AddDate(0, 0, -6))
	if err != nil {
		detail.Warnings = append(detail.Warnings, "Budget alerts unavailable: "+err.Error())
	} else {
		detail.Alerts = alerts
	}

	today := detail.Windows[0].Total
	used := float64(today.Requests)
	card := UsageCard{ID: gatewaySource, Title: "LLM gateway", Kind: "gateway", Status: usageOK, Used: &used, Unit: "requests",
		Window: "day (UTC)", ResetAt: day.AddDate(0, 0, 1).Format(time.RFC3339), Observed: now.Format(time.RFC3339),
		Summary: fmt.Sprintf("%d requests today, %d errors, %d in / %d out tokens. The gateway reports no cost, so spend against budgets is unknown.",
			today.Requests, today.Errors, today.InputTokens, today.OutputTokens)}
	if today.Errors > 0 && today.Successes == 0 {
		card.Status = usageWarning
	}
	for _, b := range detail.Budgets {
		switch {
		case b.Exhausted:
			card.Status = usageExhausted
			card.ResetAt = b.ResetAt
		case b.Error != "" && card.Status == usageOK:
			card.Status = usageWarning
		}
	}
	if len(detail.Budgets) == 0 {
		card.Gap = "No gateway budgets are configured, so there is no limit to measure against."
	} else if card.Status == usageWarning {
		card.Gap = "A budget reports an error; see details."
	}
	return card, detail, nil
}

type usageSummary struct {
	Total      UsageTotals
	ByProvider []UsageTotals
	ByModel    []UsageTotals
}

func readGatewayUsage(ctx context.Context, link tetherlink.Caller, since time.Time) (usageSummary, error) {
	raw, err := tetherlink.CallAll(ctx, link, "tether_ai_usage", map[string]any{"since": since.Format(time.RFC3339)})
	if err != nil {
		return usageSummary{}, err
	}
	var data struct {
		OK      bool `json:"ok"`
		Summary *struct {
			UsageTotals
			ByProvider []UsageTotals `json:"by_provider"`
			ByModel    []UsageTotals `json:"by_model"`
		} `json:"summary"`
	}
	if json.Unmarshal([]byte(raw), &data) != nil || data.Summary == nil {
		return usageSummary{}, fmt.Errorf("gateway usage returned an unreadable result")
	}
	return usageSummary{Total: data.Summary.UsageTotals, ByProvider: data.Summary.ByProvider, ByModel: data.Summary.ByModel}, nil
}

func readGatewayBudgets(ctx context.Context, link tetherlink.Caller) ([]UsageBudget, error) {
	raw, err := tetherlink.CallAll(ctx, link, "tether_ai_budgets", map[string]any{})
	if err != nil {
		return nil, err
	}
	var data struct {
		Budgets []struct {
			Provider    string `json:"provider"`
			Model       string `json:"model"`
			Exhausted   bool   `json:"exhausted"`
			Error       string `json:"error"`
			WindowStart string `json:"window_start"`
			Budget      struct {
				Level       string   `json:"level"`
				Scope       string   `json:"scope"`
				Window      string   `json:"window"`
				MaxCostUSD  *float64 `json:"max_cost_usd"`
				MaxRequests *float64 `json:"max_requests"`
				MaxTokens   *float64 `json:"max_tokens"`
			} `json:"usage_budget"`
		} `json:"budgets"`
	}
	if json.Unmarshal([]byte(raw), &data) != nil {
		return nil, fmt.Errorf("unreadable budgets")
	}
	out := []UsageBudget{}
	for i, b := range data.Budgets {
		if i >= 50 {
			break
		}
		out = append(out, UsageBudget{Provider: b.Provider, Model: b.Model, Level: b.Budget.Level, Scope: b.Budget.Scope,
			Window: b.Budget.Window, WindowStart: b.WindowStart, ResetAt: budgetReset(b.WindowStart, b.Budget.Window),
			MaxCostUSD: b.Budget.MaxCostUSD, MaxRequests: b.Budget.MaxRequests, MaxTokens: b.Budget.MaxTokens,
			Exhausted: b.Exhausted, Error: b.Error})
	}
	return out, nil
}

// budgetReset is the window start plus the window; empty when either is unknown.
func budgetReset(start, window string) string {
	t, err := time.Parse(time.RFC3339, start)
	if err != nil {
		return ""
	}
	switch window {
	case "hour":
		t = t.Add(time.Hour)
	case "day":
		t = t.AddDate(0, 0, 1)
	case "week":
		t = t.AddDate(0, 0, 7)
	case "month":
		t = t.AddDate(0, 1, 0)
	default:
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func readGatewayAlerts(ctx context.Context, link tetherlink.Caller, since time.Time) ([]UsageAlert, error) {
	raw, err := tetherlink.CallAll(ctx, link, "tether_ai_budget_alerts", map[string]any{"since": since.Format(time.RFC3339), "limit": usageAlertsMax})
	if err != nil {
		return nil, err
	}
	var data struct {
		Alerts []struct {
			Timestamp string `json:"timestamp"`
			Provider  string `json:"provider"`
			Model     string `json:"model"`
			Operation string `json:"operation"`
			Error     string `json:"error"`
		} `json:"alerts"`
	}
	if json.Unmarshal([]byte(raw), &data) != nil {
		return nil, fmt.Errorf("unreadable budget alerts")
	}
	out := []UsageAlert{}
	for i, a := range data.Alerts {
		if i >= usageAlertsMax {
			break
		}
		out = append(out, UsageAlert{At: a.Timestamp, Provider: a.Provider, Model: a.Model, Operation: a.Operation, Error: a.Error})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At > out[j].At })
	return out, nil
}

// top is the largest consumers by requests; rows with no key (unrouted failures) are named.
func top(rows []UsageTotals) []UsageTotals {
	out := make([]UsageTotals, 0, len(rows))
	for _, r := range rows {
		if r.Key == "" {
			r.Key = "(unrouted)"
		}
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Requests > out[j].Requests })
	if len(out) > usageTopN {
		out = out[:usageTopN]
	}
	return out
}

func (m *mod) usage(ctx context.Context, source string) (UsageResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	return m.liveUsage.read(ctx, m.tether, source)
}
