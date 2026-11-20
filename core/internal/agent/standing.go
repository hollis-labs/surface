package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// The agent's standing instructions: the owner's rules for all work, kept in
// <agent dir>/context/standing.md. The host runner puts the agent directory's text in the
// system prompt when it opens (agentdir_text.go), and the system prompt is then fixed until
// the app restarts. So:
//
//   - standing_get and standing_update read and replace the file. An update is written
//     atomically, and the text it replaces is kept in context/.history/ (a subdirectory, so
//     the context/*.md the runner reads does not pick it up), with one line in
//     .history/standing.log saying when and why.
//   - The runner remembers the text it opened with. While the file says something else, each
//     turn's context block carries a <standing changed> section with the current text (or,
//     when that is long, a pointer and the changes), so an update reaches the model before a
//     restart. Once the app restarts, the system prompt has the new text and the section stops.

const (
	// standingFile is the standing instructions' file, under the agent directory.
	standingFile = "context/standing.md"
	// standingHistory is where the replaced versions are kept, under the agent directory.
	standingHistory = "context/.history"
	// maxStandingChars bounds the standing instructions: they are in every request's system
	// prompt.
	maxStandingChars = 16_000
	// standingInline is the longest standing text the <standing changed> section repeats
	// whole; a longer one is given as its changes.
	standingInline = 6_000
	// standingDiffChars bounds the changes the section shows.
	standingDiffChars = 4_000
)

// standingPath is the standing file of an agent directory.
func standingPath(dir string) string { return filepath.Join(dir, filepath.FromSlash(standingFile)) }

