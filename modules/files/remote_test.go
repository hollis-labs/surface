package files

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/hollis-labs/surface/module/moduletest"
)

// fakeRunner answers remote calls from a table, by operation, and records them. No host
// is reached.
type fakeRunner struct {
	calls []fakeCall
	reply func(op string, args []string, stdin []byte) runResult
}

type fakeCall struct {
	host  string
	op    string
	argv  []string
	stdin []byte
}

func (f *fakeRunner) run(_ context.Context, host string, argv []string, stdin []byte, limit int) (runResult, error) {
	op := argv[4]
	f.calls = append(f.calls, fakeCall{host, op, argv, stdin})
	res := f.reply(op, argv[6:], stdin) // rel, follow, want, then the operation's own
	if len(res.out) > limit {
		res.out, res.truncated = res.out[:limit], true
	}
	return res, nil
}

func (f *fakeRunner) ops() []string {
	var out []string
	for _, c := range f.calls {
		out = append(out, c.op)
	}
	return out
}

// localRunner runs the remote script on this machine, as sh on the host would, so the
// script itself is tested without a host. The host name is ignored.
type localRunner struct{}

func (localRunner) run(ctx context.Context, _ string, argv []string, stdin []byte, limit int) (runResult, error) {
	return execute(exec.CommandContext(ctx, argv[0], argv[1:]...), stdin, limit) //nolint:gosec // the test's own argv
}

func testRemote(run runner, dir string) remote {
	return remote{cfg: Remote{ID: "box", Label: "box dev", Host: "box", Path: dir}, run: run}
}

func TestShQuoteRoundTrip(t *testing.T) {
	dir := t.TempDir()
	canary := filepath.Join(dir, "pwned")
	words := []string{
		"plain", "two words", "it's", `"double"`, `back\slash`, "$(touch " + canary + ")", "`touch " + canary + "`",
		"; touch " + canary, "| cat", "$HOME", "*", "", "--", "-o", "line\nbreak", "tab\there", "'''",
	}
	argv := append([]string{"printf", `%s\0`}, words...)
	// What ssh hands the remote login shell is commandLine(argv), run with sh -c.
	out, err := exec.Command("sh", "-c", commandLine(argv)).Output() //nolint:gosec // the test's own words
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	if !slices.Equal(got, words) {
		t.Errorf("round trip:\n got %q\nwant %q", got, words)
	}
	if _, err := os.Stat(canary); err == nil {
		t.Error("a word was run as a command")
	}
}

func TestSSHArgs(t *testing.T) {
	args := sshArgs("box", []string{"sh", "-c", "echo hi", "it's"})
	joined := strings.Join(args, " ")
	for _, want := range []string{"BatchMode=yes", "ConnectTimeout=8"} {
		if !strings.Contains(joined, want) {
			t.Errorf("ssh args lack %s: %q", want, args)
		}
	}
	n := len(args)
	if args[n-3] != "--" || args[n-2] != "box" || args[n-1] != `'sh' '-c' 'echo hi' 'it'\''s'` {
		t.Errorf("ssh args end %q", args[n-3:])
	}
	// A host that would be an option, or a shell word, never reaches exec.
	if _, err := (sshRunner{}).run(context.Background(), "-oProxyCommand=x", nil, nil, 10); !errors.Is(err, errRemote) {
		t.Errorf("an option as host: %v", err)
	}
}

func TestParseRemotes(t *testing.T) {
	rs, err := parseRemotes(`[{"id":"box-dev","label":"box ~/dev","host":"box","path":"~/dev"},{"host":"me@box.example.test","path":"/srv/x"}]`)
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 2 || rs[0].ID != "box-dev" || rs[1].ID != "me-box-example-test-srv-x" || rs[1].Label != "me@box.example.test:/srv/x" {
		t.Errorf("remotes = %+v", rs)
	}
	if p := rs[0].place(); p.ID != "remote:box-dev" || !p.Remote || p.Host != "box" {
		t.Errorf("place = %+v", p)
	}
	if rs, err := parseRemotes("  "); err != nil || rs != nil {
		t.Errorf("empty setting: %v %v", rs, err)
	}
	for _, bad := range []string{
		`not json`,
		`[{"host":"-oProxyCommand=touch x","path":"/x"}]`,
		`[{"host":"box;id","path":"/x"}]`,
		`[{"host":"box host","path":"/x"}]`,
		`[{"host":"","path":"/x"}]`,
		`[{"host":"box","path":"dev"}]`,
		`[{"host":"box","path":"~/.ssh"}]`,
		`[{"id":"a","host":"box","path":"/x"},{"id":"a","host":"box","path":"/y"}]`,
	} {
		if _, err := parseRemotes(bad); err == nil {
			t.Errorf("parseRemotes(%s) was accepted", bad)
		}
	}
}

