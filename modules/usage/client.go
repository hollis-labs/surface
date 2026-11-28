package usage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// No default Station URL: the owner sets usage.url. The key file defaults to the
	// conventional path under the home directory.
	defaultURL     = ""
	defaultKeyFile = "~/.config/station/usage/keys/surface-usage-module.key"
	fallbackKeyRel = ".config/station/usage/keys/surface-usage-module.key"
	cacheTTL       = 30 * time.Second
)

var (
	errNotConfigured  = errors.New("usage: set usage.url and usage.key_file in Settings")
	errRouteNotMapped = errors.New("usage: route not mapped on Station")
)

// ---- Shared API Response Contracts (matching modules/usage/web/types.ts) ----

// UsageWindowItem is one quota, balance, or spend window on an account.
type UsageWindowItem struct {
	ID              string   `json:"id"`
	Kind            string   `json:"kind,omitempty"`
	Group           string   `json:"group,omitempty"`
	Model           string   `json:"model,omitempty"`
	Unit            string   `json:"unit,omitempty"`
	Used            *float64 `json:"used"`
	Limit           *float64 `json:"limit"`
	Remaining       *float64 `json:"remaining"`
	WindowSeconds   *int64   `json:"window_seconds"`
	ResetsAt        string   `json:"resets_at,omitempty"`
	Rolling         bool     `json:"rolling,omitempty"`
	PartialReset    bool     `json:"partial_reset,omitempty"`
	ParentBlocked   bool     `json:"parent_blocked,omitempty"`
	BlockedByWindow string   `json:"blocked_by_window,omitempty"`
	Severity        string   `json:"severity,omitempty"`
}

// SpendByCostKind aggregates gateway spend for an account within a single cost_kind.
// Different cost_kind buckets are never summed together.
type SpendByCostKind struct {
	CostKind       string   `json:"cost_kind"`
	KnownCostUSD   *float64 `json:"known_cost_usd"`
	Events         int64    `json:"events"`
	UnpricedEvents int64    `json:"unpriced_events"`
	PartialEvents  int64    `json:"partial_events"`
	KnownTokens    int64    `json:"known_tokens"`
	Models         []string `json:"models,omitempty"`
}

// UsageAccount is one account in the Usage Monitor Now / Sources payloads.
type UsageAccount struct {
	ID              string            `json:"id"`
	Provider        string            `json:"provider"`
	Kind            string            `json:"kind"`
	Label           string            `json:"label"`
	Plan            string            `json:"plan,omitempty"`
	Enabled         bool              `json:"enabled"`
	Health          string            `json:"health"`
	Stale           bool              `json:"stale"`
	ObservedAt      string            `json:"observed_at,omitempty"`
	CollectedAt     string            `json:"collected_at,omitempty"`
	SourceID        string            `json:"source_id,omitempty"`
	Collector       string            `json:"collector,omitempty"`
	LatestError     string            `json:"latest_error,omitempty"`
	Attributes      map[string]any    `json:"attributes,omitempty"`
	Windows         []UsageWindowItem `json:"windows"`
	SpendByCostKind []SpendByCostKind `json:"spend_by_cost_kind,omitempty"`
	RunwayDays      *float64          `json:"runway_days"`
	LowBalance      bool              `json:"low_balance,omitempty"`
}

// UseItOrLoseItEntry is one ranked expiring subscription window from /v1/capacity.
type UseItOrLoseItEntry struct {
	Rank               int      `json:"rank"`
	Provider           string   `json:"provider"`
	AccountID          string   `json:"account_id"`
	AccountLabel       string   `json:"account_label"`
	BillingType        string   `json:"billing_type"`
	ModelGroup         string   `json:"model_group"`
	WindowID           string   `json:"window_id"`
	Unit               string   `json:"unit"`
	ForfeitablePercent *float64 `json:"forfeitable_percent"`
	ResetsAt           string   `json:"resets_at,omitempty"`
	SecondsToReset     *int64   `json:"seconds_to_reset"`
	ResetKind          string   `json:"reset_kind,omitempty"`
	UrgencyScore       *float64 `json:"urgency_score"`
	Modalities         []string `json:"modalities,omitempty"`
	GatewayModels      []string `json:"gateway_models,omitempty"`
}

