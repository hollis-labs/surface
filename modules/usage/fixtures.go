package usage

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

func floatPtr(v float64) *float64 {
	r := math.Round(v*1000) / 1000
	return &r
}

func intPtr(v int) *int       { return &v }
func int64Ptr(v int64) *int64 { return &v }

// buildFixtureSeries produces realistic per-window usage and balance curves derived from the
// live accounts (or the canonical 20 accounts when offline) for GET /trends when /v1/series
// returns 404 / route_not_mapped.
func buildFixtureSeries(now time.Time, accounts []UsageAccount, accountFilter, step string) []WindowSeries {
	if step == "" {
		step = "1h"
	}
	type seedWin struct {
		accountID    string
		accountLabel string
		provider     string
		windowID     string
		model        string
		group        string
		unit         string
		kind         string
		curEnd       float64
		prevEnd      float64
		limit        float64
	}

	var seeds []seedWin
	for _, a := range accounts {
		for _, w := range a.Windows {
			if w.Used == nil && w.Remaining == nil {
				continue
			}
			kind := w.Kind
			if kind == "" {
				kind = "quota"
			}
			if kind == "spend" {
				continue
			}
			curVal := 0.0
			if kind == "balance" {
				if w.Remaining != nil {
					curVal = *w.Remaining
				}
			} else if w.Used != nil {
				curVal = *w.Used
			}
			lim := 100.0
			if w.Limit != nil && *w.Limit > 0 {
				lim = *w.Limit
			} else if kind == "balance" && curVal > 0 {
				lim = curVal * 1.35
			}
			prevVal := math.Min(lim, curVal*1.25+14.0)
			if kind == "balance" {
				prevVal = curVal * 1.18
			}
			seeds = append(seeds, seedWin{
				accountID:    a.ID,
				accountLabel: a.Label,
				provider:     a.Provider,
				windowID:     w.ID,
				model:        w.Model,
				group:        w.Group,
				unit:         w.Unit,
				kind:         kind,
				curEnd:       curVal,
				prevEnd:      prevVal,
				limit:        lim,
			})
		}
	}

	if len(seeds) == 0 {
		seeds = []seedWin{
			{accountID: "claude-personal", accountLabel: "Claude", provider: "anthropic", windowID: "weekly_all", model: "All Models", group: "weekly", unit: "percent", kind: "quota", curEnd: 42.5, prevEnd: 78.0, limit: 100},
			{accountID: "codex-personal", accountLabel: "Codex", provider: "openai", windowID: "primary", model: "Codex Pro", group: "weekly", unit: "percent", kind: "quota", curEnd: 100.0, prevEnd: 94.0, limit: 100},
			{accountID: "github-personal", accountLabel: "GitHub", provider: "github", windowID: "premium_requests_monthly", model: "Copilot Premium Requests", group: "monthly", unit: "percent", kind: "quota", curEnd: 96.02, prevEnd: 88.5, limit: 100},
			{accountID: "agy-personal", accountLabel: "Antigravity", provider: "antigravity", windowID: "g0:gemini-5h", model: "Gemini Models", group: "5h", unit: "percent", kind: "quota", curEnd: 9.73, prevEnd: 64.2, limit: 100},
			{accountID: "go-workspace", accountLabel: "OpenCode Go", provider: "opencode", windowID: "rolling", model: "Go Workspace", group: "5h", unit: "percent", kind: "quota", curEnd: 18.4, prevEnd: 52.0, limit: 100},
			{accountID: "gateway-openrouter", accountLabel: "OpenRouter", provider: "openrouter", windowID: "account_balance", model: "Prepaid Balance", group: "balance", unit: "usd", kind: "balance", curEnd: 4.95, prevEnd: 48.20, limit: 245},
			{accountID: "digitalocean", accountLabel: "DigitalOcean", provider: "digitalocean", windowID: "account_balance", model: "Credit Balance", group: "balance", unit: "usd", kind: "balance", curEnd: 50.12, prevEnd: 88.50, limit: 150},
		}
	}

	out := []WindowSeries{}
	for _, s := range seeds {
		if accountFilter != "" && accountFilter != "all" && s.accountID != accountFilter {
			continue
		}
		const nPts = 12
		curPts := make([]SeriesPoint, 0, nPts)
		prevPts := make([]SeriesPoint, 0, nPts)
		for i := 0; i < nPts; i++ {
			frac := float64(i) / float64(nPts-1)
			t := now.Add(-time.Duration(nPts-1-i) * 4 * time.Hour).UTC().Format(time.RFC3339)
			if s.kind == "balance" {
				startBal := s.curEnd + (s.limit-s.curEnd)*0.22
				rem := startBal - (startBal-s.curEnd)*frac
				used := math.Max(0, s.limit-rem)
				curPts = append(curPts, SeriesPoint{
					At:           t,
					Used:         floatPtr(used),
					Remaining:    floatPtr(rem),
					MinRemaining: floatPtr(rem),
				})
				pStart := s.prevEnd * 1.15
				pRem := pStart - (pStart-s.prevEnd)*frac
				prevPts = append(prevPts, SeriesPoint{
					At:        t,
					Remaining: floatPtr(pRem),
				})
			} else {
				used := s.curEnd * math.Pow(frac, 0.85)
				rem := math.Max(0, s.limit-used)
				curPts = append(curPts, SeriesPoint{
					At:        t,
					Used:      floatPtr(used),
					Remaining: floatPtr(rem),
					MaxUsed:   floatPtr(used),
				})
				pUsed := s.prevEnd * math.Pow(frac, 0.9)
				pRem := math.Max(0, s.limit-pUsed)
				prevPts = append(prevPts, SeriesPoint{
					At:        t,
					Used:      floatPtr(pUsed),
					Remaining: floatPtr(pRem),
				})
			}
		}
		out = append(out, WindowSeries{
			AccountID:      s.accountID,
			AccountLabel:   s.accountLabel,
			Provider:       s.provider,
			WindowID:       s.windowID,
			Model:          s.model,
			Group:          s.group,
			Unit:           s.unit,
			Kind:           s.kind,
			Step:           step,
			CurrentPeriod:  curPts,
			PreviousPeriod: prevPts,
		})
	}
	return out
}

