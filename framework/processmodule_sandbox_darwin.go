//go:build darwin

package framework

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// This file is the macOS arm of the §6 sandbox backend: `sandbox-exec`
// with a generated `.sb` profile ((deny default) + narrow read/write/
// network allowances).
//
// Honest limits (design §6 macOS bullet + §11 risk 1):
//   - sandbox-exec does NOT change uid — App Sandbox needs a signed .app
//     (inapplicable to a spawned CLI), and distinct-uid needs a service
//     account. The P1 probe therefore reports BREACH on macOS (the child
//     uid equals the host uid). "macOS may be trusted-tier-only for
//     untrusted modules until a signed helper exists" — and the probe
//     FAILING P1 is the honest enforcement, not a hand-wave.
//   - sandbox-exec is Apple-deprecated but remains present on every
//     shipping macOS (verified: /usr/bin/sandbox-exec on this host).
//
// Net effect on a stock darwin host: the backend IS available and probes
// P2–P5, P7 run; P1 reports BREACH (no uid isolation); P6 reports BREACH
// (no cgroup/Job-Object limits). The backend therefore does NOT conform
// and untrusted modules fail-closed on macOS — which is the design's
// documented v1 stance.

// darwinSandboxBackend is the macOS SandboxBackend: sandbox-exec + a
// generated profile. The profile is written into the per-spawn scratch
// dir so concurrent children do not collide on a single profile file.
type darwinSandboxBackend struct {
	sandboxExecPath string
	available       bool
	missing         string
}

// defaultSandboxBackend returns the macOS sandbox-exec backend, or an
// unavailable instance if sandbox-exec is not on PATH.
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

// DeclaredProbes is the honest ceiling for sandbox-exec on macOS. P1
// (uid) is NOT declared — sandbox-exec cannot mint a distinct principal
// without a signed helper — and the conformance suite records P1 as
// BREACH. P6 (resource limits) is NOT declared — macOS has no cgroup
// equivalent reachable from sandbox-exec; Job Object limits are Windows-
// only. Both omissions mean the backend cannot conform for untrusted on
// a stock macOS host.
func (b *darwinSandboxBackend) DeclaredProbes() []ProbeID {
	return []ProbeID{
		// P1 distinct principal: NOT declared (uid not isolated).
		ProbeNoInheritedSecret,
		ProbeNoInheritedFD,
		ProbeNoNetworkEgress,
		ProbeFilesystemConfinement,
		// P6 resource limits: NOT declared (no cgroup).
		ProbeNoPrivReEscalation,
	}
}

// Wrap rewrites cmd to run the child under sandbox-exec -p <profile>.
// The profile is generated into opts.ScratchDir/profile.sb so each spawn
// gets its own (the paths are spawn-specific). Stdio assignment is
// inherited by sandbox-exec and passed through to the child.
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
	if err := os.WriteFile(profilePath, []byte(profile), 0o600); err != nil {
		return fmt.Errorf("write profile: %w", err)
	}

	// sandbox-exec -f <profile> <child> <args...>. The profile's
	// process-exec rule gates whether the child may run; (deny default)
	// makes everything else denied unless explicitly allowed.
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

// generateDarwinProfile builds the sandbox-exec profile. The shape:
//
//	(version 1)
//	(deny default)                              ; deny everything not allowed
//	(allow process*)                            ; let the child run / fork
//	(allow process-exec (subpath "<child>"))    ; the binary itself
//	(allow file-read* (subpath "/usr" "/bin" "/System" "/Library" ...))
//	(allow file-write* (subpath "<scratch>"))   ; scratch writable
//	(deny file-write* (subpath "/etc" "/var" "/Users"))  ; host tree ro
//	(deny network*)                             ; P4
//
// P1 cannot be enforced (uid unchanged) — that is the honest gap the
// probe reports. P5 is partial: the profile denies writes outside scratch
// and denies reads of $HOME, but /etc/passwd remains readable (the Go
// runtime needs name-service lookup). The probe treats any readable host
// file as BREACH, so P5 will likely report BREACH on macOS too.
func generateDarwinProfile(childPath string, opts SandboxOpts) string {
	var b strings.Builder
	// sandbox-exec resolves symlinks before matching subpaths, so the
	// profile must use the REAL paths: /var/folders → /private/var/folders,
	// /tmp → /private/tmp, /etc → /private/etc. If we pass the symlink,
	// the rule silently fails to match and the child cannot even start.
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
	// Allow the process to run / fork / signal itself.
	b.WriteString("(allow process*)\n")
	b.WriteString("(allow signal (target self))\n")
	// Allow exec of the child binary + system tool dirs. The child's
	// real path is used (EvalSymlinks above) so the subpath matches.
	b.WriteString("(allow process-exec\n")
	b.WriteString(fmt.Sprintf("  (subpath %q)\n", childReal))
	for _, dir := range []string{"/usr/bin", "/bin", "/usr/sbin", "/sbin"} {
		b.WriteString(fmt.Sprintf("  (subpath %q)\n", dir))
	}
	b.WriteString(")\n")
	// Read-only system tree (dylibs, locale, the runtime's own needs).
	// Includes macOS's per-user temp dirs ($TMPDIR resolves under
	// /var/folders → /private/var/folders) so a child whose binary lives
	// in the test's go-build temp dir can still execvp.
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
	// Scratch writable (the child's own work).
	if scratchReal != "" {
		b.WriteString(fmt.Sprintf("(allow file-write* (subpath %q))\n", scratchReal))
		b.WriteString(fmt.Sprintf("(allow file-read* (subpath %q))\n", scratchReal))
	}
	// Explicit write denials on host trees. /var and /private/var are
	// intentionally NOT in this list: macOS's per-user temp dirs live
	// there, and the default `(deny default)` already blocks every
	// write outside the scratch allow above. Listing /var here would
	// re-deny writes the test binary needs to start.
	b.WriteString("(deny file-write*\n")
	for _, dir := range []string{"/etc", "/Users", "/root"} {
		b.WriteString(fmt.Sprintf("  (subpath %q)\n", dir))
	}
	b.WriteString(")\n")
	// Deny $HOME reads (the actual secret surface — ~/.ssh, ~/.aws, etc.).
	if home := opts.homeDir(); home != "" {
		b.WriteString(fmt.Sprintf("(deny file-read* (subpath %q))\n", home))
	}
	// P4: no network egress. Every connect() from the child fails.
	b.WriteString("(deny network*)\n")
	// Note: macOS has no single sandbox-exec directive for "no-new-privs"
	// (process-setuid is NOT a valid sandbox-exec keyword on current
	// macOS — it errors "unbound variable"). The setuid-up syscall
	// failure the P7 probe body observes (syscall.Setuid(0) returns
	// EPERM on a non-root child) is the enforcement signal — no profile
	// line needed.
	return b.String()
}

// homeDir returns the operator's home dir for the P5 $HOME-read denial.
func (opts SandboxOpts) homeDir() string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return ""
}

// String renders the backend for logs.
func (b *darwinSandboxBackend) String() string {
	if b == nil {
		return "sandbox-exec(nil)"
	}
	return fmt.Sprintf("sandbox-exec(%s, available=%v)", b.sandboxExecPath, b.available)
}
