package crud

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func TestMCPWriteOmitsScopeFields(t *testing.T) {
	config := makeEntityConfig("items", "items", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "tenant_id", Type: schema.String, Required: true},
		{Name: "name", Type: schema.String, Required: true},
	})
	config.Scope.MultiTenant = true
	ent := entity.Define("items", config)
	for _, update := range []bool{false, true} {
		props := bodyToolSchema(ent, update)["properties"].(map[string]any)
		for _, name := range []string{"user_id", "tenant_id"} {
			if _, exists := props[name]; exists {
				t.Errorf("update=%v: server field %s is writable", update, name)
			}
		}
		if _, exists := props["name"]; !exists {
			t.Error("ordinary writable field omitted")
		}
	}
}
