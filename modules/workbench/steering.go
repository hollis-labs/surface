package workbench

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/hollis-labs/libs/agent/core/reflexes"
)

// Steering is an agent's reflexes, triggers and presets as the Workbench keeps them, in its
// own sidecar (workbench/steering.json), not in the agent directory: no agent-directory
// format for them is decided yet. It is draft configuration: Nanite v0.6.0 has no runtime
// for reflexes (N6-05 designs one), so eval runs do not apply it.
//
// The shapes mirror libs agent/steer v0.1.0 (libs#174, in review): Preset and Reflex below
// marshal to the same JSON as steer.Preset and steer.Reflex, and validation follows
// steer.Reflex.Validate with the vocabulary a Surface host registers. When steer is tagged,
// these types become an import of it; a host passes them as typed options (Nanite's
// agent.Options.Steering, Nanite PR 25) when it opens the agent.
type Steering struct {
	// Format is the sidecar format's version.
	Format   int      `json:"workbench_format"`
	Presets  []Preset `json:"presets,omitempty"`
	Reflexes []Reflex `json:"reflexes,omitempty"`
}

// Preset is a named bundle of behavior (steer.Preset). A mode is a preset the owner chose.
type Preset struct {
	Name         string   `json:"name"`
	About        string   `json:"about,omitempty"`
	Slots        []string `json:"slots,omitempty"`
	Instructions string   `json:"instructions,omitempty"`
	Reflexes     []string `json:"reflexes,omitempty"`
	Rules        []string `json:"rules,omitempty"`
	Tools        []string `json:"tools,omitempty"`
	OutputStyle  string   `json:"output_style,omitempty"`
	Delivery     string   `json:"delivery,omitempty"`
	Dispatch     []string `json:"dispatch,omitempty"`
}

// Reflex is one standing rule (steer.Reflex): a trigger and an action.
type Reflex struct {
	Name        string          `json:"name"`
	About       string          `json:"about,omitempty"`
	TriggerKind string          `json:"trigger_kind"`
	Trigger     json.RawMessage `json:"trigger"`
	ActionKind  string          `json:"action_kind"`
	Action      json.RawMessage `json:"action"`
	Cooldown    int             `json:"cooldown_seconds,omitempty"`
	Enabled     bool            `json:"enabled,omitempty"`
}

// Trigger kinds (steer's), and the action kinds: remind and halt are steer's own; the rest
// are the ones a Surface host registers and carries out (core/internal/reflex).
var (
	triggerKinds = []string{"text", "predicate", "event", "focus"}
	actionKinds  = []string{"remind", "halt", "run_command", "call_tool"}
	steerNameRe  = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
)

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

// Validate checks every preset and reflex, and that presets name reflexes that exist.
func (s Steering) Validate() error {
	var errs []error
	reflexNames := map[string]bool{}
	for _, r := range s.Reflexes {
		if err := r.Validate(); err != nil {
			errs = append(errs, err)
		}
		if reflexNames[r.Name] {
			errs = append(errs, fmt.Errorf("reflex %s is listed twice", r.Name))
		}
		reflexNames[r.Name] = true
	}
	presetNames := map[string]bool{}
	for _, p := range s.Presets {
		if !steerNameRe.MatchString(p.Name) {
			errs = append(errs, fmt.Errorf("preset name %q: use lower case letters, digits, dots, dashes and underscores", p.Name))
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

func object(raw json.RawMessage) map[string]any {
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	return m
}

func requireField(raw json.RawMessage, field string) error {
	if s, _ := object(raw)[field].(string); strings.TrimSpace(s) == "" {
		return fmt.Errorf("%s is required", field)
	}
	return nil
}

// Validate checks a reflex as steer.Reflex.Validate does, with Surface's action kinds.
func (r Reflex) Validate() error {
	if !steerNameRe.MatchString(r.Name) {
		return fmt.Errorf("reflex name %q: use lower case letters, digits, dots, dashes and underscores", r.Name)
	}
	if r.Cooldown < 0 {
		return fmt.Errorf("reflex %s: cooldown cannot be negative", r.Name)
	}
	t := object(r.Trigger)
	switch r.TriggerKind {
	case "text":
		p, _ := t["pattern"].(string)
		if _, err := regexp.Compile(p); p == "" || err != nil {
			return fmt.Errorf("reflex %s: text trigger: pattern must be a regular expression", r.Name)
		}
	case "predicate":
		if t == nil {
			return fmt.Errorf("reflex %s: predicate trigger: spec must be a JSON object", r.Name)
		}
		if _, err := reflexes.EvaluateTrigger("predicate", string(r.Trigger), reflexes.State{}); err != nil {
			return fmt.Errorf("reflex %s: predicate trigger: %w", r.Name, err)
		}
	case "event":
		if s, _ := t["topic"].(string); s == "" {
			return fmt.Errorf("reflex %s: event trigger: topic is required", r.Name)
		}
	case "focus":
		if s, _ := t["view"].(string); s == "" {
			return fmt.Errorf("reflex %s: focus trigger: view is required", r.Name)
		}
	default:
		return fmt.Errorf("reflex %s: trigger kind %q: use %s", r.Name, r.TriggerKind, strings.Join(triggerKinds, ", "))
	}
	var err error
	switch r.ActionKind {
	case "remind":
		err = requireField(r.Action, "body")
	case "halt":
		err = requireField(r.Action, "message")
		if err == nil && r.TriggerKind != "text" && r.TriggerKind != "predicate" {
			return fmt.Errorf("reflex %s: halt only answers a message; use a text or predicate trigger", r.Name)
		}
	case "run_command":
		err = requireField(r.Action, "command")
	case "call_tool":
		err = requireField(r.Action, "tool")
	default:
		return fmt.Errorf("reflex %s: action kind %q is not in the vocabulary (%s)", r.Name, r.ActionKind, strings.Join(actionKinds, ", "))
	}
	if err != nil {
		return fmt.Errorf("reflex %s: %s: %w", r.Name, r.ActionKind, err)
	}
	return nil
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
