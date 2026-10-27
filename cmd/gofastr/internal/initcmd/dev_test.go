package initcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitGeneratedMainUsesIsolationHelpers(t *testing.T) {
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })
	if err := os.Mkdir("demo", 0o755); err != nil {
		t.Fatal(err)
	}

	writeMainGo("demo", "app", "example.com/demo", false, "sqlite", "file:demo.db")
	data, err := os.ReadFile(filepath.Join("demo", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{
		`"github.com/DonaldMurillo/gofastr/framework/isolation"`,
		`runtimeIsolation, err := isolation.Resolve(".")`,
		`dsn := getEnv("DATABASE_URL", "file:demo.db")`,
		`runtimeIsolation.Database("sqlite3", dsn)`,
		`runtimeIsolation.Addr(getEnv("PORT", "localhost:8080"))`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("generated main.go missing %q:\n%s", want, got)
		}
	}
}

// TestInitSQLiteDriverAgreesEndToEnd pins that the generated app wires ONE
// consistent SQLite driver: the name passed to isolation.Database, the
// blank-imported driver package, and the migration dialect all agree. Both
// the friendly "sqlite" spelling and the wire "sqlite3" spelling must yield
// identical, self-consistent pure-Go SQLite wiring.
func TestInitSQLiteDriverAgreesEndToEnd(t *testing.T) {
	for _, dbDriver := range []string{"sqlite", "sqlite3"} {
		t.Run(dbDriver, func(t *testing.T) {
			dir := t.TempDir()
			project := filepath.Join(dir, "app")
			if err := os.Mkdir(project, 0o755); err != nil {
				t.Fatal(err)
			}
			writeMainGo(project, "app", "example.com/app", false, dbDriver, "file:app.db")
			got, err := os.ReadFile(filepath.Join(project, "main.go"))
			if err != nil {
				t.Fatal(err)
			}
			src := string(got)
			for _, want := range []string{
				`runtimeIsolation.Database("sqlite3",`,
				`_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"`,
				`migrate.WithDialect(migrate.DialectSQLite)`,
			} {
				if !strings.Contains(src, want) {
					t.Errorf("dbDriver=%q: generated main.go missing %q\n%s", dbDriver, want, src)
				}
			}
		})
	}
}

func TestInitGeneratedUIIsFrameworkComposed(t *testing.T) {
	dir := t.TempDir()
	project := filepath.Join(dir, "demo")
	if err := os.Mkdir(project, 0o755); err != nil {
		t.Fatal(err)
	}
	writeMainGo(project, "app", "example.com/demo", true, "sqlite", "")
	writeScreensGo(project, "app", true)

	mainBody, err := os.ReadFile(filepath.Join(project, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	homeBody, err := os.ReadFile(filepath.Join(project, "screens.go"))
	if err != nil {
		t.Fatal(err)
	}
	combined := string(mainBody) + "\n" + string(homeBody)
	for _, forbidden := range []string{
		"WithCustomCSS", "CreateStyleSheet", "style.NewStyleSheet", "render.Tag(",
	} {
		if strings.Contains(combined, forbidden) {
			t.Errorf("generated UI must not contain app-owned styling or structural markup %q", forbidden)
		}
	}
	for _, required := range []string{"ui.Container", "ui.Stack", "ui.PageHeader", "ui.Section"} {
		if !strings.Contains(string(homeBody), required) {
			t.Errorf("generated home screen missing framework primitive %q", required)
		}
	}
}

// The generated gofastr.yml must not advertise strategy values that the
// isolation resolver doesn't consult. A reader who edits "strategy: path"
// to "strategy: foo" expects to see behavior change, and nothing happens.
// Drop the decorative field rather than mislead.
func TestInitIsolationConfigOmitsDecorativeStrategy(t *testing.T) {
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })
	if err := os.Mkdir("demo", 0o755); err != nil {
		t.Fatal(err)
	}

	writeSettingsFile("demo", "sqlite")
	data, err := os.ReadFile(filepath.Join("demo", "gofastr.settings.yml"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, bad := range []string{"strategy: path", "strategy: offset"} {
		if strings.Contains(got, bad) {
			t.Errorf("generated yml advertises unrecognized %q:\n%s", bad, got)
		}
	}
}
