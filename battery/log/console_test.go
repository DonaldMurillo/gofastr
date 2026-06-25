package log

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"golang.org/x/term"
)

// forceColor / noColor helpers build a ConsoleSink writing to a buffer
// with coloring explicitly pinned, so tests are independent of the TTY
// the test runner happens to be under.
func sinkFor(w *bytes.Buffer, color bool) *consoleSink {
	on := color
	return ConsoleSink(ConsoleOpts{Writer: w, Color: &on}).(*consoleSink)
}

func TestConsoleSinkColorizesByLevel(t *testing.T) {
	var buf bytes.Buffer
	s := sinkFor(&buf, true)
	entries := []struct {
		level string
		want  string // ANSI escape expected in output
	}{
		{`"DEBUG"`, "\x1b[90m"}, // gray
		{`"INFO"`, "\x1b[34m"},  // blue
		{`"WARN"`, "\x1b[33m"},  // yellow
		{`"ERROR"`, "\x1b[31m"}, // red
	}
	for _, e := range entries {
		buf.Reset()
		entry := []byte(`{"time":"2026-06-17T14:32:07.412Z","level":` + e.level + `,"msg":"x"}`)
		if err := s.Write(entry); err != nil {
			t.Fatalf("Write: %v", err)
		}
		if !strings.Contains(buf.String(), e.want) {
			t.Fatalf("level %s: expected ANSI %q in output, got %q", e.level, e.want, buf.String())
		}
	}
}

func TestConsoleSinkNoColorIsPlainText(t *testing.T) {
	var buf bytes.Buffer
	s := sinkFor(&buf, false)
	entry := []byte(`{"time":"2026-06-17T14:32:07.412Z","level":"INFO","msg":"app.start","app":"myapp"}`)
	if err := s.Write(entry); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if strings.Contains(got, "\x1b[") {
		t.Fatalf("no-color sink emitted ANSI codes: %q", got)
	}
	// Human-readable shape: timestamp, level, msg, key=value all present.
	for _, want := range []string{"14:32:07.412", "INFO", "app.start", "app=myapp"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in plain output: %q", want, got)
		}
	}
}

func TestConsoleSinkPreservesAttrOrder(t *testing.T) {
	var buf bytes.Buffer
	s := sinkFor(&buf, false)
	// Emit attrs in a deliberate non-alphabetical order; the renderer
	// must keep that order, not json's map-randomized one.
	entry := []byte(`{"time":"2026-06-17T14:32:07.412Z","level":"INFO","msg":"m","zebra":"1","alpha":"2","mango":"3"}`)
	if err := s.Write(entry); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	iz := strings.Index(got, "zebra")
	ia := strings.Index(got, "alpha")
	im := strings.Index(got, "mango")
	if iz < 0 || ia < 0 || im < 0 {
		t.Fatalf("missing attrs in %q", got)
	}
	if !(iz < ia && ia < im) {
		t.Fatalf("attr order not preserved, got %q (zebra=%d alpha=%d mango=%d)", got, iz, ia, im)
	}
}

func TestConsoleSinkQuotesValuesWithSpaces(t *testing.T) {
	var buf bytes.Buffer
	s := sinkFor(&buf, false)
	entry := []byte(`{"time":"2026-06-17T14:32:07.412Z","level":"ERROR","msg":"http.panic","stack":"goroutine 1 [running]:\nmain.x"}`)
	if err := s.Write(entry); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	// Multi-line / space-bearing value must be quoted and single-line.
	if !strings.Contains(got, `stack="goroutine 1 [running]:\nmain.x"`) {
		t.Fatalf("expected quoted single-line stack, got %q", got)
	}
	// Each entry is exactly one line.
	if strings.Count(got, "\n") != 1 {
		t.Fatalf("expected one newline (one line), got %d: %q", strings.Count(got, "\n"), got)
	}
}

func TestConsoleSinkBareSimpleValues(t *testing.T) {
	var buf bytes.Buffer
	s := sinkFor(&buf, false)
	// Numbers / bools / simple strings stay bare; no redundant quotes.
	entry := []byte(`{"time":"2026-06-17T14:32:07.412Z","level":"INFO","msg":"m","status":200,"ok":true,"name":"alice"}`)
	if err := s.Write(entry); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	for _, want := range []string{"status=200", "ok=true", "name=alice"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
}

func TestConsoleSinkMalformedJSONFallsBack(t *testing.T) {
	var buf bytes.Buffer
	s := sinkFor(&buf, false)
	raw := []byte("not json at all")
	if err := s.Write(raw); err != nil {
		t.Fatal(err)
	}
	// Fallback writes raw bytes + newline verbatim.
	if got := buf.String(); got != "not json at all\n" {
		t.Fatalf("fallback wrote %q, want raw + newline", got)
	}
}

func TestConsoleSinkWriteAfterClose(t *testing.T) {
	var buf bytes.Buffer
	s := sinkFor(&buf, false)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	err := s.Write([]byte(`{"level":"INFO","msg":"x"}`))
	if err != ErrSinkClosed {
		t.Fatalf("Write after Close: got %v, want ErrSinkClosed", err)
	}
}

func TestConsoleSinkOneLinePerEntry(t *testing.T) {
	var buf bytes.Buffer
	s := sinkFor(&buf, false)
	for i := 0; i < 3; i++ {
		if err := s.Write([]byte(`{"time":"2026-06-17T14:32:07.412Z","level":"INFO","msg":"m","i":0}`)); err != nil {
			t.Fatal(err)
		}
	}
	if n := strings.Count(buf.String(), "\n"); n != 3 {
		t.Fatalf("expected 3 newlines for 3 entries, got %d", n)
	}
}

func TestShouldColorRespectsNoColorEnv(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	if shouldColor(os.Stderr) {
		t.Fatal("shouldColor=true with NO_COLOR set; want false")
	}
	// Unset path: when NO_COLOR is unset, behavior depends on whether
	// stderr is a real TTY (it isn't under `go test`). Just assert it
	// doesn't panic and returns a bool.
	t.Setenv("NO_COLOR", "")
	if shouldColor(os.Stderr) {
		// Under `go test`, stderr is typically not a TTY, so this
		// should be false. If it is a TTY (rare interactive run), the
		// assertion is skipped rather than failed.
		if termIsTTY() {
			t.Skip("stderr is a TTY in this environment; skipping non-TTY assertion")
		}
		t.Fatal("shouldColor=true under non-TTY go test; want false")
	}
}

// termIsTTY is a tiny helper for test guard logic only.
func termIsTTY() bool {
	return term.IsTerminal(int(os.Stderr.Fd()))
}