// buildFixturePeriods returns realistic completed period rows for GET /trends when /v1/periods
// returns 404 / route_not_mapped.
func buildFixturePeriods(now time.Time, accountFilter string) []CompletedPeriodRow {
	p1End := now.Add(-24 * time.Hour).UTC().Truncate(time.Hour)
	p1Start := p1End.Add(-7 * 24 * time.Hour)
	p2End := p1Start
	p2Start := p2End.Add(-7 * 24 * time.Hour)

	rows := []CompletedPeriodRow{
		{
			AccountID:          "codex-personal",
			AccountLabel:       "Codex",
			Provider:           "openai",
			WindowID:           "primary",
			Model:              "Codex Pro Weekly",
			Group:              "weekly",
			PeriodStart:        p1Start.Format(time.RFC3339),
			PeriodEnd:          p1End.Format(time.RFC3339),
			PeakUsedPercent:    floatPtr(100),
			UsedAtResetPercent: floatPtr(100),
			CapturedPercent:    floatPtr(100),
			ForfeitedPercent:   floatPtr(0),
			ExhaustedAt:        p1End.Add(-18 * time.Hour).Format(time.RFC3339),
			HoursExhausted:     floatPtr(18.0),
		},
		{
			AccountID:          "claude-personal",
			AccountLabel:       "Claude",
			Provider:           "anthropic",
			WindowID:           "weekly_all",
			Model:              "Claude Max Weekly (All Models)",
			Group:              "weekly",
			PeriodStart:        p1Start.Format(time.RFC3339),
			PeriodEnd:          p1End.Format(time.RFC3339),
			PeakUsedPercent:    floatPtr(84.2),
			UsedAtResetPercent: floatPtr(84.2),
			CapturedPercent:    floatPtr(84.2),
			ForfeitedPercent:   floatPtr(15.8),
			HoursExhausted:     floatPtr(0),
		},
		{
			AccountID:          "claude-personal",
			AccountLabel:       "Claude",
			Provider:           "anthropic",
			WindowID:           "weekly_scoped:fable",
			Model:              "Fable",
			Group:              "weekly",
			PeriodStart:        p2Start.Format(time.RFC3339),
			PeriodEnd:          p2End.Format(time.RFC3339),
			PeakUsedPercent:    floatPtr(42.0),
			UsedAtResetPercent: floatPtr(42.0),
			CapturedPercent:    floatPtr(42.0),
			ForfeitedPercent:   floatPtr(0),
			HoursExhausted:     floatPtr(11.5),
			ParentBlocked:      true,
		},
		{
			AccountID:          "agy-personal",
			AccountLabel:       "Antigravity",
			Provider:           "antigravity",
			WindowID:           "g0:gemini-weekly",
			Model:              "Gemini Models",
			Group:              "weekly",
			PeriodStart:        p1Start.Format(time.RFC3339),
			PeriodEnd:          p1End.Format(time.RFC3339),
			PeakUsedPercent:    floatPtr(58.4),
			UsedAtResetPercent: floatPtr(58.4),
			CapturedPercent:    floatPtr(58.4),
			ForfeitedPercent:   floatPtr(41.6),
			HoursExhausted:     floatPtr(0),
		},
		{
			AccountID:          "agy-personal",
			AccountLabel:       "Antigravity",
			Provider:           "antigravity",
			WindowID:           "g1:3p-weekly",
			Model:              "Claude and GPT models",
			Group:              "weekly",
			PeriodStart:        p1Start.Format(time.RFC3339),
			PeriodEnd:          p1End.Format(time.RFC3339),
			PeakUsedPercent:    floatPtr(22.5),
			UsedAtResetPercent: floatPtr(22.5),
			CapturedPercent:    floatPtr(22.5),
			ForfeitedPercent:   floatPtr(77.5),
			HoursExhausted:     floatPtr(0),
		},
		{
			AccountID:          "go-workspace",
			AccountLabel:       "OpenCode Go",
			Provider:           "opencode",
			WindowID:           "rolling",
			Model:              "Rolling 5h Quota",
			Group:              "5h",
			PeriodStart:        now.Add(-10 * time.Hour).UTC().Format(time.RFC3339),
			PeriodEnd:          now.Add(-5 * time.Hour).UTC().Format(time.RFC3339),
			PeakUsedPercent:    floatPtr(63.0),
			UsedAtResetPercent: floatPtr(63.0),
			CapturedPercent:    floatPtr(63.0),
			ForfeitedPercent:   floatPtr(0),
			Rolling:            true,
		},
		{
			AccountID:          "github-personal",
			AccountLabel:       "GitHub",
			Provider:           "github",
			WindowID:           "premium_requests_monthly",
			Model:              "Copilot Premium Requests",
			Group:              "monthly",
			PeriodStart:        p2Start.Format(time.RFC3339),
			PeriodEnd:          p1End.Format(time.RFC3339),
			PeakUsedPercent:    floatPtr(91.4),
			UsedAtResetPercent: floatPtr(91.4),
			CapturedPercent:    floatPtr(91.4),
			ForfeitedPercent:   floatPtr(8.6),
			HoursExhausted:     floatPtr(0),
		},
		{
			AccountID:          "elevenlabs",
			AccountLabel:       "ElevenLabs",
			Provider:           "elevenlabs",
			WindowID:           "characters_monthly_percent",
			Model:              "Creator Monthly Characters",
			Group:              "monthly",
			PeriodStart:        p2Start.Format(time.RFC3339),
			PeriodEnd:          p1End.Format(time.RFC3339),
			PeakUsedPercent:    floatPtr(48.0),
			UsedAtResetPercent: floatPtr(48.0),
			CapturedPercent:    floatPtr(48.0),
			ForfeitedPercent:   floatPtr(52.0),
			HoursExhausted:     floatPtr(0),
		},
		{
			AccountID:          "higgsfield-subscription",
			AccountLabel:       "Higgsfield",
			Provider:           "higgsfield",
			WindowID:           "credits_monthly",
			Model:              "Creator Video Credits",
			Group:              "monthly",
			PeriodStart:        p2Start.Format(time.RFC3339),
			PeriodEnd:          p1End.Format(time.RFC3339),
			PeakUsedPercent:    floatPtr(35.0),
			UsedAtResetPercent: floatPtr(35.0),
			CapturedPercent:    floatPtr(35.0),
			ForfeitedPercent:   floatPtr(65.0),
			HoursExhausted:     floatPtr(0),
		},
	}

	if accountFilter == "" || accountFilter == "all" {
		return rows
	}
	var filtered []CompletedPeriodRow
	for _, r := range rows {
		if r.AccountID == accountFilter {
			filtered = append(filtered, r)
		}
	}
	return filtered
}

