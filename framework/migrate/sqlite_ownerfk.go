package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// StaleOwnerFK is one SQLite table carrying a foreign key on the column its
// entity declares as Scope.OwnerField.
//
// The framework stamps that column from the session identity, which the auth
// battery stores in auth_users — not in the entity the relation names. So the
// key references a table where no matching row will ever exist, and every
// create violates it. Releases before v0.67 emitted the constraint anyway and
// SQLite never checked it, which is why the contradiction stayed invisible;
// Postgres has always rejected it. AutoMigrate no longer emits the clause, but
// that does nothing for a database that already has one, and SQLite has no
// DROP CONSTRAINT.
type StaleOwnerFK struct {
	Entity     string // the declaring entity's name
	Table      string // the SQLite table holding the constraint
	Column     string // the owner column the key is on
	References string // the table the stale key points at
}

// String renders one finding for a CLI report.
func (s StaleOwnerFK) String() string {
	return fmt.Sprintf("%s.%s → %s (entity %q, owner column)", s.Table, s.Column, s.References, s.Entity)
}

// FindStaleOwnerForeignKeys reports every table in the registry whose live
// SQLite schema carries a foreign key on the entity's owner column.
//
// It reads PRAGMA foreign_key_list rather than parsing the stored CREATE TABLE,
// so an inline `REFERENCES` on the column definition is found as reliably as a
// table-level FOREIGN KEY clause — SQLite reports both the same way.
//
// Non-SQLite databases return no findings: Postgres rejected this shape at
// CREATE TABLE time, so no Postgres database can be carrying one.
func FindStaleOwnerForeignKeys(ctx context.Context, db *sql.DB, registry entity.Registry) ([]StaleOwnerFK, error) {
	if db == nil || registry == nil {
		return nil, nil
	}
	dialect, err := DetectDialectStrict(db)
	if err != nil {
		return nil, err
	}
	if dialect != DialectSQLite {
		return nil, nil
	}
	var out []StaleOwnerFK
	for _, ent := range UnionEntities(registry) {
		owner := ent.Config.Scope.OwnerField
		if owner == "" || ent.Config.Unmanaged {
			continue
		}
		found, err := staleOwnerKeysForTable(ctx, db, ent.GetTable(), owner)
		if err != nil {
			return nil, err
		}
		for _, ref := range found {
			out = append(out, StaleOwnerFK{
				Entity:     ent.GetName(),
				Table:      ent.GetTable(),
				Column:     owner,
				References: ref,
			})
		}
	}
	return out, nil
}

