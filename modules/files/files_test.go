package files

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hollis-labs/surface/module"
	"github.com/hollis-labs/surface/module/moduletest"
)

// tree makes a place over a temporary directory holding the given files (a name ending in
// "/" is a folder). The root is returned with its links resolved, as locate reports paths.
func tree(t *testing.T, files map[string]string) Place {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if strings.HasSuffix(name, "/") {
			if err := os.MkdirAll(p, 0o700); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return Place{ID: "t", Name: "Test", Path: root}
}

func mustLocate(t *testing.T, p Place, rel string, follow bool) loc {
	t.Helper()
	l, err := locate(p, rel, follow)
	if err != nil {
		t.Fatalf("locate %q: %v", rel, err)
	}
	return l
}

func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("no symbolic links here: %v", err)
	}
}

func TestSensitive(t *testing.T) {
	refused := []string{
		".ssh/config", "/srv/x/.ssh/known_hosts", ".gnupg/pubring.kbx", ".SSH/ID_RSA",
		".aws/credentials", "/srv/x/.aws/credentials", ".kube/config", ".docker/config.json",
		"certs/server.pem", "tls/site.key", "vault.kdbx", "bundle.p12",
		"id_rsa", "keys/id_rsa.pub", "id_ed25519", "id_ecdsa.old",
		".env", "app/.env", ".env.production", ".netrc", ".npmrc", ".pgpass",
		".config/gh/token.json", ".local/share/app/client_secret", ".config/app/Secrets.yaml",
	}
	for _, p := range refused {
		if !sensitive(p) {
			t.Errorf("sensitive(%q) = false, want true", p)
		}
	}
	allowed := []string{
		"", "notes/todo.md", "src/token.go", "docs/secrets.md", "tokens/readme.txt",
		".env.example", ".env.sample", "environment.md", "keynote.txt", "monkey.png",
		".config/app/settings.json", ".github/workflows/ci.yml", "aws/credentials.md", "ssh/notes.txt",
	}
	for _, p := range allowed {
		if sensitive(p) {
			t.Errorf("sensitive(%q) = true, want false", p)
		}
	}
}

func TestLocate(t *testing.T) {
	p := tree(t, map[string]string{"docs/a.md": "a", "docs/sub/": "", ".ssh/config": "x", "app/.env": "x"})
	outside := tree(t, map[string]string{"private.txt": "private", "dir/in.txt": "in"})

	t.Run("inside", func(t *testing.T) {
		for rel, want := range map[string]string{
			"": "", ".": "", "/": "", "docs": "docs", "docs/": "docs", "./docs//a.md": "docs/a.md",
			"docs/sub/../a.md": "docs/a.md", "docs/new/deep/file.txt": "docs/new/deep/file.txt",
		} {
			l := mustLocate(t, p, rel, true)
			if l.Rel != want || l.Abs != filepath.Join(p.Path, filepath.FromSlash(want)) {
				t.Errorf("locate(%q) = %q at %s, want %q", rel, l.Rel, l.Abs, want)
			}
		}
	})

	t.Run("traversal", func(t *testing.T) {
		for _, rel := range []string{"..", "../x", "docs/../../x", "docs/../..", "/etc/passwd", "../" + filepath.Base(p.Path) + "/docs"} {
			if _, err := locate(p, rel, true); !errors.Is(err, errOutside) {
				t.Errorf("locate(%q): %v, want errOutside", rel, err)
			}
		}
	})

	t.Run("symlink escape", func(t *testing.T) {
		symlink(t, filepath.Join(outside.Path, "private.txt"), filepath.Join(p.Path, "leak.txt"))
		symlink(t, filepath.Join(outside.Path, "dir"), filepath.Join(p.Path, "leakdir"))
		symlink(t, filepath.Join(p.Path, "docs"), filepath.Join(p.Path, "shortcut"))
		symlink(t, filepath.Join(outside.Path, "missing"), filepath.Join(p.Path, "dangling"))

		for _, rel := range []string{"leak.txt", "leakdir", "leakdir/in.txt", "leakdir/new.txt"} {
			if _, err := locate(p, rel, true); !errors.Is(err, errOutside) {
				t.Errorf("locate(%q): %v, want errOutside", rel, err)
			}
		}
		// A link that stays inside resolves to where it leads.
		if l := mustLocate(t, p, "shortcut/a.md", true); l.Abs != filepath.Join(p.Path, "docs", "a.md") {
			t.Errorf("link inside resolved to %s", l.Abs)
		}
		// Creating through a link whose target is missing would write outside.
		if _, err := locate(p, "dangling", true); err == nil {
			t.Error("a broken link was accepted as a path to write")
		}
		// Without follow the link itself is named (to rename or delete it), but a path
		// through an escaping folder link is still refused.
		if l := mustLocate(t, p, "leak.txt", false); l.Abs != filepath.Join(p.Path, "leak.txt") {
			t.Errorf("unfollowed link resolved to %s", l.Abs)
		}
		if _, err := locate(p, "leakdir/in.txt", false); !errors.Is(err, errOutside) {
			t.Errorf("unfollowed path through an escaping link: %v", err)
		}
	})

	t.Run("denylist", func(t *testing.T) {
		symlink(t, filepath.Join(p.Path, "app", ".env"), filepath.Join(p.Path, "settings.txt"))
		for _, rel := range []string{".ssh", ".ssh/config", "app/.env", "settings.txt", "new/id_rsa"} {
			if _, err := locate(p, rel, true); !errors.Is(err, errSensitive) {
				t.Errorf("locate(%q): %v, want errSensitive", rel, err)
			}
		}
	})

	t.Run("missing root", func(t *testing.T) {
		gone := Place{ID: "gone", Name: "Gone", Path: filepath.Join(p.Path, "nope")}
		if _, err := locate(gone, "", true); !errors.Is(err, errNotFound) {
			t.Errorf("missing root: %v", err)
		}
	})
}