// buildFixtureSpend builds contract-compliant spend rows separated strictly by cost_kind,
// incorporating live /v1/summary totals when available plus multi-bucket history across the
// cost_kind taxonomy (api_billed, api_estimated, subscription_equivalent, local_compute).
func buildFixtureSpend(now time.Time, accounts []UsageAccount, costKindFilter string) []SpendGroupRow {
	day0 := now.UTC().Format("2006-01-02")
	day1 := now.Add(-24 * time.Hour).UTC().Format("2006-01-02")

	var rows []SpendGroupRow
	// Include live summary totals per account/cost_kind first.
	for _, a := range accounts {
		for _, sp := range a.SpendByCostKind {
			model := a.Provider + "-default"
			if len(sp.Models) > 0 {
				model = sp.Models[0]
			}
			rows = append(rows, SpendGroupRow{
				Bucket:         day0,
				GroupKey:       a.Provider,
				Provider:       a.Provider,
				Model:          model,
				AccountID:      a.ID,
				CostKind:       sp.CostKind,
				KnownCostUSD:   sp.KnownCostUSD,
				Events:         sp.Events,
				UnpricedEvents: sp.UnpricedEvents,
				KnownTokens:    sp.KnownTokens,
			})
		}
	}

	// Ensure all four canonical cost_kinds have realistic multi-provider / multi-model entries.
	baseline := []SpendGroupRow{
		{Bucket: day0, GroupKey: "anthropic-work", Provider: "anthropic-work", Model: "claude-sonnet-4-5", AccountID: "gateway-anthropic-work", CostKind: "api_estimated", KnownCostUSD: floatPtr(18.42), Events: 312, KnownTokens: 4820100},
		{Bucket: day1, GroupKey: "anthropic-work", Provider: "anthropic-work", Model: "claude-haiku-5-5", AccountID: "gateway-anthropic-work", CostKind: "api_estimated", KnownCostUSD: floatPtr(3.15), Events: 185, KnownTokens: 1920400},
		{Bucket: day0, GroupKey: "openrouter", Provider: "openrouter", Model: "deepseek/deepseek-r1", AccountID: "gateway-openrouter", CostKind: "api_billed", KnownCostUSD: floatPtr(14.80), Events: 240, KnownTokens: 6150000},
		{Bucket: day1, GroupKey: "openai-work", Provider: "openai-work", Model: "gpt-5.4", AccountID: "gateway-openai-work", CostKind: "api_billed", KnownCostUSD: floatPtr(9.65), Events: 142, KnownTokens: 2180000},
		{Bucket: day0, GroupKey: "digitalocean", Provider: "digitalocean", Model: "droplet-fleet", AccountID: "digitalocean", CostKind: "api_billed", KnownCostUSD: floatPtr(94.38), Events: 30, KnownTokens: 0},
		{Bucket: day0, GroupKey: "anthropic", Provider: "anthropic", Model: "claude-opus-4-7", AccountID: "claude-personal", CostKind: "subscription_equivalent", KnownCostUSD: floatPtr(142.50), Events: 690, KnownTokens: 19400000},
		{Bucket: day0, GroupKey: "openai", Provider: "openai", Model: "codex-pro", AccountID: "codex-personal", CostKind: "subscription_equivalent", KnownCostUSD: floatPtr(215.00), Events: 940, KnownTokens: 28500000},
		{Bucket: day0, GroupKey: "github", Provider: "github", Model: "copilot-premium-requests", AccountID: "github-personal", CostKind: "subscription_equivalent", KnownCostUSD: floatPtr(57.60), Events: 1440, KnownTokens: 11500000},
		{Bucket: day0, GroupKey: "antigravity", Provider: "antigravity", Model: "gemini-2.5-pro", AccountID: "agy-personal", CostKind: "subscription_equivalent", KnownCostUSD: floatPtr(68.20), Events: 410, KnownTokens: 14200000},
		{Bucket: day0, GroupKey: "vastai", Provider: "vastai", Model: "rtx-4090-worker", AccountID: "vastai", CostKind: "local_compute", KnownCostUSD: floatPtr(12.40), Events: 88, KnownTokens: 3400000},
	}
	rows = append(rows, baseline...)

	if costKindFilter == "" {
		return rows
	}
	var filtered []SpendGroupRow
	for _, r := range rows {
		if r.CostKind == costKindFilter {
			filtered = append(filtered, r)
		}
	}
	return filtered
}

