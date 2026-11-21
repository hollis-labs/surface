package agent

import (
	"context"
	"errors"
	"net/http"

	nanite "github.com/hollis-labs/nanite/agent"
	"github.com/hollis-labs/nanite/chatrunner"
	"github.com/hollis-labs/surface/module"
)

// A turn the previous process left unfinished is offered to the owner on the chat as a
// nanite.interrupted activity (chatrunner.ActivityInterrupted): resume or discard it. When
// the Nanite conversation failed its integrity checks (`integrity: blocked`) resume is never
// offered: only discard, while there is a turn to close, and reset, which moves the chat to a
// new, empty Nanite conversation and keeps the old one on disk. The page draws the offer and
// answers it here; each answer is the owner's.
//
//	POST /api/agent/conversations/{id}/interrupted/resume
//	POST /api/agent/conversations/{id}/interrupted/discard
//	POST /api/agent/conversations/{id}/interrupted/reset    → {conversation}: the new Nanite conversation

// turnRecovery is what the routes need of the Nanite runner (*chatrunner.Runner).
type turnRecovery interface {
	Resume(ctx context.Context, chatID string) error
	Discard(ctx context.Context, chatID string) error
	ResetChat(ctx context.Context, chatID string) (string, error)
}

const (
	resetBusyMessage        = "The agent is working in this conversation. Stop it, or wait for it to finish, before resetting."
	resetInterruptedMessage = "This conversation has an interrupted turn. Discard it before resetting."
)

func (s *Service) mountInterrupted(mux *http.ServeMux) {
	if s.wire == nil || s.wire.nanite == nil {
		return
	}
	w := s.wire
	s.mountRecovery(mux, w.nanite, w.naniteChatID, w.control.busy)
}

// mountRecovery serves the routes over a runner. chatID maps the wire's conversation to the
// runner's chat (a conversation upgraded from the host runner has its own); busy reports a
// run the host started on the wire's conversation.
func (s *Service) mountRecovery(mux *http.ServeMux, runner turnRecovery,
	chatID func(context.Context, string) (string, error), busy func(string) bool) {
	mux.HandleFunc("POST "+Prefix+"/conversations/{id}/interrupted/{action}", func(rw http.ResponseWriter, r *http.Request) {
		if module.ActorOf(r.Context()) != module.ActorOwner {
			fail(rw, http.StatusForbidden, "Only the owner can answer an interrupted turn.")
			return
		}
		conversation := r.PathValue("id")
		id, err := chatID(r.Context(), conversation)
		if err != nil {
			fail(rw, http.StatusInternalServerError, err.Error())
			return
		}
		switch action := r.PathValue("action"); action {
		case "resume":
			err = runner.Resume(r.Context(), id)
		case "discard":
			err = runner.Discard(r.Context(), id)
		case "reset":
			// Refused while a run holds the conversation: the runner refuses too (ErrBusy),
			// but a run the wire has started may not have reached it yet.
			if busy(conversation) {
				fail(rw, http.StatusConflict, resetBusyMessage)
				return
			}
			var next string
			next, err = runner.ResetChat(r.Context(), id)
			if err == nil {
				s.log.Info("the conversation was reset to a new Nanite conversation; the old one is kept",
					"conversation", conversation, "nanite_conversation", next)
				reply(rw, http.StatusOK, map[string]string{"conversation": next})
				return
			}
		default:
			fail(rw, http.StatusNotFound, "No action "+action+": resume, discard or reset.")
			return
		}
		switch {
		case err == nil:
			reply(rw, http.StatusOK, map[string]string{"status": "ok"})
		case errors.Is(err, nanite.ErrBusy):
			fail(rw, http.StatusConflict, resetBusyMessage)
		case errors.Is(err, chatrunner.ErrResetInterrupted):
			fail(rw, http.StatusConflict, resetInterruptedMessage)
		case errors.Is(err, nanite.ErrIntegrity):
			fail(rw, http.StatusConflict, "This conversation failed its integrity checks, so the turn cannot be resumed. Discard it, then reset the conversation.")
		default:
			fail(rw, http.StatusConflict, err.Error())
		}
	})
}
