// Package collabtest is an in-memory Caster for tests of code that reads and writes Caster
// through module/tetherlink (module/collab, a module's own Caster sync). Tests only.
package collabtest

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
// items by "<ns>/<name>", each a list of revisions, answered in the gateway's shapes and with
// its rules: a put of the text an item already has keeps its revision, and a put with no title
// or tags keeps the last ones. It is a tetherlink.Caller; Link hands it to a collab.Store.
type Fake struct {
	mu    sync.Mutex
	items map[string][]fakeRev
	// Calls are the tools called, in order.
	Calls []string
	// Down makes every call fail, as an unreachable gateway does.
	Down bool
	// BeforePut, when set, runs before each caster_put with the item's key: a test writes there
	// as someone else would between a read and a write.
	BeforePut func(key string)
	// Fail, when set, fails a call it returns true for (a tool, its arguments).
	Fail func(tool string, args map[string]any) bool
}

type fakeRev struct {
	content, title, tags, source, at string
}

// NewFake is an empty Caster.
func NewFake() *Fake { return &Fake{items: map[string][]fakeRev{}} }

// Link is the Fake as a Store's Link.
func (f *Fake) Link() (tetherlink.Caller, error) { return f, nil }

// Tags are the latest tags of "<ns>/<name>".
func (f *Fake) Tags(key string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	revs := f.items[key]
	if len(revs) == 0 {
		return ""
	}
	return revs[len(revs)-1].tags
}

// Revs is how many revisions "<ns>/<name>" has.
func (f *Fake) Revs(key string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.items[key])
}

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

// Put writes a revision directly, as another app would, by Caster's rules (putLocked).
func (f *Fake) Put(key, content, title, tags string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.putLocked(key, content, title, tags, "test")
}

func (f *Fake) putLocked(key, content, title, tags, source string) int {
	revs := f.items[key]
	if n := len(revs); n > 0 {
		last := revs[n-1]
		if last.content == content {
			return n // the same text: no new revision
		}
		if title == "" {
			title = last.title
		}
		if tags == "" {
			tags = last.tags
		}
	}
	f.items[key] = append(revs, fakeRev{content: content, title: title, tags: tags, source: source, at: time.Now().UTC().Format(time.RFC3339Nano)})
	return len(f.items[key])
}

func (f *Fake) Call(_ context.Context, tool string, args map[string]any) (string, error) {
	if tool == "caster_put" && f.BeforePut != nil {
		key, _ := args["name"].(string)
		f.BeforePut(key)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, tool)
	if f.Down || (f.Fail != nil && f.Fail(tool, args)) {
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
		tags, _ := args["tags"].(string)
		source, _ := args["source"].(string)
		rev := f.putLocked(key, content, title, tags, source)
		return out(map[string]any{"ref": fmt.Sprintf("caster://%s@%d", key, rev)})
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
		content, cut := r.content, false
		if mb, ok := args["max_bytes"].(int); ok && mb > 0 && len(content) > mb {
			content, cut = content[:mb], true
		}
		return out(map[string]any{"state": "resolved", "content": content, "title": r.title, "revision": fmt.Sprint(n), "truncated": cut})
	case "caster_ls":
		ns, _ := args["ns"].(string)
		items := []map[string]any{}
		for key, revs := range f.items {
			if !strings.HasPrefix(key, ns+"/") {
				continue
			}
			last := revs[len(revs)-1]
			items = append(items, map[string]any{"ref": "caster://" + key, "rev": len(revs), "title": last.title, "tags": last.tags, "updated": last.at})
		}
		sort.Slice(items, func(i, j int) bool { return items[i]["ref"].(string) < items[j]["ref"].(string) })
		return out(map[string]any{"items": items})
	case "caster_log":
		key, _ := args["name"].(string)
		revs := f.items[key]
		log := []map[string]any{}
		for i := len(revs) - 1; i >= 0; i-- {
			log = append(log, map[string]any{"rev": i + 1, "at": revs[i].at, "title": revs[i].title})
		}
		return out(map[string]any{"revisions": log})
	}
	return "", fmt.Errorf("the fake Caster has no tool %s", tool)
}