// staleOwnerKeysForTable returns the referenced tables of every foreign key on
// ownerCol. A missing table yields nothing rather than an error: AutoMigrate
// creates tables, and a repair scan that ran first has no finding to make.
func staleOwnerKeysForTable(ctx context.Context, db *sql.DB, table, ownerCol string) ([]string, error) {
	safe, err := query.SafeIdent(table)
	if err != nil {
		return nil, fmt.Errorf("repair scan: invalid table name %q: %w", table, err)
	}
	// PRAGMA takes no bind parameters, hence SafeIdent above.
	rows, err := db.QueryContext(ctx, fmt.Sprintf("PRAGMA foreign_key_list(%q)", safe))
	if err != nil {
		return nil, fmt.Errorf("read foreign keys of %s: %w", table, err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var out []string
	for rows.Next() {
		// The column set of this pragma has grown across SQLite versions, so
		// scan positionally into a slice sized to what the driver reports
		// rather than into a fixed struct that a newer column count breaks.
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		byName := map[string]string{}
		for i, c := range cols {
			byName[strings.ToLower(c)] = fmt.Sprintf("%v", vals[i])
		}
		if strings.EqualFold(byName["from"], ownerCol) {
			out = append(out, byName["table"])
		}
	}
	return out, rows.Err()
}

// RepairStaleOwnerForeignKeys rebuilds each affected table without the stale
// key, following SQLite's documented procedure for altering a table it cannot
// ALTER: create the replacement, copy the rows, drop the original, rename.
//
// The replacement DDL is the ORIGINAL DDL with the offending clause removed,
// not a fresh CREATE TABLE built from the entity declaration. A legacy table
// may carry columns, defaults, or constraints the declaration no longer
// mentions, and rebuilding from the declaration would drop them — a repair that
// loses data is worse than the constraint it removes.
//
// Indices and triggers belong to the table and go with the DROP, so their DDL
// is captured first and replayed after the rename.
//
// Foreign keys are disabled for the duration, because the intermediate states
// violate them by construction. PRAGMA foreign_keys is a no-op inside a
// transaction, so it is set on a dedicated connection that also runs the
// rebuild — setting it on the pool and hoping the same connection serves the
// transaction is the version of this that works until it does not.
func RepairStaleOwnerForeignKeys(ctx context.Context, db *sql.DB, stale []StaleOwnerFK) error {
	if len(stale) == 0 {
		return nil
	}
	tables := make([]StaleOwnerFK, 0, len(stale))
	seen := map[string]bool{}
	for _, s := range stale {
		if seen[s.Table] {
			continue
		}
		seen[s.Table] = true
		tables = append(tables, s)
	}

	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	// closed tracks whether the deferred restore already discarded the
	// connection, so the normal path does not double-close it.
	closed := false
	defer func() {
		if !closed {
			_ = conn.Close()
		}
	}()

	if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys=off"); err != nil {
		return fmt.Errorf("disable foreign keys for the rebuild: %w", err)
	}
	// Restoring the pragma is not optional: this connection goes back to the
	// pool and would serve every later write with enforcement silently off —
	// the exact condition v0.67 exists to end, reintroduced by the tool meant
	// to fix it. If the restore fails there is no way to make the connection
	// safe, so it is destroyed instead of returned: a closed connection cannot
	// serve anything, which is the fail-closed answer.
	restore := func() error {
		if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys=on"); err != nil {
			closed = true
			cerr := conn.Close()
			return fmt.Errorf("could not re-enable foreign keys after the rebuild (%w); "+
				"the connection was discarded so it cannot serve unenforced writes (close: %v)", err, cerr)
		}
		return nil
	}

	for _, s := range tables {
		if err := rebuildWithoutOwnerFK(ctx, conn, s); err != nil {
			if rerr := restore(); rerr != nil {
				return fmt.Errorf("repair %s: %w; additionally: %v", s.Table, err, rerr)
			}
			return fmt.Errorf("repair %s: %w", s.Table, err)
		}
	}
	if err := restore(); err != nil {
		return err
	}

	// A rebuild that leaves a violation behind has not repaired anything, and
	// the next write would be the one to find out.
	rows, err := conn.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return fmt.Errorf("verify foreign keys after the rebuild: %w", err)
	}
	defer rows.Close()
	var violations []string
	for rows.Next() {
		var table, rowid, parent, fkid any
		if err := rows.Scan(&table, &rowid, &parent, &fkid); err != nil {
			return err
		}
		violations = append(violations, fmt.Sprintf("%v → %v", table, parent))
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(violations) > 0 {
		return fmt.Errorf("the rebuilt schema still has %d foreign key violation(s): %s — these are dangling rows, not stale constraints; fix or delete them",
			len(violations), strings.Join(violations, ", "))
	}
	return nil
}

