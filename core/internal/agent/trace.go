package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/hollis-labs/libs/core/util/classify"
	"github.com/hollis-labs/libs/core/util/decide"
	"github.com/hollis-labs/libs/core/util/embed"

	"github.com/hollis-labs/surface/core/internal/classifier"
	"github.com/hollis-labs/surface/core/internal/kernel"
	"github.com/hollis-labs/surface/core/internal/router"
	"github.com/hollis-labs/surface/core/internal/slots"
	"github.com/hollis-labs/surface/core/internal/wardentoken"
	"github.com/hollis-labs/surface/module"
)

// traceOn reports whether the context trace is kept (the live setting surface.agent_trace).
func (s *Service) traceOn() bool {
	if s.trace == nil {
		return false
	}
	// With no settings store (a host built without one), the trace is on, as by default.
	cfg := s.k.Config(kernel.CoreScope)
	return cfg == nil || cfg.Bool("agent_trace")
}

// record keeps one turn in the context trace and returns its id (0 when the trace is off).
func (s *Service) record(ctx context.Context, sit module.Situation, a slots.Assembly, routed *router.Route, fired []string, halted bool) int64 {
	if !s.traceOn() {
		return 0
	}
	view, mod := "", ""
	if sit.Focused != nil {
		view, mod = sit.Focused.View, sit.Focused.Module
	}
	open := make([]string, 0, len(sit.Windows))
	for _, w := range sit.Windows {
		open = append(open, w.View)
	}
	r := ""
	if routed != nil {
		r = routed.Rule + " -> " + routed.Kind + ":" + routed.Name
	}
	turn := slots.TurnFrom(sit.Input, view, mod, sit.Mode, sit.Layout, open, a, r, fired, halted)
	id, err := s.trace.Record(ctx, turn)
	if err != nil {
		s.log.Warn("context trace: turn not kept", "err", err)
		return id
	}
	s.classifyTurn(ctx, sit, id, turn)
	return id
}

// classifyTurn labels a recorded turn in shadow and keeps the result beside it, then learns
// the slot it was given (weakly: that is the selector's own guess). Nothing reads the
// result to decide anything yet.
func (s *Service) classifyTurn(ctx context.Context, sit module.Situation, id int64, turn slots.TraceTurn) {
	if s.cls == nil || id == 0 {
		return
	}
	res, err := s.cls.Classify(ctx, sit, s.k.Slots(), "turn:"+strconv.FormatInt(id, 10), turn.Text)
	if err != nil {
		s.log.Warn("classifier: turn not classified", "err", err)
		return
	}
	if raw, err := json.Marshal(res); err == nil {
		s.trace.SetClassified(ctx, id, raw)
	}
	s.cls.Learn(turn)
	if s.cls.RefitDue() {
		s.refitCentroids()
	}
	s.classifyAsync(sit, id, turn.Text)
	s.lastMu.Lock()
	s.lastText, s.lastAt = turn.Text, time.Now()
	s.lastMu.Unlock()
}

// setupAsync gives the classifier its async tier: the decider handed in, or the decision
// gateway the config file names. Without either there is none.
func (s *Service) setupAsync(cfg FileConfig, d decide.Decider, e classify.Embedder) {
	s.asyncCtx, s.asyncCancel = context.WithCancel(context.Background()) //nolint:gosec // G118: Close calls it
	cc := cfg.Classifier
	var minted *wardentoken.Source
	givenD, givenE := d, e
	if (d == nil || e == nil) && cc.GatewayURL != "" {
		token := ""
		switch {
		case cc.KeyFile != "" && cc.Warden != nil:
			s.log.Warn("classifier: the agent config sets both classifier.key_file and classifier.warden; set one. No async tier")
			return
		case cc.Warden != nil:
			w := cc.Warden
			if w.URL == "" || w.CredentialFile == "" || w.Audience == "" {
				s.log.Warn("classifier: classifier.warden needs url, credential_file and audience; no async tier")
				return
			}
			minted = &wardentoken.Source{URL: w.URL, CredentialFile: w.CredentialFile, Audience: w.Audience, Scopes: w.Scopes, Log: s.log}
		}
		if cc.KeyFile != "" {
			raw, err := os.ReadFile(cc.KeyFile) //nolint:gosec // the operator's own key file, named in their config
			if err != nil {
				s.log.Warn("classifier: the gateway's key file is not readable; no async tier", "err", err)
				return
			}
			token = strings.TrimSpace(string(raw))
		}
		switch cc.Dialect {
		case "", DialectLLMGateway:
			if d == nil && cc.DecideModel != "" {
				if c := decide.NewJEVClient(cc.GatewayURL, token); c != nil {
					d = c
				}
			}
			if e == nil && cc.EmbedModel != "" {
				if c := embed.NewOpenAIClient(cc.GatewayURL, token, cc.EmbedModel); c != nil {
					e = c
				}
			}
		case DialectTether:
			if d == nil && cc.DecideModel != "" {
				if c := decide.NewClient(cc.GatewayURL, token, "surface"); c != nil {
					d = c
				}
			}
			if e == nil && cc.EmbedModel != "" {
				if c := embed.NewClient(cc.GatewayURL, token, "surface", cc.EmbedModel); c != nil {
					e = c
				}
			}
		default:
			s.log.Warn("classifier: unknown gateway dialect; no async tier", "dialect", cc.Dialect)
			return
		}
	}
	if minted != nil {
		if c, ok := d.(*decide.Client); ok && c != nil && givenD == nil {
			c.HTTP = withToken(c.HTTP, minted)
		}
		if c, ok := e.(*embed.Client); ok && c != nil && givenE == nil {
			c.HTTP = withToken(c.HTTP, minted)
		}
	}
	if e != nil {
		s.cls.SetEmbedder(e)
		s.refitCentroids() // on the slots' descriptions now; again once the trace is learned
	}
	if d == nil {
		if e != nil {
			s.log.Info("classifier: async tier on (embeddings only)", "embedder", e.Name())
		}
		return
	}
	purpose := cc.Purpose
	if purpose == "" {
		purpose = "surface_classify"
	}
	s.cls.SetDecider(d, classify.DecisionOptions{Purpose: purpose, Model: cc.DecideModel, CallerID: "surface"})
	s.log.Info("classifier: async tier on", "decider", d.Name(), "model", cc.DecideModel)
}

