package framework

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/core/datexport"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
	"github.com/DonaldMurillo/gofastr/internal/asciicase"
	"github.com/DonaldMurillo/gofastr/internal/fileperm"
)

const (
	exportFormatVersion = "gofastr-data-v2"
	exportFormatV1      = "gofastr-data-v1"
)

const bytesTag = "$bytes"

// ExportOption configures ExportData.
type ExportOption func(*exportConfig)

type exportConfig struct {
	createdAt time.Time
	pageSize  int
}

// WithExportTime stamps the manifest's created_at with t; zero means now.
func WithExportTime(t time.Time) ExportOption {
	return func(c *exportConfig) { c.createdAt = t }
}

// WithExportPageSize sets the rows read per SELECT; non-positive means 1000.
func WithExportPageSize(n int) ExportOption {
	return func(c *exportConfig) { c.pageSize = n }
}

type exportManifest struct {
	Format    string                 `json:"format"`
	CreatedAt string                 `json:"created_at"`
	Entities  []manifestEntry        `json:"entities"`
	Schema    migrate.SchemaSnapshot `json:"schema"`
}

type manifestEntry struct {
	Name       string   `json:"name"`
	Source     string   `json:"source"`
	Table      string   `json:"table"`
	PrimaryKey string   `json:"primary_key"`
	RowCount   int      `json:"row_count"`
	SHA256     string   `json:"sha256"`
	Columns    []string `json:"columns"`
}

type exportSource struct {
	name              string
	source            string
	table             string
	primaryKey        []string
	columns           []string
	selfRefs          []fkEdge
	laterRefs         []fkEdge
	serial            []string
	raw               bool
	timestamps, dates []string
}

// ExportData writes every row of every entity, pivot and registered battery
// table, soft-deleted rows and every physical column included, to dir as
// one <name>.ndjson per source plus manifest.json, all owner-only.
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
	if err := exportDir(dir); err != nil {
		return err
	}

	dialect := migrate.DetectDialect(a.DB)
	sources, err := a.collectSources(ctx)
	if err != nil {
		return err
	}

	manifest := exportManifest{
		Format:    exportFormatVersion,
		CreatedAt: cfg.createdAt.UTC().Format(time.RFC3339Nano),
		Schema:    migrate.SnapshotFromRegistry(a.registryView(), dialect),
	}

	var rd exportReader = a.DB
	var snapshot *sql.TxOptions
	switch {
	case dialect == migrate.DialectPostgres:
		snapshot = &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true}
	case dialect == migrate.DialectSQLite && sqliteWAL(ctx, a.DB):
		snapshot = &sql.TxOptions{ReadOnly: true}
	}
	if snapshot != nil {
		tx, err := a.DB.BeginTx(ctx, snapshot)
		if err != nil {
			return fmt.Errorf("framework: export snapshot: %w", err)
		}
		defer func() { _ = tx.Rollback() }()
		rd = tx
	}

	for _, src := range sources {
		pk := src.primaryKey
		cols := src.columns

		phys, exists, err := physicalTable(ctx, rd, src.table, dialect, !src.raw)
		if err != nil {
			return fmt.Errorf("framework: export probe %q: %w", src.table, err)
		}
		ndjsonPath := filepath.Join(dir, src.name+".ndjson")
		entry := manifestEntry{
			Name: src.name, Source: src.source, Table: src.table,
			PrimaryKey: strings.Join(pk, ","), Columns: cols,
		}
		if !exists {
			fmt.Fprintf(os.Stderr, "framework: export: table %q absent, skipping\n", src.table)
			continue
		}
		w, err := createNDJSON(ndjsonPath)
		if err != nil {
			return fmt.Errorf("framework: export write %q: %w", src.name, err)
		}
		if err := rawReadEach(ctx, rd, dialect, phys, cols, pk, cfg.pageSize, w.write, w.flush); err != nil {
			w.f.Close()
			return fmt.Errorf("framework: export %q: %w", src.table, err)
		}
		sum, err := w.close()
		if err != nil {
			return fmt.Errorf("framework: export write %q: %w", src.name, err)
		}
		entry.RowCount = w.rows
		entry.SHA256 = sum
		manifest.Entities = append(manifest.Entities, entry)
	}

	mb, _ := json.MarshalIndent(manifest, "", "  ")
	if err := fileperm.WriteOwnerOnly(filepath.Join(dir, "manifest.json"), mb); err != nil {
		return fmt.Errorf("framework: export manifest: %w", err)
	}
	return nil
}

