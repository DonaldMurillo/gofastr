package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveDevIsolationRemapsAddrAndChildEnv(t *testing.T) {
	for _, key := range []string{
		"GOFASTR_ISOLATION",
		"GOFASTR_ISOLATION_APPLIED",
		"GOFASTR_ISOLATION_ID",
		"GOFASTR_ISOLATION_PORT_8080",
	} {
		t.Setenv(key, "")
	}
	dir := t.TempDir()
	writeDevFile(t, filepath.Join(dir, ".git"), "gitdir: "+filepath.Join(t.TempDir(), ".git", "worktrees", "feature")+"\n")
	writeDevFile(t, filepath.Join(dir, "gofastr.yml"), `
isolation:
  enabled: true
  port:
    offset: 1000
    range: 1
    scan: 0
`)

	rt, addr, err := resolveDevIsolation(dir, "localhost:8080")
	if err != nil {
		t.Fatalf("resolveDevIsolation: %v", err)
	}
	if !rt.Active() {
		t.Fatal("expected dev isolation to be active")
	}
	if addr != "localhost:9080" {
		t.Fatalf("addr = %q, want localhost:9080", addr)
	}
	env := devEnvMap(rt.Env([]string{"PORT=localhost:8080"}))
	if env["PORT"] != "localhost:9080" {
		t.Fatalf("child PORT = %q, want localhost:9080", env["PORT"])
	}
	if env["GOFASTR_ISOLATION_APPLIED"] != "1" || env["GOFASTR_ISOLATION_PORT_8080"] != "9080" {
		t.Fatalf("child env missing isolation markers: %#v", env)
	}
}

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

	writeMainGo("demo", "example.com/demo", false, "sqlite", "file:demo.db")
	data, err := os.ReadFile(filepath.Join("demo", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{
		`"github.com/DonaldMurillo/gofastr/framework/isolation"`,
		`runtimeIsolation, err := isolation.Resolve(".")`,
		`runtimeIsolation.Database("sqlite3", getEnv("DATABASE_URL", "file:demo.db"))`,
		`runtimeIsolation.Addr(getEnv("PORT", "localhost:8080"))`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("generated main.go missing %q:\n%s", want, got)
		}
	}
}

// The generated gofastr.yml must not advertise strategy values that the
// isolation resolver doesn't consult — a reader who edits "strategy: path"
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

	writeIsolationConfig("demo", "sqlite")
	data, err := os.ReadFile(filepath.Join("demo", "gofastr.yml"))
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

func writeDevFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func devEnvMap(env []string) map[string]string {
	out := map[string]string{}
	for _, pair := range env {
		for i := 0; i < len(pair); i++ {
			if pair[i] == '=' {
				out[pair[:i]] = pair[i+1:]
				break
			}
		}
	}
	return out
}
