package access

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// An older build kept a configured "Grants" table, or its tombstone
// table, case-kept. This build names grants and grants_revoked, so
// EnsureSchema refuses and prints the rename: booting on would drop every
// stored grant and bring revoked ones back.
func TestGrantStoreCaseKeptTableRefusedPG(t *testing.T) {
	for _, older := range []string{"Grants", "Grants_revoked"} {
		t.Run(older, func(t *testing.T) {
			db := pgtest.DB(t)
			if _, err := db.Exec(`CREATE TABLE "` + older + `" (role TEXT, permission TEXT)`); err != nil {
				t.Fatalf("create the older table: %v", err)
			}
			s := NewGrantStore(db, NewRolePolicy(), WithGrantTable("Grants"))
			err := s.EnsureSchema(context.Background())
			want := `ALTER TABLE "` + older + `" RENAME TO "` + strings.ToLower(older) + `"`
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("EnsureSchema over %q: err = %v, want %s", older, err, want)
			}
		})
	}
}
