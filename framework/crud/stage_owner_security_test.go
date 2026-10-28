package crud

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// ownerOutbox records the owner each staged row was tagged with.
type ownerOutbox struct{ owners []string }

func (o *ownerOutbox) AppendFor(_ context.Context, _ DBExecutor, owner, _ string, _ any) (string, error) {
	o.owners = append(o.owners, owner)
	return "1", nil
}
func (o *ownerOutbox) Nudge() {}

// StageEvent tags the outbox row with the record's owner, so erasing
// that user reaches the staged payload; an entity with no owner field
// tags nobody.
func TestStageEventTagsRecordOwner(t *testing.T) {
	owned := entity.Define("memos", entity.EntityConfig{
		Fields: []schema.Field{{Name: "user_id", Type: schema.String}, {Name: "body", Type: schema.String}},
		Scope:  &entity.ScopeConfig{OwnerField: "user_id"},
	})
	open := entity.Define("posts", entity.EntityConfig{
		Fields: []schema.Field{{Name: "body", Type: schema.String}},
	})
	for _, tc := range []struct {
		ent  *entity.Entity
		want string
	}{{owned, "bob"}, {open, ""}} {
		ob := &ownerOutbox{}
		ch := NewCrudHandler(tc.ent, nil).WithJSONCase(CaseSnake)
		ch.Outbox = ob
		if err := ch.StageEvent(context.Background(), "created", map[string]any{"id": "1", "user_id": "bob", "body": "x"}); err != nil {
			t.Fatalf("StageEvent: %v", err)
		}
		if len(ob.owners) != 1 || ob.owners[0] != tc.want {
			t.Errorf("%s: staged owners %q, want [%q]", tc.ent.GetName(), ob.owners, tc.want)
		}
	}
}
