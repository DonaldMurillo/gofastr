package framework

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// TestRegistry_RejectsConflictingRelationsAcrossVersions pins the F9
// invariant — two versions of one entity share a physical table, so a
// relation on the same column/key MUST reference the same target. The gate
// fires for EVERY relation type, not only BelongsTo: a HasOne, a
// ManyToMany, or a name-deduped logical relation that diverges across
// versions is rejected at registration with a message naming the conflict.
// Each subtest registers a v1, then a conflicting v2, and expects the
// registration to panic.
func TestRegistry_RejectsConflictingRelationsAcrossVersions(t *testing.T) {
	widgetCfg := func(rels []entity.Relation) EntityConfig {
		return EntityConfig{
			Table:     "widgets",
			Fields:    []schema.Field{{Name: "title", Type: schema.String}},
			Relations: rels,
			Exposure:  &ExposureConfig{CRUD: boolPtr(false)},
		}.WithTimestamps(false)
	}

	// registerV1 then registerV2; expect v2 to panic with the F9 message.
	expectConflictPanic := func(t *testing.T, v1, v2 []entity.Relation) {
		t.Helper()
		app := NewApp(WithoutDefaultMiddleware())
		g1 := app.Group("/v1")
		app.GroupEntity(g1, "widgets", widgetCfg(v1))

		g2 := app.Group("/v2")
		defer func() {
			r := recover()
			if r == nil {
				t.Fatal("v2 registration did not panic on the relation conflict")
			}
			msg, ok := r.(string)
			if !ok {
				t.Fatalf("panic value = %T, want string", r)
			}
			if !strings.Contains(msg, "incompatible definitions across versions") {
				t.Errorf("panic message lacks the F9 relation-conflict text: %q", msg)
			}
		}()
		app.GroupEntity(g2, "widgets", widgetCfg(v2))
	}

	t.Run("HasOne_name_dedup", func(t *testing.T) {
		// No ForeignKey → the relation dedups by logical Name; the display
		// column is the Name too. A HasOne named "profile" pointing at two
		// different targets across versions is a conflict.
		expectConflictPanic(t,
			[]entity.Relation{{Type: entity.RelHasOne, Name: "profile", Entity: "profiles"}},
			[]entity.Relation{{Type: entity.RelHasOne, Name: "profile", Entity: "avatars"}},
		)
	})

	t.Run("ManyToMany_fk_target", func(t *testing.T) {
		// ForeignKey + ForeignKeyTarget set → describeRelation renders the
		// target(key) form. A ManyToMany on the same key diverging in target
		// entity is a conflict.
		expectConflictPanic(t,
			[]entity.Relation{{Type: entity.RelManyToMany, Name: "tags", Entity: "tags", ForeignKey: "tag_id", ForeignKeyTarget: "id"}},
			[]entity.Relation{{Type: entity.RelManyToMany, Name: "tags", Entity: "labels", ForeignKey: "tag_id", ForeignKeyTarget: "id"}},
		)
	})

	t.Run("extra_relation_skipped_then_conflict", func(t *testing.T) {
		// v2 carries an extra relation v1 never declared (dedup key not in
		// v1's map → skipped) AND a colliding one. The skip must not mask the
		// collision: the colliding relation still trips the gate.
		expectConflictPanic(t,
			[]entity.Relation{{Type: entity.RelHasMany, Name: "comments", Entity: "comments", ForeignKey: "post_id"}},
			[]entity.Relation{
				{Type: entity.RelHasMany, Name: "likes", Entity: "likes", ForeignKey: "like_id"},             // not in v1 → skipped
				{Type: entity.RelHasMany, Name: "comments", Entity: "other_comments", ForeignKey: "post_id"}, // collides
			},
		)
	})
}
