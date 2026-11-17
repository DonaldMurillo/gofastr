package pluginhost

import (
	"context"
	"encoding/json"
	"net/http"

	//gofastr:allow(GOFASTR1301) known upward edge, kept deliberately for now. Allow needs both halves of the scope check: ScopeMatch (whose algebra lives in framework/access, reachable downward) and HasScope, which reads the caller's token out of the request context and is genuinely battery/auth's. Splitting them means inverting HasScope through an interface defined in pluginhost — a real change to a security chokepoint, not a rename. Tracked rather than hidden: this is the only violation in the tree.
	"github.com/DonaldMurillo/gofastr/battery/auth"
)

// Allow reports whether both granted and the caller's scopes allow required.
func Allow(ctx context.Context, granted []string, required string) bool {
	if !auth.ScopeMatch(granted, required) {
		return false
	}
	return auth.HasScope(ctx, required)
}

// Guard runs next only when [Allow] permits, else [WriteCapabilityDenied].
func Guard(granted []string, required string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !Allow(r.Context(), granted, required) {
			WriteCapabilityDenied(w, required)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// WriteCapabilityDenied writes a 403 E_CAPABILITY_DENIED JSON body.
func WriteCapabilityDenied(w http.ResponseWriter, capability string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":      "E_CAPABILITY_DENIED",
		"capability": capability,
	})
}
