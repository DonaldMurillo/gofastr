package framework_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// withCWD chdirs into dir for the duration of the test and restores
// the previous working directory on cleanup.
func withCWD(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
}

// TestNewApp_AutoLoadsDotEnv pins the auto-load contract: dropping a
// .env into the CWD before NewApp causes its keys to land in
// os.Environ where any subsequent code (options, plugins, batteries)
// can see them.
func TestNewApp_AutoLoadsDotEnv(t *testing.T) {
	dir := t.TempDir()
	withCWD(t, dir)

	const k = "GOFASTR_AUTOLOAD_TEST_K"
	os.Unsetenv(k)
	t.Cleanup(func() { os.Unsetenv(k) })

	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(k+"=hello\n"), 0o644); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	_ = framework.NewApp()

	if got := os.Getenv(k); got != "hello" {
		t.Fatalf("after NewApp the .env value must be on env; got %q", got)
	}
}

// TestNewApp_ExistingEnvWinsOverDotEnv pins the precedence contract:
// operator-set env always wins over a file value.
func TestNewApp_ExistingEnvWinsOverDotEnv(t *testing.T) {
	dir := t.TempDir()
	withCWD(t, dir)

	const k = "GOFASTR_AUTOLOAD_PRECEDENCE_K"
	t.Setenv(k, "from-env")

	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(k+"=from-file\n"), 0o644); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	_ = framework.NewApp()

	if got := os.Getenv(k); got != "from-env" {
		t.Fatalf("existing env must win; got %q (the .env value would mean we clobbered)", got)
	}
}

// TestNewApp_GofastrDotEnvOffSkipsLoad pins the kill switch: setting
// GOFASTR_DOTENV=off in the real env disables the auto-load even
// when a .env is present.
func TestNewApp_GofastrDotEnvOffSkipsLoad(t *testing.T) {
	dir := t.TempDir()
	withCWD(t, dir)

	const k = "GOFASTR_AUTOLOAD_OFF_K"
	os.Unsetenv(k)
	t.Cleanup(func() { os.Unsetenv(k) })

	t.Setenv("GOFASTR_DOTENV", "off")
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(k+"=should-not-load\n"), 0o644); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	_ = framework.NewApp()

	if got := os.Getenv(k); got != "" {
		t.Fatalf("GOFASTR_DOTENV=off must suppress auto-load; got %q", got)
	}
}

// TestNewApp_AppEnvSelectsOverlay pins the .env.<APP_ENV> overlay:
// when APP_ENV=production is set, .env.production wins over .env on
// conflict.
func TestNewApp_AppEnvSelectsOverlay(t *testing.T) {
	dir := t.TempDir()
	withCWD(t, dir)

	const k = "GOFASTR_AUTOLOAD_OVERLAY_K"
	os.Unsetenv(k)
	t.Cleanup(func() { os.Unsetenv(k) })

	t.Setenv("APP_ENV", "production")
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(k+"=base\n"), 0o644); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env.production"), []byte(k+"=prod\n"), 0o644); err != nil {
		t.Fatalf("write .env.production: %v", err)
	}

	_ = framework.NewApp()

	if got := os.Getenv(k); got != "prod" {
		t.Fatalf(".env.production must override .env; got %q", got)
	}
}
