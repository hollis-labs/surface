// Package genui is Surface's host for generated UI: the reviewed GenUI contract and
// gateway from Substrate, given this app's storage, policy and effects.
//
// What generated UI is here: the agent (or a module) names one of three reviewed patterns
// (an approval, a form, a list with detail) and its parameters. It cannot send components,
// markup, code or addresses. The host turns the parameters into a validated surface, keeps
// it as an immutable snapshot, and shows it as a card. When the owner acts on it, the action
// goes through the gateway: checked against the snapshot, journaled, admitted by policy,
// executed once.
//
// Surface is one person's app on the loopback interface, so there is one scope, the owner's.
// The only effect an action has is to answer whoever showed the surface; the answer is the
// choice taken and, for a form, the values entered. A high-risk choice needs the owner to
// confirm it as a second, separate step. That is an acknowledgment, not reauthentication.
package genui

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	contract "github.com/hollis-labs/substrate/genui"
	"github.com/hollis-labs/substrate/genui/gateway"

	"github.com/hollis-labs/surface/core/internal/kernel"
	"github.com/hollis-labs/surface/core/internal/storage"
	"github.com/hollis-labs/surface/module"
)

//go:embed migrations/*.sql
var migrations embed.FS

// The one scope: this app, its owner.
var owner = gateway.Scope{Namespace: "surface", Actor: "owner"}

const (
	opAnswer = "answer"
	// CardKind is the kind of the card a surface is shown as.
	CardKind = "genui"
)

// Service is the host: the gateway's ports and the routes and tool on top of it.
type Service struct {
	k     kernel.Kernel
	cards module.Cards
	repo  *repository
	gw    *gateway.Service
	// fence serializes admission: one action is decided and executed at a time.
	fence sync.Mutex
}

// New builds the host on the core database.
func New(ctx context.Context, k kernel.Kernel, cards module.Cards) (*Service, error) {
	if err := storage.Migrate(ctx, k.DB(), "genui", migrations, "migrations"); err != nil {
		return nil, err
	}
	s := &Service{k: k, cards: cards, repo: &repository{db: k.DB()}}
	gw, err := gateway.New(gateway.Ports{Catalog: s, Surfaces: s.repo, Journal: s.repo, Policy: s, Executor: s})
	if err != nil {
		return nil, err
	}
	s.gw = gw
	return s, nil
}

// Close has nothing to release.
func (s *Service) Close(context.Context) error { return nil }

func randomID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

// The parameters of each pattern, as whoever shows a surface gives them.
type (
	choiceParam struct {
		ID    string        `json:"id"`
		Label string        `json:"label"`
		Risk  contract.Risk `json:"risk,omitempty"`
	}
	approvalParams struct {
		Title   string            `json:"title"`
		Summary string            `json:"summary,omitempty"`
		Subject string            `json:"subject,omitempty"`
		Changes []contract.Change `json:"changes,omitempty"`
		Choices []choiceParam     `json:"choices"`
	}
	formParams struct {
		Title       string               `json:"title"`
		Subject     string               `json:"subject,omitempty"`
		Fields      []contract.FormField `json:"fields"`
		SubmitLabel string               `json:"submit_label,omitempty"`
		Draft       map[string]any       `json:"draft,omitempty"`
	}
	listParams struct {
		Title    string                     `json:"title"`
		Items    []contract.ListItem        `json:"items"`
		Details  map[string]contract.Detail `json:"details,omitempty"`
		Selected string                     `json:"selected,omitempty"`
	}
)

