package crud

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
)

// canonicalTimestampGlob matches the schema.TimestampLayout text crud writes.
const canonicalTimestampGlob = "[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z"

// storedTimestampLayouts are RFC 3339 and the forms SQLite's date functions read.
var storedTimestampLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02T15:04:05.999999999",
	"2006-01-02 15:04",
	"2006-01-02T15:04",
	time.DateOnly,
}

var errTimestampLayout = errors.New("timestamp column holds text outside schema.TimestampLayout")

// storedTextColumn reads field as untyped text so the driver does not parse a DATETIME.
func storedTextColumn(field string) string {
	return query.QuoteKeyword(field) + "||''"
}

// legacyTimestampsPossible: an Unmanaged entity on SQLite, whose Timestamp text may be in any layout.
func (ch *CrudHandler) legacyTimestampsPossible(ctx context.Context) bool {
	return ch.Entity != nil && ch.Entity.Config.Unmanaged && !isPostgres(ctx, ch.dialectHandle(), ch.conn(ctx))
}

// storedTimestampKeys maps each Timestamp field to the row key of its stored text, so a keyset binds what ORDER BY sorted; nil binds canonical values.
func (ch *CrudHandler) storedTimestampKeys(ctx context.Context, fields []string) map[string]string {
	var timestampFields []string
	for _, field := range fields {
		if ch.queryField(field).Type == schema.Timestamp {
			timestampFields = append(timestampFields, field)
		}
	}
	if len(timestampFields) == 0 || !ch.legacyTimestampsPossible(ctx) {
		return nil
	}
	keys := make(map[string]string, len(timestampFields))
	for _, field := range timestampFields {
		keys[field] = ch.convertKey(storedTextColumn(field))
	}
	return keys
}

func storedTextColumns(fields []string, textKeys map[string]string) []string {
	var columns []string
	for _, field := range fields {
		if _, stored := textKeys[field]; stored {
			columns = append(columns, storedTextColumn(field))
		}
	}
	return columns
}

func storedTimestampText(field, raw string) (string, error) {
	for _, layout := range storedTimestampLayouts {
		if _, err := time.Parse(layout, raw); err == nil {
			return raw, nil
		}
	}
	if _, err := filter.CoerceValue(field, schema.Timestamp, raw); err != nil {
		return "", err
	}
	return raw, nil
}

func (ch *CrudHandler) refuseLegacyTimestampComparisons(ctx context.Context, filters []filter.ParsedFilter, where *filter.Predicate) error {
	var compared []string
	note := func(field string, op filter.FilterOp) {
		if op != filter.OpLike && ch.queryField(field).Type == schema.Timestamp && !slices.Contains(compared, field) {
			compared = append(compared, field)
		}
	}
	for _, parsed := range filters {
		note(parsed.Field, parsed.Op)
	}
	walkPredicateLeaves(where, note)
	return ch.refuseLegacyTimestampColumns(ctx, compared)
}

func (ch *CrudHandler) refuseLegacyTimestampConditions(ctx context.Context, conds []entity.Condition) error {
	var compared []string
	for _, cond := range conds {
		for _, field := range cond.ComparedTimestamps() {
			if ch.queryField(field).Type == schema.Timestamp && !slices.Contains(compared, field) {
				compared = append(compared, field)
			}
		}
	}
	return ch.refuseLegacyTimestampColumns(ctx, compared)
}

func (ch *CrudHandler) refuseLegacyTimestampColumns(ctx context.Context, compared []string) error {
	if len(compared) == 0 || !ch.legacyTimestampsPossible(ctx) {
		return nil
	}
	for _, field := range compared {
		probe := query.Select(field).From(ch.Entity.GetTable())
		probe.Where(storedTextColumn(field)+" NOT GLOB $1", canonicalTimestampGlob)
		probe.Limit(1)
		statement, args := probe.Build()
		var found any
		err := ch.conn(ctx).QueryRowContext(ctx, statement, args...).Scan(&found)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		return &timestampLayoutError{table: ch.Entity.GetTable(), field: field}
	}
	return nil
}

type timestampLayoutError struct {
	table, field string
}

func (e *timestampLayoutError) Error() string {
	return fmt.Sprintf("%s.%s: %v", e.table, e.field, errTimestampLayout)
}

func (e *timestampLayoutError) Unwrap() error { return errTimestampLayout }

func walkPredicateLeaves(where *filter.Predicate, visit func(string, filter.FilterOp)) {
	if where == nil {
		return
	}
	if len(where.Children) == 0 {
		visit(where.Field, where.Op)
		return
	}
	for index := range where.Children {
		walkPredicateLeaves(&where.Children[index], visit)
	}
}
