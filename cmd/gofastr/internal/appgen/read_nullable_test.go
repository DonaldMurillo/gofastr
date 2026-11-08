package appgen

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/framework"
)

// nullableDecl is the read-shape fixture: title is required, code is the
// primary key, ref is auto-generated, every other scalar can be null on
// the wire (a PATCH to null, a row from before the column existed).
func nullableDecl() framework.EntityDeclaration {
	return framework.EntityDeclaration{
		Name:       "notes",
		PrimaryKey: []string{"code"},
		Fields: []framework.FieldDeclaration{
			{Name: "code", Type: "string", Required: true},
			{Name: "title", Type: "string", Required: true},
			{Name: "ref", Type: "string", AutoGenerate: "uuid"},
			{Name: "body", Type: "text"},
			{Name: "views", Type: "int", Default: 5},
			{Name: "pinned", Type: "bool"},
			{Name: "status", Type: "enum", Values: []string{"draft", "live"}, Default: "draft"},
			{Name: "meta", Type: "json"},
		},
	}
}

// Optional scalars are pointers in the model struct: a value type cannot
// hold the null the wire serves, and a silent zero reads as authored data.
// Required fields, primary keys and auto-generated columns stay values —
// the server cannot serve their absence.
func TestModelOptionalScalarsArePointers(t *testing.T) {
	out := renderEntityModel(nullableDecl())
	for _, want := range []string{
		"Code string `json:\"code,omitempty\"`",
		"Title string `json:\"title,omitempty\"`",
		"Ref string `json:\"ref,omitempty\"`",
		"Body *string `json:\"body,omitempty\"`",
		"Views *int `json:\"views,omitempty\"`",
		"Pinned *bool `json:\"pinned,omitempty\"`",
		"Status *NotesStatusValue `json:\"status,omitempty\"`",
		"Meta any `json:\"meta,omitempty\"`",
	} {
		if !strings.Contains(out, "\t"+want+"\n") {
			t.Errorf("model struct missing %q:\n%s", want, out)
		}
	}
}

// The client's read struct carries the same nullability: an SDK consumer
// reading an unset field must see nil, not a fabricated zero.
func TestClientReadOptionalScalarsArePointers(t *testing.T) {
	decl := nullableDecl()
	decl.Timestamps = &[]bool{false}[0]
	out := renderClientEntity(decl, []framework.EntityDeclaration{decl})
	read := between(t, out, "type Notes struct {", "}") + "\n"
	for _, want := range []string{
		"Code string `json:\"code,omitempty\"`",
		"Title string `json:\"title,omitempty\"`",
		"Ref string `json:\"ref,omitempty\"`",
		"Body *string `json:\"body,omitempty\"`",
		"Views *int `json:\"views,omitempty\"`",
		"Pinned *bool `json:\"pinned,omitempty\"`",
		"Status *string `json:\"status,omitempty\"`",
		"Meta any `json:\"meta,omitempty\"`",
	} {
		if !strings.Contains(read, "\t"+want+"\n") {
			t.Errorf("client read struct missing %q:\n%s", want, read)
		}
	}
	if strings.Contains(read, "\tMeta *any ") {
		t.Errorf("json fields are already nilable as any:\n%s", read)
	}
}

// The classifier one read shape must share: required, key and
// auto-generated take values everywhere, everything else a pointer.
func TestReadFieldTakesValue(t *testing.T) {
	decl := nullableDecl()
	for _, f := range decl.Fields {
		got := readFieldTakesValue(decl, f)
		want := f.Name == "code" || f.Name == "title" || f.Name == "ref" || f.Name == "meta"
		if got != want {
			t.Errorf("readFieldTakesValue(%s) = %v, want %v", f.Name, got, want)
		}
	}
	// Injected timestamps are auto-generated reads: they stay values.
	if !readFieldTakesValue(decl, framework.FieldDeclaration{Name: "created_at", Type: "timestamp", AutoGenerate: "timestamp"}) {
		t.Error("an injected timestamp is auto-generated and must take a value")
	}
	if appspec.ToCamelCase("x") == "" {
		t.Fatal("unreachable")
	}
}
