package tetherlink

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestMain doubles as a fake MCP stdio server when TETHERLINK_FAKE is set: it answers
// initialize, and tools/call by echoing the tool's name and its process id; the tool "slow"
// never answers and the tool "die" ends the process.
func TestMain(m *testing.M) {
	if os.Getenv("TETHERLINK_FAKE") == "1" {
		fake()
		return
	}
	os.Exit(m.Run())
}

func fake() {
	sc := bufio.NewScanner(os.Stdin)
	out := json.NewEncoder(os.Stdout)
	for sc.Scan() {
		var req struct {
			ID     int64  `json:"id"`
			Method string `json:"method"`
			Params struct {
				Name string `json:"name"`
			} `json:"params"`
		}
		if json.Unmarshal(sc.Bytes(), &req) != nil || req.ID == 0 {
			continue
		}
		switch {
		case req.Method == "initialize":
			_ = out.Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{}})
		case req.Params.Name == "slow":
			// never answers
		case req.Params.Name == "die":
			os.Exit(0)
		default:
			text := fmt.Sprintf(`{"tool":%q,"pid":%d}`, req.Params.Name, os.Getpid())
			_ = out.Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{"content": []map[string]any{{"type": "text", "text": text}}}})
		}
	}
}

func fakeClient(t *testing.T, timeout time.Duration) (*Stdio, *procPool) {
	t.Helper()
	t.Setenv("TETHERLINK_FAKE", "1")
	// A command line of its own per test, so tests do not share a pool.
	argv := []string{"env", "TETHERLINK_FAKE=1", "TEST=" + t.Name(), os.Args[0], "-test.run=^$"}
	c := &Stdio{Module: "test", Argv: argv, Timeout: timeout, Allowed: Allowed{"a": true, "b": true, "slow": true, "die": true}}
	return c, poolFor(argv)
}

func pid(t *testing.T, text string) int {
	t.Helper()
	var r struct{ PID int }
	if err := json.Unmarshal([]byte(text), &r); err != nil {
		t.Fatalf("%q: %v", text, err)
	}
	return r.PID
}

func TestASessionIsKeptAndReused(t *testing.T) {
	c, pl := fakeClient(t, 5*time.Second)
	base := pl.started.Load()
	ctx := context.Background()
	first, err := c.Call(ctx, "a", nil)
	if err != nil {
		t.Fatal(err)
	}
	for range 5 {
		next, err := c.Call(ctx, "b", nil)
		if err != nil {
			t.Fatal(err)
		}
		if pid(t, next) != pid(t, first) {
			t.Fatal("a second process was started for a call in sequence")
		}
	}
	// Another client with the same command line shares it.
	other := &Stdio{Module: "other", Argv: c.Argv, Timeout: c.Timeout, Allowed: c.Allowed}
	if again, err := other.Call(ctx, "a", nil); err != nil || pid(t, again) != pid(t, first) {
		t.Fatalf("%v %v", again, err)
	}
	if n := pl.started.Load() - base; n > 1 {
		t.Fatalf("started %d processes", n)
	}
}

func TestATimedOutSessionIsNotReused(t *testing.T) {
	c, pl := fakeClient(t, 300*time.Millisecond)
	base := pl.started.Load()
	ctx := context.Background()
	if _, err := c.Call(ctx, "slow", nil); err == nil || !strings.Contains(err.Error(), "did not answer") {
		t.Fatalf("%v", err)
	}
	if _, err := c.Call(ctx, "a", nil); err != nil {
		t.Fatal(err)
	}
	if n := pl.started.Load() - base; n < 1 {
		t.Fatalf("started %d processes, want a fresh one after the timeout", n)
	}
}

func TestADeadSessionIsReplacedAndTheCallNotResent(t *testing.T) {
	c, pl := fakeClient(t, 5*time.Second)
	base := pl.started.Load()
	ctx := context.Background()
	if _, err := c.Call(ctx, "die", nil); err == nil {
		t.Fatal("a call the command died on succeeded")
	}
	if _, err := c.Call(ctx, "a", nil); err != nil {
		t.Fatal(err)
	}
	if n := pl.started.Load() - base; n < 1 {
		t.Fatalf("started %d, want a fresh one after the death", n)
	}
}

func TestCallsInParallelUseAtMostMaxProcs(t *testing.T) {
	c, pl := fakeClient(t, 5*time.Second)
	base := pl.started.Load()
	ctx := context.Background()
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for range 20 {
		wg.Go(func() {
			if _, err := c.Call(ctx, "a", nil); err != nil {
				errs <- err
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if n := pl.started.Load() - base; n > maxProcs {
		t.Fatalf("started %d", n)
	}
}
