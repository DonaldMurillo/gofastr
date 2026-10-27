package framework

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// An older build created a configured "AuditTrail" with its case kept.
// This build names audittrail and would write the trail into an empty
// table, so EnsureAuditTable refuses and prints the rename.
func TestEnsureAuditTableRefusesCaseKeptPG(t *testing.T) {
	db := pgtest.DB(t)
	if _, err := db.Exec(`CREATE TABLE "AuditTrail" (id TEXT PRIMARY KEY)`); err != nil {
		t.Fatalf("create the older table: %v", err)
	}
	err := EnsureAuditTable(db, "AuditTrail")
	if err == nil || !strings.Contains(err.Error(), `ALTER TABLE "AuditTrail" RENAME TO "audittrail"`) {
		t.Fatalf("EnsureAuditTable over \"AuditTrail\": err = %v, want the rename advice", err)
	}
}
