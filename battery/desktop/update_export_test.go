package desktop

import "context"

// The export_test.go pattern: production identifiers the external test
// package needs, compiled into the test binary only. Nothing here is
// API.
//
// The seam deliberately speaks in builtin types: an alias of
// internal/update.Platform here would force package desktop_test to
// load an internal package it may not import (the internal rule is
// path-based, and desktop_test is not under battery/desktop/), which
// the compiler reports as a broken import.

// TestPlatform is the updater's OS seam as tests implement it: a
// running version, a bundle location, and the two apply steps.
type TestPlatform interface {
	// Version is the running app's version; "" never updates.
	Version() string
	// Bundle returns the running bundle's file name and its parent
	// directory; ok false when unbundled.
	Bundle() (name, dir string, ok bool)
	// VerifySignature is the codesign check on the extracted bundle.
	VerifySignature(appDir string) error
	// Relaunch opens the installed bundle.
	Relaunch(appDir string) error
}

// platformAdapter adapts TestPlatform onto internal/update.Platform.
type platformAdapter struct{ inner TestPlatform }

func (a platformAdapter) Version() string { return a.inner.Version() }

func (a platformAdapter) Bundle() (string, string, bool) { return a.inner.Bundle() }

func (a platformAdapter) VerifySignature(appDir string) error {
	return a.inner.VerifySignature(appDir)
}

func (a platformAdapter) Relaunch(appDir string) error { return a.inner.Relaunch(appDir) }

// SetUpdatePlatform installs p as this battery's update platform: the
// seam tests use to observe the apply step without native code. p is
// nil to restore the OS default, or any value implementing
// TestPlatform (it panics otherwise). Set it before Run or before any
// check; the updater reads it per call.
func (b *Battery) SetUpdatePlatform(p any) {
	u := b.updaterFor()
	if p == nil {
		u.platform = nil
		return
	}
	u.platform = platformAdapter{inner: p.(TestPlatform)}
}

// ResetGrantsForTest clears every persisted permission decision.
func (b *Battery) ResetGrantsForTest(ctx context.Context) error {
	return b.grants.Reset(ctx)
}
