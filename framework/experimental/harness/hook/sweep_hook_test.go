package hook

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
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

func TestRunTimeoutKillsTermIgnoringDescendant(t *testing.T) {
	skipWindows(t)
	if _, err := os.Stat("/proc/self/stat"); err != nil {
		t.Skip("needs /proc to read process state")
	}
	pidfile := filepath.Join(t.TempDir(), "pid")
	r := New()
	_ = r.Register(Hook{
		Event:   EventPreToolUse,
		Command: `trap "" TERM; sleep 30 & echo $! > ` + pidfile + `; wait`,
		Timeout: 200 * time.Millisecond,
		Source:  "user",
	})
	res := r.Run(context.Background(), EventPreToolUse, nil)
	if !res[0].TimedOut {
		t.Fatalf("expected TimedOut: %+v", res[0])
	}
	raw, err := os.ReadFile(pidfile)
	if err != nil {
		t.Fatalf("read pidfile: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("parse pid %q: %v", raw, err)
	}
	t.Cleanup(func() {
		if procAlive(pid) {
			if p, err := os.FindProcess(pid); err == nil {
				_ = p.Kill()
			}
		}
	})
	deadline := time.Now().Add(2 * time.Second)
	for procAlive(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("descendant %d that ignores SIGTERM outlived the hook timeout", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func procAlive(pid int) bool {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false
	}
	s := string(b)
	i := strings.LastIndexByte(s, ')')
	if i < 0 || i+2 >= len(s) {
		return false
	}
	return s[i+2] != 'Z'
}
