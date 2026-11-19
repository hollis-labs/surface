// Package classifier labels the owner's messages to the embedded agent: what the message
// asks for (the act facet: command, question, task, correction, approval, chat) and which
// slot it belongs to (the slot facet: the app's slots and general).
//
// It runs in shadow by default: the agent records each turn's classification beside the turn
// in the context trace. With the setting surface.agent_selector at "classifier", a sure
// slot answer overrules the window in front (the agent's classifiedSelector). The trace is also what it
// learns from: the slot a turn was given, and above all a slot the owner pinned after it,
// which says the choice was wrong. The replay report (Replay) says whether its answers beat
// the focus selector on that record (docs/agent.md).
//
// The judges are libs core/util/classify's: the pack's rules and priors (act.json, and slot
// rules made from each slot's views, modes and name), and a naive Bayes model per facet.
// All of them run in microseconds, so the same call serves a keystroke and a submit.
//
// It also labels tone (the affect facet: neutral, frustrated, urgent, positive) with libs'
// lexicon judge. With a decider set (JEV through a decision gateway), ClassifyAsync adds a
// model's answer to every facet; that is the async tier, run after the turn, off the path.
package classifier

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/hollis-labs/libs/core/util/classify"
	"github.com/hollis-labs/libs/core/util/decide"

	"github.com/hollis-labs/surface/core/internal/kernel"
	"github.com/hollis-labs/surface/core/internal/slots"
	"github.com/hollis-labs/surface/module"
)

//go:embed act.json
var actPack []byte

// The facets.
const (
	Act    = "act"
	Slot   = "slot"
	Affect = "affect"
)

// Weights of what the trace says about a turn's slot. The slot the selector gave is the
// selector's own guess, so it is a weak label; a pin by the owner is a correction.
const (
	weightChosen    = 0.25
	weightCorrected = 1
)

// Classifier is safe for concurrent use.
type Classifier struct {
	base classify.Pack
	act  *classify.Bayes
	slot *classify.Bayes

	mu      sync.Mutex
	eng     *classify.Engine
	slotID  string // the slot set the engine was built for
	decider decide.Decider
	dopts   classify.DecisionOptions
	// centroids are the async tier's embedding judges, one per facet with examples.
	centroids []*classify.Centroid
	emb       classify.Embedder
}

// SetEmbedder adds centroid judges on the act and affect facets at classify.Async, over e.
// They vote nothing until FitCentroids has embedded the facets' examples.
func (c *Classifier) SetEmbedder(e classify.Embedder) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.emb, c.eng, c.centroids = e, nil, nil
	if e == nil {
		return
	}
	for _, f := range []string{Act, Affect} {
		c.centroids = append(c.centroids, classify.NewCentroid("centroid:"+f, f, e, classify.CentroidOptions{Phase: classify.Async}))
	}
}

// Embedder is the embedder set, or nil.
func (c *Classifier) Embedder() classify.Embedder {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.emb
}

// FitCentroids embeds each centroid facet's examples (a network call through the embedder).
func (c *Classifier) FitCentroids(ctx context.Context, all []kernel.BoundSlot) error {
	eng, err := c.engine(all)
	if err != nil {
		return err
	}
	c.mu.Lock()
	cs := slices.Clone(c.centroids)
	c.mu.Unlock()
	for i, f := range []string{Act, Affect} {
		if i >= len(cs) {
			break
		}
		if err := cs[i].Fit(ctx, eng.Pack().Examples(f)); err != nil {
			return err
		}
	}
	return nil
}

// SetDecider turns on the async tier: d is asked about every facet at classify.Async.
// A nil d turns it off.
func (c *Classifier) SetDecider(d decide.Decider, o classify.DecisionOptions) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.decider, c.dopts, c.eng = d, o, nil
}

// HasAsync reports whether a decider is set.
func (c *Classifier) HasAsync() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.decider != nil
}