// NextResetItem is an upcoming window reset in the countdown rail.
type NextResetItem struct {
	AccountID             string   `json:"account_id"`
	AccountLabel          string   `json:"account_label"`
	Provider              string   `json:"provider"`
	WindowID              string   `json:"window_id"`
	Model                 string   `json:"model,omitempty"`
	Group                 string   `json:"group,omitempty"`
	Unit                  string   `json:"unit,omitempty"`
	Used                  *float64 `json:"used"`
	Limit                 *float64 `json:"limit"`
	Remaining             *float64 `json:"remaining"`
	ResetsAt              string   `json:"resets_at"`
	SecondsToReset        *int64   `json:"seconds_to_reset"`
	PartialReset          bool     `json:"partial_reset,omitempty"`
	Rolling               bool     `json:"rolling,omitempty"`
	ParentBlocked         bool     `json:"parent_blocked,omitempty"`
	ResetCreditsAvailable bool     `json:"reset_credits_available,omitempty"`
}

// UsageSourceItem is one collector source in the Sources view.
type UsageSourceItem struct {
	ID           string         `json:"id"`
	AccountID    string         `json:"account_id"`
	AccountLabel string         `json:"account_label,omitempty"`
	Provider     string         `json:"provider,omitempty"`
	Collector    string         `json:"collector"`
	Health       string         `json:"health"`
	Stale        bool           `json:"stale"`
	ObservedAt   string         `json:"observed_at,omitempty"`
	CollectedAt  string         `json:"collected_at,omitempty"`
	LatestError  string         `json:"latest_error,omitempty"`
	WindowCount  *int           `json:"window_count"`
	Attributes   map[string]any `json:"attributes,omitempty"`
	RawURL       string         `json:"raw_url,omitempty"`
}

// UsageNowPayload is the response of GET /api/m/usage/now.
type UsageNowPayload struct {
	FetchedAt     string               `json:"fetched_at"`
	Live          bool                 `json:"live"`
	Error         string               `json:"error,omitempty"`
	UseItOrLoseIt []UseItOrLoseItEntry `json:"use_it_or_lose_it"`
	NextResets    []NextResetItem      `json:"next_resets"`
	Accounts      []UsageAccount       `json:"accounts"`
	Sources       []UsageSourceItem    `json:"sources"`
}

// UsageSourcesPayload is the response of GET /api/m/usage/sources.
type UsageSourcesPayload struct {
	FetchedAt string            `json:"fetched_at"`
	Live      bool              `json:"live"`
	Sources   []UsageSourceItem `json:"sources"`
	Accounts  []UsageAccount    `json:"accounts"`
}

// SeriesPoint is one time bucket in a window usage/balance series.
type SeriesPoint struct {
	At           string   `json:"at"`
	Used         *float64 `json:"used"`
	Remaining    *float64 `json:"remaining"`
	MaxUsed      *float64 `json:"max_used,omitempty"`
	MinRemaining *float64 `json:"min_remaining,omitempty"`
}

// WindowSeries is a per-window curve with current and previous period points.
type WindowSeries struct {
	AccountID      string        `json:"account_id"`
	AccountLabel   string        `json:"account_label"`
	Provider       string        `json:"provider"`
	WindowID       string        `json:"window_id"`
	Model          string        `json:"model,omitempty"`
	Group          string        `json:"group,omitempty"`
	Unit           string        `json:"unit"`
	Kind           string        `json:"kind"`
	Step           string        `json:"step"`
	CurrentPeriod  []SeriesPoint `json:"current_period"`
	PreviousPeriod []SeriesPoint `json:"previous_period,omitempty"`
}

// CompletedPeriodRow is a completed period's captured vs forfeited ledger row.
type CompletedPeriodRow struct {
	AccountID          string   `json:"account_id"`
	AccountLabel       string   `json:"account_label"`
	Provider           string   `json:"provider"`
	WindowID           string   `json:"window_id"`
	Model              string   `json:"model,omitempty"`
	Group              string   `json:"group,omitempty"`
	PeriodStart        string   `json:"period_start"`
	PeriodEnd          string   `json:"period_end"`
	PeakUsedPercent    *float64 `json:"peak_used_percent"`
	UsedAtResetPercent *float64 `json:"used_at_reset_percent"`
	CapturedPercent    *float64 `json:"captured_percent"`
	ForfeitedPercent   *float64 `json:"forfeited_percent"`
	ExhaustedAt        string   `json:"exhausted_at,omitempty"`
	HoursExhausted     *float64 `json:"hours_exhausted"`
	Rolling            bool     `json:"rolling,omitempty"`
	ParentBlocked      bool     `json:"parent_blocked,omitempty"`
}

// SpendGroupRow is a spend row strictly tagged with its cost_kind.
type SpendGroupRow struct {
	Bucket         string   `json:"bucket"`
	GroupKey       string   `json:"group_key"`
	Provider       string   `json:"provider,omitempty"`
	Model          string   `json:"model,omitempty"`
	AccountID      string   `json:"account_id,omitempty"`
	CostKind       string   `json:"cost_kind"`
	KnownCostUSD   *float64 `json:"known_cost_usd"`
	Events         int64    `json:"events"`
	UnpricedEvents int64    `json:"unpriced_events,omitempty"`
	KnownTokens    int64    `json:"known_tokens,omitempty"`
}

