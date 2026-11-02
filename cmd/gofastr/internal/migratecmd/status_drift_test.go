package migratecmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// `migrate status` printed "Applied: 2, Pending: 0" while `migrate up`
// refused an applied migration whose file was edited and `migrate down`
// refused one whose file was deleted. It names both now.
func TestMigrateStatusShowsDrift(t *testing.T) {
	dir := covT_migrationsDir(t)
	md := filepath.Join(dir, "migrations")
	writeMigration(t, md, "002_create_more.sql", 2, "create_more")
	dbURL := "--db-url=" + filepath.Join(dir, "x.db")
	clitest.CovT_capStdout(t, func() { runMigrateUp([]string{dbURL}) })

	// Edit 001 after apply; delete 002.
	p := filepath.Join(md, "001_create_posts.sql")
	body, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(strings.Replace(string(body), "title TEXT", "title TEXT, extra TEXT", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(md, "002_create_more.sql")); err != nil {
		t.Fatal(err)
	}

	out := clitest.CovT_capStdout(t, func() { runMigrateStatus([]string{dbURL}) })
	for _, want := range []string{
		"1 create_posts: MODIFIED",
		"2 create_more: applied, but no file in migrations/ registers it",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("status output lacks %q:\n%s", want, out)
		}
	}
}
