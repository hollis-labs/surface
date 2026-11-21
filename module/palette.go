package module

import "context"

// PaletteProvider contributes read-only, asynchronous search to the command palette.
// Search must honor ctx cancellation, read live settings and return at most q.Limit rows.
// It must never change state: only selecting a result runs its action.
type PaletteProvider struct {
	ID     string                                                             `json:"id"` // local to the module, same syntax as a module id
	Title  string                                                             `json:"title"`
	Mode   string                                                             `json:"mode,omitempty"` // commands (default) or tabs
	Search func(context.Context, Host, PaletteQuery) ([]PaletteResult, error) `json:"-"`
}

type PaletteQuery struct {
	Text          string
	Limit         int
	Scope         string // all or focused (tabs only)
	FocusedWindow string // supplied by the core, not the caller
}

// PaletteRef is an identity in Cite, resolved/opened through its existing resolver.
type PaletteRef struct {
	Kind      string `json:"kind"`
	Canonical string `json:"canonical"`
}

// PaletteResult has one action: Ref, Command, or Window. IDs are stable within a provider.
// Window results focus an existing desk window; Close offers its normal close action.
type PaletteResult struct {
	ID          string      `json:"id"`
	Title       string      `json:"title"`
	Description string      `json:"description,omitempty"`
	Icon        string      `json:"icon,omitempty"`
	Ref         *PaletteRef `json:"ref,omitempty"`
	Command     *RefOpen    `json:"command,omitempty"`
	Window      string      `json:"window,omitempty"`
	Close       bool        `json:"close,omitempty"`
}
