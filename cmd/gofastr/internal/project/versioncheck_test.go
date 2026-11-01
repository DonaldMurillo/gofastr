package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
)

func writeGoMod(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "internal", "app")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	return sub
}

func withCLIVersion(t *testing.T, v string) {
	t.Helper()
	old := cli.Version
	cli.Version = v
	t.Cleanup(func() { cli.Version = old })
}

func TestVersionMismatchWarns(t *testing.T) {
	withCLIVersion(t, "v0.94.1")
	t.Setenv(EnvNoVersionCheck, "")
	dir := writeGoMod(t, "module example.com/app\n\ngo 1.24\n\nrequire github.com/DonaldMurillo/gofastr v0.95.0\n")
	got := VersionMismatch(dir)
	if !strings.Contains(got, "v0.95.0") || !strings.Contains(got, "v0.94.1") || !strings.Contains(got, "go install github.com/DonaldMurillo/gofastr/cmd/gofastr@v0.95.0") {
		t.Fatalf("VersionMismatch = %q; want both versions and the install command", got)
	}
}

func TestVersionMismatchQuiet(t *testing.T) {
	t.Setenv(EnvNoVersionCheck, "")
	same := writeGoMod(t, "module example.com/app\n\nrequire github.com/DonaldMurillo/gofastr v0.95.0\n")
	// The replaced module asks for another version, so only the replace
	// rule can keep this quiet.
	replaced := writeGoMod(t, "module example.com/app\n\nrequire github.com/DonaldMurillo/gofastr v0.90.0\n\nreplace github.com/DonaldMurillo/gofastr => ../gofastr\n")
	other := writeGoMod(t, "module example.com/app\n\nrequire example.com/x v1.0.0\n")
	self := writeGoMod(t, "module github.com/DonaldMurillo/gofastr\n")

	withCLIVersion(t, "v0.95.0")
	for name, dir := range map[string]string{"same version": same, "replace": replaced, "no gofastr": other, "the framework itself": self} {
		if got := VersionMismatch(dir); got != "" {
			t.Errorf("%s: VersionMismatch = %q; want quiet", name, got)
		}
	}
	withCLIVersion(t, "dev")
	if got := VersionMismatch(same); got != "" {
		t.Errorf("dev CLI: VersionMismatch = %q; want quiet", got)
	}
	withCLIVersion(t, "v0.94.1")
	t.Setenv(EnvNoVersionCheck, "1")
	if got := VersionMismatch(same); got != "" {
		t.Errorf("opted out: VersionMismatch = %q; want quiet", got)
	}
}