func sqliteWAL(ctx context.Context, db *sql.DB) bool {
	var mode string
	if err := db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil {
		return false
	}
	return asciicase.Lower(mode) == "wal"
}

// ImportData restores an ExportData archive verbatim, parents first, in one
// transaction. It refuses an archive whose format, sources, columns, keys or
// checksums do not match the live schema, and then writes nothing.
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
	if manifest.Format != exportFormatVersion && manifest.Format != exportFormatV1 {
		return fmt.Errorf("framework: import: unsupported archive format %q (want %q)",
			manifest.Format, exportFormatVersion)
	}

	live, err := a.collectSources(ctx)
	if err != nil {
		return err
	}
	liveByID := make(map[string]exportSource, len(live))
	liveOrder := make(map[string]int, len(live))
	for i, s := range live {
		liveByID[s.name] = s
		liveOrder[s.name] = i
	}
	dialect := migrate.DetectDialect(a.DB)

	type planned struct {
		src  exportSource
		cols []string
		keys []string
		path string
		sum  string
		phys string
	}
	plans := make([]planned, 0, len(manifest.Entities))
	listed := make(map[string]bool, len(manifest.Entities))
	for _, ent := range manifest.Entities {
		src, ok := liveByID[ent.Name]
		if !ok {
			return fmt.Errorf("framework: import: source %q is not a live entity or registered exporter", ent.Name)
		}
		if listed[ent.Name] {
			return fmt.Errorf("framework: import: source %q is listed twice in the manifest", ent.Name)
		}
		listed[ent.Name] = true
		if ent.Table != src.table {
			return fmt.Errorf("framework: import: source %q archive table %q != live table %q",
				ent.Name, ent.Table, src.table)
		}
		liveCols := make(map[string]bool, len(src.columns))
		for _, c := range src.columns {
			liveCols[c] = true
		}
		cols := make([]string, 0, len(ent.Columns))
		for _, c := range ent.Columns {
			if !liveCols[c] {
				return fmt.Errorf("framework: import: source %q column %q is not in the live schema", ent.Name, c)
			}
			cols = append(cols, c)
		}
		keyCols, err := archiveKeyColumns(src.primaryKey, cols)
		if err != nil {
			return fmt.Errorf("framework: import: source %q: %w", ent.Name, err)
		}
		ndjsonPath := filepath.Join(dir, ent.Name+".ndjson")
		if _, err := os.Stat(ndjsonPath); err != nil {
			return fmt.Errorf("framework: import: read %q: %w", ent.Name, err)
		}
		phys, exists, err := physicalTable(ctx, a.DB, src.table, dialect, !src.raw)
		if err != nil {
			return fmt.Errorf("framework: import: probe %q: %w", src.table, err)
		}
		if !exists {
			return fmt.Errorf("framework: import: source %q table %q is not in the database", ent.Name, src.table)
		}
		plans = append(plans, planned{src: src, cols: cols, keys: keyCols, path: ndjsonPath, sum: ent.SHA256, phys: phys})
	}
	slices.SortStableFunc(plans, func(x, y planned) int {
		return liveOrder[x.src.name] - liveOrder[y.src.name]
	})

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
	type heldRefs struct {
		src  exportSource
		phys string
		refs []deferredRef
	}
	var held []heldRefs
	for _, p := range plans {
		data, err := os.ReadFile(p.path)
		if err != nil {
			return fmt.Errorf("framework: import: read %q: %w", p.src.name, err)
		}
		if sha256Hex(data) != p.sum {
			return fmt.Errorf("framework: import: source %q checksum mismatch (archive corrupt or tampered)", p.src.name)
		}
		rows, err := decodeNDJSON(data)
		if err != nil {
			return fmt.Errorf("framework: import: parse %q: %w", p.src.name, err)
		}
		if err := rowsHaveKeys(rows, p.keys); err != nil {
			return fmt.Errorf("framework: import: source %q: %w", p.src.name, err)
		}
		canonicalTimes(rows, p.src.timestamps, p.src.dates)
		var deferred []deferredRef
		if len(p.src.selfRefs) > 0 {
			rows, deferred = parentRowsFirst(rows, p.src.primaryKey, p.src.selfRefs)
		}
		if len(p.src.laterRefs) > 0 {
			var later []deferredRef
			if rows, later, err = holdLaterRefs(rows, p.src.primaryKey, p.src.laterRefs); err != nil {
				return fmt.Errorf("framework: import: write %q: %w", p.src.name, err)
			}
			held = append(held, heldRefs{src: p.src, phys: p.phys, refs: later})
		}
		if err := rawWriteAll(ctx, tx, p.phys, p.cols, rows); err != nil {
			return fmt.Errorf("framework: import: write %q: %w", p.src.name, err)
		}
		if err := applyDeferredRefs(ctx, tx, p.phys, p.src.primaryKey, deferred); err != nil {
			return fmt.Errorf("framework: import: write %q: %w", p.src.name, err)
		}
		serial := p.src.serial
		if p.src.raw {
			if serial, err = sequencedColumns(ctx, tx, dialect, p.phys, p.cols); err != nil {
				return fmt.Errorf("framework: import: write %q: %w", p.src.name, err)
			}
		}
		if err := resetSerialSequences(ctx, tx, dialect, p.phys, serial); err != nil {
			return fmt.Errorf("framework: import: write %q: %w", p.src.name, err)
		}
	}
	for _, h := range held {
		if err := applyDeferredRefs(ctx, tx, h.phys, h.src.primaryKey, h.refs); err != nil {
			return fmt.Errorf("framework: import: write %q: %w", h.src.name, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("framework: import: commit: %w", err)
	}
	committed = true
	return nil
}

func (a *App) collectSources(ctx context.Context) ([]exportSource, error) {
	var out []exportSource
	seenTable := make(map[string]bool)
	seenName := make(map[string]bool)

	if a.Registry != nil {
		g, err := a.buildDataGraph()
		if err != nil {
			return nil, err
		}
		merged := migrate.UnionEntities(a.registryView())
		names := make([]string, 0, len(merged))
		for n := range merged {
			names = append(names, n)
		}
		sort.Strings(names)
		tableCols := map[string][]string{}
		tableName := map[string]string{}
		tablePK := map[string][]string{}
		tableSerial := map[string][]string{}
		tableTimestamps := map[string][]string{}
		tableDates := map[string][]string{}
		tableSpell := map[string]string{}
		for _, n := range names {
			ent := merged[n]
			table := asciicase.Lower(ent.GetTable())
			if _, first := tableCols[table]; !first {
				tableSpell[table] = ent.GetTable()
				tableName[table] = n
				tablePK[table] = ent.KeyColumns()
				tableCols[table] = []string{}
			}
			for _, f := range ent.GetFields() {
				if f.AutoGenerate == schema.AutoIncrement {
					tableSerial[table] = appendUnique(tableSerial[table], f.Name)
				}
				switch f.Type {
				case schema.Timestamp:
					tableTimestamps[table] = appendUnique(tableTimestamps[table], f.Name)
				case schema.Date:
					tableDates[table] = appendUnique(tableDates[table], f.Name)
				default:
				}
			}
			dedup := make(map[string]bool, len(tableCols[table]))
			for _, c := range tableCols[table] {
				dedup[strings.ToLower(c)] = true
			}
			for _, f := range ent.GetFields() {
				key := strings.ToLower(f.Name)
				if dedup[key] {
					continue
				}
				dedup[key] = true
				tableCols[table] = append(tableCols[table], f.Name)
			}
		}
		pivot := map[string]bool{}
		for _, raw := range g.sorted {
			dt, t := g.tables[raw], asciicase.Lower(raw)
			if !dt.pivot || tableCols[t] != nil {
				continue
			}
			pivot[t] = true
			tableSpell[t] = raw
			tableName[t] = dt.name
			tablePK[t] = dt.key
			tableCols[t] = slices.Clone(dt.key)
		}
		edges := make([]fkEdge, len(g.edges))
		for i, e := range g.edges {
			e.child, e.parent = asciicase.Lower(e.child), asciicase.Lower(e.parent)
			edges[i] = e
		}
		tables := make([]string, 0, len(tableCols))
		for t := range tableCols {
			tables = append(tables, t)
		}
		sort.Strings(tables)
		emitted := map[string]bool{}
		for _, t := range orderTables(tables, edges, true) {
			n := tableName[t]
			src := exportSource{
				name: n, source: "entity",
				table: tableSpell[t], primaryKey: tablePK[t], columns: tableCols[t],
				serial: tableSerial[t], timestamps: tableTimestamps[t], dates: tableDates[t],
			}
			if pivot[t] {
				src.source = "pivot"
			}
			for _, e := range edges {
				switch {
				case e.child != t:
				case e.parent == t:
					src.selfRefs = append(src.selfRefs, e)
				case tableCols[e.parent] != nil && !emitted[e.parent]:
					src.laterRefs = append(src.laterRefs, e)
				}
			}
			emitted[t] = true
			out = append(out, src)
			seenName[n] = true
		}
	}
	dialect := migrate.DetectDialect(a.DB)
	entityByID := make(map[string]int, len(out))
	for i, src := range out {
		id, err := tableIdentity(ctx, a.DB, src.table, dialect, true)
		if err != nil {
			return nil, err
		}
		if j, dup := entityByID[id]; dup {
			return nil, fmt.Errorf("framework: entities %q and %q name one table (%s and %s)",
				out[j].name, src.name, out[j].table, src.table)
		}
		entityByID[id] = i
	}
	for _, ex := range datexport.All() {
		id, err := tableIdentity(ctx, a.DB, ex.Table, dialect, false)
		if err != nil {
			return nil, err
		}
		if i, ok := entityByID[id]; ok {
			out[i].columns = appendFoldUnique(out[i].columns, ex.Columns)
			continue
		}
		if seenTable[id] || seenName[ex.Name] {
			continue
		}
		var pk []string
		if ex.PrimaryKey != "" {
			pk = strings.Split(ex.PrimaryKey, ",")
			for i := range pk {
				pk[i] = strings.TrimSpace(pk[i])
			}
		}
		out = append(out, exportSource{
			name: ex.Name, source: ex.Source,
			table: ex.Table, primaryKey: pk, columns: append([]string(nil), ex.Columns...),
			raw: true,
		})
		seenTable[id] = true
		seenName[ex.Name] = true
	}
	return out, nil
}

func archiveKeyColumns(key, cols []string) ([]string, error) {
	out := make([]string, 0, len(key))
	for _, k := range key {
		i := slices.IndexFunc(cols, func(c string) bool { return asciicase.Lower(c) == asciicase.Lower(k) })
		if i < 0 {
			return nil, fmt.Errorf("the archive does not carry key column %q", k)
		}
		out = append(out, cols[i])
	}
	return out, nil
}

func rowsHaveKeys(rows []map[string]any, keys []string) error {
	for i, row := range rows {
		for _, k := range keys {
			if row[k] == nil {
				return fmt.Errorf("row %d has no value for key column %q", i+1, k)
			}
		}
	}
	return nil
}

type deferredRef struct {
	key  []any
	cols []string
	vals []any
}

func parentRowsFirst(rows []map[string]any, key []string, refs []fkEdge) ([]map[string]any, []deferredRef) {
	idOf := func(row map[string]any, cols []string) (string, bool) {
		vals := make([]any, len(cols))
		for i, c := range cols {
			vals[i] = row[c]
		}
		return keyString(vals)
	}
	index := make(map[string]int, len(rows))
	for i, r := range rows {
		if k, ok := idOf(r, key); ok {
			index[k] = i
		}
	}
	state := make([]int, len(rows))
	out := make([]map[string]any, 0, len(rows))
	var deferred []deferredRef
	var visit func(int)
	visit = func(i int) {
		if state[i] != 0 {
			return
		}
		state[i] = 1
		row := rows[i]
		for _, e := range refs {
			k, ok := idOf(row, e.cols)
			if !ok {
				continue
			}
			j, ok := index[k]
			if !ok || j == i {
				continue
			}
			if state[j] == 1 {
				ref := deferredRef{cols: e.cols, vals: make([]any, len(e.cols))}
				for _, c := range key {
					ref.key = append(ref.key, row[c])
				}
				row = maps.Clone(row)
				for n, c := range e.cols {
					ref.vals[n] = row[c]
					row[c] = nil
				}
				deferred = append(deferred, ref)
				continue
			}
			visit(j)
		}
		state[i] = 2
		out = append(out, row)
	}
	for i := range rows {
		visit(i)
	}
	return out, deferred
}

func holdLaterRefs(rows []map[string]any, key []string, refs []fkEdge) ([]map[string]any, []deferredRef, error) {
	if len(key) == 0 {
		return nil, nil, fmt.Errorf("references to a later table need a primary key to set them by")
	}
	for _, e := range refs {
		for _, c := range e.cols {
			if slices.Contains(key, c) {
				return nil, nil, fmt.Errorf("relation %q closes a cycle of tables through key column %q, which cannot be written empty", e.label, c)
			}
		}
	}
	var deferred []deferredRef
	out := make([]map[string]any, len(rows))
	for i, row := range rows {
		orig := rows[i]
		for _, e := range refs {
			ref := deferredRef{cols: e.cols, vals: make([]any, len(e.cols))}
			set := false
			for n, c := range e.cols {
				ref.vals[n] = orig[c]
				set = set || orig[c] != nil
			}
			if !set {
				continue
			}
			if out[i] == nil {
				row = maps.Clone(row)
			}
			for _, c := range key {
				if orig[c] == nil {
					return nil, nil, fmt.Errorf("relation %q is set on a row whose key column %q is NULL, so it cannot be restored after the row", e.label, c)
				}
				ref.key = append(ref.key, orig[c])
			}
			for _, c := range e.cols {
				row[c] = nil
			}
			out[i] = row
			deferred = append(deferred, ref)
		}
		out[i] = row
	}
	return out, deferred, nil
}

func applyDeferredRefs(ctx context.Context, tx *sql.Tx, table string, key []string, refs []deferredRef) error {
	for _, ref := range refs {
		sets := make([]string, len(ref.cols))
		for i, c := range ref.cols {
			sets[i] = fmt.Sprintf("%s = $%d", query.QuoteFolded(query.MustIdent(c)), i+1)
		}
		where := make([]string, len(key))
		for i, c := range key {
			where[i] = fmt.Sprintf("%s = $%d", query.QuoteFolded(query.MustIdent(c)), len(ref.cols)+i+1)
		}
		stmt := fmt.Sprintf("UPDATE %s SET %s WHERE %s", quotePhysical(table),
			strings.Join(sets, ", "), strings.Join(where, " AND "))
		res, err := tx.ExecContext(ctx, stmt, append(slices.Clone(ref.vals), ref.key...)...)
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err != nil {
			return err
		} else if n != 1 {
			return fmt.Errorf("restoring a reference of %s row %v updated %d rows, want 1", table, ref.key, n)
		}
	}
	return nil
}

type sqlExecer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func resetSerialSequences(ctx context.Context, ex sqlExecer, dialect migrate.Dialect, table string, cols []string) error {
	if dialect != migrate.DialectPostgres {
		return nil
	}
	qt := quotePhysical(table)
	for _, c := range cols {
		qc := query.QuoteFolded(query.MustIdent(c))
		stmt := fmt.Sprintf("SELECT setval(s.seqrelid, GREATEST(COALESCE(m.v, s.seqmin), s.seqmin), m.v IS NOT NULL AND m.v >= s.seqmin) "+
			"FROM pg_sequence s CROSS JOIN (SELECT MAX(%s) AS v FROM %s) m "+
			"WHERE s.seqrelid = pg_get_serial_sequence($1, $2)::regclass", qc, qt)
		if _, err := ex.ExecContext(ctx, stmt, qt, strings.ToLower(c)); err != nil {
			return fmt.Errorf("advance the sequence of %s.%s: %w", table, c, err)
		}
	}
	return nil
}

func sequencedColumns(ctx context.Context, tx *sql.Tx, dialect migrate.Dialect, table string, cols []string) ([]string, error) {
	if dialect != migrate.DialectPostgres {
		return nil, nil
	}
	var out []string
	for _, c := range cols {
		var owned bool
		//gofastr:allow(GOFASTR1502) one catalog read per column of a restored table, not per row
		if err := tx.QueryRowContext(ctx, "SELECT pg_get_serial_sequence($1, $2) IS NOT NULL",
			quotePhysical(table), strings.ToLower(c)).Scan(&owned); err != nil {
			return nil, fmt.Errorf("find the sequence of %s.%s: %w", table, c, err)
		}
		if owned {
			out = append(out, c)
		}
	}
	return out, nil
}

func (a *App) registryView() entity.Registry {
	if a.Registry == nil {
		return nil
	}
	return a.Registry
}

func rawReadEach(ctx context.Context, db exportReader, dialect migrate.Dialect, table string, columns []string, keys []string, pageSize int, emit func(map[string]any) error, pageDone func() error) error {
	qt := quotePhysical(table)
	if len(keys) == 0 {
		return rawReadUnkeyed(ctx, db, dialect, qt, columns, emit)
	}
	readCols := append([]string(nil), columns...)
	for _, k := range keys {
		if !slices.Contains(readCols, k) {
			readCols = append(readCols, k)
		}
	}
	colList, err := exportSelectList(ctx, db, dialect, qt, readCols)
	if err != nil {
		return err
	}
	keyIdx := make([]int, len(keys))
	quotedKeys := make([]string, len(keys))
	marks := make([]string, len(keys))
	for i, k := range keys {
		keyIdx[i] = indexOf(readCols, k)
		quotedKeys[i] = query.QuoteFolded(query.MustIdent(k))
		marks[i] = "$" + strconv.Itoa(i+1)
	}
	orderBy := strings.Join(quotedKeys, ", ")
	after := quotedKeys[0] + " > " + marks[0]
	if len(keys) > 1 {
		after = "(" + orderBy + ") > (" + strings.Join(marks, ", ") + ")"
	}

	lastVals := make([]any, len(keys))
	first := true
	for {
		var q string
		var args []any
		if first {
			q = fmt.Sprintf("SELECT %s FROM %s ORDER BY %s LIMIT $1",
				colList, qt, orderBy)
			args = []any{pageSize}
		} else {
			q = fmt.Sprintf("SELECT %s FROM %s WHERE %s ORDER BY %s LIMIT $%d",
				colList, qt, after, orderBy, len(keys)+1)
			args = append(slices.Clone(lastVals), pageSize)
		}
		nullKey := false
		n, err := scanExportRows(ctx, db, dialect, q, args, len(readCols), columns, emit, func(vals []any) {
			for i, ki := range keyIdx {
				lastVals[i] = vals[ki]
				nullKey = nullKey || vals[ki] == nil
			}
		})
		if err != nil {
			return err
		}
		if nullKey {
			return fmt.Errorf("table %q has a row with a NULL primary key, which the export cannot page past; give the row a key", table)
		}
		if err := pageDone(); err != nil {
			return err
		}
		first = false
		if n < pageSize {
			return nil
		}
	}
}

func rawReadUnkeyed(ctx context.Context, db exportReader, dialect migrate.Dialect, qt string, columns []string, emit func(map[string]any) error) error {
	if len(columns) == 0 {
		return fmt.Errorf("no columns to read for table %q", qt)
	}
	colList, err := exportSelectList(ctx, db, dialect, qt, columns)
	if err != nil {
		return err
	}
	q := fmt.Sprintf("SELECT %s FROM %s", colList, qt)
	_, err = scanExportRows(ctx, db, dialect, q, nil, len(columns), columns, emit, nil)
	return err
}

func exportSelectList(ctx context.Context, db exportReader, dialect migrate.Dialect, qt string, cols []string) (string, error) {
	list := make([]string, len(cols))
	for i, c := range cols {
		list[i] = query.QuoteFolded(query.MustIdent(c))
	}
	if dialect == migrate.DialectPostgres {
		return strings.Join(list, ", "), nil
	}
	rows, err := db.QueryContext(ctx, fmt.Sprintf("SELECT %s FROM %s LIMIT 0", strings.Join(list, ", "), qt))
	if err != nil {
		return "", err
	}
	defer rows.Close()
	types, err := rows.ColumnTypes()
	if err != nil {
		return "", err
	}
	for i := range list {
		switch types[i].DatabaseTypeName() {
		case "DATE", "DATETIME", "TIMESTAMP":
			list[i] = "+" + list[i]
		}
	}
	return strings.Join(list, ", "), rows.Err()
}

func scanExportRows(ctx context.Context, db exportReader, dialect migrate.Dialect, q string, args []any, width int, columns []string, emit func(map[string]any) error, seen func(vals []any)) (int, error) {
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	types, err := rows.ColumnTypes()
	if err != nil {
		return 0, err
	}
	binary := make([]bool, len(columns))
	date := make([]bool, len(columns))
	for i := range columns {
		binary[i] = dialect != migrate.DialectPostgres || strings.EqualFold(types[i].DatabaseTypeName(), "BYTEA")
		date[i] = types[i].DatabaseTypeName() == "DATE"
	}
	n := 0
	for rows.Next() {
		vals := make([]any, width)
		ptrs := make([]any, width)
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return n, err
		}
		row := make(map[string]any, len(columns))
		for i, c := range columns {
			if b, ok := vals[i].([]byte); ok && binary[i] {
				row[c] = map[string]string{bytesTag: base64.StdEncoding.EncodeToString(b)}
				continue
			}
			row[c] = normalizeScan(vals[i], date[i])
		}
		if seen != nil {
			seen(vals)
		}
		if err := emit(row); err != nil {
			return n, err
		}
		n++
	}
	return n, rows.Err()
}

