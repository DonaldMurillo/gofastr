package db

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

func TestKilnHugeMaxStringIsText(t *testing.T) {
	huge := 1e30
	if got := sqlType(schema.Field{Name: "s", Type: schema.String, Max: &huge}); got != "TEXT" {
		t.Fatalf("sqlType = %q, want TEXT", got)
	}
}
