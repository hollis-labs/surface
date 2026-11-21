package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/hollis-labs/surface/module/tetherlink"
)

// Paid sources Tether already meters: the design, audio and video suites' spend logs and
// the Tripo credit balance. Costs are the suites' own estimates; nothing here is billed data.

// UsageSpend is one provider's spend in a window.
type UsageSpend struct {
	Provider string  `json:"provider"`
	Calls    int64   `json:"calls"`
	Failed   int64   `json:"failed"`
	CostUSD  float64 `json:"cost_usd"`
}

// UsageEvent is one recent charge.
type UsageEvent struct {
	ID     string   `json:"id"`
	At     string   `json:"at"`
	What   string   `json:"what"`
	Amount *float64 `json:"amount,omitempty"`
	Unit   string   `json:"unit,omitempty"`
	Note   string   `json:"note,omitempty"`
	OK     bool     `json:"ok"`
}

const (
	suiteDays      = 30
	usageRecentMax = 10
	tripoUsageMax  = 50
	// warnAt is the share of a limit at which a card turns to warning.
	warnAt = 0.8
)

// suiteReader reads a media suite's <suite>_budget: 30-day spend per provider, today's
// spend and the optional daily cap.
func suiteReader(tool string) func(context.Context, tetherlink.Caller, time.Time) (UsageCard, UsageDetail, error) {
	return func(ctx context.Context, link tetherlink.Caller, now time.Time) (UsageCard, UsageDetail, error) {
		raw, err := tetherlink.CallAll(ctx, link, tool, map[string]any{"days": suiteDays})
		if err != nil {
			return UsageCard{}, UsageDetail{}, err
		}
		var data struct {
			Days       int      `json:"days"`
			TotalUSD   *float64 `json:"total_usd"`
			CapUSD     *float64 `json:"daily_cap_usd"`
			TodayUSD   *float64 `json:"spent_today_usd"`
			Remaining  *float64 `json:"remaining_today_usd"`
			ByProvider map[string]struct {
				Calls   int64   `json:"calls"`
				Failed  int64   `json:"failed"`
				CostUSD float64 `json:"cost_usd"`
			} `json:"by_provider"`
			Recent []struct {
				TS       string   `json:"ts"`
				Provider string   `json:"provider"`
				Model    string   `json:"model"`
				CostUSD  *float64 `json:"cost_usd"`
				Note     string   `json:"note"`
				OK       bool     `json:"ok"`
				JobID    string   `json:"job_id"`
			} `json:"recent"`
			Note string `json:"note"`
		}
		if json.Unmarshal([]byte(raw), &data) != nil || data.TotalUSD == nil {
			return UsageCard{}, UsageDetail{}, fmt.Errorf("%s returned an unreadable result", tool)
		}
		d := UsageDetail{Spend: []UsageSpend{}, Recent: []UsageEvent{}}
		for p, v := range data.ByProvider {
			d.Spend = append(d.Spend, UsageSpend{Provider: p, Calls: v.Calls, Failed: v.Failed, CostUSD: v.CostUSD})
		}
		sort.Slice(d.Spend, func(i, j int) bool {
			if d.Spend[i].CostUSD != d.Spend[j].CostUSD {
				return d.Spend[i].CostUSD > d.Spend[j].CostUSD
			}
			return d.Spend[i].Calls > d.Spend[j].Calls
		})
		if len(d.Spend) > usageTopN {
			d.Spend = d.Spend[:usageTopN]
		}
		// The suites log oldest first; show the newest.
		recent := data.Recent
		sort.SliceStable(recent, func(i, j int) bool { return recent[i].TS > recent[j].TS })
		for i, r := range recent {
			if i >= usageRecentMax {
				break
			}
			what := r.Provider
			if r.Model != "" {
				what += " · " + r.Model
			}
			d.Recent = append(d.Recent, UsageEvent{ID: r.JobID, At: r.TS, What: what, Amount: r.CostUSD, Unit: "USD", Note: r.Note, OK: r.OK})
		}
		if data.Note != "" {
			d.Notes = append(d.Notes, data.Note)
		}
		day := dayStart(now)
		card := UsageCard{Status: usageOK, Used: data.TodayUSD, Limit: data.CapUSD, Unit: "USD today",
			Window: "day", Observed: now.UTC().Format(time.RFC3339),
			Summary: fmt.Sprintf("$%.2f over the last %d days across %s. Costs are the suite's estimates.", *data.TotalUSD, suiteDays, plural(len(data.ByProvider), "provider"))}
		if data.CapUSD == nil {
			card.Gap = "No daily cap is set, so there is no limit to measure against."
		} else {
			card.ResetAt = day.AddDate(0, 0, 1).Format(time.RFC3339)
			card.Status = against(data.TodayUSD, *data.CapUSD)
			d.Notes = append(d.Notes, "The cap's reset is assumed to be 00:00 UTC; the suite does not report its day boundary.")
		}
		return card, d, nil
	}
}

