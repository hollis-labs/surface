package design

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/hollis-labs/surface/module"
)

// The way back. While a job is open (neither approved nor merged) the module reads, on a
// timer and on demand (design_refresh):
//   - the Designer's replies on the job's thread, design-<job id>, in this app's mailbox;
//   - the job's Torque task: its status and its latest comment.
//
// Only a job this instance holds is read, one thread at a time, so mail for another instance
// that shares the mailbox, or on any other thread, is never fetched, stored or marked read.
// A message on a job's thread from anyone but the configured Designer is left unread and
// untouched. A reply's body is never taken as instructions: it is stored and shown as text,
// and only its labeled lines (preview:, base:, pr:, head:) set fields, each checked first.

const (
	maxBody    = 16 << 10
	maxSubject = 300
)

// defaultEvery is how often open jobs are checked.
const defaultEvery = time.Minute

var shaPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

// parsed is what a reply's labeled lines set, and what was refused.
type parsed struct {
	Preview, Base, PR, Head string
	Ignored                 []string
}

// parseReply reads the labeled lines of a reply. Each is "label: value" on a line of its own;
// the value is its first word. A URL must be http(s); a pull request must be in repo; a head
// is a full commit SHA. Anything else on the line, and every other line, is only text.
func parseReply(body, repo string) parsed {
	var p parsed
	for line := range strings.SplitSeq(body, "\n") {
		l := strings.TrimSpace(line)
		l = strings.TrimLeft(l, "-* ")
		key, val, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		if key != "preview" && key != "base" && key != "pr" && key != "head" {
			continue
		}
		fields := strings.Fields(val)
		if len(fields) == 0 {
			continue
		}
		v := strings.Trim(fields[0], "<>`\"'")
		refuse := func(why string) { p.Ignored = append(p.Ignored, key+": "+why) }
		switch key {
		case "preview", "base":
			if !webURL(v) {
				refuse("not an http(s) URL")
				continue
			}
			if key == "preview" {
				p.Preview = v
			} else {
				p.Base = v
			}
		case "pr":
			if !webURL(v) {
				refuse("not an http(s) URL")
				continue
			}
			if !prInRepo(v, repo) {
				refuse("not a pull request in " + or(repo, "the configured repository"))
				continue
			}
			p.PR = v
		case "head":
			if v = strings.ToLower(v); !shaPattern.MatchString(v) {
				refuse("not a full commit SHA")
				continue
			}
			p.Head = v
		}
	}
	return p
}

func webURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil
}

// prInRepo says the URL is https://github.com/<repo>/pull/<number>.
func prInRepo(s, repo string) bool {
	u, err := url.Parse(s)
	if err != nil || !strings.EqualFold(u.Host, "github.com") || repo == "" {
		return false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 4 || !strings.EqualFold(parts[0]+"/"+parts[1], repo) || parts[2] != "pull" {
		return false
	}
	for _, r := range parts[3] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return parts[3] != ""
}

// taskWorking says the task has been picked up.
func taskWorking(status string) bool {
	switch strings.ToLower(status) {
	case "doing", "in_progress", "in progress", "running", "review", "blocked", "done":
		return true
	}
	return false
}

// derive moves a job's status from what came back. It never moves a job back and leaves
// approved and merged alone: requested goes to in progress when the task is being worked or a
// reply arrives, and to ready for review when there is a preview; a reply that carries a
// preview after changes were requested makes it ready for review again.
func derive(status, taskStatus, preview string, replied, replyPreview bool) string {
	switch status {
	case StatusRequested, StatusInProgress:
		if preview != "" {
			return StatusReady
		}
		if status == StatusRequested && (replied || taskWorking(taskStatus)) {
			return StatusInProgress
		}
	case StatusChangesRequested:
		if replyPreview {
			return StatusReady
		}
	}
	return status
}

// poll checks the open jobs until ctx ends.
func (m *mod) poll(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if _, err := m.refresh(ctx, ""); err != nil && !errors.Is(err, errNotConfigured) && ctx.Err() == nil {
				m.h.Log().Warn("design: checking the jobs", "err", err)
			}
		}
	}
}

// Checked is what one check of one job found.
type Checked struct {
	ID      string `json:"id"`
	Replies int    `json:"new_replies"`
	Status  string `json:"status"`
	Problem string `json:"problem,omitempty"`
}

// refresh checks one job, or every open job when id is "", for replies and the task's state.
func (m *mod) refresh(ctx context.Context, id string) ([]Checked, error) {
	m.checking.Lock()
	defer m.checking.Unlock()
	var jobs []Job
	if id != "" {
		j, err := m.st.get(ctx, id)
		if err != nil {
			return nil, err
		}
		jobs = []Job{j}
	} else {
		var err error
		if jobs, err = m.st.open(ctx); err != nil {
			return nil, err
		}
	}
	if len(jobs) == 0 {
		return []Checked{}, nil
	}
	d, err := m.dispatcher()
	if err != nil {
		return nil, err
	}
	r, ok := d.(Reader)
	if !ok {
		return nil, errors.New("the configured transport cannot read replies")
	}
	out := make([]Checked, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, m.check(ctx, r, j))
	}
	return out, nil
}

