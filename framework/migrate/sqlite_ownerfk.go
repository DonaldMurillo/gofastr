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
// entity declares as Scope.OwnerField. The column holds the session identity
// (auth_users), so the key fails every create; SQLite has no DROP CONSTRAINT.
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
// SQLite schema carries a single-column foreign key on the entity's owner
// column, inline or table-level. Non-SQLite databases return no findings.
func FindStaleOwnerForeignKeys(ctx context.Context, db *sql.DB, registry entity.Registry) ([]StaleOwnerFK, error) {
	if db == nil || registry == nil {
		return nil, nil
	}
	dialect, err := DetectDialectStrictContext(ctx, db)
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

func staleOwnerKeysForTable(ctx context.Context, db *sql.DB, table, ownerCol string) ([]string, error) {
	pragma, err := sqlitePragma("foreign_key_list", table)
	if err != nil {
		return nil, fmt.Errorf("repair scan: invalid table name %q: %w", table, err)
	}
	rows, err := db.QueryContext(ctx, pragma)
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
		if len(byKey[id]) == 1 && strings.EqualFold(byKey[id][0], ownerCol) {
			out = append(out, refOf[id])
		}
	}
	return out, nil
}

// RepairStaleOwnerForeignKeys rebuilds each affected table without the stale
// key from its stored DDL, keeping its rows, indexes and triggers, and returns
// an error when a foreign key violation remains afterwards.
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

	return withSQLiteRebuildConn(ctx, db, func(conn *sql.Conn) error {
		for _, s := range tables {
			if err := rebuildWithoutOwnerFK(ctx, conn, s); err != nil {
				return fmt.Errorf("repair %s: %w", s.Table, err)
			}
		}
		violations, err := foreignKeyViolations(ctx, conn)
		if err != nil {
			return fmt.Errorf("verify foreign keys after the rebuild: %w", err)
		}
		if len(violations) > 0 {
			return fmt.Errorf("the rebuilt schema still has %d foreign key violation(s): %s — these are dangling rows, not stale constraints; fix or delete them",
				len(violations), strings.Join(violations, ", "))
		}
		return nil
	})
}

// withSQLiteRebuildConn runs fn on one dedicated connection with
// foreign_keys OFF and legacy_alter_table ON, then restores both.
func withSQLiteRebuildConn(ctx context.Context, db *sql.DB, fn func(conn *sql.Conn) error) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
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

	if _, err := conn.ExecContext(ctx, "PRAGMA legacy_alter_table=ON"); err != nil {
		if rerr := restore(); rerr != nil {
			return fmt.Errorf("enable legacy_alter_table for the rebuild: %w; additionally: %v", err, rerr)
		}
		return fmt.Errorf("enable legacy_alter_table for the rebuild: %w", err)
	}
	if err := fn(conn); err != nil {
		if rerr := restore(); rerr != nil {
			return fmt.Errorf("%w; additionally: %v", err, rerr)
		}
		return err
	}
	return restore()
}

