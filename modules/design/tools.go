package design

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/hollis-labs/surface/module"
)

func (m *mod) tools() []module.Tool {
	raw := func(s string) json.RawMessage { return json.RawMessage(s) }
	type idArg struct {
		ID string `json:"id"`
	}
	return []module.Tool{
		{Name: "design_request", Level: module.LevelWrite,
			Description: "Hand a change to the interface to the Design team: records a design job, opens its window, creates a manual Torque task and sends the Designer the brief by Tether. title is short; brief says what should change and why, in the owner's words; references are links, components or screenshots to follow, one per line. When Tether is not configured or fails, the job is still made and the result carries the text to send by hand: show it to the owner.",
			Input:       raw(`{"type":"object","required":["title","brief"],"properties":{"title":{"type":"string"},"brief":{"type":"string"},"references":{"type":"string"}}}`),
			Describe: func(in json.RawMessage) string {
				var a struct{ Title string }
				_ = json.Unmarshal(in, &a)
				return "Send the Design team a new job: “" + a.Title + "”"
			},
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				var a struct{ Title, Brief, References string }
				if err := json.Unmarshal(in, &a); err != nil {
					return nil, err
				}
				return m.request(ctx, a.Title, a.Brief, a.References)
			}},
		{Name: "design_jobs", Level: module.LevelRead,
			Description: "List the design jobs, most recently changed first: status, pull request, preview, head_sha, latest_head, approved_head, head_differs, update_received, the Designer's replies (bodies cut short), the task's status and latest comment, and when the mail (inbox_checked) and the task (task_checked) were last read. Merged and closed jobs are left out unless all is true. To check now, design_refresh.",
			Input:       raw(`{"type":"object","properties":{"all":{"type":"boolean"}}}`),
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				var a struct{ All bool }
				_ = json.Unmarshal(in, &a)
				return m.st.list(ctx, !a.All)
			}},
		{Name: "design_job", Level: module.LevelRead,
			Description: "One design job as it stands: its status, rounds, the Designer's replies in full, the task's status and latest comment, branch head, the files the branch changes, its pull request, preview and open notes; head_sha, latest_head, approved_head and head_differs (a commit came after the approval); update_received (work came back without a preview); with when each was last read (inbox_checked, task_checked, progress.read). Reads the branch from GitHub afresh. Defaults to the job whose window is in front.",
			Input:       raw(`{"type":"object","properties":{"id":{"type":"string"}}}`),
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				var a idArg
				_ = json.Unmarshal(in, &a)
				id, err := m.jobID(ctx, a.ID)
				if err != nil {
					return nil, err
				}
				return m.view(ctx, id, true)
			}},
		{Name: "design_refresh", Level: module.LevelRead,
			Description: "Check now, rather than waiting for the timer: read the Designer's new replies and the task's state for one job (id), or for every open job, and apply them (preview, base, pull request, head, status). Returns what each check found.",
			Input:       raw(`{"type":"object","properties":{"id":{"type":"string","description":"One job; every open job when left out."}}}`),
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				var a idArg
				_ = json.Unmarshal(in, &a)
				checked, err := m.refresh(ctx, a.ID)
				if err != nil {
					return nil, err
				}
				if a.ID != "" {
					v, viewErr := m.view(ctx, a.ID, true)
					return map[string]any{"checked": checked, "job": v}, viewErr
				}
				jobs, err := m.st.list(ctx, true)
				return map[string]any{"checked": checked, "jobs": jobs}, err
			}},
		{Name: "design_set", Level: module.LevelWrite,
			Description: "Correct what is recorded about a job by hand (the Designer's replies set these on their own): preview_url (the gallery served from their branch), base_url (the same from main), pr_url, branch, or status (requested, in progress, ready for review, changes requested, closed). Use approve and feedback through their own tools, not status here.",
			Input:       raw(`{"type":"object","properties":{"id":{"type":"string"},"preview_url":{"type":"string"},"base_url":{"type":"string"},"pr_url":{"type":"string"},"head_sha":{"type":"string"},"branch":{"type":"string"},"status":{"type":"string","enum":["requested","in progress","ready for review","changes requested","closed"]}}}`),
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				var a struct {
					ID string `json:"id"`
					Change
				}
				if err := json.Unmarshal(in, &a); err != nil {
					return nil, err
				}
				id, err := m.jobID(ctx, a.ID)
				if err != nil {
					return nil, err
				}
				return m.set(ctx, id, a.Change)
			}},
		{Name: "design_feedback", Level: module.LevelWrite,
			Description: "Send the next round to the Design team: the job's open notes (pinned in its window) plus text, if any. The notes are closed as sent and the job becomes changes requested. Defaults to the job in front.",
			Input:       raw(`{"type":"object","properties":{"id":{"type":"string"},"text":{"type":"string"}}}`),
			Describe:    func(json.RawMessage) string { return "Send the open notes to the Design team as the next round" },
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				var a struct{ ID, Text string }
				_ = json.Unmarshal(in, &a)
				id, err := m.jobID(ctx, a.ID)
				if err != nil {
					return nil, err
				}
				return m.feedback(ctx, id, a.Text)
			}},
		// Act level on purpose: the approval card this tool raises is the owner's decision. At
		// write level the chat would ask first and the card again, two asks for one decision. What
		// leaves the app on approve (the go-ahead) is only sent once the owner approves the card.
		{Name: "design_approve", Level: module.LevelAct,
			Description: "Ask the owner to approve a design job at its recorded head (the commit the Designer last named) with an approval card, and wait for the answer. Refused when no head is recorded (ask the Designer for a reply with head:) or GitHub shows the branch at another commit. On approve the job records approved_head and the Design team is told to merge exactly that commit; if the head changes while the card is open, the card is superseded and the owner asked again. Surface never merges. Defaults to the job in front.",
			Input:       raw(`{"type":"object","properties":{"id":{"type":"string"},"timeout_seconds":{"type":"integer","description":"How long to wait for the owner. Default 600, at most 3600."}}}`),
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				var a struct {
					ID      string `json:"id"`
					Timeout int    `json:"timeout_seconds"`
				}
				_ = json.Unmarshal(in, &a)
				id, err := m.jobID(ctx, a.ID)
				if err != nil {
					return nil, err
				}
				wait := time.Duration(min(max(a.Timeout, 0), 3600)) * time.Second
				if wait == 0 {
					wait = 10 * time.Minute
				}
				card, err := m.askApproval(ctx, id, wait)
				if err != nil || card == nil {
					return map[string]any{"status": "already settled"}, err
				}
				done, err := m.h.Cards().Await(ctx, card.ID)
				if err != nil {
					_, _ = m.h.Cards().End(context.WithoutCancel(ctx), card.ID, "canceled")
					return nil, err
				}
				return m.settle(context.WithoutCancel(ctx), id, done)
			}},
		{Name: "design_open", Level: module.LevelAct,
			Description: "Open a design job's window: the live preview of its branch, the base, the changed files and the notes.",
			Input:       raw(`{"type":"object","required":["id"],"properties":{"id":{"type":"string"}}}`),
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				var a idArg
				_ = json.Unmarshal(in, &a)
				j, err := m.st.get(ctx, a.ID)
				if err != nil {
					return nil, err
				}
				return m.open(ctx, j)
			}},
	}
}

