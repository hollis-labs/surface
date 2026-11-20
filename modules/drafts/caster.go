package drafts

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"strings"

	"github.com/hollis-labs/surface/module"
	"github.com/hollis-labs/surface/module/collab"
	"github.com/hollis-labs/surface/module/tetherlink"
)

// Drafts in Caster: caster://collab/draft.<slug>, Basalisk's collab/v1 documents, so a draft is
// read and written from either app, every save a Caster revision. Its id here is its collab
// name ("draft.<slug>"); a draft kept in the module's own store has an id "d_…". Both are
// listed together; the setting new_in says where a new one goes.

type backend interface {
	create(ctx context.Context, title, lang, body, author string) (Draft, error)
	list(ctx context.Context, q string) ([]Draft, error)
	get(ctx context.Context, id string) (Draft, error)
	revision(ctx context.Context, id string, rev int) (Revision, error)
	revisions(ctx context.Context, id string) ([]Revision, error)
	write(ctx context.Context, id, body, author, summary string, expect int, title *string) (Draft, error)
	setLang(ctx context.Context, id, lang string) error
	remove(ctx context.Context, id string) error
}

func inCaster(id string) bool {
	return collab.ValidName(id) && strings.HasPrefix(id, collab.KindDraft+".")
}

type casterStore struct{ docs *collab.Store }

func casterErr(err error) error {
	switch {
	case errors.Is(err, collab.ErrNotFound):
		return errNotFound
	case errors.As(err, new(*collab.Conflict)):
		return errStale
	}
	return err
}

func asDraft(d collab.Doc) Draft {
	lang := d.Lang
	if lang == "" {
		lang = "markdown"
	}
	return Draft{ID: d.Name, Title: d.Title, Lang: lang, Body: d.Body, Rev: d.Rev, Created: d.Created, Updated: d.Updated, Lines: lines(d.Body)}
}

func (c *casterStore) create(ctx context.Context, title, lang, body, author string) (Draft, error) {
	if lang == "" {
		lang = "markdown"
	}
	d, err := c.docs.Create(ctx, collab.Doc{Kind: collab.KindDraft, Title: title, Lang: lang, Body: body}, author)
	if err != nil {
		return Draft{}, err
	}
	return asDraft(d), nil
}

// list is Caster's drafts; q keeps those whose title holds it (the bodies are not read for a list).
func (c *casterStore) list(ctx context.Context, q string) ([]Draft, error) {
	docs, err := c.docs.List(ctx, collab.KindDraft)
	if err != nil {
		return nil, err
	}
	out := []Draft{}
	for _, d := range docs {
		if q != "" && !strings.Contains(strings.ToLower(d.Title), strings.ToLower(q)) {
			continue
		}
		out = append(out, Draft{ID: d.Name, Title: d.Title, Lang: "markdown", Rev: d.Rev, Created: d.Created, Updated: d.Updated})
	}
	return out, nil
}

func (c *casterStore) get(ctx context.Context, id string) (Draft, error) {
	d, err := c.docs.Get(ctx, id, 0)
	if err == nil && d.Deleted {
		err = collab.ErrNotFound
	}
	if err != nil {
		return Draft{}, casterErr(err)
	}
	return asDraft(d), nil
}

func (c *casterStore) revision(ctx context.Context, id string, rev int) (Revision, error) {
	d, err := c.docs.Get(ctx, id, rev)
	if err != nil {
		return Revision{}, casterErr(err)
	}
	return Revision{Rev: d.Rev, Body: d.Body, Author: "caster", Created: d.Updated}, nil
}

func (c *casterStore) revisions(ctx context.Context, id string) ([]Revision, error) {
	log, err := c.docs.Log(ctx, id)
	if err != nil {
		return nil, casterErr(err)
	}
	out := make([]Revision, 0, len(log))
	for _, r := range log {
		out = append(out, Revision{Rev: r.Rev, Author: "caster", Created: r.At})
	}
	return out, nil
}