func TestRemoteGuard(t *testing.T) {
	ctx := context.Background()
	f := &fakeRunner{reply: func(op string, args []string, _ []byte) runResult {
		switch {
		case op == "stat" && args[0] == "notes/key-link.txt":
			// An ordinary name that is a link to a key.
			return runResult{out: []byte("f\t10\t0\t/srv/me/.ssh/id_ed25519")}
		case op == "stat":
			return runResult{out: []byte("f\t5\t0\t/srv/me/dev/" + args[0])}
		case op == "list":
			return runResult{out: []byte("/srv/me/.gnupg\x00")}
		}
		return runResult{code: 2}
	}}
	r := testRemote(f, "~/dev")

	// Refused here, before the host is asked anything.
	for _, rel := range []string{".env", "app/.env.production", ".ssh/config", "certs/site.pem", ".aws/credentials"} {
		if _, err := r.readText(ctx, rel, 1, 0, 1024); !errors.Is(err, errSensitive) {
			t.Errorf("read %s: %v", rel, err)
		}
	}
	for _, rel := range []string{"../x", "/etc/passwd", "a/../../b"} {
		if _, err := r.readText(ctx, rel, 1, 0, 1024); !errors.Is(err, errOutside) {
			t.Errorf("read %s: %v", rel, err)
		}
	}
	if _, _, err := r.writeText(ctx, ".netrc", "x", false); !errors.Is(err, errSensitive) {
		t.Errorf("write .netrc: %v", err)
	}
	if _, err := r.change(ctx, "delete", "id_rsa"); !errors.Is(err, errSensitive) {
		t.Errorf("delete id_rsa: %v", err)
	}
	if len(f.calls) != 0 {
		t.Fatalf("refused paths reached the host: %v", f.ops())
	}

	// Refused once the host says where a path really leads: nothing is read.
	if _, err := r.readText(ctx, "notes/key-link.txt", 1, 0, 1024); !errors.Is(err, errSensitive) {
		t.Errorf("read through a link to a key: %v", err)
	}
	if _, _, err := r.writeText(ctx, "notes/key-link.txt", "x", false); !errors.Is(err, errSensitive) {
		t.Errorf("write through a link to a key: %v", err)
	}
	if _, err := r.list(ctx, "gpg", false); !errors.Is(err, errSensitive) {
		t.Errorf("list a link to .gnupg: %v", err)
	}
	if ops := f.ops(); !slices.Equal(ops, []string{"stat", "stat", "list"}) {
		t.Errorf("calls = %v, want stat, stat, list and no read or write", ops)
	}
	for _, c := range f.calls {
		if c.host != "box" || c.argv[0] != "sh" || c.argv[1] != "-c" || c.argv[2] != remoteScript || c.argv[5] != "~/dev" {
			t.Errorf("call %v", c.argv[:2])
		}
	}
}

func TestRemoteExitCodes(t *testing.T) {
	ctx := context.Background()
	code := 0
	stderr := ""
	f := &fakeRunner{reply: func(string, []string, []byte) runResult { return runResult{code: code, stderr: stderr} }}
	r := testRemote(f, "/srv")
	for _, c := range []struct {
		code int
		want error
	}{{3, errNotFound}, {4, errOutside}, {5, errNotFound}, {7, errExists}, {12, errBinary}, {120, errRemote}, {255, errRemote}} {
		code, stderr = c.code, "warning\nPermission denied (publickey)."
		_, err := r.list(ctx, "x", false)
		if !errors.Is(err, c.want) {
			t.Errorf("exit %d: %v, want %v", c.code, err, c.want)
		}
		if c.code == 255 && !strings.Contains(err.Error(), "Permission denied") {
			t.Errorf("an ssh failure hides its reason: %v", err)
		}
	}
}