func rawWriteAll(ctx context.Context, tx *sql.Tx, table string, columns []string, rows []map[string]any) error {
	if len(columns) == 0 {
		return fmt.Errorf("no columns to write for table %q", table)
	}
	qt := quotePhysical(table)
	quotedCols := make([]string, len(columns))
	for i, c := range columns {
		quotedCols[i] = query.QuoteFolded(query.MustIdent(c))
	}
	phs := make([]string, len(columns))
	for i := range columns {
		phs[i] = fmt.Sprintf("$%d", i+1)
	}
	stmt := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		qt, strings.Join(quotedCols, ", "), strings.Join(phs, ", "))
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

func normalizeScan(v any, date bool) any {
	switch x := v.(type) {
	case []byte:
		return string(x)
	case time.Time:
		if date {
			return x.Format(time.DateOnly)
		}
		return schema.FormatTimestamp(x)
	default:
		return v
	}
}

func canonicalTimes(rows []map[string]any, timestamps, dates []string) {
	for _, row := range rows {
		for _, c := range timestamps {
			if s, ok := row[c].(string); ok {
				if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
					row[c] = schema.FormatTimestamp(t)
				}
			}
		}
		for _, c := range dates {
			if s, ok := row[c].(string); ok {
				if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
					if day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC); t.Equal(day) {
						row[c] = day.Format(time.DateOnly)
					}
				}
			}
		}
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

