package appgen

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// Issue #614: client.d.ts typed an optional field absent-or-value, but the
// server stores a null for one and reads return it. A field the server can
// hold as null is T | null, the rule the OpenAPI document already follows;
// a required, generated or key field never is.
func TestDTSOptionalFieldsTakeNull(t *testing.T) {
	decls := []framework.EntityDeclaration{{
		Name: "posts", Table: "posts",
		Fields: []framework.FieldDeclaration{
			{Name: "title", Type: "string", Required: true},
			{Name: "subtitle", Type: "string"},
			{Name: "views", Type: "int"},
			{Name: "status", Type: "enum", Values: []string{"draft", "live"}},
			{Name: "slug", Type: "string", AutoGenerate: "uuid"},
		},
	}}
	spec, err := buildSDKSpec(decls, &SdkOptions{name: "nulls", sdkVersion: "0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	var dts string
	for _, f := range renderSDKJSFiles(spec) {
		if f.Name == "client.d.ts" {
			dts = f.Content
		}
	}
	read := section(t, dts, "export interface Posts {")
	input := section(t, dts, "export interface PostsInput {")
	for _, want := range []string{"  subtitle?: string | null;", "  views?: number | string | null;", `  status?: "draft" | "live" | null;`} {
		if !strings.Contains(read, want) {
			t.Errorf("Posts lacks %q:\n%s", want, read)
		}
		if !strings.Contains(input, want) {
			t.Errorf("PostsInput lacks %q:\n%s", want, input)
		}
	}
	for _, refuse := range []string{"  title?: string | null", "  title: string | null", "  slug?: string | null", "  id: string | null"} {
		if strings.Contains(read, refuse) || strings.Contains(input, refuse) {
			t.Errorf("a field the server never stores as null takes null: %q", refuse)
		}
	}
}

// section returns dts from the line opening an interface to its closing
// brace.
func section(t *testing.T, dts, open string) string {
	t.Helper()
	i := strings.Index(dts, open)
	if i < 0 {
		t.Fatalf("client.d.ts lacks %q", open)
	}
	j := strings.Index(dts[i:], "\n}\n")
	return dts[i : i+j]
}
