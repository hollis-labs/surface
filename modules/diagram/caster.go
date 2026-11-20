package diagram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/hollis-labs/surface/module"
	"github.com/hollis-labs/surface/module/collab"
	"github.com/hollis-labs/surface/module/tetherlink"
)

// Diagrams in Caster: caster://collab/diagram.<slug>, Basalisk's collab/v1 documents, so the
// same drawing is read and saved from either app. Its id here is its collab name
// ("diagram.<slug>"); a diagram kept in the module's own store has an id "dg_…". Both are
// listed together; the setting new_in says where a new one goes.

// backend is where diagrams are kept: the module's own store, or Caster.
type backend interface {
	list(ctx context.Context) ([]Diagram, error)
	get(ctx context.Context, id string) (Diagram, error)
	revisions(ctx context.Context, id string) ([]Revision, error)
	revision(ctx context.Context, id string, n int) (Revision, error)
	create(ctx context.Context, title, body string) (Diagram, error)
	save(ctx context.Context, id, body, title string, base int) (Diagram, error)
	rename(ctx context.Context, id, title string) (Diagram, error)
	remove(ctx context.Context, id string) error
}

// inCaster reports whether an id names a diagram in Caster.
func inCaster(id string) bool {
	return collab.ValidName(id) && strings.HasPrefix(id, collab.KindDiagram+".")
}

// casterStore keeps diagrams in Caster's collab namespace.
type casterStore struct{ docs *collab.Store }

func (c *casterStore) list(ctx context.Context) ([]Diagram, error) {
	docs, err := c.docs.List(ctx, collab.KindDiagram)
	if err != nil {
		return nil, err
	}
	out := make([]Diagram, 0, len(docs))
	for _, d := range docs {
		out = append(out, Diagram{ID: d.Name, Title: d.Title, Rev: d.Rev, Updated: d.Updated, Created: d.Created})
	}
	return out, nil
}

func casterErr(id string, err error) error {
	var conflict *collab.Conflict
	switch {
	case errors.Is(err, collab.ErrNotFound):
		return fmt.Errorf("%w: %s", errNotFound, id)
	case errors.As(err, &conflict):
		return fmt.Errorf("%w (it is at %d in Caster)", errConflict, conflict.Rev)
	}
	return err
}

func asDiagram(d collab.Doc) Diagram {
	return Diagram{ID: d.Name, Title: d.Title, Rev: d.Rev, Body: d.Body, Created: d.Created, Updated: d.Updated}
}

func (c *casterStore) get(ctx context.Context, id string) (Diagram, error) {
	d, err := c.docs.Get(ctx, id, 0)
	if err == nil && d.Deleted {
		err = collab.ErrNotFound
	}
	if err != nil {
		return Diagram{}, casterErr(id, err)
	}
	return asDiagram(d), nil
}

func (c *casterStore) revisions(ctx context.Context, id string) ([]Revision, error) {
	log, err := c.docs.Log(ctx, id)
	if err != nil {
		return nil, casterErr(id, err)
	}
	out := make([]Revision, 0, len(log))
	for _, r := range log {
		out = append(out, Revision{N: r.Rev, Title: r.Title, Created: r.At})
	}
	return out, nil
}

func (c *casterStore) revision(ctx context.Context, id string, n int) (Revision, error) {
	d, err := c.docs.Get(ctx, id, n)
	if err != nil {
		return Revision{}, casterErr(id, err)
	}
	return Revision{N: d.Rev, Title: d.Title, Body: d.Body, Created: d.Updated}, nil
}

func (c *casterStore) create(ctx context.Context, title, body string) (Diagram, error) {
	body, err := check(body)
	if err != nil {
		return Diagram{}, err
	}
	if strings.TrimSpace(title) == "" {
		title = "Untitled diagram"
	}
	d, err := c.docs.Create(ctx, collab.Doc{Kind: collab.KindDiagram, Title: title, Lang: "json", Body: body}, "owner")
	if err != nil {
		return Diagram{}, err
	}
	return asDiagram(d), nil
}

func (c *casterStore) save(ctx context.Context, id, body, title string, base int) (Diagram, error) {
	body, err := check(body)
	if err != nil {
		return Diagram{}, err
	}
	var t *string
	if strings.TrimSpace(title) != "" {
		t = &title
	}
	d, err := c.docs.Save(ctx, id, base, t, body, "owner")
	if err != nil {
		return Diagram{}, casterErr(id, err)
	}
	return asDiagram(d), nil
}

