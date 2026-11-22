package module

import (
	"context"
	"net/http"
	"slices"
	"strings"
)

// User is who signed in, for an app whose surface.yaml sets auth: identity. The core puts
// it on the context of every request it lets through (UserOf); with auth off there is none.
// A module reads it to attribute records (created_by) and to decide what the person may do
// beyond the app-wide rule (reads need "<app>.read", writes "<app>.use"): an admin-only
// action checks HasScope("<app>.admin").
type User struct {
	// ID is the account id ("usr_…"), or the API key id for a machine caller.
	ID string `json:"id"`
	// Kind is "user" for a person and "service" for an API key or a token minted from one.
	Kind        string `json:"kind"`
	Username    string `json:"username,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	// Role is the person's role (owner, admin, member, guest), "" for a machine caller.
	Role string `json:"role,omitempty"`
	// Scopes are the effective scopes: the account's own and its role's.
	Scopes []string `json:"scopes"`
	// For is, for an API key a person made (the account menu's API keys), that person
	// ("user:<id>"): attribute the key's work to them. "" for a person and for other keys.
	For string `json:"for,omitempty"`
}

// HasScope reports whether the user holds want: exactly, through "*", or through
// "<app>.*" for "<app>.<anything>".
func (u User) HasScope(want string) bool {
	for _, s := range u.Scopes {
		if s == want || s == "*" {
			return true
		}
		if pre, ok := strings.CutSuffix(s, ".*"); ok && strings.HasPrefix(want, pre+".") {
			return true
		}
	}
	return false
}

// Person reports whether the caller is a signed-in person rather than a machine.
func (u User) Person() bool { return u.Kind == "user" }

type userKey struct{}

// WithUser returns a context carrying the signed-in user. The core sets it; a module may set
// it for work it does on a user's behalf outside a request.
func WithUser(ctx context.Context, u User) context.Context {
	u.Scopes = slices.Clone(u.Scopes)
	return context.WithValue(ctx, userKey{}, u)
}

// UserOf returns the signed-in user, and false when there is none (auth off, or work the
// module started itself).
func UserOf(ctx context.Context) (User, bool) {
	u, ok := ctx.Value(userKey{}).(User)
	return u, ok
}

// Public marks a module route that is served without a signed-in caller, for an app with
// sign-in: the module authenticates it itself (a device token, a single-use code) or it is
// meant for anyone who can reach the app (an install script). Use it sparingly:
//
//	h.Handle("POST /enroll", module.Public(http.HandlerFunc(enroll)))
//
// With auth off it changes nothing.
func Public(h http.Handler) http.Handler { return publicHandler{h} }

// IsPublic reports whether h was marked with Public.
func IsPublic(h http.Handler) bool {
	_, ok := h.(publicHandler)
	return ok
}

type publicHandler struct{ http.Handler }