// New returns a classifier with the built-in act pack and empty models seeded from its
// examples.
func New() (*Classifier, error) {
	p, err := classify.ParsePack(actPack)
	if err != nil {
		return nil, err
	}
	c := &Classifier{base: p,
		act:  classify.NewBayes("bayes:act", Act, classify.BayesOptions{MinDocs: 10}),
		slot: classify.NewBayes("bayes:slot", Slot, classify.BayesOptions{MinDocs: 20, MaxWeight: 2})}
	comp, err := classify.Compile(p)
	if err != nil {
		return nil, fmt.Errorf("classifier: act pack: %w", err)
	}
	c.act.LearnAll(comp.Examples(Act))
	return c, nil
}

// Classify labels text in the situation. all is the app's slots.
func (c *Classifier) Classify(ctx context.Context, sit module.Situation, all []kernel.BoundSlot, id, text string) (classify.Result, error) {
	eng, err := c.engine(all)
	if err != nil {
		return classify.Result{}, err
	}
	return eng.Classify(ctx, classify.Input{ID: id, Text: text, Context: Context(sit), Phase: classify.Submit}), nil
}

// ClassifyAsync is Classify with the async tier: the decider's answer joins the rest. It
// makes a network call; run it off the turn's path.
func (c *Classifier) ClassifyAsync(ctx context.Context, sit module.Situation, all []kernel.BoundSlot, id, text string) (classify.Result, error) {
	eng, err := c.engine(all)
	if err != nil {
		return classify.Result{}, err
	}
	return eng.Classify(ctx, classify.Input{ID: id, Text: text, Context: Context(sit), Phase: classify.Async}), nil
}

// engine is built once per slot set: the act pack with a slot facet over the slots.
func (c *Classifier) engine(all []kernel.BoundSlot) (*classify.Engine, error) {
	names := make([]string, 0, len(all))
	for _, s := range all {
		names = append(names, s.Name)
	}
	id := strings.Join(names, "\x00")
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.eng != nil && c.slotID == id {
		return c.eng, nil
	}
	layer := classify.Pack{Facets: []classify.FacetSpec{classify.AffectFacet()}}
	extra := []classify.Judge{c.act, classify.NewAffect("affect", Affect)}
	if sf, ok := SlotFacet(all); ok {
		layer.Facets = append(layer.Facets, sf)
		extra = append(extra, c.slot)
	}
	comp, err := classify.Compile(classify.Layer(c.base, layer))
	if err != nil {
		return nil, fmt.Errorf("classifier: %w", err)
	}
	for _, cj := range c.centroids {
		extra = append(extra, cj)
	}
	if c.decider != nil {
		dj, derr := classify.NewDecision("decide", c.decider, comp, c.dopts)
		if derr != nil {
			return nil, fmt.Errorf("classifier: %w", derr)
		}
		extra = append(extra, dj)
	}
	eng, err := classify.NewEngine(comp, extra...)
	if err != nil {
		return nil, err
	}
	c.eng, c.slotID = eng, id
	return eng, nil
}