// buildIntelligence synthesizes live signals from /v1/capacity and /v1/accounts with
// deterministic contract-compliant forecasts, anomalies, subscription value analysis, and
// reset calendar events.
func buildIntelligence(now time.Time, nowPayload UsageNowPayload) UsageIntelligencePayload {
	var (
		recs      []InsightRecommendation
		forecasts []WindowForecast
		anomalies []SpendAnomaly
		subValues []SubscriptionValueRow
		calendar  []ResetCalendarEvent
	)

	seenRec := map[string]bool{}
	addRec := func(r InsightRecommendation) {
		key := r.DedupeKey
		if key == "" {
			key = r.Kind + ":" + r.AccountID + ":" + r.WindowID
		}
		if seenRec[key] {
			return
		}
		seenRec[key] = true
		r.DedupeKey = key
		recs = append(recs, r)
	}

	// 1. Live use_it_or_lose_it expiring capacity recommendations.
	for _, u := range nowPayload.UseItOrLoseIt {
		if u.ForfeitablePercent == nil || *u.ForfeitablePercent < 20 {
			continue
		}
		if len(recs) >= 3 {
			break
		}
		sev := "info"
		if u.UrgencyScore != nil && *u.UrgencyScore >= 500 {
			sev = "warning"
		}
		models := strings.Join(u.GatewayModels, ", ")
		if models == "" {
			models = u.ModelGroup
		}
		addRec(InsightRecommendation{
			ID:           fmt.Sprintf("rec_forfeit_%s_%s", u.AccountID, u.WindowID),
			Kind:         "forfeit_soon",
			Severity:     sev,
			Title:        fmt.Sprintf("Use %.1f%% expiring %s capacity on %s before reset", *u.ForfeitablePercent, u.ModelGroup, u.AccountLabel),
			Summary:      fmt.Sprintf("Window %s on %s has %.1f%% unused subscription quota resetting at %s.", u.WindowID, u.AccountLabel, *u.ForfeitablePercent, u.ResetsAt),
			Action:       fmt.Sprintf("Route interactive or background turns to %s (%s)", u.AccountLabel, models),
			AccountID:    u.AccountID,
			AccountLabel: u.AccountLabel,
			Provider:     u.Provider,
			WindowID:     u.WindowID,
			Basis: map[string]any{
				"forfeitable_percent": *u.ForfeitablePercent,
				"urgency_score":       u.UrgencyScore,
				"resets_at":           u.ResetsAt,
			},
			Evidence: []string{
				fmt.Sprintf("Ranked #%d in Station /v1/capacity use_it_or_lose_it", u.Rank),
				fmt.Sprintf("Reset kind: %s", u.ResetKind),
			},
		})
	}

	// 2. Inspect live accounts for exhausted windows, near-exhaustion quotas (e.g. GitHub Copilot 96%), low balances, sign-in expiry, overage, and stale collectors.
	for _, a := range nowPayload.Accounts {
		for _, w := range a.Windows {
			var usedPct *float64
			if (w.Kind == "quota" || w.Unit == "percent") && w.Used != nil {
				if w.Unit == "percent" {
					usedPct = w.Used
				} else if w.Limit != nil && *w.Limit > 0 {
					p := (*w.Used / *w.Limit) * 100.0
					usedPct = &p
				}
			}
			if usedPct != nil && *usedPct >= 99.9 {
				addRec(InsightRecommendation{
					ID:           fmt.Sprintf("rec_blocked_%s_%s", a.ID, w.ID),
					Kind:         "blocked",
					Severity:     "critical",
					Title:        fmt.Sprintf("%s window %s is 100%% exhausted until reset", a.Label, w.ID),
					Summary:      fmt.Sprintf("Account %s (%s) has exhausted quota window %s. Shift workloads to remaining subscription capacity on Claude or Antigravity.", a.Label, a.ID, w.ID),
					Action:       fmt.Sprintf("Pause %s dispatches or switch to available subscription windows until %s", a.Label, w.ResetsAt),
					AccountID:    a.ID,
					AccountLabel: a.Label,
					Provider:     a.Provider,
					WindowID:     w.ID,
					Basis: map[string]any{
						"used_percent": *usedPct,
						"resets_at":    w.ResetsAt,
					},
					Evidence: []string{
						fmt.Sprintf("Live observation on %s shows %s used=%.1f%%, remaining=0%%", a.ID, w.ID, *usedPct),
					},
				})
			} else if usedPct != nil && *usedPct >= 90.0 && w.Unit == "percent" {
				remPct := math.Max(0, 100.0-*usedPct)
				addRec(InsightRecommendation{
					ID:           fmt.Sprintf("rec_headroom_%s_%s", a.ID, w.ID),
					Kind:         "plan_fit",
					Severity:     "warning",
					Title:        fmt.Sprintf("%s window %s is at %.1f%% (%.1f%% remaining before reset)", a.Label, w.ID, *usedPct, remPct),
					Summary:      fmt.Sprintf("Account %s (%s) has consumed %.1f%% of %s before its reset at %s.", a.Label, a.ID, *usedPct, w.ID, w.ResetsAt),
					Action:       fmt.Sprintf("Reserve remaining %.1f%% on %s for high-priority turns or route overflow to Antigravity / Claude", remPct, a.Label),
					AccountID:    a.ID,
					AccountLabel: a.Label,
					Provider:     a.Provider,
					WindowID:     w.ID,
					Basis: map[string]any{
						"used_percent":      math.Round(*usedPct*100) / 100,
						"remaining_percent": math.Round(remPct*100) / 100,
						"resets_at":         w.ResetsAt,
					},
					Evidence: []string{
						fmt.Sprintf("Live collector %s on %s reports %s used=%.2f%%", a.SourceID, a.ID, w.ID, *usedPct),
					},
				})
			}
		}

		if a.LowBalance {
			var remVal, limVal *float64
			for _, w := range a.Windows {
				if w.Kind == "balance" {
					remVal, limVal = w.Remaining, w.Limit
					break
				}
			}
			remText := "low balance"
			if remVal != nil {
				remText = fmt.Sprintf("$%.2f remaining", *remVal)
			}
			addRec(InsightRecommendation{
				ID:           "rec_topup_" + a.ID,
				Kind:         "top_up",
				Severity:     "warning",
				Title:        fmt.Sprintf("Top up %s prepaid balance (%s)", a.Label, remText),
				Summary:      fmt.Sprintf("Prepaid balance on %s is approaching the low-balance floor (%s).", a.Label, remText),
				Action:       fmt.Sprintf("Add credits to %s before automated gateway fallback calls fail", a.Label),
				AccountID:    a.ID,
				AccountLabel: a.Label,
				Provider:     a.Provider,
				WindowID:     "account_balance",
				Basis: map[string]any{
					"remaining_usd": remVal,
					"limit_usd":     limVal,
					"runway_days":   a.RunwayDays,
				},
				Evidence: []string{
					fmt.Sprintf("Balance collector for %s reported %s", a.ID, remText),
				},
			})
		}

		if a.Attributes != nil {
			if exp, ok := a.Attributes["signin_expires_at"].(string); ok && exp != "" {
				var secUntil *int64
				if t, err := time.Parse(time.RFC3339, exp); err == nil {
					s := int64(t.Sub(now).Seconds())
					secUntil = &s
				}
				calendar = append(calendar, ResetCalendarEvent{
					ID:           "cal_signin_" + a.ID,
					Kind:         "signin_expiry",
					AccountID:    a.ID,
					AccountLabel: a.Label,
					Provider:     a.Provider,
					At:           exp,
					SecondsUntil: secUntil,
					Summary:      fmt.Sprintf("%s OAuth / browser session expires", a.Label),
				})
				addRec(InsightRecommendation{
					ID:           "rec_human_signin_" + a.ID,
					Kind:         "human_action",
					Severity:     "info",
					Title:        fmt.Sprintf("Upcoming OAuth sign-in expiry for %s", a.Label),
					Summary:      fmt.Sprintf("Collector session for %s expires at %s.", a.Label, exp),
					Action:       fmt.Sprintf("Refresh %s OAuth session before expiry to prevent collector gaps", a.Label),
					AccountID:    a.ID,
					AccountLabel: a.Label,
					Provider:     a.Provider,
					Basis:        map[string]any{"signin_expires_at": exp},
					Evidence:     []string{fmt.Sprintf("Attribute signin_expires_at=%s on %s", exp, a.ID)},
				})
			}
			if rc, ok := a.Attributes["reset_credits_available"]; ok && rc != nil && rc != false && rc != "false" && rc != "0" && rc != 0 {
				calendar = append(calendar, ResetCalendarEvent{
					ID:           "cal_reset_credits_" + a.ID,
					Kind:         "reset_credits",
					AccountID:    a.ID,
					AccountLabel: a.Label,
					Provider:     a.Provider,
					At:           now.UTC().Format(time.RFC3339),
					SecondsUntil: int64Ptr(0),
					Summary:      fmt.Sprintf("%s has manual reset credits available (%v)", a.Label, rc),
				})
			}
		}

		if a.Stale || a.Health == "stale" || a.Health == "error" || a.LatestError != "" {
			if a.ID == "digitalocean" || a.ID == "vastai" || a.Health == "error" || a.LatestError != "" {
				sev := "warning"
				if a.Health == "error" || a.LatestError != "" {
					sev = "critical"
				}
				reason := "observation reading is stale"
				if a.LatestError != "" {
					reason = a.LatestError
				}
				addRec(InsightRecommendation{
					ID:           "rec_collector_" + a.ID,
					Kind:         "collector_broken",
					Severity:     sev,
					Title:        fmt.Sprintf("Refresh %s collector (%s)", a.Label, a.Health),
					Summary:      fmt.Sprintf("Source %s for %s is marked %s (%s). Last observation: %s.", a.SourceID, a.Label, a.Health, reason, a.ObservedAt),
					Action:       fmt.Sprintf("Verify Cerberus / Station collector timer for %s", a.SourceID),
					AccountID:    a.ID,
					AccountLabel: a.Label,
					Provider:     a.Provider,
					Basis: map[string]any{
						"health":       a.Health,
						"stale":        a.Stale,
						"observed_at":  a.ObservedAt,
						"latest_error": a.LatestError,
					},
					Evidence: []string{
						fmt.Sprintf("Account %s health=%s, stale=%v", a.ID, a.Health, a.Stale),
					},
				})
			}
		}

		// Build forecasts for quota windows with known used %
		for _, w := range a.Windows {
			if (w.Kind == "quota" || w.Unit == "percent") && w.Used != nil {
				used := *w.Used
				winSec := int64(604800)
				if w.WindowSeconds != nil && *w.WindowSeconds > 0 {
					winSec = *w.WindowSeconds
				}
				var secLeft *int64
				if w.ResetsAt != "" {
					if t, err := time.Parse(time.RFC3339, w.ResetsAt); err == nil {
						diff := int64(t.Sub(now).Seconds())
						if diff < 0 {
							diff = 0
						}
						secLeft = &diff
					}
				}
				elapsedHours := 24.0
				remHours := 48.0
				if secLeft != nil {
					remHours = float64(*secLeft) / 3600.0
					elapsedHours = math.Max(1.0, float64(winSec-*secLeft)/3600.0)
				}
				burnPerHour := used / elapsedHours
				typicalBurn := math.Max(0.25, burnPerHour*0.9)
				projected := math.Min(100.0, used+burnPerHour*remHours)
				willRunOut := used >= 100.0 || projected >= 99.5
				forfeit := math.Max(0.0, 100.0-projected)
				if w.Rolling || w.ParentBlocked {
					forfeit = 0.0
				}
				var exhAt string
				if willRunOut && burnPerHour > 0 && used < 100 {
					hrsTo100 := (100.0 - used) / burnPerHour
					exhAt = now.Add(time.Duration(hrsTo100 * float64(time.Hour))).UTC().Format(time.RFC3339)
				}
				forecasts = append(forecasts, WindowForecast{
					AccountID:             a.ID,
					AccountLabel:          a.Label,
					Provider:              a.Provider,
					WindowID:              w.ID,
					Model:                 w.Model,
					Group:                 w.Group,
					UsedPercent:           floatPtr(used),
					BurnRatePerHour:       floatPtr(burnPerHour),
					TypicalBurnPerHour:    floatPtr(typicalBurn),
					ProjectedAtReset:      floatPtr(projected),
					ProjectedExhaustionAt: exhAt,
					WillRunOut:            willRunOut,
					ForfeitForecast:       floatPtr(forfeit),
					ResetsAt:              w.ResetsAt,
					SecondsToReset:        secLeft,
					ParentBlocked:         w.ParentBlocked,
				})
			}
		}
	}

	// Add upcoming resets to calendar.
	for _, r := range nowPayload.NextResets {
		kind := "window_reset"
		if r.PartialReset {
			kind = "partial_reset"
		}
		calendar = append(calendar, ResetCalendarEvent{
			ID:           fmt.Sprintf("cal_reset_%s_%s", r.AccountID, r.WindowID),
			Kind:         kind,
			AccountID:    r.AccountID,
			AccountLabel: r.AccountLabel,
			Provider:     r.Provider,
			WindowID:     r.WindowID,
			Model:        r.Model,
			Group:        r.Group,
			At:           r.ResetsAt,
			SecondsUntil: r.SecondsToReset,
			Summary:      fmt.Sprintf("%s · %s (%s) resets", r.AccountLabel, firstNonEmpty(r.Model, r.WindowID), firstNonEmpty(r.Group, "quota")),
		})
	}
	sort.Slice(calendar, func(i, j int) bool { return calendar[i].At < calendar[j].At })

	// 3. Per-Subscription Value Matrix across real subscription accounts.
	subValues = []SubscriptionValueRow{
		{
			AccountID:              "claude-personal",
			AccountLabel:           "Claude",
			Provider:               "anthropic",
			Plan:                   "max",
			MonthlyPriceUSD:        floatPtr(200.00),
			BillingDay:             intPtr(1),
			UtilizationPercent:     floatPtr(84.2),
			ForfeitedPercent:       floatPtr(15.8),
			LeftOnTableUSD:         floatPtr(31.60),
			ExhaustionSharePercent: floatPtr(4.5),
			HoursExhausted:         floatPtr(7.5),
			OverageSpendUSD:        floatPtr(629.34),
			PlanFit:                "well_matched",
			FitReason:              "84.2% average weekly capture with peak opus bursts spilling into extra_usage.",
			PeriodsAnalyzed:        4,
		},
		{
			AccountID:              "codex-personal",
			AccountLabel:           "Codex",
			Provider:               "openai",
			Plan:                   "pro",
			MonthlyPriceUSD:        floatPtr(200.00),
			BillingDay:             intPtr(12),
			UtilizationPercent:     floatPtr(98.5),
			ForfeitedPercent:       floatPtr(1.5),
			LeftOnTableUSD:         floatPtr(3.00),
			ExhaustionSharePercent: floatPtr(22.0),
			HoursExhausted:         floatPtr(36.0),
			OverageSpendUSD:        floatPtr(0),
			PlanFit:                "under_provisioned",
			FitReason:              "Primary weekly window reached 100% exhaustion 3.5 days before reset.",
			PeriodsAnalyzed:        4,
		},
		{
			AccountID:              "github-personal",
			AccountLabel:           "GitHub",
			Provider:               "github",
			Plan:                   "pro",
			MonthlyPriceUSD:        floatPtr(10.00),
			BillingDay:             intPtr(1),
			UtilizationPercent:     floatPtr(96.0),
			ForfeitedPercent:       floatPtr(4.0),
			LeftOnTableUSD:         floatPtr(0.40),
			ExhaustionSharePercent: floatPtr(0),
			HoursExhausted:         floatPtr(0),
			OverageSpendUSD:        floatPtr(0),
			PlanFit:                "under_provisioned",
			FitReason:              "Copilot premium requests (96.0%) and included AI credits (1,440 / 1,500) are near monthly cap before Dec 1 reset.",
			PeriodsAnalyzed:        3,
		},
		{
			AccountID:              "agy-personal",
			AccountLabel:           "Antigravity",
			Provider:               "antigravity",
			Plan:                   "pro",
			MonthlyPriceUSD:        floatPtr(40.00),
			BillingDay:             intPtr(15),
			UtilizationPercent:     floatPtr(48.0),
			ForfeitedPercent:       floatPtr(52.0),
			LeftOnTableUSD:         floatPtr(20.80),
			ExhaustionSharePercent: floatPtr(0),
			HoursExhausted:         floatPtr(0),
			OverageSpendUSD:        floatPtr(0),
			PlanFit:                "over_provisioned",
			FitReason:              "52% mean forfeitable capacity across 5h and weekly Gemini/3P model groups; shift overflow tasks here.",
			PeriodsAnalyzed:        4,
		},
		{
			AccountID:              "go-workspace",
			AccountLabel:           "OpenCode Go",
			Provider:               "opencode",
			Plan:                   "workspace",
			MonthlyPriceUSD:        floatPtr(50.00),
			BillingDay:             intPtr(5),
			UtilizationPercent:     floatPtr(74.0),
			ForfeitedPercent:       floatPtr(26.0),
			LeftOnTableUSD:         floatPtr(13.00),
			ExhaustionSharePercent: floatPtr(0),
			HoursExhausted:         floatPtr(0),
			OverageSpendUSD:        floatPtr(0),
			PlanFit:                "well_matched",
			FitReason:              "Steady rolling 5h, weekly, and monthly utilization with zero exhaustion events.",
			PeriodsAnalyzed:        4,
		},
		{
			AccountID:              "elevenlabs",
			AccountLabel:           "ElevenLabs",
			Provider:               "elevenlabs",
			Plan:                   "creator",
			MonthlyPriceUSD:        floatPtr(22.00),
			BillingDay:             intPtr(13),
			UtilizationPercent:     floatPtr(32.0),
			ForfeitedPercent:       floatPtr(68.0),
			LeftOnTableUSD:         floatPtr(14.96),
			ExhaustionSharePercent: floatPtr(0),
			HoursExhausted:         floatPtr(0),
			OverageSpendUSD:        floatPtr(0),
			PlanFit:                "over_provisioned",
			FitReason:              "116,256 of 131,000 monthly character credits remain unused.",
			PeriodsAnalyzed:        3,
		},
		{
			AccountID:              "higgsfield-subscription",
			AccountLabel:           "Higgsfield",
			Provider:               "higgsfield",
			Plan:                   "creator",
			MonthlyPriceUSD:        floatPtr(29.00),
			BillingDay:             intPtr(18),
			UtilizationPercent:     floatPtr(35.0),
			ForfeitedPercent:       floatPtr(65.0),
			LeftOnTableUSD:         floatPtr(18.85),
			ExhaustionSharePercent: floatPtr(0),
			HoursExhausted:         floatPtr(0),
			OverageSpendUSD:        floatPtr(0),
			PlanFit:                "over_provisioned",
			FitReason:              "65% of monthly generation credits forfeited at last cycle boundary.",
			PeriodsAnalyzed:        3,
		},
	}

	// 4. Spend Anomalies & Overage Watch.
	obsTime := now.Add(-2 * time.Hour).UTC().Format(time.RFC3339)
	anomalies = []SpendAnomaly{
		{
			ID:                "anom_claude_overage",
			AccountID:         "claude-personal",
			AccountLabel:      "Claude",
			Provider:          "anthropic",
			Model:             "claude-opus-4-7",
			CostKind:          "api_billed",
			ObservedAt:        obsTime,
			DailySpendUSD:     floatPtr(629.34),
			TrailingMedianUSD: floatPtr(18.50),
			ThresholdUSD:      floatPtr(45.00),
			Kind:              "overage_watch",
			WindowCausedBy:    "weekly_all",
			Summary:           "Extra-usage overage balance reached $629.34 while base weekly subscription window was under heavy parallel swarm load.",
		},
		{
			ID:                "anom_openrouter_spike",
			AccountID:         "gateway-openrouter",
			AccountLabel:      "OpenRouter",
			Provider:          "openrouter",
			Model:             "deepseek/deepseek-r1",
			CostKind:          "api_billed",
			ObservedAt:        obsTime,
			DailySpendUSD:     floatPtr(14.80),
			TrailingMedianUSD: floatPtr(4.10),
			ThresholdUSD:      floatPtr(9.50),
			Kind:              "spend_spike",
			WindowCausedBy:    "account_balance",
			Summary:           "Daily spend ($14.80) exceeded 2× trailing 28d median ($4.10), drawing prepaid balance down to $4.95 / $245.00.",
		},
	}

	if recs == nil {
		recs = []InsightRecommendation{}
	}
	if forecasts == nil {
		forecasts = []WindowForecast{}
	}
	if calendar == nil {
		calendar = []ResetCalendarEvent{}
	}

	return UsageIntelligencePayload{
		FetchedAt:         now.UTC().Format(time.RFC3339),
		InsightsSource:    "fixture",
		ResetsSource:      "fixture",
		Recommendations:   recs,
		Forecasts:         forecasts,
		Anomalies:         anomalies,
		SubscriptionValue: subValues,
		ResetsCalendar:    calendar,
	}
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}
