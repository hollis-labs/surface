package workbench

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"

	nanite "github.com/hollis-labs/nanite/agent"
	"github.com/hollis-labs/nanite/evals"
)

// Stub is the YAML/JSON editing form of a Nanite eval stub. Schema is an object
// so YAML authors need not encode a JSON string inside their suite.
type Stub struct {
	Name        string         `yaml:"name" json:"name"`
	Description string         `yaml:"description" json:"description"`
	Schema      map[string]any `yaml:"schema,omitempty" json:"schema,omitempty"`
	ReadOnly    bool           `yaml:"read_only,omitempty" json:"read_only,omitempty"`
	Results     []StubResult   `yaml:"results" json:"results"`
}

type StubResult struct {
	When    map[string]any `yaml:"when,omitempty" json:"when,omitempty"`
	Text    string         `yaml:"text" json:"text"`
	IsError bool           `yaml:"is_error,omitempty" json:"is_error,omitempty"`
}

func (c Case) naniteStubs() []evals.Stub {
	var out []evals.Stub
	for _, s := range c.Stubs {
		schema := json.RawMessage(`{"type":"object"}`)
		if s.Schema != nil {
			schema, _ = json.Marshal(s.Schema)
		}
		stub := evals.Stub{Name: s.Name, Description: s.Description, Schema: schema, ReadOnly: s.ReadOnly}
		for _, r := range s.Results {
			stub.Results = append(stub.Results, evals.StubResult{When: r.When, Text: r.Text, IsError: r.IsError})
		}
		out = append(out, stub)
	}
	return out
}

var stubNameRe = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

func validateStubs(stubs []Stub) error {
	var tools []nanite.Tool
	for _, s := range (Case{Stubs: stubs}).naniteStubs() {
		if !stubNameRe.MatchString(s.Name) {
			return fmt.Errorf("stub %q: use 1-64 letters, digits, _ or -", s.Name)
		}
		if len(s.Results) == 0 {
			return fmt.Errorf("stub %q needs at least one result", s.Name)
		}
		for _, r := range s.Results {
			if r.When != nil && len(r.When) == 0 {
				return fmt.Errorf("stub %q: omit an empty when", s.Name)
			}
		}
		tools = append(tools, nanite.Tool{Name: s.Name, InputSchema: s.Schema, Handler: func(context.Context, nanite.ToolCall) (nanite.ToolResult, error) { return nanite.ToolResult{}, nil }})
	}
	var set nanite.ToolSet
	return set.SetGroup(evals.StubGroup, tools...)
}
