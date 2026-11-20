package workbench

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/hollis-labs/surface/module"
)

// Eval runs happen in a child process: the app's own binary started again with the hidden
// argument workbench-eval. nanite/evals changes the process's working directory for each
// case, which the app's server must never see (its own agent, its long-lived helper
// processes, relative paths). The parent never changes directory; it sends the request as
// JSON on the child's stdin, reads progress and the result as JSON lines on its stdout, and
// canceling a run kills the child.
//
// The request carries the gateway's URL and the key file's path, never the key: the child
// reads the key file per request as the parent would.

const (
	childArg = "workbench-eval"
	childEnv = "SURFACE_WORKBENCH_EVAL_CHILD"
)

// childRequest is what the parent sends the child.
type childRequest struct {
	Run   Run               `json:"run"`
	Suite Suite             `json:"suite"`
	Tree  map[string][]byte `json:"tree"`
	// GatewayURL and KeyFile are set for a live run only.
	GatewayURL string `json:"gateway_url,omitempty"`
	KeyFile    string `json:"key_file,omitempty"`
}

// childLine is one line of the child's stdout.
type childLine struct {
	Progress string `json:"progress,omitempty"` // a case id, graded
	Result   *Run   `json:"result,omitempty"`
	Error    string `json:"error,omitempty"`
}

// isChild reports whether this process was started as an eval child.
func isChild() bool {
	return len(os.Args) > 1 && os.Args[1] == childArg && os.Getenv(childEnv) == "1"
}

// childMain is the eval child: one request in, the run out. It returns the exit code.
func childMain(in io.Reader, out io.Writer) int {
	var mu sync.Mutex
	enc := json.NewEncoder(out)
	emit := func(l childLine) {
		mu.Lock()
		defer mu.Unlock()
		_ = enc.Encode(l)
	}
	var req childRequest
	if err := json.NewDecoder(io.LimitReader(in, 64<<20)).Decode(&req); err != nil {
		emit(childLine{Error: "workbench eval child: request: " + err.Error()})
		return 2
	}
	// The working directory changes per case: start from a directory of our own.
	if tmp, err := os.MkdirTemp("", "workbench-eval-"); err == nil {
		defer func() { _ = os.RemoveAll(tmp) }()
		_ = os.Chdir(tmp)
	}
	gw := module.Gateway{URL: req.GatewayURL, KeyFile: req.KeyFile, Model: req.Run.Model}
	run := executeSuite(context.Background(), &naniteRuntime{}, req.Run, req.Suite, Tree(req.Tree), gw,
		func(id string) { emit(childLine{Progress: id}) })
	emit(childLine{Result: &run})
	return 0
}

// execute runs a suite in a child process and returns the finished run. Canceling ctx
// kills the child.
func (x *Runner) execute(ctx context.Context, run Run, s Suite, t Tree, gw module.Gateway) Run {
	fail := func(why string) Run {
		run.Status, run.Stopped, run.Finished = "failed", scrub(why), x.now().UTC().Format(time.RFC3339)
		return run
	}
	exe := x.childExe
	if exe == "" {
		var err error
		if exe, err = os.Executable(); err != nil {
			return fail("the app's binary cannot be found to run the eval: " + err.Error())
		}
	}
	req := childRequest{Run: run, Suite: s, Tree: t}
	if run.Mode == "live" {
		req.GatewayURL, req.KeyFile = gw.URL, gw.KeyFile
	}
	body, err := json.Marshal(req)
	if err != nil {
		return fail(err.Error())
	}
	// The child's temporary files (its own, the scenarios', nanite/evals') all go under a
	// root the parent makes and removes after the child is gone, so a cancel leaks nothing.
	root, err := os.MkdirTemp(x.tmpBase, "workbench-child-")
	if err != nil {
		return fail(err.Error())
	}
	defer func() { _ = os.RemoveAll(root) }()
	cmd := exec.CommandContext(ctx, exe, childArg) //nolint:gosec // the app's own binary
	cmd.Env = childEnviron(root)
	cmd.Dir = root
	cmd.Stdin = bytes.NewReader(body)
	cmd.WaitDelay = 5 * time.Second
	var stderr tail
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fail(err.Error())
	}
	if err := cmd.Start(); err != nil {
		return fail("the eval child did not start: " + err.Error())
	}
	x.mu.Lock()
	if x.children == nil {
		x.children = map[string]int{}
	}
	x.children[run.ID] = cmd.Process.Pid
	x.mu.Unlock()
	defer func() {
		x.mu.Lock()
		delete(x.children, run.ID)
		x.mu.Unlock()
	}()
	var result *Run
	var childErr string
	graded := 0
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		var l childLine
		if json.Unmarshal(sc.Bytes(), &l) != nil {
			continue
		}
		switch {
		case l.Progress != "":
			graded++
			x.h.Events().Publish("workbench.run", map[string]any{"agent": run.Agent, "run": run.ID, "status": "running", "graded": graded, "case": l.Progress})
		case l.Result != nil:
			result = l.Result
		case l.Error != "":
			childErr = l.Error
		}
	}
	waitErr := cmd.Wait()
	switch {
	case ctx.Err() != nil:
		run.Status, run.Stopped, run.Finished = "canceled", "canceled by the owner; the eval process was stopped", x.now().UTC().Format(time.RFC3339)
		return run
	case result != nil:
		return *result
	case childErr != "":
		return fail(childErr)
	case waitErr != nil:
		return fail(fmt.Sprintf("the eval process ended: %v %s", waitErr, stderr.String()))
	}
	return fail("the eval process ended without a result")
}

// childEnviron is the child's whole environment: what it needs and nothing else of the
// server's (no tokens or keys the app was started with). Its temporary directory is root.
func childEnviron(root string) []string {
	env := []string{childEnv + "=1", "TMPDIR=" + root}
	for _, k := range []string{"PATH", "HOME", "LANG", "LC_ALL", "TZ"} {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	return env
}

// ChildPID is the process id of a run's eval child while it runs, for tests and diagnosis.
func (x *Runner) ChildPID(runID string) (int, bool) {
	x.mu.Lock()
	defer x.mu.Unlock()
	pid, ok := x.children[runID]
	return pid, ok
}

// tail keeps the last few KiB written to it.
type tail struct {
	mu  sync.Mutex
	buf []byte
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > 4096 {
		t.buf = t.buf[len(t.buf)-4096:]
	}
	return len(p), nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.TrimSpace(string(t.buf))
}

var errNoChild = errors.New("not an eval child")

// runChildIfAsked runs the eval child and exits when this process was started as one.
func runChildIfAsked() error {
	if !isChild() {
		return errNoChild
	}
	os.Exit(childMain(os.Stdin, os.Stdout))
	return nil
}
