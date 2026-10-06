package framework

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/entityui"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

// EntityUI returns this app's entity screens: lists and records drawn from
// each entity's schema, Display and States, with ext's kinds, view funcs,
// tabs and actions. Call it once, after every entity is registered. It
// checks every name ext uses and panics at boot on a bad one, naming it,
// the way App.Entity refuses a bad declaration.
//
// It mounts the bulk bar's routes beside each entity's write routes:
// POST <api>/_bulk and GET <api>/_export.csv. With ext.Jobs set it also
// creates the snapshot tables queued runs walk (gofastr_bulk_jobs and
// gofastr_bulk_items), which needs a database. A second call panics: the
// routes belong to one UI, so share the one it returned.
func (a *App) EntityUI(ext entityui.Extensions) *entityui.UI {
	if a.entityUI != nil {
		panic("framework: EntityUI was already called on this app; its bulk and export routes belong to that UI, so pass the *entityui.UI it returned instead of building a second")
	}
	host := entityUIHost{a: a}
	if ext.Jobs != nil {
		if a.DB == nil {
			panic("framework: EntityUI: Extensions.Jobs needs a database (WithDB): queued bulk runs keep their selection there")
		}
		store, err := newSQLBulkStore(context.Background(), a.DB)
		if err != nil {
			panic(fmt.Sprintf("framework: EntityUI: %v", err))
		}
		host.store = store
	}
	u, err := entityui.New(host, ext)
	if err != nil {
		panic(fmt.Sprintf("framework: EntityUI: %v", err))
	}
	for _, e := range a.Registry.AllSorted() {
		if _, ok := host.APIPath(e); !ok {
			continue
		}
		// On the router the entity's CRUD routes went on: a group's
		// sub-router carries the group's middleware, and a route beside
		// it would skip that guard.
		m := a.crudMounts[e]
		m.r.Post(m.rel+"/_bulk", u.BulkHandler(e.GetName()))
		m.r.Get(m.rel+"/_export.csv", u.ExportHandler(e.GetName()))
	}
	a.entityUI = u
	return u
}

// entityUIMounted reports whether EntityUI mounted e's bulk and export
// routes, for the OpenAPI document.
func (a *App) entityUIMounted(e *entity.Entity) bool {
	if a.entityUI == nil {
		return false
	}
	_, ok := entityUIHost{a: a}.APIPath(e)
	return ok
}

// entityUIHost is entityui's view of one App.
type entityUIHost struct {
	a     *App
	store *sqlBulkStore
}

func (h entityUIHost) Registry() entity.Registry { return h.a.Registry }

func (h entityUIHost) Crud(e *entity.Entity) (*crud.CrudHandler, error) {
	return h.a.CrudHandlerForEntity(e)
}

// APIPath reports the entity's REST base when it mounts write routes: a
// read-only mount (App.View) or CRUD turned off draws read-only screens.
func (h entityUIHost) APIPath(e *entity.Entity) (string, bool) {
	if m := h.a.entityCrudMount(e); !m.Mounted || m.ReadOnly {
		return "", false
	}
	// Where the routes actually mounted: a grouped entity lives under its
	// group's prefix, not the API prefix.
	m, ok := h.a.crudMounts[e]
	if !ok {
		return "", false
	}
	return m.full, true
}

func (h entityUIHost) Translator() *i18n.Translator { return h.a.Translator() }

func (h entityUIHost) Audit() entityui.AuditReader {
	if h.a.auditTable == "" || h.a.DB == nil {
		return nil
	}
	return auditTrail{db: h.a.DB, table: h.a.auditTable}
}

// auditTrail reads one record's audit rows for the Activity tab. The
// record's own read gate has already passed; the rows are further held to
// the caller's tenant, the scope the audit writer stamped.
type auditTrail struct {
	db    *sql.DB
	table string
}

func (t auditTrail) Trail(ctx context.Context, ent, recordID string, limit int) ([]entityui.AuditEntry, error) {
	safe, err := query.SafeIdent(t.table)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	q := fmt.Sprintf("SELECT op, actor_id, created_at, diff, reason FROM %s WHERE entity = $1 AND record_id = $2", query.QuoteIdent(safe))
	args := []any{ent, recordID}
	if tid := tenant.GetTenantID(ctx); tid != "" {
		q += " AND tenant_id = $3"
		args = append(args, tid)
	}
	q += fmt.Sprintf(" ORDER BY created_at DESC LIMIT %d", limit)
	rows, err := t.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []entityui.AuditEntry
	for rows.Next() {
		var (
			op                  string
			actor, diff, reason sql.NullString
			at                  time.Time
		)
		if err := rows.Scan(&op, &actor, &at, &diff, &reason); err != nil {
			return nil, err
		}
		e := entityui.AuditEntry{At: at, Actor: actor.String, Operation: op, Reason: reason.String}
		if diff.Valid {
			var d struct {
				Before map[string]any `json:"old"`
				After  map[string]any `json:"new"`
			}
			if err := json.Unmarshal([]byte(diff.String), &d); err != nil {
				return nil, fmt.Errorf("audit row diff: %w", err)
			}
			e.Before, e.After = d.Before, d.After
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
