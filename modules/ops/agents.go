package ops

import (
	"context"
	"net/http"
)

func (m *mod) apiAgents(w http.ResponseWriter, r *http.Request) {
	result, err := m.sessions(r.Context())
	if err != nil {
		failErr(w, err)
		return
	}
	reply(w, http.StatusOK, result)
}

// Session observations refresh only while a consuming board tab is open. Both
// views and the agent tool share the same bounded cache.
func (m *mod) sessionViewOpen(ctx context.Context) bool {
	for _, w := range m.h.Desk().Windows(ctx) {
		if w.Module == "ops" && (w.Args["layout"] == "agents" || w.Args["layout"] == "fleet") {
			return true
		}
	}
	return false
}
