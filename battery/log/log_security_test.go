package log

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

func TestFileSinkParentDirDefaultModeIs0o700(t *testing.T) {
	requirePOSIXFileModes(t)
	dir := filepath.Join(t.TempDir(), "logs")
	path := filepath.Join(dir, "app.log")

	s, err := FileSink(path, FileOpts{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("SECURITY: [log-files] parent directory mode = %o. Attack: same-host users can traverse log directories created with broader-than-owner-only permissions.", got)
	}
}

func TestTailFileRejectsSymlinkPath(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.log")
	if err := os.WriteFile(target, []byte("{\"msg\":\"secret\",\"token\":\"top-secret\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "app.log")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	entries, err := tailFile(link, 10)
	if err == nil {
		t.Fatalf("SECURITY: [log-tail] historical log reader followed symlink and exposed %#v. Attack: swapped log path can exfiltrate arbitrary JSONL files through MCP historical reads.", entries)
	}
}

func TestMetricsHandlerCarriesNoStore(t *testing.T) {
	sink := &memSink{}
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "test"}))
	app.RegisterPlugin(New(Config{Sinks: []Sink{sink}}))
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}

	p, _ := app.Plugins.Get("log")
	lp := p.(*Plugin)
	rec := httptest.NewRecorder()
	lp.MetricsHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("SECURITY: [log-metrics] metrics handler missing Cache-Control no-store: %#v", rec.Header())
	}
}

// TestForwardedHeaderCapped pins that attacker-controlled forwarding
// headers are size-capped like every other request-derived log field.
// Under Go's default ~1 MiB MaxHeaderBytes a single X-Forwarded-For /
// X-Real-IP header can otherwise write a multi-MB log line per request,
// fanned out to every file/webhook sink, the same disk/network
// amplification DoS the path/panic caps already guard against.
func TestForwardedHeaderCapped(t *testing.T) {
	huge := strings.Repeat("a", 64<<10) // 64 KiB

	cases := []struct {
		name     string
		trustXFF bool
		headers  map[string]string
	}{
		{"happy short xff", false, map[string]string{"X-Forwarded-For": "1.2.3.4"}},
		{"giant xff untrusted", false, map[string]string{"X-Forwarded-For": huge}},
		{"giant xff trusted leaks into remote", true, map[string]string{"X-Forwarded-For": huge + ", 9.9.9.9"}},
		{"giant x-real-ip trusted", true, map[string]string{"X-Real-IP": huge}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&buf, nil))
			mw := accessMiddleware(logger, tc.trustXFF)
			h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(http.MethodGet, "/p", nil)
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			h.ServeHTTP(httptest.NewRecorder(), req)

			var entry map[string]any
			if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &entry); err != nil {
				t.Fatalf("decode log entry: %v (%q)", err, buf.String())
			}

			if got := entry["forwarded_for"].(string); len(got) > maxPathLen<<1 {
				t.Fatalf("SECURITY: [log-headers] forwarded_for length = %d, want ≤ %d. Attack: a ~1 MiB X-Forwarded-For header writes a multi-MB log line per request to every sink.", len(got), maxPathLen<<1)
			}
			if got := entry["remote"].(string); len(got) > maxPathLen<<1 {
				t.Fatalf("SECURITY: [log-headers] remote length = %d, want ≤ %d. Attack: a giant trusted XFF/X-Real-IP value flows uncapped into `remote`.", len(got), maxPathLen<<1)
			}
		})
	}
}

// driveAccess runs one request through accessMiddleware with a JSON
// logger and returns the decoded entry, the same harness shape
// TestForwardedHeaderCapped uses.
func driveAccess(t *testing.T, method, target string, hdr map[string]string) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	h := accessMiddleware(logger, false)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(method, target, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	h.ServeHTTP(httptest.NewRecorder(), req)
	var entry map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &entry); err != nil {
		t.Fatalf("decode log entry: %v (%q)", err, buf.String())
	}
	return entry
}

// hasRawC0 reports whether s carries any C0 byte or DEL outside the
// tab/newline a rendered log line legitimately uses.
func hasRawC0(s string) bool {
	for i := range len(s) {
		if (s[i] < 0x20 && s[i] != '\t' && s[i] != '\n') || s[i] == 0x7f {
			return true
		}
	}
	return false
}