// rebuildWithoutOwnerFK performs one table's rebuild inside a transaction, so
// a failure anywhere leaves the original table untouched.
func rebuildWithoutOwnerFK(ctx context.Context, conn *sql.Conn, s StaleOwnerFK) error {
	safe, err := query.SafeIdent(s.Table)
	if err != nil {
		return fmt.Errorf("invalid table name %q: %w", s.Table, err)
	}

	var createSQL string
	err = conn.QueryRowContext(ctx,
		"SELECT sql FROM sqlite_master WHERE type='table' AND name = ?", s.Table).Scan(&createSQL)
	if err == sql.ErrNoRows {
		return fmt.Errorf("table not found")
	}
	if err != nil {
		return err
	}

	// Indices and triggers are dropped with the table. sql IS NULL marks an
	// index SQLite created itself (for UNIQUE or PRIMARY KEY); those come back
	// with the CREATE TABLE and must not be replayed.
	var replays []string
	objRows, err := conn.QueryContext(ctx,
		"SELECT sql FROM sqlite_master WHERE tbl_name = ? AND type IN ('index','trigger') AND sql IS NOT NULL", s.Table)
	if err != nil {
		return err
	}
	for objRows.Next() {
		var ddl string
		if err := objRows.Scan(&ddl); err != nil {
			objRows.Close()
			return err
		}
		replays = append(replays, ddl)
	}
	objRows.Close()
	if err := objRows.Err(); err != nil {
		return err
	}

	newTable := s.Table + "__gofastr_repair"
	newDDL, removed, err := ddlWithoutForeignKeyOn(createSQL, s.Column, newTable)
	if err != nil {
		return err
	}
	if !removed {
		// The pragma said there is a key on this column and the DDL rewriter
		// could not find it. Refusing is the only safe answer: proceeding would
		// copy the table and leave the constraint exactly where it was, and
		// report success.
		return fmt.Errorf("found a foreign key on %q via PRAGMA foreign_key_list but could not locate it in the stored DDL; "+
			"rebuild this table by hand:\n%s", s.Column, createSQL)
	}

	cols, err := tableColumnNames(ctx, conn, s.Table)
	if err != nil {
		return err
	}
	if len(cols) == 0 {
		return fmt.Errorf("no columns found")
	}
	quoted := make([]string, len(cols))
	for i, c := range cols {
		quoted[i] = fmt.Sprintf("%q", c)
	}
	colList := strings.Join(quoted, ", ")

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	stmts := []string{
		newDDL,
		fmt.Sprintf("INSERT INTO %q (%s) SELECT %s FROM %q", newTable, colList, colList, safe),
		fmt.Sprintf("DROP TABLE %q", safe),
		fmt.Sprintf("ALTER TABLE %q RENAME TO %q", newTable, safe),
	}
	stmts = append(stmts, replays...)
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("%w\nwhile running: %s", err, stmt)
		}
	}
	return tx.Commit()
}

// tableColumnNames returns the table's columns in declaration order, so the
// copy names them explicitly instead of relying on SELECT * lining up.
func tableColumnNames(ctx context.Context, conn *sql.Conn, table string) ([]string, error) {
	safe, err := query.SafeIdent(table)
	if err != nil {
		return nil, err
	}
	rows, err := conn.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%q)", safe))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var out []string
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		for i, c := range cols {
			if strings.EqualFold(c, "name") {
				out = append(out, fmt.Sprintf("%v", vals[i]))
			}
		}
	}
	return out, rows.Err()
}

// ddlWithoutForeignKeyOn rewrites a stored CREATE TABLE statement, removing the
// foreign key on ownerCol and renaming the table to newName. It reports whether
// a key was actually removed, so the caller can refuse rather than rebuild a
// table into the same shape and call it repaired.
//
// Two spellings have to be handled, because SQLite accepts both and reports
// them identically through PRAGMA foreign_key_list:
//
//	owner_id TEXT REFERENCES users(id)          -- column-level
//	FOREIGN KEY (owner_id) REFERENCES users(id) -- table-level
//
// AutoMigrate emitted the table-level form; a hand-written legacy schema may
// use either. The column-level form is stripped from the column definition and
// the rest of it — type, NOT NULL, DEFAULT, COLLATE — is preserved, because
// this DDL is the only record of what the table actually is.
//
// The scan is quote- and paren-aware. A column named "references", a DEFAULT
// holding the text 'FOREIGN KEY', and a CHECK constraint containing commas all
// appear in real schemas, and a split on a naive strings.Split(",") mangles the
// table instead of repairing it.
func ddlWithoutForeignKeyOn(createSQL, ownerCol, newName string) (string, bool, error) {
	open := indexAtDepthZero(createSQL, '(')
	if open < 0 {
		return "", false, fmt.Errorf("stored DDL has no column list:\n%s", createSQL)
	}
	closeIdx := matchingParen(createSQL, open)
	if closeIdx < 0 {
		return "", false, fmt.Errorf("stored DDL has an unbalanced column list:\n%s", createSQL)
	}
	body := createSQL[open+1 : closeIdx]
	tail := createSQL[closeIdx+1:] // WITHOUT ROWID, STRICT, …

	items := splitTopLevel(body)
	kept := make([]string, 0, len(items))
	removed := false
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		if isTableLevelFKOn(trimmed, ownerCol) {
			removed = true
			continue
		}
		if rewritten, dropped := columnDefWithoutReferences(trimmed, ownerCol); dropped {
			removed = true
			kept = append(kept, rewritten)
			continue
		}
		kept = append(kept, trimmed)
	}
	if !removed {
		return "", false, nil
	}
	ddl := fmt.Sprintf("CREATE TABLE %q (\n  %s\n)%s", newName, strings.Join(kept, ",\n  "), tail)
	return ddl, true, nil
}