func tableExists(ctx context.Context, db *sql.DB, table string, dialect migrate.Dialect) (bool, error) {
	_, ok, err := physicalTable(ctx, db, table, dialect, false)
	return ok, err
}

type rowQueryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type exportReader interface {
	rowQueryer
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func physicalTable(ctx context.Context, db rowQueryer, table string, dialect migrate.Dialect, folded bool) (string, bool, error) {
	physSchema, physRel, ok, err := resolveTable(ctx, db, table, dialect, folded)
	if !ok || err != nil {
		return "", ok, err
	}
	if !strings.Contains(table, ".") {
		return physRel, true, nil
	}
	return physSchema + "." + physRel, true, nil
}

func resolveTable(ctx context.Context, db rowQueryer, table string, dialect migrate.Dialect, folded bool) (string, string, bool, error) {
	name := query.MustIdent(table)
	schemaPart, rel := "", name
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		schemaPart, rel = name[:i], name[i+1:]
	}
	if dialect != migrate.DialectPostgres {
		master, physSchema := "sqlite_master", "main"
		var phys string
		if schemaPart != "" {
			physSchema = schemaPart
			master = query.QuoteFolded(schemaPart) + ".sqlite_master"
			err := db.QueryRowContext(ctx,
				"SELECT name FROM pragma_database_list WHERE name = $1 COLLATE NOCASE", schemaPart).Scan(&phys)
			if err == sql.ErrNoRows {
				return "", "", false, nil
			}
			if err != nil {
				return "", "", false, err
			}
		}
		err := db.QueryRowContext(ctx,
			"SELECT name FROM "+master+" WHERE type = 'table' AND name = $1 COLLATE NOCASE", rel).Scan(&phys)
		if err == sql.ErrNoRows {
			return "", "", false, nil
		}
		if err != nil {
			return "", "", false, err
		}
		return physSchema, phys, true, nil
	}
	spellings := func(s string) (string, string) {
		if folded {
			return asciicase.Lower(s), asciicase.Lower(s)
		}
		return s, asciicase.Lower(s)
	}
	relA, relB := spellings(rel)
	var physSchema, physRel string
	var err error
	if schemaPart == "" {
		err = db.QueryRowContext(ctx,
			"SELECT table_schema, table_name FROM information_schema.tables "+
				"WHERE table_schema = current_schema() AND table_name IN ($1, $2) "+
				"ORDER BY table_name = $1 DESC LIMIT 1", relA, relB).Scan(&physSchema, &physRel)
	} else {
		schemaA, schemaB := spellings(schemaPart)
		err = db.QueryRowContext(ctx,
			"SELECT table_schema, table_name FROM information_schema.tables "+
				"WHERE table_schema IN ($1, $2) AND table_name IN ($3, $4) "+
				"ORDER BY table_schema = $1 DESC, table_name = $3 DESC LIMIT 1",
			schemaA, schemaB, relA, relB).Scan(&physSchema, &physRel)
	}
	if err == sql.ErrNoRows {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, err
	}
	return physSchema, physRel, true, nil
}