// Property: no request-derived field of an http.access entry carries a
// raw C0/DEL byte. r.URL.Path is percent-DECODED, so %1b / %0d%0a / %00
// in a request URL are real ESC/CRLF/NUL by the time accessMiddleware
// snapshots them; core/middleware scrubs exactly this
// (safeLogPath/safeLogMethod) on its own sinks, but battery/log is the
// production access path and its entries fan out to the file sink, the
// webhook sink, the console sink and the MCP ring — a raw ESC in `path`
// is terminal-escape injection into every operator tail, and a CRLF
// forges entries in any downstream line-oriented consumer.
func TestAccessEntryScrubbedOfControlBytes(t *testing.T) {
	cases := []struct {
		name   string
		method string
		target string
		hdr    map[string]string
	}{
		{"esc in path", "GET", "/%1b%5b31mFAKE-ANSI", nil},
		{"crlf in path", "GET", "/%0d%0aFAKE-ENTRY", nil},
		{"nul in path", "GET", "/%00null", nil},
		{"del in path", "GET", "/%7fdel", nil},
		{"tab in path", "GET", "/%09tab", nil},
		// NB: r.Method cannot carry control bytes through a real server
		// (net/http validates the token), so the method needs no case
		// here; core/middleware's safeLogMethod is defense-in-depth.
		{"tab in xff", "GET", "/p", map[string]string{"X-Forwarded-For": "1.2.3.4\t5.6.7.8"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entry := driveAccess(t, tc.method, tc.target, tc.hdr)
			for _, field := range []string{"method", "path", "request_id", "remote", "forwarded_for"} {
				s, _ := entry[field].(string)
				if hasRawC0(s) {
					t.Errorf("SECURITY: [log-inject] %s: field %q carries raw control bytes: %q", tc.name, field, s)
				}
			}
		})
	}
}

// The console sink is the terminal boundary for entries whose emitters
// the plugin does not control (any app slog call), so a rendered line
// must never carry a raw C0/DEL byte, whatever the entry contains. This
// pins the observed safe split: attr values carrying control bytes are
// re-quoted by needsQuoting, and msg/attr keys keep their escaped text
// (safe, if mangled). A regression on either half — decoding msg/keys
// to raw bytes, or dropping the needsQuoting branch — reopens terminal
// escape injection straight into the operator's console.
func TestConsoleSinkRendersNoControlBytes(t *testing.T) {
	// Valid JSON with \u001b escapes — exactly what the fanout's JSON
	// encoder emits for an entry whose strings carry ESC. The malformed
	// entry path (raw bytes echoed verbatim) is documented behaviour in
	// console.go and deliberately not asserted here.
	entry := []byte(`{"time":"2026-01-01T00:00:00Z","level":"INFO","msg":"boom ` + "\\u001b[31m" + `red","k\\u001bx":"v` + "\\u001b[0m" + `","plain":"ok"}`)
	var buf bytes.Buffer
	s := ConsoleSink(ConsoleOpts{Writer: &buf})
	if err := s.Write(entry); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "plain=ok") {
		t.Fatalf("premise broken: entry not rendered: %q", out)
	}
	if hasRawC0(out) {
		t.Fatalf("SECURITY: [log-inject] console line carries raw control bytes: %q", out)
	}
}

// Property: the query string never enters the log stream. Only
// r.URL.Path is logged (never URL.String() or RawQuery), so a secret
// in a query parameter cannot reach the file sink, the webhook, or the
// MCP log tools. Pins the accessor choice against a refactor to the
// full URL.
func TestAccessLogOmitsQueryString(t *testing.T) {
	entry := driveAccess(t, "GET", "/reset?token=S3CR3T&sig=abcd", nil)
	if got, _ := entry["path"].(string); got != "/reset" {
		t.Errorf("SECURITY: [secret-leak] logged path = %q, want /reset — the query string must not be logged", got)
	}
	if blob, err := json.Marshal(entry); err == nil && strings.Contains(string(blob), "S3CR3T") {
		t.Errorf("SECURITY: [secret-leak] query-string secret reached the log entry: %s", blob)
	}
}
