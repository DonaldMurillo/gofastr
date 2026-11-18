package builtins

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestSourceKeepsToolSearchUnderAnyPack(t *testing.T) {
	for _, packs := range [][]string{{"web"}, {"no-such-pack"}} {
		tools, _ := Source{EnabledPacks: packs}.Tools(context.Background())
		found := false
		for _, tl := range tools {
			found = found || tl.Name() == "ToolSearch"
		}
		if !found {
			t.Errorf("packs %v dropped ToolSearch", packs)
		}
	}
}

func TestBuiltinMutatingFlags(t *testing.T) {
	tools, _ := Source{}.Tools(context.Background())
	want := map[string]bool{
		"Read": false, "Glob": false, "Ls": false, "Grep": false, "WebFetch": false, "ToolSearch": false, "TaskList": false,
		"Write": true, "Edit": true, "Bash": true, "Agent": true,
	}
	for _, tl := range tools {
		if m, ok := want[tl.Name()]; !ok || tl.Mutating() != m {
			t.Errorf("%s Mutating() = %v, want %v (listed: %v)", tl.Name(), tl.Mutating(), m, ok)
		}
	}
}

func TestBashExtraBlocklistAndPrefixes(t *testing.T) {
	skipNonUnix(t)
	b := Bash{ExtraBlocklist: []string{"gpg"}}
	for _, cmd := range []string{"gpg --export-secret-keys", "sudo keyctl show", "FOO=1 env BAR=2 nohup security x", "exec secret-tool lookup a b"} {
		res, _ := b.Run(context.Background(), mustCall(t, map[string]any{"cmd": cmd}), nil)
		if res == nil || !res.IsError || !strings.Contains(res.Content[0].Text, "blocked") {
			t.Errorf("Bash ran blocked command %q: %+v", cmd, res)
		}
	}
}

func TestBashOutputIsCapped(t *testing.T) {
	skipNonUnix(t)
	res, _ := (Bash{}).Run(context.Background(), mustCall(t, map[string]any{"cmd": "yes | head -c 3000000"}), nil)
	if res == nil || res.IsError {
		t.Fatalf("Bash failed: %+v", res)
	}
	out := res.Content[0].Text
	if len(out) > 1<<20+64 || !strings.HasSuffix(out, "[output truncated]\n") {
		t.Fatalf("Bash returned %d bytes, want at most 1 MiB ending in a truncation note", len(out))
	}
}

func TestWebFetchStripsCredentialHeaders(t *testing.T) {
	got := make(chan http.Header, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- r.Header.Clone()
	}))
	defer srv.Close()
	res, _ := WebFetch{HTTPClient: srv.Client(), AllowPrivateHosts: true}.Run(context.Background(), mustCall(t, map[string]any{
		"url":     srv.URL,
		"headers": map[string]string{"AUTHORIZATION": "Bearer x", "cookie": "s=1", "X-Harness-Token": "t", "X-Ok": "1"},
	}), nil)
	if res == nil || res.IsError {
		t.Fatalf("WebFetch failed: %+v", res)
	}
	h := <-got
	for _, k := range []string{"Authorization", "Cookie", "X-Harness-Token"} {
		if h.Get(k) != "" {
			t.Errorf("WebFetch forwarded %s", k)
		}
	}
	if h.Get("X-Ok") != "1" {
		t.Error("WebFetch dropped an ordinary header")
	}
}

func TestWebFetchCapsWhatTheModelSees(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("a", 100<<10)))
	}))
	defer srv.Close()
	res, _ := WebFetch{HTTPClient: srv.Client(), AllowPrivateHosts: true}.Run(context.Background(), mustCall(t, map[string]any{"url": srv.URL}), nil)
	if res == nil || res.IsError {
		t.Fatalf("WebFetch failed: %+v", res)
	}
	if n := strings.Count(res.Content[0].Text, "a"); n > webFetchModelMaxBody+200 || !strings.Contains(res.Content[0].Text, "truncated") {
		t.Fatalf("WebFetch returned %d body bytes with no truncation note, want at most %d", n, webFetchModelMaxBody)
	}
}

func TestBashTimeoutKillsBackgroundedChild(t *testing.T) {
	skipNonUnix(t)
	pidFile := filepath.Join(t.TempDir(), "pid")
	_, _ = (Bash{}).Run(context.Background(), mustCall(t, map[string]any{
		"cmd":        "sleep 30 & echo $! > " + pidFile + "; wait",
		"timeout_ms": 300,
	}), nil)
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(raw)))
	proc, _ := os.FindProcess(pid)
	defer func() { _ = proc.Kill() }()
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if proc.Signal(syscall.Signal(0)) != nil {
			return
		}
	}
	t.Fatalf("backgrounded child %d outlived the Bash timeout", pid)
}

func TestGrepStopsAtTheMatchCap(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte(strings.Repeat("needle\n", maxGrepMatches+50)), 0o600); err != nil {
		t.Fatal(err)
	}
	res, _ := Grep{}.Run(context.Background(), mustCall(t, map[string]any{"pattern": "needle", "path": dir}), nil)
	if n := strings.Count(res.Content[0].Text, ": needle"); n != maxGrepMatches {
		t.Fatalf("Grep returned %d matches, want the cap %d", n, maxGrepMatches)
	}
}

func TestWebFetchRefusesSchemeBeforeAnyRequest(t *testing.T) {
	for _, raw := range []string{"file:///etc/passwd", "gopher://example.com/", "ftp://example.com/x"} {
		res, _ := WebFetch{AllowPrivateHosts: true}.Run(context.Background(), mustCall(t, map[string]any{"url": raw}), nil)
		if res == nil || !res.IsError || !strings.Contains(res.Content[0].Text, "not allowed") {
			t.Errorf("WebFetch %q = %+v, want the scheme refused", raw, res)
		}
	}
}

func TestAssertPublicHostRefusesInternal(t *testing.T) {
	for _, h := range []string{"127.0.0.1:8080", "[::1]:443", "169.254.169.254", "10.1.2.3", "100.64.0.1", "localhost", ""} {
		if assertPublicHost(h) == nil {
			t.Errorf("assertPublicHost(%q) passed an internal host", h)
		}
	}
	if err := assertPublicHost("93.184.216.34:443"); err != nil {
		t.Errorf("assertPublicHost refused a public literal: %v", err)
	}
}

func TestCappedWriterNotesTruncationAtAnExactBoundary(t *testing.T) {
	var out strings.Builder
	w := &cappedWriter{w: &out, max: 4}
	_, _ = w.Write([]byte("abcd"))
	_, _ = w.Write([]byte("e"))
	_, _ = w.Write([]byte("f"))
	if got := out.String(); got != "abcd\n[output truncated]\n" {
		t.Fatalf("capped output = %q, want the cap's bytes and one truncation note", got)
	}
}