// withToken gives a gateway client's requests a bearer token minted from Warden.
func withToken(hc *http.Client, src *wardentoken.Source) *http.Client {
	out := &http.Client{Timeout: 30 * time.Second}
	if hc != nil {
		cp := *hc
		out = &cp
	}
	out.Transport = &wardentoken.Transport{Source: src, Base: out.Transport}
	return out
}

// classifyAsync asks the async tier about a recorded turn, off the turn's path, and keeps
// its result beside the turn. A failure leaves the column empty.
func (s *Service) classifyAsync(sit module.Situation, id int64, text string) {
	if (!s.cls.HasAsync() && s.cls.Embedder() == nil) || s.asyncCtx == nil {
		return
	}
	all := s.k.Slots()
	s.asyncWG.Add(1)
	go func() {
		defer s.asyncWG.Done()
		ctx, cancel := context.WithTimeout(s.asyncCtx, asyncTimeout)
		defer cancel()
		res, err := s.cls.ClassifyAsync(ctx, sit, all, "turn:"+strconv.FormatInt(id, 10), text)
		if err != nil || ctx.Err() != nil {
			return
		}
		if len(res.Failed) > 0 {
			s.log.Debug("classifier: async judges failed", "turn", id, "failed", res.Failed)
		}
		if raw, err := json.Marshal(res); err == nil {
			s.trace.SetClassifiedAsync(ctx, id, raw)
			s.k.Events().Publish("agent.classified", map[string]any{"turn": id, "phase": res.Phase})
		}
	}()
}

// refitCentroids fits the slot centroid again in the background, one fit at a time; a
// request while one runs makes it run once more after, so nothing learned is left out.
func (s *Service) refitCentroids() {
	if s.cls.Embedder() == nil || s.asyncCtx == nil {
		return
	}
	s.refitAgain.Store(true)
	if !s.fitting.CompareAndSwap(false, true) {
		return
	}
	s.asyncWG.Add(1)
	go func() {
		defer s.asyncWG.Done()
		defer s.fitting.Store(false)
		for s.refitAgain.Swap(false) {
			ctx, cancel := context.WithTimeout(s.asyncCtx, 2*time.Minute)
			err := s.cls.FitCentroids(ctx, s.k.Slots())
			cancel()
			if err != nil {
				s.log.Warn("classifier: the slot centroid was not fitted; it votes nothing until it is", "err", err)
				return
			}
		}
	}()
}

// asyncTimeout bounds one async classification.
const asyncTimeout = 30 * time.Second

// trainClassifier teaches the classifier the slots of every turn traced before start, the
// owner's corrections counting most, and the hints they accepted. It runs once, at start;
// what arrives after start is learned as it arrives, so it is left out here.
func (s *Service) trainClassifier(ctx context.Context, start time.Time) {
	defer close(s.trained)
	if s.cls == nil || s.trace == nil {
		return
	}
	before := func(at string) bool {
		t, err := time.Parse(time.RFC3339Nano, at)
		return err == nil && t.Before(start)
	}
	n := 0
	err := s.trace.Each(ctx, func(t slots.TraceTurn) error {
		if before(t.At) {
			s.cls.Learn(t)
			n++
		}
		return nil
	})
	if err != nil {
		s.log.Warn("classifier: trace not read", "err", err)
		return
	}
	err = s.trace.EachFeedback(ctx, func(f slots.Feedback) error {
		if before(f.At) {
			s.learnFeedback(f)
		}
		return nil
	})
	if err != nil {
		s.log.Warn("classifier: hint feedback not read", "err", err)
	}
	s.log.Debug("classifier trained from the context trace", "turns", n)
	if s.cls.Embedder() != nil {
		s.refitCentroids()
	}
}

