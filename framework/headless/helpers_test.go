package headless

import "testing"

// mustRefuse asserts that fn panics: a component asked to render
// something it must not render says so at render time, where the
// mistake is a message with a reason, rather than in the browser.
func mustRefuse(t *testing.T, why string, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Errorf("%s rendered anyway", why)
		}
	}()
	fn()
}
