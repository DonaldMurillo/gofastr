package crud

import (
	"context"
	"net/http"
	"testing"
)

// A multi-tenant create with no tenant on ctx answers 400 tenant_required,
// not a server fault.
func TestTenantMissingIsA400(t *testing.T) {
	f := crudRefusal(context.Background(), &tenantMissingError{})
	if f.code != http.StatusBadRequest || f.reason != "tenant_required" {
		t.Fatalf("refusal %d %q, want 400 tenant_required", f.code, f.reason)
	}
}
