package service

import (
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A localhost tab is sent to the address `open` signs the browser in
// at, which for an IPv6 bind is [::1], not 127.0.0.1.
func TestCanonicalRedirectFollowsBrowserAddr(t *testing.T) {
	for bind, want := range map[string]string{
		"127.0.0.1:47811": "http://127.0.0.1:47811/todos?a=1",
		"0.0.0.0:47811":   "http://127.0.0.1:47811/todos?a=1",
		"[::1]:47811":     "http://[::1]:47811/todos?a=1",
		"[::]:47811":      "http://[::1]:47811/todos?a=1",
	} {
		g := newTestGate(t, false)
		g.pinAt(browserAddr(bind), gatePort, nil)
		r := httptest.NewRequest("GET", "http://localhost:47811/todos?a=1", nil)
		if got := g.canonicalURL(r); got != want {
			t.Errorf("bind %s: localhost tab sent to %q; want %q", bind, got, want)
		}
	}
}

// Tool output and log lines are data: both reach the terminal through
// textsafe.ScrubTerminal, C1 controls and escapes dropped, lines kept.
func TestToolOutputAndLogsAreScrubbedForTheTerminal(t *testing.T) {
	const raw = "GET /x\u009b31mred\u009dtitle\u009c\x1b[0m\x7f\tok\nnext\u202e\n"
	const want = "GET /x31mredtitle[0m\tok\nnext"
	if got := toolError("launchctl", nil, []byte(raw), errors.New("exit 1")).Error(); !strings.HasSuffix(got, ": "+want) {
		t.Errorf("toolError = %q", got)
	}
	path := filepath.Join(t.TempDir(), "app.log")
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := tailFile(&out, path, 10, false); err != nil || out.String() != want+"\n" {
		t.Errorf("tailFile printed %q, err %v", out.String(), err)
	}
}
