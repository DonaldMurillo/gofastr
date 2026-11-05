package entity

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

func TestDefineOwnsRelationSlices(t *testing.T) {
	for _, length := range []int{0, 1} {
		shared := make([]Relation, length, 4)
		if length > 0 {
			shared[0] = BelongsTo("owner", "users", "owner_id", "owner_region")
		}
		define := func(name, target string) *Entity {
			return Define(name, EntityConfig{Fields: []schema.Field{{Name: target + "_id", Type: schema.Relation, To: target}}, Relations: shared})
		}
		first := define("first", "authors")
		second := define("second", "teams")
		if first.Config.Relations[length].Entity != "authors" || second.Config.Relations[length].Entity != "teams" {
			t.Fatalf("derived relations share backing storage: first=%v second=%v", first.Config.Relations, second.Config.Relations)
		}
		if length > 0 {
			shared[0].ForeignKey[0] = "mutated"
			shared[0].ForeignKey[1] = "mutated_region"
			shared[0].Entity = "changed"
			for _, ent := range []*Entity{first, second} {
				if ent.Config.Relations[0].Entity != "users" || ent.Config.Relations[0].ForeignKey[0] != "owner_id" || ent.Config.Relations[0].ForeignKey[1] != "owner_region" {
					t.Fatal("caller mutation changed a registered relation")
				}
			}
		}
	}
}
