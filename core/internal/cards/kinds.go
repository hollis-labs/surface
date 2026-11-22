package cards

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/hollis-labs/surface/module"
)

// The core's own kinds and what each one's payload holds. A card of another kind is a
// module's own: it is kept as given and the page draws it with the renderer registered
// for it, or says plainly that it has none.
//
//	info      {body, tone?}                      a notice; tone is "info", "success" or "warning"
//	error     {body, detail?}                    something failed
//	confirm   {body, confirm_label?, cancel_label?, danger?}
//	prompt    {question, body?, placeholder?, multiline?}
//	choice    {question, options: [{id, label, description?}], body?, multiple?, allow_other?}
//	form      {schema, body?, values?, submit_label?}       schema is a JSON Schema object (a subset)
//	approval  {summary, changes?: [{label, before?, after?}], requested_by?, phrase?}   with the card's risk
//	progress  {label?, value?, max?, status?, detail?}      status is "running", "done" or "failed"
//	plan      {steps: [{id, title, status?, detail?}]}      status is "pending", "active", "done" or "skipped"
//	entity    {ref?, refs?, entity_kind?, view?, question?, options?: [{id, label}], actions?: [{id, label}], data?, entities?}
//
// Answerable are confirm, prompt, choice, form and approval (and entity when it carries a
// question or selectable options/actions): the sender can wait on them.

// Answerable reports whether a kind of the core's asks the user for something.
func Answerable(kind string) bool {
	switch kind {
	case "confirm", "prompt", "choice", "form", "approval":
		return true
	}
	return false
}

// IsModuleEntityKind reports whether kind is "entity" or a module-scoped entity card kind.
func IsModuleEntityKind(kind string) bool {
	switch kind {
	case "entity", "ops.workstream", "ops.agent", "ops.host", "ops.followup",
		"registry.record", "tracker.list", "tracker.item",
		"align.proposal", "align.droppad", "work.item":
		return true
	}
	return false
}

// AnswerableCard reports whether a card asks the user for something, including an entity
// card (or module-scoped entity card) that carries a question, options, or actions.
func AnswerableCard(c module.Card) bool {
	if Answerable(c.Kind) {
		return true
	}
	if !IsModuleEntityKind(c.Kind) || len(c.Payload) == 0 {
		return false
	}
	var p struct {
		Question string   `json:"question"`
		Options  []option `json:"options"`
		Actions  []option `json:"actions"`
	}
	if json.Unmarshal(c.Payload, &p) != nil {
		return false
	}
	return p.Question != "" || len(p.Options) > 0 || len(p.Actions) > 0
}

type option struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type step struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// check refuses a card of a known kind whose payload lacks what the kind needs, so a
// mistake is the sender's error and not a blank card in front of the user.
func check(c module.Card) error {
	var p struct {
		Body       string          `json:"body"`
		Question   string          `json:"question"`
		Options    []option        `json:"options"`
		Actions    []option        `json:"actions"`
		Schema     json.RawMessage `json:"schema"`
		Summary    string          `json:"summary"`
		Steps      []step          `json:"steps"`
		Ref        string          `json:"ref"`
		Refs       []string        `json:"refs"`
		EntityKind string          `json:"entity_kind"`
		Data       json.RawMessage `json:"data"`
		Entities   json.RawMessage `json:"entities"`
		Resolved   json.RawMessage `json:"resolved"`
	}
	known := map[string]bool{"info": true, "error": true, "confirm": true, "prompt": true, "choice": true, "form": true, "approval": true, "progress": true, "plan": true, "entity": true}
	if !known[c.Kind] && !IsModuleEntityKind(c.Kind) {
		return nil
	}
	if err := json.Unmarshal(c.Payload, &p); err != nil {
		return fmt.Errorf("cards: the payload of a %s card is not an object: %w", c.Kind, err)
	}
	need := func(ok bool, what string) error {
		if ok {
			return nil
		}
		return fmt.Errorf("cards: a %s card needs %s", c.Kind, what)
	}
	if IsModuleEntityKind(c.Kind) {
		hasRef := p.Ref != "" || len(p.Refs) > 0
		hasData := (len(p.Data) > 0 && string(p.Data) != "null" && string(p.Data) != "{}") ||
			(len(p.Entities) > 0 && string(p.Entities) != "null" && string(p.Entities) != "[]" && string(p.Entities) != "{}") ||
			(len(p.Resolved) > 0 && string(p.Resolved) != "null" && string(p.Resolved) != "{}")
		if err := need(hasRef || hasData || c.Title != "" || p.Body != "" || p.Question != "", "a ref, refs, data, or a title"); err != nil {
			return err
		}
		for _, list := range [][]option{p.Options, p.Actions} {
			seen := map[string]bool{}
			for _, o := range list {
				if o.ID == "" || o.Label == "" || seen[o.ID] {
					return errors.New("cards: every option or action of an entity card needs an id of its own and a label")
				}
				seen[o.ID] = true
			}
		}
		return nil
	}
	switch c.Kind {
	case "info", "error", "confirm":
		return need(p.Body != "" || c.Title != "", "a body or a title")
	case "prompt":
		return need(p.Question != "", "a question")
	case "choice":
		if err := need(p.Question != "", "a question"); err != nil {
			return err
		}
		if err := need(len(p.Options) >= 1, "at least one option"); err != nil {
			return err
		}
		seen := map[string]bool{}
		for _, o := range p.Options {
			if o.ID == "" || o.Label == "" || seen[o.ID] {
				return errors.New("cards: every option of a choice card needs an id of its own and a label")
			}
			seen[o.ID] = true
		}
	case "form":
		var s struct {
			Type       string                     `json:"type"`
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if len(p.Schema) == 0 || json.Unmarshal(p.Schema, &s) != nil || len(s.Properties) == 0 {
			return errors.New("cards: a form card needs a schema: a JSON Schema object with properties")
		}
	case "approval":
		if err := need(p.Summary != "" || c.Title != "", "a summary or a title"); err != nil {
			return err
		}
		switch c.Risk {
		case "", "low", "medium", "high":
		default:
			return fmt.Errorf("cards: risk is low, medium or high, not %q", c.Risk)
		}
	case "plan":
		if err := need(len(p.Steps) >= 1, "at least one step"); err != nil {
			return err
		}
		for _, st := range p.Steps {
			if st.ID == "" || st.Title == "" {
				return errors.New("cards: every step of a plan card needs an id and a title")
			}
		}
	}
	return nil
}
