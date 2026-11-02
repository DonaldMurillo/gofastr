package crud

import (
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"math/big"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// SQLite keeps a DECIMAL(19,4) value in a 64-bit float once it has a
// fraction, so a value past 15 significant digits read back with digits
// gone (999999999999999.1234 came back 999999999999999.1) while the
// create answered 201. SQLite refuses it with a 400 naming the field;
// Postgres stores every digit and takes it. A value of 15 significant
// digits, and a whole number the column holds, reads back as sent on
// both, and so does any value in a TEXT column.
func TestDecimalSQLiteRefusesDigitsItWouldDrop(t *testing.T) {
	eachDialect(t, func(t *testing.T, db *sql.DB) {
		if _, err := db.Exec(`CREATE TABLE dledger (id TEXT PRIMARY KEY, amount DECIMAL(19,4), memo TEXT)`); err != nil {
			t.Fatal(err)
		}
		ent := entity.Define("dledger", entity.EntityConfig{
			Name:  "dledger",
			Table: "dledger",
			Fields: []schema.Field{
				{Name: "amount", Type: schema.Decimal},
				{Name: "memo", Type: schema.Decimal, RawType: "TEXT"},
			},
		}.WithTimestamps(false))
		ent.SetDB(db)
		ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
		pg := strings.HasSuffix(t.Name(), "postgres")
		ctx := t.Context()

		for _, v := range []string{"999999999999999.1234", "90071992547409.1234", "1234567890123.456"} {
			row, err := ch.CreateOne(ctx, map[string]any{"amount": v})
			if !pg {
				var ve *ValidationError
				if !errors.As(err, &ve) || len(ve.fields["amount"]) == 0 || ve.fields["amount"][0].Reason != schema.ReasonDecimalSignificantDigits {
					t.Errorf("sqlite create %s: err = %v, want decimal_significant_digits on amount", v, err)
				}
				continue
			}
			if err != nil {
				t.Fatalf("postgres create %s: %v", v, err)
			}
			got, err := ch.GetOne(ctx, fmt.Sprint(row["id"]), nil)
			if err != nil || !sameDecimal(got["amount"], v) {
				t.Errorf("postgres %s read back %#v (%v)", v, got["amount"], err)
			}
		}

		// What a float keeps exactly is taken on both, as is a TEXT column.
		for _, body := range []map[string]any{
			{"amount": "12345678901.2345"},
			{"amount": "123456789012345"},
			{"amount": "0.0001"},
			{"memo": "999999999999999.1234"},
		} {
			row, err := ch.CreateOne(ctx, maps.Clone(body))
			if err != nil {
				t.Fatalf("create %v: %v", body, err)
			}
			got, err := ch.GetOne(ctx, fmt.Sprint(row["id"]), nil)
			if err != nil {
				t.Fatal(err)
			}
			for k, v := range body {
				if !sameDecimal(got[k], v) {
					t.Errorf("%s %v read back %#v", k, v, got[k])
				}
			}
		}

		// An update is held to the same line.
		row, err := ch.CreateOne(ctx, map[string]any{"amount": "1"})
		if err != nil {
			t.Fatal(err)
		}
		_, err = ch.UpdateOne(ctx, fmt.Sprint(row["id"]), map[string]any{"amount": "999999999999999.1234"})
		if pg != (err == nil) {
			t.Errorf("update to 19 significant digits: err = %v (postgres=%v)", err, pg)
		}
	})
}

// sameDecimal reports whether a read-back decimal is the number sent:
// Postgres pads to the column's scale (1.5 reads 1.5000).
func sameDecimal(got, want any) bool {
	g, ok1 := new(big.Rat).SetString(fmt.Sprint(got))
	w, ok2 := new(big.Rat).SetString(fmt.Sprint(want))
	return ok1 && ok2 && g.Cmp(w) == 0
}
