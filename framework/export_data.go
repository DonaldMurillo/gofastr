package framework

import (
	"bufio"
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
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/datexport"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
	"github.com/DonaldMurillo/gofastr/internal/asciicase"
	"github.com/DonaldMurillo/gofastr/internal/fileperm"
)

// Data export/import: anti-lock-in.
//
// This is a DATA export: it dumps every entity's rows (plus every registered
// battery table) to a portable archive of NDJSON files + a manifest, and
// restores it with validation. It is distinct from ExportStatic, which
// renders the site to static HTML.
//
// # Fidelity (design decision D1)
//
// Export/import is an operator/admin operation that round-trips data
// FAITHFULLY, original primary keys, timestamps, owner_id, tenant_id, hidden
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
// before QuoteFolded. An archive's table/column names are NEVER trusted into
// SQL, they are validated against the live known set first and unknown ones
// are rejected. All VALUES are $n bound arguments.
//
// # Format (design decision D3)
//
// The archive is a directory containing one NDJSON file per source
// (<name>.ndjson, one JSON row object per line) plus a manifest.json. Import
// is staged: the whole archive is validated BEFORE any row is written, and
// the write phase runs inside a single transaction that rolls back on any
// error.
//
// # Order and coverage (design decision D4)
//
// The archive holds every entity table, every many_to_many pivot table
// AutoMigrate creates (source "pivot", named by its table), and every
// registered battery table. Sources are listed, and restored, parents
// first: a table comes after every table its BelongsTo foreign keys
// reference, and a pivot after both its ends, so a restore into an empty
// database never trips a foreign key. Rows of a self-referencing table are
// written parents first too. Two tables that reference each other have no
// such order: the one written first goes in with that reference empty, set
// once every source is in. ImportData orders by the live schema, not the
// archive's listing, so an archive written before this order existed
// restores the same way. On Postgres the sequence behind each
// auto-increment column is moved past the restored ids, so the next insert
// does not collide with one.

// exportFormatVersion is the manifest "format" marker. Bump on incompatible
// manifest-shape changes; ImportData rejects anything it does not read.
//
// v2 writes a binary value (a SQLite BLOB, a Postgres bytea) as the object
// {"$bytes": "<base64>"}: as a JSON string it lost every byte that is not
// UTF-8 and restored as TEXT. A v1 archive holds no objects, and its
// binary values restore as the strings they were written as.
const (
	exportFormatVersion = "gofastr-data-v2"
	exportFormatV1      = "gofastr-data-v1"
)

// bytesTag is the one key of the object a binary value is archived as.
const bytesTag = "$bytes"

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
	Source     string   `json:"source"`      // "entity" | "pivot" | battery name
	Table      string   `json:"table"`       // physical table (provenance)
	PrimaryKey string   `json:"primary_key"` // keyset columns, comma-joined
	RowCount   int      `json:"row_count"`
	SHA256     string   `json:"sha256"` // hex sha256 of the ndjson file bytes
	Columns    []string `json:"columns"`
}

// exportSource is the unified, internal description of one table to dump or
// restore, whether it came from the entity registry or a DataExporter.
type exportSource struct {
	name       string
	source     string
	table      string
	primaryKey []string
	columns    []string
	// selfRefs are the table's foreign keys into itself; a restore writes
	// the row each one names before the row holding it.
	selfRefs []fkEdge
	// laterRefs are the foreign keys into a table restored at or after this
	// one, which only a cycle of tables produces: a restore writes them
	// once every source is in.
	laterRefs []fkEdge
	// serial are the auto-increment columns, whose Postgres sequences a
	// restore moves past the restored ids.
	serial []string
	// raw is a registered battery table: no field declares its
	// auto-increment columns, so a restore asks the catalog for them.
	raw bool
	// timestamps and dates are the columns an entity declares Timestamp
	// and Date, which a restore writes in the layouts crud writes them in
	// (canonicalTimes).
	timestamps, dates []string
}

