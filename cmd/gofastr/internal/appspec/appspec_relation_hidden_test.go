package appspec

import "testing"

// `hidden: true` under a relation in gofastr.yml reaches Relation.Hidden,
// the opt-out that keeps a has_many out of ?include= and the published
// schema; absent, the relation stays includable.
func TestRelationHiddenKeyLoads(t *testing.T) {
	bp, err := loadBPWithEntity(t, `  - name: users
    fields:
      - name: email
        type: string
    relations:
      - type: has_many
        name: owned_notes
        entity: notes
        foreign_key: owner_ref
        hidden: true
      - type: has_many
        name: notes
        entity: notes
        foreign_key: owner_ref
  - name: notes
    fields:
      - name: owner_ref
        type: string
`)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	rels := bp.Entities[0].Relations
	if !rels[0].Hidden {
		t.Fatalf("relations[0].Hidden = false; `hidden: true` did not load")
	}
	if rels[1].Hidden {
		t.Fatalf("relations[1].Hidden = true with no hidden key")
	}
}
