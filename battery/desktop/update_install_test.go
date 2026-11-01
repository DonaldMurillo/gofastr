package desktop

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/update"
)

// The install step in-package: the single-flight lock and the check
// that the extracted bundle is the release the feed names, for this
// app. The full page-driven flow is update_harness_test.go.

const installTestID = "dev.gofastr.notes"

// installBundleZip is a Notes.app archive whose Info.plist declares id
// and version.
func installBundleZip(t *testing.T, id, version string) []byte {
	t.Helper()
	plist := `<?xml version="1.0"?><plist version="1.0"><dict>` +
		`<key>CFBundleIdentifier</key><string>` + id + `</string>` +
		`<key>CFBundleShortVersionString</key><string>` + version + `</string>` +
		`</dict></plist>`
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range []struct {
		name, body string
		dir        bool
	}{
		{"Notes.app/", "", true},
		{"Notes.app/Contents/", "", true},
		{"Notes.app/Contents/MacOS/", "", true},
		{"Notes.app/Contents/MacOS/Notes", "#!new " + version, false},
		{"Notes.app/Contents/Info.plist", plist, false},
	} {
		hdr := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		if e.dir {
			hdr.SetMode(0o755 | os.ModeDir)
		} else {
			hdr.SetMode(0o755)
		}
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			t.Fatal(err)
		}
		if !e.dir {
			if _, err := w.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// installFixture serves a signed feed for feedVersion whose archive is
// archive, and returns a battery running 1.0.0 from a temp bundle dir.
func installFixture(t *testing.T, feedVersion string, archive []byte) (*Battery, *fakeUpdatePlatform, string) {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(nil)
	sum := sha256.Sum256(archive)
	var manifest, sig []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/manifest.json":
			_, _ = w.Write(manifest)
		case "/manifest.json.sig":
			_, _ = w.Write(sig)
		case "/Notes.zip":
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	var err error
	manifest, sig, err = update.SignFeed(update.Feed{
		Version: feedVersion,
		Platforms: map[string]update.PlatformEntry{
			update.PlatformKey(): {URL: srv.URL + "/Notes.zip", SHA256: hex.EncodeToString(sum[:]), Size: int64(len(archive))},
		},
	}, priv)
	if err != nil {
		t.Fatal(err)
	}
	bundleDir := t.TempDir()
	exe := filepath.Join(bundleDir, "Notes.app", "Contents", "MacOS")
	if err := os.MkdirAll(exe, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(exe, "Notes"), []byte("#!old 1.0.0"), 0o700); err != nil {
		t.Fatal(err)
	}
	b, fp := updateTestBattery(t, srv.URL+"/manifest.json", hex.EncodeToString(pub), "1.0.0", "Notes.app", bundleDir)
	b.dataDir = t.TempDir()
	fp.identifier = installTestID
	return b, fp, bundleDir
}

// runningExe reads the executable of the bundle in place.
func runningExe(t *testing.T, bundleDir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(bundleDir, "Notes.app", "Contents", "MacOS", "Notes"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestInstallRefusesConcurrentInstall(t *testing.T) {
	b, fp, bundleDir := installFixture(t, "1.2.0", installBundleZip(t, installTestID, "1.2.0"))
	entered, release := make(chan struct{}), make(chan struct{})
	// Only the first call parks; a second one that got past the lock
	// runs on (sync.Once would park it too and hide the bug as a hang).
	var calls atomic.Int32
	fp.verifyHook = func() {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
		}
	}
	first := make(chan error, 1)
	go func() { first <- b.InstallUpdate(context.Background()) }()
	<-entered
	// The first install is mid-flight, between extraction and the swap.
	err := b.InstallUpdate(context.Background())
	close(release)
	var derr *Error
	if !errors.As(err, &derr) || derr != errUpdateInProgress {
		t.Fatalf("second install during the first = %v, want %v", err, errUpdateInProgress)
	}
	if err := <-first; err != nil {
		t.Fatalf("first install = %v", err)
	}
	if got := runningExe(t, bundleDir); got != "#!new 1.2.0" {
		t.Fatalf("installed executable = %q", got)
	}
	// After the swap the process is quitting: a third call must not
	// swap over the new bundle.
	if err := b.InstallUpdate(context.Background()); !errors.Is(err, errUpdateInstalled) {
		t.Fatalf("install after a finished install = %v, want %v", err, errUpdateInstalled)
	}
	if len(fp.relaunched) != 1 {
		t.Fatalf("relaunches = %v, want exactly one", fp.relaunched)
	}
}

func TestInstallRefusesMislabelledBundle(t *testing.T) {
	for name, archive := range map[string][]byte{
		"older version": installBundleZip(t, installTestID, "1.1.0"),
		"other app":     installBundleZip(t, "dev.gofastr.other", "1.2.0"),
	} {
		t.Run(name, func(t *testing.T) {
			b, fp, bundleDir := installFixture(t, "1.2.0", archive)
			err := b.InstallUpdate(context.Background())
			if !errors.Is(err, errUpdateArchive) {
				t.Fatalf("install = %v, want %v", err, errUpdateArchive)
			}
			if got := runningExe(t, bundleDir); got != "#!old 1.0.0" {
				t.Fatalf("the running bundle was replaced: %q", got)
			}
			if len(fp.verified)+len(fp.relaunched) != 0 {
				t.Fatalf("apply steps ran: %v %v", fp.verified, fp.relaunched)
			}
		})
	}
}

func TestInstallRefusesWithoutRunningIdentifier(t *testing.T) {
	b, fp, bundleDir := installFixture(t, "1.2.0", installBundleZip(t, installTestID, "1.2.0"))
	fp.identifier = ""
	if err := b.InstallUpdate(context.Background()); !errors.Is(err, errUpdateArchive) {
		t.Fatalf("install with no running identifier = %v", err)
	}
	if got := runningExe(t, bundleDir); got != "#!old 1.0.0" {
		t.Fatalf("the running bundle was replaced: %q", got)
	}
}
