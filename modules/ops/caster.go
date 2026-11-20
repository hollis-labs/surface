package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/hollis-labs/surface/module/tetherlink"
)

// Caster is where the workstreams live: namespace "ops", one document per workstream
// (ws.<id>) and the queue (queue). The store keeps a working copy and writes through this.
// A port of Work's GatewayCaster (modules/work/caster.go), not an import: the seam forbids it.
type Caster interface {
	// List is every document in the namespace, latest revision each.
	List(ctx context.Context) ([]Head, error)
	// Get is a document's text at a revision (0 = latest). ok is false when there is none.
	Get(ctx context.Context, name string, rev int) (it Item, ok bool, err error)
	// Put makes a new revision (none when the content is the same) and returns it.
	Put(ctx context.Context, name, content, title, mime string) (rev int, err error)
	// Log is a document's revisions, newest first.
	Log(ctx context.Context, name string) ([]Revision, error)
}

// Namespace is Ops' namespace in Caster.
const Namespace = "ops"

// Head is a listed document: its name in the namespace and latest revision.
type Head struct {
	Name    string
	Rev     int
	Title   string
	Updated string
}

// Item is a document's text at a revision.
type Item struct {
	Name    string
	Rev     int
	Title   string
	Content string
}

// Revision is one entry of a document's history.
type Revision struct {
	Rev   int    `json:"rev"`
	At    string `json:"at"`
	Title string `json:"title"`
}

// GatewayCaster reaches Caster through the Tether gateway. Link is asked for the client at
// each call, so the module's settings are read at the moment of use.
type GatewayCaster struct {
	Link func() (tetherlink.Caller, error)
}

const maxDoc = 1 << 20

func (g GatewayCaster) call(ctx context.Context, tool string, args map[string]any, all bool) ([]byte, error) {
	c, err := g.Link()
	if err != nil {
		return nil, err
	}
	var out string
	if all {
		out, err = tetherlink.CallAll(ctx, c, tool, args)
	} else {
		out, err = c.Call(ctx, tool, args)
	}
	return []byte(out), err
}

func (g GatewayCaster) List(ctx context.Context) ([]Head, error) {
	raw, err := g.call(ctx, "caster_ls", map[string]any{"ns": Namespace}, true)
	if err != nil {
		return nil, err
	}
	var out struct {
		Items []struct {
			Ref     string `json:"ref"`
			Rev     int    `json:"rev"`
			Title   string `json:"title"`
			Updated string `json:"updated"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("caster_ls: %w", err)
	}
	heads := make([]Head, 0, len(out.Items))
	for _, it := range out.Items {
		name, ok := strings.CutPrefix(it.Ref, "caster://"+Namespace+"/")
		if !ok || name == "" {
			continue
		}
		heads = append(heads, Head{Name: name, Rev: it.Rev, Title: it.Title, Updated: it.Updated})
	}
	return heads, nil
}

func (g GatewayCaster) Get(ctx context.Context, name string, rev int) (Item, bool, error) {
	ref := DocRef(name)
	if rev > 0 {
		ref += "@" + strconv.Itoa(rev)
	}
	raw, err := g.call(ctx, "caster_get", map[string]any{"ref": ref, "max_bytes": maxDoc}, false)
	if err != nil {
		return Item{}, false, err
	}
	var out struct {
		Content   string `json:"content"`
		State     string `json:"state"`
		Revision  string `json:"revision"`
		Title     string `json:"title"`
		Truncated bool   `json:"truncated"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return Item{}, false, fmt.Errorf("caster_get: %w", err)
	}
	if out.State == "not_found" {
		return Item{}, false, nil
	}
	if out.State != "resolved" {
		return Item{}, false, fmt.Errorf("caster_get %s: %s", ref, out.State)
	}
	if out.Truncated {
		return Item{}, false, fmt.Errorf("%s is larger than %d KB", ref, maxDoc>>10)
	}
	r, _ := strconv.Atoi(out.Revision)
	return Item{Name: name, Rev: r, Title: out.Title, Content: out.Content}, true, nil
}

func (g GatewayCaster) Put(ctx context.Context, name, content, title, mime string) (int, error) {
	raw, err := g.call(ctx, "caster_put", map[string]any{
		"name": Namespace + "/" + name, "content": content, "mime": mime,
		"title": title, "source": "surface:ops",
	}, false)
	if err != nil {
		return 0, err
	}
	var out struct {
		Ref string `json:"ref"`
	}
	if err = json.Unmarshal(raw, &out); err != nil {
		return 0, fmt.Errorf("caster_put: %w", err)
	}
	_, at, ok := strings.Cut(out.Ref, "@")
	rev, err := strconv.Atoi(at)
	if !ok || err != nil {
		return 0, errors.New("caster_put did not say the new revision")
	}
	return rev, nil
}

func (g GatewayCaster) Log(ctx context.Context, name string) ([]Revision, error) {
	raw, err := g.call(ctx, "caster_log", map[string]any{"name": Namespace + "/" + name}, true)
	if err != nil {
		return nil, err
	}
	var out struct {
		Revisions []Revision `json:"revisions"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("caster_log: %w", err)
	}
	return out.Revisions, nil
}

// DocRef is a document's Caster ref.
func DocRef(name string) string { return "caster://" + Namespace + "/" + name }
