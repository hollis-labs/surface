package collab

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hollis-labs/surface/module/tetherlink"
)

// Fake is an in-memory Caster for tests: caster_get, caster_put, caster_ls and caster_log over
// items by "<ns>/<name>", each a list of revisions, answered in the gateway's shapes. It is a
// tetherlink.Caller; Link hands it to a Store.
type Fake struct {
	mu    sync.Mutex
	items map[string][]fakeRev
	// Calls are the tools called, in order.
	Calls []string
	// Down makes every call fail, as an unreachable gateway does.
	Down bool
}

type fakeRev struct {
	content, title, source, at string
}

// NewFake is an empty Caster.
func NewFake() *Fake { return &Fake{items: map[string][]fakeRev{}} }

// Link is the Fake as a Store's Link.
func (f *Fake) Link() (tetherlink.Caller, error) { return f, nil }

// Content is the latest text of "<ns>/<name>", and its revision (0: no such item).
func (f *Fake) Content(key string) (string, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	revs := f.items[key]
	if len(revs) == 0 {
		return "", 0
	}
	return revs[len(revs)-1].content, len(revs)
}

// Put writes a revision directly, as another app would.
func (f *Fake) Put(key, content, title string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.items[key] = append(f.items[key], fakeRev{content: content, title: title, source: "test", at: time.Now().UTC().Format(time.RFC3339Nano)})
	return len(f.items[key])
}

func (f *Fake) Call(_ context.Context, tool string, args map[string]any) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, tool)
	if f.Down {
		return "", fmt.Errorf("gateway unreachable")
	}
	out := func(v any) (string, error) {
		b, err := json.Marshal(v)
		return string(b), err
	}
	switch tool {
	case "caster_put":
		key, _ := args["name"].(string)
		content, _ := args["content"].(string)
		title, _ := args["title"].(string)
		source, _ := args["source"].(string)
		f.items[key] = append(f.items[key], fakeRev{content: content, title: title, source: source, at: time.Now().UTC().Format(time.RFC3339Nano)})
		return out(map[string]any{"ref": fmt.Sprintf("caster://%s@%d", key, len(f.items[key]))})
	case "caster_get":
		ref, _ := args["ref"].(string)
		key, at, pinned := strings.Cut(strings.TrimPrefix(ref, "caster://"), "@")
		revs := f.items[key]
		n := len(revs)
		if pinned {
			_, _ = fmt.Sscan(at, &n)
		}
		if n < 1 || n > len(revs) {
			return out(map[string]any{"state": "not_found"})
		}
		r := revs[n-1]
		return out(map[string]any{"state": "resolved", "content": r.content, "title": r.title, "revision": fmt.Sprint(n)})
	case "caster_ls":
		ns, _ := args["ns"].(string)
		items := []map[string]any{}
		for key, revs := range f.items {
			if !strings.HasPrefix(key, ns+"/") {
				continue
			}
			last := revs[len(revs)-1]
			items = append(items, map[string]any{"ref": "caster://" + key, "rev": len(revs), "title": last.title, "updated": last.at})
		}
		sort.Slice(items, func(i, j int) bool { return items[i]["ref"].(string) < items[j]["ref"].(string) })
		return out(map[string]any{"items": items})
	case "caster_log":
		key, _ := args["name"].(string)
		revs := f.items[key]
		log := []Revision{}
		for i := len(revs) - 1; i >= 0; i-- {
			log = append(log, Revision{Rev: i + 1, At: revs[i].at, Title: revs[i].title})
		}
		return out(map[string]any{"revisions": log})
	}
	return "", fmt.Errorf("the fake Caster has no tool %s", tool)
}
