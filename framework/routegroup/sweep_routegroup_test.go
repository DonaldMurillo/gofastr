package routegroup_test

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/routegroup"
)

func TestNestedPrefixComposesEveryEnclosingGroup(t *testing.T) {
	inner := routegroup.New(router.New(), "/api").Group("/v2")
	if got := inner.Prefix(); got != "/api/v2" {
		t.Fatalf("nested Prefix() = %q, want /api/v2", got)
	}
}
