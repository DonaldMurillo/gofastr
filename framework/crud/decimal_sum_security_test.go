package crud

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func TestDecimalSumRejectsBadData(t *testing.T) {
	for _, value := range []string{"not-a-number", "1e1001", "1/3"} {
		t.Run(value, func(t *testing.T) {
			database := openBooleanReadDB(t)
			if _, err := database.Exec(`CREATE TABLE totals (id TEXT, amount TEXT)`); err != nil {
				t.Fatal(err)
			}
			if _, err := database.Exec(`INSERT INTO totals VALUES ('invalid', ?)`, value); err != nil {
				t.Fatal(err)
			}
			definition := entity.Define("totals", entity.EntityConfig{
				Fields: []schema.Field{{Name: "amount", Type: schema.Decimal, RawType: "TEXT"}},
			}.WithTimestamps(false))
			if total, err := NewCrudHandler(definition, database).SumAll(context.Background(), "amount", ListOptions{}); err == nil {
				t.Fatalf("stored %q summed to %q without a refusal", value, total)
			}
		})
	}
}
