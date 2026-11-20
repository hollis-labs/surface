package agent

import (
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// agentDirText is what the agent's authored directory says, for a runner that does not read
// the directory itself (the host runner; Nanite reads it on its own): the body of agent.md
// after its front matter, then every context/*.md in name order, each under its file name.
// The directory is read once, when the runner opens; a missing directory or file is no text.
func agentDirText(dir string, log *slog.Logger) string {
	if dir == "" {
		return ""
	}
	var parts []string
	if b, err := os.ReadFile(filepath.Join(dir, "agent.md")); err == nil { //nolint:gosec // the agent directory is the owner's own config
		if body := strings.TrimSpace(stripFront(string(b))); body != "" {
			parts = append(parts, body)
		}
	}
	files, _ := filepath.Glob(filepath.Join(dir, "context", "*.md"))
	sort.Strings(files)
	for _, f := range files {
		b, err := os.ReadFile(f) //nolint:gosec // a file in the owner's agent directory
		if err != nil {
			log.Warn("agent context file not read", "file", f, "err", err)
			continue
		}
		if t := strings.TrimSpace(string(b)); t != "" {
			parts = append(parts, "<!-- "+filepath.Base(f)+" -->\n"+t)
		}
	}
	if len(parts) > 0 {
		log.Info("agent directory read into the system prompt", "dir", dir, "parts", len(parts), "bytes", len(strings.Join(parts, "")))
	}
	return strings.Join(parts, "\n\n")
}

// stripFront drops a leading YAML front matter block (--- ... ---).
func stripFront(s string) string {
	t := strings.TrimLeft(s, "\ufeff\n")
	if !strings.HasPrefix(t, "---\n") {
		return s
	}
	if end := strings.Index(t[4:], "\n---"); end >= 0 {
		rest := t[4+end+4:]
		if i := strings.IndexByte(rest, '\n'); i >= 0 {
			return rest[i+1:]
		}
		return ""
	}
	return s
}

func joinNonEmpty(a, b string) string {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}
	return a + "\n\n" + b
}
