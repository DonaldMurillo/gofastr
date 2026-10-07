package admin

import (
	"context"
	"database/sql"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"time"

	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// auditRow is one audit entry as the admin reads it. actor_id and diff
// are nullable: system writes carry no actor.
type auditRow struct {
	ID        string
	Entity    string
	Op        string
	RecordID  string
	ActorID   sql.NullString
	CreatedAt time.Time
	Diff      sql.NullString
}

// renderAudit draws the Audit log page.
func (b *Battery) renderAudit(ctx context.Context, _ map[string]string) render.HTML {
	limit := b.cfg.AuditListLimit
	if r := appui.RequestFromContext(ctx); r != nil {
		limit = parseLimit(r.URL.Query().Get("limit"), b.cfg.AuditListLimit)
	}
	header := ui.PageHeader(ui.PageHeaderConfig{
		Title:    i18nui.T(ctx, i18nui.KeyAdminAudit),
		Subtitle: i18nui.T(ctx, i18nui.KeyAdminAuditSub),
	})
	rows, err := b.queryAudit(ctx, limit)
	if err != nil {
		// A missing audit table is the usual cause; driver text stays in
		// the log.
		b.logger().Error("admin: load audit rows", "table", b.cfg.AuditTable, "error", err)
		return ui.Stack(ui.StackConfig{Gap: ui.GapLG}, header,
			ui.Callout(ui.CalloutConfig{Variant: ui.StatusDanger}, render.Text(i18nui.T(ctx, i18nui.KeyAdminAuditLoadFailed))))
	}
	return ui.Stack(ui.StackConfig{Gap: ui.GapLG}, header, b.auditTable(ctx, rows, 2))
}

// queryAudit reads the newest limit audit rows in the caller's tenant.
// A tenant-scoped caller sees only rows stamped with their tenant
// (system rows with no tenant included in nobody's scope); a caller with
// no tenant, the platform operator, sees every row.
func (b *Battery) queryAudit(ctx context.Context, limit int) ([]auditRow, error) {
	q := fmt.Sprintf(`SELECT id, entity, op, record_id, actor_id, created_at, diff FROM %s`, b.cfg.AuditTable)
	var args []any
	if tid := tenant.GetTenantID(ctx); tid != "" {
		q += " WHERE tenant_id = $1"
		args = append(args, tid)
	}
	q += " ORDER BY created_at DESC LIMIT " + strconv.Itoa(limit)
	rows, err := b.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []auditRow
	for rows.Next() {
		var r auditRow
		if err := rows.Scan(&r.ID, &r.Entity, &r.Op, &r.RecordID, &r.ActorID, &r.CreatedAt, &r.Diff); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// auditTable draws audit rows. An entity row the admin exposes links to
// its record.
func (b *Battery) auditTable(ctx context.Context, rows []auditRow, emptyLevel int) render.HTML {
	cols := []ui.Column{
		{Key: "time", Header: i18nui.T(ctx, i18nui.KeyAdminColTime)},
		{Key: "entity", Header: i18nui.T(ctx, i18nui.KeyAdminColEntity)},
		{Key: "op", Header: i18nui.T(ctx, i18nui.KeyAdminColOperation)},
		{Key: "record", Header: i18nui.T(ctx, i18nui.KeyAdminColRecord)},
		{Key: "actor", Header: i18nui.T(ctx, i18nui.KeyAdminColActor)},
	}
	data := make([]ui.Row, len(rows))
	for i, r := range rows {
		actor := i18nui.T(ctx, i18nui.KeyAdminAuditSystem)
		if r.ActorID.Valid && r.ActorID.String != "" {
			actor = r.ActorID.String
		}
		entity := render.Text(r.Entity)
		record := html.Code(html.TextConfig{}, render.Text(r.RecordID))
		if e, ok := b.exposedNamed(r.Entity); ok {
			entity = render.Text(b.plural(ctx, e))
			if r.RecordID != "" && r.Op != "delete" {
				record = ui.Link(ui.LinkConfig{Href: b.entityBase(e) + "/" + pathSegment(r.RecordID), Text: r.RecordID})
			}
		}
		data[i] = ui.Row{ID: r.ID, Cells: map[string]render.HTML{
			"time":   timeCell(r.CreatedAt),
			"entity": entity,
			"op":     ui.StatusBadge(ui.StatusBadgeConfig{Label: r.Op, Variant: opVariant(r.Op)}),
			"record": record,
			"actor":  render.Text(actor),
		}}
	}
	return ui.DataTable(ui.DataTableConfig{
		Columns:       cols,
		Rows:          data,
		Caption:       i18nui.T(ctx, i18nui.KeyAdminAudit),
		CaptionHidden: true,
		Responsive:    ui.ResponsiveScroll,
		Ctx:           ctx,
		Empty: ui.EmptyStateConfig{
			Title:        i18nui.T(ctx, i18nui.KeyAdminAuditEmpty),
			Description:  i18nui.T(ctx, i18nui.KeyAdminAuditEmptyDesc),
			HeadingLevel: emptyLevel,
		},
	})
}

// opVariant tints an audit operation's badge.
func opVariant(op string) ui.StatusVariant {
	switch op {
	case "create", "restore", "grant", "enable":
		return ui.StatusSuccess
	case "delete", "purge", "revoke", "disable":
		return ui.StatusDanger
	case "update", "transition", "override", "replay":
		return ui.StatusInfo
	}
	return ui.StatusNeutral
}

// appendAudit records one ops mutation. The mutation already took
// effect, so a failed write is logged with what it would have recorded,
// never swallowed, and so is a mutation with no audit database to write.
func (b *Battery) appendAudit(ctx context.Context, entity, op, recordID, actorID string, diff map[string]any) {
	if b.db == nil {
		b.logger().Error("admin: no audit database; the change committed unrecorded",
			"entity", entity, "op", op, "record", recordID, "actor", actorID)
		return
	}
	if err := framework.AppendAuditEvent(ctx, b.db, b.cfg.AuditTable, entity, op, recordID, actorID, diff); err != nil {
		b.logger().Error("admin: audit write failed after the change committed",
			"entity", entity, "op", op, "record", recordID, "actor", actorID, "error", err)
	}
}

// parseLimit reads a ?limit= value: the fallback when absent or bad,
// capped at 1000.
func parseLimit(raw string, fallback int) int {
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return fallback
	}
	return min(n, 1000)
}

// sortedKeys returns m's keys in order, for deterministic markup.
func sortedKeys[V any](m map[string]V) []string {
	return slices.Sorted(maps.Keys(m))
}
