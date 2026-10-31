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

// Event stamps name whose row an update or delete event is about.
//
// eventData stamps an event with the record's own owner and tenant
// columns, but a write's record does not always carry them: the owner
// column the framework injects is Hidden, so an update's RETURNING list
// leaves it out, and a delete's payload is the key alone. The writer's
// context was the fallback, so Alice's cross-owner (owner.AllowCrossOwner)
// update or delete of Bob's row was stamped Alice: Bob's _events stream
// missed it, Alice's received it, and the outbox row named Alice for
// erasure (2026-10-27 D1, D39).
//
// An operation's entry point installs a holder with withEventStamps
// before its transaction, the write reads the row's columns into it
// before changing the row (stampRow), and eventData falls back to them
// for a record that lacks the columns, before the context. An entry point
// without a holder keeps the context fallback.

type eventStampsKey struct{}

// stampKey names one row: the table and its record id.
type stampKey struct{ table, id string }

// rowStamp is a row's owner and tenant column values, nil when unset.
type rowStamp struct{ owner, tenant any }

type eventStamps struct {
	mu   sync.Mutex
	rows map[stampKey]rowStamp
}

// withEventStamps gives ctx a stamp holder, keeping one already there so
// a nested operation's stamps reach the outer one's events.
func withEventStamps(ctx context.Context) context.Context {
	if _, ok := ctx.Value(eventStampsKey{}).(*eventStamps); ok {
		return ctx
	}
	return context.WithValue(ctx, eventStampsKey{}, &eventStamps{rows: map[stampKey]rowStamp{}})
}

// stampColumns are the columns an event about this entity is routed by.
func (ch *CrudHandler) stampColumns() (ownerCol, tenantCol string) {
	ownerCol = ch.Entity.Config.Scope.OwnerField
	if ch.Entity.Config.Scope.MultiTenant {
		tenantCol = ch.Entity.Config.TenantColumn()
	}
	return ownerCol, tenantCol
}

// stampRow reads id's owner and tenant columns into ctx's holder. It
// runs before the write changes or removes the row, unscoped: the write
// has been authorized, and the stamp only routes its event. A missing
// row stamps nothing.
func (ch *CrudHandler) stampRow(ctx context.Context, id string) error {
	st, ok := ctx.Value(eventStampsKey{}).(*eventStamps)
	if !ok || id == "" {
		return nil
	}
	oc, tc := ch.stampColumns()
	if oc == "" && tc == "" {
		return nil
	}
	// The key columns come back too: the event's record names the row by
	// its stored key, which can be spelled differently from id ("7" for
	// "007"), so the stamp is filed under both.
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

// rowStampFor is the stamp stampRow read for record's row.
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