// check reads one job's thread and task and applies what they say.
func (m *mod) check(ctx context.Context, r Reader, j Job) Checked {
	cfg := m.h.Config()
	sender, designer := cfg.String("sender_urn"), cfg.String("designer_urn")
	thread := "design-" + j.ID
	res := Checked{ID: j.ID}
	var problems []string
	changed := false
	looked := Change{} // the checked times and the task's state: written by the reader alone

	msgs, err := r.Unread(ctx, sender, thread)
	if err != nil {
		problems = append(problems, "replies: "+err.Error())
	} else {
		sort.SliceStable(msgs, func(a, b int) bool { return msgs[a].Sent < msgs[b].Sent })
		for _, msg := range msgs {
			// Only the Designer, only this job's thread, only to this app. Anything else is left
			// as it is: unread, unstored.
			if designer == "" || msg.From != designer || msg.Thread != thread || (msg.To != "" && msg.To != sender) || msg.ID == "" {
				continue
			}
			p := parseReply(msg.Body, cfg.String("repo"))
			body := msg.Body
			if len(body) > maxBody {
				body = strings.ToValidUTF8(body[:maxBody], "") + "…"
			}
			subject := msg.Subject
			if len(subject) > maxSubject {
				subject = strings.ToValidUTF8(subject[:maxSubject], "")
			}
			// Stored and applied together; marked read only once that is done. On a failure the
			// message stays unread and this pass stops, so the next one takes it, and the ones
			// after it, again in order.
			isNew, ingestErr := m.st.ingest(ctx, j.ID, Reply{
				MessageID: msg.ID, From: msg.From, Subject: subject, Body: body, Sent: msg.Sent, Received: stamp(),
				PreviewURL: p.Preview, BaseURL: p.Base, PRURL: p.PR, HeadSHA: p.Head, Ignored: strings.Join(p.Ignored, "\n"),
			})
			if ingestErr != nil {
				problems = append(problems, "taking a reply: "+ingestErr.Error())
				break
			}
			if isNew {
				res.Replies++
				changed = true
			}
			if markErr := r.MarkRead(ctx, sender, msg.ID); markErr != nil {
				// Applied already; it is read again next time, found stored, and marked then.
				problems = append(problems, "marking a reply read: "+markErr.Error())
			}
		}
		if len(problems) == 0 {
			now := stamp()
			looked.InboxChecked = &now
		}
	}

	if j.TaskID != "" {
		ts, taskErr := r.Task(ctx, j.TaskID)
		if taskErr != nil {
			problems = append(problems, "task: "+taskErr.Error())
		} else {
			now := stamp()
			comment := ""
			if ts.Comment != "" {
				comment = fmt.Sprintf("%s (%s, %s)", strings.TrimSpace(ts.Comment), or(ts.CommentAuthor, "someone"), ts.CommentAt)
				if len(comment) > 2000 {
					comment = strings.ToValidUTF8(comment[:2000], "") + "…"
				}
			}
			if ts.Status != j.TaskStatus || comment != j.TaskComment {
				changed = true
				looked.TaskStatus, looked.TaskComment = &ts.Status, &comment
			}
			looked.TaskChecked = &now
		}
	}
	if !looked.empty() {
		if _, err = m.st.update(ctx, j.ID, looked); err != nil {
			problems = append(problems, err.Error())
		}
	}

	// The status, from a fresh read, moved only forward and only if no one changed it since.
	if cur, err := m.st.get(ctx, j.ID); err == nil {
		j = cur
		if next := derive(j.Status, j.TaskStatus, j.PreviewURL, false, false); next != j.Status {
			moved, moveErr := m.st.moveStatus(ctx, j.ID, j.Status, next)
			if moveErr != nil {
				problems = append(problems, moveErr.Error())
			}
			changed = changed || moved
		}
		if cur, err = m.st.get(ctx, j.ID); err == nil {
			j = cur
		}
	} else {
		problems = append(problems, err.Error())
	}
	if changed {
		m.changed(j.ID)
	}
	m.announce(ctx, j)
	res.Status = j.Status
	res.Problem = strings.Join(problems, "; ")
	return res
}

// announce puts a job's review in front of the owner the first time it has a preview: its
// window opens (unless it is open already) and a notice says so. Once per job.
func (m *mod) announce(ctx context.Context, j Job) {
	if j.PreviewURL == "" || j.announced {
		return
	}
	yes := true
	if _, err := m.st.update(ctx, j.ID, Change{Announced: &yes}); err != nil {
		m.h.Log().Warn("design: announcing a preview", "job", j.ID, "err", err)
		return
	}
	showing := false
	for _, w := range m.h.Desk().Windows(ctx) {
		if id, _ := w.Args["id"].(string); w.View == "design.job" && id == j.ID {
			showing = true
		}
	}
	if !showing {
		if _, err := m.open(ctx, j); err != nil {
			m.h.Log().Warn("design: the job's window did not open", "job", j.ID, "err", err)
		}
	}
	payload, _ := json.Marshal(map[string]any{"body": "“" + j.Title + "” has a preview to review: its window is open.", "tone": "info"})
	if _, err := m.h.Cards().Show(ctx, module.Card{Kind: "info", Title: "Ready to review: " + j.Title, Source: "design", Subject: ref(j.ID), Payload: payload}); err != nil {
		m.h.Log().Warn("design: the notice did not show", "job", j.ID, "err", err)
	}
}
