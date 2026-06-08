package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	xharness "github.com/DonaldMurillo/gofastr/framework/harness"
	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/harness/profile"
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
	dir := t.TempDir()
	if l := buildLogger(dir, false, ""); l == nil {
		t.Fatal("nil logger")
	}
	if l := buildLogger(dir, true, "engine=debug"); l == nil {
		t.Fatal("nil logger with stderr + overrides")
	}
}

func TestMustGetwd(t *testing.T) {
	if mustGetwd() == "" {
		t.Fatal("mustGetwd empty")
	}
}

func TestLoadProfile(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	covT_chdir(t, repoRoot)
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
	a := deriveListenerSecret()
	if len(a) != 32 {
		t.Fatalf("random secret len = %d", len(a))
	}
	// Deterministic branch (machine key set).
	t.Setenv("GOFASTR_HARNESS_MACHINE_KEY", "0123456789abcdef0123456789abcdef")
	b := deriveListenerSecret()
	c := deriveListenerSecret()
	if len(b) != 32 || string(b) != string(c) {
		t.Fatal("machine-key secret must be deterministic 32 bytes")
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
