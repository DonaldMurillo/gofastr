package desktopcmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
)

// buildLinuxDesktop defaults to a plain ELF executable. The opt-in tar.gz
// format packages release artifacts, never runtime libraries or an installer.
func buildLinuxDesktop(f desktopBuildFlags, name string) error {
	arch := os.Getenv("GOARCH")
	if arch == "" {
		arch = runtime.GOARCH
	}
	if arch != "amd64" && arch != "arm64" {
		return fmt.Errorf("Linux desktop supports amd64 and arm64, not %q", arch)
	}
	if name == "." || name == ".." {
		return fmt.Errorf("--name must name an executable, not a directory")
	}
	if f.sign != "" || f.noSign || f.notarize || f.entitlements != "" {
		return fmt.Errorf("signing and notarization flags are macOS-only")
	}
	if f.scheme != "" {
		return fmt.Errorf("Linux desktop URL-scheme registration is not implemented")
	}
	if f.format == "tar.gz" {
		return buildLinuxDesktopArchive(f, name, arch)
	}
	if f.format != "" && f.format != "executable" {
		return fmt.Errorf("--format supports executable or tar.gz on Linux only")
	}
	if f.icon != "" {
		return fmt.Errorf("Linux desktop icon packaging is not implemented; the output is a plain executable")
	}
	//gofastr:allow(worldreadable) dist contains public release binaries, no application state or secrets
	if err := os.MkdirAll(f.out, 0755); err != nil {
		return err
	}
	exe := filepath.Join(f.out, name)
	cli.Info("Building %s (linux/%s, CGO_ENABLED=0)...", f.pkg, arch)
	cmd := exec.Command("go", "build", "-trimpath", "-ldflags", "-s -w", "-o", exe, f.pkg)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH="+arch)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("build: %w", err)
	}
	cli.Success("Wrote %s", exe)
	cli.Info("Runtime requires GTK 3 and WebKitGTK 4.1 in a graphical Linux session")
	return nil
}