// UsageTrendsPayload is the response of GET /api/m/usage/trends.
type UsageTrendsPayload struct {
	FetchedAt     string               `json:"fetched_at"`
	SeriesSource  string               `json:"series_source"`
	PeriodsSource string               `json:"periods_source"`
	SpendSource   string               `json:"spend_source"`
	Series        []WindowSeries       `json:"series"`
	Periods       []CompletedPeriodRow `json:"periods"`
	Spend         []SpendGroupRow      `json:"spend"`
}

// InsightRecommendation is one actionable intelligence recommendation with numeric basis & evidence.
type InsightRecommendation struct {
	ID           string         `json:"id"`
	Kind         string         `json:"kind"`
	Severity     string         `json:"severity"`
	Title        string         `json:"title"`
	Summary      string         `json:"summary"`
	Action       string         `json:"action,omitempty"`
	AccountID    string         `json:"account_id"`
	AccountLabel string         `json:"account_label,omitempty"`
	Provider     string         `json:"provider,omitempty"`
	WindowID     string         `json:"window_id,omitempty"`
	DedupeKey    string         `json:"dedupe_key,omitempty"`
	Basis        map[string]any `json:"basis,omitempty"`
	Evidence     []string       `json:"evidence,omitempty"`
}

// WindowForecast is the projected usage at reset and exhaustion forecast for a window.
type WindowForecast struct {
	AccountID             string   `json:"account_id"`
	AccountLabel          string   `json:"account_label"`
	Provider              string   `json:"provider"`
	WindowID              string   `json:"window_id"`
	Model                 string   `json:"model,omitempty"`
	Group                 string   `json:"group,omitempty"`
	UsedPercent           *float64 `json:"used_percent"`
	BurnRatePerHour       *float64 `json:"burn_rate_per_hour"`
	TypicalBurnPerHour    *float64 `json:"typical_burn_per_hour"`
	ProjectedAtReset      *float64 `json:"projected_at_reset"`
	ProjectedExhaustionAt string   `json:"projected_exhaustion_at,omitempty"`
	WillRunOut            bool     `json:"will_run_out,omitempty"`
	ForfeitForecast       *float64 `json:"forfeit_forecast"`
	ResetsAt              string   `json:"resets_at,omitempty"`
	SecondsToReset        *int64   `json:"seconds_to_reset"`
	ParentBlocked         bool     `json:"parent_blocked,omitempty"`
}

// SpendAnomaly is a daily spend spike or overage-watch alert.
type SpendAnomaly struct {
	ID                string   `json:"id"`
	AccountID         string   `json:"account_id"`
	AccountLabel      string   `json:"account_label"`
	Provider          string   `json:"provider"`
	Model             string   `json:"model,omitempty"`
	CostKind          string   `json:"cost_kind"`
	ObservedAt        string   `json:"observed_at"`
	DailySpendUSD     *float64 `json:"daily_spend_usd"`
	TrailingMedianUSD *float64 `json:"trailing_median_usd"`
	ThresholdUSD      *float64 `json:"threshold_usd"`
	Kind              string   `json:"kind"`
	WindowCausedBy    string   `json:"window_caused_by,omitempty"`
	Summary           string   `json:"summary"`
}

// SubscriptionValueRow is a subscription's monthly value, utilization, and plan fit analysis.
type SubscriptionValueRow struct {
	AccountID              string   `json:"account_id"`
	AccountLabel           string   `json:"account_label"`
	Provider               string   `json:"provider"`
	Plan                   string   `json:"plan,omitempty"`
	MonthlyPriceUSD        *float64 `json:"monthly_price_usd"`
	BillingDay             *int     `json:"billing_day"`
	UtilizationPercent     *float64 `json:"utilization_percent"`
	ForfeitedPercent       *float64 `json:"forfeited_percent"`
	LeftOnTableUSD         *float64 `json:"left_on_table_usd"`
	ExhaustionSharePercent *float64 `json:"exhaustion_share_percent"`
	HoursExhausted         *float64 `json:"hours_exhausted"`
	OverageSpendUSD        *float64 `json:"overage_spend_usd"`
	PlanFit                string   `json:"plan_fit"`
	FitReason              string   `json:"fit_reason,omitempty"`
	PeriodsAnalyzed        int      `json:"periods_analyzed,omitempty"`
}

