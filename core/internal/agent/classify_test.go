package agent

import (
	"context"
	"encoding/json"
	"hash/fnv"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/hollis-labs/libs/core/util/classify"
	"github.com/hollis-labs/libs/core/util/decide"

	"github.com/hollis-labs/surface/core/internal/slots"
	"github.com/hollis-labs/surface/module"
)

func TestEveryTracedTurnIsClassifiedInShadow(t *testing.T) {
	s, _, _, _ := setup(t, Options{})
	ctx := context.Background()
	before := s.cls.SlotDocs()

	if _, err := s.Send(ctx, "what is in the scratch pad?", "", ""); err != nil {
		t.Fatal(err)
	}
	turns, err := s.trace.Recent(ctx, 1)
	if err != nil || len(turns) != 1 {
		t.Fatalf("turns %v %v", turns, err)
	}
	var res classify.Result
	if err := json.Unmarshal(turns[0].Classified, &res); err != nil {
		t.Fatalf("classified %q: %v", turns[0].Classified, err)
	}
	if res.Label("act") != "question" || res.Phase != classify.Submit || !strings.HasPrefix(res.InputID, "turn:") {
		t.Errorf("act: %+v", res.Facets["act"])
	}
	slot, ok := res.Facets["slot"]
	if !ok || len(slot.Probabilities) != 3 { // general, notes, scratch
		t.Fatalf("slot facet: %+v", slot)
	}
	// Shadow: the selector's choice stands, whatever the classifier thought.
	if turns[0].Slot != "notes" {
		t.Errorf("the classifier changed the slot: %q", turns[0].Slot)
	}

	// The owner's pin after the turn is a correction the slot model learns.
	learned := s.cls.SlotDocs()
	if learned <= before {
		t.Fatalf("the turn's slot was not learned: %v -> %v", before, learned)
	}
	raw, _ := json.Marshal(map[string]string{"slot": "scratch"})
	if _, err := s.callOwnCtx(module.WithActor(ctx, module.ActorOwner), "context_switch", raw); err != nil {
		t.Fatal(err)
	}
	if got := s.cls.SlotDocs(); got != learned+1 {
		t.Errorf("the owner's pin was not learned as a correction: %v -> %v", learned, got)
	}
	// The agent's own pin is not a correction.
	if _, err := s.callOwnCtx(module.WithActor(ctx, module.ActorAgent), "context_switch", raw); err != nil {
		t.Fatal(err)
	}
	if got := s.cls.SlotDocs(); got != learned+1 {
		t.Errorf("the agent's pin was learned: %v", got)
	}
}

func TestClassifyRouteRecordsNothing(t *testing.T) {
	s, _, _, _ := setup(t, Options{})
	mux := http.NewServeMux()
	s.Mount(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/api/agent/classify", "application/json", strings.NewReader(`{"text":"/layout focus"}`))
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("classify: %v %v", err, resp.Status)
	}
	defer func() { _ = resp.Body.Close() }()
	var res classify.Result
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatal(err)
	}
	if res.Label("act") != "command" {
		t.Errorf("act: %+v", res.Facets["act"])
	}
	if turns, _ := s.trace.Recent(context.Background(), 10); len(turns) != 0 {
		t.Errorf("a draft was traced: %+v", turns)
	}
}

func TestAsyncTierKeepsTheDecidersAnswerBesideTheTurn(t *testing.T) {
	asked := make(chan decide.Request, 1)
	d := decide.Scripted{ID: "jev", Fn: func(_ context.Context, req decide.Request) (decide.Response, error) {
		asked <- req
		ans := map[string]decide.Answer{}
		for _, q := range req.Questions {
			probs := map[string]float64{}
			for _, o := range q.Options {
				probs[o.Key] = 0.01
			}
			want := map[string]string{"act": "task", "affect": "frustrated", "slot": "scratch"}[q.Key]
			probs[want] = 0.97
			ans[q.Key] = decide.Answer{Type: decide.Choice, Choice: want, Probabilities: probs}
		}
		return decide.Response{Model: "jev-test", Answers: ans}, nil
	}}
	s, _, _, _ := setup(t, Options{Decider: d})
	ctx := context.Background()
	if _, err := s.Send(ctx, "ugh, put this in scratch again", "", ""); err != nil {
		t.Fatal(err)
	}
	req := <-asked
	if req.Purpose != "surface_classify" || len(req.Questions) != 3 {
		t.Fatalf("request %+v", req)
	}
	s.asyncWG.Wait()
	turns, _ := s.trace.Recent(ctx, 1)
	var sub, async classify.Result
	if err := json.Unmarshal(turns[0].Classified, &sub); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(turns[0].ClassifiedAsync, &async); err != nil {
		t.Fatalf("async %q: %v", turns[0].ClassifiedAsync, err)
	}
	if sub.Phase != classify.Submit || async.Phase != classify.Async {
		t.Fatalf("phases %s %s", sub.Phase, async.Phase)
	}
	if async.Facets["slot"].Top != "scratch" || async.Facets["affect"].Top != "frustrated" {
		t.Errorf("async answer: slot %+v affect %+v", async.Facets["slot"], async.Facets["affect"])
	}
	for _, v := range sub.Facets["act"].Votes {
		if v.Judge == "decide" {
			t.Fatal("the decider voted at submit")
		}
	}
}

