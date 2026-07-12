package framework

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/datexport"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// Data export/import — anti-lock-in.
//
// This is a DATA export: it dumps every entity's rows (plus every registered
// battery table) to a portable archive of NDJSON files + a manifest, and
// restores it with validation. It is distinct from ExportStatic, which
// renders the site to static HTML.
//
// # Fidelity (design decision D1)
//
// Export/import is an operator/admin operation that round-trips data
// FAITHFULLY — original primary keys, timestamps, owner_id, tenant_id, hidden
// columns, and soft-deleted rows included. It therefore reads and writes RAW,
// bypassing the CRUD pipeline (which regenerates ids, stamps tenant/owner,
// drops hidden columns, and filters soft-deleted rows). Regenerating ids on
// import would break every cross-entity foreign key.
//
// # SQL safety (design decision D2)
//
// Table and column names are interpolated into SQL (identifiers can't be
// placeholders) and are therefore whitelisted: every table/column name is
// derived from the registry schema (entity.GetTable / entity.GetFields) or a
// registered DataExporter, and each is passed through core/query.SafeIdent
// before QuoteIdent. An archive's table/column names are NEVER trusted into
// SQL — they are validated against the live known set first and unknown ones
// are rejected. All VALUES are $n bound arguments.
//
// # Format (design decision D3)
//
// The archive is a directory containing one NDJSON file per source
// (<name>.ndjson, one JSON row object per line) plus a manifest.json. Import
// is staged: the whole archive is validated BEFORE any row is written, and
// the write phase runs inside a single transaction that rolls back on any
// error.

// exportFormatVersion is the manifest "format" marker. Bump on incompatible
// manifest-shape changes; ImportData rejects anything else.
const exportFormatVersion = "gofastr-data-v1"

// ExportOption configures ExportData.
type ExportOption func(*exportConfig)

type exportConfig struct {
	createdAt time.Time
	pageSize  int
}

// WithExportTime stamps the manifest's created_at with the given time instead
// of time.Now. Pass a fixed value when deterministic output matters (tests,
// reproducible archives). Zero means "use time.Now().UTC()".
func WithExportTime(t time.Time) ExportOption {
	return func(c *exportConfig) { c.createdAt = t }
}

// WithExportPageSize sets the keyset page size (rows per SELECT). The default
// (1000) bounds memory for large tables. Non-positive falls back to the
// default.
func WithExportPageSize(n int) ExportOption {
	return func(c *exportConfig) { c.pageSize = n }
}

// exportManifest is the archive's manifest.json.
type exportManifest struct {
	Format    string                 `json:"format"`     // exportFormatVersion
	CreatedAt string                 `json:"created_at"` // RFC3339, caller-supplied
	Entities  []manifestEntry        `json:"entities"`
	Schema    migrate.SchemaSnapshot `json:"schema"` // entity-schema fingerprint
}

// manifestEntry describes one dumped source.
type manifestEntry struct {
	Name       string   `json:"name"`        // archive key + ndjson stem
	Source     string   `json:"source"`      // "entity" | battery name
	Table      string   `json:"table"`       // physical table (provenance)
	PrimaryKey string   `json:"primary_key"` // keyset column
	RowCount   int      `json:"row_count"`
	SHA256     string   `json:"sha256"` // hex sha256 of the ndjson file bytes
	Columns    []string `json:"columns"`
}

// exportSource is the unified, internal description of one table to dump or
// restore — whether it came from the entity registry or a DataExporter.
type exportSource struct {
	name       string
	source     string
	table      string
	primaryKey string
	columns    []string
}

