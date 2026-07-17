package migrate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

// RoutineChecksum returns the lowercase-hex sha256 of a routine's Up body —
// the same identifier the routine ledger records (gofastr_routines.checksum)
// and that introspection (app_routines) compares against the ledger row to
// flag drift. Stable across boots for an unchanged Up; differs on any byte
// change to the SQL. The empty Up ("") has a real checksum (sha256 of the
// empty string), so callers MUST validate non-empty Up separately.
func RoutineChecksum(r Routine) string {
	sum := sha256.Sum256([]byte(r.Up))
	return hex.EncodeToString(sum[:])
}

// upExt / downExt / dialectSuffix encode the filename grammar RoutinesFS
// parses. Keep as constants so the loader and its docstring agree.
const (
	sqlExt          = ".sql"
	downPart        = ".down"
	dialectPostgres = ".pg"
	dialectSQLite   = ".sqlite"
)

// RoutinesFS parses a directory of SQL files into routines. The grammar:
//
//	<name>.sql            → Up, runs on every dialect (Dialect == "")
//	<name>.down.sql       → Down for the routine named <name>
//	<name>.pg.sql         → Up, Dialect == DialectPostgres
//	<name>.sqlite.sql     → Up, Dialect == DialectSQLite
//	<name>.pg.down.sql    → Down for the Postgres-only routine
//	<name>.sqlite.down.sql → Down for the SQLite-only routine
//
// A name MUST NOT have both a plain Up (`<name>.sql`) AND a dialect-suffixed
// Up (`<name>.pg.sql` or `<name>.sqlite.sql`) — that's an authoring error
// (which body is canonical?). Two dialect suffixes for the same name are
// likewise rejected. A name with only a Down file and no Up is an error
// (rollback-without-forward). Empty/whitespace-only Up files and empty
// directories are errors — the framework screams rather than silently
// no-op'ing, the same posture as a misconfigured entity.
//
// Only top-level files in dir are considered. Sub-directories, dotfiles,
// and non-`.sql` files are ignored (README.md, etc.). Routines come back in
// deterministic name-sorted order so the boot plan and generated migrations
// agree across boots.
//
// This is the primary authoring path for stored procedures: write
// `db/routines/compute_totals.pg.sql` with `CREATE OR REPLACE FUNCTION …`
// and call `app.RoutinesFS(embeddedFS, "db/routines")`.
func RoutinesFS(fsys fs.FS, dir string) ([]Routine, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("migrate.RoutinesFS: read %q: %w", dir, err)
	}

	type upFile struct {
		body    string
		dialect Dialect
	}
	ups := make(map[string]upFile, len(entries))
	downs := make(map[string]string, len(entries))

	// Single source-of-truth: classify each .sql file at the top level of dir.
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		// Skip dotfiles (".#foo.sql" editor swaps, ".gitkeep") and non-sql.
		// Only top-level — sub-directories of dir are walked by ReadDir only
		// one level deep, so a nested "routines/v2/x.sql" is naturally ignored.
		if strings.HasPrefix(name, ".") {
			continue
		}
		if !strings.HasSuffix(name, sqlExt) {
			continue
		}

		base := strings.TrimSuffix(name, sqlExt) // "foo" | "foo.down" | "foo.pg" | "foo.pg.down" | "foo.sqlite.down"

		// Split dialect + down markers off the base. The grammar is
		// [<dialect>][.down] trailing the routine name, but the routine name
		// itself may legally contain dots (e.g. "audit.log_fn"). We resolve
		// right-to-left: if the last segment is "down", it's a Down file; if
		// the segment before that (or the last one, when not Down) is a known
		// dialect suffix, it scopes the Up.
		isDown := false
		if strings.HasSuffix(base, downPart) {
			isDown = true
			base = strings.TrimSuffix(base, downPart) // "foo" | "foo.pg" | "foo.sqlite"
		}

		routineName, dialect, dsuffix := splitDialectSuffix(base)
		if dsuffix != "" && dialect == "" {
			// Unknown dialect suffix — reject loud rather than silently
			// treating "foo.mysql.sql" as a name called "foo.mysql".
			return nil, fmt.Errorf("migrate.RoutinesFS: %q: unknown dialect suffix %q (want %s or %s)",
				name, dsuffix, dialectPostgres, dialectSQLite)
		}

		body, err := fs.ReadFile(fsys, dir+"/"+name)
		if err != nil {
			return nil, fmt.Errorf("migrate.RoutinesFS: read %q: %w", name, err)
		}
		trimmed := strings.TrimSpace(string(body))

		if isDown {
			if trimmed == "" {
				return nil, fmt.Errorf("migrate.RoutinesFS: %q: down file is empty", name)
			}
			if _, exists := downs[routineName]; exists {
				return nil, fmt.Errorf("migrate.RoutinesFS: routine %q has more than one down file", routineName)
			}
			downs[routineName] = trimmed
			continue
		}

		// Up file.
		if trimmed == "" {
			return nil, fmt.Errorf("migrate.RoutinesFS: %q: up file is empty (whitespace only)", name)
		}
		existing, collision := ups[routineName]
		if collision {
			// A name with two Ups is one of two authoring errors:
			//  - plain + dialect (e.g. foo.sql + foo.pg.sql)
			//  - two dialects (e.g. foo.pg.sql + foo.sqlite.sql)
			// Both are ambiguous — surface the names so the operator sees
			// exactly which files collide.
			return nil, fmt.Errorf("migrate.RoutinesFS: routine %q has multiple up definitions (declare exactly one of plain/%s%s/%s%s)",
				routineName, dialectPostgres, sqlExt, dialectSQLite, sqlExt)
		}
		_ = existing
		ups[routineName] = upFile{body: trimmed, dialect: dialect}
	}

	if len(ups) == 0 {
		// Either the directory was empty, or it only had non-.sql / dotfiles.
		// Both are screams — a misconfigured embed path that points at the
		// parent of the routines dir silently no-ops today, and that's the
		// exact failure mode the brief asks us to eliminate.
		return nil, fmt.Errorf("migrate.RoutinesFS: no .sql up files found in %q", dir)
	}

	// A Down file with no matching Up is a rollback-without-forward — reject.
	for name := range downs {
		if _, ok := ups[name]; !ok {
			return nil, fmt.Errorf("migrate.RoutinesFS: routine %q has a down file but no up file", name)
		}
	}

	names := make([]string, 0, len(ups))
	for n := range ups {
		names = append(names, n)
	}
	sort.Strings(names)

	out := make([]Routine, 0, len(names))
	for _, n := range names {
		u := ups[n]
		r := Routine{Name: n, Up: u.body, Dialect: u.dialect}
		if d, ok := downs[n]; ok {
			r.Down = d
		}
		out = append(out, r)
	}
	return out, nil
}

