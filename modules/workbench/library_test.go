package workbench

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hollis-labs/surface/module/moduletest"
)

func start(t *testing.T) (*mod, *moduletest.Host) {
	t.Helper()
	m := &mod{}
	h := moduletest.Start(t, m)
	return m, h
}

func TestEveryTemplateValidates(t *testing.T) {
	for _, tpl := range Templates() {
		tree, suites, err := fromTemplate(tpl.Name, "probe")
		if err != nil {
			t.Fatal(err)
		}
		rep := Validate(tree)
		if !rep.OK {
			t.Fatalf("%s: %+v", tpl.Name, rep.Problems)
		}
		if rep.AgentdirDigest == "" || rep.Digest == "" {
			t.Fatalf("%s: no digest", tpl.Name)
		}
		for name, raw := range suites {
			if _, err := ParseSuite(raw); err != nil {
				t.Fatalf("%s: suite %s: %v", tpl.Name, name, err)
			}
		}
	}
}

func TestCreateSaveVersionsRollback(t *testing.T) {
	m, h := start(t)
	a, err := m.create("", "Helper", "embedded", "owner")
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != "helper" || !a.OK || a.Version != 1 {
		t.Fatalf("created: %+v", a)
	}
	// A save that does not validate is refused with the file named, and nothing changes.
	code, body := h.Do("PUT", "/agents/helper/files", `{"changes":{"host.yaml":"kind: embedded\nlifetime: forever\n"},"note":"bad"}`)
	if code != 422 || !strings.Contains(body, `"file":"host.yaml"`) {
		t.Fatalf("invalid save: %d %s", code, body)
	}
	vs, _ := m.lib.Versions("helper")
	if len(vs) != 1 {
		t.Fatalf("versions after a refused save: %d", len(vs))
	}
	// A form edit of host.yaml's runtime knobs keeps the rest of the file.
	code, body = h.Do("PUT", "/agents/helper/files", `{"edits":[{"file":"host.yaml","set":{"runtime.max_steps":12,"runtime.turn_token_budget":90000}}],"note":"tighter"}`)
	if code != 200 {
		t.Fatal(code, body)
	}
	tree, _ := m.lib.Tree("helper")
	host := string(tree["host.yaml"])
	if !strings.Contains(host, "max_steps: 12") || !strings.Contains(host, "turn_token_budget: 90000") || !strings.Contains(host, "application_id: workbench") {
		t.Fatal(host)
	}
	// The prompt's front matter and body.
	code, body = h.Do("PUT", "/agents/helper/files", `{"edits":[{"file":"agent.md","set":{"description":"Helps."},"body":"Be brief.\n"}],"note":"prompt"}`)
	if code != 200 {
		t.Fatal(code, body)
	}
	tree, _ = m.lib.Tree("helper")
	if md := string(tree["agent.md"]); !strings.Contains(md, "description: Helps.") || !strings.HasSuffix(md, "---\nBe brief.\n") || !strings.Contains(md, `schema_version: "2"`) {
		t.Fatal(md)
	}
	vs, _ = m.lib.Versions("helper")
	if len(vs) != 3 || vs[2].Note != "prompt" {
		t.Fatalf("versions: %+v", vs)
	}
	// Diff of version 1 against now, then roll back to it.
	code, body = h.Do("GET", "/agents/helper/versions/1/diff", "")
	if code != 200 || !strings.Contains(body, `"file":"agent.md"`) || !strings.Contains(body, `"file":"host.yaml"`) {
		t.Fatal(code, body)
	}
	if code, body = h.Do("POST", "/agents/helper/versions/1/rollback", ""); code != 200 {
		t.Fatal(code, body)
	}
	tree, _ = m.lib.Tree("helper")
	if tree.Digest() != vs[0].Digest {
		t.Fatal("rollback did not restore version 1")
	}
	// A save from a stale base is refused.
	if code, _ = h.Do("PUT", "/agents/helper/files", `{"changes":{"context/x.md":"x"},"base":"`+vs[1].Digest+`"}`); code == 200 {
		t.Fatal("stale base accepted")
	}
	// Paths outside the authored set are refused.
	if code, _ = h.Do("PUT", "/agents/helper/files", `{"changes":{"../escape.md":"x"}}`); code == 200 {
		t.Fatal("escape accepted")
	}
	if code, _ = h.Do("PUT", "/agents/helper/files", `{"changes":{".nanite/x":"x"}}`); code == 200 {
		t.Fatal(".nanite write accepted")
	}
}

