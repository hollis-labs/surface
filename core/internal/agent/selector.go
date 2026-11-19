package agent

import (
	"context"
	"strings"

	"github.com/hollis-labs/surface/core/internal/classifier"
	"github.com/hollis-labs/surface/core/internal/kernel"
	"github.com/hollis-labs/surface/core/internal/slots"
	"github.com/hollis-labs/surface/module"
)

// The live setting surface.agent_selector says who picks the slots: "focus" (the window in
// front, the default) or "classifier".
const (
	selectorFocus      = "focus"
	selectorClassifier = "classifier"
	// classifiedMinP is how sure the classifier must be to overrule the window in front.
	classifiedMinP = 0.7
)

// classifiedSelector is the focus selector, overruled by the classifier when the setting
// says so. The classifier leads only when the message has text, the owner has not pinned a
// slot, and the slot facet answered (it did not abstain) with p ≥ classifiedMinP for a slot
// other than the focus selector's. Otherwise the focus selector's choice stands. Either
// way the focus selector sees every situation, so its own state (the pin, the last view)
// stays right if the setting is switched back.
type classifiedSelector struct {
	focus *slots.FocusSelector
	cls   *classifier.Classifier
	mode  func() string // read at each selection: settings are live
}

// Select implements slots.Selector.
func (c *classifiedSelector) Select(ctx context.Context, sit module.Situation, all []kernel.BoundSlot) slots.Selection {
	base := c.focus.Select(ctx, sit, all)
	if c.mode() != selectorClassifier || strings.TrimSpace(sit.Input) == "" || base.Why == "pinned" {
		return base
	}
	res, err := c.cls.Classify(ctx, sit, all, "", sit.Input)
	if err != nil {
		return base
	}
	fr := res.Facets[classifier.Slot]
	if fr.Label == "" || fr.P < classifiedMinP || fr.Label == base.Primary().Name {
		return base
	}
	lead, ok := slots.Named(all, fr.Label)
	if !ok {
		return base
	}
	sel := slots.Selection{Slots: []kernel.BoundSlot{lead}, Why: "classified"}
	for _, s := range base.Slots {
		if s.Name != lead.Name {
			sel.Slots = append(sel.Slots, s)
		}
	}
	return sel
}

// selectorMode is the live surface.agent_selector setting.
func (s *Service) selectorMode() string {
	cfg := s.k.Config(kernel.CoreScope)
	if cfg == nil {
		return selectorFocus
	}
	if v := cfg.String("agent_selector"); v != "" {
		return v
	}
	return selectorFocus
}
