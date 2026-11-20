package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/hollis-labs/surface/core/internal/kernel"
	"github.com/hollis-labs/surface/core/internal/reflex"
	"github.com/hollis-labs/surface/core/internal/router"
	"github.com/hollis-labs/surface/core/internal/slots"
	"github.com/hollis-labs/surface/module"
)

func schema(s string) json.RawMessage { return json.RawMessage(s) }

// ownTools are the agent's tools over its own state: the slots, the reflexes and the
// routing rules. The Interaction Builder uses the reflex and rule tools to set a flow up.
func (s *Service) ownTools() []kernel.BoundTool {
	t := func(name, desc string, lvl module.Level, in string, run func(ctx context.Context, in json.RawMessage) (any, error)) kernel.BoundTool {
		return kernel.BoundTool{Tool: module.Tool{Name: name, Description: desc, Level: lvl, Input: schema(in)}, Module: "agent", Call: run}
	}
	return []kernel.BoundTool{
		t("context_note", "Keep your note on a slot (the kind of work): where it stands, in a few lines a later you can pick up from. Replaces the note; empty removes it. Defaults to the slot in play.",
			module.LevelAct, `{"type":"object","properties":{"note":{"type":"string"},"slot":{"type":"string"}},"required":["note"]}`,
			func(ctx context.Context, in json.RawMessage) (any, error) {
				var a struct{ Note, Slot string }
				if err := json.Unmarshal(in, &a); err != nil {
					return nil, err
				}
				if a.Slot == "" {
					a.Slot = s.Preview(ctx).Slot
				}
				if _, ok := slots.Named(s.k.Slots(), a.Slot); !ok {
					return nil, fmt.Errorf("no slot %q", a.Slot)
				}
				if err := s.notes.Set(ctx, a.Slot, a.Note); err != nil {
					return nil, err
				}
				s.k.Events().Publish("agent.note", map[string]string{"slot": a.Slot})
				return "Kept the note on " + a.Slot + ".", nil
			}),
		t("standing_get", "Read your standing instructions: the owner's rules that hold across all work (context/standing.md in your agent directory), and whether your system prompt has this text yet.",
			module.LevelRead, `{"type":"object","properties":{}}`,
			func(context.Context, json.RawMessage) (any, error) { return s.standingGet() }),
		t("standing_update", "Replace your standing instructions (the owner's rules across all work) with the whole new text, when the owner sets or changes a rule, or an address, routing or tool quirk changes. Give the full text, not a part (standing_get reads the current one), and why. The old text is kept. Work state goes in slot notes (context_note), not here.",
			module.LevelWrite, `{"type":"object","properties":{"text":{"type":"string","description":"The whole new standing text, at most 16,000 characters."},"why":{"type":"string","description":"What changed and why, in a line."}},"required":["text","why"]}`,
			func(_ context.Context, in json.RawMessage) (any, error) {
				var a struct{ Text, Why string }
				if err := json.Unmarshal(in, &a); err != nil {
					return nil, err
				}
				return s.standingUpdate(a.Text, a.Why)
			}),
		t("context_switch", "Move to another slot (kind of work) when the owner's work is plainly of that kind and the window in front does not say so. Holds until they bring a different window forward. Lists the slots when called with none.",
			module.LevelAct, `{"type":"object","properties":{"slot":{"type":"string"}}}`,
			func(ctx context.Context, in json.RawMessage) (any, error) {
				var a struct{ Slot string }
				_ = json.Unmarshal(in, &a)
				if a.Slot == "" {
					return s.slotList(ctx), nil
				}
				if s.pin == nil {
					return nil, errors.New("the slots are chosen by a classifier here; they cannot be pinned")
				}
				if _, ok := slots.Named(s.k.Slots(), a.Slot); !ok {
					return nil, fmt.Errorf("no slot %q", a.Slot)
				}
				view := ""
				if w, ok := s.k.Desk().Focused(ctx); ok {
					view = w.View
				}
				before := s.Preview(ctx).Slot
				s.pin.Pin(a.Slot, view)
				s.signal(ctx, "pin", map[string]string{"slot": a.Slot, "from": before, "view": view})
				asm := s.Preview(ctx)
				s.k.Events().Publish("agent.context", asm)
				return asm.Block, nil
			}),
		t("agent_reflex_list", "List the reflexes: standing rules that fire on the owner's message (text or predicate trigger), on an app event, or when a view comes in front, and remind you, answer for you, run a command or call a look-only tool.",
			module.LevelRead, `{"type":"object","properties":{}}`,
			func(ctx context.Context, _ json.RawMessage) (any, error) { return s.reflexes.List(ctx) }),
		t("agent_reflex_put", `Add or replace a reflex. trigger_kind: text {"pattern": regexp} | event {"topic": "notes.saved"} | focus {"view": "notes"} | predicate {libs reflex predicate}. action_kind: remind {"body"} | halt {"message"} (text/predicate only) | run_command {"command", "args"} | call_tool {"tool", "input"} (read/act tools only). enabled turns it on everywhere; off, it is on only in a layout whose setup.reflexes lists it.`,
			module.LevelWrite, `{"type":"object","properties":{"name":{"type":"string"},"about":{"type":"string"},"trigger_kind":{"type":"string","enum":["text","event","focus","predicate"]},"trigger":{"type":"object"},"action_kind":{"type":"string","enum":["remind","halt","run_command","call_tool"]},"action":{"type":"object"},"cooldown_seconds":{"type":"integer"},"enabled":{"type":"boolean"}},"required":["name","trigger_kind","trigger","action_kind","action"]}`,
			func(ctx context.Context, in json.RawMessage) (any, error) {
				var r reflex.Reflex
				if err := json.Unmarshal(in, &r); err != nil {
					return nil, err
				}
				if err := s.reflexes.Put(ctx, r); err != nil {
					return nil, err
				}
				s.k.Events().Publish("agent.reflexes", map[string]string{"name": r.Name})
				return "Kept the reflex " + r.Name + ".", nil
			}),
		t("agent_reflex_enable", "Switch a reflex on or off everywhere.",
			module.LevelWrite, `{"type":"object","properties":{"name":{"type":"string"},"enabled":{"type":"boolean"}},"required":["name","enabled"]}`,
			func(ctx context.Context, in json.RawMessage) (any, error) {
				var a struct {
					Name    string
					Enabled bool
				}
				if err := json.Unmarshal(in, &a); err != nil {
					return nil, err
				}
				if err := s.reflexes.SetEnabled(ctx, a.Name, a.Enabled); err != nil {
					return nil, err
				}
				s.k.Events().Publish("agent.reflexes", map[string]string{"name": a.Name})
				return fmt.Sprintf("%s is %s.", a.Name, map[bool]string{true: "on", false: "off"}[a.Enabled]), nil
			}),
		t("agent_reflex_delete", "Remove a reflex.",
			module.LevelConfirm, `{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}`,
			func(ctx context.Context, in json.RawMessage) (any, error) {
				var a struct{ Name string }
				if err := json.Unmarshal(in, &a); err != nil {
					return nil, err
				}
				if err := s.reflexes.Delete(ctx, a.Name); err != nil {
					return nil, err
				}
				s.k.Events().Publish("agent.reflexes", map[string]string{"name": a.Name})
				return "Removed " + a.Name + ".", nil
			}),
		t("agent_rule_list", "List the routing rules: the owner's (tried first) and the built-in ones. A rule that matches the owner's message runs a tool or command before you are asked.",
			module.LevelRead, `{"type":"object","properties":{}}`,
			func(ctx context.Context, _ json.RawMessage) (any, error) {
				own, err := s.router.Rules(ctx)
				if err != nil {
					return nil, err
				}
				return map[string]any{"owner": own, "builtin": s.router.Builtin()}, nil
			}),
		t("agent_rule_put", `Add or replace a routing rule (libs core/util/router). when: [{field, op: eq|not_eq|in|prefix|glob|exists, value|values}] over fields text, slash, arg, view, module, slot, mode, layout. target: "tool:<name>" or "command:<id>". params: the tool's input, values may use {arg} {text} {view} {module} {slot} {mode} {layout}; "_say" is what you are told. Read and act tools run directly; others are passed to you to call.`,
			module.LevelWrite, `{"type":"object","properties":{"id":{"type":"string"},"priority":{"type":"integer"},"when":{"type":"array","items":{"type":"object"}},"target":{"type":"string"},"params":{"type":"object","additionalProperties":{"type":"string"}},"disabled":{"type":"boolean"}},"required":["id","when","target"]}`,
			func(ctx context.Context, in json.RawMessage) (any, error) {
				var r router.Rule
				if err := json.Unmarshal(in, &r); err != nil {
					return nil, err
				}
				if err := s.router.Put(ctx, r); err != nil {
					return nil, err
				}
				return "Kept the rule " + r.ID + ".", nil
			}),
		t("agent_rule_delete", "Remove one of the owner's routing rules.",
			module.LevelConfirm, `{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`,
			func(ctx context.Context, in json.RawMessage) (any, error) {
				var a struct{ ID string }
				if err := json.Unmarshal(in, &a); err != nil {
					return nil, err
				}
				return "Removed " + a.ID + ".", s.router.Delete(ctx, a.ID)
			}),
	}
}