func TestList(t *testing.T) {
	p := tree(t, map[string]string{"b.txt": "bb", "A.txt": "a", "zeta/": "", "Alpha/": "", ".hidden": "h", ".env": "x"})
	root := mustLocate(t, p, "", true)

	got, err := list(root, false)
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, e := range got {
		order = append(order, e.Name)
	}
	if strings.Join(order, ",") != "Alpha,zeta,A.txt,b.txt" {
		t.Errorf("order = %v", order)
	}
	if got[3].Size != 2 || got[3].Path != "b.txt" || got[0].Dir != true || got[3].Modified == "" {
		t.Errorf("entry = %+v", got[3])
	}

	all, _ := list(root, true)
	locked := map[string]bool{}
	for _, e := range all {
		locked[e.Name] = e.Locked
	}
	if len(all) != 6 || !locked[".env"] || locked[".hidden"] {
		t.Errorf("hidden listing: %d entries, locked = %v", len(all), locked)
	}

	if _, err = list(mustLocate(t, p, "nope", true), false); !errors.Is(err, errNotFound) {
		t.Errorf("missing folder: %v", err)
	}
	ns, total, err := names(root, false, 3)
	if err != nil || total != 4 || len(ns) != 3 || ns[0] != "A.txt" || ns[1] != "Alpha/" {
		t.Errorf("names = %v, %d, %v", ns, total, err)
	}
}

func TestReadText(t *testing.T) {
	p := tree(t, map[string]string{
		"five.txt": "one\ntwo\nthree\nfour\nfive",
		"big.txt":  strings.Repeat("0123456789abcde\n", 100),
		"wide.txt": "héllo",
		"bin.dat":  "PK\x00\x01\x02",
		"dir/":     "",
	})
	read := func(rel string, offset, limit int, maxBytes int64) Text {
		t.Helper()
		got, err := readText(mustLocate(t, p, rel, true), offset, limit, maxBytes)
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		return got
	}

	if got := read("five.txt", 0, 0, 1<<20); got.Text != "one\ntwo\nthree\nfour\nfive" || got.Truncated || got.Size != 23 || got.StartLine != 1 || got.EndLine != 5 {
		t.Errorf("whole file: %+v", got)
	}
	if got := read("five.txt", 2, 2, 1<<20); got.Text != "two\nthree\n" || !got.Truncated || got.StartLine != 2 || got.EndLine != 3 {
		t.Errorf("lines 2-3: %+v", got)
	}
	if got := read("five.txt", 4, 2, 1<<20); got.Text != "four\nfive" || got.Truncated {
		t.Errorf("the last lines: %+v", got)
	}
	if got := read("five.txt", 9, 0, 1<<20); got.Text != "" || got.Truncated || got.EndLine != 0 {
		t.Errorf("past the end: %+v", got)
	}
	if got := read("big.txt", 1, 0, 40); len(got.Text) != 40 || !got.Truncated || got.Size != 1600 || got.EndLine != 3 {
		t.Errorf("byte limit: %d bytes, %+v", len(got.Text), got)
	}
	// A byte limit that falls inside a character drops the half.
	if got := read("wide.txt", 1, 0, 2); got.Text != "h" || !got.Truncated {
		t.Errorf("cut character: %+v", got)
	}
	if _, err := readText(mustLocate(t, p, "bin.dat", true), 1, 0, 1<<20); !errors.Is(err, errBinary) {
		t.Errorf("binary: %v", err)
	}
	if _, err := readText(mustLocate(t, p, "dir", true), 1, 0, 1<<20); err == nil {
		t.Error("a folder was read as text")
	}
	if _, err := readText(mustLocate(t, p, "nope.txt", true), 1, 0, 1<<20); !errors.Is(err, errNotFound) {
		t.Errorf("missing: %v", err)
	}
}

