package agent

import (
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
func (s *Service) mountFrontDesk(mux *http.ServeMux) {
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
			reply(w, 200, map[string]string{"status": action})
		})
	}
}
