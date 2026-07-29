package main

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/sdk"
)

func TestSDKHashMatchesVersionedRuntime(t *testing.T) {
	decl := framework.EntityDeclaration{
		Name:  "posts",
		Table: "posts",
		Fields: []framework.FieldDeclaration{
			{Name: "title", Type: "string"},
		},
	}
	opts := sdkOptions{apiPrefix: "api/v1", targets: []string{"go"}}
	spec, err := buildSDKSpec([]framework.EntityDeclaration{decl}, &opts)
	if err != nil {
		t.Fatalf("buildSDKSpec: %v", err)
	}
	if spec.APIPrefix != "/api/v1" {
		t.Fatalf("API prefix = %q, want /api/v1", spec.APIPrefix)
	}
	generated, err := sdkSchemaHash(spec.Decls)
	if err != nil {
		t.Fatalf("sdkSchemaHash: %v", err)
	}
	cfg, err := decl.Config()
	if err != nil {
		t.Fatalf("declaration config: %v", err)
	}
	live := sdk.SchemaHash([]sdk.NamedConfig{{
		Name:    decl.Name,
		Version: "/api/v1",
		Config:  cfg,
	}})
	if generated != live {
		t.Fatalf("generated manifest hash %s never matches the versioned runtime hash %s", generated, live)
	}
}
