package openapi

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func TestPublicScopedEntityStillDeclares401(t *testing.T) {
	for name, cfg := range map[string]entity.EntityConfig{
		"owned":  {Scope: &entity.ScopeConfig{OwnerField: "owner_id"}, Exposure: &entity.ExposureConfig{Public: true}},
		"tenant": {Scope: &entity.ScopeConfig{MultiTenant: true}, Exposure: &entity.ExposureConfig{Public: true}},
		"rbac":   {Exposure: &entity.ExposureConfig{Public: true, Access: entity.AccessControl{Read: "rbac:read"}}},
	} {
		cfg.Table = name
		cfg.Fields = []schema.Field{{Name: "owner_id", Type: schema.String}, {Name: "amount", Type: schema.Int}}
		doc := EntityOpenAPI(reg(entity.Define(name, cfg)), "Test", "1.0.0", nil).Build()
		for label, op := range gatedOpsFor(t, doc, name) {
			if !hasResponse(op, "401") {
				t.Errorf("%s: Public but scoped op %q must declare 401", name, label)
			}
		}
	}
}
