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
//
// Answerable are confirm, prompt, choice, form and approval: the sender can wait on them.

// Answerable reports whether a kind of the core's asks the user for something.
func Answerable(kind string) bool {
	switch kind {
	case "confirm", "prompt", "choice", "form", "approval":
		return true
	}
	return false
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
		Body     string          `json:"body"`
		Question string          `json:"question"`
		Options  []option        `json:"options"`
		Schema   json.RawMessage `json:"schema"`
		Summary  string          `json:"summary"`
		Steps    []step          `json:"steps"`
	}
	known := map[string]bool{"info": true, "error": true, "confirm": true, "prompt": true, "choice": true, "form": true, "approval": true, "progress": true, "plan": true}
	if !known[c.Kind] {
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
