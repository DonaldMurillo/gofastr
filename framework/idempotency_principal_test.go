package framework

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/owner"
)

// The idempotency principal keys cache entries per user. An
// unauthenticated request shares the "anon" namespace deliberately: an
// anonymous response carries no per-user data, and refusing to cache it
// would drop retry protection exactly where duplicate submits happen.
func TestOwnerPrincipalKeysPerUser(t *testing.T) {
	// The extractor is process-global. Restoring the PREVIOUS one, not
	// nil, is the difference between a self-contained test and one that
	// silently unauthenticates whatever runs after it — the same trap
	// framework/crud's installSecurityOwnerExtractor documents.
	prev := owner.GetExtractor()
	owner.SetExtractor(func(ctx context.Context) (any, bool) {
		v, ok := ctx.Value(principalTestKey{}).(string)
		return v, ok
	})
	t.Cleanup(func() { owner.SetExtractor(prev) })

	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	if got := ownerPrincipal(req); got != "anon" {
		t.Errorf("unauthenticated principal = %q, want %q", got, "anon")
	}

	authed := req.WithContext(context.WithValue(req.Context(), principalTestKey{}, "u-42"))
	if got := ownerPrincipal(authed); got != "u-42" {
		t.Errorf("authenticated principal = %q, want the owner id", got)
	}
}

type principalTestKey struct{}
