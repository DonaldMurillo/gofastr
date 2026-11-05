package windows

import "testing"

func TestTrayRegistrationTracksAdd(t *testing.T) {
	var state trayRegistration
	for _, result := range []bool{false, true, false, true} {
		calls := 0
		state.register(func() bool {
			calls++
			return result
		})
		if calls != 1 || state.live != result {
			t.Fatalf("add result %v: live=%v, calls=%d", result, state.live, calls)
		}
	}
}
