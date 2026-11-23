package module

import "context"

// Readier is a module that says whether it is fit to do its work, for the app's GET /readyz
// (the readiness probe; /healthz says only that the process answers). Optional: a module
// without it counts as ready once started.
type Readier interface {
	// Ready reports now. It is called on each probe, with a deadline of a few seconds, and
	// should answer from what the module already knows rather than do slow work.
	Ready(ctx context.Context) Readiness
}

// Readiness is a module's answer.
type Readiness struct {
	Status string `json:"status"` // ReadyOK, ReadyDegraded or ReadyDown
	// Reasons say why it is not ok. They are for the module's own detailed report: GET /readyz
	// is public and answers only the status.
	Reasons []string `json:"reasons,omitempty"`
}

// Readiness statuses, least to most serious. /readyz answers 503 for down and 200 otherwise.
const (
	ReadyOK       = "ok"
	ReadyDegraded = "degraded"
	ReadyDown     = "down"
)