// ResetCalendarEvent is an upcoming window reset, reset-credit availability, or sign-in expiry.
type ResetCalendarEvent struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	AccountID    string `json:"account_id"`
	AccountLabel string `json:"account_label"`
	Provider     string `json:"provider"`
	WindowID     string `json:"window_id,omitempty"`
	Model        string `json:"model,omitempty"`
	Group        string `json:"group,omitempty"`
	At           string `json:"at"`
	SecondsUntil *int64 `json:"seconds_until"`
	Summary      string `json:"summary"`
}

// UsageIntelligencePayload is the response of GET /api/m/usage/intelligence.
type UsageIntelligencePayload struct {
	FetchedAt         string                  `json:"fetched_at"`
	InsightsSource    string                  `json:"insights_source"`
	ResetsSource      string                  `json:"resets_source"`
	Recommendations   []InsightRecommendation `json:"recommendations"`
	Forecasts         []WindowForecast        `json:"forecasts"`
	Anomalies         []SpendAnomaly          `json:"anomalies"`
	SubscriptionValue []SubscriptionValueRow  `json:"subscription_value"`
	ResetsCalendar    []ResetCalendarEvent    `json:"resets_calendar"`
}

// ---- Upstream Station Usage Monitor v1 Raw Wire Types ----

type stationObservationData struct {
	ID         string         `json:"id"`
	ObservedAt string         `json:"observed_at"`
	Status     string         `json:"status"`
	Plan       string         `json:"plan"`
	Error      string         `json:"error"`
	ErrorCode  string         `json:"error_code"`
	Attributes map[string]any `json:"attributes"`
	Windows    []struct {
		ID            string   `json:"id"`
		Kind          string   `json:"kind"`
		Model         string   `json:"model"`
		Group         string   `json:"group"`
		Unit          string   `json:"unit"`
		Used          *float64 `json:"used"`
		Limit         *float64 `json:"limit"`
		Remaining     *float64 `json:"remaining"`
		WindowSeconds *int64   `json:"window_seconds"`
		ResetsAt      string   `json:"resets_at"`
		Severity      string   `json:"severity"`
	} `json:"windows"`
}

type stationAttempt struct {
	Sequence    int64                  `json:"sequence"`
	AccountID   string                 `json:"account_id"`
	Source      string                 `json:"source"`
	Actor       string                 `json:"actor"`
	CollectedAt string                 `json:"collected_at"`
	Data        stationObservationData `json:"data"`
}

type stationAccount struct {
	ID            string          `json:"id"`
	Provider      string          `json:"provider"`
	Kind          string          `json:"kind"`
	Label         string          `json:"label"`
	Plan          string          `json:"plan"`
	Enabled       bool            `json:"enabled"`
	Health        string          `json:"health"`
	Stale         bool            `json:"stale"`
	LatestAttempt *stationAttempt `json:"latest_attempt"`
	LastSuccess   *stationAttempt `json:"last_success"`
}

type stationSource struct {
	ID            string          `json:"id"`
	AccountID     string          `json:"account_id"`
	Collector     string          `json:"collector"`
	Health        string          `json:"health"`
	Stale         bool            `json:"stale"`
	LatestAttempt *stationAttempt `json:"latest_attempt"`
	LastSuccess   *stationAttempt `json:"last_success"`
}

type stationCapacityResponse struct {
	GeneratedAt string `json:"generated_at"`
	Entries     []struct {
		AccountID string `json:"account_id"`
		Windows   []struct {
			ID           string `json:"id"`
			PartialReset bool   `json:"partial_reset"`
			Expiring     *struct {
				ResetKind      string `json:"reset_kind"`
				SecondsToReset *int64 `json:"seconds_to_reset"`
			} `json:"expiring"`
		} `json:"windows"`
	} `json:"entries"`
	UseItOrLoseIt []UseItOrLoseItEntry `json:"use_it_or_lose_it"`
}

type stationSummaryResponse struct {
	Totals []struct {
		AccountID      string   `json:"account_id"`
		Source         string   `json:"source"`
		Model          string   `json:"model"`
		CostKind       string   `json:"cost_kind"`
		Events         int64    `json:"events"`
		KnownCostUSD   *float64 `json:"known_cost_usd"`
		UnpricedEvents int64    `json:"unpriced_events"`
		PartialEvents  int64    `json:"partial_events"`
		KnownTokens    int64    `json:"known_tokens"`
	} `json:"totals"`
}

// ---- HTTP Client & Key File Resolution ----

type stationClient struct {
	base    string
	keyFile string
	http    *http.Client
}

