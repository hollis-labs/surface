package viewer

import (
	"context"
	"strings"
	"testing"

	"github.com/hollis-labs/surface/module"
	"github.com/hollis-labs/surface/module/moduletest"
)

// Only what the Viewer shows is opened; anything else is refused before Files is asked.
func TestOpenOnlyWhatItShows(t *testing.T) {
	h := moduletest.Start(t, &mod{})
	if _, err := h.Tool("viewer_open", `{"place":"home","path":"notes/todo.txt"}`); err == nil || !strings.Contains(err.Error(), "todo.txt") {
		t.Fatalf("a text file was opened: %v", err)
	}
	for _, p := range []string{"a.png", "b.JPG", "c.mp4", "d.m4a", "e.pdf", "f.html", "g.svg"} {
		if !Reviewable.MatchString(p) {
			t.Errorf("%s is not reviewable", p)
		}
	}
}

// The marks on a file are the app's marks on its ref, as the agent reads them.
func TestMarksAreTheFilesRef(t *testing.T) {
	h := moduletest.Start(t, &mod{})
	ctx := context.Background()
	if _, err := h.MarkStore.Add(ctx, module.Mark{Ref: Ref("home", "shots/a.png"), Body: "this button is too small", Author: "owner", Anchor: []byte(`{"x":0.1,"y":0.2,"w":0.3,"h":0.1}`)}); err != nil {
		t.Fatal(err)
	}
	out, err := h.Tool("viewer_marks", `{"place":"home","path":"shots/a.png"}`)
	if err != nil {
		t.Fatal(err)
	}
	marks := out.([]module.Mark)
	if len(marks) != 1 || marks[0].Body != "this button is too small" {
		t.Fatalf("marks: %+v", marks)
	}
	if _, err := h.Tool("viewer_marks", `{"place":"home"}`); err == nil {
		t.Fatal("no path accepted")
	}
}
