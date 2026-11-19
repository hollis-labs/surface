package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/hollis-labs/surface/core/internal/kernel"
	"github.com/hollis-labs/surface/module"
)

// Cite: the page found references in some text and asks what they stand for. Each kind is
// resolved by the module that owns it (module.RefKind); the kernel batches and caches.
//
//	GET /api/cite                          {kinds: [{kind, label, module, record}]}
//	GET /api/cite/resolve?kind=K&ids=a,b   {results: {id: entity | null}}
//	GET /api/cite/record?kind=K&id=a       the entity with its body

func (s *server) citeKinds(w http.ResponseWriter, r *http.Request) {
	ok(w, map[string]any{"kinds": s.a.RefKinds(r.Context())})
}

func (s *server) citeResolve(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var ids []string
	for _, id := range strings.Split(q.Get("ids"), ",") {
		if id = strings.TrimSpace(id); id != "" {
			ids = append(ids, id)
		}
	}
	out, err := s.a.ResolveRefs(r.Context(), q.Get("kind"), ids)
	switch {
	case errors.Is(err, kernel.ErrUnknownRefKind):
		fail(w, http.StatusNotFound, err)
	case err != nil && (len(ids) == 0 || len(ids) > kernel.MaxCiteBatch):
		fail(w, http.StatusBadRequest, err)
	case err != nil:
		fail(w, http.StatusBadGateway, errors.New("the references could not be looked up: "+err.Error()))
	default:
		ok(w, map[string]any{"results": out})
	}
}

func (s *server) citeRecord(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	e, err := s.a.RecordRef(r.Context(), q.Get("kind"), q.Get("id"))
	switch {
	case errors.Is(err, kernel.ErrUnknownRefKind):
		fail(w, http.StatusNotFound, err)
	case errors.Is(err, module.ErrRefNotFound):
		fail(w, http.StatusNotFound, errors.New("that record was not found: it may have been deleted, or it may not be readable"))
	case err != nil:
		fail(w, http.StatusBadGateway, errors.New("the record could not be read: "+err.Error()))
	default:
		ok(w, e)
	}
}
