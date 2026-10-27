package migrate

import "testing"

func TestCreateIndexSQLPerDialect(t *testing.T) {
	tests := []struct {
		dialect Dialect
		unique  bool
		table   string
		want    string
	}{
		{DialectPostgres, false, "widgets", "CREATE INDEX IF NOT EXISTS idx_w ON widgets (a)"},
		{DialectSQLite, false, "widgets", "CREATE INDEX IF NOT EXISTS idx_w ON widgets (a)"},
		{DialectPostgres, true, "widgets", "CREATE UNIQUE INDEX IF NOT EXISTS idx_w ON widgets (a)"},
		{DialectSQLite, true, "widgets", "CREATE UNIQUE INDEX IF NOT EXISTS idx_w ON widgets (a)"},
		{DialectPostgres, false, "app.widgets", "CREATE INDEX IF NOT EXISTS idx_w ON app.widgets (a)"},
		{DialectSQLite, false, "app.widgets", "CREATE INDEX IF NOT EXISTS app.idx_w ON widgets (a)"},
		{DialectPostgres, true, "app.widgets", "CREATE UNIQUE INDEX IF NOT EXISTS idx_w ON app.widgets (a)"},
		{DialectSQLite, true, "app.widgets", "CREATE UNIQUE INDEX IF NOT EXISTS app.idx_w ON widgets (a)"},
	}
	for _, tt := range tests {
		if got := createIndexSQL(tt.dialect, tt.unique, "idx_w", tt.table, "a"); got != tt.want {
			t.Errorf("createIndexSQL(%v, %v, %q) = %q, want %q", tt.dialect, tt.unique, tt.table, got, tt.want)
		}
	}
}
