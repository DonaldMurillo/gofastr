package sdk

import (
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"strings"
	"testing"
)

// The generated clients' JSON keys follow the app's JSONCase, so two
// apps with the same entities and different cases are different SDK
// surfaces. The hash ignored the case until #669: flipping an app
// between camelCase and snake_case kept the drift check saying "in
// sync". It is part of the digest now, and an unset case means camel,
// the app default.
func TestSchemaHashIncludesJSONCase(t *testing.T) {
	cfg := NamedConfig{
		Name:   "posts",
		Config: entityPostsConfig(),
	}
	camel := SchemaHash([]NamedConfig{cfg}, "camelCase")
	snake := SchemaHash([]NamedConfig{cfg}, "snake_case")
	if camel == snake {
		t.Fatalf("hash identical under camelCase and snake_case (%s); JSONCase is not in the digest", camel)
	}
	// Empty means camel, the default: existing manifests of camel apps
	// re-verify without code changes.
	if unset := SchemaHash([]NamedConfig{cfg}, ""); unset != camel {
		t.Fatalf("empty case hashes differently from camelCase (%s vs %s); it must mean the default", unset, camel)
	}
	// The case does not break determinism: same inputs, same digest.
	if again := SchemaHash([]NamedConfig{cfg}, "snake_case"); again != snake {
		t.Fatalf("hash not deterministic across runs: %s vs %s", again, snake)
	}
	if !strings.HasPrefix(camel, "sha256:") {
		t.Fatalf("digest lost its prefix: %s", camel)
	}
}

func entityPostsConfig() entity.EntityConfig {
	return entity.EntityConfig{
		Name:   "posts",
		Table:  "posts",
		Fields: []schema.Field{{Name: "title", Type: schema.String, Required: true}},
	}
}

// hashOf is the test spelling of the pre-#669 one-argument SchemaHash:
// every assertion that does not care about the case hashes a camel app,
// the default.
func hashOf(named []NamedConfig) string {
	return SchemaHash(named, "")
}