// foreignKeyViolations runs PRAGMA foreign_key_check in every schema and
// names each violation as "child → parent", the child qualified outside main.
func foreignKeyViolations(ctx context.Context, q queryer) ([]string, error) {
	schemas, err := sqliteSchemas(ctx, q)
	if err != nil {
		return nil, err
	}
	var violations []string
	for _, schema := range schemas {
		//gofastr:allow(quoteident) schema was read from PRAGMA database_list with its case kept
		rows, err := q.QueryContext(ctx, "PRAGMA "+query.QuoteIdent(schema)+".foreign_key_check")
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var table, rowid, parent, fkid any
			if err := rows.Scan(&table, &rowid, &parent, &fkid); err != nil {
				rows.Close()
				return nil, err
			}
			child := fmt.Sprintf("%v", table)
			if schema != "main" {
				child = schema + "." + child
			}
			violations = append(violations, fmt.Sprintf("%s → %v", child, parent))
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return violations, nil
}

func sqliteSchemas(ctx context.Context, q queryer) ([]string, error) {
	rows, err := q.QueryContext(ctx, "PRAGMA database_list")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var seq int
		var name string
		var file sql.NullString
		if err := rows.Scan(&seq, &name, &file); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

func rebuildWithoutOwnerFK(ctx context.Context, conn *sql.Conn, s StaleOwnerFK) error {
	if _, err := query.SafeIdent(s.Table); err != nil {
		return fmt.Errorf("invalid table name %q: %w", s.Table, err)
	}
	schema, base := splitTable(s.Table)
	master, seqTable, prefix := "sqlite_master", "sqlite_sequence", ""
	if schema != "" {
		//gofastr:allow(quoteident) schema is split from the SafeIdent-checked SQLite table name being rebuilt and keeps its supplied spelling for the attached schema
		master, seqTable, prefix = query.QuoteIdent(schema)+".sqlite_master", query.QuoteIdent(schema)+".sqlite_sequence", query.QuoteIdent(schema)+"."
	}

	var stored, createSQL string
	err := conn.QueryRowContext(ctx,
		"SELECT name, sql FROM "+master+" WHERE type='table' AND name = ? COLLATE NOCASE", base).Scan(&stored, &createSQL)
	if err == sql.ErrNoRows {
		return fmt.Errorf("table not found")
	}
	if err != nil {
		return err
	}
	//gofastr:allow(quoteident) stored was read from sqlite_master with its case kept
	qualified := prefix + query.QuoteIdent(stored)

	var replays []string
	objRows, err := conn.QueryContext(ctx,
		"SELECT sql FROM "+master+" WHERE tbl_name = ? COLLATE NOCASE AND type IN ('index','trigger') AND sql IS NOT NULL", stored)
	if err != nil {
		return err
	}
	for objRows.Next() {
		var ddl string
		if err := objRows.Scan(&ddl); err != nil {
			objRows.Close()
			return err
		}
		replays = append(replays, inSchema(ddl, schema))
	}
	objRows.Close()
	if err := objRows.Err(); err != nil {
		return err
	}

	newTable := stored + "__gofastr_repair"
	newDDL, removed, err := ddlWithoutForeignKeyOn(createSQL, s.Column, newTable)
	if err != nil {
		return err
	}
	if !removed {
		return fmt.Errorf("found a foreign key on %q via PRAGMA foreign_key_list but could not locate it in the stored DDL; "+
			"rebuild this table by hand:\n%s", s.Column, createSQL)
	}

	newDDL = inSchema(newDDL, schema)
	cols, err := tableColumnNames(ctx, conn, s.Table)
	if err != nil {
		return err
	}
	if len(cols) == 0 {
		return fmt.Errorf("no columns found")
	}
	quoted := make([]string, len(cols))
	for i, c := range cols {
		//gofastr:allow(quoteident) c was read from PRAGMA table_info with its case kept
		quoted[i] = query.QuoteIdent(c)
	}
	colList := strings.Join(quoted, ", ")

	seq, hasSeq, err := autoincrementSeq(ctx, conn, seqTable, stored)
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
		//gofastr:allow(quoteident) newTable is the sqlite_master name plus a literal repair suffix
		fmt.Sprintf("INSERT INTO %s (%s) SELECT %s FROM %s", prefix+query.QuoteIdent(newTable), colList, colList, qualified),
		fmt.Sprintf("DROP TABLE %s", qualified),
		//gofastr:allow(quoteident) newTable is the sqlite_master name plus a literal repair suffix, stored came from sqlite_master
		fmt.Sprintf("ALTER TABLE %s RENAME TO %s", prefix+query.QuoteIdent(newTable), query.QuoteIdent(stored)),
	}
	stmts = append(stmts, replays...)
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("%w\nwhile running: %s", err, stmt)
		}
	}
	if hasSeq {
		if err := restoreAutoincrementSeq(ctx, tx, seqTable, stored, seq); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func autoincrementSeq(ctx context.Context, conn *sql.Conn, seqTable, table string) (int64, bool, error) {
	var seq int64
	err := conn.QueryRowContext(ctx, `SELECT seq FROM `+seqTable+` WHERE name = ?`, table).Scan(&seq)
	switch {
	case err == nil:
		return seq, true, nil
	case errors.Is(err, sql.ErrNoRows):
		return 0, false, nil
	case strings.Contains(strings.ToLower(err.Error()), "no such table"):
		return 0, false, nil
	default:
		return 0, false, err
	}
}

func restoreAutoincrementSeq(ctx context.Context, tx *sql.Tx, seqTable, table string, seq int64) error {
	res, err := tx.ExecContext(ctx, `UPDATE `+seqTable+` SET seq = ? WHERE name = ?`, seq, table)
	if err != nil {
		return err
	}
	if n, aerr := res.RowsAffected(); aerr == nil && n > 0 {
		return nil
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO `+seqTable+` (name, seq) VALUES (?, ?)`, table, seq)
	return err
}

func tableColumnNames(ctx context.Context, conn *sql.Conn, table string) ([]string, error) {
	pragma, err := sqlitePragma("table_info", table)
	if err != nil {
		return nil, err
	}
	rows, err := conn.QueryContext(ctx, pragma)
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

// ddlWithoutForeignKeyOn rewrites a stored CREATE TABLE, removing the foreign
// key on ownerCol and renaming the table to newName; it reports whether a key
// was removed.
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

// skipComment returns the index just past a comment beginning at i, or i.
// A line comment stops at the newline, leaving it in place: it separates
// tokens.
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

func isTableLevelFKOn(item, ownerCol string) bool {
	rest := stripLeadingComments(item)
	tok, after := nextSQLToken(rest)
	if strings.EqualFold(tok, "CONSTRAINT") {
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

func columnDefWithoutReferences(item, ownerCol string) (string, bool) {
	fields := strings.Fields(stripLeadingComments(item))
	if len(fields) == 0 || !strings.EqualFold(unquoteIdent(fields[0]), ownerCol) {
		return item, false
	}
	idx := indexKeywordAtDepthZero(item, "REFERENCES")
	if idx < 0 {
		return item, false
	}
	head := strings.TrimRight(item[:idx], " \t\r")
	rest := item[idx+len("REFERENCES"):]
	rest = trimLeadingInert(rest)
	_, rest = nextSQLToken(rest)
	rest = trimLeadingInert(rest)
	if strings.HasPrefix(rest, "(") {
		if end := matchingParen(rest, 0); end >= 0 {
			rest = rest[end+1:]
		}
	}
	rest = trimLeadingInert(rest)
	for {
		toks := sqlTokens(rest)
		tok := func(i int) string {
			if i < len(toks) {
				return toks[i].up
			}
			return ""
		}
		var n int
		switch {
		case tok(0) == "ON" && (tok(1) == "DELETE" || tok(1) == "UPDATE"):
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

type sqlToken struct {
	up         string
	start, end int
}

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
			if j := skipComment(s, i); j > i {
				break
			}
			i++
		}
		if i == start {
			i++
		}
		out = append(out, sqlToken{up: strings.ToUpper(s[start:i]), start: start, end: i})
	}
	return out
}

// terminateLineComment appends a newline when s ends inside a comment.
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

// skipQuoted returns the index just past the quoted run opening at i, or i.
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

// skipInert returns the index just past a quoted run or comment starting at i,
// or i.
func skipInert(s string, i int) int {
	if i >= len(s) {
		return i
	}
	if next := skipComment(s, i); next > i {
		return next
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
			j++
			continue
		}
		return j + 1
	}
	return len(s)
}
