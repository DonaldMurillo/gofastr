package hook

import (
	"context"
	"runtime"
	"strings"
	"testing"
)

func skipWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hook tests require /bin/sh")
	}
}

func TestRunOK(t *testing.T) {
	skipWindows(t)
	r := New()
	_ = r.Register(Hook{Event: EventSessionStart, Command: "echo hi", Source: "user"})
	res := r.Run(context.Background(), EventSessionStart, nil)
	if len(res) != 1 {
		t.Fatalf("got %d results", len(res))
	}
	if res[0].ExitCode != 0 || res[0].TimedOut {
		t.Errorf("res = %+v", res[0])
	}
	if !strings.Contains(res[0].Output, "hi") {
		t.Errorf("output = %q", res[0].Output)
	}
}

func TestRunNonZeroExit(t *testing.T) {
	skipWindows(t)
	r := New()
	_ = r.Register(Hook{Event: EventPostToolUse, Command: "exit 3", Source: "user"})
	res := r.Run(context.Background(), EventPostToolUse, nil)
	if res[0].ExitCode != 3 {
		t.Errorf("exit code = %d, want 3", res[0].ExitCode)
	}
}

func TestRunTimeout(t *testing.T) {
	skipWindows(t)
	r := New()
	_ = r.Register(Hook{Event: EventSessionStart, Command: "sleep 5", Timeout: 0, Source: "user"})
	// Override Timeout to a small value via re-registration; but
	// DefaultTimeout for SessionStart is 5s, which still kills sleep 5.
	// Easier: register a custom timeout.
	r2 := New()
	_ = r2.Register(Hook{Event: EventSessionStart, Command: "sleep 5", Timeout: 100_000_000, Source: "user"}) // 100ms
	res := r2.Run(context.Background(), EventSessionStart, nil)
	if !res[0].TimedOut {
		t.Errorf("expected TimedOut: %+v", res[0])
	}
}

func TestProjectHookSkippedByDefault(t *testing.T) {
	r := New()
	if err := r.Register(Hook{Event: EventSessionStart, Command: "echo bad", Source: "project"}); err != nil {
		t.Fatal(err)
	}
	if got := r.HooksFor(EventSessionStart); len(got) != 0 {
		t.Errorf("project hook not skipped: %v", got)
	}
}

func TestProjectHookAllowedWithFlag(t *testing.T) {
	r := New()
	r.AllowProjectHooks = true
	if err := r.Register(Hook{Event: EventSessionStart, Command: "echo ok", Source: "project"}); err != nil {
		t.Fatal(err)
	}
	if got := r.HooksFor(EventSessionStart); len(got) != 1 {
		t.Errorf("got %v", got)
	}
}

func TestDefaultTimeouts(t *testing.T) {
	cases := map[Event]int{
		EventSessionStart:     5,
		EventUserPromptSubmit: 5,
		EventStop:             5,
		EventPreToolUse:       30,
		EventPostToolUse:      30,
		EventCompact:          60,
	}
	for e, secs := range cases {
		got := DefaultTimeout(e)
		if int(got.Seconds()) != secs {
			t.Errorf("DefaultTimeout(%q) = %ds, want %ds", e, int(got.Seconds()), secs)
		}
	}
}
