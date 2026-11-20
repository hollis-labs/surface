package workbench

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportImportRoundTripKeepsTheDigest(t *testing.T) {
	m, h := start(t)
	if _, err := m.create("trip", "", "embedded", "owner"); err != nil {
		t.Fatal(err)
	}
	before, _ := m.lib.Tree("trip")
	data, name, err := m.export("trip")
	if err != nil {
		t.Fatal(err)
	}
	if name != "trip-v1.tar.gz" {
		t.Fatal(name)
	}
	b, err := readArchive(data)
	if err != nil {
		t.Fatal(err)
	}
	if b.Manifest == nil || b.Manifest.Digest != before.Digest() || b.Manifest.WorkbenchVersion != WorkbenchVersion || len(b.Suites) != 1 {
		t.Fatalf("manifest: %+v suites %d", b.Manifest, len(b.Suites))
	}
	// Through the route, as the page uploads it.
	body, _ := json.Marshal(map[string]string{"name": name, "data": base64.StdEncoding.EncodeToString(data)})
	code, out := doJSON(h, "POST", "/import", string(body))
	if code != 201 || !strings.Contains(out, `"id":"trip-2"`) {
		t.Fatal(code, out)
	}
	after, _ := m.lib.Tree("trip-2")
	if after.Digest() != before.Digest() {
		t.Fatal("the digest changed on the round trip")
	}
	if rb, ra := Validate(before), Validate(after); rb.AgentdirDigest != ra.AgentdirDigest {
		t.Fatal("agentdir digest changed")
	}
	suites, _ := m.run.Suites("trip-2")
	if len(suites) != 1 || suites[0].Name != "basics" {
		t.Fatalf("suites: %+v", suites)
	}
	// The export never carries Nanite's state.
	agentDir, _ := m.lib.AgentDir("trip")
	_ = os.MkdirAll(filepath.Join(agentDir, ".nanite"), 0o700)
	_ = os.WriteFile(filepath.Join(agentDir, ".nanite", "nanite.db"), []byte("state"), 0o600)
	data, _, _ = m.export("trip")
	if bytes.Contains(gunzip(t, data), []byte("nanite.db")) {
		t.Fatal(".nanite exported")
	}
}

func doJSON(h interface {
	Do(method, p, body string) (int, string)
}, method, p, body string) (int, string) {
	return h.Do(method, p, body)
}

func gunzip(t *testing.T, data []byte) []byte {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(zr)
	return buf.Bytes()
}

type tarEntry struct {
	name, body, link string
	typ              byte
	size             int64
}