func strict(raw []byte, v any) error {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// choice builds one offered choice. Every choice of a surface shares the action id, so
// only one of them can ever be taken.
func choice(label, id, action, subject string, risk contract.Risk) contract.Choice {
	if risk == "" {
		risk = contract.RiskLow
	}
	return contract.Choice{Label: label, CallFunction: contract.FunctionCall{Name: contract.FunctionName, CatalogID: contract.CatalogID,
		Args: contract.ActionArgs{
			Action: contract.Action{ID: action, Subject: contract.Subject{Kind: "surface.answer", ID: subject}, Basis: contract.Basis{Revision: "1"}, Risk: risk},
			Input:  map[string]any{"choice": id},
		}}}
}

// Materialize turns a pattern's parameters into a surface. It is the gateway's catalog.
func (s *Service) Materialize(_ context.Context, scope gateway.Scope, q gateway.ShowRequest) (gateway.Surface, error) {
	if scope != owner {
		return gateway.Surface{}, gateway.ErrForbidden
	}
	id, action := randomID(), randomID()
	var (
		props any
		title string
		data  contract.DataModel
	)
	switch q.Pattern {
	case contract.PatternApproval:
		var p approvalParams
		if err := strict(q.Params, &p); err != nil || p.Title == "" || len(p.Choices) == 0 || len(p.Choices) > 4 {
			return gateway.Surface{}, fmt.Errorf("an approval needs a title and one to four choices: %w", gateway.ErrInvalid)
		}
		subject := p.Subject
		if subject == "" {
			subject = id
		}
		out := contract.ApprovalProps{Title: p.Title, Summary: p.Summary, Changes: p.Changes, State: contract.ViewReady}
		if out.Changes == nil {
			out.Changes = []contract.Change{}
		}
		seen := map[string]bool{}
		for _, c := range p.Choices {
			if c.ID == "" || c.Label == "" || seen[c.ID] {
				return gateway.Surface{}, gateway.ErrInvalid
			}
			seen[c.ID] = true
			out.Choices = append(out.Choices, choice(c.Label, c.ID, action, subject, c.Risk))
		}
		props, title = out, p.Title
	case contract.PatternForm:
		var p formParams
		if err := strict(q.Params, &p); err != nil || p.Title == "" || len(p.Fields) == 0 {
			return gateway.Surface{}, fmt.Errorf("a form needs a title and fields: %w", gateway.ErrInvalid)
		}
		subject := p.Subject
		if subject == "" {
			subject = id
		}
		label := p.SubmitLabel
		if label == "" {
			label = "Submit"
		}
		props, title = contract.FormProps{Title: p.Title, Fields: p.Fields, Submit: choice(label, "submit", action, subject, contract.RiskLow), State: contract.ViewReady}, p.Title
		data.Draft = p.Draft
	case contract.PatternListDetail:
		var p listParams
		if err := strict(q.Params, &p); err != nil || p.Title == "" {
			return gateway.Surface{}, fmt.Errorf("a list needs a title: %w", gateway.ErrInvalid)
		}
		out := contract.ListDetailProps{Title: p.Title, Items: p.Items, State: contract.ViewReady}
		if out.Items == nil {
			out.Items = []contract.ListItem{}
		}
		if len(out.Items) == 0 {
			out.State = contract.ViewEmpty
		}
		if d, ok := p.Details[p.Selected]; ok && p.Selected != "" {
			out.SelectedID, out.Detail = p.Selected, &d
		}
		props, title = out, p.Title
	default:
		return gateway.Surface{}, gateway.ErrInvalid
	}
	env, err := contract.NewSurface(id, q.Pattern, props, data)
	if err != nil {
		return gateway.Surface{}, err
	}
	env.Title = title
	out := gateway.Surface{Scope: scope, Envelope: env, Generation: randomID(), ExpiresAt: time.Now().Add(time.Hour)}
	choices, err := contract.Choices(env)
	if err != nil {
		return gateway.Surface{}, err
	}
	for _, c := range choices {
		d, err := gateway.ChoiceDigest(c.CallFunction)
		if err != nil {
			return gateway.Surface{}, err
		}
		out.Operations = append(out.Operations, gateway.OperationBinding{ChoiceDigest: d, Operation: opAnswer})
	}
	return out, nil
}

// challenge is what the journal keeps of a confirmation that was asked for.
type challenge struct {
	Digest, Generation, ProofHash string
	ExpiresAt                     time.Time
}

func proofHash(s string) string { v := sha256.Sum256([]byte(s)); return hex.EncodeToString(v[:]) }

func refuse(reason string) gateway.Decision {
	return gateway.Decision{Outcome: &contract.Outcome{Status: contract.StatusRefused, Reason: reason}}
}

// WithFence is the gateway's policy. It re-reads the retained surface and decides: act,
// refuse, lapse, or ask the owner to confirm a high-risk choice first.
func (s *Service) WithFence(ctx context.Context, r gateway.Request, fn func(context.Context, gateway.Decision) error) error {
	s.fence.Lock()
	defer s.fence.Unlock()
	if r.Scope != owner {
		return fn(ctx, refuse("absent-scope"))
	}
	retained, err := s.repo.Get(ctx, r.Scope, r.Surface.Envelope.ID)
	if err != nil || retained.Generation != r.Surface.Generation {
		return gateway.ErrUnavailable
	}
	if retained.Superseded {
		return fn(ctx, gateway.Decision{Outcome: &contract.Outcome{Status: contract.StatusSuperseded}})
	}
	if !time.Now().Before(retained.ExpiresAt) {
		return fn(ctx, gateway.Decision{Outcome: &contract.Outcome{Status: contract.StatusExpired}})
	}
	if r.Operation != opAnswer {
		return fn(ctx, refuse("unregistered-route"))
	}
	action := r.Call.CallAgentFunction.CallFunction.Args.Action
	deadline := retained.ExpiresAt
	// The risk that counts is the one the host put on the offered choice, which the gateway
	// has already matched this call against.
	if action.Risk == contract.RiskHigh {
		var ch challenge
		if len(r.Previous.Challenge) == 0 {
			ch = challenge{Digest: r.Digest, Generation: retained.Generation, ExpiresAt: time.Now().Add(2 * time.Minute)}
			if deadline.Before(ch.ExpiresAt) {
				ch.ExpiresAt = deadline
			}
		} else if json.Unmarshal(r.Previous.Challenge, &ch) != nil {
			return gateway.ErrUnavailable
		}
		if ch.Digest != r.Digest || ch.Generation != retained.Generation || !time.Now().Before(ch.ExpiresAt) {
			return fn(ctx, gateway.Decision{Outcome: &contract.Outcome{Status: contract.StatusExpired}})
		}
		if action.Confirmation == "" {
			raw, _ := json.Marshal(ch)
			return fn(ctx, gateway.Decision{Outcome: &contract.Outcome{Status: contract.StatusNeedsConfirmation, Confirm: &contract.Confirmation{
				What:  "This is marked high risk. Confirm it once more to go ahead. This is an acknowledgment, not a sign-in.",
				Scope: []string{opAnswer}, TTLMS: time.Until(ch.ExpiresAt).Milliseconds(), Who: "you",
			}}, Challenge: raw})
		}
		if ch.ProofHash == "" || proofHash(action.Confirmation) != ch.ProofHash {
			return fn(ctx, refuse("denied"))
		}
		deadline = ch.ExpiresAt
	}
	bounded, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	return fn(bounded, gateway.Decision{Authority: &gateway.Authority{Generation: retained.Generation, Deadline: deadline, Risk: action.Risk}})
}

// Confirm is the owner's second step on a high-risk choice: it issues the proof the
// waiting action needs. The page calls it when the owner presses Confirm.
func (s *Service) Confirm(ctx context.Context, raw []byte) (string, error) {
	call, err := contract.DecodeCall(raw)
	if err != nil || call.CallAgentFunction.CallFunction.Args.Action.Confirmation != "" {
		return "", gateway.ErrInvalid
	}
	s.fence.Lock()
	defer s.fence.Unlock()
	key := gateway.Key{Scope: owner, ActionID: call.CallAgentFunction.CallFunction.Args.Action.ID}
	rec, err := s.repo.Load(ctx, key)
	if err != nil || rec.Phase != gateway.PhaseWaiting || rec.Claimed {
		return "", gateway.ErrForbidden
	}
	now, err := gateway.ChoiceDigest(call.CallAgentFunction.CallFunction)
	was, wasErr := gateway.ChoiceDigest(rec.Request.CallAgentFunction.CallFunction)
	if err != nil || wasErr != nil || now != was || call.CallAgentFunction.SurfaceID != rec.Request.CallAgentFunction.SurfaceID {
		return "", gateway.ErrForbidden
	}
	surface, err := s.repo.Get(ctx, owner, call.CallAgentFunction.SurfaceID)
	if err != nil || surface.Superseded || !time.Now().Before(surface.ExpiresAt) {
		return "", gateway.ErrForbidden
	}
	var ch challenge
	if json.Unmarshal(rec.Challenge, &ch) != nil || ch.Digest != rec.Digest || ch.Generation != surface.Generation || !time.Now().Before(ch.ExpiresAt) {
		return "", gateway.ErrForbidden
	}
	proof := randomID()
	ch.ProofHash = proofHash(proof)
	rec.Challenge, _ = json.Marshal(ch)
	if _, swapped, err := s.repo.CompareAndSwap(ctx, key, rec.Revision, rec); err != nil || !swapped {
		return "", gateway.ErrForbidden
	}
	return proof, nil
}

// Execute is the one effect: the answer reaches whoever showed the surface, by closing the
// card it was shown as. The gateway has journaled a claim first, so this runs once.
func (s *Service) Execute(ctx context.Context, e gateway.Execution) (contract.Outcome, error) {
	if ctx.Err() != nil || !time.Now().Before(e.Authority.Deadline) {
		return contract.Outcome{}, gateway.ErrUnavailable
	}
	input := e.Request.Call.CallAgentFunction.CallFunction.Args.Input
	result, err := json.Marshal(input)
	if err != nil {
		return contract.Outcome{}, err
	}
	if card, ok := s.cardOf(ctx, e.Request.Surface.Envelope.ID); ok {
		// The card may have lapsed or been closed by its sender meanwhile; the action is
		// recorded either way, and the receipt says which.
		_, _ = s.cards.Respond(context.WithoutCancel(ctx), card.ID, module.CardOutcome{Status: "submitted", Data: input})
	}
	return contract.Outcome{Status: contract.StatusOK, Result: result, Receipt: "answered"}, nil
}

func subjectOf(surfaceID string) string { return "genui:" + surfaceID }

// cardOf finds the card a surface was shown as.
func (s *Service) cardOf(ctx context.Context, surfaceID string) (module.Card, bool) {
	all, err := s.cards.List(ctx, true)
	if err != nil {
		return module.Card{}, false
	}
	for _, c := range all {
		if c.Kind == CardKind && c.Subject == subjectOf(surfaceID) {
			return c, true
		}
	}
	return module.Card{}, false
}

// ShowRequest is what the agent, a module or an outside client gives to show a surface.
type ShowRequest struct {
	Pattern contract.Pattern `json:"pattern"`
	Params  json.RawMessage  `json:"params"`
	// Source is who shows it: "agent", or a module's id.
	Source string `json:"-"`
	// Blocking says the sender waits on the answer.
	Blocking bool `json:"-"`
}

// Show validates a pattern's parameters into a surface, keeps it, and puts it in front of
// the owner as a card.
func (s *Service) Show(ctx context.Context, q ShowRequest) (module.Card, contract.Envelope, error) {
	env, err := s.gw.Show(ctx, owner, gateway.ShowRequest{Pattern: q.Pattern, Params: q.Params})
	if err != nil {
		return module.Card{}, env, err
	}
	return s.publish(ctx, env, q)
}

func (s *Service) publish(ctx context.Context, env contract.Envelope, q ShowRequest) (module.Card, contract.Envelope, error) {
	source, blocking := q.Source, q.Blocking
	body := map[string]any{"envelope": env}
	if q.Pattern == contract.PatternListDetail {
		// A list keeps its parameters, so a click on an item can ask for that item's detail.
		body["params"] = q.Params
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return module.Card{}, env, err
	}
	retained, err := s.repo.Get(ctx, owner, env.ID)
	if err != nil {
		return module.Card{}, env, err
	}
	card, err := s.cards.Show(ctx, module.Card{Kind: CardKind, Title: env.Title, Source: source, Subject: subjectOf(env.ID),
		Blocking: blocking, Payload: payload, ExpiresAt: retained.ExpiresAt.UTC().Format(time.RFC3339)})
	return card, env, err
}

// Select answers a click on a list item: the host makes a new surface with that item's
// detail, which replaces the one shown. The page never invents the detail.
func (s *Service) Select(ctx context.Context, surfaceID, itemID string) (contract.Envelope, error) {
	card, ok := s.cardOf(ctx, surfaceID)
	if !ok || card.Status != "" {
		return contract.Envelope{}, gateway.ErrNotFound
	}
	old, err := s.repo.Get(ctx, owner, surfaceID)
	if err != nil {
		return contract.Envelope{}, err
	}
	var kept struct {
		Params listParams `json:"params"`
	}
	if err = json.Unmarshal(card.Payload, &kept); err != nil {
		return contract.Envelope{}, gateway.ErrInvalid
	}
	kept.Params.Selected = itemID
	raw, err := json.Marshal(kept.Params)
	if err != nil {
		return contract.Envelope{}, err
	}
	env, err := s.gw.Show(ctx, owner, gateway.ShowRequest{Pattern: contract.PatternListDetail, Params: raw})
	if err != nil {
		return env, err
	}
	if err = s.repo.Supersede(ctx, owner, surfaceID, old.Generation); err != nil && !errors.Is(err, gateway.ErrNotFound) {
		return env, err
	}
	// The new surface takes the old one's place: the old card closes as replaced.
	if _, err = s.cards.End(ctx, card.ID, "superseded"); err != nil {
		return env, err
	}
	_, _, err = s.publish(ctx, env, ShowRequest{Pattern: contract.PatternListDetail, Params: raw, Source: card.Source})
	return env, err
}
