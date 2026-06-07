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

// TestNestedLike_EscapesWildcards pins I1: a nested _like wraps the value in a
// contains pattern with escaped LIKE metacharacters + ESCAPE, matching the
// hardened top-level _like, so caller-supplied % / _ are matched literally
// (no wildcard-probe injection).
func TestNestedLike_EscapesWildcards(t *testing.T) {
	nf := nestedFilter{
		Relation: entity.Relation{Type: entity.RelManyToOne, Name: "author", Entity: "users", ForeignKey: "author_id"},
		Field:    "name",
		Op:       filter.OpLike,
		Value:    "50%_x",
	}
	sql, args := buildExistsSubquery("posts", "id", nf)
	if !strings.Contains(sql, "LIKE $1 ESCAPE") {
		t.Errorf("expected ESCAPE clause, got: %s", sql)
	}
	if len(args) != 1 || args[0] != `%50\%\_x%` {
		t.Errorf("arg = %q, want %%50\\%%\\_x%% (escaped, contains-wrapped)", args[0])
	}
}
