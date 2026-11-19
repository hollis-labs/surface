// Package viewer is the Viewer module: look at an artifact (a picture, a video, a recording, a
// PDF, an HTML mock) and point at what you mean. Drag a box on it, or click for a pin, and say
// what about it; a mark on a video or a recording remembers the moment. The bytes come from the
// Files module (its places and its checks); the marks are the app's own (Host.Marks), on the
// ref viewer:<place>:<path>, so the agent reads them with the rest.
//
// Ported from Basalisk's Viewer (frontend/src/apps/viewer), which kept the marks in Caster.
package viewer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"regexp"
	"strings"

	"github.com/hollis-labs/surface/module"
)

func init() { module.Register(&mod{}) }

type mod struct{ module.Base }

const viewID = "viewer"

// Reviewable are the files the Viewer shows.
var Reviewable = regexp.MustCompile(`(?i)\.(png|jpe?g|gif|webp|avif|svg|mp4|webm|mov|m4v|mp3|wav|m4a|ogg|pdf|html?)$`)

// Ref is the marks ref of a file.
func Ref(place, rel string) string { return "viewer:" + place + ":" + rel }

func (m *mod) Manifest() module.Manifest {
	return module.Manifest{
		ID: "viewer", Title: "Viewer", Version: "0.1.0",
		About: "Look at a picture, a video, a recording, a PDF or an HTML mock, and mark what you mean on it.",
		Views: []module.View{{
			ID: viewID, Title: "Viewer", Icon: "scan-search", Width: 1240, Height: 800,
			Menu: true, Multi: true, Bare: true, DefaultKey: "viewer",
			Render: module.Component("viewer/main"),
			Describe: func(_ context.Context, _ module.Host, w module.Window) (module.WindowContext, error) {
				place, _ := w.Args["place"].(string)
				rel, _ := w.Args["path"].(string)
				if place == "" {
					return module.WindowContext{Kind: "data", Ref: "viewer"}, nil
				}
				return module.WindowContext{Kind: "data", Ref: Ref(place, rel), Extra: map[string]any{"place": place, "path": rel}}, nil
			},
		}},
		Commands: []module.Command{
			{ID: "viewer.open", Title: "Viewer: look at a file", Section: "Viewer", Keywords: "annotate image video mock pdf artifact point mark",
				Run: func(ctx context.Context, h module.Host, args map[string]any) (any, error) {
					place, _ := args["place"].(string)
					rel, _ := args["path"].(string)
					return m.open(ctx, h, place, rel)
				}},
		},
		Tools: []module.Tool{
			{Name: "viewer_open", Level: module.LevelAct,
				Description: "Open a file in the Viewer: a picture, video, recording, PDF or HTML page in one of the Files places (place id and path within it, as files tools name them).",
				Input:       json.RawMessage(`{"type":"object","required":["place","path"],"properties":{"place":{"type":"string"},"path":{"type":"string"}}}`),
				Run: func(ctx context.Context, h module.Host, in json.RawMessage) (any, error) {
					var a struct{ Place, Path string }
					_ = json.Unmarshal(in, &a)
					return m.open(ctx, h, a.Place, a.Path)
				}},
			{Name: "viewer_marks", Level: module.LevelRead,
				Description: "The marks the owner left on a file in the Viewer: each with what they said and where (a box as fractions of the picture: x, y, w, h; t: the moment in seconds, for a video or recording).",
				Input:       json.RawMessage(`{"type":"object","required":["place","path"],"properties":{"place":{"type":"string"},"path":{"type":"string"}}}`),
				Run: func(ctx context.Context, h module.Host, in json.RawMessage) (any, error) {
					var a struct{ Place, Path string }
					_ = json.Unmarshal(in, &a)
					if a.Place == "" || a.Path == "" {
						return nil, errors.New("give the place and the path")
					}
					return h.Marks().List(ctx, Ref(a.Place, a.Path))
				}},
		},
	}
}

// open opens the Viewer on a file, after Files has checked it (an unknown place, a path outside
// it or a sensitive file is refused there), one window per file.
func (m *mod) open(ctx context.Context, h module.Host, place, rel string) (module.Window, error) {
	if place == "" {
		return h.Desk().Open(ctx, viewID, module.OpenOptions{Key: "viewer"})
	}
	rel = strings.TrimPrefix(path.Clean("/"+rel), "/")
	if !Reviewable.MatchString(rel) {
		return module.Window{}, fmt.Errorf("the Viewer shows pictures, video, sound, PDF and HTML; not %s", path.Base(rel))
	}
	out, err := h.RunCommand(ctx, "files.path", map[string]any{"place": place, "path": rel})
	if err != nil {
		return module.Window{}, err
	}
	if p, ok := out.(map[string]string); ok {
		if fi, statErr := os.Stat(p["path"]); statErr != nil || !fi.Mode().IsRegular() {
			return module.Window{}, fmt.Errorf("there is no file %s in %s", rel, place)
		}
	}
	return h.Desk().Open(ctx, viewID, module.OpenOptions{
		Key: Ref(place, rel), Title: "Viewer: " + path.Base(rel),
		Args: map[string]any{"place": place, "path": rel},
	})
}