func tableIdentity(ctx context.Context, db rowQueryer, table string, dialect migrate.Dialect, folded bool) (string, error) {
	physSchema, physRel, ok, err := resolveTable(ctx, db, table, dialect, folded)
	if err != nil {
		return "", fmt.Errorf("framework: probe %q: %w", table, err)
	}
	if !ok {
		return "declared:" + asciicase.Lower(table), nil
	}
	if dialect != migrate.DialectPostgres {
		return "table:" + asciicase.Lower(physSchema+"."+physRel), nil
	}
	return "table:" + physSchema + "." + physRel, nil
}

func quotePhysical(phys string) string {
	parts := strings.Split(query.MustIdent(phys), ".")
	for i, p := range parts {
		//gofastr:allow(quoteident) phys is the spelling read back from the catalog
		parts[i] = query.QuoteIdent(p)
	}
	return strings.Join(parts, ".")
}

func appendFoldUnique(cols, add []string) []string {
	have := make(map[string]bool, len(cols)+len(add))
	for _, c := range cols {
		have[asciicase.Lower(c)] = true
	}
	for _, c := range add {
		if k := asciicase.Lower(c); !have[k] {
			have[k] = true
			cols = append(cols, c)
		}
	}
	return cols
}

type ndjsonFile struct {
	f    *os.File
	buf  *bufio.Writer
	h    hash.Hash
	enc  *json.Encoder
	rows int
}