// against grades used against a positive limit.
func against(used *float64, limit float64) string {
	switch {
	case used == nil:
		return usageUnknown
	case limit <= 0 || *used >= limit:
		return usageExhausted
	case *used >= warnAt*limit:
		return usageWarning
	}
	return usageOK
}

func dayStart(now time.Time) time.Time {
	now = now.UTC()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
}

// readTripo reads the credit balance and the recent ledger. Tripo credits are prepaid: there
// is no limit or reset, only what remains.
func readTripo(ctx context.Context, link tetherlink.Caller, now time.Time) (UsageCard, UsageDetail, error) {
	raw, err := tetherlink.CallAll(ctx, link, "tripo_balance", map[string]any{})
	if err != nil {
		return UsageCard{}, UsageDetail{}, err
	}
	var bal struct {
		Balance *float64 `json:"balance"`
		Frozen  *float64 `json:"frozen"`
	}
	if json.Unmarshal([]byte(raw), &bal) != nil || bal.Balance == nil {
		return UsageCard{}, UsageDetail{}, fmt.Errorf("tripo_balance returned an unreadable result")
	}
	d := UsageDetail{Recent: []UsageEvent{}}
	card := UsageCard{Status: usageOK, Remaining: bal.Balance, Unit: "credits", Observed: now.UTC().Format(time.RFC3339)}
	var biggest float64
	raw, err = tetherlink.CallAll(ctx, link, "tripo_usage", map[string]any{"limit": tripoUsageMax})
	if err != nil {
		d.Warnings = append(d.Warnings, "Recent Tripo charges unavailable: "+err.Error())
	} else {
		var led struct {
			Total   *float64 `json:"total_credits"`
			Entries int      `json:"returned_entries"`
			Usage   []struct {
				Type    string   `json:"type"`
				TaskID  string   `json:"task_id"`
				Status  string   `json:"status"`
				At      string   `json:"created_at"`
				Credits *float64 `json:"credits_consumed"`
			} `json:"usage"`
		}
		if json.Unmarshal([]byte(raw), &led) != nil {
			d.Warnings = append(d.Warnings, "Recent Tripo charges were unreadable.")
		} else {
			sort.SliceStable(led.Usage, func(i, j int) bool { return led.Usage[i].At > led.Usage[j].At })
			for i, u := range led.Usage {
				if u.Credits != nil && *u.Credits > biggest {
					biggest = *u.Credits
				}
				if i < usageRecentMax {
					d.Recent = append(d.Recent, UsageEvent{ID: u.TaskID, At: u.At, What: strings.ReplaceAll(u.Type, "_", " "), Amount: u.Credits, Unit: "credits", Note: u.Status, OK: u.Status == "success"})
				}
			}
			if led.Total != nil {
				card.Summary = fmt.Sprintf("%g credits spent over the last %d charges.", *led.Total, len(led.Usage))
			}
		}
	}
	switch {
	case *bal.Balance <= 0:
		card.Status = usageExhausted
	case biggest > 0 && *bal.Balance < biggest:
		card.Status = usageWarning
		card.Gap = fmt.Sprintf("Fewer credits remain than the largest recent charge (%g).", biggest)
	}
	if bal.Frozen != nil && *bal.Frozen > 0 {
		d.Notes = append(d.Notes, fmt.Sprintf("%g credits are frozen by tasks in progress.", *bal.Frozen))
	}
	d.Notes = append(d.Notes, "Tripo credits are prepaid: there is no limit or reset, only the remaining balance.")
	return card, d, nil
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}
