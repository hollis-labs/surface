package profile

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	gosync "sync"
	"time"

	"github.com/hollis-labs/libs/agent/agentdir"
	"github.com/hollis-labs/nanite/agent/authored"
	"gopkg.in/yaml.v3"
)

// BindingPath expands ~/ and requires an absolute authored directory.
func BindingPath(dir string) (string, error) {
	dir = strings.TrimSpace(dir)
	if strings.HasPrefix(dir, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, strings.TrimPrefix(dir, "~/"))
	}
	if !filepath.IsAbs(dir) {
		return "", fmt.Errorf("agent folder must be an absolute path or start with ~/")
	}
	return filepath.Clean(dir), nil
}

// LoadBinding validates a standard agent folder. host.yaml is validated but its
// runtime settings are ignored: bound agents share the app runtime and authority.
func LoadBinding(id, title, dir string) (*Profile, error) {
	dir, err := BindingPath(dir)
	if err != nil {
		return nil, fmt.Errorf("binding %s: %w", id, err)
	}
	files := withoutHostFS{FS: os.DirFS(dir)}
	// Load performs the bounded walk and Nanite authored validation on everything
	// except host.yaml. Validate the actual host with agentdir afterwards.
	p, err := Load(id, title, files)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, fmt.Errorf("binding %s: agent.md is missing", id)
	}
	if p.steeringRaw != nil {
		p.files["steering.json"] = p.steeringRaw
	}
	for name, raw := range p.files {
		if strings.HasPrefix(name, "tools/") && strings.HasSuffix(name, "/tool.yaml") {
			pkg := strings.TrimSuffix(strings.TrimPrefix(name, "tools/"), "/tool.yaml")
			tool, parseErr := authored.ParseTool(pkg, raw)
			if parseErr != nil {
				return nil, parseErr
			}
			if len(tool.Env) > 0 || len(tool.Needs) > 0 {
				return nil, fmt.Errorf("%s: bound agents cannot request environment passthrough or Warden tokens", name)
			}
		}
	}
	host, err := os.Lstat(filepath.Join(dir, "host.yaml"))
	if errors.Is(err, fs.ErrNotExist) {
		return p, nil
	}
	if err != nil {
		return nil, fmt.Errorf("binding %s: host.yaml: %w", id, err)
	}
	if !host.Mode().IsRegular() || host.Size() > maxFile {
		return nil, fmt.Errorf("binding %s: host.yaml must be a regular file of at most 1 MiB", id)
	}
	if _, err = agentdir.Load(dir); err != nil {
		return nil, fmt.Errorf("binding %s: %w", id, err)
	}
	raw, err := fs.ReadFile(files, "host.yaml")
	if err != nil {
		return nil, err
	}
	var hostKeys struct {
		Runtime map[string]any `yaml:"runtime"`
	}
	if err = yaml.Unmarshal(raw, &hostKeys); err != nil {
		return nil, err
	}
	for key := range hostKeys.Runtime {
		p.IgnoredRuntime = append(p.IgnoredRuntime, key)
	}
	sort.Strings(p.IgnoredRuntime)
	return p, nil
}

// withoutHostFS hides only the runtime file from the built-in profile validator.
type withoutHostFS struct{ fs.FS }

func (f withoutHostFS) ReadDir(name string) ([]fs.DirEntry, error) {
	entries, err := fs.ReadDir(f.FS, name)
	if name != "." {
		return entries, err
	}
	out := make([]fs.DirEntry, 0, len(entries))
	for _, e := range entries {
		if e.Name() != "host.yaml" {
			out = append(out, e)
		}
	}
	return out, err
}

var bindingStageMu gosync.Mutex

// StageBinding writes an immutable authored snapshot, without host.yaml. Keeping
// snapshots by content digest lets concurrent turns and recovery retain their agent.
func (p *Profile) StageBinding(root string) (string, error) {
	names := make([]string, 0, len(p.files))
	for name := range p.files {
		names = append(names, name)
	}
	sort.Strings(names)
	h := sha256.New()
	want := map[string]staged{}
	for _, name := range names {
		raw := p.files[name]
		fmt.Fprintf(h, "%d:%s:%t:%d:", len(name), name, p.exec[name], len(raw))
		_, _ = h.Write(raw)
		want[name] = staged{data: raw, exec: p.exec[name]}
	}
	dir := filepath.Join(root, hex.EncodeToString(h.Sum(nil)))
	bindingStageMu.Lock()
	defer bindingStageMu.Unlock()
	if err := sync(dir, want); err != nil {
		return "", err
	}
	now := time.Now()
	if err := os.Chtimes(dir, now, now); err != nil {
		return "", err
	}
	return dir, nil
}

// TouchBinding records use of an existing snapshot during interrupted-turn
// recovery. It never recreates a pruned snapshot or touches an authored folder.
func TouchBinding(root, dir string) error {
	if dir == "" || filepath.Dir(filepath.Clean(dir)) != filepath.Clean(root) {
		return nil
	}
	bindingStageMu.Lock()
	defer bindingStageMu.Unlock()
	info, err := os.Lstat(filepath.Clean(dir))
	if err != nil {
		return fmt.Errorf("bound agent snapshot unavailable for recovery: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("bound agent snapshot unavailable for recovery: not a directory")
	}
	now := time.Now()
	if err = os.Chtimes(filepath.Clean(dir), now, now); err != nil {
		return fmt.Errorf("bound agent snapshot unavailable for recovery: %w", err)
	}
	return nil
}
