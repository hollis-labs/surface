package script

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/hollis-labs/surface/core/internal/kernel"
	"github.com/hollis-labs/surface/module"
)

const stepSchema = `{"type":"object","required":["say"],"properties":{
	"say":{"type":"string"},
	"point":{"type":"array","items":{"type":"object","properties":{"window":{"type":"string"},"view":{"type":"string"},"anchor":{"type":"string"},"label":{"type":"string"}}}},
	"style":{"type":"string","enum":["highlight","spotlight"]},
	"await":{"type":"object","properties":{"kind":{"type":"string","enum":["ack","open","focus"]},"view":{"type":"string"}}},
	"hint":{"type":"string"},
	"do":{"type":"array","items":{"type":"object","required":["tool"],"properties":{"tool":{"type":"string"},"input":{"type":"object"}}}},
	"expect":{"type":"object","properties":{"focused":{"type":"string"},"open":{"type":"array","items":{"type":"string"}},"closed":{"type":"array","items":{"type":"string"}},"mode":{"type":"string"},"layout":{"type":"string"},"tool":{"type":"object","properties":{"tool":{"type":"string"},"input":{"type":"object"}}},"contains":{"type":"string"}}},
	"hold_ms":{"type":"integer"},
	"zoom":{"type":"boolean"}}}`

// Tools are the scripts' tools, for every agent.
func (s *Service) Tools() []kernel.BoundTool {
	t := func(name string, lvl module.Level, desc, in string, run func(context.Context, json.RawMessage) (any, error)) kernel.BoundTool {
		return kernel.BoundTool{Tool: module.Tool{Name: name, Level: lvl, Description: desc, Input: json.RawMessage(in)}, Module: "script", Call: run}
	}
	named := func(raw json.RawMessage) (string, error) {
		var a struct{ Name string }
		if err := json.Unmarshal(raw, &a); err != nil || a.Name == "" {
			return "", errors.New("name the script")
		}
		return a.Name, nil
	}
	return []kernel.BoundTool{
		t("script_list", module.LevelRead, "List the scripts: walk-throughs that run as a guide for the owner, a recorded demo, or a test.",
			`{"type":"object","properties":{}}`,
			func(ctx context.Context, _ json.RawMessage) (any, error) { return s.List(ctx) }),
		t("script_get", module.LevelRead, "Read a script with its steps.",
			`{"type":"object","required":["name"],"properties":{"name":{"type":"string"}}}`,
			func(ctx context.Context, raw json.RawMessage) (any, error) {
				n, err := named(raw)
				if err != nil {
					return nil, err
				}
				return s.Get(ctx, n)
			}),
		t("script_save", module.LevelWrite, "Save a script (replacing one of the same name). Each step says one thing, may point at windows or anchors (as ade_point), may await the owner (ack, open or focus a view, as a guide), may do look-only tool calls first (desk_open, desk_focus, browser_open, ade_* and other read or act tools; never one that changes data), and may expect what the desk shows after (focused view, open and closed views, mode, layout, or a look-only tool whose answer contains a text). One script is an onboarding guide, a recorded demo and a test.",
			`{"type":"object","required":["name","steps"],"properties":{"name":{"type":"string"},"about":{"type":"string"},"steps":{"type":"array","items":`+stepSchema+`}}}`,
			func(ctx context.Context, raw json.RawMessage) (any, error) {
				var sc Script
				if err := json.Unmarshal(raw, &sc); err != nil {
					return nil, err
				}
				if err := s.Save(ctx, sc); err != nil {
					return nil, err
				}
				return "Saved the script " + sc.Name + ".", nil
			}),
		t("script_delete", module.LevelConfirm, "Delete a script.",
			`{"type":"object","required":["name"],"properties":{"name":{"type":"string"}}}`,
			func(ctx context.Context, raw json.RawMessage) (any, error) {
				n, err := named(raw)
				if err != nil {
					return nil, err
				}
				return "Deleted.", s.Delete(ctx, n)
			}),
		t("script_run", module.LevelAct, "Run a script. mode guide: on the owner's desk, moved on by them (onboarding). demo: on its own, step after step, pointing as it goes (for a recording); returns at once. test: without the page, checking each step's expectations, and putting the desk back after; returns the report.",
			`{"type":"object","required":["name","mode"],"properties":{"name":{"type":"string"},"mode":{"type":"string","enum":["guide","demo","test"]}}}`,
			func(ctx context.Context, raw json.RawMessage) (any, error) {
				var a struct{ Name, Mode string }
				if err := json.Unmarshal(raw, &a); err != nil {
					return nil, err
				}
				return s.Run(ctx, a.Name, a.Mode)
			}),
		t("script_stop", module.LevelAct, "Stop the demo that is running.",
			`{"type":"object","properties":{}}`,
			func(context.Context, json.RawMessage) (any, error) {
				if s.Stop() {
					return "Stopped.", nil
				}
				return "No demo is running.", nil
			}),
	}
}

// Run runs a script in a mode.
func (s *Service) Run(ctx context.Context, name, mode string) (any, error) {
	switch mode {
	case "guide":
		return s.Guide(ctx, name)
	case "demo":
		sc, err := s.Demo(ctx, name)
		if err != nil {
			return nil, err
		}
		return map[string]any{"script": sc.Name, "mode": "demo", "steps": len(sc.Steps), "status": "running"}, nil
	case "test":
		return s.Test(ctx, name)
	}
	return nil, errors.New(`mode is guide, demo or test`)
}

// Mount serves the scripts for the page and for a recorder:
//
//	GET  /api/scripts             every script
//	GET  /api/scripts/{name}      one
//	POST /api/scripts/{name}/run  {mode}: guide | demo | test
//	POST /api/scripts/stop        stop the demo
func (s *Service) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/scripts", func(w http.ResponseWriter, r *http.Request) {
		l, err := s.List(r.Context())
		reply(w, l, err)
	})
	mux.HandleFunc("GET /api/scripts/{name}", func(w http.ResponseWriter, r *http.Request) {
		sc, err := s.Get(r.Context(), r.PathValue("name"))
		reply(w, sc, err)
	})
	mux.HandleFunc("POST /api/scripts/{name}/run", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Mode string }
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in)
		out, err := s.Run(r.Context(), r.PathValue("name"), in.Mode)
		reply(w, out, err)
	})
	mux.HandleFunc("POST /api/scripts/stop", func(w http.ResponseWriter, _ *http.Request) {
		reply(w, map[string]bool{"stopped": s.Stop()}, nil)
	})
}

func reply(w http.ResponseWriter, v any, err error) {
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(v)
}
