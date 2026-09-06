package crud

import (
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

// TestStreamSeatPrincipalBuckets pins the identity the seat cap counts by:
// owner id, else authenticated user, else tenant, else "" (anonymous
// callers on a Public stream share one bucket). Also covers the lazy
// seatRegistry allocation on a hand-built handler.
func TestStreamSeatPrincipalBuckets(t *testing.T) {
	ch := &CrudHandler{}
	req := httptest.NewRequest("GET", "/", nil)

	if got := ch.streamSeatPrincipal(req, "o1"); got != "owner:o1" {
		t.Errorf("owner: got %q want owner:o1", got)
	}
	uReq := req.WithContext(handler.SetUser(req.Context(), &testUser{id: "u1"}))
	if got := ch.streamSeatPrincipal(uReq, nil); got != "user:u1" {
		t.Errorf("user: got %q want user:u1", got)
	}
	tReq := req.WithContext(tenant.SetTenantID(req.Context(), "t1"))
	if got := ch.streamSeatPrincipal(tReq, nil); got != "tenant:t1" {
		t.Errorf("tenant: got %q want tenant:t1", got)
	}
	if got := ch.streamSeatPrincipal(req, nil); got != "" {
		t.Errorf("anonymous: got %q want empty bucket", got)
	}
	// An owner id wins over a user/tenant also in context.
	if got := ch.streamSeatPrincipal(uReq, "o2"); got != "owner:o2" {
		t.Errorf("owner precedence: got %q want owner:o2", got)
	}

	// seatRegistry allocates on first use for a literal handler.
	if ch.seatRegistry() == nil {
		t.Fatal("seatRegistry returned nil")
	}
	if ch.seatRegistry() != ch.seats {
		t.Fatal("seatRegistry did not memoise the allocation")
	}
}
