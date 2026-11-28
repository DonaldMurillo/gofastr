package telemetrycmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
	itel "github.com/DonaldMurillo/gofastr/internal/telemetry"
	"github.com/DonaldMurillo/gofastr/internal/telemetry/telemetrytest"
)

// --json prints a stored line, and a terminal acts on what it is shown:
// a line that is not JSON comes out as a JSON string, and the
// characters JSON leaves bare and a terminal obeys come out as escapes.
// What a program reads from the output is unchanged.
func TestJSONLineIsSafeToPrint(t *testing.T) {
	plain := `{"time":"2026-11-14T00:00:00Z","msg":"café 日本 ok"}`
	if got := string(jsonLine([]byte(plain))); got != plain {
		t.Errorf("an ordinary line was rewritten:\n%s", got)
	}

	risky := "{\"msg\":\"a\u009b2Jb\u202ec\u2066d\u200fe\"}"
	got := string(jsonLine([]byte(risky)))
	for _, r := range got {
		if (r >= 0x80 && r <= 0x9f) || (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) || r == 0x200f {
			t.Fatalf("U+%04X reached the output: %q", r, got)
		}
	}
	var before, after map[string]string
	if err := json.Unmarshal([]byte(risky), &before); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(got), &after); err != nil || after["msg"] != before["msg"] {
		t.Errorf("the escaped line reads as %q (err %v), want the same text %q", after["msg"], err, before["msg"])
	}

	// Torn by a crash, with an escape sequence in it.
	torn := "{\"time\":\"2026-11-14T00:00:00Z\",\"level\":\"INFO\" torn \x1b[2J\x1b]0;owned\a\xff}"
	got = string(jsonLine([]byte(torn)))
	if strings.ContainsAny(got, "\x1b\a") || !utf8.ValidString(got) {
		t.Fatalf("a control byte or a byte that is not UTF-8 reached the output: %q", got)
	}
	var text string
	if err := json.Unmarshal([]byte(got), &text); err != nil || !strings.Contains(text, "torn \x1b[2J") {
		t.Errorf("a line that is not JSON should print as a JSON string of itself: %q (err %v)", got, err)
	}
}

// Every --json command escapes each rune textsafe.IsUnsafe names, not only C1
// and bidi controls: zero-width, tag and line-separator runes hide or move text.
func TestEveryJSONCommandEscapesUnsafeRunes(t *testing.T) {
	dir := t.TempDir()
	at := now.Add(-time.Minute)
	id := strings.Repeat("e", 32)
	route := "GET /a\u009b\u2028\u200b\U000E0041"
	trace := itel.AppendTrace(nil, itel.Trace{Time: at, TraceID: id, Route: route, Path: "/a\u202e", Status: 500, Dur: time.Millisecond,
		Error: true, Err: "boom\u2066\ufeff"})
	telemetrytest.Append(t, dir, itel.KindTraces, at, trace)
	telemetrytest.Append(t, dir, itel.KindLogs, at, []byte(`{"time":"`+at.Format(time.RFC3339Nano)+`","level":"INFO","msg":"m`+"\u2029\u200d"+`","trace_id":"`+id+`"}`+"\n"))
	r := itel.NewRollup()
	r.Observe(at, route, time.Millisecond, true, false)
	telemetrytest.Append(t, dir, itel.KindStats, at.Add(time.Minute), r.Take(at.Add(time.Minute), true))
	var astral map[string]string
	if err := json.Unmarshal(jsonLine([]byte(`{"m":"a`+"\U000E0041"+`b"}`)), &astral); err != nil || astral["m"] != "a\U000E0041b" {
		t.Errorf("a rune past U+FFFF is not escaped as a surrogate pair: %q, %v", astral["m"], err)
	}
	for _, args := range [][]string{
		{"logs", "--dir", dir, "--json"},
		{"requests", "--dir", dir, "--json"},
		{"issues", "--dir", dir, "--json"},
		{"stats", "--dir", dir, "--json"},
		{"request", id, "--dir", dir, "--json"},
	} {
		out, err := run(t, args...)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if strings.TrimSpace(out) == "" {
			t.Fatalf("%v printed nothing", args)
		}
		for _, r := range out {
			if r != '\n' && textsafe.IsUnsafe(r) {
				t.Errorf("%v printed %U raw: %q", args, r, out)
				break
			}
		}
		for line := range strings.Lines(out) {
			if !json.Valid([]byte(line)) {
				t.Errorf("%v printed a line that is not JSON: %q", args, line)
			}
		}
	}
}

// Through the commands: logs --json and request --json print stored
// lines, a torn one and one with a C1 control among them.
func TestJSONOutputIsScrubbed(t *testing.T) {
	dir := t.TempDir()
	at := now.Add(-time.Minute)
	stamp := at.UTC().Format(time.RFC3339Nano)
	id := strings.Repeat("d", 32)
	logs := `{"time":"` + stamp + `","level":"INFO","msg":"a` + "\u009b" + `2Jb","trace_id":"` + id + `"}` + "\n" +
		`{"time":"` + stamp + `","level":"INFO" torn ` + "\x1b[2J}\n"
	if err := os.WriteFile(filepath.Join(dir, itel.FileName(itel.KindLogs, at)), []byte(logs), 0o600); err != nil {
		t.Fatal(err)
	}
	trace := itel.AppendTrace(nil, itel.Trace{Time: at, TraceID: id, Route: "GET /a", Status: 200, Dur: time.Millisecond,
		Spans: []itel.Span{{SpanID: "s", Name: "n" + "\u009b" + "x", Dur: time.Millisecond}}})
	if err := os.WriteFile(filepath.Join(dir, itel.FileName(itel.KindTraces, at)), trace, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"logs", "--dir", dir, "--json"}, {"request", id, "--dir", dir, "--json"}} {
		out, err := run(t, args...)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if strings.ContainsRune(out, 0x9b) || strings.Contains(out, "\x1b") {
			t.Errorf("%v printed a character a terminal obeys: %q", args, out)
		}
		if !strings.Contains(out, `\u009b`) {
			t.Errorf("%v: the character is gone instead of escaped: %q", args, out)
		}
	}
}

func TestTornJSONLineEscapesUnsafeRunes(t *testing.T) {
	torn := "{\"msg\":\"a\u009b2Jb‮c⁦d‏e torn"
	got := string(jsonLine([]byte(torn)))
	for _, r := range got {
		if r >= 0x20 && textsafe.IsUnsafe(r) {
			t.Fatalf("U+%04X reached the output: %q", r, got)
		}
	}
	var text string
	if err := json.Unmarshal([]byte(got), &text); err != nil || text != torn {
		t.Errorf("the torn line reads as %q (err %v), want %q", text, err, torn)
	}
}
