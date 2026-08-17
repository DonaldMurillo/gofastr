package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// seedTypesYAML builds a minimal blueprint whose comments.post_id is a
// relation to posts (a UUID string column at runtime) and whose seed rows
// are provided per test.
func seedTypesYAML(seedRows string) string {
	return `app:
  name: SeedTypes
  module: example.com/seedtypes
entities:
  - name: posts
    fields:
      - name: title
        type: string
        required: true
  - name: comments
    fields:
      - name: body
        type: text
        required: true
      - name: post_id
        type: relation
        to: posts
        required: true
      - name: author
        type: string
      - name: views
        type: int
      - name: rating
        type: float
      - name: price
        type: decimal
      - name: published
        type: bool
seed:
  - entity: posts
    rows:
      - title: First post
      - title: Second post
  - entity: comments
    rows:
` + seedRows
}

func loadSeedTypesBlueprint(t *testing.T, seedRows string) error {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	writeTestFile(t, path, seedTypesYAML(seedRows))
	_, err := loadBlueprint(path)
	return err
}

// The blog-blueprint defect: a numeric literal seeded into a relation
// field. Relation columns are UUID strings at runtime, so the generated
// app dies at boot with "seed comments: validation failed" — zero
// actionable detail, after generate said everything was fine. The check
// must fire at generate/validate time and teach the @ref form.
func TestSeedRejectsNumericRelationValue(t *testing.T) {
	err := loadSeedTypesBlueprint(t, "      - body: hi\n        post_id: 1\n")
	if err == nil {
		t.Fatal("numeric seed into a relation field must fail at generate time, got nil")
	}
	for _, want := range []string{"comments", "post_id", "1", "@posts.title=", "relation"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q:\n%s", want, err.Error())
		}
	}
}

// The general invariant: a seed value whose Go type can never satisfy the
// target field's runtime validator. Each row seeds one column with a
// value the runtime would reject ("must be a string", "must be a
// number", ...). All of them must be caught before any code is emitted.
func TestSeedRejectsUnsatisfiableValueTypes(t *testing.T) {
	for _, tc := range []struct {
		name string
		rows string
		want string
	}{
		{"int into string field", "      - body: hi\n        author: 42\n", "author"},
		{"bool into string field", "      - body: hi\n        author: true\n", "author"},
		{"string into float field", "      - body: hi\n        rating: \"4.5\"\n", "rating"},
		{"bool into float field", "      - body: hi\n        rating: true\n", "rating"},
		{"int into relation field", "      - body: hi\n        post_id: 2\n", "post_id"},
		{"bool into int field", "      - body: hi\n        views: true\n", "views"},
		{"non-numeric string into int field", "      - body: hi\n        views: many\n", "views"},
		{"fractional float into int field", "      - body: hi\n        views: 1.5\n", "views"},
		{"bool into decimal field", "      - body: hi\n        price: true\n", "price"},
		{"non-boolean string into bool field", "      - body: hi\n        published: yes\n", "published"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := loadSeedTypesBlueprint(t, tc.rows)
			if err == nil {
				t.Fatalf("seed row must be rejected at generate time:\n%s", tc.rows)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error must name the offending field %q:\n%s", tc.want, err.Error())
			}
		})
	}
}

// Everything the runtime validators DO accept must keep flowing through:
// @refs on relations, ints on int columns, numbers on decimals (the
// emitter re-quotes them), true/false and 1/0 on bools, integral floats
// on ints, integer-parsable strings on ints.
func TestSeedAcceptsSatisfiableValueTypes(t *testing.T) {
	rows := `      - body: ref row
        post_id: "@posts.title=First post"
        views: 10
        rating: 4.5
        price: 19.99
        published: true
      - body: coercions row
        post_id: "@posts.title=Second post"
        author: "42"
        views: 7.0
        price: "3.50"
        published: 1
`
	if err := loadSeedTypesBlueprint(t, rows); err != nil {
		t.Fatalf("valid seed rows rejected:\n%v", err)
	}
}
