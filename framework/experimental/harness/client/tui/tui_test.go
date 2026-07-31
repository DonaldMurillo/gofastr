package tui

import "testing"

func TestTruncate(t *testing.T) {
	cases := []struct {
		in, want string
		max      int
	}{
		{"hello", "hello", 10},
		{"hello world", "hello…", 6},
		{"x", "x", 1}, // exact fit
		{"", "", 0},
	}
	for _, c := range cases {
		got := truncate(c.in, c.max)
		if got != c.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", c.in, c.max, got, c.want)
		}
	}
}

func TestCtrlCWindow(t *testing.T) {
	defer func(old func() int64) { nowFn = old }(nowFn)
	var clock int64
	nowFn = func() int64 { return clock }

	tui := &TUI{}
	tui.lastCtrlC = 0
	clock = 1_000_000_000
	if exit := tui.handleCtrlC(); exit {
		t.Fatal("first Ctrl-C shouldn't exit")
	}
	// Within 2 seconds — should exit.
	clock = 2_500_000_000
	if exit := tui.handleCtrlC(); !exit {
		t.Fatal("second Ctrl-C within 2s should exit")
	}

	// Far apart — first counts as fresh.
	tui.lastCtrlC = 0
	clock = 10_000_000_000
	if exit := tui.handleCtrlC(); exit {
		t.Fatal("fresh Ctrl-C shouldn't exit")
	}
	clock = 13_000_000_000 // 3s later
	if exit := tui.handleCtrlC(); exit {
		t.Fatal("Ctrl-C >2s apart shouldn't exit")
	}
}
