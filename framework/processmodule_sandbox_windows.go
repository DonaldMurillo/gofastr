//go:build windows

package framework

import (
	"errors"
	"os/exec"
)

type windowsStubBackend struct{}

func defaultSandboxBackend() SandboxBackend {
	return &windowsStubBackend{}
}

func (b *windowsStubBackend) Name() string    { return "windows-stub" }
func (b *windowsStubBackend) Available() bool { return false }
func (b *windowsStubBackend) MissingReason() string {
	return "Windows sandbox backend not implemented in v1 (AppContainer/restricted-token + Job Object + WFP rules pending)"
}

func (b *windowsStubBackend) DeclaredProbes() []ProbeID { return nil }

func (b *windowsStubBackend) Wrap(cmd *exec.Cmd, _ SandboxOpts) error {
	return errors.New("windows sandbox backend is a v1 stub: refuses to wrap untrusted child (fail-closed)")
}