func (m *mod) routes(h module.Host) {
	h.Handle("GET /jobs", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jobs, err := m.st.list(r.Context(), r.URL.Query().Get("all") != "1")
		reply(w, jobs, err)
	}))
	h.Handle("GET /jobs/{id}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v, err := m.view(r.Context(), r.PathValue("id"), r.URL.Query().Get("fresh") == "1")
		reply(w, v, err)
	}))
	h.Handle("PUT /jobs/{id}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var c Change
		if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
			reply(w, nil, err)
			return
		}
		j, err := m.set(r.Context(), r.PathValue("id"), c)
		reply(w, j, err)
	}))
	h.Handle("POST /jobs/{id}/feedback", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var a struct{ Text string }
		_ = json.NewDecoder(r.Body).Decode(&a)
		out, err := m.feedback(r.Context(), r.PathValue("id"), a.Text)
		reply(w, out, err)
	}))
	// Approve from the window: the same card the agent raises; the answer settles it.
	h.Handle("POST /jobs/{id}/approve", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		card, err := m.askApproval(r.Context(), id, 30*time.Minute)
		if err != nil || card == nil {
			reply(w, map[string]any{"status": "already settled"}, err)
			return
		}
		m.awaitSettle(id, *card)
		reply(w, map[string]any{"card": card.ID}, nil)
	}))
	// Whether a preview answers, from the server (which sits beside the reviewer's browser):
	// a framed page from another origin cannot say so itself.
	h.Handle("GET /jobs/{id}/probe", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		j, err := m.st.get(r.Context(), r.PathValue("id"))
		if err != nil {
			reply(w, nil, err)
			return
		}
		target := j.PreviewURL
		if r.URL.Query().Get("which") == "base" {
			target = j.BaseURL
		}
		reply(w, probe(r.Context(), target), nil)
	}))
}