// isTableLevelFKOn reports whether item is a table constraint of the form
// [CONSTRAINT name] FOREIGN KEY (ownerCol) REFERENCES … — and only when the
// key's column list is exactly ownerCol. A composite key that happens to
// include the owner column is left alone: dropping it would remove a
// constraint on the other columns too, which nobody asked for.
func isTableLevelFKOn(item, ownerCol string) bool {
	rest := item
	if up := strings.ToUpper(rest); strings.HasPrefix(up, "CONSTRAINT") {
		// Skip "CONSTRAINT <name>".
		fields := strings.Fields(rest)
		if len(fields) < 2 {
			return false
		}
		rest = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(rest[len(fields[0]):]), fields[1]))
	}
	if !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(rest)), "FOREIGN KEY") {
		return false
	}
	rest = strings.TrimSpace(rest)
	open := strings.IndexByte(rest, '(')
	if open < 0 {
		return false
	}
	closeIdx := matchingParen(rest, open)
	if closeIdx < 0 {
		return false
	}
	cols := splitTopLevel(rest[open+1 : closeIdx])
	if len(cols) != 1 {
		return false
	}
	return strings.EqualFold(unquoteIdent(strings.TrimSpace(cols[0])), ownerCol)
}

// columnDefWithoutReferences strips an inline REFERENCES clause from
// ownerCol's definition, returning the rewritten definition and whether one was
// found. Everything before REFERENCES is kept verbatim; everything after it —
// the target, its optional column list, and any ON DELETE / ON UPDATE /
// DEFERRABLE modifiers — belongs to the key and goes with it.
func columnDefWithoutReferences(item, ownerCol string) (string, bool) {
	fields := strings.Fields(item)
	if len(fields) == 0 || !strings.EqualFold(unquoteIdent(fields[0]), ownerCol) {
		return item, false
	}
	idx := indexKeywordAtDepthZero(item, "REFERENCES")
	if idx < 0 {
		return item, false
	}
	head := strings.TrimSpace(item[:idx])
	rest := item[idx+len("REFERENCES"):]
	// Consume the target table and its optional (col) list.
	rest = strings.TrimLeft(rest, " \t\n")
	// Table name: up to whitespace or '('.
	i := 0
	for i < len(rest) && rest[i] != ' ' && rest[i] != '\t' && rest[i] != '\n' && rest[i] != '(' {
		i++
	}
	rest = strings.TrimLeft(rest[i:], " \t\n")
	if strings.HasPrefix(rest, "(") {
		if end := matchingParen(rest, 0); end >= 0 {
			rest = rest[end+1:]
		}
	}
	// Drop the key's own modifiers; anything else on the line is kept.
	rest = strings.TrimSpace(rest)
	for {
		up := strings.ToUpper(rest)
		switch {
		case strings.HasPrefix(up, "ON DELETE "), strings.HasPrefix(up, "ON UPDATE "):
			rest = strings.TrimSpace(rest[len("ON DELETE "):])
			// The action itself is one or two words (CASCADE, SET NULL, NO ACTION).
			f := strings.Fields(rest)
			if len(f) > 0 {
				n := 1
				if strings.EqualFold(f[0], "SET") || strings.EqualFold(f[0], "NO") {
					n = 2
				}
				rest = strings.TrimSpace(strings.Join(f[min(n, len(f)):], " "))
			}
			continue
		case strings.HasPrefix(up, "DEFERRABLE"), strings.HasPrefix(up, "NOT DEFERRABLE"):
			f := strings.Fields(rest)
			rest = strings.TrimSpace(strings.Join(f[min(1, len(f)):], " "))
			continue
		case strings.HasPrefix(up, "INITIALLY "):
			f := strings.Fields(rest)
			rest = strings.TrimSpace(strings.Join(f[min(2, len(f)):], " "))
			continue
		case strings.HasPrefix(up, "MATCH "):
			f := strings.Fields(rest)
			rest = strings.TrimSpace(strings.Join(f[min(2, len(f)):], " "))
			continue
		}
		break
	}
	if rest != "" {
		return head + " " + rest, true
	}
	return head, true
}

