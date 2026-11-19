package frag

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"

	"github.com/hollis-labs/surface/module"
)

// A saved fragment as a reference (kind "fragments.fragment"): read from FE's reader
// (GET /v1/reader/items/{id}) with the owner's key, the same address and key file as delivery.
// Reading needs no delivery: only frag.fe_url and frag.fe_token_file. Reading only.
//
// Ported from Basalisk (internal/api/frag_cite.go), in personal mode.

const fragmentKind = "fragments.fragment"

var fragmentID = regexp.MustCompile(`^[0-9a-f]{64}$`)

// readerMaxBytes bounds a reader item's answer.
const readerMaxBytes = 2 << 20

var errNoFragment = errors.New("no such fragment")

type readerItem struct {
	Schema   string `json:"schema_version"`
	ID       string `json:"fragment_id"`
	Revision int    `json:"revision"`
	Display  struct {
		Title struct {
			Value string `json:"value"`
		} `json:"title"`
		Summary struct {
			Value string `json:"value"`
		} `json:"summary"`
	} `json:"display"`
	Article struct {
		Preview string `json:"preview_markdown"`
	} `json:"article"`
}

func (m *Mod) refs() []module.RefKind {
	return []module.RefKind{{
		Kind: fragmentKind, Label: "Fragment", ID: fragmentID,
		Ready: func(_ context.Context, h module.Host) bool {
			c := h.Config()
			_, err := origin(c.String("fe_url"))
			return err == nil && strings.TrimSpace(c.String("fe_token_file")) != ""
		},
		Resolve: func(ctx context.Context, _ module.Host, ids []string) (map[string]module.RefEntity, error) {
			var (
				mu     sync.Mutex
				wg     sync.WaitGroup
				out    = map[string]module.RefEntity{}
				failed error
				done   int
				sem    = make(chan struct{}, 4)
			)
			for _, id := range ids {
				wg.Add(1)
				go func() {
					defer wg.Done()
					sem <- struct{}{}
					defer func() { <-sem }()
					e, err := m.fragment(ctx, id, false)
					mu.Lock()
					defer mu.Unlock()
					switch {
					case err == nil:
						out[id], done = e, done+1
					case errors.Is(err, errNoFragment):
						done++
					default:
						failed = err
					}
				}()
			}
			wg.Wait()
			if failed != nil && done == 0 {
				return nil, failed
			}
			return out, nil
		},
		Record: func(ctx context.Context, _ module.Host, id string) (module.RefEntity, error) {
			e, err := m.fragment(ctx, id, true)
			if errors.Is(err, errNoFragment) {
				return e, module.ErrRefNotFound
			}
			return e, err
		},
	}}
}

// fragment reads one fragment from FE's reader.
func (m *Mod) fragment(ctx context.Context, id string, full bool) (module.RefEntity, error) {
	if !fragmentID.MatchString(id) {
		return module.RefEntity{}, errNoFragment
	}
	cfg := m.h.Config()
	fe, err := origin(cfg.String("fe_url"))
	if err != nil {
		return module.RefEntity{}, err
	}
	bearer, err := readToken(cfg.String("fe_token_file"))
	if err != nil {
		return module.RefEntity{}, errors.New("the Fragments Engine key file cannot be read")
	}
	// The origin is the owner's own setting, checked by origin(); the id matched fragmentID.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fe+"/v1/reader/items/"+id, nil)
	if err != nil {
		return module.RefEntity{}, err
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Accept", "application/json")
	res, err := m.w.cl.http.Do(req) //nolint:gosec // see above: no request input chooses the host
	if err != nil {
		return module.RefEntity{}, errors.New("the Fragments Engine could not be reached")
	}
	defer func() { _ = res.Body.Close() }()
	switch {
	case res.StatusCode == http.StatusNotFound:
		return module.RefEntity{}, errNoFragment
	case res.StatusCode != http.StatusOK:
		return module.RefEntity{}, fmt.Errorf("the Fragments Engine answered %d", res.StatusCode) // never the body
	}
	var item readerItem
	if json.NewDecoder(io.LimitReader(res.Body, readerMaxBytes)).Decode(&item) != nil || item.Schema != "fe.reader.item.v1" || item.ID != id {
		return module.RefEntity{}, errors.New("the Fragments Engine answered with something other than a reader item")
	}
	title := strings.TrimSpace(item.Display.Title.Value)
	if title == "" {
		title = "Fragment " + id[:8]
	}
	e := module.RefEntity{
		Title: title, Subtitle: "Fragment", Description: strings.TrimSpace(item.Display.Summary.Value),
		ChipLabel: clipRunes(title, 48), URL: fe + "/sysop/reader/" + id,
		Fields: []module.RefField{{Label: "Fragment", Value: id[:12] + "…", Mono: true}, {Label: "Revision", Value: fmt.Sprint(item.Revision)}},
	}
	if full {
		e.Body = item.Article.Preview
	}
	return e, nil
}

func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
