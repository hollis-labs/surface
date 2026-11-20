package agent

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	nanite "github.com/hollis-labs/nanite/agent"

	"github.com/hollis-labs/surface/core/internal/agent/profile"
	"github.com/hollis-labs/surface/module"
)

// Module profiles: a module may ship an agent profile (module.Manifest.Profile), and the
// app's one agent works as the profile of the module in front, or of the one the owner or
// the agent switched to. Every profile shares the app's conversation, tools, approvals,
// Approver and identity; a profile brings instructions, context, skills, helper variants
// and tool packages, never authority.
//
// Which profile a turn runs as:
//
//  1. the conversation's pin, when the owner (the panel's picker) or the agent
//     (profile_switch) set one: a module's profile, or "default", the app's seat;
//  2. else the profile of the module whose window is in front;
//  3. else the app's own seat (no profile).
//
// Profiles need the Nanite runner, which reads its agent directory at the start of every
// turn: the runner opens on a staged directory and Surface writes the seat with the active
// profile over it before each turn (profile/stage.go). On any other runner every turn runs as
// the seat, and the app says so once in its log.

// Profile choices that are not a module.
const (
	// ProfileDefault pins the app's own seat.
	ProfileDefault = "default"
	// ProfileAuto clears a pin: the window in front chooses again.
	ProfileAuto = "auto"
)

// TopicProfile is published when a conversation's profile pin changes.
const TopicProfile = "agent.profile"

// profiles is the app's module profiles and the runner's staged directory.
type profiles struct {
	all   map[string]*profile.Profile
	order []string
	// stage is the staged agent directory the Nanite runner opened on; "" when profiles are
	// off (no module ships one, or the runner is not Nanite).
	stage, seat string
	mu          sync.Mutex // serializes staging
	staged      string     // the profile last staged ("" the seat)
}

// loadProfiles reads and checks every module's profile. A profile that does not check out
// is left out, with the reason in the log: it must not stop the app.
func (s *Service) loadProfiles() {
	s.prof = &profiles{all: map[string]*profile.Profile{}}
	for _, b := range s.k.Profiles() {
		p, err := profile.Load(b.Module, b.Title, b.Files)
		switch {
		case err != nil:
			s.log.Error("a module's agent profile does not check out; it is left out", "module", b.Module, "err", err)
		case p == nil:
			s.log.Debug("a module declares an agent profile slot with nothing in it yet", "module", b.Module)
		default:
			s.prof.all[p.ID] = p
			s.prof.order = append(s.prof.order, p.ID)
		}
	}
}

// profilesOn reports whether turns run as profiles: there is one, and the runner stages them.
func (s *Service) profilesOn() bool { return s.prof != nil && s.prof.stage != "" }

// stageDir is where the Nanite runner's staged agent directory is kept.
func (s *Service) stageDir(agent string) string {
	return filepath.Join(s.k.DataDir(), "agents", agent+".staged")
}

// profileBuiltins is the Nanite tool groups the profiles need on top of the config's: the
// subtask tool when one has variants (a variant only narrows the parent's tools) and the
// read-only skill tools when one has skills. Tool packages run code, so their group
// ("tools") is the owner's to turn on in the agent config; without it they are not offered.
func (s *Service) profileBuiltins(groups []nanite.BuiltinGroup) []nanite.BuiltinGroup {
	has := func(g nanite.BuiltinGroup) bool {
		for _, x := range groups {
			if x == g {
				return true
			}
		}
		return false
	}
	for _, id := range s.prof.order {
		p := s.prof.all[id]
		if len(p.Variants) > 0 && !has(nanite.BuiltinSubtasks) {
			groups = append(groups, nanite.BuiltinSubtasks)
		}
		if len(p.Skills) > 0 && !has(nanite.BuiltinSkills) {
			groups = append(groups, nanite.BuiltinSkills)
		}
		if len(p.Tools) > 0 && !has(nanite.BuiltinTools) {
			s.log.Info("a profile ships tool packages; they are offered only when the agent config's builtins include \"tools\"",
				"module", id, "tools", p.Tools)
		}
	}
	return groups
}

// ProfileChoice is the profile a turn runs as, and why.
type ProfileChoice struct {
	// ID is the module whose profile it is; "" is the app's own seat.
	ID string `json:"id"`
	// Why is "pinned", "focus", "default" or "off" (the runner does not run profiles).
	Why string `json:"why"`
}

// chooseProfile is the profile for a turn in conversation, in the situation sit.
func (s *Service) chooseProfile(ctx context.Context, conversation string, sit module.Situation) ProfileChoice {
	if !s.profilesOn() {
		return ProfileChoice{Why: "off"}
	}
	if pin, err := readProfilePin(ctx, s.k.DB(), conversation); err != nil {
		s.log.Warn("the profile pin was not read", "err", err)
	} else if pin == ProfileDefault {
		return ProfileChoice{Why: "pinned"}
	} else if _, ok := s.prof.all[pin]; ok {
		return ProfileChoice{ID: pin, Why: "pinned"}
	}
	if sit.Focused != nil {
		if _, ok := s.prof.all[sit.Focused.Module]; ok {
			return ProfileChoice{ID: sit.Focused.Module, Why: "focus"}
		}
	}
	return ProfileChoice{Why: "default"}
}

