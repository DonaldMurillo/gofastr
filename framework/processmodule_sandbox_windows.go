//go:build windows

package framework

import (
	"errors"
	"os/exec"
)

// This file is the Windows arm of the §6 sandbox backend. v1 ships a
// documented STUB: it does not confine the child, and DeclaredProbes is
// empty. HostSandboxBackend filters it to nil (Available()=false), so
// NewSandboxRunner errors and the supervisor fail-closes every
// TrustUntrusted module on Windows — never a silent downgrade to
// TrustedProcessRunner.
//
// This is the honest v1 stance per design §6 + §11 risk 1: a real Windows
// backend needs a restricted/AppContainer token (P1/P5/P7) + a Job Object
// with per-limit fields (P6) + per-SID WFP firewall rules (P4). Each is
// substantial Win32 work (CreateRestrictedToken / JobObjectAssociate /
// FwpmFilterAdd0); none is reachable from Go's syscall.SysProcAttr alone.
// The stub's job is to make "Windows does not run untrusted modules in
// v1" an EXPLICIT, fail-closed answer rather than a silent unconfined
// spawn.
//
// The probe bodies (processmodule_probe_windows.go) mirror the portable
// probes so that WHEN a real backend lands, the same observable-outcome
// contract exercises it without re-authoring the suite.

// windowsStubBackend is the documented-stub Windows backend. Available
// is always false; every probe is unreachable.
type windowsStubBackend struct{}

// defaultSandboxBackend returns the Windows stub. HostSandboxBackend
// filters it to nil via Available()=false.
func defaultSandboxBackend() SandboxBackend {
	return &windowsStubBackend{}
}

func (b *windowsStubBackend) Name() string    { return "windows-stub" }
func (b *windowsStubBackend) Available() bool { return false }
func (b *windowsStubBackend) MissingReason() string {
	return "Windows sandbox backend not implemented in v1 (AppContainer/restricted-token + Job Object + WFP rules pending)"
}

// DeclaredProbes is empty: the stub enforces nothing. The conformance
// suite records every probe as UNREACHABLE, the backend does not conform,
// and untrusted fails closed.
func (b *windowsStubBackend) DeclaredProbes() []ProbeID { return nil }

// Wrap always errors — the stub refuses to spawn an untrusted child
// unconfined. This is the fail-closed seam.
func (b *windowsStubBackend) Wrap(cmd *exec.Cmd, _ SandboxOpts) error {
	return errors.New("windows sandbox backend is a v1 stub: refuses to wrap untrusted child (fail-closed)")
}
