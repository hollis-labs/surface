package profile

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Staging: Nanite reads its agent directory again at the start of every turn, and the
// directory is fixed when the agent opens. So the runner opens the agent on a staged
// directory under the app's data directory, and before each turn writes into it the app's
// seat (the owner's agent directory: host.yaml, agent.md, context/, skills/, variants/ and
// the rest) with the active profile laid over it:
//
//   - agent.md is the seat's, front matter and all (one identity, one definition), with the
//     profile's instructions after it under a heading;
//   - the profile's context files are added as context/profile.<name>.md, beside the seat's;
//   - its skills, variants and tool packages are added, replacing the seat's of the same name.
//
// Only what changed is written, each file atomically, and what is no longer wanted is
// removed, so a turn never sees a half-written directory. With no profile the staged
// directory is the seat as it is.

// ContextPrefix names the profile's context files in the staged directory.
const ContextPrefix = "profile."

// staged is one file to be in the staged directory.
type staged struct {
	data []byte
	exec bool
}

// Stage makes dst the seat with p laid over it (p nil: the seat alone).
func Stage(dst, seat string, p *Profile) error {
	want, err := seatFiles(seat)
	if err != nil {
		return fmt.Errorf("profile: the seat %s: %w", seat, err)
	}
	if p != nil {
		base, ok := want["agent.md"]
		if !ok {
			return errors.New("profile: the seat has no agent.md")
		}
		want["agent.md"] = staged{data: composeAgent(base.data, p)}
		for name, raw := range p.files {
			top, rest, _ := strings.Cut(name, "/")
			switch top {
			case "agent.md":
			case "context":
				want["context/"+ContextPrefix+rest] = staged{data: raw}
			case "skills", "tools":
				// A profile's skill or tool package replaces the seat's of the same name whole.
				unit, _, _ := strings.Cut(rest, "/")
				for k := range want {
					if strings.HasPrefix(k, top+"/"+unit+"/") {
						delete(want, k)
					}
				}
			}
		}
		for name, raw := range p.files {
			top, _, _ := strings.Cut(name, "/")
			if top == "skills" || top == "variants" || top == "tools" {
				want[name] = staged{data: raw, exec: p.exec[name]}
			}
		}
	}
	return sync(dst, want)
}

// composeAgent is the staged agent.md: the seat's, then the profile's instructions.
func composeAgent(seat []byte, p *Profile) []byte {
	var b bytes.Buffer
	b.Write(bytes.TrimRight(seat, "\n"))
	title := p.Title
	if title == "" {
		title = p.ID
	}
	fmt.Fprintf(&b, "\n\n## Profile: %s\n\nYou are working as the %s profile (the %s module's). What follows holds while this profile is active; everything above still holds, and your tools, approvals and conversation are the same. Its context files are context/%s*.md.\n\n",
		title, title, p.ID, ContextPrefix)
	b.WriteString(p.Body)
	b.WriteString("\n")
	return b.Bytes()
}

// seatFiles reads the seat: every regular file, by slash path, leaving out dot entries (the
// standing history, Nanite's own state) and evals/.
func seatFiles(seat string) (map[string]staged, error) {
	out := map[string]staged{}
	total := 0
	err := filepath.WalkDir(seat, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(seat, p)
		if err != nil || rel == "." {
			return err
		}
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(path.Base(rel), ".") && rel != ".mcp.json" || rel == "evals" {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if total += int(info.Size()); total > 4*maxTotal {
			return fmt.Errorf("the seat's files come to more than %d bytes", 4*maxTotal)
		}
		raw, err := os.ReadFile(p) //nolint:gosec // the owner's own agent directory
		if err != nil {
			return err
		}
		out[rel] = staged{data: raw, exec: info.Mode()&0o100 != 0}
		return nil
	})
	return out, err
}

// sync makes dir hold exactly want: files written only when they differ, each atomically;
// files and directories no longer wanted removed.
func sync(dir string, want map[string]staged) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	var stale []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil || rel == "." {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if _, ok := want[filepath.ToSlash(rel)]; !ok {
			stale = append(stale, p)
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, p := range stale {
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	for name, f := range want {
		target := filepath.Join(dir, filepath.FromSlash(name))
		mode := os.FileMode(0o600)
		if f.exec {
			mode = 0o700
		}
		if cur, err := os.ReadFile(target); err == nil && bytes.Equal(cur, f.data) { //nolint:gosec // under the staged directory
			if info, err := os.Stat(target); err == nil && info.Mode().Perm() == mode {
				continue
			}
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		if err := writeAtomic(target, f.data, mode); err != nil {
			return err
		}
	}
	return pruneDirs(dir)
}

// pruneDirs removes the empty directories under dir, deepest first.
func pruneDirs(dir string) error {
	var dirs []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() && p != dir {
			dirs = append(dirs, p)
		}
		return nil
	})
	for i := len(dirs) - 1; i >= 0; i-- {
		if entries, err := os.ReadDir(dirs[i]); err == nil && len(entries) == 0 {
			_ = os.Remove(dirs[i])
		}
	}
	return nil
}

// writeAtomic replaces path with body: a temporary dot file beside it (which no reader of
// the directory picks up), synced, then renamed over it.
func writeAtomic(target string, body []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }()
	if _, err = f.Write(body); err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp, mode)
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp, target)
}
