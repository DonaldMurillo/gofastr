package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/dotenv"
)

// Secrets from the blueprint (JWT signing key, DB credentials, seed admin
// password) must never land as literals in generated Go source — that
// source gets committed. They belong in the generated .env (gitignored via
// the generated .gitignore); the code reads them through getEnv.
func TestBlueprintNeverInlinesSecrets(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	writeTestFile(t, path, `
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
	bp, err := loadBlueprint(path)
	if err != nil {
		t.Fatalf("loadBlueprint: %v", err)
	}
	files := mustRenderBlueprintFiles(t, bp)
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
		if !strings.HasSuffix(f.name, ".go") {
			continue
		}
		for what, lit := range secrets {
			if strings.Contains(f.content, lit) {
				t.Errorf("%s: generated Go source inlines the %s %q — it would be committed", f.name, what, lit)
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

	// The runtime must actually read the env vars the .env carries.
	appGo := byName[filepath.Join("blueprint", "app.go")]
	for _, want := range []string{`os.Getenv("JWT_SECRET")`, `os.Getenv("ADMIN_SEED_PASSWORD")`} {
		if !strings.Contains(appGo, want) {
			t.Errorf("blueprint/app.go missing %s", want)
		}
	}
	mainGo := byName["main.go"]
	if !strings.Contains(mainGo, `getEnv("DATABASE_URL"`) {
		t.Errorf("main.go missing getEnv(\"DATABASE_URL\"):\n%s", mainGo)
	}
	// .env must be loaded before openBlueprintDB — NewApp's auto-load
	// happens after the DB has already been opened.
	if !strings.Contains(mainGo, "dotenv.LoadAndApply") {
		t.Errorf("main.go must load .env before opening the DB:\n%s", mainGo)
	}
}

// Without secrets in the blueprint there is nothing to hide: no .env is
// emitted, and sqlite file DSNs stay inline (they hold no credentials).
func TestBlueprintNoEnvWithoutSecrets(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	writeTestFile(t, path, `
app:
  name: Plain
  module: example.com/plain
entities:
  - name: posts
    fields:
      - name: title
        type: string
`)
	bp, err := loadBlueprint(path)
	if err != nil {
		t.Fatalf("loadBlueprint: %v", err)
	}
	byName := filesByName(mustRenderBlueprintFiles(t, bp))
	if env, ok := byName[".env"]; ok && env != "" {
		t.Fatalf("secret-free blueprint should not emit .env, got:\n%s", env)
	}
}

func fileNames(files []generatedFile) []string {
	names := make([]string, len(files))
	for i, f := range files {
		names[i] = f.name
	}
	return names
}

// DSNs the URL parser rejects and libpq-quoted passwords must still be
// detected and fully redacted — both were paths for the credential to
// land verbatim (or partially) in committed source.
func TestDSNRedactionFailsClosed(t *testing.T) {
	cases := map[string]struct{ dsn, redacted string }{
		"unparseable url": {
			// '%zz' is an invalid escape: url.Parse errors on this DSN.
			dsn:      "postgres://app:pa%zzword@db:5432/app",
			redacted: "postgres://app@db:5432/app",
		},
		"quoted kv password": {
			dsn:      "host=db user=app password='se cret' dbname=x",
			redacted: "host=db user=app dbname=x",
		},
		"plain kv password": {
			dsn:      "host=db password=pw dbname=x",
			redacted: "host=db dbname=x",
		},
	}
	for name, c := range cases {
		if !dsnHasSecret(c.dsn) {
			t.Errorf("%s: dsnHasSecret(%q) = false, want true", name, c.dsn)
		}
		if got := redactDSN(c.dsn); got != c.redacted {
			t.Errorf("%s: redactDSN(%q) = %q, want %q", name, c.dsn, got, c.redacted)
		}
	}
}

// Values the generator writes to .env must round-trip through BOTH
// readers of that file: core/dotenv (the generated app and its e2e test)
// and `gofastr pack`. Quote-leading or space-edged secrets used to be
// written bare, so one reader mangled them and the other failed outright.
func TestEnvValuesRoundTrip(t *testing.T) {
	jwt := `"leading quote" and $dollar`
	seed := `'quoted' #hash \backslash`
	dsn := "postgres://app:plainpw@db:5432/app"

	var bp Blueprint
	bp.App.Name = "RT"
	bp.App.Module = "example.com/rt"
	bp.App.DBURL = dsn
	bp.App.Auth.Enabled = true
	bp.App.Auth.JWTSecret = jwt
	bp.App.Admin.SeedEmail = "admin@example.com"
	bp.App.Admin.SeedPassword = seed

	env := renderBlueprintEnv(bp)
	want := map[string]string{"JWT_SECRET": jwt, "DATABASE_URL": dsn, "ADMIN_SEED_PASSWORD": seed}

	parsed, err := dotenv.Parse(strings.NewReader(env))
	if err != nil {
		t.Fatalf("core/dotenv cannot parse the generated .env: %v\n%s", err, env)
	}
	for k, v := range want {
		if parsed[k] != v {
			t.Errorf("dotenv round-trip %s = %q, want %q", k, parsed[k], v)
		}
	}

	path := filepath.Join(t.TempDir(), ".env")
	writeTestFile(t, path, env)
	packed := packReadDotEnv(path)
	for k, v := range want {
		if packed[k] != v {
			t.Errorf("pack round-trip %s = %q, want %q", k, packed[k], v)
		}
	}
}
