package crud

import (
	"context"
	"fmt"
	"math/big"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
)

func TestSQLiteDecimalSumIsExact(t *testing.T) {
	for _, test := range []struct {
		name   string
		values []any
		want   string
	}{
		{"mixed tenths", []any{"0.1", "0.2"}, "0.3"},
		{"money tie", []any{"0.1", "0.235"}, "0.335"},
		{"tenths", []any{"0.1", "0.1", "0.1", "0.1", "0.1", "0.1", "0.1", "0.1", "0.1", "0.1", "0.1", "0.1", "0.1", "0.1", "0.1"}, "1.5"},
		{"round once", []any{"1.3375", "1.3375", nil}, "2.675"},
		{"cancellation", []any{"999999999999999", "0.1", "-999999999999999"}, "0.1"},
		{"nulls", []any{nil, nil}, "0"},
		{"empty", nil, "0"},
	} {
		t.Run(test.name, func(t *testing.T) {
			database := openBooleanReadDB(t)
			if _, err := database.Exec(`CREATE TABLE totals (id TEXT, kind TEXT, amount DECIMAL(19,4))`); err != nil {
				t.Fatal(err)
			}
			for index, value := range test.values {
				if _, err := database.Exec(`INSERT INTO totals VALUES (?, 'included', ?)`, fmt.Sprint(index), value); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := database.Exec(`INSERT INTO totals VALUES ('excluded', 'excluded', 900)`); err != nil {
				t.Fatal(err)
			}
			definition := entity.Define("totals", entity.EntityConfig{
				Fields: []schema.Field{{Name: "kind", Type: schema.String}, {Name: "amount", Type: schema.Decimal}},
			}.WithTimestamps(false))
			handler := NewCrudHandler(definition, database)
			total, err := handler.SumAll(context.Background(), "amount", ListOptions{Where: &filter.Predicate{Field: "kind", Op: filter.OpEq, Value: "included"}})
			if err != nil || total != test.want {
				t.Fatalf("SumAll = %q, %v; want %q", total, err, test.want)
			}
			if test.name == "round once" {
				number, ok := new(big.Rat).SetString(total)
				if !ok || number.FloatString(2) != "2.68" {
					t.Fatalf("money total %q does not round to 2.68", total)
				}
			}
		})
	}
}
