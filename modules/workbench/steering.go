package workbench

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/hollis-labs/libs/agent/steer"
	nanite "github.com/hollis-labs/nanite/agent"
)

// Steering is Workbench's sidecar configuration, applied through Nanite's
// agent.Options.Steering in eval runs.
type Steering struct {
	// Format is the sidecar format's version.
	Format   int            `json:"workbench_format"`
	Presets  []steer.Preset `json:"presets,omitempty"`
	Reflexes []steer.Reflex `json:"reflexes,omitempty"`
}

// vocabulary is the action kinds a Surface host carries out: steer's remind and halt, and
// Surface's run_command and call_tool (core/internal/reflex).
func vocabulary() *steer.Vocabulary {
	v := steer.NewVocabulary()
	_ = v.Register("run_command", requireField("command"))
	_ = v.Register("call_tool", requireField("tool"))
	return v
}

// parseSteering decodes and checks a steering sidecar strictly. Empty input is no steering.
func parseSteering(raw []byte) (Steering, error) {
	var s Steering
	if len(bytes.TrimSpace(raw)) == 0 {
		return s, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return s, fmt.Errorf("steering.json: %w", err)
	}
	return s, s.Validate()
}

// Validate checks every reflex and preset with steer's own validation, that names are not
// repeated, and that presets name reflexes the file defines.
func (s Steering) Validate() error {
	var errs []error
	v := vocabulary()
	reflexNames := map[string]bool{}
	for _, r := range s.Reflexes {
		if err := r.Validate(v); err != nil {
			errs = append(errs, err)
		}
		if reflexNames[r.Name] {
			errs = append(errs, fmt.Errorf("reflex %s is listed twice", r.Name))
		}
		reflexNames[r.Name] = true
	}
	presetNames := map[string]bool{}
	for _, p := range s.Presets {
		if err := p.Validate(); err != nil {
			errs = append(errs, err)
		}
		if presetNames[p.Name] {
			errs = append(errs, fmt.Errorf("preset %s is listed twice", p.Name))
		}
		presetNames[p.Name] = true
		for _, r := range p.Reflexes {
			if !reflexNames[r] {
				errs = append(errs, fmt.Errorf("preset %s names reflex %s, which is not defined", p.Name, r))
			}
		}
	}
	return errors.Join(errs...)
}

func requireField(field string) func(json.RawMessage) error {
	return func(raw json.RawMessage) error {
		var m map[string]any
		if json.Unmarshal(raw, &m) != nil {
			return errors.New("spec must be a JSON object")
		}
		if s, _ := m[field].(string); strings.TrimSpace(s) == "" {
			return fmt.Errorf("%s is required", field)
		}
		return nil
	}
}

// Options are runtime options: settings Nanite takes only in code (agent.Options), with no
// agent-directory file yet. The Workbench keeps them in its sidecar
// (workbench/options.json) and passes them when it runs the agent (eval runs); a host that
// opens the agent would pass them the same way. Of them, only CompactAt is exposed: slot
// budgets and placement are the engine's own.
type Options struct {
	Format int `json:"workbench_format"`
	// CompactAt is agent.Options.CompactAt: the request size in tokens (system blocks, tools
	// and conversation) at which the conversation is compacted. 0: 80% of the model's
	// context window.
	CompactAt int `json:"compact_at,omitempty"`
}

func parseOptions(raw []byte) (Options, error) {
	var o Options
	if len(bytes.TrimSpace(raw)) == 0 {
		return o, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&o); err != nil {
		return o, fmt.Errorf("options.json: %w", err)
	}
	if o.CompactAt < 0 {
		return o, errors.New("options.json: compact_at must not be negative")
	}
	return o, nil
}

// evalSteering installs the authored presets and reflexes. Eval runs have no
// Surface command/tool executor, so only Nanite's remind/halt actions execute.
func evalSteering(raw []byte) (*nanite.Steering, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil
	}
	s, err := parseSteering(raw)
	if err != nil {
		return nil, err
	}
	reg, err := steer.NewRegistry(steer.DefaultPresets()...)
	if err != nil {
		return nil, err
	}
	for _, p := range s.Presets {
		if err := reg.Add(steer.LayerApp, "", p); err != nil {
			return nil, err
		}
	}
	return &nanite.Steering{Registry: reg, Vocabulary: vocabulary(), Reflexes: func() []steer.Reflex { return s.Reflexes }}, nil
}