func TestParseListing(t *testing.T) {
	body := strings.Join([]string{
		"f\tf\t12\t1700000000.5\tb.md",
		"d\td\t4096\t1700000000\tSrc",
		"l\td\t9\t1700000000\tlinked",
		"l\tN\t9\t1700000000\tbroken",
		"f\tf\t3\t1700000000\t.hidden",
		"f\tf\t3\t1700000000\t.env",
		"f\tf\t3\t1700000000\tname\twith tab",
		"f\tf\t3\t17000", // cut short by the byte limit
	}, "\x00")
	l := rloc{Rel: "dev", Abs: "/srv/me/dev"}
	got := parseListing(l, []byte(body), false)
	var names []string
	for _, e := range got {
		names = append(names, e.Name)
	}
	if want := []string{"linked", "Src", "b.md", "broken", "name\twith tab"}; !slices.Equal(names, want) {
		t.Fatalf("names = %q, want %q", names, want)
	}
	if e := got[0]; !e.Dir || !e.Symlink || e.Size != 0 || e.Path != "dev/linked" {
		t.Errorf("link to a folder = %+v", e)
	}
	if e := got[2]; e.Size != 12 || e.Modified != "2023-11-14T22:13:20Z" || e.Dir {
		t.Errorf("file = %+v", e)
	}
	all := parseListing(l, []byte(body), true)
	if len(all) != 7 {
		t.Errorf("with hidden: %d entries", len(all))
	}
	for _, e := range all {
		if (e.Name == ".env") != e.Locked {
			t.Errorf("%s locked = %v", e.Name, e.Locked)
		}
	}
}

