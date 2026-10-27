package a2a

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// An older build kept a prefixed "Agent_" task or push table case-kept.
// This build names the folded tables, so the store refuses to open and
// prints the rename.
func TestSQLStoreCaseKeptTableRefusedPG(t *testing.T) {
	for _, older := range []string{"Agent_a2a_tasks", "Agent_a2a_push_configs"} {
		t.Run(older, func(t *testing.T) {
			db := pgtest.DB(t)
			if _, err := db.Exec(`CREATE TABLE "` + older + `" (id TEXT PRIMARY KEY)`); err != nil {
				t.Fatalf("create the older table: %v", err)
			}
			_, err := NewSQLStore(db, WithTablePrefix("Agent_"))
			want := `ALTER TABLE "` + older + `" RENAME TO "` + strings.ToLower(older) + `"`
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("NewSQLStore over %q: err = %v, want %s", older, err, want)
			}
		})
	}
}
