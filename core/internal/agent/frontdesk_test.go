package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hollis-labs/libs/agent/desk"
	"github.com/hollis-labs/surface/module"
)

func TestFrontDeskConfigOptIn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.json")
	t.Setenv("SURFACE_AGENT_CONFIG", path)
	c, err := LoadConfig("desk")
	if err != nil {
		t.Fatal(err)
	}
	if c.FrontDesk.options() != nil {
		t.Fatal("front desk enabled without opt-in")
	}
	raw := `{"front_desk":{"mode":"auto","plan_with_model":true,"intake_timeout_seconds":60,"merge_window_ms":100,"resume_interrupted":true}}`
	if err = os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	c, err = LoadConfig("desk")
	if err != nil {
		t.Fatal(err)
	}
	opts := c.FrontDesk.options()
	if opts.Intake.Mode != desk.IntakeAuto || !opts.PlanWithModel || opts.IntakeTimeout != time.Minute || opts.Policy.MergeWindow != 100*time.Millisecond || !c.FrontDesk.ResumeInterrupted {
		t.Fatalf("options: %+v", opts)
	}
	for _, bad := range []FrontDeskConfig{{Mode: "automatic"}, {IntakeTimeoutSeconds: -1}, {IntakeTimeoutSeconds: 301}, {MergeWindowMS: -1}, {MergeWindowMS: 60001}} {
		body, _ := json.Marshal(map[string]any{"front_desk": bad})
		if err = os.WriteFile(path, body, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err = LoadConfig("desk"); err == nil {
			t.Fatalf("accepted invalid config %+v", bad)
		}
	}
}
func TestFrontDeskRoutesDisabledAndOwnerOnly(t *testing.T) {
	t.Setenv("SURFACE_AGENT_RUNNER", RunnerScripted)
	s, _, _, _ := setup(t, Options{})
	mux := http.NewServeMux()
	s.Mount(mux)
	for _, action := range []string{"accept", "decline"} {
		for _, actor := range []module.Actor{module.ActorOwner, module.ActorOutside, module.ActorAgent} {
			req := httptest.NewRequest("POST", Prefix+"/front-desk/plans/missing/"+action, nil)
			req = req.WithContext(module.WithActor(req.Context(), actor))
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			want := http.StatusConflict
			if actor.Delegate() {
				want = http.StatusForbidden
			}
			if rec.Code != want {
				t.Fatalf("%s %s: %d %s", action, actor, rec.Code, rec.Body)
			}
		}
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", Prefix+"/front-desk", nil))
	if rec.Code != 200 || rec.Body.String() != "{\"enabled\":false}\n" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}
