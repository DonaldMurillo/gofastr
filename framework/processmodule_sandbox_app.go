package framework

import (
	"sync"
)

type processModuleSandbox struct {
	backend    SandboxBackend
	backendSet bool
	once       sync.Once
	runner     *SandboxRunner
	err        error
}

// WithSandboxBackend sets the backend untrusted process modules run under, in place
// of the host default (bwrap on Linux, sandbox-exec on macOS). It must pass the
// conformance probes; nil makes every untrusted registration fail.
func WithSandboxBackend(b SandboxBackend) AppOption {
	return func(a *App) {
		a.sandbox.backend = b
		a.sandbox.backendSet = true
	}
}

// ProcessModuleSandbox probes the sandbox backend on its first call and returns
// the runner, or a [SandboxConformanceError] when untrusted modules cannot run on
// this host. Later calls return the kept result.
func (a *App) ProcessModuleSandbox() (*SandboxRunner, error) {
	s := &a.sandbox
	s.once.Do(func() {
		b := s.backend
		if !s.backendSet {
			b = defaultSandboxBackend()
		}
		s.runner, s.err = NewSandboxRunner(b, SandboxOpts{})
	})
	return s.runner, s.err
}