// readStanding is the standing text, trimmed; "" when there is none.
func readStanding(dir string) (string, error) {
	b, err := os.ReadFile(standingPath(dir)) //nolint:gosec // the owner's own agent directory
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// agentDir is the app's agent directory, or an error when there is none.
func (s *Service) agentDir() (string, error) {
	if s.cfgErr != nil {
		return "", fmt.Errorf("the agent config is not readable: %w", s.cfgErr)
	}
	dir := s.cfg.AgentDir(s.k.AppName())
	if dir == "" {
		return "", errors.New("this app has no agent directory")
	}
	return dir, nil
}

// noteStandingLoaded remembers the standing text the runner's system prompt was built with.
func (s *Service) noteStandingLoaded(dir string) {
	text, err := readStanding(dir)
	if err != nil {
		s.log.Warn("standing instructions not read", "err", err)
	}
	s.standingMu.Lock()
	s.standingLoaded, s.standingDir = &text, dir
	s.standingMu.Unlock()
}

// StandingState is what standing_get says.
type StandingState struct {
	Path string `json:"path"`
	Text string `json:"text"`
	// InSystemPrompt says whether the system prompt has this text (false: it changed since
	// the runner opened, and the context block carries it until a restart).
	InSystemPrompt bool `json:"in_system_prompt"`
	Chars          int  `json:"chars"`
}

func (s *Service) standingGet() (StandingState, error) {
	dir, err := s.agentDir()
	if err != nil {
		return StandingState{}, err
	}
	text, err := readStanding(dir)
	if err != nil {
		return StandingState{}, err
	}
	s.standingMu.Lock()
	loaded := s.standingLoaded
	s.standingMu.Unlock()
	return StandingState{Path: standingPath(dir), Text: text, Chars: utf8.RuneCountInString(text),
		InSystemPrompt: loaded == nil || *loaded == text}, nil
}

// standingUpdate replaces the standing text, keeping the old one in the history.
func (s *Service) standingUpdate(text, why string) (string, error) {
	text, why = strings.TrimSpace(text), strings.Join(strings.Fields(why), " ")
	switch n := utf8.RuneCountInString(text); {
	case n == 0:
		return "", errors.New("the standing text is empty; give the whole new text (standing_get reads the current one)")
	case n > maxStandingChars:
		return "", fmt.Errorf("the standing text is %d characters, over %d; keep to rules that hold across all work and move work state to slot notes", n, maxStandingChars)
	case why == "":
		return "", errors.New("say why the standing instructions change (why)")
	}
	dir, err := s.agentDir()
	if err != nil {
		return "", err
	}
	s.standingWrite.Lock()
	defer s.standingWrite.Unlock()
	path := standingPath(dir)
	oldRaw, err := os.ReadFile(path) //nolint:gosec // the owner's own agent directory
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	old := strings.TrimSpace(string(oldRaw))
	if old == text {
		return "Standing is unchanged: the text is the same as what is there.", nil
	}
	now := time.Now().UTC()
	hist := filepath.Join(dir, filepath.FromSlash(standingHistory))
	if err = os.MkdirAll(hist, 0o700); err != nil {
		return "", err
	}
	kept := ""
	if oldRaw != nil {
		if kept, err = keepVersion(hist, now, oldRaw); err != nil {
			return "", fmt.Errorf("the previous standing text was not kept, so it was not replaced: %w", err)
		}
	}
	mode := os.FileMode(0o600)
	if fi, serr := os.Stat(path); serr == nil {
		mode = fi.Mode().Perm()
	}
	if err = writeAtomic(path, []byte(text+"\n"), mode); err != nil {
		return "", err
	}
	d := lineDiff(old, text)
	logLine := fmt.Sprintf("%s\t%s\t+%d -%d\t%s\n", now.Format(time.RFC3339), kept, d.added, d.removed, why)
	if f, ferr := os.OpenFile(filepath.Join(hist, "standing.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); ferr == nil { //nolint:gosec // under the agent directory
		_, _ = f.WriteString(logLine)
		_ = f.Close()
	} else {
		s.log.Warn("standing log not written", "err", ferr)
	}
	s.k.Events().Publish("agent.standing", map[string]any{"added": d.added, "removed": d.removed, "why": why})

	var b strings.Builder
	fmt.Fprintf(&b, "Standing updated: +%d -%d lines (%d -> %d characters).", d.added, d.removed,
		utf8.RuneCountInString(old), utf8.RuneCountInString(text))
	if kept != "" {
		fmt.Fprintf(&b, " The previous text is kept as %s/%s.", standingHistory, kept)
	}
	b.WriteString(" It applies from your next turn (the context block carries it until the app restarts).\n")
	b.WriteString(clip(d.text, 2000))
	return strings.TrimSpace(b.String()), nil
}

// keepVersion writes a replaced standing text into the history and returns its file name.
func keepVersion(hist string, now time.Time, body []byte) (string, error) {
	base := "standing-" + now.Format("20060102T150405.000Z")
	for i := 1; ; i++ {
		name := base + ".md"
		if i > 1 {
			name = fmt.Sprintf("%s-%d.md", base, i)
		}
		f, err := os.OpenFile(filepath.Join(hist, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // under the agent directory
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if _, err = f.Write(body); err == nil {
			err = f.Sync()
		}
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		return name, err
	}
}

// writeAtomic replaces path with body: a temporary file beside it (not named *.md, so a
// reader of context/*.md never sees it), synced, then renamed over it.
func writeAtomic(path string, body []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }() // gone already after the rename
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
	return os.Rename(tmp, path)
}

// standingSection is the <standing changed> section of the context block: "" unless the
// runner keeps the standing text it opened with and the file now says something else.
func (s *Service) standingSection() string {
	s.standingMu.Lock()
	loaded, dir := s.standingLoaded, s.standingDir
	s.standingMu.Unlock()
	if loaded == nil {
		return ""
	}
	now, err := readStanding(dir)
	if err != nil || now == *loaded {
		return ""
	}
	var b strings.Builder
	b.WriteString("<standing changed>\n")
	switch {
	case now == "":
		b.WriteString("Your standing instructions (" + standingFile + ") were removed since this app started. The standing.md text in your system prompt no longer holds.\n")
	case utf8.RuneCountInString(now) <= standingInline:
		b.WriteString("Your standing instructions (" + standingFile + ") changed since this app started. The text below replaces the standing.md text in your system prompt:\n")
		b.WriteString(now + "\n")
	default:
		b.WriteString("Your standing instructions (" + standingFile + ") changed since this app started. The standing.md text in your system prompt is the old one; these are the changes (standing_get reads the whole current text):\n")
		d := lineDiff(*loaded, now)
		text := d.text
		if r := []rune(text); len(r) > standingDiffChars {
			text = string(r[:standingDiffChars]) + "\n…(more changes: read standing_get)"
		}
		b.WriteString(text + "\n")
	}
	b.WriteString("</standing changed>")
	return b.String()
}

// diff is a line diff: the lines removed and added, in order, as "- " and "+ " lines.
type diff struct {
	added, removed int
	text           string
}

// maxDiffCells bounds the line diff's table; past it the changed middle is given as replaced
// whole.
const maxDiffCells = 4_000_000

func lineDiff(a, b string) diff {
	split := func(s string) []string {
		if s == "" {
			return nil
		}
		return strings.Split(s, "\n")
	}
	x, y := split(a), split(b)
	// The common head and tail are unchanged.
	for len(x) > 0 && len(y) > 0 && x[0] == y[0] {
		x, y = x[1:], y[1:]
	}
	for len(x) > 0 && len(y) > 0 && x[len(x)-1] == y[len(y)-1] {
		x, y = x[:len(x)-1], y[:len(y)-1]
	}
	var out []string
	var d diff
	del := func(l string) { out = append(out, "- "+l); d.removed++ }
	add := func(l string) { out = append(out, "+ "+l); d.added++ }
	if len(x)*len(y) > maxDiffCells {
		for _, l := range x {
			del(l)
		}
		for _, l := range y {
			add(l)
		}
	} else {
		// lcs[i][j] is the longest common subsequence of x[i:] and y[j:].
		lcs := make([][]int, len(x)+1)
		for i := range lcs {
			lcs[i] = make([]int, len(y)+1)
		}
		for i := len(x) - 1; i >= 0; i-- {
			for j := len(y) - 1; j >= 0; j-- {
				if x[i] == y[j] {
					lcs[i][j] = lcs[i+1][j+1] + 1
				} else {
					lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
				}
			}
		}
		i, j := 0, 0
		for i < len(x) && j < len(y) {
			switch {
			case x[i] == y[j]:
				i++
				j++
			case lcs[i+1][j] >= lcs[i][j+1]:
				del(x[i])
				i++
			default:
				add(y[j])
				j++
			}
		}
		for ; i < len(x); i++ {
			del(x[i])
		}
		for ; j < len(y); j++ {
			add(y[j])
		}
	}
	d.text = strings.Join(out, "\n")
	return d
}

// keepingAsks are the lines that ask the agent to keep its state current for a turn in the
// primary slot: the note on the slot it left, and a note here that may be behind.
func (s *Service) keepingAsks(ctx context.Context, primary string, owner bool) []string {
	var out []string
	if ask := s.leftSlotAsk(primary, owner); ask != "" {
		out = append(out, ask)
	}
	if owner {
		if hint := s.staleNoteHint(ctx, primary); hint != "" {
			out = append(out, hint)
		}
	}
	return out
}

// leftSlotAsk is the line asking the agent to bring the note on the slot it just left up to
// date: once, on the first owner turn in a different slot from the turn before. Turns the
// owner did not type (errands, automatic turns) neither ask nor move the slot remembered.
func (s *Service) leftSlotAsk(primary string, owner bool) string {
	if !owner {
		return ""
	}
	s.lastMu.Lock()
	prev := s.turnSlot
	s.turnSlot = primary
	s.lastMu.Unlock()
	if prev == "" || prev == primary {
		return ""
	}
	return fmt.Sprintf("You have just come from the %s slot. If where things stand there changed and your note on it does not say so yet, bring it up to date now with context_note (slot %q) before you go on; say nothing of this to the owner.", prev, prev)
}

// Staleness: a primary slot's note older than staleAfter while the owner has sent at least
// staleTurns messages in that slot since it was written.
const (
	staleAfter = 24 * time.Hour
	staleTurns = 5
)

// staleNoteHint is a line saying the primary slot's note may be behind, or "". It counts the
// owner's turns from the context trace, so it is quiet when the trace is off.
func (s *Service) staleNoteHint(ctx context.Context, slot string) string {
	if !s.traceOn() {
		return ""
	}
	note, updated := s.notes.Get(ctx, slot)
	if note == "" {
		return ""
	}
	at, err := time.Parse(time.RFC3339, updated)
	if err != nil || time.Since(at) < staleAfter {
		return ""
	}
	n := s.trace.TurnsSince(ctx, slot, at)
	if n < staleTurns {
		return ""
	}
	return fmt.Sprintf("Your note on %s is from %s and the owner has sent %d messages here since. If where things stand moved, bring it up to date with context_note.", slot, at.Format("2 Jan"), n)
}
