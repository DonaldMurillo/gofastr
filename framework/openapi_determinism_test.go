package framework

import (
	"encoding/json"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/openapi"
)

// TestOpenAPI_DeterministicAcrossBuilds pins that two identical registry
// constructions produce byte-identical /openapi.json output. Without
// sorted iteration, Go's randomised map walk yields a different ordering
// of tags / paths each run — that breaks ETag caching for clients, and
// breaks golden-file diffs for codegen consumers.
func TestOpenAPI_DeterministicAcrossBuilds(t *testing.T) {
	buildReg := func() *Registry {
		reg := NewRegistry()
		// Use a handful of names so map randomisation is overwhelmingly
		// likely to surface order divergence across runs.
		names := []string{"foxtrot", "alpha", "echo", "delta", "charlie", "bravo", "golf"}
		for _, n := range names {
			ent := entity.Define(n, entity.EntityConfig{
				Fields: []schema.Field{{Name: "name", Type: schema.String, Required: true}},
			})
			if err := reg.Register(ent); err != nil {
				t.Fatal(err)
			}
		}
		return reg
	}

	render := func(reg *Registry) []byte {
		spec := openapi.EntityOpenAPI(reg, "Test", "1.0.0")
		raw, err := json.Marshal(spec.Build())
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}

	a := render(buildReg())
	b := render(buildReg())
	if string(a) != string(b) {
		t.Errorf("OpenAPI bytes differ across identical registry builds — non-deterministic iteration:\n--- a ---\n%s\n--- b ---\n%s", a, b)
	}
}
