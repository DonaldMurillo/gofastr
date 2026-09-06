package desktop_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/desktoptest"
)

// The updater under the harness: the real Run flow (listener, boot
// token, frozen registry, chokepoint) with an httptest feed on
// 127.0.0.1 and a fake update platform injected through the test seam,
// so the apply step (codesign verify, swap, relaunch) is observed
// without native code.

// harnessUpdatePlatform is the fake applier: reports a fixed running
// version and bundle, records the apply steps.
type harnessUpdatePlatform struct {
	version string
	name    string
	dir     string

	mu         sync.Mutex
	verified   []string
	relaunched []string
}

func (f *harnessUpdatePlatform) Version() string { return f.version }

func (f *harnessUpdatePlatform) Bundle() (string, string, bool) {
	return f.name, f.dir, f.name != ""
}

func (f *harnessUpdatePlatform) VerifySignature(appDir string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.verified = append(f.verified, appDir)
	return nil
}

func (f *harnessUpdatePlatform) Relaunch(appDir string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.relaunched = append(f.relaunched, appDir)
	return nil
}

// harnessAppZip builds a minimal Notes.app bundle zip.
func harnessAppZip(t *testing.T, marker string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	files := map[string]struct {
		content []byte
		mode    fs.FileMode
	}{
		"Notes.app/":                     {nil, 0o755 | fs.ModeDir},
		"Notes.app/Contents/":            {nil, 0o755 | fs.ModeDir},
		"Notes.app/Contents/MacOS/":      {nil, 0o755 | fs.ModeDir},
		"Notes.app/Contents/MacOS/Notes": {[]byte("#!new binary " + marker), 0o755},
		"Notes.app/Contents/Info.plist":  {[]byte(`<?xml version="1.0"?><plist/>`), 0o644},
	}
	for name, f := range files {
		hdr := &zip.FileHeader{Name: name, Method: zip.Deflate}
		hdr.SetMode(f.mode)
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			t.Fatal(err)
		}
		if f.content != nil {
			if _, err := w.Write(f.content); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestUpdatesCheckAndInstallThroughThePage(t *testing.T) {
	// The release side: a key pair, a signed feed, and both served
	// from loopback alongside the archive.
	feedDir := t.TempDir()
	archivePath := filepath.Join(feedDir, "Notes-1.2.0.zip")
	if err := os.WriteFile(archivePath, harnessAppZip(t, "v1.2.0"), 0o600); err != nil {
		t.Fatal(err)
	}
	pub, priv, _ := ed25519.GenerateKey(nil)
	keyPath := filepath.Join(feedDir, "update.key")
	seedPub := hex.EncodeToString(priv.Seed()) + hex.EncodeToString(pub)
	if err := os.WriteFile(keyPath, []byte(seedPub), 0o600); err != nil {
		t.Fatal(err)
	}
	var manifest, sig, archiveBytes []byte
	var err error
	mux := http.NewServeMux()
	mux.HandleFunc("/feed/manifest.json", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(manifest) })
	mux.HandleFunc("/feed/manifest.json.sig", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(sig) })
	mux.HandleFunc("/Notes-1.2.0.zip", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(archiveBytes) })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// The feed is written with the archive URL on the same loopback
	// origin the updater will download from (plain http is allowed to
	// 127.0.0.1).
	archiveURL := srv.URL + "/Notes-1.2.0.zip"
	if err := desktop.SignUpdateFeed(feedDir, keyPath, "1.2.0", "What changed",
		"darwin-arm64", archivePath, archiveURL); err != nil {
		t.Fatal(err)
	}
	manifest, err = os.ReadFile(filepath.Join(feedDir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	sig, err = os.ReadFile(filepath.Join(feedDir, "manifest.json.sig"))
	if err != nil {
		t.Fatal(err)
	}
	archiveBytes, err = os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}

	// The running app: version 1.0.0, a fake bundle directory holding
	// the "current" Notes.app, updates pointed at the feed. Interval
	// -1 keeps the 30 s scheduled check out of the test.
	bundleDir := t.TempDir()
	currentApp := filepath.Join(bundleDir, "Notes.app")
	if err := os.MkdirAll(filepath.Join(currentApp, "Contents", "MacOS"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(currentApp, "Contents", "MacOS", "Notes"), []byte("#!old binary v1.0.0"), 0o755); err != nil {
		t.Fatal(err)
	}
	dataDir := t.TempDir()
	t.Setenv("GOFASTR_DESKTOP_DATA_DIR", dataDir)

	app, d := uiApp(t, desktop.Config{
		Title: "Updates",
		Update: &desktop.UpdateConfig{
			FeedURL:   srv.URL + "/feed/manifest.json",
			PublicKey: hex.EncodeToString(pub),
			Interval:  -1,
		},
	})
	fp := &harnessUpdatePlatform{version: "1.0.0", name: "Notes.app", dir: bundleDir}
	d.SetUpdatePlatform(fp)

	h := desktoptest.Run(t, app, d)

	// The capability is in the frozen manifest.
	found := false
	for _, c := range h.Manifest().Capabilities {
		if c.Name == "updates" {
			found = true
		}
	}
	if !found {
		t.Fatal("updates capability missing from the manifest")
	}

	// Check is ungated and reports the newer release.
	var check struct {
		Available bool   `json:"available"`
		Version   string `json:"version"`
		Notes     string `json:"notes"`
	}
	h.Call("updates", "check", nil).MustResult(t, &check)
	if !check.Available || check.Version != "1.2.0" || check.Notes != "What changed" {
		t.Fatalf("check = %+v", check)
	}
	if n := len(h.Shell.Prompts()); n != 0 {
		t.Fatalf("prompts = %d, want 0 (check is ungated)", n)
	}

	// Install without a grant: the OS prompt fires, the fake shell
	// denies unanswered prompts, the call is refused, and nothing was
	// downloaded or swapped.
	h.Call("updates", "install", nil).AssertCode(t, desktop.CodeDenied)
	if prompts := h.Shell.Prompts(); len(prompts) != 1 ||
		prompts[0].Capability != "updates" || prompts[0].Permission != "updates:install" {
		t.Fatalf("prompts = %+v", prompts)
	}
	if n := len(fp.verified) + len(fp.relaunched); n != 0 {
		t.Fatalf("apply steps ran without a grant: %v %v", fp.verified, fp.relaunched)
	}
	if _, err := os.Stat(filepath.Join(bundleDir, "Notes.app.new")); !os.IsNotExist(err) {
		t.Fatal("staged bundle left beside the app after a denied install")
	}

	// Install with the user's Allow: the full path runs (download,
	// sha256 and size verify, extract, codesign, swap, relaunch), the
	// response still reaches the page, and the swap is observable on
	// disk.
	if err := d.ResetGrantsForTest(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.Answer(desktop.DecisionAllow)
	h.Call("updates", "install", nil).AssertOK(t)

	fp.mu.Lock()
	verified, relaunched := append([]string(nil), fp.verified...), append([]string(nil), fp.relaunched...)
	fp.mu.Unlock()
	if len(verified) != 1 || !filepath.IsAbs(verified[0]) || filepath.Base(verified[0]) != "Notes.app" {
		t.Fatalf("codesign verify calls = %v", verified)
	}
	if len(relaunched) != 1 || relaunched[0] != currentApp {
		t.Fatalf("relaunch calls = %v, want the installed bundle", relaunched)
	}
	// The extracted executable replaced the old one.
	exe, err := os.ReadFile(filepath.Join(currentApp, "Contents", "MacOS", "Notes"))
	if err != nil || !bytes.Contains(exe, []byte("v1.2.0")) {
		t.Fatalf("installed executable = %q, %v", exe, err)
	}
	exeInfo, err := os.Stat(filepath.Join(currentApp, "Contents", "MacOS", "Notes"))
	if err != nil || exeInfo.Mode().Perm() != 0o755 {
		t.Fatalf("installed executable mode = %v, %v", exeInfo, err)
	}
	// The previous bundle moved aside; no staging left in the data dir.
	if _, err := os.Stat(currentApp + ".old"); err != nil {
		t.Fatalf("the old bundle was not moved aside: %v", err)
	}
	if _, err := os.Stat(filepath.Join(bundleDir, "Notes.app.new")); !os.IsNotExist(err) {
		t.Fatal("the .new staging bundle survived the swap")
	}
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "update-") {
			t.Fatalf("staging directory %s survived the install", e.Name())
		}
	}
	// The quit the install scheduled ends Run; h.Quit is idempotent
	// and returns Run's error (the install's own quit path is also
	// covered by the cleanup).
	if err := h.Quit(); err != nil {
		t.Fatalf("Run returned %v after the install", err)
	}
}
