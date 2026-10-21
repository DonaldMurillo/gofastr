package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// buildLinuxDesktop emits a plain ELF executable. Runtime shared libraries
// belong to the distribution; this command does not bundle or install GTK.
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
	if f.icon != "" {
		return fmt.Errorf("Linux desktop icon packaging is not implemented; the output is a plain executable")
	}
	//gofastr:allow(worldreadable) dist contains public release binaries, no application state or secrets
	if err := os.MkdirAll(f.out, 0755); err != nil {
		return err
	}
	exe := filepath.Join(f.out, name)
	info("Building %s (linux/%s, CGO_ENABLED=0)...", f.pkg, arch)
	cmd := exec.Command("go", "build", "-trimpath", "-ldflags", "-s -w", "-o", exe, f.pkg)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH="+arch)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("build: %w", err)
	}
	success("Wrote %s", exe)
	info("Runtime requires GTK 3 and WebKitGTK 4.1 in a graphical Linux session")
	return nil
}