// splitDialectSuffix strips a single known dialect suffix (`.pg` or `.sqlite`)
// off the end of base, returning (routineName, dialect, suffixMatched). When
// base ends in a dot-segment that isn't a known dialect, suffixMatched is the
// trailing segment (so the caller can decide whether to reject).
//
// The routine name is everything before the dialect suffix. A name may itself
// contain dots ("audit.log_fn") — we only strip a known suffix, so
// "audit.log_fn.pg" → ("audit.log_fn", DialectPostgres, ".pg") and
// "audit.log_fn.else" → ("audit.log_fn.else", "", ".else") (rejected upstream).
func splitDialectSuffix(base string) (name string, dialect Dialect, suffix string) {
	switch {
	case strings.HasSuffix(base, dialectPostgres):
		return strings.TrimSuffix(base, dialectPostgres), DialectPostgres, dialectPostgres
	case strings.HasSuffix(base, dialectSQLite):
		return strings.TrimSuffix(base, dialectSQLite), DialectSQLite, dialectSQLite
	default:
		// Trailing dot-segment (if any) is reported so an unknown dialect
		// suffix surfaces as a clear error rather than being folded into the
		// routine name.
		if idx := strings.LastIndex(base, "."); idx >= 0 {
			return base, "", base[idx:]
		}
		return base, "", ""
	}
}
