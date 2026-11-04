package crud

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
)

func (ch *CrudHandler) resetImageDerived(ctx context.Context, req *http.Request, key []any, body map[string]any, sets func(schema.Field) bool) (map[string]bool, error) {
	if len(key) == 0 {
		return nil, nil
	}
	var columns []string
	fields := make(map[string]schema.Field)
	for _, field := range ch.Entity.GetFields() {
		fields[field.Name] = field
		if _, sent := body[field.Name]; sent && field.Type == schema.Image && sets(field) {
			columns = append(columns, field.Name)
		}
	}
	if len(columns) == 0 {
		return nil, nil
	}
	condition, args := ch.keyCond(key)
	selectRow := query.Select(columns...).From(ch.Entity.GetTable()).Where(condition, args...)
	ch.ApplyTenantScope(selectRow, req)
	applyOwnerScope(ch, selectRow, req, false)
	if ch.Entity.Config.Scope.SoftDelete {
		selectRow.Where("deleted_at IS NULL")
	}
	statement, args := selectRow.Build()
	previous := make([]sql.NullString, len(columns))
	destinations := make([]any, len(columns))
	for index := range previous {
		destinations[index] = &previous[index]
	}
	if err := ch.conn(ctx).QueryRowContext(ctx, statement, args...).Scan(destinations...); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("read image derivatives source: %w", err)
	}
	cleared := make(map[string]bool)
	for index, column := range columns {
		replacement, _ := body[column].(string)
		if replacement == previous[index].String {
			continue
		}
		for _, suffix := range []string{derivedColumnSuffixes.blurHash, derivedColumnSuffixes.placeholder, derivedColumnSuffixes.variants} {
			name := column + suffix
			field, declared := fields[name]
			if !declared || !ch.updateSets(WithServerWrites(ctx), field, true) || !ch.upsertSetsGuarded(ctx, name, true) {
				continue
			}
			if _, sent := body[name]; sent && sets(field) {
				continue
			}
			body[name] = nil
			cleared[name] = true
		}
	}
	return cleared, nil
}
