package hook

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func TestRunTimeoutReturnsPromptly(t *testing.T) {
	skipWindows(t)
	r := New()
	_ = r.Register(Hook{Event: EventPreToolUse, Command: "sleep 30", Timeout: 100 * time.Millisecond, Source: "user"})
	start := time.Now()
	res := r.Run(context.Background(), EventPreToolUse, nil)
	if !res[0].TimedOut {
		t.Fatalf("expected TimedOut: %+v", res[0])
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("a 100ms hook timeout held Run for %s", d)
	}
}

func TestRunOutputCapped(t *testing.T) {
	skipWindows(t)
	r := New()
	_ = r.Register(Hook{Event: EventPreToolUse, Command: "head -c 200000 /dev/zero | tr '\\0' x", Source: "user"})
	res := r.Run(context.Background(), EventPreToolUse, nil)
	if n := strings.Count(res[0].Output, "x"); n != 64*1024 {
		t.Fatalf("kept %d output bytes, want 65536", n)
	}
	if strings.Count(res[0].Output, "[hook output truncated]") != 1 {
		t.Fatalf("want exactly one truncation marker")
	}
}

func TestCappedWriterMarksWriteAfterExactFill(t *testing.T) {
	var buf bytes.Buffer
	w := capped(&buf, 4)
	_, _ = w.Write([]byte("abcd"))
	_, _ = w.Write([]byte("efgh"))
	_, _ = w.Write([]byte("ijkl"))
	if got := buf.String(); got != "abcd\n[hook output truncated]\n" {
		t.Fatalf("output = %q", got)
	}
}