// ExportData dumps every entity's rows plus every registered battery table to
// a portable archive under dir: one <name>.ndjson per source plus a
// manifest.json. Reads are raw (all physical columns, all rows including
// soft-deleted), paged by primary-key keyset. See the package comment for the
// fidelity and SQL-safety contracts.
func (a *App) ExportData(ctx context.Context, dir string, opts ...ExportOption) error {
	if a == nil {
		return fmt.Errorf("framework: ExportData on nil App")
	}
	if a.DB == nil {
		return fmt.Errorf("framework: ExportData requires App.DB")
	}
	cfg := exportConfig{pageSize: 1000}
	for _, o := range opts {
		o(&cfg)
	}
	if cfg.pageSize <= 0 {
		cfg.pageSize = 1000
	}
	if cfg.createdAt.IsZero() {
		cfg.createdAt = time.Now().UTC()
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("framework: export mkdir %q: %w", dir, err)
	}

	dialect := migrate.DetectDialect(a.DB)
	sources := a.collectSources()

	manifest := exportManifest{
		Format:    exportFormatVersion,
		CreatedAt: cfg.createdAt.UTC().Format(time.RFC3339Nano),
		Schema:    migrate.SnapshotFromRegistry(a.registryView(), dialect),
	}

	for _, src := range sources {
		// Identifiers come from the registry/exporter and are validated +
		// quoted at the single raw-SQL boundary (rawReadAll / tableExists) via
		// query.MustIdent — a misconfigured registration fails loud (panic)
		// there rather than silently interpolating an unsafe name.
		pk := src.primaryKey
		if pk == "" {
			pk = "id"
		}
		cols := src.columns

		exists, err := tableExists(ctx, a.DB, src.table, dialect)
		if err != nil {
			return fmt.Errorf("framework: export probe %q: %w", src.table, err)
		}
		ndjsonPath := filepath.Join(dir, src.name+".ndjson")
		entry := manifestEntry{
			Name: src.name, Source: src.source, Table: src.table,
			PrimaryKey: pk, Columns: cols,
		}
		if !exists {
			// A registered table that isn't present in this DB (e.g. the auth
			// battery registered but the host didn't create the table, or a
			// renamed table). Skip it from the archive with a note rather than
			// failing the whole export.
			fmt.Fprintf(os.Stderr, "framework: export: table %q absent, skipping\n", src.table)
			continue
		}
		rows, err := rawReadAll(ctx, a.DB, src.table, cols, pk, cfg.pageSize)
		if err != nil {
			return fmt.Errorf("framework: export read %q: %w", src.table, err)
		}
		sum, err := writeNDJSON(ndjsonPath, rows)
		if err != nil {
			return fmt.Errorf("framework: export write %q: %w", src.name, err)
		}
		entry.RowCount = len(rows)
		entry.SHA256 = sum
		manifest.Entities = append(manifest.Entities, entry)
	}

	// MarshalIndent of the manifest struct cannot fail (no unencodable fields).
	mb, _ := json.MarshalIndent(manifest, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), mb, 0o644); err != nil {
		return fmt.Errorf("framework: export manifest: %w", err)
	}
	return nil
}

