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
	for range 3 {
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

// TestEscapersPinPreMergeOutputs pins escapeUnsafe and quoteTerminalSafe
// to the byte outputs of the two loop bodies they were merged from,
// captured at HEAD before the merge. Inputs and wants are built from
// \xNN escapes and rune slices so every control byte is visible.
func TestEscapersPinPreMergeOutputs(t *testing.T) {
	var (
		bs    = "\x5c" // one backslash
		dq    = "\x22" // one double quote
		fffd  = string([]rune{0xfffd})
		c1    = string([]rune{0x80, 0x85, 0x9b, 0x9f})
		bidi  = string([]rune{0x200e, 0x202e, 0x2066, 0x2069, 0x2060, 0xfeff})
		flag  = "\U0001f3f4\U000e0067\U000e0062\U000e0077\U000e006c\U000e0073\U000e007f"
		mixed = "q=" + dq + "x" + dq + bs + "n" + bs + bs + " " + string([]rune{0x202e}) + " \x9b"
	)
	u := func(hex string) string { return bs + "u" + hex }
	escapedFlag := "\U0001f3f4" + u("e0067") + u("e0062") + u("e0077") + u("e006c") + u("e0073") + u("e007f")
	cases := []struct {
		name       string
		in         string
		wantPlain  string
		wantQuoted string
	}{
		{"plain", "hello", "hello", dq + "hello" + dq},
		{"spaces", "two words", "two words", dq + "two words" + dq},
		{"equals", "key=value", "key=value", dq + "key=value" + dq},
		{"quotes", "say " + dq + "hi" + dq, "say " + dq + "hi" + dq,
			dq + "say " + bs + dq + "hi" + bs + dq + dq},
		{"backslash", "a" + bs + "b", "a" + bs + "b", dq + "a" + bs + bs + "b" + dq},
		{"short escapes", "a b\x0ac\x0dd\x09e", "a b" + bs + "nc" + bs + "rd" + bs + "te",
			dq + "a b" + bs + "nc" + bs + "rd" + bs + "te" + dq},
		{"c0", "\x00\x01\x02\x1b\x1f", u("0000") + u("0001") + u("0002") + u("001b") + u("001f"),
			dq + u("0000") + u("0001") + u("0002") + u("001b") + u("001f") + dq},
		{"del", "\x7f", u("007f"), dq + u("007f") + dq},
		{"c1 controls", c1, u("0080") + u("0085") + u("009b") + u("009f"),
			dq + u("0080") + u("0085") + u("009b") + u("009f") + dq},
		{"bidi and zero width", bidi,
			u("200e") + u("202e") + u("2066") + u("2069") + u("2060") + u("feff"),
			dq + u("200e") + u("202e") + u("2066") + u("2069") + u("2060") + u("feff") + dq},
		{"invalid utf8", "bad\xff\xfe bytes\x80", "bad\xff\xfe bytes\x80",
			dq + "bad" + fffd + fffd + " bytes" + fffd + dq},
		{"subdivision flag", flag, flag, dq + escapedFlag + dq},
		{"subdivision flag and tab", flag + "\t", escapedFlag + bs + "t", dq + escapedFlag + bs + "t" + dq},
		{"mixed quotes backslash bidi c1", mixed,
			"q=" + dq + "x" + dq + bs + "n" + bs + bs + " " + u("202e") + " " + fffd,
			dq + "q=" + bs + dq + "x" + bs + dq + bs + bs + "n" + bs + bs + bs + bs + " " + u("202e") + " " + fffd + dq},
	}
	for _, tc := range cases {
		if got := escapeUnsafe(tc.in); got != tc.wantPlain {
			t.Errorf("%s: escapeUnsafe = %q, want %q", tc.name, got, tc.wantPlain)
		}
		if got := quoteTerminalSafe(tc.in); got != tc.wantQuoted {
			t.Errorf("%s: quoteTerminalSafe = %q, want %q", tc.name, got, tc.wantQuoted)
		}
	}
}