// stageProfile writes the staged directory for a turn as the chosen profile. The seat is
// read again each time, so the owner's edits to it (standing.md) reach the next turn.
func (s *Service) stageProfile(c ProfileChoice) error {
	if !s.profilesOn() {
		return nil
	}
	pr := s.prof
	pr.mu.Lock()
	defer pr.mu.Unlock()
	if err := profile.Stage(pr.stage, pr.seat, pr.all[c.ID]); err != nil {
		return err
	}
	if pr.staged != c.ID {
		s.log.Info("the agent's profile changed", "from", profileName(pr.staged), "to", profileName(c.ID), "why", c.Why)
		pr.staged = c.ID
	}
	return nil
}

func profileName(id string) string {
	if id == "" {
		return ProfileDefault
	}
	return id
}

// profileLine is the context block's line on the profile in play, for the agent.
func (s *Service) profileLine(c ProfileChoice) string {
	if !s.profilesOn() || len(s.prof.order) == 0 {
		return ""
	}
	how := map[string]string{"pinned": "pinned", "focus": "from the window in front", "default": "no module profile in front"}[c.Why]
	if c.ID == "" {
		return fmt.Sprintf("Profile: the app's own (%s). profile_switch lists the others.", how)
	}
	return fmt.Sprintf("Profile: %s (%s, %s). Its instructions are in your system prompt under \"Profile\". profile_switch changes it.", c.ID, s.prof.all[c.ID].Title, how)
}

// conversationID is the conversation the owner's panel and the agent's tools act on: the
// wire's one conversation for the app's agent, or "" with no wire.
func (s *Service) conversationID(ctx context.Context) string {
	if s.wire == nil {
		return ""
	}
	view, err := s.wire.svc.EnsureConversation(ctx, s.wire.agent)
	if err != nil {
		return ""
	}
	return view.ConversationID
}

// ProfileInfo is one profile, for the panel and profile_switch.
type ProfileInfo struct {
	*profile.Profile
	Active bool `json:"active"`
}

// ProfileState is what GET /api/agent/profiles says.
type ProfileState struct {
	// On says turns run as profiles (the Nanite runner); off, every turn is the seat.
	On     bool          `json:"on"`
	Runner string        `json:"runner,omitempty"`
	Active ProfileChoice `json:"active"`
	// Pinned is the conversation's pin: a module id, "default", or "".
	Pinned   string        `json:"pinned,omitempty"`
	Profiles []ProfileInfo `json:"profiles"`
}

func (s *Service) profileState(ctx context.Context) ProfileState {
	conv := s.conversationID(ctx)
	st := ProfileState{On: s.profilesOn(), Profiles: []ProfileInfo{}}
	if s.wire != nil {
		st.Runner = s.wire.runner
	}
	st.Active = s.chooseProfile(ctx, conv, s.k.Situation(ctx))
	if st.On {
		st.Pinned, _ = readProfilePin(ctx, s.k.DB(), conv)
	}
	if s.prof != nil {
		for _, id := range s.prof.order {
			st.Profiles = append(st.Profiles, ProfileInfo{Profile: s.prof.all[id], Active: st.Active.ID == id})
		}
	}
	return st
}

// pinProfile sets the conversation's profile pin: a module's id, "default" for the seat, or
// "auto" (or "") to let the window in front choose again. It changes what the agent is told
// from its next turn; the tools, levels and approvals are the same for every profile.
func (s *Service) pinProfile(ctx context.Context, choice string, actor module.Actor) (ProfileState, error) {
	choice = strings.TrimSpace(choice)
	if !s.profilesOn() {
		return ProfileState{}, errors.New("this app's agent does not run module profiles (they need the Nanite runner, and a module that ships one)")
	}
	switch choice {
	case "", ProfileAuto:
		choice = ""
	case ProfileDefault:
	default:
		if _, ok := s.prof.all[choice]; !ok {
			return ProfileState{}, fmt.Errorf("no profile %q (known: %s, %s, %s)", choice, strings.Join(s.prof.order, ", "), ProfileDefault, ProfileAuto)
		}
	}
	conv := s.conversationID(ctx)
	if err := writeProfilePin(ctx, s.k.DB(), conv, choice, string(actor)); err != nil {
		return ProfileState{}, err
	}
	st := s.profileState(ctx)
	s.k.Events().Publish(TopicProfile, map[string]any{"conversation_id": conv, "pinned": choice, "active": st.Active, "actor": actor})
	return st, nil
}

// The pins, per conversation, in the core database: they outlive a restart, and a new
// conversation starts unpinned.

const profilePinSchema = `CREATE TABLE IF NOT EXISTS agent_profile_pins (
	conversation TEXT PRIMARY KEY,
	profile      TEXT NOT NULL,
	actor        TEXT NOT NULL DEFAULT '',
	updated_at   TEXT NOT NULL
)`

func openProfilePins(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, profilePinSchema)
	return err
}

func readProfilePin(ctx context.Context, db *sql.DB, conversation string) (string, error) {
	var p string
	err := db.QueryRowContext(ctx, `SELECT profile FROM agent_profile_pins WHERE conversation = ?`, conversation).Scan(&p)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return p, err
}

func writeProfilePin(ctx context.Context, db *sql.DB, conversation, choice, actor string) error {
	if choice == "" {
		_, err := db.ExecContext(ctx, `DELETE FROM agent_profile_pins WHERE conversation = ?`, conversation)
		return err
	}
	_, err := db.ExecContext(ctx, `INSERT INTO agent_profile_pins (conversation, profile, actor, updated_at) VALUES (?,?,?,?)
		ON CONFLICT(conversation) DO UPDATE SET profile = excluded.profile, actor = excluded.actor, updated_at = excluded.updated_at`,
		conversation, choice, actor, time.Now().UTC().Format(time.RFC3339))
	return err
}
