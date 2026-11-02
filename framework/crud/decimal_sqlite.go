package crud

import (
	"context"
	"fmt"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// refuseInexactDecimals refuses, on SQLite, a Decimal value its column
// would keep only as a 64-bit float: one with a fraction and more than
// schema.SQLiteDecimalDigits significant digits. SQLite has no exact
// decimal type, so 999999999999999.1234, which DECIMAL(19,4) holds,
// read back as 999999999999999.1 while the write answered success.
// Postgres stores every digit, so it takes the value. The refusal is a
// field error with ReasonDecimalSignificantDigits, the 400 the other
// decimal checks answer.
//
// It runs after schema validation, so every value it reads is a decimal
// literal the column's precision and scale admit. The dialect is asked
// only when some value would lose digits on SQLite, which costs one
// SELECT version() on the write's connection (inside its transaction
// when ctx carries one) for those writes alone.
func (ch *CrudHandler) refuseInexactDecimals(ctx context.Context, body map[string]any) error {
	var fields map[string][]schema.Violation
	for _, f := range ch.entitySchema().Fields {
		if f.Type != schema.Decimal {
			continue
		}
		v, ok := body[f.Name]
		if !ok || v == nil {
			continue
		}
		s, ok := v.(string)
		if !ok {
			st, isStringer := v.(fmt.Stringer)
			if !isStringer {
				continue
			}
			s = st.String()
		}
		if s == "" || schema.DecimalExactOnSQLite(f.RawType, s) {
			continue
		}
		if fields == nil {
			if isPostgres(ctx, ch.conn(ctx)) {
				return nil
			}
			fields = map[string][]schema.Violation{}
		}
		fields[f.Name] = []schema.Violation{{
			Reason: schema.ReasonDecimalSignificantDigits,
			Params: map[string]any{"max": schema.SQLiteDecimalDigits},
		}}
	}
	if fields != nil {
		return &ValidationError{fields: fields}
	}
	return nil
}
