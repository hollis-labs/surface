package ade

import (
	"encoding/json"
	"net/http"
)

// Mount serves what the page needs:
//
//	GET    /api/ade                    the attention, the guide and where it stands
//	PUT    /api/ade/anchors/{window}   [{name, label}]: what the window's view declared ([] forgets it)
//	POST   /api/ade/guide              a Report: where the guide stands
//	DELETE /api/ade/attention          the owner dismissed what the agent pointed at
func (s *Service) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/ade", func(w http.ResponseWriter, _ *http.Request) { reply(w, 200, s.Snapshot()) })
	mux.HandleFunc("PUT /api/ade/anchors/{window}", func(w http.ResponseWriter, r *http.Request) {
		var list []Anchor
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&list); err != nil {
			reply(w, 400, map[string]string{"code": "invalid_request", "message": "The body is a list of {name, label}."})
			return
		}
		s.SetAnchors(r.PathValue("window"), list)
		w.WriteHeader(204)
	})
	mux.HandleFunc("POST /api/ade/guide", func(w http.ResponseWriter, r *http.Request) {
		var rep Report
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&rep); err != nil {
			reply(w, 400, map[string]string{"code": "invalid_request", "message": "The body is a report."})
			return
		}
		s.SetReport(rep)
		w.WriteHeader(204)
	})
	mux.HandleFunc("DELETE /api/ade/attention", func(w http.ResponseWriter, _ *http.Request) {
		s.ClearAttention()
		w.WriteHeader(204)
	})
}

func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