func TestRemoteReadBounds(t *testing.T) {
	ctx := context.Background()
	text := strings.Repeat("0123456789\n", 400) // 4400 bytes
	binary := false
	f := &fakeRunner{reply: func(op string, args []string, _ []byte) runResult {
		switch op {
		case "stat":
			n := "0"
			if binary {
				n = "3"
			}
			return runResult{out: []byte("f\t4400\t" + n + "\t/srv/" + args[0])}
		case "read":
			// args: rel, follow, want, offset, max bytes. Keep to the byte bound, as head -c does.
			if args[2] != "/srv/"+args[0] || args[1] != "1" {
				return runResult{code: 10}
			}
			body := text
			if args[3] == "3" {
				body = strings.Join(strings.SplitAfter(text, "\n")[2:], "")
			}
			if args[4] != "1025" {
				t.Errorf("max bytes asked = %s, want the limit and one", args[4])
			}
			return runResult{out: []byte("4400 0\n" + body[:min(len(body), 1025)])}
		}
		return runResult{code: 2}
	}}
	r := testRemote(f, "/srv")
	tx, err := r.readText(ctx, "a.txt", 1, 0, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if len(tx.Text) != 1024 || !tx.Truncated || tx.Size != 4400 || tx.StartLine != 1 || tx.EndLine != 94 {
		t.Errorf("read = %d bytes, truncated %v, size %d, lines %d-%d", len(tx.Text), tx.Truncated, tx.Size, tx.StartLine, tx.EndLine)
	}
	tx, err = r.readText(ctx, "a.txt", 3, 2, 1024)
	if err != nil || tx.Text != "0123456789\n0123456789\n" || tx.StartLine != 3 || tx.EndLine != 4 || !tx.Truncated {
		t.Errorf("read from line 3: %+v %v", tx, err)
	}
	binary = true
	if _, err := r.readText(ctx, "a.bin", 1, 0, 1024); !errors.Is(err, errBinary) {
		t.Errorf("binary: %v", err)
	}

	// A write over the bound is refused here; one under it carries its text on stdin.
	if _, _, err := r.writeText(ctx, "big.txt", strings.Repeat("x", maxBody+1), false); err == nil {
		t.Error("a write over the bound was sent")
	}
	f.calls = nil
	f.reply = func(op string, args []string, _ []byte) runResult {
		if op == "stat" {
			return runResult{out: []byte("m\t0\t0\t/srv/" + args[0])}
		}
		return runResult{out: []byte("created")}
	}
	if _, created, err := r.writeText(ctx, "new.txt", "hello", true); err != nil || !created {
		t.Fatalf("write: %v %v", created, err)
	}
	w := f.calls[1]
	if w.op != "write" || string(w.stdin) != "hello" || !slices.Equal(w.argv[6:], []string{"new.txt", "1", "/srv/new.txt", "1", "5"}) {
		t.Errorf("write call = %q stdin %q", w.argv[4:], w.stdin)
	}
}

func TestParseGrep(t *testing.T) {
	out := "/srv/me/dev/notes/a.md\x0012:a needle here\n" +
		"/srv/me/dev/notes/sub/.env\x001:needle=x\n" +
		"/elsewhere/b.md\x003:needle\n" +
		"/srv/me/dev/notes/b.md\x004:" + strings.Repeat("needle ", 100) + "\n" +
		"/srv/me/dev/notes/c.md\x005:needle\n"
	start := rloc{Rel: "notes", Abs: "/srv/me/dev/notes"}
	m, cut := parseGrep(start, []byte(out), 2)
	if len(m) != 2 || !cut || m[0] != (Match{Path: "notes/a.md", Line: 12, Text: "a needle here"}) || m[1].Path != "notes/b.md" || len(m[1].Text) > grepLineClip+4 {
		t.Errorf("matches = %+v, truncated %v", m, cut)
	}
	// A search of one file names that file.
	m, _ = parseGrep(rloc{Rel: "notes/a.md", Abs: "/srv/me/dev/notes/a.md"}, []byte("/srv/me/dev/notes/a.md\x001:x\n"), 5)
	if len(m) != 1 || m[0].Path != "notes/a.md" {
		t.Errorf("one file: %+v", m)
	}
}

// The remote script, run on this machine by sh. It needs GNU find (and GNU head for the
// search), as a Linux host has; where they are missing those parts are skipped.
func TestRemoteScript(t *testing.T) {
	if err := exec.Command("find", "/", "-maxdepth", "0", "-printf", "").Run(); err != nil {
		t.Skip("this machine's find has no -printf")
	}
	ctx := context.Background()
	home := tree(t, map[string]string{
		"dev/notes/a.md":   "one\ntwo needle\nthree\n",
		"dev/notes/b.txt":  "NEEDLE\n",
		"dev/bin.dat":      "a\x00b",
		"dev/empty/":       "",
		"dev/.hidden":      "h",
		"dev/sp ace/it's":  "quoted needle",
		"dev/.git/x":       "needle",
		".ssh/id_rsa":      "key",
		"dev/conf/.ssh/id": "key",
		"outside/s.txt":    "s",
		"dev/notes/.env.x": "needle",
	})
	dev := filepath.Join(home.Path, "dev")
	symlink(t, filepath.Join(home.Path, "outside"), filepath.Join(dev, "out"))
	symlink(t, filepath.Join(dev, "conf", ".ssh", "id"), filepath.Join(dev, "key.txt"))
	t.Setenv("HOME", home.Path)
	r := testRemote(localRunner{}, "~/dev")

	entries, lerr := r.list(ctx, "", false)
	if lerr != nil {
		t.Fatal(lerr)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name)
	}
	if want := []string{"conf", "empty", "notes", "out", "sp ace", "bin.dat", "key.txt"}; !slices.Equal(names, want) {
		t.Errorf("list = %q, want %q", names, want)
	}
	if _, err := r.list(ctx, "out", false); !errors.Is(err, errOutside) {
		t.Errorf("list through a link out of the root: %v", err)
	}
	if _, err := r.readText(ctx, "out/s.txt", 1, 0, 100); !errors.Is(err, errOutside) {
		t.Errorf("read through a link out of the root: %v", err)
	}
	if _, err := r.readText(ctx, "key.txt", 1, 0, 100); !errors.Is(err, errSensitive) {
		t.Errorf("read a link to a key: %v", err)
	}
	if _, err := r.readText(ctx, "../.ssh/id_rsa", 1, 0, 100); !errors.Is(err, errOutside) {
		t.Errorf("read out of the root: %v", err)
	}
	if _, err := r.readText(ctx, "bin.dat", 1, 0, 100); !errors.Is(err, errBinary) {
		t.Errorf("read binary: %v", err)
	}
	if _, err := r.readText(ctx, "nope.md", 1, 0, 100); !errors.Is(err, errNotFound) {
		t.Errorf("read missing: %v", err)
	}
	if tx, err := r.readText(ctx, "notes/a.md", 2, 1, 100); err != nil || tx.Text != "two needle\n" || !tx.Truncated || tx.Size != 21 {
		t.Errorf("read line 2: %+v %v", tx, err)
	}
	if tx, err := r.readText(ctx, "sp ace/it's", 1, 0, 5); err != nil || tx.Text != "quote" || !tx.Truncated {
		t.Errorf("read a quoted name, 5 bytes: %+v %v", tx, err)
	}

	// Changes.
	if _, created, err := r.writeText(ctx, "new/deep/n.md", "hi", false); err != nil || !created {
		t.Fatalf("create: %v %v", created, err)
	}
	if _, created, err := r.writeText(ctx, "new/deep/n.md", "hello", false); err != nil || created {
		t.Fatalf("replace: %v %v", created, err)
	}
	if b, _ := os.ReadFile(filepath.Join(dev, "new", "deep", "n.md")); string(b) != "hello" { //nolint:gosec // the test's own temporary folder
		t.Errorf("replaced content = %q", b)
	}
	if _, _, err := r.writeText(ctx, "new/deep/n.md", "x", true); !errors.Is(err, errExists) {
		t.Errorf("create_only over a file: %v", err)
	}
	if _, _, err := r.writeText(ctx, "bin.dat", "x", false); !errors.Is(err, errBinary) {
		t.Errorf("replace binary: %v", err)
	}
	if _, err := r.change(ctx, "mkdir", "made"); err != nil {
		t.Errorf("mkdir: %v", err)
	}
	if _, err := r.change(ctx, "mkdir", "made"); !errors.Is(err, errExists) {
		t.Errorf("mkdir again: %v", err)
	}
	if _, _, err := r.move(ctx, "new/deep/n.md", "made/m.md"); err != nil {
		t.Errorf("move: %v", err)
	}
	if _, _, err := r.move(ctx, "made/m.md", "notes/a.md"); !errors.Is(err, errExists) {
		t.Errorf("move onto a file: %v", err)
	}
	if _, _, err := r.move(ctx, "made", "made/inner"); err == nil {
		t.Error("moved a folder into itself")
	}
	if _, err := r.change(ctx, "delete", "made"); err == nil {
		t.Error("deleted a folder that is not empty")
	}
	if _, err := r.change(ctx, "delete", "made/m.md"); err != nil {
		t.Errorf("delete: %v", err)
	}
	if _, err := r.change(ctx, "delete", "made"); err != nil {
		t.Errorf("delete the empty folder: %v", err)
	}
	if _, err := r.change(ctx, "delete", "out"); err != nil {
		t.Errorf("delete a link (the link, not its target): %v", err)
	}
	if _, err := os.Stat(filepath.Join(home.Path, "outside/s.txt")); err != nil {
		t.Errorf("deleting the link touched its target: %v", err)
	}

	if err := exec.Command("sh", "-c", "head -z -n 1 </dev/null").Run(); err != nil {
		t.Log("this machine's head has no -z: the search is not run")
		return
	}
	res, err := r.grep(ctx, "", grepQuery{Query: "needle"})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, m := range res.Matches {
		got = append(got, m.Path)
	}
	slices.Sort(got)
	if want := []string{"notes/a.md", "notes/b.txt", "sp ace/it's"}; !slices.Equal(got, want) || res.Files == 0 {
		t.Errorf("grep = %q (%d files), want %q", got, res.Files, want)
	}
	if res, err := r.grep(ctx, "notes", grepQuery{Query: "^two", Regexp: true}); err != nil || len(res.Matches) != 1 || res.Matches[0].Line != 2 {
		t.Errorf("grep regexp: %+v %v", res, err)
	}
	if res, err := r.grep(ctx, "", grepQuery{Query: "needle", Glob: "*.txt"}); err != nil || len(res.Matches) != 1 {
		t.Errorf("grep glob: %+v %v", res, err)
	}
	if res, err := r.grep(ctx, "", grepQuery{Query: "needle", MaxMatches: 1}); err != nil || len(res.Matches) != 1 || !res.Truncated {
		t.Errorf("grep limit: %+v %v", res, err)
	}
}

