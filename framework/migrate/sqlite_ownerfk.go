package migrate

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
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
	byKey := map[string][]string{}
	refOf := map[string]string{}
	var order []string
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
		// A composite key spans several rows sharing one id, so the columns
		// are collected per key and judged together below.
		id := byName["id"]
		byKey[id] = append(byKey[id], byName["from"])
		refOf[id] = byName["table"]
		order = append(order, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, id := range order {
		if seen[id] {
			continue
		}
		seen[id] = true
		// ONLY a key whose entire column list is the owner column. A composite
		// key that merely includes it is doing real work on the other columns
		// and is not the stale shape: the framework never emitted one, and
		// ddlWithoutForeignKeyOn deliberately refuses to rewrite it. Reporting
		// it here made AutoMigrate warn that "every create fails" about a
		// constraint that is working, and made `--apply` abort on a healthy
		// schema with an error telling the operator to rebuild by hand.
		if len(byKey[id]) == 1 && strings.EqualFold(byKey[id][0], ownerCol) {
			out = append(out, refOf[id])
		}
	}
	return out, nil
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
	restore := func() error {
		// legacy_alter_table goes back first and unconditionally: it changes
		// how every later ALTER on this pooled connection behaves, and leaving
		// it on is a quieter defect than an unenforced key because nothing
		// fails until someone renames a table.
		restoreCtx := context.WithoutCancel(ctx)
		_, legacyErr := conn.ExecContext(restoreCtx, "PRAGMA legacy_alter_table=OFF")
		if _, err := conn.ExecContext(restoreCtx, "PRAGMA foreign_keys=on"); err == nil && legacyErr == nil {
			return nil
		} else {
			if err == nil {
				err = legacyErr
			}
			closed = true
			bad := conn.Raw(func(any) error { return driver.ErrBadConn })
			cerr := conn.Close()
			return fmt.Errorf("could not re-enable foreign keys after the rebuild (%w); "+
				"the connection was retired so it cannot serve unenforced writes (retire: %v, close: %v)", err, bad, cerr)
		}
	}

	// legacy_alter_table=ON for the duration, because a VIEW that references
	// the table stops the rebuild dead without it. Modern ALTER TABLE RENAME
	// re-resolves every object that names the table, and at the point of the
	// rename the original has already been dropped, so SQLite fails the whole
	// transaction with `error in view <name>: no such table`. Reproduced with
	// a one-line view over the rebuilt table.
	//
	// Legacy rename is the documented mode for exactly this procedure: it
	// renames the table and leaves referencing objects alone, which is what we
	// want since the replacement takes the original's name and the view
	// resolves against it again afterwards.
	if _, err := conn.ExecContext(ctx, "PRAGMA legacy_alter_table=ON"); err != nil {
		// restore is already defined above precisely for this: an early return
		// here used to leave the pool a connection with foreign keys OFF,
		// because the deferred cleanup only closes and Close returns the
		// connection to the pool.
		if rerr := restore(); rerr != nil {
			return fmt.Errorf("enable legacy_alter_table for the rebuild: %w; additionally: %v", err, rerr)
		}
		return fmt.Errorf("enable legacy_alter_table for the rebuild: %w", err)
	}
	// Restoring the pragma is not optional: this connection goes back to the
	// pool and would serve every later write with enforcement silently off,
	// the exact condition v0.67 exists to end, reintroduced by the tool meant
	// to fix it.
	//
	// Two things make that harder than it looks.
	//
	// The restore runs on a context that cannot be cancelled. Using the
	// caller's ctx meant a cancelled repair skipped the restore entirely and
	// handed the pool a connection with enforcement off, which is the worst
	// outcome of the three and the easiest to trigger: cancel during the row
	// copy.
	//
	// And sql.Conn.Close does NOT destroy a connection, it returns it to the
	// pool. An earlier version of this comment claimed otherwise. Retiring a
	// connection is what driver.ErrBadConn from Raw is for, so a connection
	// whose pragma could not be restored is marked bad and never serves
	// another query.

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
		quoted[i] = quoteIdent(c)
	}
	colList := strings.Join(quoted, ", ")

	// AUTOINCREMENT's high-water mark lives in sqlite_sequence keyed by table
	// name, and DROP TABLE deletes that row. The rebuild then re-seeds it from
	// the copied rows, so a table whose highest rows had been DELETEd handed
	// the next insert a rowid it had already used — the one thing
	// AUTOINCREMENT exists to prevent. Read before the drop, put back after
	// the rename.
	seq, hasSeq, err := autoincrementSeq(ctx, conn, s.Table)
	if err != nil {
		return err
	}

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
	if hasSeq {
		if err := restoreAutoincrementSeq(ctx, tx, s.Table, seq); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// autoincrementSeq reads the table's AUTOINCREMENT high-water mark, if it has
// one. An absent sqlite_sequence row, or an absent sqlite_sequence table,
// means the table does not use AUTOINCREMENT and there is nothing to carry.
func autoincrementSeq(ctx context.Context, conn *sql.Conn, table string) (int64, bool, error) {
	var seq int64
	err := conn.QueryRowContext(ctx, `SELECT seq FROM sqlite_sequence WHERE name = ?`, table).Scan(&seq)
	switch {
	case err == nil:
		return seq, true, nil
	case errors.Is(err, sql.ErrNoRows):
		return 0, false, nil
	case strings.Contains(strings.ToLower(err.Error()), "no such table"):
		// No AUTOINCREMENT table has ever existed in this database.
		return 0, false, nil
	default:
		return 0, false, err
	}
}

// restoreAutoincrementSeq puts the saved high-water mark back. The rebuild
// re-seeds the row from the rows it copied, so the UPDATE normally finds one;
// the INSERT covers a table whose highest rows were all deleted before the
// repair, which is exactly the case where the mark matters.
func restoreAutoincrementSeq(ctx context.Context, tx *sql.Tx, table string, seq int64) error {
	res, err := tx.ExecContext(ctx, `UPDATE sqlite_sequence SET seq = ? WHERE name = ?`, seq, table)
	if err != nil {
		return err
	}
	if n, aerr := res.RowsAffected(); aerr == nil && n > 0 {
		return nil
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO sqlite_sequence (name, seq) VALUES (?, ?)`, table, seq)
	return err
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

// skipComment returns the index just past a comment beginning at i, or i when
// no comment starts there. The comment half of skipInert, without the quoted
// runs: callers that are looking for an identifier must not have it eaten.
func skipComment(s string, i int) int {
	if i >= len(s) {
		return i
	}
	if s[i] == '-' && i+1 < len(s) && s[i+1] == '-' {
		if nl := strings.IndexByte(s[i:], '\n'); nl >= 0 {
			return i + nl
		}
		return len(s)
	}
	if s[i] == '/' && i+1 < len(s) && s[i+1] == '*' {
		if end := strings.Index(s[i+2:], "*/"); end >= 0 {
			return i + 2 + end + 2
		}
		return len(s)
	}
	return i
}

// stripLeadingComments removes comment runs from the front of a column or
// constraint definition, so the identifier checks below see the definition
// itself.
//
// Only COMMENTS are skipped, never a quoted run. skipInert treats a quoted
// identifier as inert too, and at offset zero the first token is the column
// NAME, so `"user_id" TEXT REFERENCES users(id)` was stripped down to
// ` TEXT REFERENCES …`; the matcher then compared TEXT against the owner column,
// found nothing, and the repair refused a table it can perfectly well rebuild.
//
// Splitting on top-level commas leaves a comment that preceded a definition
// attached to the FRONT of it, and both callers identify a definition by its
// first token. Without this, `-- owner\n user_id TEXT REFERENCES users(id)`
// reads as a definition named "--" and its inline key is never found, which
// the caller reports as "could not locate it in the stored DDL" and refuses.
// Fail-closed, but a refusal on a table the rewriter can handle.
func stripLeadingComments(item string) string {
	for {
		trimmed := strings.TrimLeft(item, " \t\n\r")
		next := skipComment(trimmed, 0)
		if next == 0 || trimmed == "" {
			return trimmed
		}
		item = trimmed[next:]
	}
}

// isTableLevelFKOn reports whether item is a table constraint of the form
// [CONSTRAINT name] FOREIGN KEY (ownerCol) REFERENCES … — and only when the
// key's column list is exactly ownerCol. A composite key that happens to
// include the owner column is left alone: dropping it would remove a
// constraint on the other columns too, which nobody asked for.
func isTableLevelFKOn(item, ownerCol string) bool {
	// Token by token, never strings.Fields plus a literal prefix. Fields
	// splits `CONSTRAINT "fk owner" FOREIGN KEY` inside the quotes, and a
	// "FOREIGN KEY" prefix test demands exactly one space, so `FOREIGN\nKEY`
	// missed. Both ended as "found the key via PRAGMA but not in the DDL",
	// which refuses a table the repair can rebuild.
	rest := stripLeadingComments(item)
	tok, after := nextSQLToken(rest)
	if strings.EqualFold(tok, "CONSTRAINT") {
		// The constraint's own name, which may be quoted and hold spaces.
		_, after = nextSQLToken(after)
		tok, after = nextSQLToken(after)
	}
	if !strings.EqualFold(tok, "FOREIGN") {
		return false
	}
	if tok, after = nextSQLToken(after); !strings.EqualFold(tok, "KEY") {
		return false
	}
	rest = trimLeadingInert(after)
	open := strings.IndexByte(rest, '(')
	if open != 0 {
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
	fields := strings.Fields(stripLeadingComments(item))
	if len(fields) == 0 || !strings.EqualFold(unquoteIdent(fields[0]), ownerCol) {
		return item, false
	}
	idx := indexKeywordAtDepthZero(item, "REFERENCES")
	if idx < 0 {
		return item, false
	}
	// Right-trimmed WITHOUT the newline, which is load-bearing: it is what
	// terminates a `--` comment. TrimSpace ate it, so
	// `user_id TEXT -- stamped from the session\n  REFERENCES users(id) NOT NULL`
	// rebuilt as `user_id TEXT -- stamped from the session NOT NULL` and the
	// NOT NULL fell inside the comment. SQLite ACCEPTS that, so the repair
	// reported success on a table that had silently lost a constraint. The
	// list comma the caller joins on lands on that line too, which loses the
	// following column the same way.
	head := strings.TrimRight(item[:idx], " \t\r")
	rest := item[idx+len("REFERENCES"):]
	// Consume the target table and its optional (col) list. Whitespace AND
	// comments, because a comment may legally sit between REFERENCES and its
	// target, and a plain TrimLeft left it in place for the name scan to
	// swallow as the table name.
	rest = trimLeadingInert(rest)
	// Table name: up to whitespace or '(', with a quoted name counting as one
	// token. `REFERENCES "user table"(id)` stopped at the space INSIDE the
	// quotes and stranded `table"(id)` in the rebuilt column.
	i := 0
	for i < len(rest) {
		if j := skipQuoted(rest, i); j > i {
			i = j
			continue
		}
		if rest[i] == ' ' || rest[i] == '\t' || rest[i] == '\n' || rest[i] == '\r' || rest[i] == '(' {
			break
		}
		i++
	}
	rest = trimLeadingInert(rest[i:])
	if strings.HasPrefix(rest, "(") {
		if end := matchingParen(rest, 0); end >= 0 {
			rest = rest[end+1:]
		}
	}
	// Drop the key's own modifiers, keeping anything else on the line VERBATIM.
	//
	// Consumed by advancing an index over the original text, never by
	// strings.Fields plus a rejoin. Tokenizing collapses whitespace inside a
	// quoted literal that follows: `ON DELETE CASCADE DEFAULT 'guest  account'`
	// rejoined as `DEFAULT 'guest account'`, and the rebuilt table silently
	// stored a different default than the one it was asked to preserve.
	// Comment-aware: `REFERENCES u(id) -- why\n ON DELETE CASCADE` left the
	// comment at the head, so the modifier loop matched nothing and the
	// action was stranded without its clause.
	rest = trimLeadingInert(rest)
	for {
		// Matched and consumed on TOKENS carrying their SOURCE SPANS, because
		// a comment is legal ANYWHERE a space is: `ON /* n */ DELETE CASCADE`
		// and `ON DELETE /* n */ CASCADE` both parse in SQLite. Every earlier
		// shape of this loop matched whitespace-delimited words, so it read
		// `/*` as the action word, matched nothing, broke, and left the
		// modifier stranded without the REFERENCES clause it belongs to.
		//
		// Three review rounds each found one more input this way — a wrapped
		// modifier, a comment before the first one, a comment between two —
		// which is the signal that matching on words was the wrong SHAPE,
		// not that any one case had been missed. Consuming by span takes the
		// comments out with the modifier they sit inside.
		toks := sqlTokens(rest)
		tok := func(i int) string {
			if i < len(toks) {
				return toks[i].up
			}
			return ""
		}
		var n int // tokens to consume
		switch {
		case tok(0) == "ON" && (tok(1) == "DELETE" || tok(1) == "UPDATE"):
			// "ON", the action word, then one or two words of action.
			n = 3
			if tok(2) == "SET" || tok(2) == "NO" {
				n = 4
			}
		case tok(0) == "NOT" && tok(1) == "DEFERRABLE":
			n = 2
		case tok(0) == "DEFERRABLE":
			n = 1
		case tok(0) == "INITIALLY", tok(0) == "MATCH":
			n = 2
		default:
			n = 0
		}
		if n == 0 || n > len(toks) {
			break
		}
		rest = rest[toks[n-1].end:]
	}
	rest = strings.TrimRight(rest, " \t\n\r")
	if rest != "" {
		return terminateLineComment(head + " " + rest), true
	}
	return terminateLineComment(head), true
}

// sqlToken is one token of a column definition, with the source span it
// occupies. The span is what the modifier loop consumes, so a comment sitting
// between the words of a modifier is removed along with it rather than
// stopping the scan.
type sqlToken struct {
	up         string // upper-cased, for keyword matching
	start, end int    // byte span in the source
}

// sqlTokens splits s into tokens, skipping whitespace and comments and
// treating a quoted identifier as a single token.
func sqlTokens(s string) []sqlToken {
	space := func(b byte) bool { return b == ' ' || b == '\t' || b == '\n' || b == '\r' }
	var out []sqlToken
	i := 0
	for i < len(s) {
		if space(s[i]) {
			i++
			continue
		}
		if j := skipComment(s, i); j > i {
			i = j
			continue
		}
		start := i
		if s[i] == '(' || s[i] == ',' {
			i++
			out = append(out, sqlToken{up: strings.ToUpper(s[start:i]), start: start, end: i})
			continue
		}
		for i < len(s) {
			if j := skipQuoted(s, i); j > i {
				i = j
				continue
			}
			if space(s[i]) || s[i] == '(' || s[i] == ',' {
				break
			}
			// A comment ends the word it touches: `CASCADE-- why` is CASCADE
			// followed by a comment, not one long token.
			if j := skipComment(s, i); j > i {
				break
			}
			i++
		}
		if i == start {
			// A zero-length token would spin forever. Unreachable while the
			// comment skip above runs first, which is exactly why it is here:
			// a mutation that removed that skip hung the test binary rather
			// than failing it, and a scanner that can hang is worse than one
			// that is wrong.
			i++
		}
		out = append(out, sqlToken{up: strings.ToUpper(s[start:i]), start: start, end: i})
	}
	return out
}

// terminateLineComment appends a newline when s ends inside an unterminated
// comment, because the caller appends to what this returns: the list comma, or
// the closing paren. Without it a definition ending in a trailing comment
// swallows that comma and takes the NEXT column with it — the same failure an
// eaten newline once caused for a NOT NULL.
func terminateLineComment(s string) string {
	i := 0
	for i < len(s) {
		if j := skipQuoted(s, i); j > i {
			i = j
			continue
		}
		if s[i] == '-' && i+1 < len(s) && s[i+1] == '-' {
			nl := strings.IndexByte(s[i:], '\n')
			if nl < 0 {
				return s + "\n"
			}
			i += nl
			continue
		}
		if s[i] == '/' && i+1 < len(s) && s[i+1] == '*' {
			e := strings.Index(s[i+2:], "*/")
			if e < 0 {
				return s + "\n"
			}
			i += 2 + e + 2
			continue
		}
		i++
	}
	return s
}

// trimLeadingInert drops leading whitespace AND comments from s. The scanners
// below are looking for the next real token, and a comment sitting where they
// expect one is read as that token.
func trimLeadingInert(s string) string {
	i := 0
	for i < len(s) {
		if s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r' {
			i++
			continue
		}
		if j := skipComment(s, i); j > i {
			i = j
			continue
		}
		break
	}
	return s[i:]
}

// nextSQLToken returns the first token of s and the remainder. A quoted
// identifier is ONE token even when it holds spaces, '(' ends a token and is
// returned as one itself, and leading whitespace and comments are skipped.
func nextSQLToken(s string) (string, string) {
	s = trimLeadingInert(s)
	if s == "" {
		return "", ""
	}
	if s[0] == '(' {
		return "(", s[1:]
	}
	i := 0
	for i < len(s) {
		if j := skipQuoted(s, i); j > i {
			i = j
			continue
		}
		if s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r' || s[i] == '(' {
			break
		}
		i++
	}
	return s[:i], s[i:]
}

// quoteIdent wraps an identifier the way SQLite escapes one: a literal double
// quote is DOUBLED. Go's %q escapes it as \" instead, which SQLite has no
// notion of, so a column named `we"ird` — legal, written `"we""ird"` in DDL,
// and preserved correctly by the rewriter — produced a syntax error at the
// row copy and refused a table the repair had already rebuilt.
func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// skipQuoted returns the index just past the quoted run opening at i, or i
// when s[i] does not open one. Unlike skipInert it does not treat `--` or
// `/*` as a comment: inside a word those are part of the word.
func skipQuoted(s string, i int) int {
	if i >= len(s) {
		return i
	}
	switch s[i] {
	case '"', '\'', '`', '[':
		return skipInert(s, i)
	}
	return i
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
		if skip := skipInert(s, i); skip > i {
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
		if skip := skipInert(s, i); skip > i {
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
		if skip := skipInert(body, i); skip > i {
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
		if skip := skipInert(s, i); skip > i {
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

// skipInert returns the index just past the run starting at i that carries no
// SQL structure, or i if no such run starts there. Two kinds qualify: a quoted
// identifier or literal, and a comment.
//
// Comments matter as much as quotes and were the omission. SQLite stores the
// CREATE TABLE text verbatim in sqlite_master, comments included, and a comma
// inside `-- the pk, obviously` split one column definition into two: the
// following column was absorbed as type words of a phantom column, and the
// result was DDL SQLite accepts. A rewriter for real user tables cannot read
// punctuation it does not know is inert.
//
// SQLite doubles a quote character to escape it; a -- comment runs to the
// newline; a block comment runs to the closing delimiter and does not nest.
func skipInert(s string, i int) int {
	if i >= len(s) {
		return i
	}
	if s[i] == '-' && i+1 < len(s) && s[i+1] == '-' {
		if nl := strings.IndexByte(s[i:], '\n'); nl >= 0 {
			return i + nl // leave the newline in place: it separates tokens
		}
		return len(s)
	}
	if s[i] == '/' && i+1 < len(s) && s[i+1] == '*' {
		if end := strings.Index(s[i+2:], "*/"); end >= 0 {
			return i + 2 + end + 2
		}
		return len(s)
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
