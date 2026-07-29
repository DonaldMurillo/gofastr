package sdk

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func enumConfig(values ...string) entity.EntityConfig {
	return entity.EntityConfig{
		Name: "posts",
		Fields: []schema.Field{{
			Name:   "status",
			Type:   schema.String,
			Values: values,
		}},
	}.WithTimestamps(false)
}

// fmt.Sprint renders []string{"draft ready"} and []string{"draft", "ready"}
// identically. They are different client schemas and must not deduplicate.
func TestHashKeepsAmbiguousEnumsDistinct(t *testing.T) {
	oneValue := enumConfig("draft ready")
	twoValues := enumConfig("draft", "ready")

	manifest := SchemaHash([]NamedConfig{{Name: "posts", Config: oneValue}})
	live := SchemaHash([]NamedConfig{
		{Name: "posts", Config: oneValue, Version: "/api/v1"},
		{Name: "posts", Config: twoValues, Version: "/api/v2"},
	})
	if live == manifest {
		t.Fatalf("two divergent enum schemas collapsed to the manifest hash %s", live)
	}
}

func TestHashIsInputOrderIndependent(t *testing.T) {
	oneValue := enumConfig("draft ready")
	twoValues := enumConfig("draft", "ready")

	forward := SchemaHash([]NamedConfig{
		{Name: "posts", Config: oneValue, Version: "/api/v1"},
		{Name: "posts", Config: twoValues, Version: "/api/v2"},
	})
	reverse := SchemaHash([]NamedConfig{
		{Name: "posts", Config: twoValues, Version: "/api/v2"},
		{Name: "posts", Config: oneValue, Version: "/api/v1"},
	})
	if forward != reverse {
		t.Fatalf("the same divergent schemas hash differently by input order: forward=%s reverse=%s", forward, reverse)
	}
}
