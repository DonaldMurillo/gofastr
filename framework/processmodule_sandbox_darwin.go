//go:build darwin

package framework

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/DonaldMurillo/gofastr/internal/fileperm"
)

type darwinSandboxBackend struct {
	sandboxExecPath string
	available       bool
	missing         string
}

func defaultSandboxBackend() SandboxBackend {
	b := &darwinSandboxBackend{}
	if p, err := exec.LookPath("sandbox-exec"); err == nil {
		b.sandboxExecPath = p
		b.available = true
	} else {
		b.missing = "sandbox-exec not on PATH"
	}
	return b
}

func (b *darwinSandboxBackend) Name() string          { return "sandbox-exec" }
func (b *darwinSandboxBackend) Available() bool       { return b != nil && b.available }
func (b *darwinSandboxBackend) MissingReason() string { return b.missing }

func (b *darwinSandboxBackend) DeclaredProbes() []ProbeID {
	return []ProbeID{
		ProbeNoInheritedSecret,
		ProbeNoInheritedFD,
		ProbeNoNetworkEgress,
		ProbeFilesystemConfinement,
		ProbeNoPrivReEscalation,
	}
}

func (b *darwinSandboxBackend) Wrap(cmd *exec.Cmd, opts SandboxOpts) error {
	if b == nil || !b.available {
		return errors.New("sandbox-exec unavailable")
	}
	child := cmd.Path
	if child == "" {
		return errors.New("sandbox-exec: cmd.Path is empty")
	}

	profileDir := opts.ScratchDir
	if profileDir == "" {
		profileDir = "."
	}
	profilePath := filepath.Join(profileDir, "gofastr-sandbox.sb")
	profile := generateDarwinProfile(child, opts)
	if err := fileperm.WriteOwnerOnly(profilePath, []byte(profile)); err != nil {
		return fmt.Errorf("write profile: %w", err)
	}

	args := []string{
		b.sandboxExecPath,
		"-f", profilePath,
	}
	args = append(args, child)
	args = append(args, cmd.Args[1:]...)

	cmd.Path = b.sandboxExecPath
	cmd.Args = args
	return nil
}

func generateDarwinProfile(childPath string, opts SandboxOpts) string {
	var b strings.Builder
	childReal, _ := filepath.EvalSymlinks(childPath)
	if childReal == "" {
		childReal = childPath
	}
	pkgReal := opts.PackageDir
	if pkgReal != "" {
		if r, err := filepath.EvalSymlinks(pkgReal); err == nil {
			pkgReal = r
		}
	}
	scratchReal := opts.ScratchDir
	if scratchReal != "" {
		if r, err := filepath.EvalSymlinks(scratchReal); err == nil {
			scratchReal = r
		}
	}
	b.WriteString("(version 1)\n")
	b.WriteString("(deny default)\n")
	b.WriteString("(allow process*)\n")
	b.WriteString("(allow signal (target self))\n")
	b.WriteString("(allow process-exec\n")
	b.WriteString(fmt.Sprintf("  (subpath %q)\n", childReal))
	for _, dir := range []string{"/usr/bin", "/bin", "/usr/sbin", "/sbin"} {
		b.WriteString(fmt.Sprintf("  (subpath %q)\n", dir))
	}
	b.WriteString(")\n")
	b.WriteString("(allow file-read*\n")
	for _, dir := range []string{
		"/usr", "/bin", "/sbin", "/System", "/Library",
		"/dev", "/private/etc", "/etc",
		"/var/folders", "/private/var/folders",
		"/tmp", "/private/tmp",
		"/var/tmp", "/private/var/tmp",
	} {
		b.WriteString(fmt.Sprintf("  (subpath %q)\n", dir))
	}
	if pkgReal != "" {
		b.WriteString(fmt.Sprintf("  (subpath %q)\n", pkgReal))
	}
	b.WriteString(")\n")
	b.WriteString("(allow file-map-executable\n")
	b.WriteString(fmt.Sprintf("  (subpath %q)\n", childReal))
	for _, dir := range []string{"/usr", "/System", "/Library"} {
		b.WriteString(fmt.Sprintf("  (subpath %q)\n", dir))
	}
	if pkgReal != "" {
		b.WriteString(fmt.Sprintf("  (subpath %q)\n", pkgReal))
	}
	b.WriteString(")\n")
	b.WriteString("(allow file-read-metadata)\n")
	b.WriteString("(allow file-read-data (literal \"/\"))\n")
	b.WriteString("(allow sysctl-read)\n")
	if scratchReal != "" {
		b.WriteString(fmt.Sprintf("(allow file-write* (subpath %q))\n", scratchReal))
		b.WriteString(fmt.Sprintf("(allow file-read* (subpath %q))\n", scratchReal))
	}
	b.WriteString("(deny file-write*\n")
	for _, dir := range []string{"/etc", "/Users", "/root"} {
		b.WriteString(fmt.Sprintf("  (subpath %q)\n", dir))
	}
	b.WriteString(")\n")
	if home := opts.homeDir(); home != "" {
		b.WriteString(fmt.Sprintf("(deny file-read* (subpath %q))\n", home))
	}
	b.WriteString("(deny network*)\n")
	return b.String()
}

func (opts SandboxOpts) homeDir() string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return ""
}

func (b *darwinSandboxBackend) String() string {
	if b == nil {
		return "sandbox-exec(nil)"
	}
	return fmt.Sprintf("sandbox-exec(%s, available=%v)", b.sandboxExecPath, b.available)
}
