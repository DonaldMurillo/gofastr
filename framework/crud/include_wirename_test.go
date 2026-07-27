package crud

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// Sol #15. deepConvertMap applied the PARENT handler's wire map at every
// depth, so an included row was renamed by the wrong entity's schema:
// posts aliasing body_text->"summary" renamed comments' body_text to
// "summary", ignoring comments' own declared alias "content".
func TestInclude_UsesTargetEntityWireMapNotParents(t *testing.T) {
	comments := entity.Define("comments", entity.EntityConfig{
		Table: "comments",
		Fields: []schema.Field{
			{Name: "id", Type: schema.Int},
			{Name: "body_text", Type: schema.String, WireName: "content"},
		},
	}.WithTimestamps(false))

	posts := entity.Define("posts", entity.EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "id", Type: schema.Int},
			{Name: "body_text", Type: schema.String, WireName: "summary"},
			{Name: "post_id", Type: schema.Int},
		},
		Relations: []entity.Relation{entity.HasMany("comments", "comments", "post_id")},
	}.WithTimestamps(false))

	reg := versionedReg{m: map[string]*entity.Entity{
		"posts|": posts, "comments|": comments,
	}}

	ch := &CrudHandler{Entity: posts, Registry: reg}

	rel, ok := relationByName(posts, "comments")
	if !ok {
		t.Fatal("relation not declared")
	}

	rows := []map[string]any{{"id": 1, "body_text": "hello"}}
	got, err := ch.formatRelationValueDeep(rel, rows, true,
		map[uintptr]*convertedSubtree{}, newIncludeBudget())
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	out, ok := got.([]map[string]any)
	if !ok || len(out) != 1 {
		t.Fatalf("unexpected shape: %#v", got)
	}
	if _, has := out[0]["content"]; !has {
		t.Errorf("included row should use the TARGET's alias %q; got keys %v",
			"content", wireKeysOf(out[0]))
	}
	if _, has := out[0]["summary"]; has {
		t.Errorf("included row used the PARENT's alias %q — wrong entity's wire map",
			"summary")
	}
}

func wireKeysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// wireConverterFor has three fallbacks, each of which must degrade to the
// handler's own converter rather than dropping keys or panicking: no
// registry, an unresolvable target, and a target that declares no aliases.
func TestInclude_WireConverterFallbacks(t *testing.T) {
	plain := entity.Define("plain", entity.EntityConfig{
		Table:  "plain",
		Fields: []schema.Field{{Name: "body_text", Type: schema.String}},
	}.WithTimestamps(false))
	parent := entity.Define("posts", entity.EntityConfig{
		Table:  "posts",
		Fields: []schema.Field{{Name: "body_text", Type: schema.String, WireName: "summary"}},
	}.WithTimestamps(false))

	t.Run("no registry falls back to the handler converter", func(t *testing.T) {
		ch := &CrudHandler{Entity: parent}
		if got := ch.wireConverterFor("anything")("body_text"); got == "" {
			t.Error("converter dropped the key")
		}
	})

	t.Run("unresolvable target falls back", func(t *testing.T) {
		ch := &CrudHandler{Entity: parent, Registry: versionedReg{m: map[string]*entity.Entity{}}}
		if got := ch.wireConverterFor("missing")("body_text"); got == "" {
			t.Error("converter dropped the key")
		}
	})

	t.Run("target with no aliases uses plain case conversion", func(t *testing.T) {
		ch := &CrudHandler{Entity: parent, Registry: versionedReg{
			m: map[string]*entity.Entity{"plain|": plain},
		}}
		got := ch.wireConverterFor("plain")("body_text")
		// Must NOT pick up the parent's "summary" alias.
		if got == "summary" {
			t.Error("a target with no aliases inherited the parent's alias")
		}
		if got == "" {
			t.Error("converter dropped the key")
		}
	})
}
