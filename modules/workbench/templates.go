package workbench

import (
	"embed"
	"fmt"
	"io/fs"
	"slices"
	"strings"
)

// The starters an agent is created from. blank is the least that validates; embedded,
// task and stable follow libs agent/agentdir's examples for each kind, and embedded also
// carries a skill, a context file, a subtask variant, steering and an eval suite to show
// each part of the editor.
//
//go:embed all:templates
var templateFS embed.FS

// Template describes a starter.
type Template struct {
	Name  string `json:"name"`
	About string `json:"about"`
}

// Templates lists the starters.
func Templates() []Template {
	return []Template{
		{Name: "blank", About: "The least that validates: agent.md and host.yaml for an embedded agent."},
		{Name: "embedded", About: "An embedded agent with a skill, a context file, a reviewer variant, steering and an eval suite."},
		{Name: "task", About: "A task agent (kind task, lifetime until-task-end)."},
		{Name: "stable", About: "A stable agent on a seat (kind stable, lifetime on-demand)."},
	}
}

// fromTemplate returns a starter's files with the agent id filled in, and its eval suites.
func fromTemplate(name, id string) (Tree, map[string][]byte, error) {
	if !slices.ContainsFunc(Templates(), func(t Template) bool { return t.Name == name }) {
		return nil, nil, fmt.Errorf("workbench: no template %q", name)
	}
	base := "templates/" + name
	t := Tree{}
	suites := map[string][]byte{}
	err := fs.WalkDir(templateFS, base, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := templateFS.ReadFile(p)
		if err != nil {
			return err
		}
		b = []byte(strings.ReplaceAll(string(b), "{{id}}", id))
		rel := strings.TrimPrefix(p, base+"/")
		if strings.HasPrefix(rel, "workbench/evals/") {
			suites[strings.TrimPrefix(rel, "workbench/evals/")] = b
			return nil
		}
		t[rel] = b
		return nil
	})
	return t, suites, err
}