// ExportData dumps every entity's rows plus every registered battery table to
// a portable archive under dir: one <name>.ndjson per source plus a
// manifest.json. Reads are raw (all physical columns, all rows including
// soft-deleted), paged by primary-key keyset (a table with no key is read in
// one statement). See the package comment for the
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
	// 0700: the archive is a raw dump of every physical column
	// (password hashes, token hashes, sessions on a real app), so it
	// gets the repo's owner-only discipline like every other
	// secret-bearing artifact. The export dir is that artifact, so an
	// existing one is tightened too; a symlink is refused rather than
	// having its target chmodded.
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

	// On Postgres every table is read in one REPEATABLE READ snapshot. Read
	// one by one, a busy app's archive held a comment whose post committed
	// after the posts were read (a restore then trips the foreign key),
	// and keyset paging could chase rows inserted ahead of its cursor
	// without end. A read-only snapshot takes no lock a writer waits on.
	var rd exportReader = a.DB
	if dialect == migrate.DialectPostgres {
		tx, err := a.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
		if err != nil {
			return fmt.Errorf("framework: export snapshot: %w", err)
		}
		defer func() { _ = tx.Rollback() }()
		rd = tx
	}

	for _, src := range sources {
		// Identifiers come from the registry/exporter and are validated +
		// quoted at the single raw-SQL boundary (rawReadAll / tableExists) via
		// query.MustIdent, a misconfigured registration fails loud (panic)
		// there rather than silently interpolating an unsafe name.
		// A raw table may declare no key. It is read in one pass: paging by
		// a guessed "id" read a string literal on SQLite and stopped after
		// the first page.
		pk := src.primaryKey
		cols := src.columns

		// An entity table is spelled as AutoMigrate folded it; a battery
		// table as the catalog has it.
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
			// A registered table that isn't present in this DB (e.g. the auth
			// battery registered but the host didn't create the table, or a
			// renamed table). Skip it from the archive with a note rather than
			// failing the whole export.
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

	// MarshalIndent of the manifest struct cannot fail (no unencodable fields).
	mb, _ := json.MarshalIndent(manifest, "", "  ")
	if err := fileperm.WriteOwnerOnly(filepath.Join(dir, "manifest.json"), mb); err != nil {
		return fmt.Errorf("framework: export manifest: %w", err)
	}
	return nil
}