func createNDJSON(path string) (*ndjsonFile, error) {
	f, err := fileperm.CreateOwnerOnly(path)
	if err != nil {
		return nil, err
	}
	h := sha256.New()
	buf := bufio.NewWriter(io.MultiWriter(f, h))
	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false)
	return &ndjsonFile{f: f, buf: buf, h: h, enc: enc}, nil
}

func (w *ndjsonFile) write(row map[string]any) error {
	for _, column := range slices.Sorted(maps.Keys(row)) {
		value, floating := row[column].(float64)
		if floating && (math.IsNaN(value) || math.IsInf(value, 0)) {
			return fmt.Errorf("row %d column %q: non-finite float %v cannot be exported as JSON", w.rows+1, column, value)
		}
		if text, ok := row[column].(string); ok && !utf8.ValidString(text) {
			return fmt.Errorf("row %d column %q: text that is not valid UTF-8 cannot be exported as JSON", w.rows+1, column)
		}
	}
	if err := w.enc.Encode(row); err != nil {
		return err
	}
	w.rows++
	return nil
}

func (w *ndjsonFile) flush() error { return w.buf.Flush() }

func (w *ndjsonFile) close() (string, error) {
	if err := w.buf.Flush(); err != nil {
		w.f.Close()
		return "", err
	}
	if err := w.f.Close(); err != nil {
		return "", err
	}
	return hex.EncodeToString(w.h.Sum(nil)), nil
}

