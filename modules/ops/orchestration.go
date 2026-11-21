package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

const orchestratorAddress = "msg://agent/agent-mux/agt_hlg0h0e50u"

// OrchestrationRequest also retains a pending delivery's exact envelope, so retries
// after a lost response or restart use the same sender, payload and idempotency key.
type OrchestrationRequest struct {
	At        string `json:"at" yaml:"at"`
	Note      string `json:"note" yaml:"note"`
	MessageID string `json:"message_id" yaml:"message_id"`
	RequestID string `json:"request_id" yaml:"request_id"`
	Sender    string `json:"-" yaml:"sender,omitempty"`
	Payload   string `json:"-" yaml:"payload,omitempty"`
}

type sendInput struct {
	BaseRev   int    `json:"base_rev"`
	RequestID string `json:"request_id"`
	Note      string `json:"note"`
}

func (m *mod) sendOrchestration(ctx context.Context, id string, in sendInput) (Workstream, error) {
	m.sendMu.Lock()
	defer m.sendMu.Unlock()
	if !ValidID(in.RequestID) || len(in.RequestID) > 100 {
		return Workstream{}, bad("give a stable request_id (a slug or UUID) for this send")
	}
	if len(in.Note) > 4096 {
		return Workstream{}, bad("note must be at most 4096 bytes")
	}
	if err := m.sync(ctx, true); err != nil {
		return Workstream{}, err
	}
	w, ok, err := m.st.Get(ctx, id)
	if err != nil || !ok {
		return Workstream{}, errOr(err, ErrNotFound)
	}
	req := w.OrchestrationRequest
	if req != nil && req.RequestID == in.RequestID {
		if req.Note != in.Note {
			return Workstream{}, bad("request_id already belongs to a different note")
		}
		if req.MessageID != "" {
			return w, nil
		}
	} else {
		if req != nil && req.MessageID == "" {
			return Workstream{}, bad("retry the pending orchestration request before sending another")
		}
		sender := strings.TrimSpace(m.h.Config().String("sender_address"))
		if !strings.HasPrefix(sender, "msg://") {
			return Workstream{}, bad("set ops.sender_address to Surface's Tether identity in Settings")
		}
		if in.BaseRev <= 0 || w.Rev != in.BaseRev {
			return Workstream{}, &Conflict{Rev: w.Rev}
		}
		if w.SettledAt != "" || (w.Stage != "planned" && w.Stage != "parked" && w.Stage != "queued") {
			return Workstream{}, bad("only planned, parked or queued workstreams can be sent to orchestration")
		}
		req = &OrchestrationRequest{At: now(), Note: in.Note, RequestID: in.RequestID, Sender: sender}
		links := make([]map[string]string, 0, len(w.Links))
		registryID := ""
		for _, l := range w.Links {
			links = append(links, map[string]string{"kind": l.Kind, "id": l.ID})
			if l.Kind == "registry.record" && strings.HasPrefix(l.ID, "workstream:") {
				registryID = l.ID
			}
		}
		payload := map[string]any{"kind": "request", "to": "@claude-code", "re": Ref(id), "attention": "agent", "awaiting": "@claude-code", "subject": "Sequence and launch: " + w.Title, "body": "The owner asks you to sequence and launch this workstream by current priorities, or your best judgment if none are set, and queue it for execution.", "intent": "sequence_and_launch", "schema_version": 1, "request_id": req.RequestID, "workstream_id": id, "title": w.Title, "stage": w.Stage, "links": links, "note": in.Note, "cite": Ref(id), "requested_by": actor(ctx), "at": req.At}
		if registryID != "" {
			payload["registry_id"] = registryID
		}
		raw, e := json.Marshal(payload)
		if e != nil {
			return Workstream{}, e
		}
		req.Payload = string(raw)
		_, err = m.st.change(ctx, id, in.BaseRev, func(d *Doc, current Workstream) error {
			if current.Rev != in.BaseRev {
				return &Conflict{Rev: current.Rev}
			}
			return d.Set("orchestration_request", req)
		})
		if err != nil {
			return Workstream{}, err
		}
	}
	if req.Payload == "" || req.Sender == "" {
		return Workstream{}, bad("pending request has no delivery envelope")
	}
	link, err := m.tether()
	if err != nil {
		return Workstream{}, err
	}
	raw, err := link.Call(ctx, "tether_message_send", map[string]any{"from": req.Sender, "to": orchestratorAddress, "kind": "request", "idempotency_key": "ops:" + id + ":" + req.RequestID, "payload_json": req.Payload})
	if err != nil {
		return Workstream{}, fmt.Errorf("orchestration request pending; retry the same request: %w", err)
	}
	var out struct {
		Message struct {
			ID string `json:"id"`
		} `json:"message"`
	}
	if err = json.Unmarshal([]byte(raw), &out); err != nil {
		return Workstream{}, err
	}
	if out.Message.ID == "" {
		return Workstream{}, fmt.Errorf("Tether returned no message receipt; retry the same request")
	}
	req.MessageID = out.Message.ID
	got, err := m.st.change(ctx, id, 0, func(d *Doc, current Workstream) error {
		if current.OrchestrationRequest == nil || current.OrchestrationRequest.RequestID != req.RequestID {
			return bad("orchestration request changed while sending; message %s was delivered", req.MessageID)
		}
		if current.OrchestrationRequest.MessageID != "" {
			return nil
		}
		if e := d.Set("orchestration_request", req); e != nil {
			return e
		}
		return addTransition(d, Transition{From: current.Stage, To: current.Stage, At: req.At, By: actor(ctx), Why: "sent to orchestration: " + req.MessageID})
	})
	if err != nil {
		return Workstream{}, fmt.Errorf("message %s delivered; retry this request to record its receipt: %w", req.MessageID, err)
	}
	m.actionChanged(ctx, id, "sent to orchestration")
	return got, nil
}

func (m *mod) apiSendOrchestration(w http.ResponseWriter, r *http.Request) {
	var in sendInput
	if !decode(w, r, &in, 8192) {
		return
	}
	got, err := m.sendOrchestration(r.Context(), r.PathValue("id"), in)
	if err != nil {
		failErr(w, err)
		return
	}
	reply(w, http.StatusOK, got)
}
