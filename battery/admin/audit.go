package admin

import (
	"context"
	"database/sql"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
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

	Diff sql.NullString
}

// auditOps are the operations the audit filter offers: the fixed set the
// CRUD hooks and entityui's bulk runs write (crud's create, update,
// delete, restore, purge and state_override, entityui's bulk summary),
// then what the operations pages write (a queue replay, a role grant or
// revoke, a user's role assignment, the module levers). A state
// transition writes "transition:<key>", which no fixed select could
// enumerate, so it is filtered by entity instead; a refused operation
// shows in the unfiltered log.
var auditOps = []string{
	"create", "update", "delete", "restore", "purge", "state_override", "bulk",
	"replay", "grant", "revoke", "assign-roles",
	opModuleEnable, opModuleDisable, opModuleBump, opModuleRevoke,
}

// auditDayLayout is the ?from= and ?to= format the date inputs post. A
// day parses as UTC midnight, so the bounds are UTC days whatever the
// server's zone; the audit page documents it.
const auditDayLayout = "2006-01-02"

// maxAuditActor is the longest ?actor= the page accepts, in bytes.
const maxAuditActor = 200

// auditFilter is the Audit log page's narrowing, every value validated
// before it reaches SQL. to is exclusive: the parsed day plus one, so
// ?to= covers that whole day.
type auditFilter struct {
	actor   string
	entity  string
	op      string
	from    time.Time
	to      time.Time
	hasFrom bool
	hasTo   bool
}

// parseAuditFilter reads the page's query string. An invalid value is
// dropped and its param name reported, so the page never 500s on a
// hand-edited URL and never prints the value it refused. The same
// predicate text runs on both dialects: one query, $n placeholders.
func (b *Battery) parseAuditFilter(q url.Values) (auditFilter, []string) {
	var f auditFilter
	var warned []string
	if a := q.Get("actor"); a != "" {
		if len(a) > maxAuditActor || textsafe.HasControlBytes(a) {
			warned = append(warned, "actor")
		} else {
			f.actor = a
		}
	}
	if e := q.Get("entity"); e != "" {
		if _, ok := b.exposedNamed(e); !ok {
			warned = append(warned, "entity")
		} else {
			f.entity = e
		}
	}
	if op := q.Get("op"); op != "" {
		if !slices.Contains(auditOps, op) {
			warned = append(warned, "op")
		} else {
			f.op = op
		}
	}
	day := func(name string, set func(time.Time)) {
		if raw := q.Get(name); raw != "" {
			if d, err := time.Parse(auditDayLayout, raw); err != nil {
				warned = append(warned, name)
			} else {
				set(d)
			}
		}
	}
	day("from", func(d time.Time) { f.from, f.hasFrom = d, true })
	day("to", func(d time.Time) { f.to, f.hasTo = d.AddDate(0, 0, 1), true })
	return f, warned
}

// renderAudit draws the Audit log page: the filter form, then the rows
// it narrows to.
func (b *Battery) renderAudit(ctx context.Context, _ map[string]string) render.HTML {
	limit := b.cfg.AuditListLimit
	var f auditFilter
	var warned []string
	if r := appui.RequestFromContext(ctx); r != nil {
		limit = parseLimit(r.URL.Query().Get("limit"), b.cfg.AuditListLimit)
		f, warned = b.parseAuditFilter(r.URL.Query())
	}
	header := ui.PageHeader(ui.PageHeaderConfig{
		Title:    i18nui.T(ctx, i18nui.KeyAdminAudit),
		Subtitle: i18nui.T(ctx, i18nui.KeyAdminAuditSub),
	})
	rows, err := b.queryAuditWhere(ctx, limit, f)
	if err != nil {
		// A missing audit table is the usual cause; driver text stays in
		// the log.
		b.logger().Error("admin: load audit rows", "table", b.cfg.AuditTable, "error", err)
		return ui.Stack(ui.StackConfig{Gap: ui.GapLG}, header,
			ui.Callout(ui.CalloutConfig{Variant: ui.StatusDanger}, render.Text(i18nui.T(ctx, i18nui.KeyAdminAuditLoadFailed))))
	}
	above := []render.HTML{header, b.auditFilterForm(ctx, f)}
	for _, param := range warned {
		above = append(above, ui.Callout(ui.CalloutConfig{Variant: ui.StatusWarning},
			render.Text(i18nui.TVars(ctx, i18nui.KeyAdminAuditBadFilter, map[string]string{"param": param}))))
	}
	return ui.Stack(ui.StackConfig{Gap: ui.GapLG}, append(above, b.auditTable(ctx, rows, 2))...)
}

