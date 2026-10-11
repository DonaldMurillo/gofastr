package crud

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// ErrAggregateMasked refuses an aggregate read under WithReadHooks on an
// entity with AfterList hooks. Those hooks mask rows after the query; a
// total the database computes never passes through them, so it could
// count what they hide. Read the rows through ListAll instead.
var ErrAggregateMasked = errors.New("crud: an aggregate cannot pass through AfterList hooks")

// GroupCount is one group GroupCountAll returns: a stored value of the
// field (nil for NULL) and how many rows hold it.
type GroupCount struct {
	Value any
	Count int
}

// SumAll returns SUM(field) over the rows ListAll(ctx, opts) would read,
// computed by the database: every match, not a page. The total is the
// database's own decimal text ("0" when no row matches), so a Decimal
// column sums without float rounding where the database keeps it exact.
// field names a visible Int, Float or Decimal column. opts takes the
// filters and Search; Fields, Sorts, Limit, Offset and Includes are
// refused, since an aggregate reads every match.
func (ch *CrudHandler) SumAll(ctx context.Context, field string, opts ListOptions) (string, error) {
	f, err := ch.aggregateField(ctx, "SumAll", field, opts, func(t schema.FieldType) bool {
		return t == schema.Int || t == schema.Float || t == schema.Decimal
	})
	if err != nil {
		return "", err
	}
	col := f.Name
	qb, _, err := ch.scopedSelect(ctx, "SumAll", opts, []string{col})
	if err != nil {
		return "", err
	}
	inner, args := qb.Build()
	var total any
	stmt := "SELECT SUM(" + query.QuoteIdent(col) + ") FROM (" + inner + ") AS agg_src"
	if err := ch.DB.QueryRowContext(ctx, stmt, args...).Scan(&total); err != nil {
		return "", err
	}
	switch v := total.(type) {
	case nil:
		return "0", nil
	case int64:
		return strconv.FormatInt(v, 10), nil
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), nil
	case []byte:
		return string(v), nil
	case string:
		return v, nil
	default:
		return "", fmt.Errorf("SumAll: unexpected %T from the database", total)
	}
}

// GroupCountAll counts the rows ListAll(ctx, opts) would read per stored
// value of field, ordered by value, computed by the database. limit, when
// positive, caps the groups returned; ask for one more than a chart draws
// to tell a capped result from a complete one. field names a visible
// column that is not JSON; opts is refused as SumAll refuses it.
func (ch *CrudHandler) GroupCountAll(ctx context.Context, field string, opts ListOptions, limit int) ([]GroupCount, error) {
	f, err := ch.aggregateField(ctx, "GroupCountAll", field, opts, func(t schema.FieldType) bool {
		return t != schema.JSON
	})
	if err != nil {
		return nil, err
	}
	col := f.Name
	qb, _, err := ch.scopedSelect(ctx, "GroupCountAll", opts, []string{col})
	if err != nil {
		return nil, err
	}
	inner, args := qb.Build()
	q := query.QuoteIdent(col)
	stmt := "SELECT " + q + ", COUNT(*) FROM (" + inner + ") AS agg_src GROUP BY " + q + " ORDER BY " + q
	if limit > 0 {
		stmt += " LIMIT " + strconv.Itoa(limit)
	}
	rows, err := ch.DB.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GroupCount
	for rows.Next() {
		var g GroupCount
		if err := rows.Scan(&g.Value, &g.Count); err != nil {
			return nil, err
		}
		// The value a list read would carry: a bool column reads true or
		// false, not SQLite's 1 or 0.
		g.Value = convertDatabaseValue(g.Value, f.Type == schema.Bool)
		out = append(out, g)
	}
	return out, rows.Err()
}

// aggregateField resolves field (its column or wire name) to a visible
// field whose type ok accepts. It refuses the ListOptions an aggregate
// cannot honor, and an aggregate AfterList hooks would mask.
func (ch *CrudHandler) aggregateField(ctx context.Context, op, field string, opts ListOptions, ok func(schema.FieldType) bool) (schema.Field, error) {
	if len(opts.Fields) > 0 || len(opts.Sorts) > 0 || opts.Limit != 0 || opts.Offset != 0 || len(opts.Includes) > 0 {
		return schema.Field{}, fmt.Errorf("%s: Fields, Sorts, Limit, Offset and Includes do not apply to an aggregate", op)
	}
	if readHooksEnabled(ctx) && ch.Hooks != nil && len(ch.Hooks.HooksFor(hook.AfterList)) > 0 {
		return schema.Field{}, fmt.Errorf("%s: %w", op, ErrAggregateMasked)
	}
	for _, f := range ch.snapshotFields() {
		if f.Name != field && (f.WireName == "" || f.WireName != field) {
			continue
		}
		if f.Hidden {
			break
		}
		if !ok(f.Type) {
			return schema.Field{}, fmt.Errorf("%s: field %q cannot be aggregated this way", op, field)
		}
		return f, nil
	}
	return schema.Field{}, fmt.Errorf("%s: unknown field %q", op, field)
}
