package appgen

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// dtsExport matches a top-level declaration client.d.ts exports.
var dtsExport = regexp.MustCompile(`(?m)^export (?:declare )?(?:class|interface|type|const|function|enum) ([A-Za-z_$][A-Za-z0-9_$]*)`)

// dtsRuntimeTypes must name every export the runtime block declares, or
// checkDTSTypeNames lets an entity reuse one and the d.ts fails to
// type-check with "Duplicate identifier".
func TestDTSRuntimeTypesListsEveryExport(t *testing.T) {
	// One entity, since the README needs one; its own exports are dropped
	// below so only the runtime block's remain.
	decl := framework.EntityDeclaration{Name: "posts", Table: "posts",
		Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}}}
	ent := buildEntityModel(decl, nil)
	spec := sdkSpec{Decls: []framework.EntityDeclaration{decl}, Entities: []cliEntity{ent}}
	var dts string
	for _, f := range renderSDKJSFiles(spec) {
		if f.Name == "client.d.ts" {
			dts = f.Content
		}
	}
	if dts == "" {
		t.Fatal("renderSDKJSFiles wrote no client.d.ts")
	}
	var got []string
	for _, m := range dtsExport.FindAllStringSubmatch(dts, -1) {
		if strings.HasPrefix(m[1], ent.Struct) || m[1] == jsResourceProp(ent)+"Fields" {
			continue
		}
		got = append(got, m[1])
	}
	slices.Sort(got)
	want := slices.Clone(dtsRuntimeTypes)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("dtsRuntimeTypes = %v, the runtime block exports %v", want, got)
	}
}

// An entity named no_blob derives the interface NoBlob, which the runtime
// already exports; it is refused instead of shipping a broken d.ts.
func TestNoBlobEntityRefused(t *testing.T) {
	decl := framework.EntityDeclaration{Name: "no_blob", Table: "no_blob",
		Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}}}
	spec := sdkSpec{Decls: []framework.EntityDeclaration{decl}, Entities: []cliEntity{buildEntityModel(decl, nil)}}
	err := checkDTSTypeNames(spec)
	if err == nil || !strings.Contains(err.Error(), "NoBlob") {
		t.Fatalf("checkDTSTypeNames = %v, want a NoBlob clash", err)
	}
}
