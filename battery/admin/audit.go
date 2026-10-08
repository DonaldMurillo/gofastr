package admin

import (
	"context"
	"database/sql"
	"encoding/json"
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
	"github.com/DonaldMurillo/gofastr/framework/entity"
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

// auditPageParam is the Audit log page's page number in its query.
const auditPageParam = "p"

// renderAudit draws the Audit log page: the filter form, then one page
// of the rows it narrows to, newest first, with the pager under them. A
// page turn keeps the filter; a page past the end shows the last one.
func (b *Battery) renderAudit(ctx context.Context, _ map[string]string) render.HTML {
	limit := b.cfg.AuditListLimit
	page := 1
	var f auditFilter
	var warned []string
	carry := url.Values{}
	if r := appui.RequestFromContext(ctx); r != nil {
		q := r.URL.Query()
		limit = parseLimit(q.Get("limit"), b.cfg.AuditListLimit)
		f, warned = b.parseAuditFilter(q)
		if n, err := strconv.Atoi(q.Get(auditPageParam)); err == nil && n > 1 {
			page = n
		}
		carry = q
		carry.Del(auditPageParam)
	}
	header := ui.PageHeader(ui.PageHeaderConfig{
		Title:    i18nui.T(ctx, i18nui.KeyAdminAudit),
		Subtitle: i18nui.T(ctx, i18nui.KeyAdminAuditSub),
	})
	total, err := b.countAuditWhere(ctx, f)
	var rows []auditRow
	pages := 1
	if err == nil {
		pages = pageCount(total, limit)
		page = min(page, pages)
		rows, err = b.queryAuditWhere(ctx, limit, (page-1)*limit, f)
	}
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
	var pager *ui.PaginationConfig
	if pages > 1 {
		pager = &ui.PaginationConfig{
			Page:      page,
			Pages:     pages,
			Path:      b.cfg.PathPrefix + "/audit",
			Query:     carry,
			PageParam: auditPageParam,
			Ctx:       ctx,
		}
	}
	return ui.Stack(ui.StackConfig{Gap: ui.GapLG}, append(above, b.auditTable(ctx, rows, 2, pager))...)
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
	return b.queryAuditWhere(ctx, limit, 0, auditFilter{})
}

// countAuditWhere counts the audit rows the filter narrows to in the
// caller's tenant, for the page's pager.
func (b *Battery) countAuditWhere(ctx context.Context, f auditFilter) (int, error) {
	where, args := auditWhere(ctx, f)
	var n int
	err := b.db.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s`, b.cfg.AuditTable)+where, args...).Scan(&n)
	return n, err
}

// queryAuditWhere reads one page of audit rows in the caller's tenant
// narrowed by the page's validated filter: limit rows after skipping
// offset, newest first, the id breaking a tie so pages never overlap.
func (b *Battery) queryAuditWhere(ctx context.Context, limit, offset int, f auditFilter) ([]auditRow, error) {
	where, args := auditWhere(ctx, f)
	q := fmt.Sprintf(`SELECT id, entity, op, record_id, actor_id, created_at, diff FROM %s`, b.cfg.AuditTable) + where +
		" ORDER BY created_at DESC, id DESC LIMIT " + strconv.Itoa(limit) + " OFFSET " + strconv.Itoa(offset)
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

// auditWhere is the filter's WHERE clause, "" when nothing narrows, and
// its arguments. A tenant-scoped caller sees only rows stamped with
// their tenant (system rows with no tenant included in nobody's scope);
// a caller with no tenant, the platform operator, sees every row. Values
// only ever travel as placeholders, and the built predicate is the same
// text on Postgres and SQLite.
func auditWhere(ctx context.Context, f auditFilter) (string, []any) {
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
	if len(conds) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

// auditTable draws audit rows, with pager under them when it is set. An
// entity row the admin exposes links to its record.
func (b *Battery) auditTable(ctx context.Context, rows []auditRow, emptyLevel int, pager *ui.PaginationConfig) render.HTML {
	cols := []ui.Column{
		{Key: "time", Header: i18nui.T(ctx, i18nui.KeyAdminColTime)},
		{Key: "actor", Header: i18nui.T(ctx, i18nui.KeyAdminColActor)},
		{Key: "op", Header: i18nui.T(ctx, i18nui.KeyAdminColOperation)},
		{Key: "record", Header: i18nui.T(ctx, i18nui.KeyAdminColRecord)},
		{Key: "changes", Header: i18nui.T(ctx, i18nui.KeyAdminColChanges)},
	}
	names := b.actorNames(ctx, rows)
	now := time.Now()
	data := make([]ui.Row, len(rows))
	for i, r := range rows {
		before, after := auditSides(r)
		record, changes := b.auditRecord(ctx, r, before, after)
		data[i] = ui.Row{ID: r.ID, Cells: map[string]render.HTML{
			"time":    agoCell(ctx, now, r.CreatedAt),
			"actor":   render.Text(actorLabel(ctx, names, r)),
			"op":      ui.StatusBadge(ui.StatusBadgeConfig{Label: r.Op, Variant: opVariant(r.Op)}),
			"record":  record,
			"changes": changes,
		}}
	}
	return ui.DataTable(ui.DataTableConfig{
		Columns:       cols,
		Rows:          data,
		Caption:       i18nui.T(ctx, i18nui.KeyAdminAudit),
		CaptionHidden: true,
		// Phone cards keep the record and its changes in view; a scrolled
		// table showed only who and when.
		Responsive: ui.ResponsiveCards,
		Pagination: pager,
		Ctx:        ctx,
		Empty: ui.EmptyStateConfig{
			Title:        i18nui.T(ctx, i18nui.KeyAdminAuditEmpty),
			Description:  i18nui.T(ctx, i18nui.KeyAdminAuditEmptyDesc),
			HeadingLevel: emptyLevel,
		},
	})
}

// auditRecord draws one row's record and what it changed. An exposed
// entity's record reads "Invoice · INV-1010": a live one titled the way
// the record screen's breadcrumb titles it and linked to that screen, a
// deleted or purged one titled from the row's stored copy. An entity the
// admin does not expose shows its table name and the id. The reads run
// elevated: the audit log is behind the admin gate, and the trail must
// name records whatever the entity's own read permission says.
func (b *Battery) auditRecord(ctx context.Context, r auditRow, before, after map[string]any) (record, changes render.HTML) {
	if r.Entity == "access" {
		if record, changes, ok := b.accessAudit(ctx, r); ok {
			return record, changes
		}
	}
	e, ok := b.exposedNamed(r.Entity)
	if ok && r.Op == "bulk" {
		if record, changes, ok := bulkAudit(ctx, e, r); ok {
			return record, changes
		}
	}
	if !ok || b.ui == nil {
		id := html.Code(html.TextConfig{}, render.Text(r.RecordID))
		return render.Join(render.Text(r.Entity+" "), id), ui.EmptyValue()
	}
	var title render.HTML
	switch t, live := b.auditTitle(ctx, e, r, before, after); {
	case live:
		title = ui.Link(ui.LinkConfig{Href: b.entityBase(e) + "/" + pathSegment(r.RecordID), Text: t, Variant: ui.LinkTitle})
	case t != "":
		title = render.Text(t)
	default:
		title = html.Code(html.TextConfig{}, render.Text(r.RecordID))
	}
	record = render.Join(ui.Muted(render.Text(b.singular(ctx, e)+" · ")), title)
	changes = b.ui.Changes(b.elevate(ctx), e.GetName(), before, after)
	if changes == "" {
		changes = ui.EmptyValue()
	}
	return record, changes
}

// accessAudit draws a Roles or User roles row: "Role · billing" with the
// permission granted, revoked or refused, or "User · ada@example.com"
// with the roles set or the one refused. ok is false for a row it has
// no words for, which keeps the generic table name and id.
func (b *Battery) accessAudit(ctx context.Context, r auditRow) (record, changes render.HTML, ok bool) {
	var d struct {
		Permission  string   `json:"permission"`
		Roles       []string `json:"roles"`
		RefusedRole string   `json:"refused_role"`
	}
	if !r.Diff.Valid || json.Unmarshal([]byte(r.Diff.String), &d) != nil {
		return "", "", false
	}
	switch r.Op {
	case "grant", "revoke", "grant-refused", "revoke-refused":
		if d.Permission == "" {
			return "", "", false
		}
		key := map[string]i18nui.Key{
			"grant": i18nui.KeyAdminAuditGranted, "revoke": i18nui.KeyAdminAuditRevoked,
			"grant-refused": i18nui.KeyAdminAuditGrantRefused, "revoke-refused": i18nui.KeyAdminAuditRevokeRefused,
		}[r.Op]
		record = render.Join(ui.Muted(render.Text(i18nui.T(ctx, i18nui.KeyAdminAuditRole)+" · ")), render.Text(r.RecordID))
		return record, i18nui.TVarsHTML(ctx, key, map[string]render.HTML{"permission": ui.InlineCode(d.Permission)}), true
	case "assign-roles", "assign-roles-refused":
		who := b.actorName(ctx, r.RecordID)
		if who == "" {
			who = r.RecordID
		}
		record = render.Join(ui.Muted(render.Text(i18nui.T(ctx, i18nui.KeyAdminAuditUser)+" · ")), render.Text(who))
		if r.Op == "assign-roles-refused" {
			return record, i18nui.TVarsHTML(ctx, i18nui.KeyAdminAuditAssignRefused, map[string]render.HTML{"role": ui.InlineCode(d.RefusedRole)}), true
		}
		if len(d.Roles) == 0 {
			return record, ui.Muted(render.Text(i18nui.T(ctx, i18nui.KeyAdminAuditNoRoles))), true
		}
		tags := make([]render.HTML, len(d.Roles))
		for i, role := range d.Roles {
			tags[i] = ui.Tag(ui.TagConfig{Label: role})
		}
		return record, ui.Cluster(ui.ClusterConfig{Gap: ui.GapXS}, tags...), true
	}
	return "", "", false
}

// bulkAudit draws a bulk run's summary row: "2 payments" and what the run
// did, with what it skipped or failed on. ok is false for a detail that
// does not parse or an action it has no word for.
func bulkAudit(ctx context.Context, e *entity.Entity, r auditRow) (record, changes render.HTML, ok bool) {
	if !r.Diff.Valid {
		return "", "", false
	}
	var d struct {
		Action  string `json:"action"`
		Count   int    `json:"count"`
		Skipped int    `json:"skipped"`
		Failed  int    `json:"failed"`
	}
	if json.Unmarshal([]byte(r.Diff.String), &d) != nil || d.Count < 0 || d.Skipped < 0 || d.Failed < 0 {
		return "", "", false
	}
	var verb i18nui.Key
	switch {
	case d.Action == "delete":
		verb = i18nui.KeyAdminAuditBulkDeleted
	case d.Action == "restore":
		verb = i18nui.KeyAdminAuditBulkRestored
	case strings.HasPrefix(d.Action, "set:"), strings.HasPrefix(d.Action, "move:"):
		verb = i18nui.KeyAdminAuditBulkUpdated
	default:
		return "", "", false
	}
	record = i18nui.TVarsHTML(ctx, i18nui.KeyAdminAuditBulkRecord, map[string]render.HTML{
		"count":  render.Text(strconv.Itoa(d.Count)),
		"entity": render.Text(entityNoun(ctx, e, d.Count)),
	})
	parts := []render.HTML{render.Text(i18nui.T(ctx, verb))}
	if d.Skipped > 0 {
		parts = append(parts, ui.Muted(render.Text(i18nui.TVars(ctx, i18nui.KeyAdminAuditBulkSkipped, map[string]string{"count": strconv.Itoa(d.Skipped)}))))
	}
	if d.Failed > 0 {
		parts = append(parts, ui.Muted(render.Text(i18nui.TVars(ctx, i18nui.KeyAdminAuditBulkFailed, map[string]string{"count": strconv.Itoa(d.Failed)}))))
	}
	return record, ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM}, parts...), true
}

// auditTitle names an exposed entity's audit row record: a live one by
// its title, read the way the record screen's breadcrumb reads it (live
// is true), and a deleted, purged or unreadable one from the row's
// stored copy, its old side else its new. It answers "" when neither
// names it. The reads run elevated, as auditRecord says why.
func (b *Battery) auditTitle(ctx context.Context, e *entity.Entity, r auditRow, before, after map[string]any) (title string, live bool) {
	if b.ui == nil {
		return "", false
	}
	ectx := b.elevate(ctx)
	if r.RecordID != "" && r.Op != "delete" && r.Op != "purge" {
		if t, ok := b.ui.RecordTitle(ectx, e.GetName(), r.RecordID); ok && t != "" {
			return t, true
		}
	}
	snap := before
	if snap == nil {
		snap = after
	}
	if snap == nil {
		return "", false
	}
	if t, ok := b.ui.SnapshotTitle(ectx, e.GetName(), snap); ok {
		return t, false
	}
	return "", false
}

// auditSides is a row's stored old and new values, nil where the row
// stored none or its diff does not parse.
func auditSides(r auditRow) (before, after map[string]any) {
	if !r.Diff.Valid {
		return nil, nil
	}
	var d struct {
		Before map[string]any `json:"old"`
		After  map[string]any `json:"new"`
	}
	if json.Unmarshal([]byte(r.Diff.String), &d) != nil {
		return nil, nil
	}
	return d.Before, d.After
}

// agoCell is a time as the activity feed says it, "2h ago", with the
// exact UTC time on hover and in the datetime attribute.
func agoCell(ctx context.Context, now, t time.Time) render.HTML {
	if t.IsZero() {
		return ui.EmptyValue()
	}
	u := t.UTC()
	return html.Time(html.TimeConfig{Datetime: u.Format(time.RFC3339), ExtraAttrs: html.Attrs{"title": u.Format("2006-01-02 15:04 UTC")}},
		render.Text(ui.Ago(ctx, now, t)))
}

// opKind folds the operations that carry a key onto one name: every
// "transition:<key>" is a transition, and state_override an override.
func opKind(op string) string {
	switch {
	case strings.HasPrefix(op, "transition:"):
		return "transition"
	case op == "state_override":
		return "override"
	}
	return op
}

// opVariant tints an audit operation's badge.
func opVariant(op string) ui.StatusVariant {
	switch opKind(op) {
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

// pageCount is how many pages of size limit hold total rows, at least
// one, so an empty list still draws its first page.
func pageCount(total, limit int) int {
	if limit <= 0 {
		return 1
	}
	return max((total+limit-1)/limit, 1)
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