// auditFilterForm is the page's filter: the list toolbar's Filters
// dropdown, a GET form that navigates, so the filter state lives in the
// page's own query string. Entity and operation are facets; the actor
// and the date range ride along as Extra, counted on the badge. Reset
// drops back to the bare page.
func (b *Battery) auditFilterForm(ctx context.Context, f auditFilter) render.HTML {
	entityOpts := []ui.FacetOption{{Value: "", Label: i18nui.T(ctx, i18nui.KeyAdminAuditAnyEntity)}}
	opOpts := []ui.FacetOption{{Value: "", Label: i18nui.T(ctx, i18nui.KeyAdminAuditAnyOp)}}
	for _, e := range b.ents {
		entityOpts = append(entityOpts, ui.FacetOption{Value: e.GetName(), Label: b.plural(ctx, e)})
	}
	for _, op := range auditOps {
		opOpts = append(opOpts, ui.FacetOption{Value: op, Label: op})
	}
	applied := 0
	for _, set := range []bool{f.actor != "", f.hasFrom, f.hasTo} {
		if set {
			applied++
		}
	}
	return ui.FilterToolbar(ui.FilterToolbarConfig{
		Action:   b.cfg.PathPrefix + "/audit",
		Dropdown: true,
		Facets: []ui.Facet{
			{Name: "entity", Label: i18nui.T(ctx, i18nui.KeyAdminColEntity), Options: entityOpts, Value: f.entity},
			{Name: "op", Label: i18nui.T(ctx, i18nui.KeyAdminColOperation), Options: opOpts, Value: f.op},
		},
		Extra: []render.HTML{
			ui.TextField(ui.TextFieldConfig{Name: "actor", Label: i18nui.T(ctx, i18nui.KeyAdminColActor), Value: f.actor}),
			ui.DateField(ui.DateFieldConfig{Name: "from", Label: i18nui.T(ctx, i18nui.KeyAdminAuditFrom), Value: dayValue(f.hasFrom, f.from)}),
			ui.DateField(ui.DateFieldConfig{Name: "to", Label: i18nui.T(ctx, i18nui.KeyAdminAuditTo), Value: dayValue(f.hasTo, f.to.AddDate(0, 0, -1))}),
		},
		Applied: applied,
		Ctx:     ctx,
	})
}

// dayValue formats a parsed filter day back for the date input; "" when
// the filter is off.
func dayValue(set bool, t time.Time) string {
	if !set {
		return ""
	}
	return t.Format(auditDayLayout)
}

// queryAudit reads the newest limit audit rows in the caller's tenant,
// unfiltered: the dashboard's recent activity. The page narrows through
// queryAuditWhere.
func (b *Battery) queryAudit(ctx context.Context, limit int) ([]auditRow, error) {
	return b.queryAuditWhere(ctx, limit, auditFilter{})
}

// queryAuditWhere reads the newest limit audit rows in the caller's
// tenant narrowed by the page's validated filter. A tenant-scoped caller
// sees only rows stamped with their tenant (system rows with no tenant
// included in nobody's scope); a caller with no tenant, the platform
// operator, sees every row. Values only ever travel as placeholders, and
// the built predicate is the same text on Postgres and SQLite.
func (b *Battery) queryAuditWhere(ctx context.Context, limit int, f auditFilter) ([]auditRow, error) {
	q := fmt.Sprintf(`SELECT id, entity, op, record_id, actor_id, created_at, diff FROM %s`, b.cfg.AuditTable)
	var conds []string
	var args []any
	add := func(cond string, v any) {
		conds = append(conds, fmt.Sprintf("%s $%d", cond, len(args)+1))
		args = append(args, v)
	}
	if tid := tenant.GetTenantID(ctx); tid != "" {
		add("tenant_id =", tid)
	}
	if f.actor != "" {
		add("actor_id =", f.actor)
	}
	if f.entity != "" {
		add("entity =", f.entity)
	}
	if f.op != "" {
		add("op =", f.op)
	}
	if f.hasFrom {
		add("created_at >=", f.from)
	}
	if f.hasTo {
		add("created_at <", f.to)
	}
	if len(conds) > 0 {
		q += " WHERE " + strings.Join(conds, " AND ")
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
	names := b.actorNames(ctx, rows)
	data := make([]ui.Row, len(rows))
	for i, r := range rows {
		actor := actorLabel(ctx, names, r)
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
