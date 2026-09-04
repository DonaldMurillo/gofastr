package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Pins [init-dsn], found by the 2026-09-04 red-probe round; fixed in
// writeMainGo keeping a credentialed DSN out of the committed main.go
// (dsnHasSecret decides; the 0600 gitignored .env is the DSN's only home).
// Property: credential-bearing DSNs never land as literals in generated,
// committed Go source — the scaffold routes DATABASE_URL through the
// owner-only, gitignored .env and the generated main.go carries only an
// env read with no secret fallback (the contract
// TestBlueprintNeverInlinesSecrets + TestDSNRedactionFailsClosed pin for
// the blueprint generator: "Secrets never land in committed source").
// Surfaces: cmd/gofastr/init.go::runInit (--db=postgres sets dbURL),
// cmd/gofastr/init.go::writeMainGo (emits getEnv("DATABASE_URL", "") with
// a fail-closed fatal when unset; sqlite file DSNs stay inline per the
// documented exception), versus blueprint.go::renderBlueprintEnv.
func TestInitMainGoNeverInlinesDSN(t *testing.T) {
	dir := t.TempDir()
	covT_chdir(t, dir)

	// The documented, flag-supported invocation; nothing exotic.
	code := covT_capExit(t, func() { runInit([]string{"demo", "--db=postgres"}) })
	if code == 1 {
		t.Fatal("runInit exited 1; scaffold failed (see TestInitAcceptsDBDriverAliases for the pinned happy path)")
	}

	mainGo, err := os.ReadFile(filepath.Join(dir, "demo", "main.go"))
	if err != nil {
		t.Fatalf("read scaffolded main.go: %v", err)
	}
	if strings.Contains(string(mainGo), "postgres://user:password@") {
		t.Fatalf("SECURITY: [init-dsn] scaffolded main.go inlines the credential-bearing DATABASE_URL literal %q — the committed, safe-to-edit file the blueprint generator's own contract (TestBlueprintNeverInlinesSecrets: \"Secrets never land in committed source\") keeps DSN-free; the DSN belongs in the 0600 gitignored .env alone:\n%s", "postgres://user:password@…", trimForReport(string(mainGo)))
	}
	// False-positive guard: the runtime must still read DATABASE_URL from
	// the environment, and fail closed with a pointer at the .env when it
	// is unset (never fall back to a pasted literal).
	for _, want := range []string{
		`getEnv("DATABASE_URL"`,
		"DATABASE_URL is not set",
	} {
		if !strings.Contains(string(mainGo), want) {
			t.Fatalf("fix direction violated: main.go must keep the env read and the fail-closed unset path (%q missing):\n%s", want, trimForReport(string(mainGo)))
		}
	}

	// The .env is the DSN's one home: the scaffold still writes the full
	// URL there (0600, gitignored), so the app runs out of the box.
	env, err := os.ReadFile(filepath.Join(dir, "demo", ".env"))
	if err != nil {
		t.Fatalf("read scaffolded .env: %v", err)
	}
	if !strings.Contains(string(env), "DATABASE_URL=postgres://user:password@localhost:5432/demo") {
		t.Fatalf("scaffolded .env lost the DSN — routing it out of main.go must not drop it:\n%s", env)
	}

	// The documented exception holds: a sqlite scaffold keeps its file DSN
	// inline (a local file path is not a credential).
	sqlite := t.TempDir()
	covT_chdir(t, sqlite)
	code = covT_capExit(t, func() { runInit([]string{"lite"}) })
	if code == 1 {
		t.Fatal("runInit sqlite scaffold failed")
	}
	mainGo, err = os.ReadFile(filepath.Join(sqlite, "lite", "main.go"))
	if err != nil {
		t.Fatalf("read sqlite main.go: %v", err)
	}
	if !strings.Contains(string(mainGo), `getEnv("DATABASE_URL", "file:lite.db")`) {
		t.Fatalf("sqlite file DSN no longer inlined (documented exception):\n%s", trimForReport(string(mainGo)))
	}
}

func trimForReport(s string) string {
	if len(s) > 800 {
		return s[:800] + "…"
	}
	return s
}
