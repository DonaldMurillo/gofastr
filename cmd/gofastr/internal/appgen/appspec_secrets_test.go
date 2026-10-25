package appgen

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/internal/dsnredact"
)

// Secrets from the app spec (JWT signing key, DB credentials, seed admin
// password) must never land as literals in generated Go source. That
// source gets committed. They belong in the generated .env (gitignored via
// the generated .gitignore); the code reads them through getEnv.
func TestAppSpecNeverInlinesSecrets(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	clitest.WriteTestFile(t, path, `
app:
  name: Secrets
  module: example.com/secrets
  db:
    driver: postgres
    url: postgres://app:sup3rsecretpw@db:5432/app
  auth:
    enabled: true
    dev_mode: false
    jwt_secret: topsecret-jwt-key
  admin:
    enabled: true
    login_path: /login
    seed_email: admin@example.com
    seed_password: hunter2-admin
entities:
  - name: posts
    fields:
      - name: title
        type: string
screens:
  - name: dashboard
    route: /dashboard
    access:
      auth: true
    body:
      - kind: entity_list
        entity: posts
        fields: [title]
`)
	bp, err := appspec.LoadAppSpec(path)
	if err != nil {
		t.Fatalf("loadAppSpec: %v", err)
	}
	files := mustRenderAppSpecFiles(t, bp)
	byName := filesByName(files)
	if _, ok := byName["e2e_test.go"]; !ok {
		t.Fatal("expected e2e_test.go to be generated (it used to inline the admin password)")
	}

	secrets := map[string]string{
		"db password":   "sup3rsecretpw",
		"jwt secret":    "topsecret-jwt-key",
		"seed password": "hunter2-admin",
	}
	for _, f := range files {
		if !strings.HasSuffix(f.Name, ".go") {
			continue
		}
		for what, lit := range secrets {
			if strings.Contains(f.Content, lit) {
				t.Errorf("%s: generated Go source inlines the %s %q — it would be committed", f.Name, what, lit)
			}
		}
	}

	env := byName[".env"]
	if env == "" {
		t.Fatalf("generator emitted no .env; files: %v", fileNames(files))
	}
	for what, lit := range secrets {
		if !strings.Contains(env, lit) {
			t.Errorf(".env missing the %s (%q):\n%s", what, lit, env)
		}
	}

	gitignore := byName[".gitignore"]
	if !strings.Contains(gitignore, ".env") {
		t.Fatalf("generated .gitignore must ignore .env; got:\n%q", gitignore)
	}
	// The default SQLite DSN writes <name>.db plus its WAL/SHM siblings in
	// the module root, and `gofastr build` writes bin/server: a first
	// `git add .` committed the dev database (seeded admin hash included)
	// and the binary.
	for _, line := range []string{"bin/", "*.db", "*.db-shm", "*.db-wal"} {
		if !slices.Contains(strings.Split(gitignore, "\n"), line) {
			t.Errorf("generated .gitignore does not ignore %s; got:\n%s", line, gitignore)
		}
	}

	// The runtime must actually read the env vars the .env carries.
	appGo := byName["app.go"]
	for _, want := range []string{`os.Getenv("JWT_SECRET")`, `os.Getenv("ADMIN_SEED_PASSWORD")`} {
		if !strings.Contains(appGo, want) {
			t.Errorf("app.go missing %s", want)
		}
	}
	mainGo := byName["main.go"]
	if !strings.Contains(mainGo, `getEnv("DATABASE_URL"`) {
		t.Errorf("main.go missing getEnv(\"DATABASE_URL\"):\n%s", mainGo)
	}
	// .env must be loaded before openDB. NewApp's auto-load
	// happens after the DB has already been opened.
	if !strings.Contains(mainGo, "dotenv.LoadAndApply") {
		t.Errorf("main.go must load .env before opening the DB:\n%s", mainGo)
	}
}

// Without secrets in the app spec there is nothing to hide: no .env is
// emitted, and sqlite file DSNs stay inline (they hold no credentials).
func TestAppSpecNoEnvWithoutSecrets(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	clitest.WriteTestFile(t, path, `
app:
  name: Plain
  module: example.com/plain
entities:
  - name: posts
    fields:
      - name: title
        type: string
`)
	bp, err := appspec.LoadAppSpec(path)
	if err != nil {
		t.Fatalf("loadAppSpec: %v", err)
	}
	byName := filesByName(mustRenderAppSpecFiles(t, bp))
	if env, ok := byName[".env"]; ok && env != "" {
		t.Fatalf("secret-free app spec should not emit .env, got:\n%s", env)
	}
}

func fileNames(files []GeneratedFile) []string {
	names := make([]string, len(files))
	for i, f := range files {
		names[i] = f.Name
	}
	return names
}

// DSNs the URL parser rejects and libpq-quoted passwords must still be
// detected and fully redacted. Both were paths for the credential to
// land verbatim (or partially) in committed source.
func TestDSNRedactionFailsClosed(t *testing.T) {
	cases := map[string]struct{ dsn, redacted string }{
		"unparseable url": {
			// '%zz' is an invalid escape: url.Parse errors on this DSN.
			dsn:      "postgres://app:pa%zzword@db:5432/app",
			redacted: "postgres://app@db:5432/app",
		},
		"quoted kv password": {
			dsn:      "host=db user=app password='se cret' dbname=x", // not-a-secret: fixture exercising the DSN redactor
			redacted: "host=db user=app dbname=x",
		},
		"plain kv password": {
			dsn:      "host=db password=pw dbname=x",
			redacted: "host=db dbname=x",
		},
	}
	for name, c := range cases {
		if !dsnredact.HasSecret(c.dsn) {
			t.Errorf("%s: dsnredact.HasSecret(%q) = false, want true", name, c.dsn)
		}
		if got := redactDSN(c.dsn); got != c.redacted {
			t.Errorf("%s: redactDSN(%q) = %q, want %q", name, c.dsn, got, c.redacted)
		}
	}
}