// write saves a new revision in Caster; one that changes nothing writes nothing.
func (c *casterStore) write(ctx context.Context, id, body, author, _ string, expect int, title *string) (Draft, error) {
	cur, err := c.get(ctx, id)
	if err != nil {
		return Draft{}, err
	}
	if expect != 0 && expect != cur.Rev {
		return Draft{}, errStale
	}
	newTitle := cur.Title
	if title != nil && strings.TrimSpace(*title) != "" {
		newTitle = strings.TrimSpace(*title)
	}
	if body == cur.Body && newTitle == cur.Title {
		return cur, nil
	}
	d, err := c.docs.Save(ctx, id, cur.Rev, &newTitle, body, author)
	if err != nil {
		return Draft{}, casterErr(err)
	}
	return asDraft(d), nil
}

func (c *casterStore) setLang(ctx context.Context, id, lang string) error {
	_, err := c.docs.Update(ctx, id, 0, func(d *collab.Doc) { d.Lang = lang }, "owner")
	return casterErr(err)
}

// remove hides the draft, as Basalisk does: its revisions stay in Caster.
func (c *casterStore) remove(ctx context.Context, id string) error {
	cur, err := c.get(ctx, id)
	if err != nil {
		return err
	}
	_, err = c.docs.SetDeleted(ctx, id, cur.Rev, true)
	return casterErr(err)
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

func (r *router) create(ctx context.Context, title, lang, body, author string) (Draft, error) {
	if r.m.h.Config().String("new_in") == "caster" {
		return r.cast.create(ctx, title, lang, body, author)
	}
	return r.local.create(ctx, title, lang, body, author)
}

// list is the module's own drafts and, when Tether is set, Caster's, most recent first. A
// Caster that cannot be reached leaves its drafts out rather than failing the list.
func (r *router) list(ctx context.Context, q string) ([]Draft, error) {
	out, err := r.local.list(ctx, q)
	if err != nil || !r.m.casterReady() {
		return out, err
	}
	remote, err := r.cast.list(ctx, q)
	if err != nil {
		slog.Warn("drafts: Caster's drafts could not be listed", "err", err)
		return out, nil
	}
	all := append(remote, out...)
	sort.SliceStable(all, func(i, j int) bool { return all[i].Updated > all[j].Updated })
	return all, nil
}

func (r *router) get(ctx context.Context, id string) (Draft, error) { return r.of(id).get(ctx, id) }
func (r *router) revision(ctx context.Context, id string, rev int) (Revision, error) {
	return r.of(id).revision(ctx, id, rev)
}
func (r *router) revisions(ctx context.Context, id string) ([]Revision, error) {
	return r.of(id).revisions(ctx, id)
}
func (r *router) write(ctx context.Context, id, body, author, summary string, expect int, title *string) (Draft, error) {
	return r.of(id).write(ctx, id, body, author, summary, expect, title)
}
func (r *router) setLang(ctx context.Context, id, lang string) error {
	return r.of(id).setLang(ctx, id, lang)
}
func (r *router) remove(ctx context.Context, id string) error { return r.of(id).remove(ctx, id) }

// tether is the configured link to Tether, read from the settings at each call.
func (m *mod) tether() (tetherlink.Caller, error) {
	if m.link != nil {
		return m.link, nil
	}
	cfg := m.h.Config()
	c := tetherlink.New("drafts", tetherlink.Config{URL: cfg.String("tether_url"), TokenFile: cfg.String("tether_token_file"), Command: cfg.String("tether_command")}, collab.Tools)
	if c == nil {
		return nil, errors.New("drafts in Caster need Tether: set drafts.tether_command (or drafts.tether_url) in Settings")
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

var casterSettings = []module.Setting{
	{Key: "new_in", Title: "New drafts are kept in", About: "Caster (caster://collab/draft.<name>, shared with Basalisk) or the app's own store. Both are listed either way.", Type: module.SettingEnum, Default: "local", Options: []string{"local", "caster"}},
	{Key: "tether_command", Title: "Tether command", About: "A command that speaks MCP over stdio to Tether, through which Caster is reached.", Type: module.SettingString, Default: ""},
	{Key: "tether_url", Title: "Tether MCP URL", About: "Instead of a command: Tether's MCP endpoint over HTTP.", Type: module.SettingString, Default: ""},
	{Key: "tether_token_file", Title: "Tether token file", About: "The file holding the token for the MCP URL.", Type: module.SettingString, Default: ""},
}