func TestWriteText(t *testing.T) {
	p := tree(t, map[string]string{"a.txt": "old", "bin.dat": "\x00\x01", "dir/": ""})
	content := func(rel string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(p.Path, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}

	if created, err := writeText(mustLocate(t, p, "new/deep/n.txt", true), "hello", false); err != nil || !created || content("new/deep/n.txt") != "hello" {
		t.Errorf("create: %v, %v", created, err)
	}
	if err := os.Chmod(filepath.Join(p.Path, "a.txt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if created, err := writeText(mustLocate(t, p, "a.txt", true), "new", false); err != nil || created || content("a.txt") != "new" {
		t.Errorf("replace: %v, %v", created, err)
	}
	if fi, _ := os.Stat(filepath.Join(p.Path, "a.txt")); fi.Mode().Perm() != 0o600 {
		t.Errorf("replace changed the permissions to %v", fi.Mode().Perm())
	}
	if des, _ := os.ReadDir(p.Path); len(des) != 4 {
		t.Errorf("replace left a temporary file behind: %v", des)
	}
	if _, err := writeText(mustLocate(t, p, "a.txt", true), "again", true); !errors.Is(err, errExists) || content("a.txt") != "new" {
		t.Errorf("create_only over a file: %v", err)
	}
	if _, err := writeText(mustLocate(t, p, "bin.dat", true), "text", false); !errors.Is(err, errBinary) || content("bin.dat") != "\x00\x01" {
		t.Errorf("replace a binary file: %v", err)
	}
	if _, err := writeText(mustLocate(t, p, "dir", true), "text", false); err == nil {
		t.Error("a folder was replaced by a file")
	}
	if _, err := writeText(mustLocate(t, p, "", true), "text", false); err == nil {
		t.Error("the place root was written as a file")
	}

	// A write through a link that stays inside replaces the target and keeps the link.
	symlink(t, filepath.Join(p.Path, "a.txt"), filepath.Join(p.Path, "link.txt"))
	if _, err := writeText(mustLocate(t, p, "link.txt", true), "via link", false); err != nil || content("a.txt") != "via link" {
		t.Errorf("write through a link: %v", err)
	}
	if fi, _ := os.Lstat(filepath.Join(p.Path, "link.txt")); fi.Mode()&os.ModeSymlink == 0 {
		t.Error("the link was replaced by a file")
	}
}

func TestMakeDirMoveRemove(t *testing.T) {
	p := tree(t, map[string]string{"a.txt": "a", "b.txt": "b", "full/x.txt": "x", "empty/": ""})
	exists := func(rel string) bool {
		_, err := os.Lstat(filepath.Join(p.Path, filepath.FromSlash(rel)))
		return err == nil
	}
	at := func(rel string) loc { return mustLocate(t, p, rel, false) }

	if err := makeDir(at("made/inner")); err != nil || !exists("made/inner") {
		t.Errorf("mkdir: %v", err)
	}
	if err := makeDir(at("a.txt")); !errors.Is(err, errExists) {
		t.Errorf("mkdir over a file: %v", err)
	}

	if err := move(at("a.txt"), at("b.txt")); !errors.Is(err, errExists) || !exists("a.txt") {
		t.Errorf("move onto a file: %v", err)
	}
	if err := move(at("a.txt"), at("empty")); !errors.Is(err, errExists) {
		t.Errorf("move onto a folder: %v", err)
	}
	if err := move(at("a.txt"), at("a.txt")); !errors.Is(err, errExists) {
		t.Errorf("move onto itself: %v", err)
	}
	if err := move(at("a.txt"), at("nowhere/a.txt")); !errors.Is(err, errNotFound) {
		t.Errorf("move into a missing folder: %v", err)
	}
	if err := move(at("full"), at("full/inside")); err == nil {
		t.Error("a folder was moved into itself")
	}
	if err := move(at("missing.txt"), at("c.txt")); !errors.Is(err, errNotFound) {
		t.Errorf("move a missing file: %v", err)
	}
	if err := move(at(""), at("c")); err == nil {
		t.Error("the place root was moved")
	}
	if err := move(at("a.txt"), at("full/renamed.txt")); err != nil || exists("a.txt") || !exists("full/renamed.txt") {
		t.Errorf("move: %v", err)
	}

	if err := remove(at("full")); err == nil || !exists("full/x.txt") {
		t.Errorf("a folder with files in it was deleted: %v", err)
	}
	if err := remove(at("")); err == nil {
		t.Error("the place root was deleted")
	}
	if err := remove(at("missing.txt")); !errors.Is(err, errNotFound) {
		t.Errorf("delete a missing file: %v", err)
	}
	if err := remove(at("b.txt")); err != nil || exists("b.txt") {
		t.Errorf("delete a file: %v", err)
	}
	if err := remove(at("empty")); err != nil || exists("empty") {
		t.Errorf("delete an empty folder: %v", err)
	}

	// Deleting a link removes the link and leaves what it points at.
	symlink(t, filepath.Join(p.Path, "full"), filepath.Join(p.Path, "link"))
	if err := remove(at("link")); err != nil || exists("link") || !exists("full/x.txt") {
		t.Errorf("delete a link: %v", err)
	}
}

func TestGrep(t *testing.T) {
	p := tree(t, map[string]string{
		"a.md":                  "first line\nNeedle here\nlast",
		"src/b.go":              "package b // needle\n",
		"src/deep/c.txt":        "needle three",
		".git/config":           "needle in git",
		"node_modules/x/y.js":   "needle in deps",
		"bin.dat":               "needle\x00",
		"huge.txt":              "needle\n" + strings.Repeat("x", grepMaxFileSize),
		"app/.env":              "NEEDLE=1",
		"one/two/three/four.md": "needle four",
	})
	root := mustLocate(t, p, "", true)
	paths := func(r grepResult) string {
		var out []string
		for _, m := range r.Matches {
			out = append(out, m.Path)
		}
		return strings.Join(out, ",")
	}

	r, err := grep(context.Background(), root, grepQuery{Query: "needle"})
	if err != nil {
		t.Fatal(err)
	}
	if got := paths(r); got != "a.md,one/two/three/four.md,src/b.go,src/deep/c.txt" || r.Truncated {
		t.Errorf("matches = %s (truncated %v)", got, r.Truncated)
	}
	if m := r.Matches[0]; m.Line != 2 || m.Text != "Needle here" {
		t.Errorf("first match = %+v", m)
	}

	if r, _ := grep(context.Background(), root, grepQuery{Query: `^need\w+ (three|four)$`, Regexp: true}); paths(r) != "one/two/three/four.md,src/deep/c.txt" {
		t.Errorf("regexp matches = %s", paths(r))
	}
	if r, _ := grep(context.Background(), root, grepQuery{Query: "needle", Glob: "*.go"}); paths(r) != "src/b.go" {
		t.Errorf("glob matches = %s", paths(r))
	}
	if r, _ := grep(context.Background(), root, grepQuery{Query: "needle", MaxDepth: 1}); paths(r) != "a.md,src/b.go" {
		t.Errorf("depth 1 matches = %s", paths(r))
	}
	if r, _ := grep(context.Background(), root, grepQuery{Query: "needle", MaxMatches: 2}); len(r.Matches) != 2 || !r.Truncated {
		t.Errorf("limit 2: %d matches, truncated %v", len(r.Matches), r.Truncated)
	}
	if r, _ := grep(context.Background(), mustLocate(t, p, "src/b.go", true), grepQuery{Query: "needle"}); paths(r) != "src/b.go" {
		t.Errorf("one file: %s", paths(r))
	}
	if _, err := grep(context.Background(), root, grepQuery{Query: "(", Regexp: true}); err == nil {
		t.Error("a bad regular expression was accepted")
	}
}

// start runs the module against a temporary home directory, so the place made on first
// start is a test folder and not the real one.
func start(t *testing.T, files map[string]string) (*moduletest.Host, Place) {
	t.Helper()
	home := tree(t, files)
	t.Setenv("HOME", home.Path)
	t.Setenv("USERPROFILE", home.Path)
	h := moduletest.Start(t, &mod{})
	var places []Place
	status, body := h.Do("GET", "/places", "")
	if err := json.Unmarshal([]byte(body), &places); err != nil || status != http.StatusOK || len(places) != 1 {
		t.Fatalf("places after first start: %d %s", status, body)
	}
	if places[0].ID != "home" || places[0].Path != home.Path {
		t.Fatalf("first place = %+v, want home at %s", places[0], home.Path)
	}
	return h, places[0]
}

func TestRoutes(t *testing.T) {
	h, _ := start(t, map[string]string{"notes/a.md": "alpha", "notes/pic.png": "\x89PNG\x00", ".ssh/id_rsa": "key", ".dot": "d"})
	do := func(method, p, body string, want int) map[string]any {
		t.Helper()
		status, out := h.Do(method, p, body)
		if status != want {
			t.Fatalf("%s %s = %d %s, want %d", method, p, status, out, want)
		}
		var v map[string]any
		_ = json.Unmarshal([]byte(out), &v)
		return v
	}

	if v := do("GET", "/list?place=home&path=", "", 200); len(v["entries"].([]any)) != 1 {
		t.Errorf("list hides dotfiles by default: %v", v)
	}
	// The setting is live: the next listing shows them.
	if err := h.Settings.Set(context.Background(), "show_hidden", true); err != nil {
		t.Fatal(err)
	}
	if v := do("GET", "/list?place=home&path=", "", 200); len(v["entries"].([]any)) != 3 {
		t.Errorf("list with show_hidden: %v", v)
	}

	if v := do("GET", "/read?place=home&path=notes/a.md", "", 200); v["text"] != "alpha" || v["truncated"] != false || v["size"] != float64(5) {
		t.Errorf("read: %v", v)
	}
	if err := h.Settings.Set(context.Background(), "preview_limit_kb", 1); err != nil {
		t.Fatal(err)
	}
	do("POST", "/write", `{"place":"home","path":"notes/long.txt","text":"`+strings.Repeat("x", 3000)+`"}`, 200)
	if v := do("GET", "/read?place=home&path=notes/long.txt", "", 200); len(v["text"].(string)) != 1024 || v["truncated"] != true {
		t.Errorf("read past the preview limit: %d bytes, truncated %v", len(v["text"].(string)), v["truncated"])
	}

	do("GET", "/read?place=home&path=.ssh/id_rsa", "", http.StatusForbidden)
	do("GET", "/raw?place=home&path=.ssh/id_rsa", "", http.StatusForbidden)
	do("GET", "/read?place=home&path=../x", "", http.StatusForbidden)
	do("GET", "/list?place=nope&path=", "", http.StatusNotFound)
	do("GET", "/read?place=home&path=notes/pic.png", "", http.StatusUnprocessableEntity)
	if status, body := h.Do("GET", "/raw?place=home&path=notes/pic.png", ""); status != 200 || body != "\x89PNG\x00" {
		t.Errorf("raw = %d %q", status, body)
	}

	do("POST", "/mkdir", `{"place":"home","path":"notes/sub"}`, 200)
	do("POST", "/mkdir", `{"place":"home","path":"notes/sub"}`, http.StatusConflict)
	do("POST", "/write", `{"place":"home","path":"notes/a.md","text":"x","create_only":true}`, http.StatusConflict)
	do("POST", "/write", `{"place":"home","path":"notes/.env","text":"x"}`, http.StatusForbidden)
	do("POST", "/move", `{"place":"home","from":"notes/a.md","to":"notes/long.txt"}`, http.StatusConflict)
	do("POST", "/move", `{"place":"home","from":"notes/a.md","to":"notes/sub/a.md"}`, 200)
	do("POST", "/delete", `{"place":"home","path":"notes"}`, http.StatusUnprocessableEntity)
	do("POST", "/delete", `{"place":"home","path":"notes/sub/a.md"}`, 200)
	do("POST", "/delete", `{"place":"home","path":"notes/sub"}`, 200)
	do("POST", "/mkdir", `not json`, http.StatusBadRequest)

	// Places: one is added for a folder, and removing it leaves the folder alone.
	extra := tree(t, map[string]string{"keep.txt": "keep"})
	added := do("POST", "/places", `{"name":"Work Notes","path":"`+extra.Path+`"}`, 200)
	if added["id"] != "work-notes" {
		t.Errorf("added place = %v", added)
	}
	if again := do("POST", "/places", `{"name":"Work Notes","path":"`+extra.Path+`"}`, 200); again["id"] != "work-notes-2" {
		t.Errorf("second place of the same name = %v", again)
	}
	do("POST", "/places", `{"name":"x","path":"relative/dir"}`, http.StatusUnprocessableEntity)
	do("POST", "/places", `{"name":"x","path":"`+filepath.Join(extra.Path, "missing")+`"}`, http.StatusUnprocessableEntity)
	do("GET", "/read?place=work-notes&path=keep.txt", "", 200)
	do("DELETE", "/places/work-notes", "", 200)
	do("DELETE", "/places/work-notes", "", http.StatusNotFound)
	do("GET", "/read?place=work-notes&path=keep.txt", "", http.StatusNotFound)
	if _, err := os.Stat(filepath.Join(extra.Path, "keep.txt")); err != nil {
		t.Errorf("removing a place touched its files: %v", err)
	}

	if !strings.Contains(strings.Join(h.Bus.Topics(), ","), topic) {
		t.Errorf("no %s was published: %v", topic, h.Bus.Topics())
	}
}

func TestTools(t *testing.T) {
	h, home := start(t, map[string]string{"docs/a.md": "one\ntwo\nthree\n", "docs/b.txt": "find me", "top.txt": "t", ".aws/credentials": "k"})
	ctx := context.Background()
	call := func(name, input string) map[string]any {
		t.Helper()
		out, err := h.Tool(name, input)
		if err != nil {
			t.Fatalf("%s %s: %v", name, input, err)
		}
		raw, _ := json.Marshal(out)
		var v map[string]any
		_ = json.Unmarshal(raw, &v)
		return v
	}
	fails := func(name, input string, want error) {
		t.Helper()
		if _, err := h.Tool(name, input); err == nil || (want != nil && !errors.Is(err, want)) {
			t.Errorf("%s %s: %v, want %v", name, input, err, want)
		}
	}

	if out, err := h.Tool("files_places", ""); err != nil || len(out.([]Place)) != 1 {
		t.Fatalf("files_places: %v %v", out, err)
	}

	// With no Files window in front there is no folder to default to.
	fails("files_list", `{}`, nil)
	if v := call("files_list", `{"place":"home"}`); v["count"] != float64(2) {
		t.Errorf("list of the root: %v", v)
	}

	// files_open takes the main window to a file's folder and selects the file.
	opened := call("files_open", `{"place":"home","path":"docs/a.md"}`)
	if opened["path"] != "docs" || opened["selected"] != "docs/a.md" {
		t.Errorf("files_open: %v", opened)
	}
	if v := call("files_list", `{}`); v["path"] != "docs" || v["count"] != float64(2) {
		t.Errorf("list of the folder in front: %v", v)
	}
	if v := call("files_open", `{"place":"home","path":""}`); v["window"] != opened["window"] || v["path"] != "" {
		t.Errorf("files_open reuses the main window: %v", v)
	}
	if w, _ := h.DeskState.Focused(ctx); w.Args["path"] != "" || w.Args["select"] != nil {
		t.Errorf("the main window was not taken to the root: %v", w.Args)
	}
	fails("files_open", `{"place":"home","path":"nope"}`, errNotFound)

	if v := call("files_read", `{"path":"docs/a.md","offset":2,"limit":1}`); v["text"] != "two\n" || v["truncated"] != true || v["end_line"] != float64(2) {
		t.Errorf("files_read: %v", v)
	}
	fails("files_read", `{"place":"home","path":".aws/credentials"}`, errSensitive)
	fails("files_read", `{"place":"home","path":"../../etc/passwd"}`, errOutside)

	if v := call("files_grep", `{"query":"FIND"}`); len(v["matches"].([]any)) != 1 {
		t.Errorf("files_grep: %v", v)
	}

	before := len(h.Bus.Published)
	if v := call("files_write", `{"place":"home","path":"docs/new.md","text":"# new"}`); v["created"] != true {
		t.Errorf("files_write: %v", v)
	}
	if v := call("files_write", `{"place":"home","path":"docs/new.md","text":"# newer"}`); v["created"] != false {
		t.Errorf("files_write over a text file: %v", v)
	}
	fails("files_write", `{"place":"home","path":"docs/new.md","text":"x","create_only":true}`, errExists)
	fails("files_write", `{"place":"home","path":"docs/new.md"}`, nil)
	call("files_mkdir", `{"place":"home","path":"docs/archive"}`)
	fails("files_move", `{"place":"home","from":"docs/new.md","to":"docs/b.txt"}`, errExists)
	call("files_move", `{"place":"home","from":"docs/new.md","to":"docs/archive/new.md"}`)
	fails("files_delete", `{"place":"home","path":"docs"}`, nil)
	call("files_delete", `{"place":"home","path":"docs/archive/new.md"}`)
	if b, err := os.ReadFile(filepath.Join(home.Path, "docs", "b.txt")); err != nil || string(b) != "find me" {
		t.Errorf("a refused move changed its destination: %q %v", b, err)
	}

	// Every change is announced with the place and path.
	var last map[string]string
	events := h.Bus.Published[before:]
	if len(events) != 6 {
		t.Fatalf("%d events for 5 changes (a move announces both ends), want 6", len(events))
	}
	_ = json.Unmarshal(events[len(events)-1].Data, &last)
	if events[len(events)-1].Topic != topic || last["place"] != "home" || last["path"] != "docs/archive/new.md" {
		t.Errorf("last event = %s %v", events[len(events)-1].Topic, last)
	}
}

func TestCommandsAndContext(t *testing.T) {
	h, home := start(t, map[string]string{"docs/a.md": "a", "docs/b.md": "b", ".ssh/id_rsa": "k"})
	ctx := context.Background()

	// files.path gives another module the real path, after the same checks.
	out, err := h.RunCommand(ctx, "files.path", map[string]any{"place": "home", "path": "docs/a.md"})
	if err != nil || out.(map[string]string)["path"] != filepath.Join(home.Path, "docs", "a.md") {
		t.Errorf("files.path = %v, %v", out, err)
	}
	if _, err = h.RunCommand(ctx, "files.path", map[string]any{"place": "home", "path": ".ssh/id_rsa"}); !errors.Is(err, errSensitive) {
		t.Errorf("files.path on a credential: %v", err)
	}
	if _, err = h.RunCommand(ctx, "files.path", map[string]any{"place": "home", "path": "../up"}); !errors.Is(err, errOutside) {
		t.Errorf("files.path outside the place: %v", err)
	}

	out, err = h.RunCommand(ctx, "files.open", map[string]any{"place": "home", "path": "docs"})
	if err != nil {
		t.Fatal(err)
	}
	w := out.(module.Window)
	man := h.Manifest()
	wc, err := man.Views[0].Describe(ctx, h, w)
	if err != nil || wc.Kind != "folder" || wc.Ref != "files:home:docs" || wc.Extra["entries"] != 2 || wc.Extra["path"] != "docs" {
		t.Errorf("describe a folder: %+v, %v", wc, err)
	}

	// The page reports the selected file; the window is then about that file.
	w.State = map[string]any{"selection": "docs/a.md"}
	wc, _ = man.Views[0].Describe(ctx, h, w)
	if wc.Kind != "file" || wc.Ref != "files:home:docs/a.md" || wc.Extra["place"] != "home" {
		t.Errorf("describe a selected file: %+v", wc)
	}

	wc.View = viewID
	know, err := man.Slots[0].Knowledge(ctx, h, module.Situation{Focused: &wc})
	if err != nil || !strings.Contains(know, "- a.md\n- b.md\n") || !strings.Contains(know, `"docs/a.md"`) || !strings.Contains(know, "2 items") {
		t.Errorf("knowledge = %q, %v", know, err)
	}
	if know, _ := man.Slots[0].Knowledge(ctx, h, module.Situation{}); know != "" {
		t.Errorf("knowledge with nothing in front = %q", know)
	}
}
