package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/hollis-labs/surface/core/internal/kernel"
	"github.com/hollis-labs/surface/module"
)

func (s *server) paletteProviders(w http.ResponseWriter, _ *http.Request) {
	ok(w, s.a.PaletteProviders())
}
func (s *server) paletteSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	rows, err := s.a.SearchPalette(r.Context(), q.Get("provider"), module.PaletteQuery{Text: q.Get("q"), Scope: q.Get("scope")})
	switch {
	case errors.Is(err, kernel.ErrPaletteProvider):
		fail(w, http.StatusNotFound, err)
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		fail(w, http.StatusGatewayTimeout, err)
	case err != nil:
		fail(w, http.StatusBadRequest, err)
	default:
		ok(w, rows)
	}
}
