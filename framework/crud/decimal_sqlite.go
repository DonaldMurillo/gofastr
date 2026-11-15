package crud

import (
	"context"
	"fmt"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// refuseInexactDecimals refuses, on SQLite only, a Decimal its column would not store as sent.
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
		if s == "" {
			continue
		}
		violation, refused := schema.SQLiteDecimalRefusal(f.RawType, s)
		if !refused {
			continue
		}
		if fields == nil {
			if isPostgres(ctx, ch.dialectHandle(), ch.conn(ctx)) {
				return nil
			}
			fields = map[string][]schema.Violation{}
		}
		fields[f.Name] = []schema.Violation{violation}
	}
	if fields != nil {
		return &ValidationError{fields: fields}
	}
	return nil
}
