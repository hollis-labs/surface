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
	// stale: an earlier read shown while GitHub is left alone.
	stale bool
}

// PullRequest is the part of a pull request a review needs.
type PullRequest struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
	State  string `json:"state"` // open, closed, merged
	Draft  bool   `json:"draft,omitempty"`
	Title  string `json:"title,omitempty"`
	// HeadSHA is the pull request's head: for a merged one, the commit that was merged (not the
	// merge commit).
	HeadSHA string `json:"head_sha,omitempty"`
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
	quiet   time.Time
	atCache map[string]cachedPulls
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
func (g *github) progress(ctx context.Context, repo, base, branch string, prNumber int, fresh bool) Progress {
	key := fmt.Sprintf("%s|%s|%s|%d", repo, base, branch, prNumber)
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
	p := g.read(ctx, repo, base, branch, prNumber)
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
	p.stale = true
	return p
}

// read reads the branch against base, and its pull request: the one numbered prNumber (the
// job's recorded pull request) when given, else the first opened from the branch.
func (g *github) read(ctx context.Context, repo, base, branch string, prNumber int) Progress {
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
	if prNumber > 0 {
		var pr ghPull
		if err := g.get(ctx, fmt.Sprintf("/repos/%s/pulls/%d", repo, prNumber), &pr); err != nil {
			p.Problem = "the pull request could not be read: " + err.Error()
			p.limited = errors.Is(err, errRefusedGH)
			return p
		}
		p.PR = pr.toPR()
		return p
	}
	owner, _, _ := strings.Cut(repo, "/")
	var prs []ghPull
	if err := g.get(ctx, fmt.Sprintf("/repos/%s/pulls?state=all&head=%s", repo, url.QueryEscape(owner+":"+branch)), &prs); err != nil {
		p.Problem = "the pull request could not be read: " + err.Error()
		p.limited = errors.Is(err, errRefusedGH)
		return p
	}
	if len(prs) > 0 {
		p.PR = prs[0].toPR()
	}
	return p
}

// ghPull is a pull request as GitHub's REST API gives it.
type ghPull struct {
	Number   int    `json:"number"`
	HTMLURL  string `json:"html_url"`
	State    string `json:"state"`
	Draft    bool   `json:"draft"`
	Title    string `json:"title"`
	MergedAt string `json:"merged_at"`
	Head     struct {
		SHA string `json:"sha"`
	} `json:"head"`
}

func (pr ghPull) toPR() *PullRequest {
	state := pr.State
	if pr.MergedAt != "" {
		state = "merged"
	}
	return &PullRequest{Number: pr.Number, URL: pr.HTMLURL, State: state, Draft: pr.Draft, Title: pr.Title, HeadSHA: strings.ToLower(pr.Head.SHA)}
}

// pullsAt lists the pull requests GitHub associates with a commit: what else may have merged it.
// Answers are kept 20 seconds; while GitHub is being left alone it is not asked.
func (g *github) pullsAt(ctx context.Context, repo, sha string, fresh bool) ([]PullRequest, error) {
	key := "at|" + repo + "|" + sha
	g.mu.Lock()
	if c, ok := g.atCache[key]; ok && !fresh && time.Since(c.at) < 20*time.Second {
		g.mu.Unlock()
		return c.prs, nil
	}
	if time.Now().Before(g.quiet) {
		g.mu.Unlock()
		return nil, errRefusedGH
	}
	g.mu.Unlock()
	var raw []ghPull
	if err := g.get(ctx, fmt.Sprintf("/repos/%s/commits/%s/pulls", repo, url.PathEscape(sha)), &raw); err != nil {
		if errors.Is(err, errRefusedGH) {
			g.mu.Lock()
			g.quiet = time.Now().Add(backoff)
			g.mu.Unlock()
		}
		return nil, err
	}
	out := make([]PullRequest, 0, len(raw))
	for _, r := range raw {
		out = append(out, *r.toPR())
	}
	g.mu.Lock()
	if g.atCache == nil {
		g.atCache = map[string]cachedPulls{}
	}
	g.atCache[key] = cachedPulls{time.Now(), out}
	g.mu.Unlock()
	return out, nil
}

type cachedPulls struct {
	at  time.Time
	prs []PullRequest
}

// prRef reads a GitHub pull request URL: its repository (lower case) and number.
func prRef(u string) (repo string, n int, ok bool) {
	pu, err := url.Parse(u)
	if err != nil || !strings.EqualFold(pu.Host, "github.com") {
		return "", 0, false
	}
	parts := strings.Split(strings.Trim(pu.Path, "/"), "/")
	if len(parts) != 4 || parts[2] != "pull" {
		return "", 0, false
	}
	if _, err := fmt.Sscanf(parts[3], "%d", &n); err != nil || n <= 0 || fmt.Sprint(n) != parts[3] {
		return "", 0, false
	}
	return strings.ToLower(parts[0] + "/" + parts[1]), n, true
}

// samePR says two pull request URLs name the same pull request (repository and number).
func samePR(a, b string) bool {
	ra, na, oka := prRef(a)
	rb, nb, okb := prRef(b)
	return oka && okb && ra == rb && na == nb
}

// prNumberIn is the number of the job's recorded pull request when it is one in repo, else 0.
func prNumberIn(u, repo string) int {
	r, n, ok := prRef(u)
	if !ok || r != strings.ToLower(repo) {
		return 0
	}
	return n
}

// mergedOnGitHub reads what GitHub says was merged against the approval: the job's own pull
// request (pr) and the pull requests that contain the approved commit (atHead). A job is merged
// only when all three hold: the merged pull request is the approved one (same repository and
// number), its head is the approved commit, and the job is still approved. Any other merged pull
// request is a warning naming both pull requests and both commits, and the status stays.
func mergedOnGitHub(j Job, pr *PullRequest, atHead []PullRequest) (merged bool, warning string) {
	if j.Status == StatusMerged {
		return false, ""
	}
	var seen []PullRequest
	if pr != nil {
		seen = append(seen, *pr)
	}
	seen = append(seen, atHead...)
	var warnings []string
	for _, m := range seen {
		if m.State != "merged" {
			continue
		}
		switch {
		case j.ApprovedHead == "":
			warnings = append(warnings, fmt.Sprintf("%s was merged at %s, and the job was never approved", m.URL, or(m.HeadSHA, "an unknown commit")))
		case !samePR(m.URL, j.ApprovedPR):
			warnings = append(warnings, fmt.Sprintf("%s was merged at %s, but the approval is for %s at %s", m.URL, or(m.HeadSHA, "an unknown commit"), or(j.ApprovedPR, "no pull request"), j.ApprovedHead))
		case m.HeadSHA != j.ApprovedHead:
			warnings = append(warnings, fmt.Sprintf("%s was merged at %s, but the approved commit is %s", m.URL, or(m.HeadSHA, "an unknown commit"), j.ApprovedHead))
		case j.Status != StatusApproved:
			warnings = append(warnings, fmt.Sprintf("%s was merged at the approved commit %s, but the job is %s; not moved", m.URL, j.ApprovedHead, j.Status))
		default:
			merged = true
		}
	}
	if merged || len(warnings) == 0 {
		return merged, ""
	}
	return false, "merged on GitHub without approval of this commit: " + strings.Join(dedupe(warnings), "; ")
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := in[:0]
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// advance moves a job's status along from what the branch shows. It never moves a job back and
// never touches the reviewer's own statuses (changes requested, approved) or a settled job. A
// merged pull request is not read here: mergedOnGitHub holds it to the approval.
func advance(status string, p Progress) string {
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