func TestNoAsyncTierWithoutADecider(t *testing.T) {
	s, _, _, _ := setup(t, Options{})
	if _, err := s.Send(context.Background(), "hello", "", ""); err != nil {
		t.Fatal(err)
	}
	s.asyncWG.Wait()
	turns, _ := s.trace.Recent(context.Background(), 1)
	if len(turns[0].ClassifiedAsync) != 0 {
		t.Fatalf("async result without a decider: %s", turns[0].ClassifiedAsync)
	}
	if _, ok := mustResult(t, turns[0].Classified).Facets["affect"]; !ok {
		t.Fatal("no affect facet")
	}
}

func mustResult(t *testing.T, raw json.RawMessage) classify.Result {
	t.Helper()
	var r classify.Result
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestHintFeedbackIsKeptLearnedAndAnAcceptedSlotPins(t *testing.T) {
	s, _, _, _ := setup(t, Options{})
	mux := http.NewServeMux()
	s.Mount(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	post := func(body string) int {
		resp, err := http.Post(srv.URL+"/api/agent/classify/feedback", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		return resp.StatusCode
	}
	slotsBefore, actBefore := s.cls.SlotDocs(), s.cls.ActDocs()
	if code := post(`{"text":"jot this down","facet":"slot","label":"scratch","accepted":true}`); code != 200 {
		t.Fatalf("slot feedback: %d", code)
	}
	if s.Preview(context.Background()).Slot != "scratch" {
		t.Error("an accepted slot hint did not pin the slot")
	}
	if code := post(`{"text":"what's next?","facet":"act","label":"question","accepted":true}`); code != 200 {
		t.Fatalf("act feedback: %d", code)
	}
	if code := post(`{"text":"x","facet":"act","label":"task","accepted":false}`); code != 200 {
		t.Fatalf("dismissal: %d", code)
	}
	if s.cls.SlotDocs() != slotsBefore+1 || s.cls.ActDocs() != actBefore+1 {
		t.Errorf("learned: slot %v -> %v, act %v -> %v", slotsBefore, s.cls.SlotDocs(), actBefore, s.cls.ActDocs())
	}
	if code := post(`{"text":"x","facet":"slot","label":"nope","accepted":true}`); code != 400 {
		t.Errorf("unknown slot: %d", code)
	}
	if code := post(`{"text":"x"}`); code != 400 {
		t.Errorf("no facet: %d", code)
	}
	var n int
	_ = s.trace.EachFeedback(context.Background(), func(slots.Feedback) error { n++; return nil })
	if n != 3 {
		t.Errorf("feedback rows %d", n)
	}
	// No turn yet, so nothing was signaled as a correction.
	if turns, _ := s.trace.Recent(context.Background(), 10); len(turns) != 0 {
		t.Errorf("turns %+v", turns)
	}
}

// wordsEmbedder puts each word in one of 32 dimensions: texts that share words are near.
type wordsEmbedder struct{ calls atomic.Int32 }

func (*wordsEmbedder) Name() string { return "words" }
func (w *wordsEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	w.calls.Add(1)
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v := make([]float32, 32)
		for _, word := range classify.Tokens(t) {
			h := fnv.New32a()
			_, _ = h.Write([]byte(word))
			v[h.Sum32()%32]++
		}
		out[i] = v
	}
	return out, nil
}

func TestCentroidsVoteAtAsyncOnceFitted(t *testing.T) {
	emb := &wordsEmbedder{}
	s, _, _, _ := setup(t, Options{Embedder: emb})
	s.asyncWG.Wait() // the centroids are fitted in the background at start
	if emb.calls.Load() == 0 {
		t.Fatal("the centroids were not fitted")
	}
	ctx := context.Background()
	if _, err := s.Send(ctx, "how do I pin a slot?", "", ""); err != nil {
		t.Fatal(err)
	}
	s.asyncWG.Wait()
	turns, _ := s.trace.Recent(ctx, 1)
	async := mustResult(t, turns[0].ClassifiedAsync)
	var centroid bool
	for _, v := range async.Facets["act"].Votes {
		centroid = centroid || v.Judge == "centroid:act"
	}
	if !centroid {
		t.Fatalf("no centroid vote at async: %+v", async.Facets["act"].Votes)
	}
	for _, v := range mustResult(t, turns[0].Classified).Facets["act"].Votes {
		if v.Judge == "centroid:act" {
			t.Fatal("a centroid voted at submit")
		}
	}
}

func TestEmergingClustersWhatTheClassifierCouldNotLabel(t *testing.T) {
	s, _, _, _ := setup(t, Options{Embedder: &wordsEmbedder{}})
	s.asyncWG.Wait()
	mux := http.NewServeMux()
	s.Mount(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	ctx := context.Background()
	abstained, _ := json.Marshal(classify.Result{Facets: map[string]classify.FacetResult{"act": {Facet: "act", Abstained: true}}})
	for _, text := range []string{
		"book the standup for tomorrow morning please",
		"book the standup for friday morning please",
		"book the standup for monday morning please",
		"book the standup for tomorrow morning please", // a copy: counted once
		"the build log says nothing useful today",
	} {
		if _, err := s.Send(ctx, text, "", ""); err != nil {
			t.Fatal(err)
		}
		turns, _ := s.trace.Recent(ctx, 1)
		s.trace.SetClassified(ctx, turns[0].ID, abstained)
	}
	s.asyncWG.Wait()
	resp, err := http.Get(srv.URL + "/api/agent/classify/emerging?facet=act")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("emerging: %v %v", err, resp.Status)
	}
	defer func() { _ = resp.Body.Close() }()
	var out struct {
		Abstained int              `json:"abstained"`
		Groups    []classify.Group `json:"groups"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out.Abstained != 5 || len(out.Groups) != 1 || out.Groups[0].Distinct != 3 || len(out.Groups[0].Members) != 4 ||
		!strings.Contains(out.Groups[0].Text, "standup") {
		t.Fatalf("emerging %+v", out)
	}
}

func TestEmergingNeedsAnEmbedder(t *testing.T) {
	s, _, _, _ := setup(t, Options{})
	mux := http.NewServeMux()
	s.Mount(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/api/agent/classify/emerging")
	if err != nil || resp.StatusCode != 409 {
		t.Fatalf("without an embedder: %v %v", err, resp.Status)
	}
	_ = resp.Body.Close()
}

// fakeLLMGateway answers JEV's System-1 API and OpenAI-compatible embeddings, as llm-gateway
// relays them, and records what it was asked.
func fakeLLMGateway(t *testing.T, key string) (*httptest.Server, *atomic.Int32, *atomic.Int32) {
	t.Helper()
	var decides, embeds atomic.Int32
	emb := &wordsEmbedder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+key {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var body map[string]json.RawMessage
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch r.URL.Path {
		case "/v1/systemone":
			decides.Add(1)
			var qs map[string]struct {
				Criteria map[string]any `json:"criteria"`
			}
			_ = json.Unmarshal(body["questions"], &qs)
			answers := map[string]any{}
			for k, q := range qs {
				probs := map[string]float64{}
				first := ""
				for opt := range q.Criteria {
					probs[opt] = 0.01
					if first == "" || opt < first {
						first = opt
					}
				}
				probs[first] = 0.9
				answers[k] = map[string]any{"type": "choice", "choice": first, "probabilities": probs}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"model": "jev-1.13.0", "answers": answers})
		case "/v1/embeddings":
			embeds.Add(1)
			var in []string
			_ = json.Unmarshal(body["input"], &in)
			vecs, _ := emb.Embed(r.Context(), in)
			var data []map[string]any
			for i, v := range vecs {
				data = append(data, map[string]any{"index": i, "embedding": v})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &decides, &embeds
}

func TestTheConfigFileTurnsOnTheLLMGatewayTier(t *testing.T) {
	key := strings.Repeat("g", 12) + "-fixture"
	srv, decides, embeds := fakeLLMGateway(t, key)
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "gateway.key")
	if err := os.WriteFile(keyFile, []byte(key+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _ := json.Marshal(map[string]any{"classifier": map[string]string{
		"gateway_url": srv.URL, "key_file": keyFile, "decide_model": "jev", "embed_model": "embed-local"}})
	path := filepath.Join(dir, "agent.json")
	if err := os.WriteFile(path, cfg, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SURFACE_AGENT_CONFIG", path)
	s, _, _, _ := setup(t, Options{})
	s.asyncWG.Wait() // centroids fitted through /v1/embeddings
	if embeds.Load() == 0 {
		t.Fatal("the examples were not embedded")
	}
	if _, err := s.Send(context.Background(), "what changed today?", "", ""); err != nil {
		t.Fatal(err)
	}
	s.asyncWG.Wait()
	if decides.Load() != 1 {
		t.Fatalf("decide calls %d", decides.Load())
	}
	turns, _ := s.trace.Recent(context.Background(), 1)
	async := mustResult(t, turns[0].ClassifiedAsync)
	judges := map[string]bool{}
	for _, v := range async.Facets["act"].Votes {
		judges[v.Judge] = true
	}
	if !judges["decide"] || !judges["centroid:act"] {
		t.Fatalf("async judges %v (failed %v)", judges, async.Failed)
	}
}
