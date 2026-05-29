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
// fanned out to every file/webhook sink — the same disk/network
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
