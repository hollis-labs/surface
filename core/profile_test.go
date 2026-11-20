package core

import (
	"context"
	"embed"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/hollis-labs/surface/module"
)

// profileMod is a module that ships an agent profile (testdata/profiletest/profile).
type profileMod struct{ module.Base }

//go:embed all:testdata/profiletest/profile
var profileTestFiles embed.FS

func (profileMod) Manifest() module.Manifest {
	return module.Manifest{ID: "profiletest", Title: "Profile test", Version: "0.0.1",
		Views:   []module.View{{ID: "profiletest", Title: "Profile test", Render: module.Component("profiletest/main")}},
		Profile: module.ProfileFS(profileTestFiles, "testdata/profiletest/profile")}
}

// emptySlotMod declares a profile slot with nothing in it yet.
type emptySlotMod struct{ module.Base }

func (emptySlotMod) Manifest() module.Manifest {
	return module.Manifest{ID: "emptyslot", Title: "Empty slot", Version: "0.0.1", Profile: fstest.MapFS{"README.md": {Data: []byte("# Coming soon")}}}
}

func init() {
	module.Register(profileMod{})
	module.Register(emptySlotMod{})
}

type profileState struct {
	On     bool   `json:"on"`
	Runner string `json:"runner"`
	Active struct {
		ID  string `json:"id"`
		Why string `json:"why"`
	} `json:"active"`
	Pinned   string `json:"pinned"`
	Profiles []struct {
		ID       string   `json:"id"`
		Variants []string `json:"variants"`
		Tools    []string `json:"tools"`
		Active   bool     `json:"active"`
	} `json:"profiles"`
}