// ImportData restores an archive written by ExportData into the live database.
// It validates the WHOLE archive before writing a single row (missing
// manifest, unknown source, incompatible columns, checksum mismatch are all
// rejected up front), then writes every source inside a single transaction,
// rolling back on any error, parents before children (decision D4).
// Original ids/timestamps/owner/tenant and soft-deleted rows are preserved
// verbatim.
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

	// Build the live known-source index: only tables present in the live
	// registry or a registered exporter may be restored. Archive entries that
	// don't match a live source are rejected, a malicious archive's table
	// names never reach this map as keys and therefore never reach SQL.
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

	// ---- Staged validation: nothing is written until every source checks out.
	type planned struct {
		src  exportSource // live source (trusted table + columns)
		cols []string     // archive columns, confirmed ⊆ live.columns
		keys []string     // the archive columns holding the primary key
		path string
		phys string // the table as the catalog spells it (physicalTable)
	}
	plans := make([]planned, 0, len(manifest.Entities))
	listed := make(map[string]bool, len(manifest.Entities))
	for _, ent := range manifest.Entities {
		src, ok := liveByID[ent.Name]
		if !ok {
			return fmt.Errorf("framework: import: source %q is not a live entity or registered exporter", ent.Name)
		}
		// A source listed twice names the same file twice: a keyless table
		// took every row twice and committed.
		if listed[ent.Name] {
			return fmt.Errorf("framework: import: source %q is listed twice in the manifest", ent.Name)
		}
		listed[ent.Name] = true
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
		keyCols, err := archiveKeyColumns(src.primaryKey, cols)
		if err != nil {
			return fmt.Errorf("framework: import: source %q: %w", ent.Name, err)
		}
		ndjsonPath := filepath.Join(dir, ent.Name+".ndjson")
		data, err := os.ReadFile(ndjsonPath)
		if err != nil {
			return fmt.Errorf("framework: import: read %q: %w", ent.Name, err)
		}
		if sum := sha256Hex(data); sum != ent.SHA256 {
			return fmt.Errorf("framework: import: source %q checksum mismatch (archive corrupt or tampered)", ent.Name)
		}
		// Resolved before the transaction opens: a single-connection pool
		// would deadlock reading the catalog while the tx holds it.
		phys, exists, err := physicalTable(ctx, a.DB, src.table, dialect, !src.raw)
		if err != nil {
			return fmt.Errorf("framework: import: probe %q: %w", src.table, err)
		}
		if !exists {
			return fmt.Errorf("framework: import: source %q table %q is not in the database", ent.Name, src.table)
		}
		plans = append(plans, planned{src: src, cols: cols, keys: keyCols, path: ndjsonPath, phys: phys})
	}
	// Parents first, by the live schema (decision D4): the archive's own
	// listing may predate that order.
	slices.SortStableFunc(plans, func(x, y planned) int {
		return liveOrder[x.src.name] - liveOrder[y.src.name]
	})

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
	// The references a cycle of tables keeps out of the inserts, set once
	// every source is written.
	type heldRefs struct {
		src  exportSource
		phys string
		refs []deferredRef
	}
	var held []heldRefs
	for _, p := range plans {
		rows, err := readNDJSON(p.path)
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

// collectSources enumerates every table to dump/restore: one source per
// PHYSICAL TABLE, then registered DataExporters whose table is not already an
// entity table. An exporter for an entity table adds the columns the entity
// does not declare to that table's source. Identifiers are SafeIdent-checked
// at the use sites (export read / import write), so a single choke point
// enforces D2.
//
// Registry entities are collapsed through migrate.UnionEntities, the same
// version-union helper migration, snapshot, and the seed runner use, so two
// versions of one entity name produce exactly ONE source (one <name>.ndjson)
// for their shared table. Without this, iterating AllSorted() emits one source
// per version under the same <name>.ndjson path; the second write clobbers the
// first while the manifest gains two entries for one table, and import either
// checksum-mismatches or double-inserts the same primary keys (F3).
//
// Distinct entity names that explicitly target the same physical table are
// also collapsed to one source (the lex-first name) whose column list is the
// union across every entity sharing the table, a faithful backup reads every
// physical column, not just one name's projection. Two entities that spell
// one table differently (users and public.users) are refused: the
// grouping keys on the declared spelling, and the database's resolution
// is what shows them to be one table.
//
// Exporters compare with the entity sources and with each other by the
// table each spelling resolves to (tableIdentity), so an exporter
// configured as public.users joins the users entity's source.
//
// Pivot tables follow (source "pivot"), then every entity and pivot source
// is ordered parents first (decision D4); battery tables come last, in
// registration order.
func (a *App) collectSources(ctx context.Context) ([]exportSource, error) {
	var out []exportSource
	seenTable := make(map[string]bool) // tableIdentity → claimed by an exporter
	seenName := make(map[string]bool)

	if a.Registry != nil {
		g, err := a.buildDataGraph()
		if err != nil {
			return nil, err
		}
		merged := migrate.UnionEntities(a.registryView())
		// Sort by name for a deterministic archive, then group by physical
		// table so each table appears exactly once.
		names := make([]string, 0, len(merged))
		for n := range merged {
			names = append(names, n)
		}
		sort.Strings(names)
		tableCols := map[string][]string{} // table → unioned column names
		tableName := map[string]string{}   // table → lex-first entity name
		tablePK := map[string][]string{}   // table → primary-key columns
		tableSerial := map[string][]string{}
		tableTimestamps := map[string][]string{}
		tableDates := map[string][]string{}
		// The maps are keyed by the folded table: Widgets and widgets are one
		// table to the unquoted DDL, and two sources for it exported its rows
		// twice and failed the restore on the second copy's keys. A source
		// keeps the spelling of its lex-first entity.
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
					// Every other type restores as the archive holds it.
				}
			}
			// Union this entity's columns into the table's set. Duplicates are
			// deduped by lowercased name (Postgres folds unquoted identifiers),
			// preserving the first-seen casing.
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
		// Pivot tables: no entity describes them, so their link columns are
		// their columns and their key.
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
		// Emit one source per table, parents first; tables with no
		// dependency between them stay table-name-sorted for deterministic
		// output.
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
	// Sources compare by the table their spelling resolves to, so users
	// and public.users (main.users on SQLite) are one table: comparing the
	// declared strings exported it twice, and the restore then failed on
	// the second copy's keys.
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
			// An entity already exports this table, and its source keeps
			// it. A battery can own columns the entity does not declare
			// (auth's name column on an entity user table), so those join
			// the entity's column list: skipping the exporter whole lost
			// them on export and restore.
			out[i].columns = appendFoldUnique(out[i].columns, ex.Columns)
			continue
		}
		if seenTable[id] || seenName[ex.Name] {
			// A name collision, or a table an earlier exporter claimed:
			// the first wins.
			continue
		}
		// An empty PrimaryKey means the table has no key: rawReadAll reads
		// it in one statement. Defaulting to "id" paged a keyless SQLite
		// table by the string literal 'id' and stopped after one page. A
		// composite key is comma-joined, the manifest's own spelling.
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

