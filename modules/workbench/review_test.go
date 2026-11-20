package workbench

// Regression tests for the review of PR 81: one or more for each finding.

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/hollis-labs/surface/module"
	"github.com/hollis-labs/surface/module/moduletest"
)

// H1: a save, a rollback and an accepted proposal change only authored files: Nanite's
// .nanite/ state and any other file in the agent directory survive.
func TestSavesNeverTouchStateOrUnknownFiles(t *testing.T) {
	m, h := start(t)
	f := &fakeGateway{usage: 500}
	withGateway(t, h, f)
	if _, err := m.create("keep", "", "embedded", "owner"); err != nil {
		t.Fatal(err)
	}
	dir, _ := m.lib.AgentDir("keep")
	plant := map[string]string{
		".nanite/nanite.db":              "db",
		".nanite/agent.lock":             "",
		".nanite/transcripts/c1/0.jsonl": "{}",
		".nanite/jobs/j1.log":            "log",
		"NOTES.txt":                      "the owner's own notes",
		"context/house-style.md.bak":     "kept",
		"context/.history/1":             "kept",
	}
	for p, body := range plant {
		target := filepath.Join(dir, p)
		_ = os.MkdirAll(filepath.Dir(target), 0o700)
		if err := os.WriteFile(target, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	survived := func(when string) {
		t.Helper()
		for p, body := range plant {
			b, err := os.ReadFile(filepath.Join(dir, p)) //nolint:gosec // the test's own directory
			if err != nil || string(b) != body {
				t.Fatalf("%s: %s did not survive: %v", when, p, err)
			}
		}
	}
	info, _ := os.Stat(dir)
	// A save that removes a file and changes another.
	if code, body := h.Do("PUT", "/agents/keep/files", `{"changes":{"context/house-style.md":null,"agent.md":null}}`); code == 200 {
		t.Fatal("agent.md removed", body)
	}
	if code, body := h.Do("PUT", "/agents/keep/files", `{"changes":{"context/house-style.md":null,"context/new.md":"# New\n"},"note":"swap"}`); code != 200 {
		t.Fatal(code, body)
	}
	survived("save")
	if _, err := os.Stat(filepath.Join(dir, "context", "house-style.md")); err == nil {
		t.Fatal("a removed authored file is still there")
	}
	if after, _ := os.Stat(dir); !os.SameFile(info, after) {
		t.Fatal("the agent directory was replaced, not edited in place")
	}
	// A rollback.
	if code, body := h.Do("POST", "/agents/keep/versions/1/rollback", ""); code != 200 {
		t.Fatal(code, body)
	}
	survived("rollback")
	if _, err := os.Stat(filepath.Join(dir, "context", "house-style.md")); err != nil {
		t.Fatal("rollback did not restore an authored file")
	}
	// An accepted proposal.
	if _, err := m.run.SaveSuite("keep", []byte(loopSuite)); err != nil {
		t.Fatal(err)
	}
	r, err := m.run.Start(t.Context(), "keep", "loop", "live", Caps{}, "")
	if err != nil {
		t.Fatal(err)
	}
	m.run.Wait()
	p, err := m.run.Propose(t.Context(), "keep", r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.run.Decide("keep", p.ID, 1, true); err != nil {
		t.Fatal(err)
	}
	survived("accept")
}

// H1/L3: Start recovers what an older swap or an interrupted write left, and marks runs left
// running as interrupted.
func TestStartRecoversLeftoversAndInterruptedRuns(t *testing.T) {
	data := t.TempDir()
	m := &mod{}
	h := moduletest.New(t, m)
	if err := h.Settings.Set(t.Context(), "root", data); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(t.Context(), h); err != nil {
		t.Fatal(err)
	}
	if _, err := m.create("crash", "", "blank", "owner"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.create("swap", "", "blank", "owner"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.run.SaveSuite("crash", []byte(loopSuite)); err != nil {
		t.Fatal(err)
	}
	stuck := Run{ID: newID(), Agent: "crash", Suite: "loop", Status: "running", Mode: "live", Cases: []CaseResult{}}
	if err := m.run.saveRun(stuck); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(data, "crash", "agent")
	_ = os.WriteFile(filepath.Join(dir, ".tmp-123"), []byte("half"), 0o600)
	_ = os.MkdirAll(filepath.Join(dir, ".nanite"), 0o700)
	_ = os.WriteFile(filepath.Join(dir, ".nanite", ".tmp-keep"), []byte("nanite's"), 0o600)
	// An older Workbench crashed between moving agent aside and moving the new one in.
	swap := filepath.Join(data, "swap", "agent")
	if err := os.Rename(swap, swap+".old"); err != nil {
		t.Fatal(err)
	}
	_ = os.MkdirAll(swap+".next", 0o700)

	m2 := &mod{}
	h2 := moduletest.New(t, m2)
	_ = h2.Settings.Set(t.Context(), "root", data)
	if err := m2.Start(t.Context(), h2); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".tmp-123")); err == nil {
		t.Fatal("a temporary file was left")
	}
	if _, err := os.Stat(filepath.Join(dir, ".nanite", ".tmp-keep")); err != nil {
		t.Fatal("recovery touched .nanite/")
	}
	if _, err := os.Stat(filepath.Join(swap, "agent.md")); err != nil {
		t.Fatal("the agent set aside was not put back")
	}
	if _, err := os.Stat(swap + ".next"); err == nil {
		t.Fatal("agent.next was left")
	}
	r, _ := m2.run.GetRun("crash", stuck.ID)
	if r.Status != "interrupted" {
		t.Fatalf("stuck run: %s", r.Status)
	}
}

// H2: the server process never changes directory during a run; the work happens in a child.
func TestEvalRunsInAChildAndTheServerKeepsItsDirectory(t *testing.T) {
	m, h := start(t)
	withGateway(t, h, &fakeGateway{usage: 500})
	if _, err := m.create("cwd", "", "embedded", "owner"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.run.SaveSuite("cwd", []byte(loopSuite)); err != nil {
		t.Fatal(err)
	}
	want, _ := os.Getwd()
	stop := make(chan struct{})
	var moved string
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			if d, _ := os.Getwd(); d != want && moved == "" {
				moved = d
			}
			runtime.Gosched()
		}
	}()
	r, err := m.run.Start(t.Context(), "cwd", "loop", "live", Caps{}, "")
	if err != nil {
		t.Fatal(err)
	}
	sawChild := false
	for i := 0; i < 200; i++ {
		if pid, ok := m.run.ChildPID(r.ID); ok && pid != os.Getpid() {
			sawChild = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	m.run.Wait()
	close(stop)
	wg.Wait()
	if moved != "" {
		t.Fatalf("the server's directory moved to %s during a run", moved)
	}
	if !sawChild {
		t.Fatal("no eval child process was seen")
	}
	got, _ := m.run.GetRun("cwd", r.ID)
	if got.Status != "done" || got.Passed != 1 {
		t.Fatalf("run: %+v", got)
	}
}

// H2: canceling a run kills its child.
func TestCancelKillsTheChild(t *testing.T) {
	m, h := start(t)
	release := make(chan struct{})
	asked := make(chan struct{}, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked <- struct{}{}
		select { // a gateway that never answers
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) }) // runs first: the handler lets go before Close waits
	key := filepath.Join(t.TempDir(), "key")
	_ = os.WriteFile(key, []byte("k"), 0o600)
	h.AgentState.GW = &module.Gateway{Model: "claude-test-1", URL: srv.URL, KeyFile: key}
	if _, err := m.create("cancel", "", "embedded", "owner"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.run.SaveSuite("cancel", []byte(loopSuite)); err != nil {
		t.Fatal(err)
	}
	r, err := m.run.Start(t.Context(), "cancel", "loop", "live", Caps{}, "")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-asked:
	case <-time.After(30 * time.Second):
		t.Fatal("the child never reached the gateway")
	}
	pid, ok := m.run.ChildPID(r.ID)
	if !ok {
		t.Fatal("no child")
	}
	if !m.run.Cancel(r.ID) {
		t.Fatal("not canceled")
	}
	done := make(chan struct{})
	go func() { m.run.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("the run did not end after cancel")
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("the child %d is still there: %v", pid, err)
	}
	got, _ := m.run.GetRun("cancel", r.ID)
	if got.Status != "canceled" {
		t.Fatalf("status %s", got.Status)
	}
}

// M1: an outside caller (no Origin: MCP, the HTTP tools, the CLI) cannot spend, accept,
// roll back, delete or read from disk through the routes.
func TestOutsideCallersAreRefused(t *testing.T) {
	m, h := start(t)
	withGateway(t, h, &fakeGateway{usage: 500})
	if _, err := m.create("guarded", "", "embedded", "owner"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.run.SaveSuite("guarded", []byte(loopSuite)); err != nil {
		t.Fatal(err)
	}
	outside := func(method, path, body string) int {
		req := httptest.NewRequest(method, "/api/m/workbench"+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(module.WithActor(req.Context(), module.ActorOutside))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	for _, c := range []struct{ method, path, body string }{
		{"POST", "/agents/guarded/suites/loop/run", `{"mode":"live","confirm":true}`},
		{"POST", "/agents/guarded/runs/x/rerun", `{"mode":"live","confirm":true}`},
		{"POST", "/agents/guarded/runs/x/propose", `{"confirm":true}`},
		{"POST", "/agents/guarded/proposals/x/1/accept", ``},
		{"POST", "/agents/guarded/versions/1/rollback", ``},
		{"DELETE", "/agents/guarded", ``},
		{"POST", "/import-dir", `{"path":"/tmp"}`},
	} {
		if code := outside(c.method, c.path, c.body); code != http.StatusForbidden {
			t.Errorf("%s %s from outside: %d", c.method, c.path, code)
		}
	}
	runs, _ := m.run.Runs("guarded")
	if len(runs) != 0 {
		t.Fatal("an outside caller started a run")
	}
	// An offline run spends nothing and stays open to outside callers.
	if code := outside("POST", "/agents/guarded/suites/loop/run", `{}`); code != http.StatusAccepted {
		t.Fatalf("offline run from outside: %d", code)
	}
	m.run.Wait()
}

// M2: a small archive that inflates hugely is refused quickly, without the memory.
func TestArchiveBombIsRefusedWithBoundedMemory(t *testing.T) {
	var buf bytes.Buffer
	gz, _ := gzip.NewWriterLevel(&buf, gzip.BestSpeed)
	tw := tar.NewWriter(gz)
	zeros := make([]byte, maxFileBytes)
	for i := 0; i < 64; i++ { // 64 MiB inflated, past the 24 MiB limit
		name := "context/f" + strings.Repeat("x", i%7) + string(rune('a'+i%26)) + ".md"
		_ = tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Size: int64(len(zeros)), Mode: 0o600})
		_, _ = tw.Write(zeros)
	}
	_ = tw.Close()
	_ = gz.Close()
	if buf.Len() > maxArchiveBytes {
		t.Skipf("the bomb compressed to %d bytes", buf.Len())
	}
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	start := time.Now()
	_, err := readArchive(buf.Bytes())
	runtime.ReadMemStats(&after)
	if err == nil {
		t.Fatal("the bomb was accepted")
	}
	if grew := after.TotalAlloc - before.TotalAlloc; grew > 120<<20 {
		t.Fatalf("reading the bomb allocated %d MiB", grew>>20)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("refusing the bomb took %v", d)
	}
	// Skipped entries inflate too, and are capped the same way.
	buf.Reset()
	gz, _ = gzip.NewWriterLevel(&buf, gzip.BestSpeed)
	tw = tar.NewWriter(gz)
	for i := 0; i < 40; i++ {
		_ = tw.WriteHeader(&tar.Header{Name: "junk/" + string(rune('a'+i%26)) + strings.Repeat("y", i/26) + ".bin", Typeflag: tar.TypeReg, Size: int64(len(zeros)), Mode: 0o600})
		_, _ = tw.Write(zeros)
	}
	_ = tw.Close()
	_ = gz.Close()
	if _, err := readArchive(buf.Bytes()); err == nil {
		t.Fatal("40 MiB of skipped entries was read")
	}
}

// L6: two names for one file on a case-insensitive disk are refused.
func TestCaseInsensitiveDuplicatesAreRefused(t *testing.T) {
	entries := append(minimal(t), tarEntry{name: "context/Notes.md", body: "a"}, tarEntry{name: "context/notes.md", body: "b"})
	if _, err := readArchive(makeTar(t, entries)); err == nil || !strings.Contains(err.Error(), "case-insensitive") {
		t.Fatalf("duplicates: %v", err)
	}
}

// M3: credentials that the first version missed.
func TestSecretFalseNegatives(t *testing.T) {
	base, _, _ := fromTemplate("embedded", "s")
	// Built at run time so that the repository's own secret scan does not read these
	// fixtures as secrets.
	ant := "sk-" + "ant-" + strings.Repeat("a1", 12)
	gh := "gh" + "p_" + strings.Repeat("Z9", 12)
	pw := "bob" + ":" + "hunter22" + "@"
	flagVal := "--tok" + "en=" + "abcdefghijk123"
	query := "api" + "_key=" + "abcdef123456"
	probes := map[string]struct{ file, content string }{
		"a GitHub token under a name with no key word":  {".mcp.json", `{"mcpServers":{"x":{"command":"t","env":{"GITHUB_PAT":"` + gh + `"}}}}`},
		"a password inside a URL":                       {".mcp.json", `{"mcpServers":{"x":{"type":"http","url":"https://` + pw + `example.com/mcp"}}}`},
		"a key passed as an argument":                   {".mcp.json", `{"mcpServers":{"x":{"command":"t","args":["--api-key","` + ant + `"]}}}`},
		"a key in a flag=value argument":                {".mcp.json", `{"mcpServers":{"x":{"command":"t","args":["` + flagVal + `"]}}}`},
		"a Bearer value that is only partly a variable": {".mcp.json", `{"mcpServers":{"x":{"type":"http","url":"https://e.com","headers":{"Authorization":"Bearer ` + ant + ` ${X}"}}}}`},
		"a key in a URL query":                          {".mcp.json", `{"mcpServers":{"x":{"type":"http","url":"https://e.com/mcp?` + query + `"}}}`},
		"a key in a provider base_url":                  {"providers.yaml", "providers:\n  p:\n    wire: openai-chat\n    base_url: https://e.com/v1?" + query + "\n"},
		"a scheme that is not a reference":              {"host.yaml", "kind: embedded\nlifetime: application\napplication_id: a\nruntime:\n  env:\n    SERVICE_TOKEN: https://not-a-ref\n"},
	}
	for name, p := range probes {
		bad := base.Clone()
		bad[p.file] = []byte(p.content)
		if rep := Validate(bad); rep.OK {
			t.Errorf("%s: accepted", name)
		}
	}
	// In a prompt body or a skill resource: a warning when saved, and the export is refused.
	m, _ := start(t)
	if _, err := m.create("leaky", "", "embedded", "owner"); err != nil {
		t.Fatal(err)
	}
	tree, _ := m.lib.Tree("leaky")
	tree["skills/summarize/notes.txt"] = []byte("use " + ant + " for testing\n")
	rep := Validate(tree)
	if !rep.OK {
		t.Fatal(rep.Problems)
	}
	warned := false
	for _, p := range rep.Problems {
		if p.File == "skills/summarize/notes.txt" && p.Severity == "warning" {
			warned = true
		}
	}
	if !warned {
		t.Fatal("no warning for a key in a skill resource")
	}
	if _, _, err := m.lib.Save("leaky", tree, "", "leak", "owner"); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := m.export("leaky"); err == nil || !strings.Contains(err.Error(), "notes.txt") {
		t.Fatalf("export with a key in a skill resource: %v", err)
	}
	// References stay fine.
	ok := base.Clone()
	ok[".mcp.json"] = []byte(`{"mcpServers":{"x":{"type":"http","url":"https://e.com","headers":{"Authorization":"Bearer ${TOKEN}"}},"y":{"command":"t","env":{"` + credName + `":"keychain://svc/name","GITHUB_PAT":"${GITHUB_PAT}"},"args":["--token","${TOKEN}"]}}}`)
	if rep := Validate(ok); !rep.OK {
		t.Fatal(rep.Problems)
	}
}

// M4: a hunk moved far from its line, a truncated hunk, an ambiguous context and a hunk with
// no context are refused; nothing is applied in part.
func TestHunksAreAppliedStrictly(t *testing.T) {
	var lines []string
	for i := 1; i <= 200; i++ {
		lines = append(lines, "line "+string(rune('A'+i%26))+strings.Repeat("-", i))
	}
	text := strings.Join(lines, "\n") + "\n"
	target := lines[150] // line 151
	moved := "@@ -3,1 +3,1 @@\n-" + target + "\n+changed\n"
	if _, err := applyUnified(text, moved); err == nil {
		t.Fatal("a hunk for line 3 was applied at line 151")
	}
	withCtx := "@@ -3,2 +3,2 @@\n " + lines[149] + "\n-" + target + "\n+changed\n"
	if _, err := applyUnified(text, withCtx); err == nil {
		t.Fatal("a hunk 147 lines off was applied")
	}
	truncated := "@@ -151,3 +151,3 @@\n " + lines[149] + "\n-" + target + "\n"
	if _, err := applyUnified(text, truncated); err == nil || !strings.Contains(err.Error(), "cut short") {
		t.Fatalf("a truncated hunk: %v", err)
	}
	ambiguous := "one\ntwo\nx\none\ntwo\nx\n"
	if _, err := applyUnified(ambiguous, "@@ -10,2 +10,2 @@\n one\n-two\n+2\n"); err == nil {
		t.Fatal("an ambiguous context was applied")
	}
	good := "@@ -150,3 +150,3 @@\n " + lines[149] + "\n-" + target + "\n+changed\n " + lines[151] + "\n"
	out, err := applyUnified(text, good)
	if err != nil || !strings.Contains(out, "\nchanged\n") {
		t.Fatal(err)
	}
	// Two hunks, the second bad: the first is not applied either.
	both := good + "@@ -3,1 +3,1 @@\n-nothing like this\n+x\n"
	if _, err := applyUnified(text, both); err == nil {
		t.Fatal("a diff with a bad hunk was applied")
	}
	// A new file takes a hunk with no context.
	if out, err := applyUnified("", "@@ -0,0 +1,2 @@\n+a\n+b\n"); err != nil || out != "a\nb\n" {
		t.Fatalf("new file: %q %v", out, err)
	}
}

// M5: requests running side by side cannot together pass the run's token budget.
func TestTheTokenBudgetIsReservedBeforeEachRequest(t *testing.T) {
	m := &usageMeter{budget: 10000}
	if !m.reserve(6000) {
		t.Fatal("first reservation refused")
	}
	if m.reserve(6000) {
		t.Fatal("a second request in flight passed the budget")
	}
	if _, over := m.state(); !over {
		t.Fatal("the run was not stopped")
	}
}

// L1: a save between applying an edit and saving it refuses the accept, and two accepts of
// the same edit race to one version.
func TestDecideRacingASave(t *testing.T) {
	m, h := start(t)
	withGateway(t, h, &fakeGateway{usage: 500})
	if _, err := m.create("race", "", "embedded", "owner"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.run.SaveSuite("race", []byte(loopSuite)); err != nil {
		t.Fatal(err)
	}
	r, _ := m.run.Start(t.Context(), "race", "loop", "live", Caps{}, "")
	m.run.Wait()
	p, err := m.run.Propose(t.Context(), "race", r.ID)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := m.run.Decide("race", p.ID, 1, true); err == nil {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	vs, _ := m.lib.Versions("race")
	if wins != 1 || len(vs) != 2 {
		t.Fatalf("%d accepts won, %d versions", wins, len(vs))
	}
	// The base digest: a save made from an old read is refused.
	tree, _ := m.lib.Tree("race")
	old := tree.Digest()
	tree["context/x.md"] = []byte("x")
	if _, _, err := m.lib.Save("race", tree, old, "one", "owner"); err != nil {
		t.Fatal(err)
	}
	tree["context/y.md"] = []byte("y")
	if _, _, err := m.lib.Save("race", tree, old, "two", "owner"); err == nil {
		t.Fatal("a save from a stale read went through")
	}
}

// L2: the configuration files go through a confirm-level tool.
func TestConfigFilesNeedTheConfirmTool(t *testing.T) {
	m, h := start(t)
	if _, err := m.create("cfg", "", "blank", "owner"); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"host.yaml", ".mcp.json", "permissions.yaml", "providers.yaml"} {
		in, _ := json.Marshal(map[string]string{"agent": "cfg", "file": f, "content": "x", "note": "n"})
		if _, err := h.Tool("workbench_write_file", string(in)); err == nil {
			t.Errorf("workbench_write_file wrote %s", f)
		}
	}
	for _, tool := range h.Manifest().Tools {
		if tool.Name == "workbench_write_config" && tool.Level != module.LevelConfirm {
			t.Fatal("workbench_write_config is not confirm level")
		}
	}
	out, err := h.Tool("workbench_write_config", `{"agent":"cfg","file":"permissions.yaml","content":"permissions:\n  mode: plan\n","note":"plan mode"}`)
	if err != nil || !out.(map[string]any)["saved"].(bool) {
		t.Fatal(out, err)
	}
}

// L4: json_path's exists: false and equals: null.
func TestJSONPathExistsFalseAndNull(t *testing.T) {
	s, err := ParseSuite([]byte("name: j\ncases:\n  - id: a\n    turns: [hi]\n    graders:\n      - json_path: {path: error, exists: false}\n      - json_path: {path: next, equals: null}\n"))
	if err != nil {
		t.Fatal(err)
	}
	absent, null := s.Cases[0].Graders[0], s.Cases[0].Graders[1]
	if r := absent.grade(Outcome{Answer: `{"next": null}`}); !r.Pass {
		t.Fatal("exists: false failed on an absent path", r.Detail)
	}
	if r := absent.grade(Outcome{Answer: `{"error": "x"}`}); r.Pass {
		t.Fatal("exists: false passed on a present path")
	}
	if r := null.grade(Outcome{Answer: `{"next": null}`}); !r.Pass {
		t.Fatal("equals: null failed on null", r.Detail)
	}
	if r := null.grade(Outcome{Answer: `{"next": 3}`}); r.Pass {
		t.Fatal("equals: null passed on 3 (read as an existence check)")
	}
	if _, err := ParseSuite([]byte("name: j\ncases:\n  - id: a\n    turns: [hi]\n    graders:\n      - json_path: {path: p, equalz: 1}\n")); err == nil {
		t.Fatal("an unknown json_path field was accepted")
	}
}

// L5: the estimates behind the rerun and propose dialogs.
func TestWorstCaseEstimates(t *testing.T) {
	m, h := start(t)
	withGateway(t, h, &fakeGateway{usage: 500})
	if _, err := m.create("est", "", "embedded", "owner"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.run.SaveSuite("est", []byte(loopSuite)); err != nil {
		t.Fatal(err)
	}
	e, err := m.run.Estimate(t.Context(), "est", "loop", Caps{})
	if err != nil || e.WorstUSD <= 0 || e.WorstUSD > e.Caps.MaxUSD {
		t.Fatalf("estimate: %+v %v", e, err)
	}
	r, _ := m.run.Start(t.Context(), "est", "loop", "live", Caps{}, "")
	m.run.Wait()
	code, body := h.Do("GET", "/agents/est/runs/"+r.ID+"/propose-estimate", "")
	if code != 200 || !strings.Contains(body, `"worst_usd"`) || !strings.Contains(body, `"input_tokens"`) {
		t.Fatal(code, body)
	}
}

var _ = context.Background
