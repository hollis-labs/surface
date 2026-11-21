package agent

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/hollis-labs/libs/agent/desk"
	"github.com/hollis-labs/nanite/chatrunner"
	"github.com/hollis-labs/surface/module"
)

// FrontDeskConfig is optional host policy; nil keeps the existing chat behavior.
// Mode off enables paced deliveries without intake planning. Planning and automatic
// restart resume are explicit opt-ins and never change tool approvals.
type FrontDeskConfig struct {
	Mode                 string `json:"mode,omitempty"`
	PlanWithModel        bool   `json:"plan_with_model,omitempty"`
	PlannerInstructions  string `json:"planner_instructions,omitempty"`
	IntakeTimeoutSeconds int    `json:"intake_timeout_seconds,omitempty"`
	MergeWindowMS        int    `json:"merge_window_ms,omitempty"`
	ResumeInterrupted    bool   `json:"resume_interrupted,omitempty"`
}

func (c *FrontDeskConfig) validate() error {
	if c == nil {
		return nil
	}
	switch c.Mode {
	case "", "off", "shadow", "suggest", "auto":
	default:
		return fmt.Errorf("front_desk.mode must be off, shadow, suggest or auto")
	}
	if c.IntakeTimeoutSeconds < 0 || c.IntakeTimeoutSeconds > 300 {
		return fmt.Errorf("front_desk.intake_timeout_seconds must be between 0 and 300")
	}
	if c.MergeWindowMS < 0 || c.MergeWindowMS > 60000 {
		return fmt.Errorf("front_desk.merge_window_ms must be between 0 and 60000")
	}
	return nil
}
func (c *FrontDeskConfig) options() *chatrunner.DeskOptions {
	if c == nil {
		return nil
	}
	mode := desk.IntakeMode(c.Mode)
	if mode == "" {
		mode = desk.IntakeOff
	}
	policy := desk.DefaultPolicy()
	if c.MergeWindowMS > 0 {
		policy.MergeWindow = time.Duration(c.MergeWindowMS) * time.Millisecond
	}
	return &chatrunner.DeskOptions{Intake: desk.Intake{Mode: mode}, PlanWithModel: c.PlanWithModel, PlannerInstructions: c.PlannerInstructions, IntakeTimeout: time.Duration(c.IntakeTimeoutSeconds) * time.Second, Policy: &policy}
}
func openPlanDecisions(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS agent_plan_decisions (
 conversation TEXT NOT NULL, plan TEXT NOT NULL, action TEXT NOT NULL CHECK(action IN ('accept','decline')),
 PRIMARY KEY(conversation, plan))`)
	return err
}

func (s *Service) planDecision(ctx context.Context, conversation, plan string) (string, error) {
	var action string
	err := s.k.DB().QueryRowContext(ctx, `SELECT action FROM agent_plan_decisions WHERE conversation=? AND plan=?`, conversation, plan).Scan(&action)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return action, err
}

func (s *Service) recordPlanDecision(ctx context.Context, conversation, plan, action string) error {
	_, err := s.k.DB().ExecContext(ctx, `INSERT INTO agent_plan_decisions(conversation,plan,action) VALUES(?,?,?)`, conversation, plan, action)
	if err == nil {
		s.k.Events().Publish("agent.plan.decision", map[string]string{"plan": plan, "status": action})
	}
	return err
}

func (s *Service) mountFrontDesk(mux *http.ServeMux) {
	mux.HandleFunc("GET "+Prefix+"/front-desk/plans/{plan}", func(w http.ResponseWriter, r *http.Request) {
		action, err := s.planDecision(r.Context(), s.conversationID(r.Context()), r.PathValue("plan"))
		if err != nil {
			fail(w, 500, "could not read the plan decision")
			return
		}
		reply(w, 200, map[string]string{"status": action})
	})
	mux.HandleFunc("GET "+Prefix+"/front-desk", func(w http.ResponseWriter, r *http.Request) {
		if s.wire.nanite == nil || s.wire.nanite.Desk() == nil {
			reply(w, 200, map[string]any{"enabled": false})
			return
		}
		reply(w, 200, map[string]any{"enabled": true, "topics": s.wire.nanite.Desk().Ledger().List(nil)})
	})
	for _, action := range []string{"accept", "decline"} {
		mux.HandleFunc("POST "+Prefix+"/front-desk/plans/{plan}/"+action, func(w http.ResponseWriter, r *http.Request) {
			if module.ActorOf(r.Context()).Delegate() {
				fail(w, http.StatusForbidden, "only the owner can answer a front-desk plan")
				return
			}
			if s.wire.nanite == nil || s.wire.nanite.Desk() == nil {
				fail(w, http.StatusConflict, "the front desk is not enabled")
				return
			}
			// Resolve the current durable conversation through the same mapping used for turns.
			conversation := s.conversationID(r.Context())
			id, err := s.wire.naniteChatID(r.Context(), conversation)
			if err != nil {
				fail(w, http.StatusConflict, err.Error())
				return
			}
			previous, err := s.planDecision(r.Context(), conversation, r.PathValue("plan"))
			if err != nil {
				fail(w, 500, "could not read the plan decision")
				return
			}
			if previous != "" {
				fail(w, http.StatusConflict, "this plan was already answered")
				return
			}
			d := s.wire.nanite.Desk()
			if action == "accept" {
				err = d.AcceptPlan(r.Context(), id, r.PathValue("plan"), nil)
			} else {
				err = d.DeclinePlan(id, r.PathValue("plan"))
			}
			if err != nil {
				fail(w, http.StatusConflict, err.Error())
				return
			}
			// The answer has taken effect; persist it even if the browser disconnects.
			save, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
			defer cancel()
			if err := s.recordPlanDecision(save, conversation, r.PathValue("plan"), action); err != nil {
				fail(w, 500, "plan answered but its display state could not be saved")
				return
			}
			reply(w, 200, map[string]string{"status": action})
		})
	}
}
