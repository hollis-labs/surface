package agent

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/hollis-labs/surface/core/internal/agent/profile"
)

// ParseBindings checks the persisted module-to-folder setting without coercing paths.
func ParseBindings(value any) (map[string]string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var bindings map[string]string
	if err := json.Unmarshal(raw, &bindings); err != nil || bindings == nil {
		return nil, fmt.Errorf("bindings must be an object mapping module IDs to folder paths")
	}
	for id, dir := range bindings {
		if id == "" || strings.TrimSpace(dir) == "" {
			return nil, fmt.Errorf("binding %q needs a folder path; remove the entry to reset", id)
		}
	}
	return bindings, nil
}

func (s *Service) binding(id string) string {
	if s.bindings == nil {
		return ""
	}
	return s.bindings()[id]
}

func (s *Service) hasProfile(id string) bool {
	if id == "" {
		return false
	}
	return s.binding(id) != "" || s.prof.all[id] != nil
}

// BindingInfo includes modules without built-in profiles so any module can be bound.
type BindingInfo struct {
	ID             string   `json:"id"`
	Title          string   `json:"title"`
	Dir            string   `json:"dir,omitempty"`
	Name           string   `json:"name,omitempty"`
	Error          string   `json:"error,omitempty"`
	IgnoredRuntime []string `json:"ignored_runtime,omitempty"`
}

func (s *Service) bindingState() []BindingInfo {
	out := []BindingInfo{}
	for id, title := range s.modules {
		b := BindingInfo{ID: id, Title: title, Dir: s.binding(id)}
		if b.Dir != "" {
			p, err := profile.LoadBinding(id, title, b.Dir)
			if err != nil {
				b.Error = err.Error()
			} else {
				b.Name = p.Name
				b.IgnoredRuntime = p.IgnoredRuntime
			}
		}
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
