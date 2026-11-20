package widget

type cleanupT interface{ Cleanup(func()) }

// IsolateForTest swaps in an empty widget registry for the test's lifetime and
// restores the process-global one at cleanup, so a test that mounts widgets
// sees only its own and leaves none behind for the next test in the binary.
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
