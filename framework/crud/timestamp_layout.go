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

// canonicalTimestampGlob is the SQLite GLOB pattern text written in
// schema.TimestampLayout for a UTC instant matches, the one spelling crud
// writes and binds.
const canonicalTimestampGlob = "[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z"

// storedTimestampLayouts are the spellings a stored-text cursor value is
// checked against: RFC 3339 and the forms SQLite's date functions read
// (lang_datefunc.html, formats 1 to 7, the zone optional). A tampered
// cursor holding anything else is a 400, as it is on a canonical column.
var storedTimestampLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02T15:04:05.999999999",
	"2006-01-02 15:04",
	"2006-01-02T15:04",
	time.DateOnly,
}

// errTimestampLayout is the refusal of a comparison on a Timestamp column
// whose stored text is not in schema.TimestampLayout.
var errTimestampLayout = errors.New("timestamp column holds text outside schema.TimestampLayout")

// storedTextColumn is the select expression that reads field's stored
// value as text with no declared type, so the SQLite driver hands back
// the text instead of parsing a DATETIME column into a time.Time.
func storedTextColumn(field string) string {
	return query.QuoteKeyword(field) + "||''"
}

// legacyTimestampsPossible reports whether this entity's Timestamp
// columns may hold text in another layout and are compared as text: an
// Unmanaged table (one the framework did not create, so another writer
// may have filled it) on SQLite. Postgres compares a timestamp column as
// an instant, and every row crud writes is canonical. It asks the
// dialect only for an Unmanaged entity.
func (ch *CrudHandler) legacyTimestampsPossible(ctx context.Context) bool {
	return ch.Entity != nil && ch.Entity.Config.Unmanaged && !isPostgres(ctx, ch.dialectHandle(), ch.conn(ctx))
}

// storedTimestampKeys maps each Timestamp field among fields to the row
// key its storedTextColumn scans under, when legacyTimestampsPossible:
// then a keyset compares the text the anchor row stores, which is what
// ORDER BY sorts, so a cursor walk follows the sorted list whatever the
// layout. Rebinding the anchor in schema.TimestampLayout moved it: a
// space-separated "2026-03-03 09:00:00" anchor bound as
// "2026-03-03T09:00:00.000000Z" sorted after every row of that day, and
// an "...00Z" anchor before itself. nil means the keyset binds the
// canonical value.
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

// storedTextColumns is the storedTextColumn of every field textKeys
// names, in fields order.
func storedTextColumns(fields []string, textKeys map[string]string) []string {
	var columns []string
	for _, field := range fields {
		if _, stored := textKeys[field]; stored {
			columns = append(columns, storedTextColumn(field))
		}
	}
	return columns
}

// storedTimestampText checks a stored-text keyset value the cursor or
// the anchor row carried and returns it to bind as it is.
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

// refuseLegacyTimestampComparisons refuses a filter that compares a
// Timestamp column holding text outside schema.TimestampLayout, when
// legacyTimestampsPossible. The filter binds its value in that layout and
// SQLite compares text, so against "2026-03-03 10:00:00" the bind
// "2026-03-03T09:00:00.000000Z" read as later and at_gt dropped the row,
// and an "...00Z" row read as later than its own instant. The check
// reads one row: any non-NULL value of a compared column that does not
// match canonicalTimestampGlob. Pattern matching (like) compares the text
// as sent and is not checked. Rewriting the stored values, or reading the
// table through a view that writes them in the canonical layout, clears
// the refusal.
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

// refuseLegacyTimestampConditions is refuseLegacyTimestampComparisons for
// a TypedQuery's Where chain: the columns its TimestampColumn comparisons
// name (entity.Condition.ComparedTimestamps). The typed reads and the
// bulk UpdateAll/DeleteAll bound the same canonical text, so they
// silently missed the row stored as "2026-03-03 10:00:00", or wrote the
// wrong set, while the filter surfaces refused.
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

// refuseLegacyTimestampColumns runs the one-row probe over each Timestamp
// column in compared, when legacyTimestampsPossible.
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

// timestampLayoutError names the column refuseLegacyTimestampComparisons
// found holding text outside schema.TimestampLayout.
type timestampLayoutError struct {
	table, field string
}

func (e *timestampLayoutError) Error() string {
	return fmt.Sprintf("%s.%s: %v", e.table, e.field, errTimestampLayout)
}

func (e *timestampLayoutError) Unwrap() error { return errTimestampLayout }

// walkPredicateLeaves calls visit with every leaf of a ?where= tree.
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
