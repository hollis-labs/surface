// Package design is the Design module: design work handed to a design team and reviewed live.
//
// The owner asks the agent for a change to the interface; the agent records a job and hands it
// to the Design team (a Tether message and a manual Torque task, behind Dispatcher). The team
// works on a branch and serves the component gallery from it with hot reload
// (make design-preview); the job's window frames that preview beside the same page from the
// base, the files the branch changes, and the owner's notes, which are marks on the job. Notes
// go back as the next round; approval goes back as the go-ahead to merge. Progress is read from
// the branch and its pull request, and from what comes back (inbox.go): the Designer's replies
// on the job's thread and the job's Torque task. Surface never merges.
package design

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/hollis-labs/surface/module"
)

//go:embed migrations/*.sql
var migrations embed.FS

func init() { module.Register(&mod{}) }

type mod struct {
	module.Base
	h  module.Host
	st *store
	gh *github
	// dispatch replaces the configured Dispatcher (tests).
	dispatch Dispatcher
	// every is how often open jobs are checked (tests shorten it); stop ends the checking.
	every    time.Duration
	stop     context.CancelFunc
	life     context.Context // ends at Stop: what the module does on its own runs under it
	checking sync.Mutex
}

// ref is what a job's marks are pinned to.
func ref(id string) string { return "design:" + id }

func (m *mod) Manifest() module.Manifest {
	return module.Manifest{
		ID: "design", Title: "Design", Version: "0.1.0",
		About: "Design work handed to the Design team and reviewed live: jobs, a live preview of the branch, notes, approval.",
		Views: []module.View{
			{
				ID: "design", Title: "Design", Icon: "palette", Width: 720, Height: 560, Menu: true, Bare: true,
				Render: module.Component("design/jobs"),
				Describe: func(context.Context, module.Host, module.Window) (module.WindowContext, error) {
					return module.WindowContext{Kind: "data", Ref: "design:jobs"}, nil
				},
			},
			{
				ID: "design.job", Title: "Design job", Icon: "palette", Width: 1280, Height: 820, Bare: true, Multi: true,
				Render: module.Component("design/job"),
				Describe: func(ctx context.Context, _ module.Host, w module.Window) (module.WindowContext, error) {
					id, _ := w.Args["id"].(string)
					j, err := m.st.get(ctx, id)
					if err != nil {
						return module.WindowContext{}, nil //nolint:nilerr // a job that is gone adds no context; that is not a failure
					}
					extra := map[string]any{"status": j.Status, "branch": j.Branch}
					for _, k := range []string{"showing", "section", "theme"} {
						if v, ok := w.State[k]; ok {
							extra[k] = v
						}
					}
					return module.WindowContext{Kind: "task", Ref: ref(j.ID), Title: j.Title, URL: j.PreviewURL, Extra: extra}, nil
				},
			},
		},
		Tools: m.tools(),
		Store: &module.Store{Migrations: migrations, Dir: "migrations"},
		Settings: []module.Setting{
			{Key: "repo", Title: "Repository", About: "owner/name on GitHub, where the Design team branches and opens pull requests.", Type: module.SettingString, Default: "hollis-labs/surface"},
			{Key: "base_branch", Title: "Base branch", Type: module.SettingString, Default: "main"},
			{Key: "designer_urn", Title: "Designer's Tether address", About: "Who receives the brief and the rounds.", Type: module.SettingString, Default: ""},
			{Key: "sender_urn", Title: "This app's Tether address", About: "Who the messages are from. Empty: nothing is sent; the text is shown to send by hand.", Type: module.SettingString, Default: ""},
			{Key: "torque_project", Title: "Torque project", About: "Where the job's task is created (manual: it does not start on its own).", Type: module.SettingString, Default: ""},
			{Key: "tether_url", Title: "Tether MCP URL", About: "Tether's MCP endpoint over HTTP. Or set a command instead.", Type: module.SettingString, Default: ""},
			{Key: "tether_token_file", Title: "Tether token file", About: "A file holding the bearer token for the MCP URL. The token is read when needed and never shown.", Type: module.SettingString, Default: ""},
			{Key: "tether_command", Title: "Tether MCP command", About: "A command that speaks MCP over stdio, words separated by spaces. Used when no URL is set.", Type: module.SettingString, Default: ""},
			{Key: "check_seconds", Title: "Check every (seconds)", About: "How often open jobs are checked for the Designer's replies and their task's state. Read at start; at least 15.", Type: module.SettingInt, Default: 60},
			{Key: "github_token_file", Title: "GitHub token file", About: "Optional: a file holding a token for reading the repository (a public one reads without).", Type: module.SettingString, Default: ""},
		},
		Slots: []module.Slot{{
			Name: "design", About: "design work: interface changes the Design team makes on a branch, reviewed live in the job's window",
			Views:        []string{"design", "design.job"},
			Tools:        []string{"design_request", "design_jobs", "design_job", "design_refresh", "design_set", "design_feedback", "design_approve", "design_open"},
			Where:        "the design jobs (design_jobs, design_job); notes are marks on design:<job id>",
			Instructions: "A change to how the interface looks is design work: hand it to the Design team with design_request rather than editing components yourself. A job follows itself: the Designer's replies, its task and its preview are read on a timer; before saying where a job stands, read it (design_job) or check now (design_refresh), and say when it was last checked. Notes the owner pins in the job's window are the next round's feedback; send them with design_feedback when the owner says so. Only design_approve approves, and the Design team merges.",
			Knowledge:    m.knowledge,
		}},
	}
}