// A remote place through the module: the setting, the places, the tools and the routes.
func TestRemoteModule(t *testing.T) {
	f := &fakeRunner{reply: func(op string, args []string, _ []byte) runResult {
		switch op {
		case "stat":
			return runResult{out: []byte("f\t3\t0\t/srv/box/" + args[0])}
		case "list":
			return runResult{out: []byte("/srv/box\x00f\tf\t3\t1700000000\ta.md\x00")}
		case "read":
			return runResult{out: []byte("3 0\nabc")}
		}
		return runResult{code: 2}
	}}
	m := &mod{runner: f}
	home := tree(t, nil)
	t.Setenv("HOME", home.Path)
	h := moduletest.Start(t, m)
	ctx := context.Background()

	if out, _ := h.Tool("files_places", "{}"); len(out.([]Place)) != 1 {
		t.Fatalf("places with no remotes: %v", out)
	}
	if _, err := h.Tool("files_list", `{"place":"remote:box"}`); !errors.Is(err, errNotFound) {
		t.Errorf("a remote not in the setting: %v", err)
	}
	if err := h.Settings.Set(ctx, "remotes", `[{"id":"box","label":"box","host":"box","path":"/srv/box"}]`); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(h.Bus.Topics(), topic) {
		t.Error("changing the remotes published nothing for the views")
	}
	out, err := h.Tool("files_places", "{}")
	if ps := out.([]Place); err != nil || len(ps) != 2 || ps[1].ID != "remote:box" || !ps[1].Remote {
		t.Fatalf("places: %+v %v", out, err)
	}
	list, err := h.Tool("files_list", `{"place":"remote:box","path":""}`)
	if err != nil || list.(map[string]any)["count"] != 1 {
		t.Errorf("files_list: %v %v", list, err)
	}
	read, err := h.Tool("files_read", `{"place":"remote:box","path":"a.md"}`)
	if raw, _ := json.Marshal(read); err != nil || !bytes.Contains(raw, []byte(`"text":"abc"`)) {
		t.Errorf("files_read: %s %v", raw, err)
	}
	if _, err := h.Tool("files_read", `{"place":"remote:box","path":".ssh/id_rsa"}`); !errors.Is(err, errSensitive) {
		t.Errorf("files_read a key: %v", err)
	}
	if status, body := h.Do("GET", "/read?place=remote:box&path=a.md", ""); status != http.StatusOK || !strings.Contains(body, "abc") {
		t.Errorf("GET /read: %d %s", status, body)
	}
	if status, _ := h.Do("DELETE", "/places/remote:box", ""); status == http.StatusOK {
		t.Error("a remote place was removed through the store")
	}
	if _, err := h.RunCommand(ctx, "files.path", map[string]any{"place": "remote:box", "path": "a.md"}); err == nil {
		t.Error("files.path gave a local path for a remote file")
	}
	for _, c := range f.calls {
		if c.op != "stat" && c.op != "list" && c.op != "read" {
			t.Errorf("a read-only test ran %s", c.op)
		}
	}
	// A setting that does not parse leaves the local places.
	if err := h.Settings.Set(ctx, "remotes", `[{"host":"-x"}]`); err != nil {
		t.Fatal(err)
	}
	if out, err := h.Tool("files_places", "{}"); err != nil || len(out.([]Place)) != 1 {
		t.Errorf("places with a bad setting: %v %v", out, err)
	}
}
