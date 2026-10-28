package migrate_test

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// A Max past Postgres's VARCHAR limit (10485760) used to emit a width the
// CREATE TABLE refuses, and one past the int range a negative width.
func TestHugeMaxStringIsText(t *testing.T) {
	for _, max := range []float64{1e30, 10485761} {
		m := max
		got := migrate.SQLType(schema.Field{Name: "s", Type: schema.String, Max: &m}, migrate.DialectPostgres)
		if got != "TEXT" {
			t.Errorf("Max %v: SQLType = %q, want TEXT", max, got)
		}
	}
	ok := 255.0
	if got := migrate.SQLType(schema.Field{Name: "s", Type: schema.String, Max: &ok}, migrate.DialectPostgres); got != "VARCHAR(255)" {
		t.Errorf("Max 255: SQLType = %q, want VARCHAR(255)", got)
	}
}
