package main

// Defect C regression: on a fresh DB, owner-scoped seed rows must exist after
// first boot. The data-seed hook resolves the bootstrap admin to stamp the
// owner column; if the admin-seed hook runs LATER (registration order), the
// admin does not exist yet, the owner-context guard rejects the writes, and
// the rows are silently missing. Boots a generated app against a fresh
// SQLite file DB and queries it directly. Gated by -short (compiles + boots
// the binary).

import (
	"context"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

const seedOrderBlueprintYAML = `app:
  name: SeedOrder
  module: example.com/seedorder
  db:
    driver: sqlite
    url: file:seedorder.db
  auth:
    enabled: true
  admin:
    enabled: true
    seed_email: admin@example.com
    seed_password: seed-pw-123
entities:
  - name: posts
    crud: true
    owner_field: owner_id
    fields:
      - name: title
        type: string
        required: true
screens:
  - name: home
    route: /
    title: Home
    body:
      - type: heading
        level: 1
        text: SeedOrder
seed:
  - entity: posts
    rows:
      - title: Owner-scoped seed post
`

func TestBlueprintSeed_OwnerScopedRowsOnFreshDB(t *testing.T) {
	if testing.Short() {
		t.Skip("boots a generated app against a fresh DB")
	}
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	goVersion, err := repoGoVersion(repoRoot)
	if err != nil {
		t.Fatalf("repoGoVersion: %v", err)
	}
	goMod := "module example.com/seedorder\n\ngo " + goVersion + "\n\nrequire github.com/DonaldMurillo/gofastr v0.0.0\n\nreplace github.com/DonaldMurillo/gofastr => " + repoRoot + "\n"
	writeTestFile(t, filepath.Join(dir, "go.mod"), goMod)
	if err := copyGoSum(repoRoot, dir); err != nil {
		t.Fatalf("copy go.sum: %v", err)
	}
	writeTestFile(t, filepath.Join(dir, "gofastr.yml"), seedOrderBlueprintYAML)

	generate := exec.Command("go", "run", filepath.Join(repoRoot, "cmd", "gofastr"), "generate", "--from=gofastr.yml")
	generate.Dir = dir
	if out, err := generate.CombinedOutput(); err != nil {
		t.Fatalf("gofastr generate: %v\n%s", err, out)
	}
	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = dir
	if out, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy: %v\n%s", err, out)
	}
	appBin := testExecutablePath(filepath.Join(dir, "app"))
	build := exec.Command("go", "build", "-o", appBin, ".")
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	dbFile := filepath.Join(dir, "seedorder.db")
	port := nextE2EPort(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app := exec.CommandContext(ctx, appBin)
	app.Dir = dir
	app.Env = append(os.Environ(),
		"PORT=localhost:"+port,
		"DATABASE_URL=file:"+dbFile,
		"DB_DRIVER=sqlite3",
		"ADMIN_SEED_PASSWORD=seed-pw-123",
		"JWT_SECRET=test-jwt-secret-for-seed-order",
		"GOFASTR_ISOLATION=off",
	)
	var appOut syncBuffer
	app.Stdout = &appOut
	app.Stderr = &appOut
	configureTestProcessGroup(app)
	if err := app.Start(); err != nil {
		t.Fatalf("start app: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		_ = killTestProcessTree(app)
		_ = app.Wait()
	})

	base := "http://localhost:" + port
	waitForBody(t, base+"/", 90*time.Second, &appOut)

	// Query the DB file directly: the seed must have written owner-scoped rows
	// stamped with the bootstrap admin's id. With the bug the data-seed hook
	// runs before the admin exists, so the owner-context guard rejects every
	// CreateOne and the table stays empty.
	dbq, err := sql.Open("sqlite3", dbFile)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer dbq.Close()
	var n int
	if err := dbq.QueryRow("SELECT COUNT(*) FROM posts").Scan(&n); err != nil {
		t.Fatalf("count posts: %v", err)
	}
	if n < 1 {
		t.Errorf("owner-scoped seed rows missing on fresh DB: got %d rows, want >= 1. App output:\n%s", n, appOut.String())
	}
	var ownerID string
	if err := dbq.QueryRow("SELECT owner_id FROM posts LIMIT 1").Scan(&ownerID); err != nil {
		t.Errorf("read owner_id: %v", err)
	} else if ownerID == "" {
		t.Errorf("seeded post has empty owner_id (admin principal not on context): app output:\n%s", appOut.String())
	} else {
		// Stronger: the owner must be the bootstrap admin, not some other user.
		var adminID string
		if qerr := dbq.QueryRow("SELECT id FROM auth_users WHERE email = 'admin@example.com'").Scan(&adminID); qerr != nil {
			t.Logf("could not read admin id for comparison: %v (non-fatal)", qerr)
		} else if adminID != ownerID {
			t.Errorf("seeded post owner_id %q != bootstrap admin id %q", ownerID, adminID)
		} else {
			t.Logf("seeded post owner_id %q matches bootstrap admin", ownerID)
		}
	}
	if t.Failed() {
		t.FailNow()
	}
}
