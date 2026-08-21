package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runInit accepts --db=<driver> and silently scaffolded SQLite for any value
// that wasn't "postgres", so --db=mysql wrote dbDriver="mysql" with a SQLite
func TestInitRejectsUnknownDBDriver(t *testing.T) {
	dir := t.TempDir()
	covT_chdir(t, dir)
	// osExit unwinds via panic (covT_capExit), and covT_capStdout reads its
	// temp file only AFTER fn returns, so the read is skipped on the panic
	// path. Swap os.Stdout ourselves and read inline once covT_capExit has
	// recovered the panic (it returns normally, so execution continues here).
	old := os.Stdout
	f, err := os.CreateTemp(t.TempDir(), "stdout-*")
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = f
	code := covT_capExit(t, func() { runInit([]string{"app", "--db=mysql"}) })
	os.Stdout = old
	_ = f.Close()
	captured, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	out := string(captured)
	if code != 1 {
		t.Fatalf("unknown --db=mysql should exit 1, got %d", code)
	}
	// The error must point the author at the accepted spellings.
	lower := strings.ToLower(out)
	if !strings.Contains(lower, "sqlite") || !strings.Contains(lower, "postgres") {
		t.Fatalf("error should list accepted drivers (sqlite, postgres), got: %q", out)
	}
	// And it must not have scaffolded a broken project.
	if _, err := os.Stat(filepath.Join(dir, "app", "main.go")); err == nil {
		t.Fatalf("unknown --db should not have created the project")
	}
}

// Accepted aliases still work: the docs-canonical "sqlite" and "postgres", plus
// the wire spellings "sqlite3"/"postgresql". Each must scaffold a main.go whose
// driver wiring matches the chosen database.
func TestInitAcceptsDBDriverAliases(t *testing.T) {
	for _, tc := range []struct {
		flag string
		want string // substring expected in main.go
	}{
		{"--db=sqlite", `runtimeIsolation.Database("sqlite3",`},
		{"--db=sqlite3", `runtimeIsolation.Database("sqlite3",`},
		{"--db=postgres", "lib/pq"},
		{"--db=postgresql", "lib/pq"},
	} {
		t.Run(tc.flag, func(t *testing.T) {
			dir := t.TempDir()
			covT_chdir(t, dir)
			out := covT_capStdout(t, func() { runInit([]string{"app", tc.flag}) })
			main, err := os.ReadFile(filepath.Join(dir, "app", "main.go"))
			if err != nil {
				t.Fatalf("runInit %s did not scaffold main.go: %v (out=%q)", tc.flag, err, out)
			}
			if !strings.Contains(string(main), tc.want) {
				t.Fatalf("%s: main.go missing %q\n%s", tc.flag, tc.want, string(main)[:200])
			}
		})
	}
}

// The scaffold must emit WithConfig BEFORE any other framework option.
// WithConfig replaces the whole AppConfig, so with it scaffolded last the
// natural paste point for a granular option, next to WithDB, was before
// WithConfig, which silently zeroed it (the 2026-07-26 eval's #2 footgun).
// Ordering makes every below-the-line paste work; framework.NewApp warns at
// boot about the remaining above-the-line case.
func TestInitScaffoldsWithConfigFirst(t *testing.T) {
	for _, args := range [][]string{{"app"}, {"app", "--no-entity"}} {
		dir := t.TempDir()
		covT_chdir(t, dir)
		covT_capStdout(t, func() { runInit(args) })
		b, err := os.ReadFile(filepath.Join(dir, "app", "main.go"))
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		main := string(b)
		cfg := strings.Index(main, "framework.WithConfig(")
		if cfg < 0 {
			t.Fatalf("%v: scaffolded main.go has no WithConfig", args)
		}
		if db := strings.Index(main, "framework.WithDB("); db >= 0 && db < cfg {
			t.Errorf("%v: WithDB at byte %d precedes WithConfig at %d — options pasted beside WithDB land before WithConfig and are silently discarded", args, db, cfg)
		}
	}
}
