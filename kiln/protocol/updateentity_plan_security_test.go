package protocol_test

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/kiln/protocol"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// CONTRACT-QUESTION red: update_page_element documents a reversibility
// rationale for skipping the gate; if that rationale is meant to cover
// update_entity too, delete this and document it — but then delete_field
// demanding a plan for ONE field while update_entity silently drops ALL
// fields is the inconsistency this pins.
//
// Pins: the destructive-op plan gate (requirePlan doc: "the destructive-op
// safety gate") applies to wholesale entity replacement — delete_field needs
// an approved plan naming the ONE field it removes, so update_entity
// replacing the entity with a one-field version (dropping every other field,
// flipping CrossOwnerRead/OwnerField to zero) must demand the same.
// Surfaces: kiln/protocol/protocol.go::UpdateEntity
// Finding: UpdateEntity never calls requirePlan and UpdateEntityArgs carries
// no PlanID — unlike DeleteEntity and DeleteField — so a full entity
// replacement applies with no plan, no needs_plan, no consumption, while the
// journal replay it drives (replay.go OpUpdateEntity:
// w.Entities[p.Entity.Name] = p.Entity) silently discards every field the
// replacement omits.
// Fix direction: add PlanID to UpdateEntityArgs and route UpdateEntity
// through requirePlan with target {op:"update_entity",name:<Name>}, same as
// delete_field/delete_entity.
func TestUpdateEntityPlanGate(t *testing.T) {
	tools := newTools(t)
	ctx := context.Background()

	if res := tools.AddEntity(ctx, protocol.AddEntityArgs{
		Entity: &world.Entity{Name: "posts", Fields: []world.Field{
			{Name: "title", Type: "string"},
			{Name: "body", Type: "text"},
		}},
	}); !res.OK {
		t.Fatalf("setup broken: add_entity: %+v", res)
	}

	// Wholesale replacement keeping ONE of the two fields. delete_field on
	// posts.body for this same loss demands an approved plan; the
	// replacement demands nothing today.
	res := tools.UpdateEntity(ctx, protocol.UpdateEntityArgs{
		Entity: &world.Entity{
			Name:   "posts",
			Fields: []world.Field{{Name: "title", Type: "string"}},
		},
	})
	if res.OK || res.Kind != "needs_plan" {
		t.Errorf("SECURITY: [updateentity-plan-gate] update_entity replacing posts with a one-field version returned kind=%q ok=%v — want needs_plan: requirePlan gates delete_field (one field) and delete_entity, but wholesale entity replacement (drops EVERY omitted field, flips cross_owner_read/owner_field) bypasses the destructive-op gate entirely: %+v", res.Kind, res.OK, res)
	}

	// The world must still carry BOTH fields: the gate held the edit back.
	got := tools.WorldGet(ctx, protocol.WorldGetArgs{Path: "entities.posts"})
	if !got.OK {
		t.Fatalf("setup broken: world_get entities.posts: %+v", got)
	}
	ent, ok := got.Result.(*world.Entity)
	if !ok {
		t.Fatalf("setup broken: world_get returned %T, want *world.Entity", got.Result)
	}
	if len(ent.Fields) != 2 {
		t.Errorf("SECURITY: [updateentity-plan-gate] posts now has %d fields (%v) after an ungated update_entity — body was dropped with no plan, no needs_plan, and no plan consumption; delete_field removing the same single field would have been blocked pending an approved plan", len(ent.Fields), fieldNames(ent.Fields))
	}
}

func fieldNames(fs []world.Field) []string {
	out := make([]string, len(fs))
	for i, f := range fs {
		out[i] = f.Name
	}
	return out
}