// The Nanite runner works as the profile of the module in front, as the app's own agent
// otherwise, and as the conversation's pin when there is one; the run log says which.
func TestModuleProfilesOnTheNaniteRunner(t *testing.T) {
	model := newFakeModel(t)
	naniteConfig(t, model.srv.URL)
	ctx := context.Background()
	data := t.TempDir()
	app, err := New(ctx, Options{Name: "profiles", Title: "Profiles", DataDir: data, Modules: []string{"scratch", "profiletest", "emptyslot"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = app.Start(ctx); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(app.Handler())
	t.Cleanup(func() {
		srv.CloseClientConnections()
		srv.Close()
		_ = app.Stop(context.Background())
	})

	var st profileState
	do(t, srv, "GET", "/api/agent/profiles", "", &st)
	if !st.On || st.Runner != "nanite" || len(st.Profiles) != 1 || st.Profiles[0].ID != "profiletest" ||
		len(st.Profiles[0].Variants) != 1 || len(st.Profiles[0].Tools) != 1 {
		t.Fatalf("profiles: %+v", st)
	}

	a := &lateApp{t: t, srv: srv}
	var conv struct {
		ID string `json:"conversation_id"`
	}
	do(t, srv, "POST", "/api/agent/agents/profiles/conversation", "{}", &conv)
	var att struct {
		ID string `json:"attachment_id"`
	}
	do(t, srv, "POST", "/api/agent/conversations/"+conv.ID+"/attachments", `{"role":"controller","stream":"committed"}`, &att)
	a.conv, a.att = conv.ID, att.ID
	turn := func(text string, n int) string {
		t.Helper()
		if code := do(t, srv, "POST", "/api/agent/conversations/"+a.conv+"/turns", `{"attachment":"`+a.att+`","text":"`+text+`"}`, nil); code != 202 {
			t.Fatalf("turn %q: %d", text, code)
		}
		a.until("the model was asked: "+text, func() bool { return model.sentWith(text) != "" })
		a.until("the run ended", func() bool {
			runs := a.runs()
			return len(runs) == n && runs[0].Status == "finished"
		})
		return model.sentWith(text)
	}
	profileOf := func() string {
		var runs []struct {
			Profile string `json:"profile"`
		}
		do(t, srv, "GET", "/api/agent/runs", "", &runs)
		return runs[0].Profile
	}

	// Scratch in front: the app's own agent.
	do(t, srv, "POST", "/api/desk/actions", `{"type":"open","view":"scratch"}`, nil)
	body := turn("first question", 1)
	if strings.Contains(body, "SAMPLE-PROFILE-MARKER") || !strings.Contains(body, "card_ask") {
		t.Errorf("the seat's turn: %s", body)
	}
	if p := profileOf(); p != "default" {
		t.Errorf("run log profile: %q", p)
	}

	// The profile's module in front: its instructions and context, the seat's still there,
	// and the subtask tool for its variant.
	do(t, srv, "POST", "/api/desk/actions", `{"type":"open","view":"profiletest"}`, nil)
	do(t, srv, "GET", "/api/agent/profiles", "", &st)
	if st.Active.ID != "profiletest" || st.Active.Why != "focus" {
		t.Fatalf("active: %+v", st.Active)
	}
	body = turn("second question", 2)
	for _, want := range []string{"SAMPLE-PROFILE-MARKER", "SAMPLE-CONTEXT-MARKER", "card_ask", "Profile: profiletest", `"subtask"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the profile's turn lacks %q", want)
		}
	}
	if p := profileOf(); p != "profiletest" {
		t.Errorf("run log profile: %q", p)
	}
	// Tool packages are the owner's to turn on: the config names no "tools" group.
	if strings.Contains(body, "tool__echo") {
		t.Error("a tool package was offered without the tools group")
	}

	// The owner pins the app's own agent: it holds with the module in front.
	if code := do(t, srv, "POST", "/api/agent/profiles/pin", `{"profile":"default"}`, &st); code != 200 || st.Pinned != "default" || st.Active.ID != "" {
		t.Fatalf("pin: %d %+v", code, st)
	}
	body = turn("third question", 3)
	if strings.Contains(body, "SAMPLE-PROFILE-MARKER") {
		t.Error("the pinned seat's turn has the profile")
	}
	if p := profileOf(); p != "default" {
		t.Errorf("run log profile: %q", p)
	}
	// The history is one conversation whatever the profile.
	if !strings.Contains(body, "first question") || !strings.Contains(body, "second question") {
		t.Error("the conversation's history was not shared across profiles")
	}

	// The agent's profile_switch: listed, then pinned, then back to automatic.
	var out struct {
		Result json.RawMessage `json:"result"`
	}
	if code := do(t, srv, "POST", "/api/tools/profile_switch", `{"profile":"profiletest"}`, &out); code != 200 {
		t.Fatalf("profile_switch: %d %s", code, out.Result)
	}
	do(t, srv, "GET", "/api/agent/profiles", "", &st)
	if st.Pinned != "profiletest" || st.Active.Why != "pinned" {
		t.Fatalf("after profile_switch: %+v", st)
	}
	if code := do(t, srv, "POST", "/api/agent/profiles/pin", `{"profile":"nope"}`, nil); code != 400 {
		t.Errorf("an unknown profile: %d", code)
	}
	st = profileState{}
	do(t, srv, "POST", "/api/agent/profiles/pin", `{"profile":"auto"}`, &st)
	if st.Pinned != "" || st.Active.Why != "focus" {
		t.Errorf("auto: %+v", st)
	}

	// The staged directory is under the data directory; the owner's seat is untouched.
	if _, err := os.Stat(filepath.Join(data, "agents", "profiles.staged", "agent.md")); err != nil {
		t.Errorf("staged directory: %v", err)
	}
	seat, err := os.ReadFile(filepath.Join(filepath.Dir(os.Getenv("SURFACE_AGENT_CONFIG")), "agents", "profiles", "agent.md"))
	if err != nil || strings.Contains(string(seat), "Profile:") {
		t.Errorf("the seat was changed: %v", err)
	}
}

// naniteConfig writes an agent config for the Nanite runner whose gateway is the fake model.
// The key file's path has its links resolved: Nanite refuses a credential reached through one
// (a temporary directory on macOS is).
func naniteConfig(t *testing.T, gateway string) {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(dir, "key")
	if err = os.WriteFile(key, []byte("test-key"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, "agent.json")
	raw, _ := json.Marshal(map[string]any{"model": "test-model", "gateway_url": gateway, "key_file": key})
	if err = os.WriteFile(cfg, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SURFACE_AGENT_CONFIG", cfg)
	t.Setenv("SURFACE_AGENT_RUNNER", "nanite")
	t.Setenv("NANITE_DEFAULT_MODEL", "test-model")
}

// On the host runner every turn is the app's own agent: profiles are listed, off.
func TestModuleProfilesAreOffOnTheHostRunner(t *testing.T) {
	model := newFakeModel(t)
	hostConfig(t, model.srv.URL)
	ctx := context.Background()
	app, err := New(ctx, Options{Name: "profilehost", Title: "Profile host", DataDir: t.TempDir(), Modules: []string{"scratch", "profiletest"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = app.Start(ctx); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(app.Handler())
	t.Cleanup(func() {
		srv.Close()
		_ = app.Stop(context.Background())
	})
	do(t, srv, "POST", "/api/desk/actions", `{"type":"open","view":"profiletest"}`, nil)
	var st profileState
	do(t, srv, "GET", "/api/agent/profiles", "", &st)
	if st.On || st.Active.Why != "off" || st.Active.ID != "" || len(st.Profiles) != 1 {
		t.Fatalf("profiles: %+v", st)
	}
	if code := do(t, srv, "POST", "/api/agent/profiles/pin", `{"profile":"profiletest"}`, nil); code != 400 {
		t.Errorf("a pin with profiles off: %d", code)
	}
}
