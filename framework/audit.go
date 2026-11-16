package framework

import (
	"context"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"

	"slices"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/datexport"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
	"github.com/DonaldMurillo/gofastr/framework/internal/casing"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

// AuditConfig configures WithAuditLog.
type AuditConfig struct {
	// Table is the destination table for audit rows. Defaults to "audit_log".
	Table string

	// Actor resolves the actor id from the request context; "" means none.
	// Nil reads GetID() of the user on the context (handler.GetUser).
	Actor func(context.Context) string

	// Entities restricts auditing to the named entities. Empty means audit
	// every registered entity.
	Entities []string

	// Redact rewrites a copy of the row before it is stored in the diff
	// column; nil returns count as an empty map. On delete it gets
	// {"id": <record_id>} and the "id" it returns, if any, is stored as
	// record_id. Nil drops password-, token-, secret- and key-like fields
	// and stores a sensitive key as "sha256:<hex>".
	Redact func(entity string, row map[string]any) map[string]any
}

type auditOp string

const (
	auditOpCreate auditOp = "create"
	auditOpUpdate auditOp = "update"
	auditOpDelete auditOp = "delete"
)

// EnsureAuditTable creates the audit table ("audit_log" when table is
// empty) or adds the columns an older one lacks. It is idempotent.
func EnsureAuditTable(db *sql.DB, table string) error {
	if table == "" {
		table = "audit_log"
	}
	dialect := migrate.DetectDialect(db)

	tsType := "DATETIME"
	if dialect == migrate.DialectPostgres {
		tsType = "TIMESTAMPTZ"
	}
	safeTable, err := query.SafeIdent(table)
	if err != nil {
		return fmt.Errorf("audit: invalid table name %q: %w", table, err)
	}
	if dialect == migrate.DialectPostgres {
		if err := query.RefuseCaseKeptTables(context.Background(), db, safeTable); err != nil {
			return fmt.Errorf("audit: %w", err)
		}
	}
	stmt := fmt.Sprintf(
		//gofastr:allow(GOFASTR1415) EraseUserData anonymizes this table on its built-in audit plane (actor_id and the diff); registerAuditExport below covers the export
		`CREATE TABLE IF NOT EXISTS %s (
		id          TEXT PRIMARY KEY NOT NULL,
		entity      TEXT NOT NULL,
		op          TEXT NOT NULL,
		record_id   TEXT NOT NULL,
		actor_id    TEXT,
		tenant_id   TEXT,
		created_at  %s NOT NULL,
		diff        TEXT,
		reason      TEXT
	)`, query.QuoteFolded(safeTable), tsType)
	if _, err = db.Exec(stmt); err != nil {
		return err
	}
	live, err := migrate.ReadLiveColumns(context.Background(), db, safeTable, dialect)
	if err != nil {
		return fmt.Errorf("audit: read %s columns: %w", safeTable, err)
	}
	if err := addAuditColumns(db, safeTable, dialect, live); err != nil {
		return err
	}
	registerAuditExport(safeTable)
	return nil
}

func registerAuditExport(table string) {
	datexport.Register(datexport.DataExporter{
		Name: datexport.ScopedName("audit_log", table), Source: "audit", Table: table, PrimaryKey: "id",
		Columns: []string{"id", "entity", "op", "record_id", "actor_id", "tenant_id", "created_at", "diff", "reason"},
	})
}

func addAuditColumns(db *sql.DB, safeTable string, dialect migrate.Dialect, live map[string]string) error {
	for _, col := range []string{"tenant_id", "reason"} {
		if _, ok := live[col]; ok {
			continue
		}
		clause := "ADD COLUMN"
		if dialect == migrate.DialectPostgres {
			clause = "ADD COLUMN IF NOT EXISTS"
		}
		alter := fmt.Sprintf("ALTER TABLE %s %s %s TEXT", query.QuoteFolded(safeTable), clause, col)
		if _, err := db.Exec(alter); err != nil {
			now, rerr := migrate.ReadLiveColumns(context.Background(), db, safeTable, dialect)
			if _, ok := now[col]; rerr != nil || !ok {
				return fmt.Errorf("audit: add %s column: %w", col, err)
			}
		}
	}
	return nil
}

// WithAuditLog writes an audit row, in the write's own transaction, for
// every create, update and delete of the entities registered so far (or
// cfg.Entities). It panics when the app has no DB or the table cannot be
// created.
func (a *App) WithAuditLog(cfg AuditConfig) *App {
	if a.DB == nil {
		panic("framework: WithAuditLog requires a database (use WithDB)")
	}
	table := cfg.Table
	if table == "" {
		table = "audit_log"
	}
	if err := EnsureAuditTable(a.DB, table); err != nil {
		panic(fmt.Sprintf("framework: EnsureAuditTable: %v", err))
	}
	a.auditTable = table
	a.auditActor = cfg.actor
	a.auditCfg = &cfg
	a.modules.auditToggle = func(ctx context.Context, name string, enabled bool) {
		op := "module_disable"
		if enabled {
			op = "module_enable"
		}
		auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := AppendAuditEvent(auditCtx, a.DB, table, "module", op, name, cfg.actor(ctx), nil); err != nil {
			a.Logger().Error("module audit write failed after the change committed",
				"op", op, "module", textsafe.ScrubControlBytes(name), "error", textsafe.ScrubControlBytes(err.Error()))
		}
	}

	want := map[string]bool{}
	for _, name := range cfg.Entities {
		want[name] = true
	}

	for _, ent := range a.Registry.AllSorted() {
		if len(want) == 0 || want[ent.GetName()] {
			ent.MarkAudited()
		}
	}

	for name, ent := range a.Registry.All() {
		if len(want) > 0 && !want[name] {
			continue
		}
		ent := ent
		key := auditKeyOf(a.Registry, name)
		hr := a.HookRegistry(name)

		hr.RegisterHook(hook.AfterCreate, func(ctx context.Context, data any) error {
			row, ok := data.(map[string]any)
			if !ok {
				return fmt.Errorf("audit: AfterCreate payload type = %T, want map[string]any (framework contract drift?)", data)
			}
			id := recordIDOf(ctx, ent.GetName(), row, key)
			redacted := cfg.applyRedact(ent.GetName(), row)
			diff := buildAuditCreateDiff(redacted, row, auditMeta(ctx))
			op := auditOpCreate
			if o := crud.AuditOperationFor(ctx, ent.GetName(), id); o != "" {
				op = auditOp(sanitizeAuditField(o))
			}
			return writeAuditRow(ctx, a.DB, table, ent.GetName(), op, cfg.keyID(key, id), cfg.actor(ctx), diff)
		})
		hr.RegisterHook(hook.AfterUpdate, func(ctx context.Context, data any) error {
			row, ok := data.(map[string]any)
			if !ok {
				return fmt.Errorf("audit: AfterUpdate payload type = %T, want map[string]any (framework contract drift?)", data)
			}
			id := recordIDOf(ctx, ent.GetName(), row, key)
			redactedNew := cfg.applyRedact(ent.GetName(), row)
			var redactedOld map[string]any
			var originalOld map[string]any
			if pre := crud.AuditPreImageFromContext(ctx); pre != nil {
				originalOld = pre
				redactedOld = cfg.applyRedact(ent.GetName(), pre)
			}
			diff := buildAuditUpdateDiff(redactedOld, redactedNew, originalOld, row, auditMeta(ctx))
			op := auditOpUpdate
			if o := crud.AuditOperationFor(ctx, ent.GetName(), id); o != "" {
				op = auditOp(sanitizeAuditField(o))
			}
			return writeAuditRow(ctx, a.DB, table, ent.GetName(), op, cfg.keyID(key, id), cfg.actor(ctx), diff)
		})
		hr.RegisterHook(hook.AfterDelete, func(ctx context.Context, data any) error {
			originalID, ok := data.(string)
			if !ok {
				return fmt.Errorf("audit: AfterDelete payload type = %T, want string (framework contract drift?)", data)
			}
			recordID := cfg.keyID(key, originalID)
			if cfg.Redact != nil {
				redacted := cfg.applyRedact(ent.GetName(), map[string]any{"id": originalID})
				if v, ok := redacted["id"]; ok {
					recordID = sanitizeAuditField(fmt.Sprintf("%v", v))
				}
			}
			var diff []byte
			pre := crud.AuditPreImageFromContext(ctx)
			meta := auditMeta(ctx)
			if pre != nil {
				redactedOld := cfg.applyRedact(ent.GetName(), pre)
				diff = buildAuditDeleteDiff(redactedOld, pre, meta)
			} else if meta != nil {
				diff = buildAuditDeleteDiff(nil, nil, meta)
			}
			op := auditOpDelete
			if o := crud.AuditOperationFor(ctx, ent.GetName(), originalID); o != "" {
				op = auditOp(sanitizeAuditField(o))
			}
			return writeAuditRow(ctx, a.DB, table, ent.GetName(), op, sanitizeAuditField(recordID), cfg.actor(ctx), diff)
		})
	}
	return a
}

type auditKey struct {
	cols   []string
	wires  [][]string
	fields []map[string]bool
}

func versionKey(ent *entity.Entity) auditKey {
	cols := ent.KeyColumns()
	names := make([]string, len(cols))
	for i, c := range cols {
		names[i] = wireKeyOf(ent, c)
	}
	fields := map[string]bool{}
	for _, f := range ent.GetFields() {
		fields[wireKeyOf(ent, f.Name)] = true
	}
	return auditKey{cols: cols, wires: [][]string{names}, fields: []map[string]bool{fields}}
}

func auditKeyOf(reg *Registry, name string) auditKey {
	var k auditKey
	for _, e := range reg.AllSorted() {
		if e.GetName() != name {
			continue
		}
		v := versionKey(e)
		if k.cols == nil {
			k.cols = v.cols
		}
		k.wires = append(k.wires, v.wires...)
		k.fields = append(k.fields, v.fields...)
	}
	return k
}

func (k auditKey) sensitive() bool {
	for i, col := range k.cols {
		if isDefaultSensitiveField(col) {
			return true
		}
		for _, names := range k.wires {
			if i < len(names) && isDefaultSensitiveField(names[i]) {
				return true
			}
		}
	}
	return false
}

func (c AuditConfig) keyID(k auditKey, id string) string {
	id = sanitizeAuditField(id)
	if c.Redact != nil || id == "" || !k.sensitive() {
		return id
	}
	return auditKeyDigest(id)
}

func auditKeyDigest(id string) string {
	sum := sha256.Sum256([]byte(id))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// AuditDigestID reports whether recordID is the "sha256:<hex>" digest
// WithAuditLog stores in place of a sensitive key; a digest names no record.
func AuditDigestID(recordID string) bool {
	hexPart, ok := strings.CutPrefix(recordID, "sha256:")
	if !ok || len(hexPart) != 2*sha256.Size {
		return false
	}
	_, err := hex.DecodeString(hexPart)
	return err == nil
}

func (c AuditConfig) actor(ctx context.Context) string {
	resolve := c.Actor
	if resolve == nil {
		resolve = requestUserID
	}
	var out string
	func() {
		defer func() {
			if r := recover(); r != nil {
				out = ""
			}
		}()
		out = resolve(ctx)
	}()
	return sanitizeAuditField(out)
}

func requestUserID(ctx context.Context) string {
	u, ok := handler.GetUser(ctx)
	if !ok || u == nil {
		return ""
	}
	if id, ok := u.(interface{ GetID() string }); ok {
		return id.GetID()
	}
	return ""
}

func sanitizeAuditField(s string) string {
	s = strings.ToValidUTF8(s, "\uFFFD")
	if s == "" {
		return s
	}
	if !strings.ContainsFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

var defaultSensitiveSuffixes = []string{
	"password",
	"password_hash",
	"passwordhash",
	"secret",
	"token",
	"api_key",
	"apikey",
	"auth_key",
	"authkey",
	"private_key",
	"privatekey",
	"access_token",
	"accesstoken",
	"refresh_token",
	"refreshtoken",
	"session_key",
	"sessionkey",
}

func defaultRedact(row map[string]any) map[string]any {
	if len(row) == 0 {
		return row
	}
	out := make(map[string]any, len(row))
	for k, v := range row {
		if isDefaultSensitiveField(k) {
			continue
		}
		out[k] = v
	}
	return out
}

func isDefaultSensitiveField(name string) bool {
	folded := strings.Map(func(r rune) rune {
		if r == '-' || r == '.' || r == ' ' {
			return '_'
		}
		return r
	}, casing.ToSnake(name))
	for _, form := range [2]string{strings.ToLower(name), strings.ToLower(folded)} {
		for _, suf := range defaultSensitiveSuffixes {
			if form == suf || strings.HasSuffix(form, "_"+suf) {
				return true
			}
		}
	}
	return false
}

func auditMeta(ctx context.Context) map[string]any {
	r := crud.AuditRequestFromContext(ctx)
	if r == nil {
		return nil
	}
	meta := map[string]any{}
	if ip := clientIP(r); ip != "" {
		meta["client_ip"] = ip
	}
	if ua := r.UserAgent(); ua != "" {
		meta["user_agent"] = ua
	}
	if len(meta) == 0 {
		return nil
	}
	return meta
}

func clientIP(r *http.Request) string {
	addr := r.RemoteAddr
	if addr == "" {
		return ""
	}
	if host, _, ok := strings.CutLast(addr, ":"); ok {
		return host
	}
	return addr
}

func buildAuditCreateDiff(redacted, original map[string]any, meta map[string]any) []byte {
	payload := map[string]any{"new": redacted}
	if meta != nil {
		payload["meta"] = meta
	}
	if diff, err := json.Marshal(payload); err == nil {
		return diff
	}
	payload["new"] = original
	if diff, err := json.Marshal(payload); err == nil {
		return diff
	}
	return []byte(`{"new":{}}`)
}

func buildAuditUpdateDiff(redactedOld, redactedNew, originalOld, originalNew map[string]any, meta map[string]any) []byte {
	payload := map[string]any{"new": redactedNew}
	if redactedOld != nil {
		payload["old"] = redactedOld
	}
	if meta != nil {
		payload["meta"] = meta
	}
	if diff, err := json.Marshal(payload); err == nil {
		return diff
	}
	payload["new"] = originalNew
	if diff, err := json.Marshal(payload); err == nil {
		return diff
	}
	if originalOld != nil {
		payload["old"] = originalOld
		if diff, err := json.Marshal(payload); err == nil {
			return diff
		}
	}
	return []byte(`{"new":{}}`)
}

func buildAuditDeleteDiff(redactedOld, originalOld map[string]any, meta map[string]any) []byte {
	if redactedOld == nil && meta == nil {
		return nil
	}
	payload := map[string]any{}
	if redactedOld != nil {
		payload["old"] = redactedOld
	}
	if meta != nil {
		payload["meta"] = meta
	}
	if diff, err := json.Marshal(payload); err == nil {
		return diff
	}
	if originalOld != nil {
		payload["old"] = originalOld
		if diff, err := json.Marshal(payload); err == nil {
			return diff
		}
	}
	return []byte(`{"old":{}}`)
}

func (c AuditConfig) applyRedact(entity string, row map[string]any) map[string]any {
	if c.Redact == nil {
		return defaultRedact(row)
	}
	copied := make(map[string]any, len(row))
	maps.Copy(copied, row)
	var out map[string]any
	panicked := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
			}
		}()
		out = c.Redact(entity, copied)
	}()
	if panicked {
		return row
	}
	if out == nil {
		return map[string]any{}
	}
	return out
}

