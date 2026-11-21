package browser

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Opt in with SURFACE_TEST_CHROME=/path/to/chrome. This launches a disposable
// headless test process, no app/service, credentials or real upstream fixture server.
func TestSnapshotBrowserIsolation(t *testing.T) {
	chrome := os.Getenv("SURFACE_TEST_CHROME")
	if chrome == "" {
		t.Skip("set SURFACE_TEST_CHROME for the real-browser isolation check")
	}
	m, _ := snapshotMod(t, map[string]snapshotFixture{"https://example.com/": {`<h1>Visible static page</h1><script>parent.postMessage('escaped','*')</script><img src="data:text/html,bad"><a href="https://example.net/navigation">No frame navigation</a><style>h1{color:red}</style>`, "text/html"}})
	p, err := m.snapshot(context.Background(), "https://example.com/")
	if err != nil {
		t.Fatal(err)
	}
	// Exercise CSP and sandbox independently of sanitization, by appending deliberate
	// active content to the already-sanitized document in this test only.
	hostile := strings.Replace(p.HTML, "</body>", `<script>parent.postMessage('escaped','*');fetch('https://example.net/script-leak')</script><img src="https://example.net/image-leak"><style>p{background:url(https://example.net/css-leak)}</style><p>Network probe</p></body>`, 1)
	encoded, _ := json.Marshal(hostile)
	parent := `<!doctype html><body><p id="result">PENDING</p><iframe sandbox="" referrerpolicy="no-referrer"></iframe><script>
 let escaped=false;addEventListener('message',()=>escaped=true);
 const frame=document.querySelector('iframe');frame.srcdoc=` + string(encoded) + `;
 frame.onload=()=>setTimeout(()=>{document.querySelector('#result').textContent=(!escaped&&frame.contentDocument===null&&frame.getAttribute('sandbox')==='')?'ISOLATED':'FAILED'},100);
 </script></body>`
	dir := t.TempDir()
	page := filepath.Join(dir, "parent.html")
	log := filepath.Join(dir, "network.json")
	if err = os.WriteFile(page, []byte(parent), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	//nolint:gosec // explicit opt-in local test executable, fixed arguments, isolated temporary profile
	cmd := exec.CommandContext(ctx, chrome, "--headless=new", "--disable-gpu", "--no-first-run", "--no-default-browser-check", "--disable-background-networking", "--disable-component-update", "--disable-sync", "--user-data-dir="+filepath.Join(dir, "profile"), "--log-net-log="+log, "--virtual-time-budget=1500", "--dump-dom", "file://"+page)
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("Chrome: %v", err)
	}
	if !strings.Contains(string(output), `id="result">ISOLATED</p>`) {
		t.Fatalf("frame isolation check failed: %s", output)
	}
	data, err := os.ReadFile(log) //nolint:gosec // test-owned file under t.TempDir
	if err != nil {
		t.Fatal(err)
	}
	var netlog struct {
		Events []struct {
			Params map[string]any `json:"params"`
		} `json:"events"`
	}
	if err = json.Unmarshal(data, &netlog); err != nil {
		t.Fatal(err)
	}
	for _, event := range netlog.Events {
		if u, ok := event.Params["url"].(string); ok && strings.Contains(u, "example.net") {
			t.Fatalf("browser attempted upstream network request: %s", u)
		}
	}
}
