package framework

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// checkRowIsolationCompat stops two versions of one entity disagreeing on
// MultiTenant / OwnerField / SoftDelete, on the principle that letting them
// differ makes the weaker version a bypass of the stronger one over the SHARED
// PHYSICAL TABLE.
//
// EntityConfig.Access and EntityConfig.Public are enforced per-version by the
// very same CRUD handlers (framework/crud/owner.go permissionForOp reads
// ch.Entity.Config.Access; requireAuthenticated reads Config.Public), against
// the very same table, and they are NOT compared. A v2 declaring blank
// Access, or Public: true, is a straight authz/authn bypass of v1 reached by
// changing one path prefix.
//
// Note Public's own doc: "Has no effect when OwnerField or Access is set",
// so the Public case bites exactly when v1's only protection is the
// framework's secure-by-default session requirement, which is the default
// posture for every entity that does not opt into owner scoping.
func TestVersionsMustAgreeOnAccess(t *testing.T) {
	titleOnly := []schema.Field{{Name: "title", Type: schema.String}}

	tests := []struct {
		name string
		v1   entity.EntityConfig
		v2   entity.EntityConfig
		want string // substring the rejection SHOULD name
	}{
		{
			name: "RBAC-gated beside un-gated",
			v1:   entity.EntityConfig{Fields: titleOnly, Exposure: &entity.ExposureConfig{Access: entity.AccessControl{Read: "records:read"}}},
			v2:   entity.EntityConfig{Fields: titleOnly},
			want: "access",
		},
		{
			name: "session-required beside Public",
			v1:   entity.EntityConfig{Fields: titleOnly},
			v2:   entity.EntityConfig{Fields: titleOnly, Exposure: &entity.ExposureConfig{Public: true}},
			want: "public",
		},
		{
			name: "owner-scoped beside cross-owner-read",
			v1:   entity.EntityConfig{Fields: []schema.Field{{Name: "title", Type: schema.String}, {Name: "owner_id", Type: schema.String, Hidden: true}}, Scope: &entity.ScopeConfig{OwnerField: "owner_id"}},
			v2:   entity.EntityConfig{Fields: []schema.Field{{Name: "title", Type: schema.String}, {Name: "owner_id", Type: schema.String, Hidden: true}}, Scope: &entity.ScopeConfig{OwnerField: "owner_id", CrossOwnerRead: "records:read_all"}},
			want: "cross",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reg := NewRegistry()
			v1 := entity.Define("records", tc.v1.WithTimestamps(false))
			v1.Version = "/api/v1"
			v2 := entity.Define("records", tc.v2.WithTimestamps(false))
			v2.Version = "/api/v2"
			if err := reg.Register(v1); err != nil {
				t.Fatalf("register v1: %v", err)
			}
			err := reg.Register(v2)
			if err == nil {
				t.Fatalf("registry accepted two versions of %q that disagree on access posture — "+
					"v2 (%s) is a bypass of v1 (%s) over the same table %q",
					v1.Config.Name, v2.Version, v1.Version, v1.GetTable())
			}
			if !strings.Contains(strings.ToLower(err.Error()), tc.want) {
				t.Errorf("rejection should name %q:\n%s", tc.want, err)
			}
		})
	}
}
