package crud

import (
	"errors"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func TestRelationColumnDiagnostics(t *testing.T) {
	cols := []string{"bad key"}
	t.Run("key", func(t *testing.T) {
		_, _, _, err := keyInClause(cols, []string{"1"}, 1)
		const want = `invalid key column "bad key": query: unsafe SQL identifier "bad key"`
		if err == nil || err.Error() != want {
			t.Errorf("keyInClause error = %v, want %q", err, want)
		}
		if cause := errors.Unwrap(err); cause == nil || cause.Error() != `query: unsafe SQL identifier "bad key"` {
			t.Errorf("keyInClause cause = %v, want unsafe identifier error", cause)
		}
	})
	t.Run("foreign key", func(t *testing.T) {
		_, err := relationFKs(entity.Relation{Name: "parent", ForeignKey: cols})
		const want = `invalid column "bad key": query: unsafe SQL identifier "bad key"`
		diagnostic := errors.Unwrap(err)
		if diagnostic == nil || diagnostic.Error() != want {
			t.Errorf("relationFKs column error = %v, want %q", diagnostic, want)
		}
		if cause := errors.Unwrap(diagnostic); cause == nil || cause.Error() != `query: unsafe SQL identifier "bad key"` {
			t.Errorf("relationFKs cause = %v, want unsafe identifier error", cause)
		}
	})
	t.Run("keyword", func(t *testing.T) {
		clause, _, ok, err := keyInClause([]string{"select"}, []string{"1"}, 1)
		if err != nil || !ok || clause != `"select" IN ($1)` {
			t.Errorf("keyInClause returned %q, %t, %v, want a quoted keyword", clause, ok, err)
		}
	})
}
