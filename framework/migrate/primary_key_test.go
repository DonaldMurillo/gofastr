package migrate

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// A replica that read the key before a peer changed it re-reads the key
// under the lock and, finding it already declared, changes nothing: the
// stale read alone would refuse the populated table.
func TestConvergeKeyRereadsLiveKey(t *testing.T) {
	db := openSQLiteForMigrateTest(t)
	db.SetMaxOpenConns(1)
	ctx := context.Background()
	for _, q := range []string{
		"CREATE TABLE enrollments (student_id TEXT NOT NULL, course_id TEXT NOT NULL, PRIMARY KEY (student_id, course_id))",
		"INSERT INTO enrollments (student_id, course_id) VALUES ('s1', 'c1')",
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	ent := entity.Define("enrollment", entity.EntityConfig{
		Table: "enrollments",
		Fields: []schema.Field{
			{Name: "student_id", Type: schema.String, Required: true},
			{Name: "course_id", Type: schema.String, Required: true},
		},
		PrimaryKey: []string{"student_id", "course_id"},
	}.WithTimestamps(false))
	stale := liveKey{cols: []string{"id"}}
	if err := convergeKey(ctx, db, ent, map[string]*entity.Entity{"enrollment": ent}, DialectSQLite, stale); err != nil {
		t.Fatalf("convergeKey with a stale pre-lock key: %v", err)
	}
}
