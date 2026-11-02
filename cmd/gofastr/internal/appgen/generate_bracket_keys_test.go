package appgen

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// stageDecls declares stage beside stage_in: the bare key ?stage_in=
// reads two ways (stage_in's equality, stage's IN list) and the server
// answers it 400.
func stageDecls() []framework.EntityDeclaration {
	return []framework.EntityDeclaration{{
		Name:  "visits",
		Table: "visits",
		Fields: []framework.FieldDeclaration{
			{Name: "stage", Type: "string"},
			{Name: "stage_in", Type: "string"},
			{Name: "title", Type: "string"},
		},
	}}
}

// The CLI cannot reach a key that reads two ways: stage's --stage-in
// (its IN list) is the same flag as stage_in's equality flag, so the
// generator refuses the entity and names both fields. The SDKs below
// still spell the bracket keys.
func TestRenderCLI_CollidingInFlagRefused(t *testing.T) {
	_, err := buildCLISpec(stageDecls(), CliOptions{OutDir: "cli", Binary: "myapp", verbs: "list"}, "example.com/app/entities/client")
	if err == nil || !strings.Contains(err.Error(), `fields "stage" and "stage_in" both derive CLI flag --stage-in`) {
		t.Fatalf("buildCLISpec err = %v, want the --stage-in collision", err)
	}
}

// The SDK readmes name each ambiguous key with its bracket spellings, and
// a spec with none gets the readme it always had.
func TestSDKReadmesNameCollidingKeys(t *testing.T) {
	spec, err := buildSDKSpec(stageDecls(), &SdkOptions{name: "app"})
	if err != nil {
		t.Fatal(err)
	}
	for name, md := range map[string]string{"go": renderSDKGoReadme(spec), "js": renderSDKJSReadme(spec)} {
		for _, want := range []string{"`stage_in` on `visits`: send `stage_in[eq]`", "or `stage[in]`"} {
			if !strings.Contains(md, want) {
				t.Errorf("%s readme lacks %q:\n%s", name, want, md)
			}
		}
	}
	plain, err := buildSDKSpec([]framework.EntityDeclaration{{
		Name: "posts", Table: "posts",
		Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}},
	}}, &SdkOptions{name: "app"})
	if err != nil {
		t.Fatal(err)
	}
	for name, md := range map[string]string{"go": renderSDKGoReadme(plain), "js": renderSDKJSReadme(plain)} {
		if strings.Contains(md, "operator word in") {
			t.Errorf("%s readme of a spec with no colliding key gained the bracket note:\n%s", name, md)
		}
	}
}

// An OpenAPI spec naming a colliding key's two readings (x[in], x_in[eq])
// derives plain kebab flags, and the wire name is sent as declared.
func TestOAKebabSplitsBracketOperator(t *testing.T) {
	for in, want := range map[string]string{"x[in]": "x-in", "x_in[eq]": "x-in-eq", "created_at[gte]": "created-at-gte"} {
		if got := oaKebab(in); got != want || !oaValidFlag(got) {
			t.Errorf("oaKebab(%q) = %q, want %q", in, got, want)
		}
	}
}