// SlotInfo is one slot, for the panel and context_switch.
type SlotInfo struct {
	Name        string   `json:"name"`
	Module      string   `json:"module,omitempty"`
	About       string   `json:"about"`
	Views       []string `json:"views,omitempty"`
	Modes       []string `json:"modes,omitempty"`
	Tools       []string `json:"tools,omitempty"`
	Note        string   `json:"note,omitempty"`
	NoteUpdated string   `json:"note_updated,omitempty"`
	Active      bool     `json:"active"`
}

func (s *Service) slotList(ctx context.Context) []SlotInfo {
	active := s.Preview(ctx).Slot
	all := append([]kernel.BoundSlot{slots.General}, s.k.Slots()...)
	out := make([]SlotInfo, 0, len(all))
	for _, sl := range all {
		i := SlotInfo{Name: sl.Name, Module: sl.Module, About: sl.About, Views: sl.Views, Modes: sl.Modes, Tools: sl.Tools, Active: sl.Name == active}
		i.Note, i.NoteUpdated = s.notes.Get(ctx, sl.Name)
		out = append(out, i)
	}
	return out
}

// Tools are the agent's own tools (slots, reflexes, routing rules), bound like the app's,
// for the app to offer outside agents too (/api/tools, MCP).
func (s *Service) Tools() []kernel.BoundTool { return s.ownTools() }
