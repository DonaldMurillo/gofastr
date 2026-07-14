package pluginhost

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/DonaldMurillo/gofastr/battery/auth"
)

// Allow reports whether a plugin action requiring the `required` capability is
// permitted. It is the intersection of two sides and DEFAULT-DENIES:
//
//  1. Module grant — `granted` is the capability set THIS plugin mount was
//     granted by the host. It is the ceiling: a plugin can never exceed its
//     declared grants, even under a session cookie. A nil/empty `granted`
//     denies everything. Matching uses auth.ScopeMatch (the same resource:verb
//     wildcard grammar as token scopes — NOT a weaker parallel matcher), so a
//     granted "*:*" is the explicit "grant everything" (dev/trusted) form.
//  2. Caller authority — the request's own scopes must also permit it. A scoped
//     API token restricts BELOW the plugin grant; a session/JWT caller is
//     unscoped, so the plugin grant is the binding limit.
//
// This inverts the old behavior, where an unscoped session caller passed every
// capability (auth.HasScope returns true with no token): the plugin grant set
// now confines the untrusted plugin regardless of how the user authenticated.
func Allow(ctx context.Context, granted []string, required string) bool {
	if !auth.ScopeMatch(granted, required) {
		return false // default-deny: outside the plugin's grant set
	}
	return auth.HasScope(ctx, required) // caller-authority side
}

// Guard is the enforcement chokepoint a plugin's privileged RPC/route mounts so
// a forgotten check fails CLOSED. It runs next only when Allow permits
// `required` for the `granted` set; otherwise it writes 403 with the
// E_CAPABILITY_DENIED code and does not call next.
func Guard(granted []string, required string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !Allow(r.Context(), granted, required) {
			WriteCapabilityDenied(w, required)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// WriteCapabilityDenied writes the canonical 403 capability-denial response
// (JSON body carrying the E_CAPABILITY_DENIED code and the offending
// capability) so every plugin route denies uniformly.
func WriteCapabilityDenied(w http.ResponseWriter, capability string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":      "E_CAPABILITY_DENIED",
		"capability": capability,
	})
}
