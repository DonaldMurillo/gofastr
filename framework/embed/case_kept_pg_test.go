package embed

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// An older build kept a configured "Burns" table case-kept. This build
// names burns and would accept every burned nonce again, so the store
// refuses to open and prints the rename.
func TestSQLBurnStoreCaseKeptTableRefusedPG(t *testing.T) {
	db := pgtest.DB(t)
	if _, err := db.Exec(`CREATE TABLE "Burns" (nonce_id TEXT PRIMARY KEY)`); err != nil {
		t.Fatalf("create the older table: %v", err)
	}
	_, err := NewSQLBurnStore(db, WithBurnTable("Burns"))
	if err == nil || !strings.Contains(err.Error(), `ALTER TABLE "Burns" RENAME TO "burns"`) {
		t.Fatalf("NewSQLBurnStore over \"Burns\": err = %v, want the rename advice", err)
	}
}
