package dotenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The documented ${NAME} lookup order is: the value an earlier file in
// the precedence chain set, then this file's earlier keys, then
// os.Environ, then empty. Load used to expand each file against its own
// keys only, so the docs' own example PATH_TPL="${HOME}/bin" loaded as
// "/bin", and a .env DATABASE_URL built from ${DB_HOST} kept the .env
// host while .env.local's DB_HOST was the one applied.

func writeEnvFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoad_ExpandFallsBackToEnviron(t *testing.T) {
	t.Setenv("GOFASTR_DOTENV_TEST_HOME", "/home/op")
	dir := t.TempDir()
	p := writeEnvFile(t, dir, ".env", `PATH_TPL="${GOFASTR_DOTENV_TEST_HOME}/bin"`+"\n")
	got, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if got["PATH_TPL"] != "/home/op/bin" {
		t.Fatalf("PATH_TPL = %q, want /home/op/bin", got["PATH_TPL"])
	}
}

func TestLoad_ExpandReadsEarlierFileValue(t *testing.T) {
	dir := t.TempDir()
	local := writeEnvFile(t, dir, ".env.local", "DB_HOST=db.internal\n")
	base := writeEnvFile(t, dir, ".env", "DB_HOST=localhost\nDATABASE_URL=\"postgres://${DB_HOST}/app\"\n")
	got, err := Load(local, base)
	if err != nil {
		t.Fatal(err)
	}
	if got["DB_HOST"] != "db.internal" {
		t.Fatalf("DB_HOST = %q, want the .env.local value", got["DB_HOST"])
	}
	if got["DATABASE_URL"] != "postgres://db.internal/app" {
		t.Fatalf("DATABASE_URL = %q, want it built from the DB_HOST Load keeps", got["DATABASE_URL"])
	}
}

func TestLoad_SameFileKeyBeatsEnviron(t *testing.T) {
	t.Setenv("GOFASTR_DOTENV_TEST_X", "from-env")
	dir := t.TempDir()
	p := writeEnvFile(t, dir, ".env", "GOFASTR_DOTENV_TEST_X=from-file\nY=\"${GOFASTR_DOTENV_TEST_X}\"\n")
	got, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if got["Y"] != "from-file" {
		t.Fatalf("Y = %q, want from-file (documented order: file keys before os.Environ)", got["Y"])
	}
}

func TestParse_ReadsNoEnvironment(t *testing.T) {
	t.Setenv("GOFASTR_DOTENV_TEST_ENV", "leaked")
	got, err := Parse(strings.NewReader(`A="${GOFASTR_DOTENV_TEST_ENV}"`))
	if err != nil {
		t.Fatal(err)
	}
	if got["A"] != "" {
		t.Fatalf("Parse read the environment: A = %q", got["A"])
	}
}

// An environment value referenced once is input, not multiplication: a
// long PATH in a one-line file must not trip the expansion budget. The
// same value repeated many times on one line still does.
func TestLoad_EnvValueCountsAsBudgetInput(t *testing.T) {
	t.Setenv("GOFASTR_DOTENV_TEST_BIG", strings.Repeat("p", 4096))
	dir := t.TempDir()
	once := writeEnvFile(t, dir, "once", `P="${GOFASTR_DOTENV_TEST_BIG}"`+"\n")
	got, err := Load(once)
	if err != nil {
		t.Fatalf("one reference to a long env value refused: %v", err)
	}
	if len(got["P"]) != 4096 {
		t.Fatalf("len(P) = %d, want 4096", len(got["P"]))
	}
	many := writeEnvFile(t, dir, "many", `P="`+strings.Repeat("${GOFASTR_DOTENV_TEST_BIG}", 64)+`"`+"\n")
	if _, err := Load(many); err == nil || !strings.Contains(err.Error(), "expansion exceeded") {
		t.Fatalf("64 copies of a 4 KiB env value: err = %v, want the expansion budget refusal", err)
	}
}
