package isolation

import (
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestResolveActivatesOnlyForLinkedWorktreeByDefault(t *testing.T) {
	clearIsolationEnv(t)
	main := t.TempDir()
	writeFile(t, filepath.Join(main, ".git", "HEAD"), "ref: refs/heads/main\n")
	writeFile(t, filepath.Join(main, "gofastr.yml"), "isolation:\n  enabled: true\n")

	rt, err := Resolve(main)
	if err != nil {
		t.Fatalf("Resolve main: %v", err)
	}
	if rt.Active() {
		t.Fatal("main checkout should not be isolated in default worktree mode")
	}

	linked := t.TempDir()
	writeFile(t, filepath.Join(linked, ".git"), "gitdir: "+filepath.Join(main, ".git", "worktrees", "feature")+"\n")
	writeFile(t, filepath.Join(linked, "gofastr.yml"), "isolation:\n  enabled: true\n")

	rt, err = Resolve(linked)
	if err != nil {
		t.Fatalf("Resolve linked: %v", err)
	}
	if !rt.Active() {
		t.Fatal("linked worktree should be isolated")
	}
}

func TestResolveHonorsConfigAndEnvOff(t *testing.T) {
	clearIsolationEnv(t)
	dir := linkedWorktree(t)
	writeFile(t, filepath.Join(dir, "gofastr.yml"), "isolation:\n  enabled: false\n")

	rt, err := Resolve(dir)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if rt.Active() {
		t.Fatal("disabled config should disable isolation")
	}

	writeFile(t, filepath.Join(dir, "gofastr.yml"), "isolation:\n  enabled: true\n")
	t.Setenv(envIsolation, "off")
	rt, err = Resolve(dir)
	if err != nil {
		t.Fatalf("Resolve with env off: %v", err)
	}
	if rt.Active() {
		t.Fatal("GOFASTR_ISOLATION=off should disable isolation")
	}
}

func TestAddrIsStableAndDoesNotDoubleOffsetAppliedPort(t *testing.T) {
	clearIsolationEnv(t)
	dir := linkedWorktree(t)
	writeFile(t, filepath.Join(dir, "gofastr.yml"), "isolation:\n  enabled: true\n  port:\n    offset: 1000\n    range: 1\n    scan: 0\n")
	rt, err := Resolve(dir)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	addr, err := rt.Addr(":8080")
	if err != nil {
		t.Fatalf("Addr: %v", err)
	}
	if addr != ":9080" {
		t.Fatalf("addr = %q, want :9080", addr)
	}

	t.Setenv(envApplied, "1")
	t.Setenv(envPortPrefix+"8080", "9080")
	addr, err = rt.Addr(":9080")
	if err != nil {
		t.Fatalf("Addr applied: %v", err)
	}
	if addr != ":9080" {
		t.Fatalf("already-applied addr = %q, want :9080", addr)
	}
	addr, err = rt.Addr(":8080")
	if err != nil {
		t.Fatalf("Addr base applied: %v", err)
	}
	if addr != ":9080" {
		t.Fatalf("mapped addr = %q, want :9080", addr)
	}
}

func TestEnvRewritesExplicitValuesAndTemplates(t *testing.T) {
	clearIsolationEnv(t)
	dir := linkedWorktree(t)
	writeFile(t, filepath.Join(dir, "gofastr.yml"), `
isolation:
  enabled: true
  port:
    offset: 1000
    range: 1
    scan: 0
  services:
    redis: 6379
  env:
    REDIS_URL: "redis://localhost:{port:redis}/0-{id}"
`)
	rt, err := Resolve(dir)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	env := envMap(rt.Env([]string{
		"PORT=localhost:8080",
		"DATABASE_URL=postgres://user:pass@localhost:5432/app?sslmode=disable",
	}))
	if env["PORT"] != "localhost:9080" {
		t.Fatalf("PORT = %q, want localhost:9080", env["PORT"])
	}
	if !strings.Contains(env["DATABASE_URL"], "/app_"+rt.ID()+"?") {
		t.Fatalf("DATABASE_URL was not suffixed: %q", env["DATABASE_URL"])
	}
	if env["REDIS_URL"] != "redis://localhost:7379/0-"+rt.ID() {
		t.Fatalf("REDIS_URL = %q", env["REDIS_URL"])
	}
	if env[envApplied] != "1" || env[envID] != rt.ID() || env[envPortPrefix+"8080"] != "9080" {
		t.Fatalf("missing isolation markers: %#v", env)
	}
}

func TestEnvCanPreserveExplicitValues(t *testing.T) {
	clearIsolationEnv(t)
	dir := linkedWorktree(t)
	writeFile(t, filepath.Join(dir, "gofastr.yml"), "isolation:\n  enabled: true\n  port:\n    offset: 1000\n    range: 1\n    scan: 0\n")
	rt, err := Resolve(dir)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	env := envMap(rt.Env([]string{
		envRewriteExplicit + "=0",
		"PORT=localhost:8080",
		"DATABASE_URL=postgres://localhost:5432/app",
	}))
	if env["PORT"] != "localhost:8080" {
		t.Fatalf("PORT = %q, want explicit value preserved", env["PORT"])
	}
	if env["DATABASE_URL"] != "postgres://localhost:5432/app" {
		t.Fatalf("DATABASE_URL = %q, want explicit value preserved", env["DATABASE_URL"])
	}
}

func TestDatabaseRewritesSQLiteAndPostgres(t *testing.T) {
	clearIsolationEnv(t)
	dir := linkedWorktree(t)
	writeFile(t, filepath.Join(dir, "gofastr.yml"), "isolation:\n  enabled: true\n")
	rt, err := Resolve(dir)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	_, sqliteDSN, err := rt.Database("sqlite3", "file:app.db?_foreign_keys=on")
	if err != nil {
		t.Fatalf("sqlite Database: %v", err)
	}
	wantDir := filepath.Join(dir, ".gofastr", "isolation", rt.ID())
	if !strings.HasPrefix(strings.TrimPrefix(sqliteDSN, "file:"), wantDir) || !strings.HasSuffix(sqliteDSN, "app.db?_foreign_keys=on") {
		t.Fatalf("sqlite dsn = %q, want under %s", sqliteDSN, wantDir)
	}
	if _, err := os.Stat(wantDir); err != nil {
		t.Fatalf("sqlite isolation dir was not created: %v", err)
	}

	_, pgDSN, err := rt.Database("postgres", "postgres://user:pass@localhost:5432/app?sslmode=disable")
	if err != nil {
		t.Fatalf("postgres Database: %v", err)
	}
	if !strings.Contains(pgDSN, "/app_"+rt.ID()+"?sslmode=disable") {
		t.Fatalf("postgres dsn = %q, want database suffix", pgDSN)
	}
}

func linkedWorktree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".git"), "gitdir: "+filepath.Join(t.TempDir(), ".git", "worktrees", "feature")+"\n")
	return dir
}

func clearIsolationEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		envIsolation,
		envApplied,
		envID,
		envRewriteExplicit,
		envPortPrefix + "8080",
		envPortPrefix + "9080",
		envPortPrefix + "redis",
	} {
		t.Setenv(key, "")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRenderPortValueKeepsPortOnlyShape(t *testing.T) {
	if got := renderPortValue("8080", 9080); got != strconv.Itoa(9080) {
		t.Fatalf("renderPortValue = %q", got)
	}
}

// Postgres identifiers are capped at 63 bytes by default
// (NAMEDATALEN-1). A long DB name + suffix silently truncates server-side,
// so two worktrees that differ only past byte 63 would collide.
// The rewrite must keep the final db name <= 63 bytes.
func TestPostgresDSNFitsIdentifierLimit(t *testing.T) {
	clearIsolationEnv(t)
	dir := linkedWorktree(t)
	writeFile(t, filepath.Join(dir, "gofastr.yml"), "isolation:\n  enabled: true\n")
	rt, err := Resolve(dir)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	longName := strings.Repeat("a", 60) // 60 + 14 ("_wt_xxxxxxxxxx") = 74, overflows
	dsn := "postgres://u:p@localhost:5432/" + longName + "?sslmode=disable"
	_, rewritten, err := rt.Database("postgres", dsn)
	if err != nil {
		t.Fatalf("Database: %v", err)
	}
	u, err := url.Parse(rewritten)
	if err != nil {
		t.Fatalf("parse rewritten: %v", err)
	}
	db := strings.TrimPrefix(u.Path, "/")
	if len(db) > 63 {
		t.Fatalf("rewritten db = %q (len %d), must be <= 63 to avoid Postgres silent truncation", db, len(db))
	}
	// And the suffix (or some unique-ID fragment) must remain present so two
	// worktrees with the same base name don't collide on the truncated form.
	if !strings.Contains(db, rt.ID()[len(rt.ID())-6:]) {
		t.Fatalf("rewritten db = %q missing ID fragment; isolation lost", db)
	}
}

// Two Runtime instances in the same process must not alias each other's
// in-memory port mappings. With a package-global cache, rt2 inherits rt1's
// mapping for port 8080 even though rt2's id and offset differ.
func TestRuntimePortStateIsPerInstance(t *testing.T) {
	clearIsolationEnv(t)
	dir1 := linkedWorktree(t)
	writeFile(t, filepath.Join(dir1, "gofastr.yml"),
		"isolation:\n  enabled: true\n  port:\n    offset: 1000\n    range: 1\n    scan: 0\n")
	dir2 := linkedWorktree(t)
	writeFile(t, filepath.Join(dir2, "gofastr.yml"),
		"isolation:\n  enabled: true\n  port:\n    offset: 2000\n    range: 1\n    scan: 0\n")

	rt1, err := Resolve(dir1)
	if err != nil {
		t.Fatalf("Resolve dir1: %v", err)
	}
	rt2, err := Resolve(dir2)
	if err != nil {
		t.Fatalf("Resolve dir2: %v", err)
	}
	addr1, err := rt1.Addr(":8080")
	if err != nil {
		t.Fatalf("rt1.Addr: %v", err)
	}
	addr2, err := rt2.Addr(":8080")
	if err != nil {
		t.Fatalf("rt2.Addr: %v", err)
	}
	if addr1 == addr2 {
		t.Fatalf("rt1 and rt2 both mapped :8080 to %q — port cache is leaking across Runtime instances", addr1)
	}
}

// A misconfigured scan value must not hang startup for thousands of
// net.Listen attempts. The resolver caps scan at a sane upper bound.
func TestPortScanIsClamped(t *testing.T) {
	clearIsolationEnv(t)
	dir := linkedWorktree(t)
	writeFile(t, filepath.Join(dir, "gofastr.yml"),
		"isolation:\n  enabled: true\n  port:\n    scan: 1000000\n")
	rt, err := Resolve(dir)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if rt.cfg.Port.Scan > portScanMax {
		t.Fatalf("Port.Scan = %d, want clamped to <= %d", rt.cfg.Port.Scan, portScanMax)
	}
}

// Resolve must not walk past the git root looking for gofastr.yml.
// Otherwise a parent workspace's config silently applies to nested projects.
func TestDiscoverConfigBoundedAtGitRoot(t *testing.T) {
	clearIsolationEnv(t)
	parent := t.TempDir()
	// Parent has a gofastr.yml that should NOT be picked up.
	writeFile(t, filepath.Join(parent, "gofastr.yml"), "isolation:\n  enabled: true\n  port:\n    offset: 5000\n")
	// Project is a linked worktree inside parent, with its own .git pointer
	// and no gofastr.yml of its own.
	project := filepath.Join(parent, "project")
	writeFile(t, filepath.Join(project, ".git"), "gitdir: "+filepath.Join(t.TempDir(), ".git", "worktrees", "feature")+"\n")

	rt, err := Resolve(project)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	// The parent's offset=5000 must NOT have been loaded.
	if rt.cfg.Port.Offset == 5000 {
		t.Fatalf("Resolve walked past git root and picked up parent gofastr.yml (offset=%d)", rt.cfg.Port.Offset)
	}
}