// ImportData restores an archive written by ExportData into the live database.
// It validates the WHOLE archive before writing a single row (missing
// manifest, unknown source, incompatible columns, checksum mismatch are all
// rejected up front), then writes every source inside a single transaction —
// rolling back on any error. Original ids/timestamps/owner/tenant and
// soft-deleted rows are preserved verbatim.
func (a *App) ImportData(ctx context.Context, dir string) error {
	if a == nil {
		return fmt.Errorf("framework: ImportData on nil App")
	}
	if a.DB == nil {
		return fmt.Errorf("framework: ImportData requires App.DB")
	}
	mb, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return fmt.Errorf("framework: import: read manifest: %w", err)
	}
	var manifest exportManifest
	if err := json.Unmarshal(mb, &manifest); err != nil {
		return fmt.Errorf("framework: import: parse manifest: %w", err)
	}
	if manifest.Format != exportFormatVersion {
		return fmt.Errorf("framework: import: unsupported archive format %q (want %q)",
			manifest.Format, exportFormatVersion)
	}

	// Build the live known-source index: only tables present in the live
	// registry or a registered exporter may be restored. Archive entries that
	// don't match a live source are rejected — a malicious archive's table
	// names never reach this map as keys and therefore never reach SQL.
	live := a.collectSources()
	liveByID := make(map[string]exportSource, len(live))
	for _, s := range live {
		liveByID[s.name] = s
	}

	// ---- Staged validation: nothing is written until every source checks out.
	type planned struct {
		src  exportSource // live source (trusted table + columns)
		cols []string     // archive columns, confirmed ⊆ live.columns
		path string
	}
	plans := make([]planned, 0, len(manifest.Entities))
	for _, ent := range manifest.Entities {
		src, ok := liveByID[ent.Name]
		if !ok {
			return fmt.Errorf("framework: import: source %q is not a live entity or registered exporter", ent.Name)
		}
		if ent.Table != src.table {
			return fmt.Errorf("framework: import: source %q archive table %q != live table %q",
				ent.Name, ent.Table, src.table)
		}
		// Build the live column set (each is SafeIdent-safe by construction).
		liveCols := make(map[string]bool, len(src.columns))
		for _, c := range src.columns {
			liveCols[c] = true
		}
		// Every archive column must be a known live column. This is the
		// injection firewall (D2): a column name smuggled in the archive is
		// rejected here unless it exactly matches a registry/exporter column.
		cols := make([]string, 0, len(ent.Columns))
		for _, c := range ent.Columns {
			if !liveCols[c] {
				return fmt.Errorf("framework: import: source %q column %q is not in the live schema", ent.Name, c)
			}
			cols = append(cols, c)
		}
		ndjsonPath := filepath.Join(dir, ent.Name+".ndjson")
		data, err := os.ReadFile(ndjsonPath)
		if err != nil {
			return fmt.Errorf("framework: import: read %q: %w", ent.Name, err)
		}
		if sum := sha256Hex(data); sum != ent.SHA256 {
			return fmt.Errorf("framework: import: source %q checksum mismatch (archive corrupt or tampered)", ent.Name)
		}
		plans = append(plans, planned{src: src, cols: cols, path: ndjsonPath})
	}

	// ---- Write phase: one transaction, rollback on any error.
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("framework: import: begin tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	for _, p := range plans {
		rows, err := readNDJSON(p.path)
		if err != nil {
			return fmt.Errorf("framework: import: parse %q: %w", p.src.name, err)
		}
		if err := rawWriteAll(ctx, tx, p.src.table, p.cols, rows); err != nil {
			return fmt.Errorf("framework: import: write %q: %w", p.src.name, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("framework: import: commit: %w", err)
	}
	committed = true
	return nil
}

// collectSources enumerates every table to dump/restore: registry entities
// first (alphabetical), then registered DataExporters whose table is not
// already an entity table. Identifiers are SafeIdent-checked at the use sites
// (export read / import write), so a single choke point enforces D2.
func (a *App) collectSources() []exportSource {
	var out []exportSource
	seenTable := make(map[string]bool)
	seenName := make(map[string]bool)

	if a.Registry != nil {
		for _, ent := range a.Registry.AllSorted() {
			cols := make([]string, 0, len(ent.GetFields()))
			for _, f := range ent.GetFields() {
				cols = append(cols, f.Name)
			}
			pk := ent.PrimaryKey
			if pk == "" {
				pk = "id"
			}
			out = append(out, exportSource{
				name: ent.GetName(), source: "entity",
				table: ent.GetTable(), primaryKey: pk, columns: cols,
			})
			seenTable[ent.GetTable()] = true
			seenName[ent.GetName()] = true
		}
	}
	for _, ex := range datexport.All() {
		if seenTable[ex.Table] || seenName[ex.Name] {
			// Already covered by an entity (or a name collision) — registry wins.
			continue
		}
		pk := ex.PrimaryKey
		if pk == "" {
			pk = "id"
		}
		out = append(out, exportSource{
			name: ex.Name, source: ex.Source,
			table: ex.Table, primaryKey: pk, columns: append([]string(nil), ex.Columns...),
		})
		seenTable[ex.Table] = true
		seenName[ex.Name] = true
	}
	return out
}

// registryView returns the App's registry as the entity.Registry interface
// (nil-safe), for migrate.SnapshotFromRegistry.
func (a *App) registryView() entity.Registry {
	if a.Registry == nil {
		return nil
	}
	return a.Registry
}

// rawReadAll reads every physical row of table, all columns, ordered and paged
// by primary-key keyset. No soft-delete filter, no owner/tenant scope — a full
// backup. Scanned values are normalized so the JSON form round-trips: []byte →
// string, time.Time → RFC3339 string; int64/float64/bool/nil pass through.
func rawReadAll(ctx context.Context, db *sql.DB, table string, columns []string, pk string, pageSize int) ([]map[string]any, error) {
	// Identifiers are registry-derived; MustIdent validates + fails fast on the
	// impossible (a malformed name never reaches here from a valid registry).
	qt := query.MustIdent(table)
	qpk := query.MustIdent(pk)
	// readCols is the column list we SELECT. It always includes the keyset
	// column; if pk isn't a declared column we append it for paging only and
	// keep it OUT of the emitted row map.
	readCols := append([]string(nil), columns...)
	hasPK := false
	for _, c := range columns {
		if c == pk {
			hasPK = true
			break
		}
	}
	if !hasPK {
		readCols = append(readCols, pk)
	}
	selList := make([]string, len(readCols))
	for i, c := range readCols {
		selList[i] = query.QuoteIdent(query.MustIdent(c))
	}
	colList := strings.Join(selList, ", ")
	pkSelectIndex := len(readCols) - 1
	if hasPK {
		pkSelectIndex = indexOf(readCols, pk)
	}

	var out []map[string]any
	var lastVal any
	first := true
	for {
		var q string
		var args []any
		if first {
			q = fmt.Sprintf("SELECT %s FROM %s ORDER BY %s LIMIT $1",
				colList, query.QuoteIdent(qt), query.QuoteIdent(qpk))
			args = []any{pageSize}
		} else {
			q = fmt.Sprintf("SELECT %s FROM %s WHERE %s > $1 ORDER BY %s LIMIT $2",
				colList, query.QuoteIdent(qt), query.QuoteIdent(qpk), query.QuoteIdent(qpk))
			args = []any{lastVal, pageSize}
		}
		rows, err := db.QueryContext(ctx, q, args...)
		if err != nil {
			return nil, err
		}
		n := 0
		for rows.Next() {
			vals := make([]any, len(readCols))
			ptrs := make([]any, len(readCols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				rows.Close()
				return nil, err
			}
			row := make(map[string]any, len(columns))
			for i, c := range columns {
				row[c] = normalizeScan(vals[i])
			}
			lastVal = vals[pkSelectIndex]
			out = append(out, row)
			n++
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
		first = false
		if n < pageSize {
			break
		}
	}
	return out, nil
}

// rawWriteAll inserts every row into table (all values verbatim) inside the
// given transaction. The INSERT column list and table name are re-validated
// through SafeIdent here — the single SQL-building choke point — so even a
// caller mistake cannot interpolate an unsafe identifier. All values are $n
// bound arguments.
func rawWriteAll(ctx context.Context, tx *sql.Tx, table string, columns []string, rows []map[string]any) error {
	if len(columns) == 0 {
		return fmt.Errorf("no columns to write for table %q", table)
	}
	qt := query.MustIdent(table)
	quotedCols := make([]string, len(columns))
	for i, c := range columns {
		quotedCols[i] = query.QuoteIdent(query.MustIdent(c))
	}
	phs := make([]string, len(columns))
	for i := range columns {
		phs[i] = fmt.Sprintf("$%d", i+1)
	}
	stmt := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		query.QuoteIdent(qt), strings.Join(quotedCols, ", "), strings.Join(phs, ", "))
	for _, row := range rows {
		args := make([]any, len(columns))
		for i, c := range columns {
			args[i] = row[c]
		}
		if _, err := tx.ExecContext(ctx, stmt, args...); err != nil {
			return err
		}
	}
	return nil
}

// normalizeScan maps driver-returned values to a JSON-stable form.
func normalizeScan(v any) any {
	switch x := v.(type) {
	case []byte:
		return string(x)
	case time.Time:
		return x.UTC().Format(time.RFC3339Nano)
	default:
		return v
	}
}

func indexOf(ss []string, s string) int {
	for i, v := range ss {
		if v == s {
			return i
		}
	}
	return -1
}

// tableExists reports whether table exists in the live DB. Dialect-aware so it
// works on both SQLite and Postgres without raising on a missing table.
func tableExists(ctx context.Context, db *sql.DB, table string, dialect migrate.Dialect) (bool, error) {
	qt := query.MustIdent(table)
	if dialect == migrate.DialectPostgres {
		var ok bool
		err := db.QueryRowContext(ctx,
			"SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = $1)", table).Scan(&ok)
		return ok, err
	}
	// SQLite: probe the sqlite_master catalog by quoted name.
	var name string
	err := db.QueryRowContext(ctx,
		"SELECT name FROM sqlite_master WHERE type = 'table' AND name = $1", qt).Scan(&name)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

// writeNDJSON writes rows one JSON object per line and returns the hex sha256
// of the file bytes. The JSON is buffered first so the checksum and the file
// share one byte stream.
func writeNDJSON(path string, rows []map[string]any) (string, error) {
	var buf strings.Builder
	h := sha256.New()
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	for _, row := range rows {
		if err := enc.Encode(row); err != nil {
			return "", err
		}
	}
	data := []byte(buf.String())
	h.Write(data) // hash.Hash.Write never returns an error (documented)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// readNDJSON reads one JSON object per line into a slice of maps.
func readNDJSON(path string) ([]map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	dec := json.NewDecoder(strings.NewReader(string(data)))
	for dec.More() {
		var m map[string]any
		if err := dec.Decode(&m); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
