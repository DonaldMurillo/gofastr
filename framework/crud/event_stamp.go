package crud

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

type eventStampsKey struct{}

type stampKey struct{ table, id string }

type rowStamp struct{ owner, tenant any }

type eventStamps struct {
	mu   sync.Mutex
	rows map[stampKey]rowStamp
}

// withEventStamps gives ctx a stamp holder unless it has one.
func withEventStamps(ctx context.Context) context.Context {
	if _, ok := ctx.Value(eventStampsKey{}).(*eventStamps); ok {
		return ctx
	}
	return context.WithValue(ctx, eventStampsKey{}, &eventStamps{rows: map[stampKey]rowStamp{}})
}

func (ch *CrudHandler) stampColumns() (ownerCol, tenantCol string) {
	ownerCol = ch.Entity.Config.Scope.OwnerField
	if ch.Entity.Config.Scope.MultiTenant {
		tenantCol = ch.Entity.Config.TenantColumn()
	}
	return ownerCol, tenantCol
}

// stampRow reads row id's owner and tenant columns, unscoped, into ctx's
// holder before a write changes the row.
func (ch *CrudHandler) stampRow(ctx context.Context, id string) error {
	st, ok := ctx.Value(eventStampsKey{}).(*eventStamps)
	if !ok || id == "" {
		return nil
	}
	oc, tc := ch.stampColumns()
	if oc == "" && tc == "" {
		return nil
	}
	keys := ch.keyCols()
	cols := slices.Clone(keys)
	for _, c := range []string{oc, tc} {
		if c != "" {
			cols = append(cols, c)
		}
	}
	kc, ka := ch.keyWhere(id)
	sqlStr, args := query.Select(cols...).From(ch.Entity.GetTable()).Where(kc, ka...).Build()
	vals := make([]sql.NullString, len(cols))
	dest := make([]any, len(cols))
	for i := range vals {
		dest[i] = &vals[i]
	}
	err := ch.conn(ctx).QueryRowContext(ctx, sqlStr, args...).Scan(dest...)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read event owner: %w", err)
	}
	stored := make(map[string]any, len(keys))
	for i, c := range keys {
		if vals[i].Valid {
			stored[c] = vals[i].String
		}
	}
	var s rowStamp
	i := len(keys)
	if oc != "" {
		if vals[i].Valid {
			s.owner = vals[i].String
		}
		i++
	}
	if tc != "" && vals[i].Valid {
		s.tenant = vals[i].String
	}
	table := ch.Entity.GetTable()
	st.mu.Lock()
	defer st.mu.Unlock()
	st.rows[stampKey{table, id}] = s
	if key, ok := entity.RowKey(stored, keys, nil); ok {
		st.rows[stampKey{table, key}] = s
	}
	return nil
}

func (ch *CrudHandler) rowStampFor(ctx context.Context, record any) (rowStamp, bool) {
	st, ok := ctx.Value(eventStampsKey{}).(*eventStamps)
	if !ok {
		return rowStamp{}, false
	}
	rec, ok := record.(map[string]any)
	if !ok {
		return rowStamp{}, false
	}
	id, ok := ch.rowID(rec)
	if !ok {
		return rowStamp{}, false
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	s, ok := st.rows[stampKey{ch.Entity.GetTable(), id}]
	return s, ok
}