func recordIDOf(ctx context.Context, entity string, row map[string]any, k auditKey) string {
	if id, ok := crud.AuditRecordIDFor(ctx, entity); ok {
		return id
	}
	return stringifyPK(row, k)
}

func stringifyPK(row map[string]any, k auditKey) string {
	if row == nil {
		return ""
	}
	order := make([]int, len(k.wires))
	score := make([]int, len(k.wires))
	for v := range k.wires {
		order[v] = v
		if v < len(k.fields) {
			for key := range row {
				if k.fields[v][key] {
					score[v]++
				}
			}
		}
	}
	slices.SortStableFunc(order, func(a, b int) int { return score[b] - score[a] })
	for _, v := range order {
		names := k.wires[v]
		wire := map[string]string{}
		for i, c := range k.cols {
			if i < len(names) {
				wire[c] = names[i]
			}
		}
		if id, ok := entity.RowKey(row, k.cols, func(c string) string { return wire[c] }); ok {
			return id
		}
	}
	if id, ok := entity.RowKey(row, k.cols, func(c string) string { return c }); ok {
		return id
	}
	if len(k.cols) == 1 {
		for _, key := range []string{"id", "ID"} {
			if v, ok := row[key]; ok {
				return fmt.Sprintf("%v", v)
			}
		}
	}
	return ""
}

