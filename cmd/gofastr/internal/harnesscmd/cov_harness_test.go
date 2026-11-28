package harnesscmd

import (
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	xharness "github.com/DonaldMurillo/gofastr/framework/experimental/harness"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/profile"
)

func TestIsTTY(t *testing.T) {
	if isTTY(nil) {
		t.Fatal("nil file is not a tty")
	}
	// A regular file is not a terminal.
	f, err := os.CreateTemp(t.TempDir(), "x")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if isTTY(f) {
		t.Fatal("temp file should not be a tty")
	}
}

func TestMachineKeyFromEnv(t *testing.T) {
	t.Setenv("GOFASTR_HARNESS_MACHINE_KEY", "")
	if k, err := machineKeyFromEnv(); err != nil || k != nil {
		t.Fatalf("empty → nil,nil (got %v,%v)", k, err)
	}
	t.Setenv("GOFASTR_HARNESS_MACHINE_KEY", "0123456789abcdef0123456789abcdef") // 32 raw bytes
	if k, err := machineKeyFromEnv(); err != nil || len(k) != 32 {
		t.Fatalf("32-byte raw key accepted (got len=%d, err=%v)", len(k), err)
	}
	t.Setenv("GOFASTR_HARNESS_MACHINE_KEY", "tooshort")
	if _, err := machineKeyFromEnv(); err == nil {
		t.Fatal("bad value should error, not silently downgrade")
	}
}

func TestBuildLogger(t *testing.T) {
	readLog := func(dir string) string {
		t.Helper()
		files, _ := filepath.Glob(filepath.Join(dir, "log", "harness-*.log"))
		if len(files) != 1 {
			t.Fatalf("want one daily log file under %s/log, got %v", dir, files)
		}
		b, _ := os.ReadFile(files[0])
		return string(b)
	}
	dir := t.TempDir()
	quiet := covT_capStderr(t, func() {
		l := buildLogger(dir, false, "")
		l.Info("info-line")
		l.Debug("debug-line")
	})
	if got := readLog(dir); !strings.Contains(got, "info-line") || strings.Contains(got, "debug-line") {
		t.Fatalf("default logger should log info, not debug, to the file: %q", got)
	}
	if quiet != "" {
		t.Fatalf("toStderr=false wrote to stderr: %q", quiet)
	}
	dir = t.TempDir()
	errOut := covT_capStderr(t, func() {
		l := buildLogger(dir, true, "engine=debug")
		l.WithComponent("engine").Debug("engine-debug")
		l.WithComponent("other").Debug("other-debug")
	})
	for name, got := range map[string]string{"file": readLog(dir), "stderr": errOut} {
		if !strings.Contains(got, "engine-debug") || strings.Contains(got, "other-debug") {
			t.Fatalf("%s: engine=debug override should apply to engine only: %q", name, got)
		}
	}
}

func TestMustGetwd(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	clitest.CovT_chdir(t, dir)
	if got := mustGetwd(); got != dir {
		t.Fatalf("mustGetwd = %q, want %q", got, dir)
	}
}

func TestLoadProfile(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	clitest.CovT_chdir(t, repoRoot)
	if _, err := loadProfile(false, ""); err != nil {
		t.Fatalf("default profile: %v", err)
	}
	if _, err := loadProfile(true, ""); err != nil {
		t.Fatalf("framework profile: %v", err)
	}
	if _, err := loadProfile(false, "does/not/exist.toml"); err == nil {
		t.Fatal("missing explicit profile should error")
	}
}

func TestResolveDefaultModelMalformed(t *testing.T) {
	h := &xharness.Harness{}
	prov, model := resolveDefaultModel(h, &profile.Profile{DefaultModel: "no-colon"})
	if prov != nil || model != "" {
		t.Fatal("malformed default_model should yield nil provider")
	}
	// Provider name that matches nothing → nil.
	prov, _ = resolveDefaultModel(h, &profile.Profile{DefaultModel: "nope:model"})
	if prov != nil {
		t.Fatal("unknown provider should yield nil")
	}
}

func TestDeriveListenerSecret(t *testing.T) {
	// Random branch (no machine key).
	t.Setenv("GOFASTR_HARNESS_MACHINE_KEY", "")
	a := DeriveListenerSecret()
	if len(a) != 32 {
		t.Fatalf("random secret len = %d", len(a))
	}
	if string(a) == string(DeriveListenerSecret()) {
		t.Fatal("two secrets without a machine key are equal")
	}
	// Deterministic branch (machine key set).
	t.Setenv("GOFASTR_HARNESS_MACHINE_KEY", "0123456789abcdef0123456789abcdef")
	b := DeriveListenerSecret()
	c := DeriveListenerSecret()
	if len(b) != 32 || string(b) != string(c) {
		t.Fatal("machine-key secret must be deterministic 32 bytes")
	}
	// SHA-256 of "harness-http:" and the key; a new prefix would void every token a restart should keep.
	if got, want := hex.EncodeToString(b), "18387a27904c6e6e76e7ff0d4c43479d3b87a7ce9f526534cbabacd44e3ebd28"; got != want {
		t.Fatalf("machine-key secret = %s, want %s", got, want)
	}
}

func TestShortSessionLabel(t *testing.T) {
	if shortSessionLabel("short") != "short" {
		t.Fatal("short passthrough")
	}
	long := "sess_0123456789abcdefghij"
	got := shortSessionLabel(long)
	if !strings.HasSuffix(got, "…") || !strings.HasPrefix(got, long[:14]) {
		t.Fatalf("truncation = %q", got)
	}
}

func TestChatPageHandler(t *testing.T) {
	sess := ids.NewSessionID()
	h := func(w http.ResponseWriter, r *http.Request) { chatPage(w, r, sess, "tok") }

	// Root path → chat HTML.
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "gofastr harness") {
		t.Fatalf("root page: code=%d", rec.Code)
	}
	// /endpoints → landing reference.
	rec = httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/endpoints", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("endpoints page code = %d", rec.Code)
	}
	// Unknown path → 404.
	rec = httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown path code = %d", rec.Code)
	}
}
