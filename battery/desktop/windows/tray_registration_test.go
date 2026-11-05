package windows

import (
	"errors"
	"testing"
)

func TestTrayRegistrationTracksAdd(t *testing.T) {
	var state trayRegistration
	for _, result := range []bool{false, true, false, true} {
		calls := 0
		state.add = func() bool {
			calls++
			return result
		}
		state.register()
		if calls != 1 || state.live != result {
			t.Fatalf("add result %v: live=%v, calls=%d", result, state.live, calls)
		}
	}
}

func TestTrayLifecycleDecisions(t *testing.T) {
	for _, test := range []struct {
		name       string
		results    []bool
		hidden     bool
		closeHides bool
		wantHide   bool
		wantShow   int
	}{
		{"initial failure", []bool{false}, false, true, false, 0},
		{"initial success", []bool{true}, false, true, true, 0},
		{"recover initial failure", []bool{false, true}, false, true, true, 0},
		{"recover hidden icon", []bool{true, true}, true, true, true, 0},
		{"failed hidden recovery", []bool{true, false}, true, true, false, 1},
		{"failed visible recovery", []bool{true, false}, false, true, false, 0},
		{"nonhiding tray", []bool{true, false}, true, false, false, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			adds, shows := 0, 0
			state := trayRegistration{
				closeHides: test.closeHides, taskbarCreated: 0x9000,
				add: func() bool {
					result := test.results[adds]
					adds++
					return result
				},
				visible: func() bool { return !test.hidden },
				show:    func() { shows++ },
			}
			state.register()
			if len(test.results) > 1 && !state.handleMessage(0x9000, true) {
				t.Fatal("TaskbarCreated was not handled")
			}
			if adds != len(test.results) || shows != test.wantShow {
				t.Fatalf("native calls: add=%d show=%d, want add=%d show=%d", adds, shows, len(test.results), test.wantShow)
			}
			if got := state.hideOnClose(true, nil); got != test.wantHide {
				t.Fatalf("hide on main close = %v, want %v", got, test.wantHide)
			}
			if state.hideOnClose(false, nil) || state.hideOnClose(true, errors.New("shutdown")) {
				t.Fatal("secondary window or failed run may not hide on close")
			}
		})
	}
}

func TestTrayIgnoresOtherMessages(t *testing.T) {
	for _, test := range []struct {
		name       string
		registered uint32
		message    uint32
		mainWindow bool
	}{
		{"another message", 0x9000, 0x9001, true},
		{"secondary window", 0x9000, 0x9000, false},
		{"zero message", 0, 0, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := trayRegistration{taskbarCreated: test.registered}
			if state.handleMessage(test.message, test.mainWindow) {
				t.Fatal("unrelated message triggered tray recovery")
			}
		})
	}
}