// archiveKeyColumns returns the archive column holding each of a source's
// primary-key columns, matched folded as the unquoted DDL matches them.
// An archive that leaves a key column out is refused: the restore would
// write it NULL, which SQLite admits in a non-integer key (the rows can
// then never be read by key), or let Postgres mint new ids that every
// reference to the old ones misses. A keyless raw table has nothing to
// check.
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

// rowsHaveKeys refuses a row whose key column is null or missing, for the
// reason archiveKeyColumns refuses the column.
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

// deferredRef is a reference a cycle of rows kept out of its row's INSERT:
// the row went in with cols empty, and an UPDATE sets them to vals once
// every row of the table exists.
type deferredRef struct {
	key  []any
	cols []string
	vals []any
}

// parentRowsFirst orders a self-referencing table's rows so each row comes
// after the row its own foreign keys name: keyset order would write a reply
// before the comment it answers whenever the reply's key sorts first, and
// the insert would trip the constraint. Rows keep their order otherwise.
// No insert order satisfies a cycle of rows (two comments naming each
// other), so the reference that closes the cycle is emptied in a copy of
// its row and returned as a deferredRef for applyDeferredRefs.
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
	state := make([]int, len(rows)) // 0 unvisited, 1 in progress, 2 done
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
				// j is waiting on this row: the edge closes a cycle.
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

// holdLaterRefs empties, in a copy of each row, the foreign keys that name a
// table not yet written (a cycle of tables), and returns them as
// deferredRefs to set once every table is in. A table with no key cannot be
// referenced, so it is never in a cycle; one that still reaches here is
// refused rather than written with its references lost.
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
		// Values are read from the row as archived: two references that
		// share a column (a tenant) each keep it, though the first empties
		// it on the row written.
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
					// UPDATE ... WHERE key = NULL would match nothing.
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

// applyDeferredRefs sets the references parentRowsFirst kept out of their
// rows' INSERTs, now that every row they name exists.
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

// sqlExecer is the slice of *sql.Tx the sequence reset needs.
type sqlExecer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// resetSerialSequences moves the Postgres sequence behind each of table's
// auto-increment columns past the restored ids: an INSERT naming the id
// never calls nextval, so without this the next generated id is 1 and
// collides with a restored row. An empty table restarts at the sequence's
// minimum, and so does a table whose ids all sit below it (an id of 0 or a
// negative one): setval refuses a value under the minimum, and the minimum
// is already above every restored id. A column with no sequence joins no
// pg_sequence row and nothing is set. SQLite needs nothing: its rowid
// allocation follows the largest id present.
//
// table is a physicalTable spelling. pg_get_serial_sequence parses its table
// argument as an identifier, so it gets the quoted spelling, case kept; the
// column argument is taken literally and is lowered to match.
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

// sequencedColumns returns the columns of a raw table that own a Postgres
// sequence: a SERIAL or an identity column, which pg_get_serial_sequence
// answers for both. A registered battery table declares no field, so its
// restore would otherwise leave every sequence at 1. Arguments are spelled
// as resetSerialSequences spells them. SQLite has no sequence to find.
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

// registryView returns the App's registry as the entity.Registry interface
// (nil-safe), for migrate.SnapshotFromRegistry.
func (a *App) registryView() entity.Registry {
	if a.Registry == nil {
		return nil
	}
	return a.Registry
}