func decodeNDJSON(data []byte) ([]map[string]any, error) {
	var out []map[string]any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	for dec.More() {
		var m map[string]any
		if err := dec.Decode(&m); err != nil {
			return nil, err
		}
		for _, k := range slices.Sorted(maps.Keys(m)) {
			switch x := m[k].(type) {
			case json.Number:
				m[k] = jsonNumberValue(x)
			case map[string]any:
				b, err := taggedBytes(x)
				if err != nil {
					return nil, fmt.Errorf("column %q: %w", k, err)
				}
				m[k] = b
			case []any:
				return nil, fmt.Errorf("column %q: an array is not a column value", k)
			}
		}
		out = append(out, m)
	}
	return out, nil
}

func taggedBytes(obj map[string]any) ([]byte, error) {
	s, ok := obj[bytesTag].(string)
	if !ok || len(obj) != 1 {
		return nil, fmt.Errorf("an object value must be {%q: <base64>}", bytesTag)
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("bytes value: %w", err)
	}
	return b, nil
}

func jsonNumberValue(n json.Number) any {
	if i, err := n.Int64(); err == nil {
		return i
	}
	s := n.String()
	if !strings.ContainsAny(s, ".eE") {
		return s
	}
	if f, err := n.Float64(); err == nil {
		return f
	}
	return s
}

func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func exportDir(dir string) error {
	fi, err := os.Lstat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		if err := fileperm.MkdirPrivate(dir); err != nil {
			return fmt.Errorf("framework: export mkdir %q: %w", dir, err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("framework: export dir %q: %w", dir, err)
	}
	if !fi.IsDir() {
		return fmt.Errorf("framework: export dir %q is not a directory (a symlink is refused, not followed)", dir)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("framework: export chmod %q: %w", dir, err)
	}
	if err := fileperm.Restrict(dir, true); err != nil {
		return fmt.Errorf("framework: export restrict %q: %w", dir, err)
	}
	return nil
}
