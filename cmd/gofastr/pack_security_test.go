package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// Property: a CLI-written artifact that carries secrets recovered from .env
// is owner-only on disk (create AND overwrite), and pack's "this output
// contains secrets — do NOT commit it" warning fires for every DSN class
// the generator itself treats as secret. `gofastr pack` and `gofastr
// generate` form a round-trip pair over the same credentials: what generate
// hides in the gitignored .env, pack rehydrates into its output, so the two
// detectors and the two writers must not diverge.

// TestPackOutOwnerOnlyOnOverwrite: `pack -o out.yml` writes 0600, but
// os.WriteFile's mode argument only applies at CREATE — over a pre-existing
// 0644 out.yml (e.g. one left by `gofastr pack > out.yml`, which the
// generated .gitignore does not cover) the secrets land and the file stays
// world-readable. runPack's own comment ("Warn loudly and write 0600 rather
// than 0644") is the contract; init's writeEnvFile shows the repo pattern:
// open-handle chmod before the write so overwrite tightens too.
func TestPackOutOwnerOnlyOnOverwrite(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits")
	}
	bp := Blueprint{
		App: BlueprintApp{
			Name:     "Packperm",
			Module:   "example.com/packperm",
			DBDriver: "postgres",
			DBURL:    "postgres://user:pw@db:5432/packperm",
			Auth:     BlueprintAuth{Enabled: true, JWTSecret: "shhh"},
		},
		Entities: []framework.EntityDeclaration{{
			Name:   "notes",
			Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}},
		}},
	}
	// The on-disk shape `gofastr pack` reads back: a generated app whose
	// .env carries DATABASE_URL and JWT_SECRET.
	dir := materializeBlueprint(t, bp)
	if _, err := os.Stat(filepath.Join(dir, ".env")); err != nil {
		t.Fatalf("generated app has no .env — pack would recover no secrets and the property is untested: %v", err)
	}

	out := filepath.Join(t.TempDir(), "out.yml")
	if err := os.WriteFile(out, []byte("stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(out, 0o644); err != nil {
		t.Fatal(err) // defeat umask so the precondition is exact
	}

	exitCode := 0
	origExit := osExit
	osExit = func(c int) { exitCode = c }
	defer func() { osExit = origExit }()
	runPack([]string{dir, "-o", out})
	if exitCode != 0 {
		t.Fatalf("runPack exited %d", exitCode)
	}

	// The output must actually carry the rehydrated secret, or the mode
	// assertion below is vacuous.
	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "postgres://user:pw@db:5432/packperm") {
		t.Fatalf("packed output does not carry the credentialed DSN — secrets were not rehydrated, so this run exercised the unwarned path: %q", body)
	}

	info, err := os.Stat(out)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got&0o077 != 0 {
		t.Errorf("SECURITY: pack -o output is %04o — secret-bearing yml readable beyond its owner", got)
	}
}

// TestPackDetectsKeywordDSNSecret: secretsInBlueprint must flag every DSN
// class the generator's own dsnHasSecret hides. generate routes a
// keyword/value DSN (`host=db user=app password=hunter2 dbname=app`) into
// the gitignored .env precisely because dsnHasSecret sees the `password=`
// pair; pack rehydrates that DSN from the same .env, checks it for "@" only,
// and packs it back into the yml with no warning — on the default no -o
// path that yml goes to stdout, and the shell redirect creates a 0644 file
// nothing gitignores.
func TestPackDetectsKeywordDSNSecret(t *testing.T) {
	for _, tc := range []struct {
		name string
		dsn  string
		want bool
	}{
		{"keyword/value DSN with password", "host=db user=app password=hunter2 dbname=app", true},
		{"URL DSN with password", "postgres://user:pw@db:5432/app", true},
		{"sqlite file DSN", "file:app.db", false},
		{"unparseable URL DSN with userinfo", "postgres://user:pw%zz@db:5432/app", true},
	} {
		bp := Blueprint{App: BlueprintApp{DBURL: tc.dsn}}
		got := secretsInBlueprint(bp)
		if got != tc.want {
			t.Errorf("%s: secretsInBlueprint(%q) = %v, want %v", tc.name, tc.dsn, got, tc.want)
		}
		// Parity with the generator's detector: any DSN generate hides
		// from committed source, pack must warn on.
		if gen := dsnHasSecret(tc.dsn); gen && !got {
			t.Errorf("%s: dsnHasSecret(%q) = true but secretsInBlueprint = false — pack's do-NOT-commit warning silently disappears for a DSN class the generator itself hides", tc.name, tc.dsn)
		}
	}
}
