package classifier

import (
	"context"

	"github.com/hollis-labs/surface/core/internal/kernel"
	"github.com/hollis-labs/surface/core/internal/slots"
	"github.com/hollis-labs/surface/module"
)

// ReplayReport is how a fresh classifier would have done on the trace, against the focus
// selector, turn by turn in order: each turn is classified before it is learned, so every
// answer is a prediction about a turn the model has not seen.
//
// The gold slot is the one the owner pinned after the turn, else the one the selector gave.
// So on uncorrected turns the selector is right by definition and Agreement says how often
// the classifier agrees with it; Corrected is the only place either can be scored fairly.
type ReplayReport struct {
	Turns int `json:"turns"`
	// Agreement is how often the classifier's slot (answered or not) matched the selector's.
	Agreement float64 `json:"agreement"`
	// Answered is how often the slot facet did not abstain.
	Answered float64 `json:"answered"`
	// Corrected counts turns the owner re-pinned; on them the selector was wrong.
	Corrected int `json:"corrected"`
	// CorrectedHits is how many of those the classifier's top slot got right.
	CorrectedHits int `json:"corrected_hits"`
	// Acts counts the act facet's answers ("" for an abstention).
	Acts map[string]int `json:"acts"`
	// Misses are corrected turns the classifier got wrong too, up to 20.
	Misses []ReplayMiss `json:"misses,omitempty"`
}

// ReplayMiss is a corrected turn the classifier also got wrong.
type ReplayMiss struct {
	Text     string  `json:"text"`
	Selector string  `json:"selector"`
	Owner    string  `json:"owner"`
	Got      string  `json:"got"`
	P        float64 `json:"p"`
}

// Replay runs a fresh classifier over turns, oldest first.
func Replay(ctx context.Context, turns []slots.TraceTurn, all []kernel.BoundSlot) (ReplayReport, error) {
	c, err := New()
	if err != nil {
		return ReplayReport{}, err
	}
	rep := ReplayReport{Acts: map[string]int{}}
	var agree, answered int
	for _, t := range turns {
		if t.Text == "" || t.Halted {
			continue
		}
		sit := module.Situation{Mode: t.Mode, Layout: t.Layout}
		if t.View != "" || t.Module != "" {
			sit.Focused = &module.WindowContext{View: t.View, Module: t.Module}
		}
		res, err := c.Classify(ctx, sit, all, "", t.Text)
		if err != nil {
			return rep, err
		}
		rep.Turns++
		rep.Acts[res.Label(Act)]++
		fr := res.Facets[Slot]
		if !fr.Abstained {
			answered++
		}
		if fr.Top == t.Slot {
			agree++
		}
		if gold, _, corrected := SlotLabel(t); corrected {
			rep.Corrected++
			if fr.Top == gold {
				rep.CorrectedHits++
			} else if len(rep.Misses) < 20 {
				rep.Misses = append(rep.Misses, ReplayMiss{Text: t.Text, Selector: t.Slot, Owner: gold, Got: fr.Top, P: fr.P})
			}
		}
		c.Learn(t)
	}
	if rep.Turns > 0 {
		rep.Agreement = float64(agree) / float64(rep.Turns)
		rep.Answered = float64(answered) / float64(rep.Turns)
	}
	return rep, nil
}