// set applies a change made by hand: only the fields a person may set, then the same status
// rule the replies follow (a preview makes a requested job ready for review), then the
// announcement if this is the job's first preview.
func (m *mod) set(ctx context.Context, id string, c Change) (Job, error) {
	c = Change{Branch: c.Branch, PRURL: c.PRURL, PreviewURL: c.PreviewURL, BaseURL: c.BaseURL, Status: c.Status, TaskID: c.TaskID, HeadSHA: c.HeadSHA}
	// Approved and merged are reached only through their own rules (the approval card; the merge
	// of the approved commit), never set by hand.
	if c.Status != nil && (*c.Status == StatusApproved || *c.Status == StatusMerged) {
		return Job{}, fmt.Errorf("%s is not set by hand: approval goes through design_approve, and a job is merged when the approved commit is", *c.Status)
	}
	if c.HeadSHA != nil && *c.HeadSHA != "" && !shaPattern.MatchString(strings.ToLower(*c.HeadSHA)) {
		return Job{}, fmt.Errorf("head_sha %q is not a full commit SHA", *c.HeadSHA)
	}
	j, err := m.st.update(ctx, id, c)
	if err != nil {
		return Job{}, err
	}
	if c.Status == nil {
		if next := derive(j.Status, j.TaskStatus, j.PreviewURL, false, false); next != j.Status {
			if _, err = m.st.moveStatus(ctx, id, j.Status, next); err != nil {
				return Job{}, err
			}
			if j, err = m.st.get(ctx, id); err != nil {
				return Job{}, err
			}
		}
	}
	m.changed(id)
	m.announce(ctx, j)
	m.supersede(ctx, j)
	return j, nil
}

// Probe is whether a preview answered.
type Probe struct {
	URL       string `json:"url"`
	Reachable bool   `json:"reachable"`
	Problem   string `json:"problem,omitempty"`
}

func probe(ctx context.Context, target string) Probe {
	p := Probe{URL: target}
	if target == "" {
		p.Problem = "no preview URL is set for this job yet"
		return p
	}
	u, err := url.Parse(target)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		p.Problem = "not an http(s) URL"
		return p
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		msg := err.Error()
		if i := strings.LastIndex(msg, ": "); i >= 0 {
			msg = msg[i+2:]
		}
		p.Problem = "the preview does not answer (" + msg + ")"
		return p
	}
	_ = res.Body.Close()
	if res.StatusCode >= 400 {
		p.Problem = fmt.Sprintf("the preview answered %s", res.Status)
		return p
	}
	p.Reachable = true
	return p
}

func reply(w http.ResponseWriter, v any, err error) {
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		status := http.StatusUnprocessableEntity
		if strings.Contains(err.Error(), errNotFound.Error()) {
			status = http.StatusNotFound
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(v)
}