func wireKeyOf(ent *entity.Entity, col string) string {
	for _, f := range ent.GetFields() {
		if f.Name == col && f.WireName != "" {
			return f.WireName
		}
	}
	return casing.ToCamel(col)
}

func writeAuditRow(ctx context.Context, db *sql.DB, table, ent string, op auditOp, recordID, actor string, diff []byte) error {
	var rb [16]byte
	if _, err := cryptorand.Read(rb[:]); err != nil {
		return fmt.Errorf("audit: mint row id: %w", err)
	}
	id := "aud_" + hex.EncodeToString(rb[:])
	var diffArg any
	if diff != nil {
		diffArg = string(diff)
	}
	var exec interface {
		ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	} = db
	if tx, ok := TxFromContext(ctx); ok {
		exec = tx
	}
	safeTable, err := query.SafeIdent(table)
	if err != nil {
		return fmt.Errorf("audit: invalid table name %q: %w", table, err)
	}
	tenantID := sanitizeAuditField(tenant.GetTenantID(ctx))
	if reason := sanitizeAuditField(crud.StateOverrideReason(ctx)); reason != "" {
		_, err = exec.ExecContext(ctx,
			fmt.Sprintf("INSERT INTO %s (id, entity, op, record_id, actor_id, tenant_id, created_at, diff, reason) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)", query.QuoteFolded(safeTable)),
			id, ent, string(op), recordID, nullIfEmpty(actor), nullIfEmpty(tenantID), time.Now().UTC(), diffArg, reason,
		)
		return err
	}
	_, err = exec.ExecContext(ctx,
		fmt.Sprintf("INSERT INTO %s (id, entity, op, record_id, actor_id, tenant_id, created_at, diff) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)", query.QuoteFolded(safeTable)),
		id, ent, string(op), recordID, nullIfEmpty(actor), nullIfEmpty(tenantID), time.Now().UTC(), diffArg,
	)
	return err
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// AppendAuditEvent appends a non-CRUD audit row, such as an auth event,
// with the hooks' row writer. An empty diff is stored as NULL. It writes in
// the transaction on ctx if there is one, else through db.
func AppendAuditEvent(ctx context.Context, db *sql.DB, table, entity, op, recordID, actorID string, diff map[string]any) error {
	if table == "" {
		table = "audit_log"
	}
	var diffBytes []byte
	if len(diff) > 0 {
		b, err := json.Marshal(diff)
		if err != nil {
			return fmt.Errorf("audit: marshal diff: %w", err)
		}
		diffBytes = b
	}
	return writeAuditRow(ctx, db, table,
		sanitizeAuditField(entity),
		auditOp(sanitizeAuditField(op)),
		sanitizeAuditField(recordID),
		sanitizeAuditField(actorID),
		diffBytes,
	)
}
