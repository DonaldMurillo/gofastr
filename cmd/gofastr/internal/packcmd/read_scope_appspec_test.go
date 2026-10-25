package packcmd

import (
	"reflect"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appgen"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// readScopeRoundTripYAML exercises every emission shape: unrestricted set and
// unset, the four ops, single values, value lists, and bools.
const readScopeRoundTripYAML = `app:
  name: RSRT
  module: example.com/rsrt
entities:
  - name: posts
    crud: true
    read_scope:
      unrestricted: posts:review
      filter:
        - field: status
          op: eq
          value: published
        - field: category
          op: neq
          value: internal
        - field: visibility
          op: in
          values: [public, unlisted]
        - field: approved
          op: not_in
          values: ["false", "pending"]
    fields:
      - name: title
        type: string
      - name: status
        type: enum
        values: [draft, published]
      - name: category
        type: string
      - name: visibility
        type: string
      - name: approved
        type: bool
  - name: comments
    crud: true
    read_scope:
      filter:
        - field: approved
          value: "true"
    fields:
      - name: body
        type: text
      - name: approved
        type: bool
`

// TestPack_ReadScopeSerializerRoundTrip: a parsed app spec carrying
// read_scope must survive encode -> decode unchanged, or a pack operation
// silently drops the security posture.
func TestPack_ReadScopeSerializerRoundTrip(t *testing.T) {
	a, err := decodeAppSpecString(readScopeRoundTripYAML)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	yml, err := encodeAppSpecYAML(a)
	if err != nil {
		t.Fatalf("serialize app spec: %v", err)
	}
	b, err := decodeAppSpecString(yml)
	if err != nil {
		t.Fatalf("re-parse serialized yaml: %v\n--- yaml ---\n%s", err, yml)
	}
	if !reflect.DeepEqual(a, b) {
		t.Errorf("round-trip mismatch.\n%s\n--- serialized yaml ---\n%s", clitest.FirstAppSpecDiff(a, b), yml)
	}
}

// TestPack_ReadScopeRoundTripsThroughGo: the generate leg (renderEntityRegistration
// -> Go literal) and the pack leg (packEntityDeclFromCall -> declaration) must
// both carry read_scope, or `gofastr pack` drops the posture even though the
// serializer round-trip passes.
func TestPack_ReadScopeRoundTripsThroughGo(t *testing.T) {
	a, err := decodeAppSpecString(readScopeRoundTripYAML)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	dir := materializeAppSpec(t, a)
	got, err := appgen.PackReadEntities(dir)
	if err != nil {
		t.Fatalf("packReadEntities: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("recovered %d entities, want 2", len(got))
	}
	posts := got[0].Exposure.ReadScope
	if posts == nil {
		t.Fatalf("posts ReadScope lost through generate+pack: %#v", got[0].Exposure)
	}
	if posts.Unrestricted != "posts:review" || len(posts.Filter) != 4 {
		t.Fatalf("posts ReadScope changed: %#v", posts)
	}
	if posts.Filter[2].Op != "in" || !reflect.DeepEqual(posts.Filter[2].Values, []string{"public", "unlisted"}) {
		t.Fatalf("in-predicate changed: %#v", posts.Filter[2])
	}
	if posts.Filter[3].Op != "not_in" || !reflect.DeepEqual(posts.Filter[3].Values, []string{"false", "pending"}) {
		t.Fatalf("not_in-predicate changed: %#v", posts.Filter[3])
	}
	comments := got[1].Exposure.ReadScope
	if comments == nil || len(comments.Filter) != 1 || comments.Filter[0].Value != "true" || comments.Filter[0].Op != "" {
		t.Fatalf("comments ReadScope changed: %#v", comments)
	}
}

// TestRegisterEmitsReadScopeLiteral pins the generated Go: the literal must
// reference framework.ReadScopeConfig so a re-export rename fails here, not
// in every generated app.
func TestRegisterEmitsReadScopeLiteral(t *testing.T) {
	bp, err := decodeAppSpecString(readScopeRoundTripYAML)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	files, err := appgen.RenderGeneratedProject(bp.Entities)
	if err != nil {
		t.Fatalf("renderGeneratedProject: %v", err)
	}
	var entityFile string
	// The registration literal lives in the entity's own file, not register.go.
	for _, f := range files {
		if f.Name == "posts.go" {
			entityFile = f.Content
		}
	}
	if entityFile == "" {
		t.Fatalf("no posts entity file among %d files", len(files))
	}
	want := `ReadScope: &framework.ReadScopeConfig{Unrestricted: "posts:review", Filter: []framework.RowPredicate{framework.RowPredicate{Field: "status", Op: "eq", Value: "published"}, framework.RowPredicate{Field: "category", Op: "neq", Value: "internal"}, framework.RowPredicate{Field: "visibility", Op: "in", Values: []string{"public", "unlisted"}}, framework.RowPredicate{Field: "approved", Op: "not_in", Values: []string{"false", "pending"}}}`
	if !strings.Contains(entityFile, want) {
		t.Fatalf("posts.go missing ReadScope literal:\n%s\n--- file ---\n%s", want, entityFile)
	}
}
