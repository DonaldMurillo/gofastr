package appgen

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/framework"
)

// TestGeneratedWireKeysMatchServer: the server puts a column's value under
// its JSONCase key, which keeps the first word as written (Title stays
// Title, HTTPCode stays HTTPCode) and drops a leading underscore by
// capitalizing what follows (_note becomes Note). Every generated surface
// lowered the first letter instead, so a field named Title went out as
// "title": the typed repo's Create returned 201 with the field empty,
// reads decoded it as the zero value, and the Go and JS SDKs and the CLI
// sent and read a key the server ignores. Each surface must spell the key
// the server does, read from a live CrudHandler.
func TestGeneratedWireKeysMatchServer(t *testing.T) {
	authors := framework.EntityDeclaration{Name: "authors", Fields: []framework.FieldDeclaration{{Name: "email", Type: "string"}}}
	posts := framework.EntityDeclaration{
		Name: "posts",
		Fields: []framework.FieldDeclaration{
			{Name: "Title", Type: "string"},
			{Name: "_note", Type: "string"},
			{Name: "HTTPCode", Type: "int"},
			{Name: "ok_field", Type: "string"},
			{Name: "Author_id", Type: "relation", To: "authors"},
		},
		Relations: []framework.Relation{{Type: framework.RelManyToOne, Name: "Writer", Entity: "authors", ForeignKey: []string{"Author_id"}}},
	}
	decls := []framework.EntityDeclaration{authors, posts}

	cfg, err := posts.Config()
	if err != nil {
		t.Fatal(err)
	}
	h := framework.NewCrudHandler(framework.Define(posts.Name, cfg), nil)
	// server and wrong: the key the server reads and writes, and the
	// lower-first spelling the generator used to emit.
	type key struct{ server, wrong string }
	var keys []key
	for _, name := range []string{"Title", "_note", "HTTPCode", "Author_id", "Writer"} {
		k := key{server: h.WireKey(name)}
		k.wrong = strings.ToLower(k.server[:1]) + k.server[1:]
		if k.server == k.wrong {
			t.Fatalf("%s: the server key %q is already lower-first; the probe tests nothing", name, k.server)
		}
		keys = append(keys, k)
	}
	if got := h.WireKey("ok_field"); got != "okField" {
		t.Fatalf("control: ok_field is %q on the wire, want okField", got)
	}

	opts := SdkOptions{name: "app"}
	spec, err := buildSDKSpec(decls, &opts)
	if err != nil {
		t.Fatal(err)
	}
	sdk, err := renderSDKFiles(spec, []string{"go", "js"}, false)
	if err != nil {
		t.Fatal(err)
	}
	sdkFile := func(suffix string) string {
		for _, f := range sdk {
			if strings.HasSuffix(f.Name, suffix) {
				return f.Content
			}
		}
		t.Fatalf("the SDK render has no %s", suffix)
		return ""
	}
	goSurfaces := map[string]string{
		"entity model":      renderEntityModel(posts),
		"repo write shapes": renderEntityWriteShapes(posts, decls),
		"typed client":      renderClientEntity(posts, decls),
		"Go SDK client":     sdkFile("go/client.go"),
	}
	for surface, src := range goSurfaces {
		for _, k := range keys {
			if k.server == "Writer" && surface != "entity model" {
				continue // only the model carries an included relation
			}
			if !strings.Contains(src, `json:"`+k.server) {
				t.Errorf("%s: no json tag %q", surface, k.server)
			}
			if strings.Contains(src, `json:"`+k.wrong+`"`) || strings.Contains(src, `json:"`+k.wrong+`,`) {
				t.Errorf("%s: json tag %q, which the server does not read", surface, k.wrong)
			}
		}
	}
	dts := sdkFile("client.d.ts")
	for _, k := range keys[:3] {
		if !strings.Contains(dts, "  "+k.server+"?:") {
			t.Errorf("client.d.ts: no field %q", k.server)
		}
		if strings.Contains(dts, "  "+k.wrong+"?:") || strings.Contains(dts, "  "+k.wrong+":") {
			t.Errorf("client.d.ts: field %q, which the server does not send", k.wrong)
		}
	}
}

// TestWireKeyMatchesCrudHandler pins appspec.WireKey to the server's rule
// over the name shapes that tell the two spellings apart.
func TestWireKeyMatchesCrudHandler(t *testing.T) {
	decl := framework.EntityDeclaration{Name: "things", Fields: []framework.FieldDeclaration{{Name: "label", Type: "string"}}}
	cfg, err := decl.Config()
	if err != nil {
		t.Fatal(err)
	}
	h := framework.NewCrudHandler(framework.Define(decl.Name, cfg), nil)
	for _, name := range []string{"label", "ok_field", "Title", "_note", "__proto__", "HTTPCode", "HTTP_server", "a__b", "x_", "a_1b", "already_camelCase", "line_2_total"} {
		if got, want := appspec.WireKey(name), h.WireKey(name); got != want {
			t.Errorf("WireKey(%q) = %q, the server sends %q", name, got, want)
		}
	}
}