// SlotFacet is the slot facet for a set of slots: one label per slot and general, priors
// from what is in front (a view the slot names, the module behind the view, a mode the
// slot names) and text rules from the slot's and its module's names. With no slot of a
// module's own there is nothing to choose and no facet.
func SlotFacet(all []kernel.BoundSlot) (classify.FacetSpec, bool) {
	f := classify.FacetSpec{Name: Slot, About: "Which kind of work the message belongs to.", MinP: 0.5, MinMargin: 0.1,
		Labels: []classify.LabelSpec{{Name: slots.General.Name, About: slots.General.About}},
		Rules: []classify.RuleSpec{{ID: "general:nothing-in-front", Label: slots.General.Name,
			When: []classify.Cond{{Field: "view", Op: classify.OpAbsent}}, Weight: 0.5}}}
	seen := map[string]bool{slots.General.Name: true}
	for _, s := range all {
		if s.Name == "" || seen[s.Name] {
			continue
		}
		seen[s.Name] = true
		f.Labels = append(f.Labels, classify.LabelSpec{Name: s.Name, About: s.About})
		if len(s.Views) > 0 {
			f.Rules = append(f.Rules, classify.RuleSpec{ID: s.Name + ":view", Label: s.Name,
				When: []classify.Cond{{Field: "view", Op: classify.OpIn, Values: slices.Clone(s.Views)}}, Weight: 2})
		} else if s.Module != "" {
			f.Rules = append(f.Rules, classify.RuleSpec{ID: s.Name + ":module", Label: s.Name,
				When: []classify.Cond{{Field: "module", Op: classify.OpEq, Value: s.Module}}, Weight: 1.5})
		}
		if len(s.Modes) > 0 {
			f.Rules = append(f.Rules, classify.RuleSpec{ID: s.Name + ":mode", Label: s.Name,
				When: []classify.Cond{{Field: "mode", Op: classify.OpIn, Values: slices.Clone(s.Modes)}}, Weight: 1})
		}
		if words := nameWords(s.Name, s.Module); len(words) > 0 {
			f.Rules = append(f.Rules, classify.RuleSpec{ID: s.Name + ":named", Label: s.Name, Words: words, Weight: 1.5})
		}
	}
	return f, len(f.Labels) > 1
}

// nameWords are the words a message would use to name a slot: its name and its module's,
// whole and in parts, three letters or more.
func nameWords(names ...string) []string {
	var out []string
	add := func(w string) {
		w = strings.ToLower(strings.TrimSpace(w))
		if len([]rune(w)) >= 3 && !slices.Contains(out, w) {
			out = append(out, w)
		}
	}
	for _, n := range names {
		add(n)
		for _, part := range strings.FieldsFunc(n, func(r rune) bool { return r == '.' || r == '_' || r == '-' || r == ' ' }) {
			add(part)
		}
	}
	return out
}

// Context is the situation as flat classifier fields.
func Context(sit module.Situation) map[string]string {
	ctx := map[string]string{}
	set := func(k, v string) {
		if v != "" {
			ctx[k] = v
		}
	}
	if f := sit.Focused; f != nil {
		set("view", f.View)
		set("module", f.Module)
		set("kind", f.Kind)
	}
	set("mode", sit.Mode)
	set("layout", sit.Layout)
	return ctx
}

// SlotLabel is what a traced turn says its slot should have been: the slot the owner pinned
// after it (a correction), or else the slot it was given (the selector's own guess, weakly).
func SlotLabel(t slots.TraceTurn) (label string, weight float64, corrected bool) {
	for _, s := range t.Signals {
		if s.Kind != "pin" || s.Actor != string(module.ActorOwner) {
			continue
		}
		var d struct {
			Slot string `json:"slot"`
		}
		if json.Unmarshal(s.Detail, &d) == nil && d.Slot != "" {
			return d.Slot, weightCorrected, true
		}
	}
	return t.Slot, weightChosen, false
}

// Learn adds one traced turn to the slot model.
func (c *Classifier) Learn(t slots.TraceTurn) {
	if strings.TrimSpace(t.Text) == "" || t.Halted {
		return
	}
	if label, w, _ := SlotLabel(t); label != "" {
		c.slot.Learn(t.Text, label, w)
	}
}

// Correct adds an owner's correction of the newest turn's slot.
func (c *Classifier) Correct(text, slot string) {
	if strings.TrimSpace(text) != "" && slot != "" {
		c.slot.Learn(text, slot, weightCorrected)
	}
}

// LearnAct adds an owner's confirmation of a message's act.
func (c *Classifier) LearnAct(text, act string) {
	if strings.TrimSpace(text) != "" && act != "" {
		c.act.Learn(text, act, weightCorrected)
	}
}

// ActDocs is how much the act model has learned.
func (c *Classifier) ActDocs() float64 { return c.act.Docs() }

// SlotDocs is how much the slot model has learned.
func (c *Classifier) SlotDocs() float64 { return c.slot.Docs() }
