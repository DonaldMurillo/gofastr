package widget

// cleanupT is the subset of *testing.T that IsolateForTest needs,
// declared structurally so this production package never imports
// testing (the same shape as registry.IsolateForTest).
type cleanupT interface{ Cleanup(func()) }

// IsolateForTest swaps in an empty widget registry for the test's
// lifetime and restores the process-global one at cleanup, so a test
// that mounts widgets sees only its own and leaves none behind for the
// next test in the binary. The routes a mount registered stay on the
// router the test built; only the registry is isolated.
func IsolateForTest(t cleanupT) {
	registryMu.Lock()
	saved := registry
	registry = map[string]*Definition{}
	registryMu.Unlock()
	t.Cleanup(func() {
		registryMu.Lock()
		registry = saved
		registryMu.Unlock()
	})
}
