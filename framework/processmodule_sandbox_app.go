package framework

import (
	"sync"
)

// App-level wiring for the untrusted process-module tier (issue #608).
// Before it, NewSandboxRunner had no production call site, so every
// TrustUntrusted descriptor failed Register on every host. The app now
// builds the runner itself, once, the first time an untrusted module
// registers (or the host asks first through ProcessModuleSandbox).

// processModuleSandbox is the App's untrusted-tier runner state.
type processModuleSandbox struct {
	// backend is what WithSandboxBackend set. backendSet distinguishes
	// "use the host default" from an explicit nil, which is the
	// fail-closed "this app hosts no untrusted modules" choice.
	backend    SandboxBackend
	backendSet bool

	once   sync.Once
	runner *SandboxRunner
	err    error
}

// WithSandboxBackend sets the backend untrusted process modules run under
// (TrustUntrusted descriptors passed to RegisterProcessModule). Without
// it the app uses this host's built-in backend: bwrap on Linux,
// sandbox-exec on macOS. Pass a custom [SandboxBackend] to confine
// modules some other way; it has to pass the same P1–P7 conformance
// probe. Passing nil makes every untrusted registration fail closed.
func WithSandboxBackend(b SandboxBackend) AppOption {
	return func(a *App) {
		a.sandbox.backend = b
		a.sandbox.backendSet = true
	}
}

// ProcessModuleSandbox returns the runner untrusted process modules run
// under, building it on the first call: it probes the backend
// ([WithSandboxBackend], else the host default) with the P1–P7
// conformance suite and keeps the result for the life of the App. The
// error wraps [ErrSandboxUnavailable] and carries the conformance report
// when the backend is missing or does not conform; untrusted modules
// cannot run on this host then.
//
// RegisterProcessModule calls it for a TrustUntrusted descriptor and
// panics with that error, the same way it panics on any other refused
// registration. A host that should still boot on a machine without a
// conforming sandbox calls it first and registers the untrusted module
// only when it returns nil.
//
// The probe re-runs this binary as its probe child; the framework's
// package init answers it, so the host's main needs no hook. The first
// call takes as long as seven short-lived child processes, about a
// second on Linux; later calls return the kept result.
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
