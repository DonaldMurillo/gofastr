package migrate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/DonaldMurillo/gofastr/internal/bom"
	"io/fs"
	"path"
	"sort"
	"strings"
)

// RoutineChecksum returns the lowercase-hex sha256 of a routine's Up body, the
// checksum the routine ledger records. An empty Up still has one, so callers
// validate a non-empty Up separately.
func RoutineChecksum(r Routine) string {
	sum := sha256.Sum256([]byte(r.Up))
	return hex.EncodeToString(sum[:])
}

const (
	sqlExt          = ".sql"
	downPart        = ".down"
	dialectPostgres = ".pg"
	dialectSQLite   = ".sqlite"
)

// RoutinesFS parses the top-level .sql files of dir into routines, sorted by
// name. The grammar:
//
//	<name>.sql             Up, every dialect
//	<name>.pg.sql          Up, Postgres only
//	<name>.sqlite.sql      Up, SQLite only
//	<name>[.pg|.sqlite].down.sql  Down for that routine
//
// Dotfiles, sub-directories and other files are ignored. It errors on an
// unknown dialect suffix, a .down not immediately before .sql, an empty file,
// two Ups or two Downs for one name, a Down with no Up or of another dialect,
// and a directory with no Up.
func RoutinesFS(fsys fs.FS, dir string) ([]Routine, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("migrate.RoutinesFS: read %q: %w", dir, err)
	}

	type routineFile struct {
		body    string
		dialect Dialect
	}
	ups := make(map[string]routineFile, len(entries))
	downs := make(map[string]routineFile, len(entries))

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if !strings.HasSuffix(name, sqlExt) {
			continue
		}

		base := strings.TrimSuffix(name, sqlExt)

		isDown := false
		if strings.HasSuffix(base, downPart) {
			isDown = true
			base = strings.TrimSuffix(base, downPart)
		}

		routineName, dialect, dsuffix := splitDialectSuffix(base)
		if strings.HasSuffix(routineName, downPart) {
			return nil, fmt.Errorf("migrate.RoutinesFS: %q: .down must follow the dialect suffix, immediately before .sql", name)
		}
		if dsuffix != "" && dialect == "" {
			return nil, fmt.Errorf("migrate.RoutinesFS: %q: unknown dialect suffix %q (want %s or %s)",
				name, dsuffix, dialectPostgres, dialectSQLite)
		}

		body, err := fs.ReadFile(fsys, path.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("migrate.RoutinesFS: read %q: %w", name, err)
		}
		body = bom.TrimBytes(body)
		trimmed := strings.TrimSpace(string(body))

		if isDown {
			if trimmed == "" {
				return nil, fmt.Errorf("migrate.RoutinesFS: %q: down file is empty", name)
			}
			if _, exists := downs[routineName]; exists {
				return nil, fmt.Errorf("migrate.RoutinesFS: routine %q has more than one down file", routineName)
			}
			downs[routineName] = routineFile{body: trimmed, dialect: dialect}
			continue
		}

		if trimmed == "" {
			return nil, fmt.Errorf("migrate.RoutinesFS: %q: up file is empty (whitespace only)", name)
		}
		existing, collision := ups[routineName]
		if collision {
			return nil, fmt.Errorf("migrate.RoutinesFS: routine %q has multiple up definitions (declare exactly one of plain/%s%s/%s%s)",
				routineName, dialectPostgres, sqlExt, dialectSQLite, sqlExt)
		}
		_ = existing
		ups[routineName] = routineFile{body: trimmed, dialect: dialect}
	}

	if len(ups) == 0 {
		return nil, fmt.Errorf("migrate.RoutinesFS: no .sql up files found in %q", dir)
	}

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
			if d.dialect != "" && d.dialect != u.dialect {
				return nil, fmt.Errorf("migrate.RoutinesFS: routine %q: down dialect %q does not match up dialect %q", n, d.dialect, u.dialect)
			}
			r.Down = d.body
		}
		out = append(out, r)
	}
	return out, nil
}

// splitDialectSuffix strips a known dialect suffix off base; for any other
// trailing dot-segment it returns that segment with an empty dialect.
func splitDialectSuffix(base string) (name string, dialect Dialect, suffix string) {
	switch {
	case strings.HasSuffix(base, dialectPostgres):
		return strings.TrimSuffix(base, dialectPostgres), DialectPostgres, dialectPostgres
	case strings.HasSuffix(base, dialectSQLite):
		return strings.TrimSuffix(base, dialectSQLite), DialectSQLite, dialectSQLite
	default:
		if idx := strings.LastIndex(base, "."); idx >= 0 {
			return base, "", base[idx:]
		}
		return base, "", ""
	}
}
