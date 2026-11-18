// Package router stands in front of the agent. The owner's message is first put to a set of
// rules (libs core/util/router: conditions on a few plain fields of the message and the
// situation); a rule that matches names a tool or a command to run directly.
//
// A routed call that only looks or changes what is on screen (read and act levels) runs
// straight away and the agent is told it was done. One that writes is not run here: the
// agent is told the owner asked for it, and calls it itself, so the owner's say on a change
// is never skipped.
//
// Subject fields: text (the message), slash (the word after a leading "/"), arg (the rest
// after the slash word), view, module (of the window in front), slot, mode, layout.
// Targets: "tool:<name>" or "command:<id>". A rule that is not "auto" only tells the agent
// what the owner asked for; an auto rule runs it (within the level limit above). Param values may use {text}, {arg}, {view},
// {module}, {slot}, {mode}, {layout}; a param named "_say" is what the agent is told.
//
// The built-in rules are in rules.json. The owner's own (and the Interaction Builder's) live
// in the core database and are tried first.
package router

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	librouter "github.com/hollis-labs/libs/core/util/router"

	"github.com/hollis-labs/surface/core/internal/storage"
)

//go:embed rules.json
var builtin []byte

//go:embed migrations/*.sql
var migrations embed.FS

// Rule is a libs router rule.
type Rule = librouter.Rule

// Fields is what a message is routed on.
type Fields struct {
	Text, View, Module, Slot, Mode, Layout string
}

// Route is a rule that matched.
type Route struct {
	Rule  string         `json:"rule"`
	Kind  string         `json:"kind"` // "tool" or "command"
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
	Say   string         `json:"say,omitempty"`
	// Ask is set when the rule is not auto: the agent is told what was asked, and decides.
	Ask    bool              `json:"ask,omitempty"`
	Params map[string]string `json:"-"`
}

// Router holds the rules.
type Router struct {
	db      *sql.DB
	builtin []Rule
}

// Open reads the built-in rules and migrates the owner's rules table into db.
func Open(ctx context.Context, db *sql.DB) (*Router, error) {
	if err := storage.Migrate(ctx, db, "router", migrations, "migrations"); err != nil {
		return nil, err
	}
	set, err := librouter.ParseRuleSet(builtin)
	if err != nil {
		return nil, fmt.Errorf("built-in rules: %w", err)
	}
	return &Router{db: db, builtin: set.Rules}, nil
}

// Builtin returns the shipped rules.
func (r *Router) Builtin() []Rule { return slices.Clone(r.builtin) }

// Rules returns the owner's rules.
func (r *Router) Rules(ctx context.Context) ([]Rule, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT body FROM route_rules ORDER BY priority DESC, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Rule
	for rows.Next() {
		var body string
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		var rule Rule
		if json.Unmarshal([]byte(body), &rule) == nil {
			out = append(out, rule)
		}
	}
	return out, rows.Err()
}

// Put adds an owner rule or replaces the one with its id.
func (r *Router) Put(ctx context.Context, rule Rule) error {
	if !strings.HasPrefix(rule.Target, "tool:") && !strings.HasPrefix(rule.Target, "command:") {
		return fmt.Errorf("rule %q: target must be tool:<name> or command:<id>", rule.ID)
	}
	if _, err := librouter.Compile(librouter.RuleSet{Version: "check", Strategy: librouter.FirstMatch, Rules: []Rule{rule}}, librouter.Config{}); err != nil {
		return err
	}
	body, _ := json.Marshal(rule)
	_, err := r.db.ExecContext(ctx, `INSERT INTO route_rules (id, priority, body) VALUES (?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET priority = excluded.priority, body = excluded.body`, rule.ID, rule.Priority, string(body))
	return err
}

// Delete removes an owner rule.
func (r *Router) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM route_rules WHERE id = ?`, id)
	return err
}

var slashRe = regexp.MustCompile(`^/([A-Za-z0-9_.-]+)\s*(.*)$`)

// Route puts a message to the rules: the owner's first, then the built-in ones. It returns
// nil when nothing applies.
func (r *Router) Route(ctx context.Context, f Fields) (*Route, error) {
	own, err := r.Rules(ctx)
	if err != nil {
		return nil, err
	}
	rules := append(own, r.builtin...)
	// Owner rules first: lift their priority above every built-in one, keeping their order.
	for i := range own {
		rules[i].Priority += 1_000_000
	}
	router, err := librouter.Compile(librouter.RuleSet{Version: "surface", Strategy: librouter.FirstMatch, Rules: rules}, librouter.Config{})
	if err != nil {
		return nil, err
	}
	text := strings.TrimSpace(f.Text)
	fields := map[string]string{"text": text, "view": f.View, "module": f.Module, "slot": f.Slot, "mode": f.Mode, "layout": f.Layout}
	if m := slashRe.FindStringSubmatch(text); m != nil {
		fields["slash"] = strings.ToLower(m[1])
		if a := strings.TrimSpace(m[2]); a != "" {
			fields["arg"] = a
		}
	}
	for k, v := range fields {
		if v == "" {
			delete(fields, k)
		}
	}
	d, err := router.Route(ctx, librouter.Subject{ID: "message", Fields: fields})
	if err != nil || (d.Outcome != librouter.Routed && d.Outcome != librouter.Suggested) {
		return nil, err
	}
	for _, m := range d.Matches {
		if m.Disposition != librouter.Apply && m.Disposition != librouter.Ask {
			continue
		}
		kind, name, _ := strings.Cut(m.Target, ":")
		rt := &Route{Rule: m.RuleID, Kind: kind, Name: name, Input: map[string]any{}, Params: m.Params, Ask: m.Disposition == librouter.Ask}
		for k, v := range m.Params {
			v = expand(v, fields)
			if k == "_say" {
				rt.Say = v
				continue
			}
			if strings.HasPrefix(k, "_") {
				continue
			}
			rt.Input[k] = v
		}
		return rt, nil
	}
	return nil, nil
}

var placeholder = regexp.MustCompile(`\{([a-z_]+)\}`)

func expand(s string, fields map[string]string) string {
	return placeholder.ReplaceAllStringFunc(s, func(p string) string { return fields[p[1:len(p)-1]] })
}