func (m *mod) Start(_ context.Context, h module.Host) error {
	m.h = h
	m.st = &store{db: h.Store()}
	if m.gh == nil {
		m.gh = &github{api: "https://api.github.com", client: &http.Client{Timeout: 15 * time.Second}}
	}
	m.gh.tokenFile = func() string { return h.Config().String("github_token_file") }
	m.routes(h)
	every := m.every
	if every == 0 {
		every = defaultEvery
		if n := h.Config().Int("check_seconds"); n > 0 {
			every = time.Duration(max(n, 15)) * time.Second
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.stop, m.life = cancel, ctx
	go m.poll(ctx, every)
	return nil
}

func (m *mod) Stop(context.Context) error {
	if m.stop != nil {
		m.stop()
	}
	return nil
}

func (m *mod) changed(id string) { m.h.Events().Publish("design.changed", map[string]any{"id": id}) }

// noteLine is an open note as the Design team reads it.
type noteLine struct {
	ID    string `json:"id"`
	Where string `json:"where"`
	Body  string `json:"body"`
}

// Anchor is where a note points in the gallery.
type Anchor struct {
	Section   string `json:"section"`
	Component string `json:"component,omitempty"`
}

func (m *mod) openNotes(ctx context.Context, id string) ([]noteLine, error) {
	marks, err := m.h.Marks().List(ctx, ref(id))
	if err != nil {
		return nil, err
	}
	out := []noteLine{}
	for _, mk := range marks {
		if mk.Status != "open" {
			continue
		}
		var a Anchor
		_ = json.Unmarshal(mk.Anchor, &a)
		where := or(a.Section, "general")
		if a.Component != "" {
			where += " / " + a.Component
		}
		body := mk.Body
		for _, r := range mk.Replies {
			body += " (" + r.Body + ")"
		}
		out = append(out, noteLine{ID: mk.ID, Where: where, Body: body})
	}
	return out, nil
}

// View is a job as a window and a tool show it: the job, what its branch says, its open notes.
type View struct {
	Job
	Progress Progress   `json:"progress"`
	Notes    []noteLine `json:"notes"`
}

// view reads a job's branch and moves its status along when the branch says so.
func (m *mod) view(ctx context.Context, id string, fresh bool) (View, error) {
	j, err := m.st.get(ctx, id)
	if err != nil {
		return View{}, err
	}
	repo := m.h.Config().String("repo")
	// The pull request read is the job's own, by its number, once one is recorded.
	p := m.gh.progress(ctx, repo, or(m.h.Config().String("base_branch"), "main"), j.Branch, prNumberIn(j.PRURL, repo), fresh)
	// What the branch says moves the job forward only from the status just read, and fills a
	// missing pull request only: an owner's write since is never undone.
	moved := false
	if next := advance(j.Status, p); next != j.Status {
		if moved, err = m.st.moveStatus(ctx, id, j.Status, next); err != nil {
			return View{}, err
		}
	}
	if p.PR != nil && j.PRURL == "" {
		filled, fillErr := m.st.fillPR(ctx, id, p.PR.URL)
		if fillErr != nil {
			return View{}, fillErr
		}
		moved = moved || filled
	}
	// A merged pull request counts only as the merge of the approved commit; anything else is
	// said loudly, once, and changes nothing.
	// For an approved job, also every pull request that contains the approved commit: another
	// one merged at the same commit is not the approved merge.
	var atHead []PullRequest
	if j.Status == StatusApproved && j.ApprovedHead != "" {
		atHead, _ = m.gh.pullsAt(ctx, repo, j.ApprovedHead, fresh)
	}
	switch merged, warning := mergedOnGitHub(j, p.PR, atHead); {
	case merged:
		ok, mergeErr := m.st.mergeApproved(ctx, id, j.ApprovedHead, j.ApprovedPR)
		if mergeErr != nil {
			return View{}, mergeErr
		}
		moved = moved || ok
	case warning != "":
		isNew, warnErr := m.st.warnMerge(ctx, id, warning)
		if warnErr != nil {
			return View{}, warnErr
		}
		if isNew {
			moved = true
			m.mergeNotice(ctx, j, warning)
		}
	}
	if moved {
		if j, err = m.st.get(ctx, id); err != nil {
			return View{}, err
		}
		m.changed(id)
		m.supersede(ctx, j)
	}
	notes, err := m.openNotes(ctx, id)
	if err != nil {
		return View{}, err
	}
	return View{Job: j, Progress: p, Notes: notes}, nil
}

// mergeNotice raises the merge warning as a notice, once per warning.
func (m *mod) mergeNotice(ctx context.Context, j Job, warning string) {
	payload, _ := json.Marshal(map[string]any{"body": warning, "tone": "warning"})
	if _, err := m.h.Cards().Show(ctx, module.Card{Kind: "info", Title: "Merged without approval of this commit: " + j.Title, Source: "design", Subject: ref(j.ID) + ":merge", Payload: payload}); err != nil {
		m.h.Log().Warn("design: the merge warning did not show", "job", j.ID, "err", err)
	}
}

// focusedJob is the job whose window is in front, if one is.
func (m *mod) focusedJob(ctx context.Context) string {
	if w, ok := m.h.Desk().Focused(ctx); ok && w.View == "design.job" {
		id, _ := w.Args["id"].(string)
		return id
	}
	return ""
}

func (m *mod) jobID(ctx context.Context, given string) (string, error) {
	if given != "" {
		return given, nil
	}
	if id := m.focusedJob(ctx); id != "" {
		return id, nil
	}
	return "", errors.New("which job? give its id (design_jobs lists them), or open its window")
}

func (m *mod) open(ctx context.Context, j Job) (module.Window, error) {
	return m.h.Desk().Open(ctx, "design.job", module.OpenOptions{Key: j.ID, Title: "Design: " + j.Title, Args: map[string]any{"id": j.ID}})
}

// Request records a job, opens its window and hands it to the Design team.
func (m *mod) request(ctx context.Context, title, brief, refs string) (map[string]any, error) {
	j, err := m.st.create(ctx, title, brief, refs)
	if err != nil {
		return nil, err
	}
	msg, task := m.briefMessage(j)
	out := m.deliver(ctx, msg, &task)
	if err = m.st.addRound(ctx, j.ID, j.Brief, out); err != nil {
		return nil, err
	}
	if out.TaskID != "" {
		if j, err = m.st.update(ctx, j.ID, Change{TaskID: &out.TaskID}); err != nil {
			return nil, err
		}
	}
	m.changed(j.ID)
	if _, err := m.open(ctx, j); err != nil {
		m.h.Log().Warn("design: the job's window did not open", "job", j.ID, "err", err)
	}
	j, _ = m.st.get(ctx, j.ID)
	return map[string]any{"job": j, "outbox": out}, nil
}

// Feedback sends the open notes and text as the next round and marks the job changes requested.
func (m *mod) feedback(ctx context.Context, id, text string) (map[string]any, error) {
	j, err := m.st.get(ctx, id)
	if err != nil {
		return nil, err
	}
	notes, err := m.openNotes(ctx, id)
	if err != nil {
		return nil, err
	}
	if settled(j.Status) {
		return nil, fmt.Errorf("the job is %s: no new round can be sent on it (for more changes, request a new job)", j.Status)
	}
	if len(notes) == 0 && strings.TrimSpace(text) == "" {
		return nil, errors.New("nothing to send: there are no open notes on this job and no text")
	}
	n := len(j.Rounds) + 1
	out := m.deliver(ctx, feedbackMessage(j, n, text, notes), nil)
	var asked strings.Builder
	asked.WriteString(strings.TrimSpace(text))
	for _, nl := range notes {
		fmt.Fprintf(&asked, "\n- [%s] %s", nl.Where, nl.Body)
	}
	if err = m.st.addRound(ctx, id, strings.TrimSpace(asked.String()), out); err != nil {
		return nil, err
	}
	// The notes are in this round now: they are closed, with a line saying where they went.
	for _, nl := range notes {
		_, _ = m.h.Marks().Reply(ctx, nl.ID, module.MarkReply{Body: fmt.Sprintf("Sent in round %d.", n), Author: "agent"})
		_ = m.h.Marks().Resolve(ctx, nl.ID)
	}
	st := StatusChangesRequested
	if _, err := m.st.update(ctx, id, Change{Status: &st}); err != nil {
		return nil, err
	}
	if err := m.st.roundSent(ctx, id); err != nil {
		return nil, err
	}
	m.changed(id)
	return map[string]any{"round": n, "notes": len(notes), "outbox": out}, nil
}

// errNoPR: an approval names a pull request, and none in the repository is recorded.
var errNoPR = errors.New("no pull request in the repository is recorded for this job, so there is nothing to approve: ask the Designer for a reply with a line pr: <pull request URL> (design_refresh reads it)")

// errNoHead: an approval names a commit, and none is recorded.
var errNoHead = errors.New("no head is recorded for this job, so there is nothing to approve: ask the Designer for a reply with a line head: <full commit SHA> (design_refresh reads it)")

func short(sha string) string { return sha[:min(len(sha), 7)] }

// askApproval puts the approval card in front of the owner, for the job's recorded head and
// that head only. It returns nil when the job is already settled. It refuses when no head is
// recorded, and when GitHub shows the branch at another commit (saying so on a notice), so the
// owner never approves a commit they were not shown.
func (m *mod) askApproval(ctx context.Context, id string, wait time.Duration) (*module.Card, error) {
	j, err := m.st.get(ctx, id)
	if err != nil {
		return nil, err
	}
	if settled(j.Status) {
		return nil, nil
	}
	if j.HeadSHA == "" {
		return nil, errNoHead
	}
	repo := m.h.Config().String("repo")
	if prNumberIn(j.PRURL, repo) == 0 {
		return nil, fmt.Errorf("%w (recorded: %q)", errNoPR, j.PRURL)
	}
	p := m.gh.progress(ctx, repo, or(m.h.Config().String("base_branch"), "main"), j.Branch, prNumberIn(j.PRURL, repo), true)
	if !p.stale && p.Exists && p.Head != "" && !strings.EqualFold(p.Head, j.HeadSHA) { // only a fresh read of the branch is compared
		why := fmt.Sprintf("GitHub shows %s at %s, but the head the Designer named is %s. Nothing can be approved until the job is refreshed with a reply naming the branch's head.", j.Branch, short(p.Head), short(j.HeadSHA))
		payload, _ := json.Marshal(map[string]any{"body": why, "tone": "warning"})
		_, _ = m.h.Cards().Show(ctx, module.Card{Kind: "info", Title: "Not approvable yet: " + j.Title, Source: "design", Subject: ref(id), Payload: payload})
		return nil, errors.New(why)
	}
	notes, _ := m.openNotes(ctx, id)
	changes := []map[string]string{
		{"label": "Status", "before": j.Status, "after": StatusApproved},
		{"label": "Commit", "after": j.HeadSHA},
		{"label": "Branch", "after": j.Branch},
	}
	changes = append(changes, map[string]string{"label": "Pull request", "after": j.PRURL})
	summary := "Approve “" + j.Title + "”: " + j.PRURL + " at commit " + short(j.HeadSHA) + "? The Design team is told to merge exactly that pull request at that commit; a later commit needs a new round."
	if len(notes) > 0 {
		summary += fmt.Sprintf(" %d note(s) are still open and would not be sent.", len(notes))
	}
	payload, _ := json.Marshal(map[string]any{"summary": summary, "changes": changes, "requested_by": "design", "head": j.HeadSHA, "pr": j.PRURL})
	card, err := m.h.Cards().Show(ctx, module.Card{
		Kind: "approval", Title: "Approve the design at " + short(j.HeadSHA) + ": " + j.Title, Source: "design", Subject: ref(id),
		Risk: "medium", Blocking: true, Payload: payload, ExpiresAt: time.Now().Add(wait).UTC().Format(time.RFC3339),
	})
	if err != nil {
		return nil, err
	}
	if err := m.st.pendApproval(ctx, id, card.ID, j.HeadSHA, j.PRURL); err != nil {
		return nil, err
	}
	return &card, nil
}

// settle acts on the owner's answer to an approval card. The approval is recorded only for the
// head on that card, only while that card is still the job's open approval and the job's head
// is still that head (store.approve); then the Design team is told to merge that commit.
func (m *mod) settle(ctx context.Context, id string, done module.Card) (map[string]any, error) {
	approved := false
	if done.Status == "submitted" && done.Outcome != nil {
		for _, d := range done.Outcome.Decisions {
			approved = approved || d.Action == "approve"
		}
	}
	j, err := m.st.get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !approved {
		if done.Status != "" { // answered or ended; an open card (a wait that ran out) stays pending
			_ = m.st.dropApproval(ctx, id, done.ID)
		}
		out := map[string]any{"approved": false, "card": or(done.Status, "open")}
		if done.Status == "superseded" {
			out["reason"] = "the head changed while the card was open; the owner is asked again on the new head"
		}
		return out, nil
	}
	head, pr := "", ""
	if j.approvalCard == done.ID {
		head, pr = j.approvalHead, j.approvalPR
	}
	ok, err := m.st.approve(ctx, id, done.ID, head, pr)
	if err != nil {
		return nil, err
	}
	if !ok {
		return map[string]any{"approved": false, "reason": "not recorded: the job's head changed, or the job was settled, after this card was raised; nothing was approved"}, nil
	}
	if j, err = m.st.get(ctx, id); err != nil {
		return nil, err
	}
	out := m.deliver(ctx, approvedMessage(j), nil)
	m.changed(id)
	return map[string]any{"approved": true, "status": j.Status, "approved_head": j.ApprovedHead, "approved_pr": j.ApprovedPR, "outbox": out}, nil
}

// awaitSettle waits for the owner's answer to card in the background and settles it.
func (m *mod) awaitSettle(id string, card module.Card) {
	go func() { //nolint:gosec // detached on purpose: the owner answers long after the ask
		ctx, cancel := context.WithTimeout(m.life, 31*time.Minute)
		defer cancel()
		if done, err := m.h.Cards().Await(ctx, card.ID); err == nil {
			if _, err := m.settle(context.WithoutCancel(ctx), id, done); err != nil {
				m.h.Log().Warn("design: approval", "job", id, "err", err)
			}
		}
	}()
}

// supersede ends an open approval card that no longer stands: its job is settled (approved,
// merged, closed), so no stale card is left to answer; or its head is no longer the job's, and
// then the owner is asked again on the new head.
func (m *mod) supersede(ctx context.Context, j Job) {
	if j.approvalCard == "" || (j.approvalHead == j.HeadSHA && j.approvalPR == j.PRURL && !settled(j.Status)) {
		return
	}
	_, _ = m.h.Cards().End(ctx, j.approvalCard, "superseded")
	_ = m.st.dropApproval(ctx, j.ID, j.approvalCard)
	if settled(j.Status) {
		return
	}
	card, err := m.askApproval(ctx, j.ID, 30*time.Minute)
	if err != nil || card == nil {
		if err != nil {
			m.h.Log().Warn("design: asking again on the new head", "job", j.ID, "err", err)
		}
		return
	}
	m.awaitSettle(j.ID, *card)
}

// followed is what came back for a job and when it was looked at, so the agent never reports
// a state older than it says.
func followed(j Job) string {
	var b strings.Builder
	if j.TaskID != "" {
		fmt.Fprintf(&b, " Task %s: %s", j.TaskID, or(j.TaskStatus, "not read yet"))
		if j.TaskChecked != "" {
			fmt.Fprintf(&b, " (checked %s)", j.TaskChecked)
		}
		b.WriteString(".")
	}
	if n := len(j.Replies); n > 0 {
		fmt.Fprintf(&b, " %d repl(ies) from the Designer, the latest %s.", n, j.Replies[n-1].Sent)
	} else {
		b.WriteString(" No reply from the Designer yet.")
	}
	if j.InboxChecked != "" {
		fmt.Fprintf(&b, " Mail checked %s.", j.InboxChecked)
	}
	if j.PRURL != "" {
		fmt.Fprintf(&b, " PR %s.", j.PRURL)
	}
	return b.String()
}

// knowledge is the slot's: the job in front, its status and its open notes.
func (m *mod) knowledge(ctx context.Context, _ module.Host, s module.Situation) (string, error) {
	var b strings.Builder
	if s.Focused != nil && strings.HasPrefix(s.Focused.Ref, "design:d_") {
		id := strings.TrimPrefix(s.Focused.Ref, "design:")
		j, err := m.st.get(ctx, id)
		if err != nil {
			return "", nil //nolint:nilerr // a job that is gone adds no context; that is not a failure
		}
		fmt.Fprintf(&b, "The design job in front: %s “%s”, status %s, branch %s.", j.ID, j.Title, j.Status, j.Branch)
		if j.PreviewURL == "" {
			b.WriteString(" No preview URL yet (it is set from the Designer's reply).")
		}
		b.WriteString(followed(j))
		if v, ok := s.Focused.Extra["showing"]; ok {
			fmt.Fprintf(&b, " The window shows the %v", v)
			if sec, ok := s.Focused.Extra["section"]; ok {
				fmt.Fprintf(&b, ", section %v", sec)
			}
			b.WriteString(".")
		}
		b.WriteString("\n")
		notes, _ := m.openNotes(ctx, id)
		if len(notes) == 0 {
			b.WriteString("No open notes.\n")
		} else {
			b.WriteString("Open notes (the next round's feedback):\n")
			for _, n := range notes {
				fmt.Fprintf(&b, "- [%s] %s\n", n.Where, n.Body)
			}
		}
		return b.String(), nil
	}
	jobs, err := m.st.list(ctx, true)
	if err != nil || len(jobs) == 0 {
		return "", err
	}
	b.WriteString("Open design jobs:\n")
	for i, j := range jobs {
		if i == 8 {
			break
		}
		fmt.Fprintf(&b, "- %s “%s”: %s.%s\n", j.ID, j.Title, j.Status, followed(j))
	}
	return b.String(), nil
}