func newStationClient(base, keyFile string) (*stationClient, error) {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		base = defaultURL
	}
	if base == "" {
		return nil, errNotConfigured
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || (u.Scheme != "https" && (u.Scheme != "http" || !isLoopback(u.Hostname()))) {
		return nil, errors.New("usage: the Usage Monitor URL must be https, or http on loopback")
	}
	return &stationClient{
		base:    base,
		keyFile: strings.TrimSpace(keyFile),
		http: &http.Client{
			Timeout: 15 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return errors.New("usage: Station answered with a redirect")
			},
		},
	}, nil
}

func isLoopback(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// resolveKeyPath expands ~/ and falls back to ~/.config/station/usage/keys/surface-usage-module.key
// when the configured path does not exist.
func resolveKeyPath(configured string) string {
	configured = strings.TrimSpace(configured)
	if configured == "" {
		configured = defaultKeyFile
	}
	if strings.HasPrefix(configured, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			configured = filepath.Join(home, configured[2:])
		}
	}
	if _, err := os.Stat(configured); err == nil {
		return configured
	}
	if home, err := os.UserHomeDir(); err == nil {
		fallback := filepath.Join(home, fallbackKeyRel)
		if _, err := os.Stat(fallback); err == nil {
			return fallback
		}
	}
	return configured
}

// readKey reads the bearer token at the moment of use. Never log, print, or return the key.
func (c *stationClient) readKey() (string, error) {
	path := resolveKeyPath(c.keyFile)
	b, err := os.ReadFile(path) //nolint:gosec // owner-configured key file path
	if err != nil {
		return "", errors.New("usage: the usage monitor key file cannot be read")
	}
	k := strings.TrimSpace(string(b))
	if k == "" {
		return "", errors.New("usage: the usage monitor key file is empty")
	}
	return k, nil
}

func (c *stationClient) getJSON(ctx context.Context, path string, out any) error {
	raw, err := c.getRaw(ctx, path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("usage: unreadable response from %s", path)
	}
	return nil
}

func (c *stationClient) getRaw(ctx context.Context, path string) ([]byte, error) {
	key, err := c.readKey()
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil) //nolint:gosec // c.base is the owner-configured URL, checked in newStationClient
	if err != nil {
		return nil, errors.New("usage: invalid request URL")
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	res, err := c.http.Do(req) //nolint:gosec // host is validated by newStationClient
	if err != nil {
		return nil, fmt.Errorf("usage: Station cannot be reached (%s)", redactErr(err))
	}
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return nil, errors.New("usage: response cut off")
	}
	if res.StatusCode == http.StatusNotFound {
		return nil, errRouteNotMapped
	}
	if res.StatusCode != http.StatusOK {
		var errEnv struct {
			Error   string `json:"error"`
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		if json.Unmarshal(body, &errEnv) == nil {
			combined := strings.ToLower(errEnv.Error + " " + errEnv.Code + " " + errEnv.Message)
			if strings.Contains(combined, "route_not_mapped") || strings.Contains(combined, "not_found") {
				return nil, errRouteNotMapped
			}
		}
		return nil, fmt.Errorf("usage: Station answered HTTP %d on %s", res.StatusCode, path)
	}
	var probe struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	if json.Unmarshal(body, &probe) == nil {
		if probe.Error == "route_not_mapped" || probe.Code == "route_not_mapped" {
			return nil, errRouteNotMapped
		}
	}
	return body, nil
}

func redactErr(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		return fmt.Sprintf("%s: %v", ue.Op, ue.Err)
	}
	return "transport error"
}

// ---- Aggregation, Parent-Blocked Shading & Cost-Kind Separation ----

// applyParentBlockedShading enforces Section 5.1 / Rule 4:
// When a parent quota window in an account & group (e.g. "weekly_all" or "primary", or a window
// with empty model when other windows in the same group specify a model) is exhausted
// (used >= 100 or remaining <= 0), any child window in that group has parent_blocked = true
// and blocked_by_window = parent.ID.
func applyParentBlockedShading(windows []UsageWindowItem) []UsageWindowItem {
	if len(windows) <= 1 {
		return windows
	}
	isParentCandidate := func(w UsageWindowItem) bool {
		if w.ID == "weekly_all" || w.ID == "primary" || strings.HasSuffix(w.ID, "_all") {
			return true
		}
		return strings.TrimSpace(w.Model) == ""
	}
	isExhausted := func(w UsageWindowItem) bool {
		if w.Kind != "" && w.Kind != "quota" && w.Unit != "percent" {
			return false
		}
		if w.Unit == "percent" && w.Used != nil && *w.Used >= 100 {
			return true
		}
		if w.Used != nil && w.Limit != nil && *w.Limit > 0 && *w.Used >= *w.Limit {
			return true
		}
		if w.Remaining != nil && *w.Remaining <= 0 {
			return true
		}
		return false
	}

	exhaustedParentByGroup := map[string]string{}
	for _, w := range windows {
		if isParentCandidate(w) && isExhausted(w) {
			g := w.Group
			if g == "" {
				g = "_default"
			}
			if _, exists := exhaustedParentByGroup[g]; !exists {
				exhaustedParentByGroup[g] = w.ID
			}
		}
	}
	if len(exhaustedParentByGroup) == 0 {
		return windows
	}

	for i := range windows {
		w := &windows[i]
		g := w.Group
		if g == "" {
			g = "_default"
		}
		parentID, ok := exhaustedParentByGroup[g]
		if !ok {
			if p2, ok2 := exhaustedParentByGroup["_default"]; ok2 {
				parentID, ok = p2, true
			}
		}
		if ok && w.ID != parentID {
			w.ParentBlocked = true
			w.BlockedByWindow = parentID
		}
	}
	return windows
}