// rename is a save with the new title (Caster keeps no title apart from a revision).
func (c *casterStore) rename(ctx context.Context, id, title string) (Diagram, error) {
	if strings.TrimSpace(title) == "" {
		return Diagram{}, errors.New("a title is needed")
	}
	cur, err := c.get(ctx, id)
	if err != nil {
		return Diagram{}, err
	}
	return c.save(ctx, id, cur.Body, title, cur.Rev)
}

// remove hides the diagram, as Basalisk does: its revisions stay in Caster.
func (c *casterStore) remove(ctx context.Context, id string) error {
	cur, err := c.get(ctx, id)
	if err != nil {
		return err
	}
	_, err = c.docs.SetDeleted(ctx, id, cur.Rev, true)
	return casterErr(id, err)
}

// router sends each id to where it is kept and lists both.
type router struct {
	m     *mod
	local *store
	cast  *casterStore
}

func (r *router) of(id string) backend {
	if inCaster(id) {
		return r.cast
	}
	return r.local
}

// list is the module's own diagrams and, when Tether is set, Caster's, most recent first. A
// Caster that cannot be reached leaves its diagrams out rather than failing the list.
func (r *router) list(ctx context.Context) ([]Diagram, error) {
	out, err := r.local.list(ctx)
	if err != nil {
		return nil, err
	}
	if !r.m.casterReady() {
		return out, nil
	}
	remote, err := r.cast.list(ctx)
	if err != nil {
		slog.Warn("diagram: Caster's diagrams could not be listed", "err", err)
		return out, nil
	}
	all := append(remote, out...)
	sortByUpdated(all)
	return all, nil
}

func (r *router) get(ctx context.Context, id string) (Diagram, error) { return r.of(id).get(ctx, id) }
func (r *router) revisions(ctx context.Context, id string) ([]Revision, error) {
	return r.of(id).revisions(ctx, id)
}
func (r *router) revision(ctx context.Context, id string, n int) (Revision, error) {
	return r.of(id).revision(ctx, id, n)
}

func (r *router) create(ctx context.Context, title, body string) (Diagram, error) {
	if r.m.newInCaster() {
		return r.cast.create(ctx, title, body)
	}
	return r.local.create(ctx, title, body)
}

func (r *router) save(ctx context.Context, id, body, title string, base int) (Diagram, error) {
	return r.of(id).save(ctx, id, body, title, base)
}
func (r *router) rename(ctx context.Context, id, title string) (Diagram, error) {
	return r.of(id).rename(ctx, id, title)
}
func (r *router) remove(ctx context.Context, id string) error { return r.of(id).remove(ctx, id) }

// tether is the configured link to Tether, read from the settings at each call.
func (m *mod) tether() (tetherlink.Caller, error) {
	if m.link != nil {
		return m.link, nil
	}
	cfg := m.h.Config()
	c := tetherlink.New("diagram", tetherlink.Config{URL: cfg.String("tether_url"), TokenFile: cfg.String("tether_token_file"), Command: cfg.String("tether_command")}, collab.Tools)
	if c == nil {
		return nil, errors.New("diagrams in Caster need Tether: set diagram.tether_command (or diagram.tether_url) in Settings")
	}
	return c, nil
}

func (m *mod) casterReady() bool {
	if m.link != nil {
		return true
	}
	c := m.h.Config()
	return strings.TrimSpace(c.String("tether_command")) != "" || strings.TrimSpace(c.String("tether_url")) != ""
}

func (m *mod) newInCaster() bool { return m.h.Config().String("new_in") == "caster" }

// casterSettings are what reaching Caster takes, and where a new diagram goes.
var casterSettings = []module.Setting{
	{Key: "new_in", Title: "New diagrams are kept in", About: "Caster (caster://collab/diagram.<name>, shared with Basalisk) or the app's own store. Both are listed either way.", Type: module.SettingEnum, Default: "local",
		Options: []string{"local", "caster"}},
	{Key: "tether_command", Title: "Tether command", About: "A command that speaks MCP over stdio to Tether, through which Caster is reached.", Type: module.SettingString, Default: ""},
	{Key: "tether_url", Title: "Tether MCP URL", About: "Instead of a command: Tether's MCP endpoint over HTTP.", Type: module.SettingString, Default: ""},
	{Key: "tether_token_file", Title: "Tether token file", About: "The file holding the token for the MCP URL.", Type: module.SettingString, Default: ""},
}

func sortByUpdated(d []Diagram) {
	sort.SliceStable(d, func(i, j int) bool { return d[i].Updated > d[j].Updated })
}