// signal keeps something that followed a turn, when the trace is on.
func (s *Service) signal(ctx context.Context, kind string, detail any) {
	if !s.traceOn() {
		return
	}
	actor := module.ActorOf(ctx)
	s.trace.Signal(ctx, kind, string(actor), detail)
	// An owner's pin right after a turn says which slot the turn wanted.
	if d, ok := detail.(map[string]string); ok && kind == "pin" && actor == module.ActorOwner && s.cls != nil {
		s.lastMu.Lock()
		text, at := s.lastText, s.lastAt
		s.lastMu.Unlock()
		if text != "" && time.Since(at) <= correctionWindow {
			s.cls.Correct(text, d["slot"])
		}
	}
}

// correctionWindow is how long after a turn an owner's pin still corrects it (the trace's
// own signal window).
const correctionWindow = 15 * time.Minute

// traceEvent turns the app's events into signals: where the owner went after a turn.
func (s *Service) traceEvent(ev module.Event) {
	if ev.Topic != "desk.focus" {
		return
	}
	var f struct {
		Window string `json:"window"`
		View   string `json:"view"`
		By     string `json:"by"`
	}
	if json.Unmarshal(ev.Data, &f) != nil || module.Actor(f.By).Delegate() || f.Window == "" {
		return
	}
	s.signal(module.WithActor(context.Background(), module.ActorOwner), "focus", map[string]string{"window": f.Window, "view": f.View})
}

// traceRoutes serves the trace: the newest turns, and every turn as JSON lines for training.
func (s *Service) traceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET "+Prefix+"/trace", func(w http.ResponseWriter, r *http.Request) {
		if s.trace == nil {
			reply(w, 200, []slots.TraceTurn{})
			return
		}
		n, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		turns, err := s.trace.Recent(r.Context(), n)
		if err != nil {
			fail(w, 500, err.Error())
			return
		}
		if turns == nil {
			turns = []slots.TraceTurn{}
		}
		reply(w, 200, map[string]any{"on": s.traceOn(), "turns": turns})
	})
	mux.HandleFunc("GET "+Prefix+"/trace/export", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Header().Set("Content-Disposition", `attachment; filename="context-trace.jsonl"`)
		if s.trace == nil {
			return
		}
		if err := s.trace.Export(r.Context(), w); err != nil {
			s.log.Warn("context trace: export failed", "err", err)
		}
	})
}

// learnFeedback learns an accepted hint: the owner said the label was right for the draft.
// A dismissed hint says only that the label was wrong, which these models cannot use yet;
// it is kept for later.
func (s *Service) learnFeedback(f slots.Feedback) {
	if !f.Accepted || s.cls == nil {
		return
	}
	switch f.Facet {
	case classifier.Slot:
		s.cls.Correct(f.Text, f.Label)
	case classifier.Act:
		s.cls.LearnAct(f.Text, f.Label)
	}
}

// emerging clusters the newest turns whose facet abstained, through the embedder: what the
// owner keeps asking that the classifier has no label for. Candidate labels, not answers.
func (s *Service) emerging(ctx context.Context, facet string, limit int) ([]classify.Group, int, error) {
	e := s.cls.Embedder()
	if e == nil {
		return nil, 0, errNoEmbedder
	}
	turns, err := s.trace.Recent(ctx, limit)
	if err != nil {
		return nil, 0, err
	}
	var items []classify.Item
	for _, t := range turns {
		if len(t.Classified) == 0 {
			continue
		}
		var r classify.Result
		if json.Unmarshal(t.Classified, &r) != nil {
			continue
		}
		if fr, ok := r.Facets[facet]; ok && fr.Abstained && strings.TrimSpace(t.Text) != "" {
			items = append(items, classify.Item{ID: strconv.FormatInt(t.ID, 10), Text: t.Text})
		}
	}
	if len(items) == 0 {
		return []classify.Group{}, 0, nil
	}
	texts := make([]string, len(items))
	for i, it := range items {
		texts[i] = it.Text
	}
	vecs, err := e.Embed(ctx, texts)
	if err != nil {
		return nil, len(items), err
	}
	for i := range items {
		items[i].Vector = vecs[i]
	}
	groups := classify.Emerging(items, 0, 0)
	if groups == nil {
		groups = []classify.Group{}
	}
	return groups, len(items), nil
}

var errNoEmbedder = errors.New("no embedder: set classifier.gateway_url and classifier.embed_model in the agent config")