// unquoteIdent strips the four quoting styles SQLite accepts on an identifier.
func unquoteIdent(s string) string {
	s = strings.TrimSpace(s)
	if len(s) < 2 {
		return s
	}
	switch s[0] {
	case '"', '`', '\'':
		if s[len(s)-1] == s[0] {
			return s[1 : len(s)-1]
		}
	case '[':
		if s[len(s)-1] == ']' {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// indexAtDepthZero returns the index of the first occurrence of c that is
// outside every quoted string and every nested paren.
func indexAtDepthZero(s string, c byte) int {
	depth := 0
	for i := 0; i < len(s); i++ {
		if skip := skipQuoted(s, i); skip > i {
			i = skip - 1
			continue
		}
		switch s[i] {
		case '(':
			if depth == 0 && c == '(' {
				return i
			}
			depth++
		case ')':
			depth--
		default:
			if depth == 0 && s[i] == c {
				return i
			}
		}
	}
	return -1
}

// matchingParen returns the index of the ')' closing the '(' at open.
func matchingParen(s string, open int) int {
	depth := 0
	for i := open; i < len(s); i++ {
		if skip := skipQuoted(s, i); skip > i {
			i = skip - 1
			continue
		}
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// splitTopLevel splits a column/constraint list on commas that are outside both
// quotes and nested parens.
func splitTopLevel(body string) []string {
	var out []string
	start, depth := 0, 0
	for i := 0; i < len(body); i++ {
		if skip := skipQuoted(body, i); skip > i {
			i = skip - 1
			continue
		}
		switch body[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, body[start:i])
				start = i + 1
			}
		}
	}
	out = append(out, body[start:])
	return out
}

// indexKeywordAtDepthZero finds a bare keyword outside quotes and parens,
// bounded by non-identifier characters so "REFERENCES" does not match inside
// "self_references".
func indexKeywordAtDepthZero(s, keyword string) int {
	depth := 0
	up := strings.ToUpper(s)
	for i := 0; i < len(s); i++ {
		if skip := skipQuoted(s, i); skip > i {
			i = skip - 1
			continue
		}
		switch s[i] {
		case '(':
			depth++
			continue
		case ')':
			depth--
			continue
		}
		if depth != 0 || !strings.HasPrefix(up[i:], keyword) {
			continue
		}
		if i > 0 && isIdentByte(s[i-1]) {
			continue
		}
		if end := i + len(keyword); end < len(s) && isIdentByte(s[end]) {
			continue
		}
		return i
	}
	return -1
}

func isIdentByte(c byte) bool {
	return c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// skipQuoted returns the index just past the quoted run starting at i, or i if
// no quote starts there. SQLite doubles a quote character to escape it.
func skipQuoted(s string, i int) int {
	if i >= len(s) {
		return i
	}
	var closer byte
	switch s[i] {
	case '"', '\'', '`':
		closer = s[i]
	case '[':
		closer = ']'
	default:
		return i
	}
	for j := i + 1; j < len(s); j++ {
		if s[j] != closer {
			continue
		}
		if closer != ']' && j+1 < len(s) && s[j+1] == closer {
			j++ // a doubled quote is an escape, not a terminator
			continue
		}
		return j + 1
	}
	return len(s)
}
