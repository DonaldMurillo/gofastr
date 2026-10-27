package semantic

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// An older build kept a configured "Chunks" table case-kept. This build
// names chunks, so EnsureSchema refuses and prints the rename.
func TestPgVectorCaseKeptTableRefused(t *testing.T) {
	db := pgtest.DB(t)
	if _, err := db.Exec(`CREATE TABLE "Chunks" (id TEXT PRIMARY KEY)`); err != nil {
		t.Fatalf("create the older table: %v", err)
	}
	s, err := NewPgVector(db, PgVectorConfig{Table: "Chunks", Dim: 3})
	if err != nil {
		t.Fatalf("NewPgVector: %v", err)
	}
	err = s.EnsureSchema(context.Background())
	if err == nil || !strings.Contains(err.Error(), `ALTER TABLE "Chunks" RENAME TO "chunks"`) {
		t.Fatalf("EnsureSchema over \"Chunks\": err = %v, want the rename advice", err)
	}
}
