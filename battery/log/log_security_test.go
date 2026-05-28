package log

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
