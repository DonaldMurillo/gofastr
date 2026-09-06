package framework

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// Pins the silent per-version row partition by divergent tenant columns,
// found by the 2026-09-04 red-probe round; fixed in registry.go by
// checkRowIsolationCompat comparing the resolved TenantColumn() whenever
// MultiTenant is set, same error family as the other row-isolation
// mismatches.
// Family: F14 multiversion schema conflicts (row-isolation divergence)
// Property: two versions of one entity name share one physical table, so they
// must scope rows through the SAME tenant column — checkRowIsolationCompat
// rejects versions that disagree on MultiTenant/OwnerField/SoftDelete/Public/
// Access/CrossOwnerRead with "versions of one entity share one table, so they
// must agree on which rows a request may see"; the tenant COLUMN is that same
// class of setting.
// Surfaces: framework/registry.go::checkRowIsolationCompat (now compares
// TenantColumn() when MultiTenant is set), framework/entity/entity.go::
// TenantColumn (per-version column choice), framework/crud tenant scoping
// (WHERE <TenantColumn()> = ? built per version).

// multiTenantCfg builds a posts config with multi-tenancy on, optionally
// through a non-default tenant column. Uses the framework package's own
// re-exported config types (no entity import needed from this test).
func multiTenantCfg(tenantField string) EntityConfig {
	scope := &ScopeConfig{MultiTenant: true, TenantField: tenantField}
	return EntityConfig{
		Table:  "posts",
		Scope:  scope,
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
	}.WithTimestamps(false)
}

// TestVersionConflict_TenantColumnDiverges: divergent tenant columns across
// versions of one entity must fail registration exactly like a MultiTenant
// mismatch does.
func TestVersionConflict_TenantColumnDiverges(t *testing.T) {
	for _, tc := range []struct {
		name, v1, v2 string
	}{
		{"default vs custom", "", "account_id"},
		{"custom vs custom", "account_id", "org_id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			msg, panicked := registerTwoVersions(t,
				multiTenantCfg(tc.v1),
				multiTenantCfg(tc.v2),
			)
			if !panicked {
				t.Fatalf("SECURITY: [multiversion] two versions of entity \"posts\" registered with DIFFERENT tenant "+
					"columns (%s vs %s) and no error — the shared table partitions its rows per version silently",
					displayTenantCol(tc.v1), displayTenantCol(tc.v2))
			}
			if !strings.Contains(msg, "tenant") {
				t.Fatalf("panic should name the diverged tenant setting:\n%s", msg)
			}
		})
	}
}

// TestVersionsSameTenantColumnAccepted: an explicit TenantField "tenant_id"
// and the implicit default are the SAME resolved column, so registering both
// must stay clean — the guard compares the resolved column, not the raw
// string, and must not over-block the ordinary case.
func TestVersionsSameTenantColumnAccepted(t *testing.T) {
	_, panicked := registerTwoVersions(t,
		multiTenantCfg(""),
		multiTenantCfg("tenant_id"),
	)
	if panicked {
		t.Fatal("SECURITY: [multiversion] two versions agreeing on the resolved tenant column tenant_id " +
			"were rejected — the guard must compare TenantColumn(), not the raw TenantField string")
	}
}

// displayTenantCol mirrors EntityConfig.TenantColumn's resolution for the
// failure message.
func displayTenantCol(tenantField string) string {
	if tenantField == "" {
		return "tenant_id"
	}
	return tenantField
}
