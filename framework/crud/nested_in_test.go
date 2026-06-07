package crud

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
)

// TestNestedIn_BelongsToCoalescesToIN pins the fix: a nested _in on a
// BelongsTo (ManyToOne) emits a single `col IN ($1,$2)` inside one EXISTS with
// all values as args — not separate AND-ed equals that can never all hold for a
// single related row.
func TestNestedIn_BelongsToCoalescesToIN(t *testing.T) {
	nf := nestedFilter{
		Relation: entity.Relation{Type: entity.RelManyToOne, Name: "author", Entity: "users", ForeignKey: "author_id"},
		Field:    "name",
		Op:       filter.OpIn,
		Values:   []string{"alice", "bob"},
	}
	sql, args := buildExistsSubquery("posts", "id", nf)

	if !strings.Contains(sql, "users.name IN ($1,$2)") {
		t.Errorf("expected coalesced IN, got: %s", sql)
	}
	if strings.Count(sql, "EXISTS") != 1 {
		t.Errorf("expected a single EXISTS, got: %s", sql)
	}
	if len(args) != 2 || args[0] != "alice" || args[1] != "bob" {
		t.Errorf("args = %v, want [alice bob]", args)
	}
}