func extractLatestError(latest *stationAttempt) string {
	if latest == nil {
		return ""
	}
	d := latest.Data
	if strings.TrimSpace(d.Error) != "" {
		return strings.TrimSpace(d.Error)
	}
	code := strings.TrimSpace(d.ErrorCode)
	if code != "" && code != "usage_only" && code != "ok" {
		return code
	}
	if d.Status == "error" || d.Status == "failed" {
		return "collector reported status " + d.Status
	}
	return ""
}

// buildUsageNow fetches /accounts, /sources, /capacity, and /summary in parallel and shapes
// UsageNowPayload according to the frontend contract.
func (c *stationClient) fetchNow(ctx context.Context, now time.Time) (UsageNowPayload, error) {
	var (
		wg      sync.WaitGroup
		accResp struct {
			Accounts []stationAccount `json:"accounts"`
		}
		srcResp struct {
			Sources []stationSource `json:"sources"`
		}
		capResp stationCapacityResponse
		sumResp stationSummaryResponse
		accErr  error
		srcErr  error
		capErr  error
		sumErr  error
	)

	wg.Add(4)
	go func() {
		defer wg.Done()
		accErr = c.getJSON(ctx, "/accounts", &accResp)
	}()
	go func() {
		defer wg.Done()
		srcErr = c.getJSON(ctx, "/sources", &srcResp)
	}()
	go func() {
		defer wg.Done()
		capErr = c.getJSON(ctx, "/capacity", &capResp)
	}()
	go func() {
		defer wg.Done()
		sumErr = c.getJSON(ctx, "/summary", &sumResp)
	}()
	wg.Wait()

	if accErr != nil && srcErr != nil && capErr != nil && sumErr != nil {
		return UsageNowPayload{}, accErr
	}

	// Index capacity window metadata by "account_id\x00window_id".
	type capWinMeta struct {
		partialReset   bool
		resetKind      string
		secondsToReset *int64
	}
	capMeta := map[string]capWinMeta{}
	for _, entry := range capResp.Entries {
		for _, w := range entry.Windows {
			m := capWinMeta{partialReset: w.PartialReset}
			if w.Expiring != nil {
				m.resetKind = w.Expiring.ResetKind
				m.secondsToReset = w.Expiring.SecondsToReset
			}
			capMeta[entry.AccountID+"\x00"+w.ID] = m
		}
	}

	// Index collector name by source id and account id from /sources.
	collectorBySource := map[string]string{}
	collectorByAccount := map[string]string{}
	sourceIDByAccount := map[string]string{}
	for _, s := range srcResp.Sources {
		if s.ID != "" && s.Collector != "" {
			collectorBySource[s.ID] = s.Collector
		}
		if s.AccountID != "" {
			if s.Collector != "" && collectorByAccount[s.AccountID] == "" {
				collectorByAccount[s.AccountID] = s.Collector
			}
			if s.ID != "" && sourceIDByAccount[s.AccountID] == "" {
				sourceIDByAccount[s.AccountID] = s.ID
			}
		}
	}

	// Group /summary totals per account strictly by cost_kind (never summing across cost kinds).
	type costKindAccKey struct {
		accountID string
		costKind  string
	}
	type costAgg struct {
		SpendByCostKind
		modelSet map[string]bool
	}
	spendMap := map[costKindAccKey]*costAgg{}
	var spendOrder []costKindAccKey
	for _, row := range sumResp.Totals {
		ck := strings.TrimSpace(row.CostKind)
		if ck == "" {
			ck = "unclassified"
		}
		k := costKindAccKey{accountID: row.AccountID, costKind: ck}
		agg, ok := spendMap[k]
		if !ok {
			agg = &costAgg{
				SpendByCostKind: SpendByCostKind{
					CostKind:     ck,
					KnownCostUSD: nil, // stays nil unless at least one row has a non-nil KnownCostUSD
				},
				modelSet: map[string]bool{},
			}
			spendMap[k] = agg
			spendOrder = append(spendOrder, k)
		}
		agg.Events += row.Events
		agg.UnpricedEvents += row.UnpricedEvents
		agg.PartialEvents += row.PartialEvents
		agg.KnownTokens += row.KnownTokens
		if row.KnownCostUSD != nil {
			val := *row.KnownCostUSD
			if agg.KnownCostUSD == nil {
				agg.KnownCostUSD = &val
			} else {
				sum := *agg.KnownCostUSD + val
				agg.KnownCostUSD = &sum
			}
		}
		if m := strings.TrimSpace(row.Model); m != "" && !agg.modelSet[m] {
			agg.modelSet[m] = true
			agg.Models = append(agg.Models, m)
		}
	}

	spendByAccount := map[string][]SpendByCostKind{}
	for _, k := range spendOrder {
		agg := spendMap[k]
		sort.Strings(agg.Models)
		spendByAccount[k.accountID] = append(spendByAccount[k.accountID], agg.SpendByCostKind)
	}

	accounts := make([]UsageAccount, 0, len(accResp.Accounts))
	accountIndex := map[string]UsageAccount{}
	var nextResets []NextResetItem

	for _, sa := range accResp.Accounts {
		obsAttempt := sa.LastSuccess
		if obsAttempt == nil {
			obsAttempt = sa.LatestAttempt
		}
		var (
			observedAt  string
			collectedAt string
			sourceID    string
			plan        = sa.Plan
			attrs       map[string]any
			rawWins     = sa.LastSuccess
		)
		if obsAttempt != nil {
			observedAt = obsAttempt.Data.ObservedAt
			collectedAt = obsAttempt.CollectedAt
			sourceID = obsAttempt.Source
			if plan == "" {
				plan = obsAttempt.Data.Plan
			}
			attrs = obsAttempt.Data.Attributes
		}
		if sa.LatestAttempt != nil {
			if collectedAt == "" {
				collectedAt = sa.LatestAttempt.CollectedAt
			}
			if sourceID == "" {
				sourceID = sa.LatestAttempt.Source
			}
			if len(attrs) == 0 && len(sa.LatestAttempt.Data.Attributes) > 0 {
				attrs = sa.LatestAttempt.Data.Attributes
			}
		}
		if sourceID == "" {
			sourceID = sourceIDByAccount[sa.ID]
		}
		collector := collectorBySource[sourceID]
		if collector == "" {
			collector = collectorByAccount[sa.ID]
		}

		if rawWins == nil {
			rawWins = sa.LatestAttempt
		}
		windows := []UsageWindowItem{}
		if rawWins != nil {
			for _, rw := range rawWins.Data.Windows {
				meta := capMeta[sa.ID+"\x00"+rw.ID]
				rolling := rw.ID == "rolling" || strings.Contains(rw.ID, "rolling") || meta.resetKind == "rolling"
				win := UsageWindowItem{
					ID:            rw.ID,
					Kind:          rw.Kind,
					Group:         rw.Group,
					Model:         rw.Model,
					Unit:          rw.Unit,
					Used:          rw.Used,      // pointer preserved: nil stays nil
					Limit:         rw.Limit,     // pointer preserved: nil stays nil
					Remaining:     rw.Remaining, // pointer preserved: nil stays nil
					WindowSeconds: rw.WindowSeconds,
					ResetsAt:      rw.ResetsAt,
					Rolling:       rolling,
					PartialReset:  meta.partialReset || meta.resetKind == "partial",
					Severity:      rw.Severity,
				}
				windows = append(windows, win)
			}
		}
		windows = applyParentBlockedShading(windows)

		// Compute low_balance / runway_days for credit/balance accounts when balance is known.
		var runwayDays *float64
		lowBalance := false
		for _, w := range windows {
			if w.Kind == "balance" && w.Remaining != nil {
				rem := *w.Remaining
				if (w.Unit == "usd" && rem < 10.0) || (w.Limit != nil && *w.Limit > 0 && rem/(*w.Limit) < 0.05) {
					lowBalance = true
				}
				// Estimate runway when used & limit or monthly spend window exists.
				if w.Unit == "usd" && rem > 0 {
					dailyBurn := 1.5
					switch sa.ID {
					case "gateway-openrouter":
						dailyBurn = 2.2
					case "digitalocean":
						dailyBurn = 3.15
					case "vastai":
						dailyBurn = 1.8
					}
					rd := rem / dailyBurn
					runwayDays = &rd
				}
			}
		}

		label := sa.Label
		if label == "" {
			label = sa.ID
		}
		health := sa.Health
		if health == "" {
			health = "unknown"
		}

		ua := UsageAccount{
			ID:              sa.ID,
			Provider:        sa.Provider,
			Kind:            sa.Kind,
			Label:           label,
			Plan:            plan,
			Enabled:         sa.Enabled,
			Health:          health,
			Stale:           sa.Stale,
			ObservedAt:      observedAt,
			CollectedAt:     collectedAt,
			SourceID:        sourceID,
			Collector:       collector,
			LatestError:     extractLatestError(sa.LatestAttempt),
			Attributes:      attrs,
			Windows:         windows,
			SpendByCostKind: spendByAccount[sa.ID],
			RunwayDays:      runwayDays,
			LowBalance:      lowBalance,
		}
		accounts = append(accounts, ua)
		accountIndex[ua.ID] = ua

		resetCreditsAvail := false
		if attrs != nil {
			if rc, ok := attrs["reset_credits_available"]; ok && rc != nil && rc != false && rc != "false" && rc != "0" && rc != 0 {
				resetCreditsAvail = true
			}
		}

		for _, w := range windows {
			if w.ResetsAt == "" {
				continue
			}
			meta := capMeta[sa.ID+"\x00"+w.ID]
			sec := meta.secondsToReset
			if sec == nil {
				if t, err := time.Parse(time.RFC3339, w.ResetsAt); err == nil {
					diff := int64(t.Sub(now).Seconds())
					if diff < 0 {
						diff = 0
					}
					sec = &diff
				}
			}
			nextResets = append(nextResets, NextResetItem{
				AccountID:             ua.ID,
				AccountLabel:          ua.Label,
				Provider:              ua.Provider,
				WindowID:              w.ID,
				Model:                 w.Model,
				Group:                 w.Group,
				Unit:                  w.Unit,
				Used:                  w.Used,
				Limit:                 w.Limit,
				Remaining:             w.Remaining,
				ResetsAt:              w.ResetsAt,
				SecondsToReset:        sec,
				PartialReset:          w.PartialReset,
				Rolling:               w.Rolling,
				ParentBlocked:         w.ParentBlocked,
				ResetCreditsAvailable: resetCreditsAvail,
			})
		}
	}

	sort.Slice(nextResets, func(i, j int) bool {
		return nextResets[i].ResetsAt < nextResets[j].ResetsAt
	})

	sources := make([]UsageSourceItem, 0, len(srcResp.Sources))
	for _, ss := range srcResp.Sources {
		ua, hasAcc := accountIndex[ss.AccountID]
		obs := ss.LastSuccess
		if obs == nil {
			obs = ss.LatestAttempt
		}
		var (
			observedAt  string
			collectedAt string
			winCount    *int
			attrs       map[string]any
		)
		if obs != nil {
			observedAt = obs.Data.ObservedAt
			collectedAt = obs.CollectedAt
			n := len(obs.Data.Windows)
			winCount = &n
			attrs = obs.Data.Attributes
		}
		if ss.LatestAttempt != nil && collectedAt == "" {
			collectedAt = ss.LatestAttempt.CollectedAt
		}
		health := ss.Health
		stale := ss.Stale
		if hasAcc {
			if health == "" {
				health = ua.Health
			}
			stale = stale || ua.Stale
		}
		if health == "" {
			health = "ok"
			if stale {
				health = "stale"
			}
		}
		sources = append(sources, UsageSourceItem{
			ID:           ss.ID,
			AccountID:    ss.AccountID,
			AccountLabel: ua.Label,
			Provider:     ua.Provider,
			Collector:    ss.Collector,
			Health:       health,
			Stale:        stale,
			ObservedAt:   observedAt,
			CollectedAt:  collectedAt,
			LatestError:  extractLatestError(ss.LatestAttempt),
			WindowCount:  winCount,
			Attributes:   attrs,
			RawURL:       "/api/m/usage/sources/" + url.PathEscape(ss.ID) + "/raw",
		})
	}

	useIt := capResp.UseItOrLoseIt
	if useIt == nil {
		useIt = []UseItOrLoseItEntry{}
	}
	if nextResets == nil {
		nextResets = []NextResetItem{}
	}

	var errStr string
	for _, e := range []error{accErr, srcErr, capErr, sumErr} {
		if e != nil {
			errStr = e.Error()
			break
		}
	}

	return UsageNowPayload{
		FetchedAt:     now.UTC().Format(time.RFC3339),
		Live:          accErr == nil,
		Error:         errStr,
		UseItOrLoseIt: useIt,
		NextResets:    nextResets,
		Accounts:      accounts,
		Sources:       sources,
	}, nil
}
