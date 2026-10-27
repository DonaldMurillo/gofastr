package desktopcmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// The tiny fixture owns the real linker-symbol path. Executing both outputs
// proves the actual build commands embed the value, not only a helper's string.
func TestLinuxBuildEmbedsScheme(t *testing.T) {
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		t.Skip("Linux desktop supports amd64 and arm64")
	}
	fixture := t.TempDir()
	clitest.WriteFileAll(t, filepath.Join(fixture, "go.mod"), "module github.com/DonaldMurillo/gofastr\n\ngo 1.21\n")
	clitest.WriteFileAll(t, filepath.Join(fixture, "battery", "desktop", "scheme.go"), "package desktop\nvar builtDeepLinkScheme = \"unembedded\"\nfunc Scheme() string { return builtDeepLinkScheme }\n")
	clitest.WriteFileAll(t, filepath.Join(fixture, "main.go"), "package main\nimport (\"fmt\"; \"github.com/DonaldMurillo/gofastr/battery/desktop\")\nfunc main() { fmt.Println(desktop.Scheme()) }\n")
	t.Chdir(fixture)
	t.Setenv("GOARCH", runtime.GOARCH)
	for _, format := range []string{"executable", "tar.gz"} {
		for _, scheme := range []string{"", "gofastr-test+v1"} {
			t.Run(format+"/"+scheme, func(t *testing.T) {
				out := filepath.Join(t.TempDir(), "dist")
				f := desktopBuildFlags{id: "dev.example.scheme", version: "1.0.0", pkg: ".", out: out, format: format, scheme: scheme}
				if err := buildLinuxDesktop(f, "Scheme Fixture"); err != nil {
					t.Fatal(err)
				}
				executable := filepath.Join(out, "Scheme Fixture")
				if format == "tar.gz" {
					root := f.id + "-" + f.version + "-linux-" + runtime.GOARCH
					data, err := os.ReadFile(filepath.Join(out, root+".tar.gz"))
					if err != nil {
						t.Fatal(err)
					}
					_, contents := decodeLinuxArchive(t, data)
					executable = filepath.Join(t.TempDir(), "archived-app")
					if err := os.WriteFile(executable, contents[root+"/bin/"+f.id], 0700); err != nil {
						t.Fatal(err)
					}
				}
				output, err := exec.Command(executable).Output()
				if err != nil {
					t.Fatal(err)
				}
				want := scheme
				if want == "" {
					want = "unembedded"
				}
				if got := strings.TrimSpace(string(output)); got != want {
					t.Fatalf("compiled scheme = %q, want %q", got, want)
				}
			})
		}
	}
}