// rawReadEach hands every physical row of table, all columns, to emit,
// ordered and paged by primary-key keyset, and calls pageDone after each
// page. No soft-delete filter, no owner/tenant scope, a full backup. Only
// the row in hand is held: collecting every page into one slice let a
// large table exhaust memory whatever the page size (2026-10-27 S11).
// Scanned values are normalized so the JSON form round-trips: a binary
// value → {"$bytes": base64}, any other []byte → string, time.Time →
// schema.TimestampLayout (YYYY-MM-DD for a DATE column); int64/float64/
// bool/nil pass through. SQLite time columns are read as stored
// (exportSelectList), so only Postgres hands back a time.Time.
//
// A composite key pages by row value, (a, b) > ($1, $2), which Postgres and
// SQLite both compare column by column. A table with no key has nothing to
// page by and is read in one statement.
func rawReadEach(ctx context.Context, db exportReader, dialect migrate.Dialect, table string, columns []string, keys []string, pageSize int, emit func(map[string]any) error, pageDone func() error) error {
	// table is a physicalTable spelling; quotePhysical validates it and keeps
	// its case.
	qt := quotePhysical(table)
	if len(keys) == 0 {
		return rawReadUnkeyed(ctx, db, dialect, qt, columns, emit)
	}
	// readCols is the column list we SELECT. It always includes the keyset
	// columns; a key column that isn't declared is appended for paging only
	// and kept OUT of the emitted row map.
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
		// SQLite lets a non-INTEGER key hold NULL. Paging past one asks
		// for key > NULL, which matches nothing: the rest of the table
		// was dropped under a valid checksum.
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

// rawReadUnkeyed reads every row of a table with no key in one statement.
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

// exportSelectList is the SELECT list an export reads cols with. On SQLite
// a column declared DATE, DATETIME or TIMESTAMP is read as +c: the driver
// parses those columns' text into time.Time by the declared type, which
// drops the spelling the row stored, and SQLite compares the spelling. A
// restore that wrote back a reformatted value ("...05.12Z" for crud's
// "...05.120000Z", or a battery's "2024-01-03 10:00:00+00:00" as
// "2024-01-03T10:00:00Z") made filters miss it and same-second rows
// reorder (2026-10-27 D12). Unary + returns its operand unchanged, text,
// number or blob, and an expression has no declared type, so the driver
// hands the stored value through. The driver upper-cases declared types.
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

// scanExportRows runs q and hands each row to emit as a map of the first
// len(columns) values, normalized for JSON, and returns how many it read.
// seen, when set, sees each row's raw values first.
//
// A binary value is tagged (see exportFormatVersion). On SQLite every
// []byte the driver returns is a BLOB; on Postgres the driver returns
// numeric, uuid and json as []byte text too, so only a bytea column is.
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

// rawWriteAll inserts every row into table (all values verbatim) inside the
// given transaction. The INSERT column list and table name are re-validated
// through SafeIdent here, the single SQL-building choke point, so even a
// caller mistake cannot interpolate an unsafe identifier. All values are $n
// bound arguments.
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

// normalizeScan maps driver-returned values to a JSON-stable form. An
// instant (Postgres; SQLite reads its time columns as text) leaves in the
// layout crud writes a Timestamp in, or YYYY-MM-DD for a DATE column, so a
// restore into SQLite stores what crud would have.
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

// canonicalTimes rewrites, in place, each RFC 3339 value of a declared
// Timestamp column in schema.TimestampLayout, and each midnight-UTC one of
// a Date column as YYYY-MM-DD. Archives written before v0.96.0 hold the
// trimmed spelling the export used to write, which a SQLite filter or
// cursor bound in the canonical layout does not match. A value of any
// other shape is kept as the archive holds it.
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

// tableExists reports whether table exists in the live DB, spelled as its
// owner may have created it (see physicalTable).
func tableExists(ctx context.Context, db *sql.DB, table string, dialect migrate.Dialect) (bool, error) {
	_, ok, err := physicalTable(ctx, db, table, dialect, false)
	return ok, err
}

// rowQueryer is the slice of *sql.DB and *sql.Tx physicalTable needs.
type rowQueryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// exportReader is what an export reads through: the pool, or on Postgres
// the snapshot transaction ExportData holds.
type exportReader interface {
	rowQueryer
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// physicalTable finds the table a declared name stands for and returns its
// catalog spelling, for quotePhysical. Dialect-aware so it works on both
// SQLite and Postgres without raising on a missing table.
//
// On Postgres a name's spelling depends on the DDL that made it. AutoMigrate
// writes it unquoted, so an entity's Widgets is widgets, and folded says
// the caller knows that. The batteries fold their tables too, but a host
// registers tables its own migrations made, and those may be quoted with
// their case kept ("Jobs"), so otherwise the name matches its own
// spelling first and its folded one second, never another case variant.
// A schema part ("app.widgets") is matched
// the same way; without one the probe reads the current schema only, as
// migrate.ReadLiveColumns does, because a same-named table elsewhere is not
// on the search path the later queries use.
//
// SQLite identifiers have no case, so the name matches without it.
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

// resolveTable is physicalTable's lookup: the schema and name the catalog
// holds the table under. The schema is filled in for a bare name too
// (current_schema() on Postgres, main on SQLite), so two spellings of one
// table, users and public.users, resolve to the same pair.
//
// SQLite keeps one sqlite_master per schema, and its rows name each table
// without the schema, so a qualified name reads its own schema's
// sqlite_master with the bare name.
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
			// A schema the connection has not attached has no
			// sqlite_master to read, and its table is absent.
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
	// spellings is the owner's spelling first, then the folded one.
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

// tableIdentity names the table a declared spelling resolves to, so a bare
// and a schema-qualified spelling of one table compare equal. A table the
// database does not hold keys on its folded declared name: it is never
// exported, and the import refuses it.
func tableIdentity(ctx context.Context, db rowQueryer, table string, dialect migrate.Dialect, folded bool) (string, error) {
	physSchema, physRel, ok, err := resolveTable(ctx, db, table, dialect, folded)
	if err != nil {
		return "", fmt.Errorf("framework: probe %q: %w", table, err)
	}
	if !ok {
		return "declared:" + asciicase.Lower(table), nil
	}
	if dialect != migrate.DialectPostgres {
		// SQLite names have no case: Main.Widgets is main.widgets.
		return "table:" + asciicase.Lower(physSchema+"."+physRel), nil
	}
	return "table:" + physSchema + "." + physRel, nil
}

// quotePhysical quotes a physicalTable spelling as it is, each
// dot-separated part on its own.
func quotePhysical(phys string) string {
	parts := strings.Split(query.MustIdent(phys), ".")
	for i, p := range parts {
		//gofastr:allow(quoteident) phys is the spelling read back from the catalog
		parts[i] = query.QuoteIdent(p)
	}
	return strings.Join(parts, ".")
}

// appendFoldUnique appends each of add that cols does not already hold,
// comparing ASCII-folded as the unquoted DDL does, so EMAIL beside email
// is not read twice.
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

// ndjsonFile writes rows one JSON object per line to an owner-only file and
// hashes the bytes on their way to it, so the checksum and the file share
// one byte stream without the file being held in memory: the export used
// to build the whole serialized table in a buffer first (2026-10-27 S11).
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
	if err := w.enc.Encode(row); err != nil {
		return err
	}
	w.rows++
	return nil
}

// flush hands the buffered rows to the file, once per page.
func (w *ndjsonFile) flush() error { return w.buf.Flush() }

// close flushes and closes the file and returns the hex sha256 of its bytes.
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

// readNDJSON reads one JSON object per line into a slice of maps. Numbers
// decode exactly (see jsonNumberValue): a float64 holds integers only up to
// 2^53, so a BIGINT id or an amount in cents above that would restore as a
// different value. A {"$bytes": base64} object decodes to the []byte it
// tags, which binds as a BLOB or bytea; any other object or array is
// refused.
func readNDJSON(path string) ([]map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.UseNumber()
	for dec.More() {
		var m map[string]any
		if err := dec.Decode(&m); err != nil {
			return nil, err
		}
		for k, v := range m {
			switch x := v.(type) {
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

// taggedBytes decodes the {"$bytes": base64} object a binary value is
// archived as.
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

// jsonNumberValue is the value a restored number binds as: an int64 when the
// number is an integer that fits, a float64 when it is not an integer, and
// its decimal text when it is an integer too large for int64 (the database
// parses the text exactly; a float64 would round it).
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

// exportDir makes dir the export's owner-only directory: a missing one
// is created 0700, an existing directory is tightened to 0700, and a
// symlink (whose target the chmod would rewrite) is refused.
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