func TestDuplicateAndDeleteToTrash(t *testing.T) {
	m, h := start(t)
	if _, err := m.create("one", "", "blank", "owner"); err != nil {
		t.Fatal(err)
	}
	code, body := h.Do("POST", "/agents/one/duplicate", `{}`)
	if code != 201 || !strings.Contains(body, `"id":"one-copy"`) {
		t.Fatal(code, body)
	}
	code, body = h.Do("DELETE", "/agents/one", "")
	if code != 200 {
		t.Fatal(code, body)
	}
	var out struct{ Trash string }
	_ = json.Unmarshal([]byte(body), &out)
	if _, err := os.Stat(filepath.Join(out.Trash, "agent", "agent.md")); err != nil {
		t.Fatal("not in the trash:", err)
	}
	list, _ := m.lib.List()
	if len(list) != 1 || list[0].ID != "one-copy" {
		t.Fatalf("list: %+v", list)
	}
}

// credName is an environment name that looks like a credential's (built, so the public-repo
// scan does not read the fixtures as secrets).
var credName = "SERVICE_" + "TOKEN"

func TestValidationNamesTheFile(t *testing.T) {
	tree, _, _ := fromTemplate("embedded", "v")
	cases := []struct {
		file, content, want string
	}{
		{".mcp.json", `{"mcpServers":{"x":{"type":"http"}}}`, "requires an HTTP(S) URL"},
		{".mcp.json", `{"mcpServers":{"x":{"command":"tool","env":{"` + credName + `":"written-in-the-file"}}}}`, "credential is named"},
		{"variants/reviewer.md", "---\nread_only: true\ncan_spawn: true\n---\nReview.\n", "cannot be combined"},
		{"variants/reviewer.md", "---\nlast_n: 80\n---\nReview.\n", "last_n must be at most 50"},
		{"variants/reviewer.md", "---\nsurprise: 1\n---\nReview.\n", "surprise"},
		{"variants/code.md", "Do it.\n", "built-in variant"},
		{"providers.yaml", "providers:\n  anthropic:\n    wire: anthropic\n    base_url: http://x\n", "built in"},
		{"providers.yaml", "providers:\n  local:\n    wire: openai-chat\n    base_url: http://127.0.0.1:1/v1\n    headers: {X-Api-Key: abc}\n", "X-Api-Key"},
		{"host.yaml", "kind: embedded\nlifetime: application\napplication_id: a\nruntime:\n  subtask_limit: 9\n", "subtask_limit is at most 8"},
		{steeringFile, `{"workbench_format":1,"reflexes":[{"name":"x","trigger_kind":"event","trigger":{},"action_kind":"remind","action":{"body":"b"}}]}`, "topic is required"},
		{steeringFile, `{"workbench_format":1,"reflexes":[{"name":"x","trigger_kind":"focus","trigger":{"view":"v"},"action_kind":"halt","action":{"message":"m"}}]}`, "halt only answers a message"},
		{steeringFile, `{"workbench_format":1,"presets":[{"name":"p","reflexes":["missing"]}]}`, "not defined"},
		{optionsFile, `{"workbench_format":1,"compact_at":-1}`, "compact_at"},
	}
	for _, c := range cases {
		bad := tree.Clone()
		bad[c.file] = []byte(c.content)
		rep := Validate(bad)
		if rep.OK {
			t.Fatalf("%s accepted: %s", c.file, c.content)
		}
		found := false
		for _, p := range rep.Problems {
			if p.File == c.file && strings.Contains(p.Message, c.want) {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s: want %q, got %+v", c.file, c.want, rep.Problems)
		}
	}
	// References are not literal credentials.
	ok := tree.Clone()
	ok[".mcp.json"] = []byte(`{"mcpServers":{"x":{"command":"tool","env":{"` + credName + `":"${VARIABLE}","` + credName + `2":"keychain://x/y"}}}}`)
	if rep := Validate(ok); !rep.OK {
		t.Fatal(rep.Problems)
	}
}

func TestPreviewShowsTheBasePromptReadOnly(t *testing.T) {
	tree, _, _ := fromTemplate("embedded", "p")
	p, err := (&naniteRuntime{}).Preview(t.Context(), tree)
	if err != nil {
		t.Fatal(err)
	}
	var universal, agentSlot bool
	for _, s := range p.Slots {
		if s.Name == "universal" && s.ReadOnly && len(s.Content) > 100 {
			universal = true
		}
		if strings.Contains(s.Content, "You are p, an agent embedded") && !s.ReadOnly {
			agentSlot = true
		}
	}
	if !universal || !agentSlot || p.Tokens == 0 {
		t.Fatalf("preview: %+v", p.Slots)
	}
}
