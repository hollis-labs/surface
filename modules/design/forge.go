package design

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Progress is what the branch and its pull request say about a job.
type Progress struct {
	// Exists says the branch has been pushed.
	Exists bool   `json:"exists"`
	Head   string `json:"head,omitempty"`
	// HeadMessage is the head commit's first line; HeadDate when it was made.
	HeadMessage string   `json:"head_message,omitempty"`
	HeadDate    string   `json:"head_date,omitempty"`
	Files       []string `json:"files"`
	// PR is the pull request from the branch, when there is one.
	PR *PullRequest `json:"pr,omitempty"`
	// Problem is why it could not be read, in a line.
	Problem string `json:"problem,omitempty"`
	Read    string `json:"read"`
	// limited: GitHub refused for its rate limit (or the token); the reader backs off.
	limited bool
}

// PullRequest is the part of a pull request a review needs.
type PullRequest struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
	State  string `json:"state"` // open, closed, merged
	Draft  bool   `json:"draft,omitempty"`
	Title  string `json:"title,omitempty"`
}

// github reads branches, comparisons and pull requests over GitHub's REST API. A token is
// optional (a public repository reads without one, at a lower rate limit).
type github struct {
	api       string
	tokenFile func() string
	client    *http.Client

	mu    sync.Mutex
	cache map[string]cached
	// quiet is until when GitHub is not asked again after it refused.
	quiet time.Time
}

// backoff is how long GitHub is left alone after it refuses.
const backoff = 5 * time.Minute

var errRefusedGH = errors.New("GitHub refused")

type cached struct {
	at time.Time
	p  Progress
}

var errNoBranch = errors.New("not found")

func (g *github) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.api+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if f := g.tokenFile(); f != "" {
		if t, terr := readToken(f); terr == nil {
			req.Header.Set("Authorization", "Bearer "+t)
		}
	}
	res, err := g.client.Do(req)
	if err != nil {
		return fmt.Errorf("GitHub cannot be reached: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	switch {
	case res.StatusCode == http.StatusNotFound:
		return errNoBranch
	case res.StatusCode == http.StatusForbidden || res.StatusCode == http.StatusTooManyRequests:
		return fmt.Errorf("%w (%s): the rate limit, or a token without access (design.github_token_file)", errRefusedGH, res.Status)
	case res.StatusCode/100 != 2:
		return fmt.Errorf("GitHub answered %s", res.Status)
	}
	return json.NewDecoder(res.Body).Decode(out)
}

// progress reads a branch against base. Answers are kept 20 seconds, so a pane that polls
// and an agent that asks share one read.
func (g *github) progress(ctx context.Context, repo, base, branch string, fresh bool) Progress {
	key := repo + "|" + base + "|" + branch
	g.mu.Lock()
	c, ok := g.cache[key]
	if ok && !fresh && time.Since(c.at) < 20*time.Second {
		g.mu.Unlock()
		return c.p
	}
	if time.Now().Before(g.quiet) { // backing off: the last good read, said to be old
		g.mu.Unlock()
		return lastKnown(c, ok, "GitHub refused (rate limit); not asked again until "+g.quiet.UTC().Format(time.RFC3339))
	}
	g.mu.Unlock()
	p := g.read(ctx, repo, base, branch)
	g.mu.Lock()
	defer g.mu.Unlock()
	if p.limited {
		g.quiet = time.Now().Add(backoff)
		return lastKnown(c, ok, p.Problem)
	}
	if g.cache == nil {
		g.cache = map[string]cached{}
	}
	g.cache[key] = cached{time.Now(), p}
	return p
}

// lastKnown is the last good read with a line saying why it is not fresh, or the problem alone.
func lastKnown(c cached, ok bool, why string) Progress {
	if !ok || c.p.Problem != "" {
		return Progress{Files: []string{}, Read: stamp(), Problem: why}
	}
	p := c.p
	p.Problem = why + "; showing what was read at " + p.Read
	return p
}

func (g *github) read(ctx context.Context, repo, base, branch string) Progress {
	p := Progress{Files: []string{}, Read: stamp()}
	if repo == "" || branch == "" {
		p.Problem = "no repository or branch to read"
		return p
	}
	var cmp struct {
		Commits []struct {
			SHA    string `json:"sha"`
			Commit struct {
				Message string `json:"message"`
				Author  struct {
					Date string `json:"date"`
				} `json:"author"`
			} `json:"commit"`
		} `json:"commits"`
		Files []struct {
			Filename string `json:"filename"`
		} `json:"files"`
	}
	err := g.get(ctx, fmt.Sprintf("/repos/%s/compare/%s...%s", repo, url.PathEscape(base), url.PathEscape(branch)), &cmp)
	switch {
	case errors.Is(err, errNoBranch):
		p.Problem = "the branch " + branch + " has not been pushed yet"
		return p
	case err != nil:
		p.Problem = err.Error()
		p.limited = errors.Is(err, errRefusedGH)
		return p
	}
	p.Exists = true
	if n := len(cmp.Commits); n > 0 {
		last := cmp.Commits[n-1]
		p.Head = last.SHA
		p.HeadMessage, _, _ = strings.Cut(last.Commit.Message, "\n")
		p.HeadDate = last.Commit.Author.Date
	}
	for _, f := range cmp.Files {
		p.Files = append(p.Files, f.Filename)
	}
	owner, _, _ := strings.Cut(repo, "/")
	var prs []struct {
		Number   int    `json:"number"`
		HTMLURL  string `json:"html_url"`
		State    string `json:"state"`
		Draft    bool   `json:"draft"`
		Title    string `json:"title"`
		MergedAt string `json:"merged_at"`
	}
	if err := g.get(ctx, fmt.Sprintf("/repos/%s/pulls?state=all&head=%s", repo, url.QueryEscape(owner+":"+branch)), &prs); err != nil {
		p.Problem = "the pull request could not be read: " + err.Error()
		p.limited = errors.Is(err, errRefusedGH)
		return p
	}
	if len(prs) > 0 {
		pr := prs[0]
		state := pr.State
		if pr.MergedAt != "" {
			state = "merged"
		}
		p.PR = &PullRequest{Number: pr.Number, URL: pr.HTMLURL, State: state, Draft: pr.Draft, Title: pr.Title}
	}
	return p
}

// advance moves a job's status along from what the branch shows. It never moves a job back,
// and leaves the reviewer's own statuses (changes requested, approved) until the pull request
// is merged.
func advance(status string, p Progress) string {
	if p.PR != nil && p.PR.State == "merged" {
		return StatusMerged
	}
	switch status {
	case StatusRequested:
		if p.PR != nil && p.PR.State == "open" && !p.PR.Draft {
			return StatusReady
		}
		if p.Exists && p.Head != "" {
			return StatusInProgress
		}
	case StatusInProgress:
		if p.PR != nil && p.PR.State == "open" && !p.PR.Draft {
			return StatusReady
		}
	}
	return status
}
