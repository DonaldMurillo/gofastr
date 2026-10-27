package crud

import (
	"reflect"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// A unique field's bare column name is MySQL's key name only. Postgres
// folds a unique index named Email over Token to email, and that matched
// the Email field's alias first: a duplicate Token answered fields.Email
// instead of the bare conflict a sensitive field gets.
func TestPgIndexNotReadAsMySQLKey(t *testing.T) {
	ent := entity.Define("acct", entity.EntityConfig{
		Name: "acct", Table: "acct",
		Fields: []schema.Field{
			{Name: "Email", Type: schema.String, Unique: true},
			{Name: "Token", Type: schema.String, NoQuery: true},
		},
		Indices: []entity.Index{{Name: "Email", Columns: []string{"Token"}, Unique: true}},
	}.WithTimestamps(false))
	for _, c := range []struct {
		msg  string
		want []string
	}{
		{`ERROR: duplicate key value violates unique constraint "email" (SQLSTATE 23505)`, []string{"Token"}},
		{`ERROR: duplicate key value violates unique constraint "acct_email_key" (SQLSTATE 23505)`, []string{"Email"}},
		{`Error 1062: Duplicate entry 'a@b' for key 'Email'`, []string{"Email"}},
	} {
		if got := conflictColumns(ent, c.msg); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s\n got %v, want %v", c.msg, got, c.want)
		}
	}
}
