package query

import (
	"reflect"
	"testing"
)

// A Set called after a Where still binds to its own placeholder: the
// args follow the SQL, not the call order.
func TestUpdateSetAfterWhereBindsInOrder(t *testing.T) {
	sql, args := Update("notes").
		Set("deleted_at", nil).
		Where("id = $1", "n1").
		Where("tenant_id = $1", "t1").
		Set("updated_at", "now").
		Build()
	wantSQL := "UPDATE notes SET deleted_at = $1, updated_at = $2 WHERE (id = $3) AND (tenant_id = $4)"
	if sql != wantSQL {
		t.Fatalf("sql = %q, want %q", sql, wantSQL)
	}
	if want := []any{nil, "now", "n1", "t1"}; !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %#v, want %#v", args, want)
	}
}