func makeTar(t *testing.T, entries []tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		typ := e.typ
		if typ == 0 {
			typ = tar.TypeReg
		}
		size := int64(len(e.body))
		if e.size > 0 {
			size = e.size
		}
		if typ != tar.TypeReg {
			size = 0
		}
		if err := tw.WriteHeader(&tar.Header{Name: e.name, Typeflag: typ, Size: size, Mode: 0o600, Linkname: e.link}); err != nil {
			t.Fatal(err)
		}
		if typ == tar.TypeReg {
			if e.size > 0 {
				_, _ = tw.Write(bytes.Repeat([]byte("x"), int(e.size)))
			} else {
				_, _ = tw.Write([]byte(e.body))
			}
		}
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

func minimal(t *testing.T) []tarEntry {
	tree, _, _ := fromTemplate("blank", "mini")
	return []tarEntry{{name: "agent.md", body: string(tree["agent.md"])}, {name: "host.yaml", body: string(tree["host.yaml"])}}
}

func TestImportRefusesUnsafeArchives(t *testing.T) {
	ok := makeTar(t, minimal(t))
	if _, err := readArchive(ok); err != nil {
		t.Fatal("a plain archive refused:", err)
	}
	cases := map[string][]tarEntry{
		"zip-slip":      append(minimal(t), tarEntry{name: "../evil.md", body: "x"}),
		"nested slip":   append(minimal(t), tarEntry{name: "context/../../evil.md", body: "x"}),
		"absolute":      append(minimal(t), tarEntry{name: "/etc/evil.md", body: "x"}),
		"symlink":       append(minimal(t), tarEntry{name: "context/link.md", typ: tar.TypeSymlink, link: "/etc/passwd"}),
		"hard link":     append(minimal(t), tarEntry{name: "context/link.md", typ: tar.TypeLink, link: "agent.md"}),
		"too large":     append(minimal(t), tarEntry{name: "context/big.md", size: maxFileBytes + 10}),
		"no agent file": {{name: "host.yaml", body: "kind: embedded\n"}},
	}
	for name, entries := range cases {
		if _, err := readArchive(makeTar(t, entries)); err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
	many := minimal(t)
	for i := range maxArchiveFiles + 1 {
		many = append(many, tarEntry{name: "notes/n" + string(rune('a'+i%26)) + ".txt", body: "x"})
	}
	if _, err := readArchive(makeTar(t, many)); err == nil || !strings.Contains(err.Error(), "entries") {
		t.Fatalf("too many entries: %v", err)
	}
	if _, err := readArchive([]byte("not an archive")); err == nil {
		t.Fatal("garbage accepted")
	}
	if _, err := readArchive(make([]byte, maxArchiveBytes+1)); err == nil {
		t.Fatal("oversized upload accepted")
	}
	// Files that are not part of an agent are skipped, not unpacked.
	b, err := readArchive(makeTar(t, append(minimal(t), tarEntry{name: ".nanite/nanite.db", body: "x"}, tarEntry{name: "notes.txt", body: "x"})))
	if err != nil || len(b.Skipped) != 2 || len(b.Tree) != 2 {
		t.Fatalf("skipped: %v %v %d", err, b.Skipped, len(b.Tree))
	}
}

func TestImportZip(t *testing.T) {
	tree, _, _ := fromTemplate("blank", "zipped")
	write := func(extra func(*zip.Writer)) []byte {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		for _, p := range tree.Paths() {
			w, _ := zw.Create(p)
			_, _ = w.Write(tree[p])
		}
		if extra != nil {
			extra(zw)
		}
		_ = zw.Close()
		return buf.Bytes()
	}
	if b, err := readArchive(write(nil)); err != nil || b.Tree.Digest() != tree.Digest() {
		t.Fatal("zip refused or changed:", err)
	}
	slip := write(func(zw *zip.Writer) {
		w, _ := zw.Create("../../evil.md")
		_, _ = w.Write([]byte("x"))
	})
	if _, err := readArchive(slip); err == nil {
		t.Fatal("zip-slip accepted")
	}
	link := write(func(zw *zip.Writer) {
		hdr := &zip.FileHeader{Name: "context/link.md"}
		hdr.SetMode(os.ModeSymlink | 0o777)
		w, _ := zw.CreateHeader(hdr)
		_, _ = w.Write([]byte("/etc/passwd"))
	})
	if _, err := readArchive(link); err == nil {
		t.Fatal("zip symlink accepted")
	}
}

func TestImportDirCopiesAndNeverWrites(t *testing.T) {
	m, h := start(t)
	src := t.TempDir()
	tree, _, _ := fromTemplate("embedded", "outside")
	if err := writeAgentDir(filepath.Join(src, "a"), tree); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(src, "a")
	_ = os.MkdirAll(filepath.Join(dir, ".nanite"), 0o700)
	_ = os.WriteFile(filepath.Join(dir, ".nanite", "nanite.db"), []byte("state"), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "context", "notes.md.bak"), []byte("old"), 0o600)
	_ = os.MkdirAll(filepath.Join(dir, "basalisk-import"), 0o700)
	_ = os.WriteFile(filepath.Join(dir, "basalisk-import", "grants.json"), []byte("{}"), 0o600)
	snapshot := func() string {
		var b strings.Builder
		_ = filepath.Walk(dir, func(p string, info os.FileInfo, _ error) error {
			data, _ := os.ReadFile(p) //nolint:gosec // the test's own directory
			b.WriteString(p + info.Mode().String() + info.ModTime().String() + string(data))
			return nil
		})
		return b.String()
	}
	before := snapshot()
	body, _ := json.Marshal(map[string]string{"path": dir})
	code, out := h.Do("POST", "/import-dir", string(body))
	if code != 201 {
		t.Fatal(code, out)
	}
	if snapshot() != before {
		t.Fatal("the source directory changed")
	}
	got, _ := m.lib.Tree("outside")
	if _, ok := got["context/notes.md.bak"]; ok || got.Digest() != withoutSidecar(tree).Digest() {
		t.Fatalf("copied: %v", got.Paths())
	}
	if !strings.Contains(out, "skipped: .nanite/") || !strings.Contains(out, "basalisk-import/grants.json") {
		t.Fatal(out)
	}
	// A symlink anywhere refuses the import.
	_ = os.Symlink("/etc/hosts", filepath.Join(dir, "context", "hosts.md"))
	if code, _ := h.Do("POST", "/import-dir", string(body)); code == 201 {
		t.Fatal("symlink imported")
	}
	if code, _ := h.Do("POST", "/import-dir", `{"path":"relative/dir"}`); code == 201 {
		t.Fatal("relative path imported")
	}
}

func withoutSidecar(t Tree) Tree {
	out := Tree{}
	for p, b := range t {
		if agentPath(p) {
			out[p] = b
		}
	}
	return out
}
