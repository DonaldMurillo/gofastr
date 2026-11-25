package desktopcmd

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/internal/procalive"
)

const updateE2EID = "dev.gofastr.updatee2e"

type updateE2EReport struct {
	PID         int    `json:"pid"`
	Predecessor string `json:"predecessor"`
	Available   bool   `json:"available"`
	FeedVersion string `json:"feed_version"`
	Error       string `json:"error"`
}

// TestLinuxDesktopUpdateEndToEnd runs the whole Linux update path in
// real processes, on the desktoptest shell in place of GTK: build 1.0.0
// and 2.0.0 as release archives, install 1.0.0 with desktop install,
// sign a feed offering 2.0.0, start the installed launcher, let 1.0.0
// install the update and relaunch, and check that 2.0.0 started after
// it, cleared the previous installation, and that desktop uninstall
// then removes every installed file.
func TestLinuxDesktopUpdateEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the app twice")
	}
	dist := t.TempDir()
	archives := map[string]string{}
	for _, version := range []string{"1.0.0", "2.0.0"} {
		out := filepath.Join(dist, version)
		f := desktopBuildFlags{id: updateE2EID, version: version, pkg: "./testdata/updatemain", out: out, format: "tar.gz"}
		if err := buildLinuxDesktopArchive(f, "Update E2E", runtime.GOARCH); err != nil {
			t.Fatalf("build %s: %v", version, err)
		}
		root, err := linuxBundleRoot(f, "Update E2E", runtime.GOARCH)
		if err != nil {
			t.Fatal(err)
		}
		archives[version] = filepath.Join(out, root+".tar.gz")
	}

	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(dist, "update.key")
	if err := os.WriteFile(keyPath, []byte(hex.EncodeToString(priv.Seed())+hex.EncodeToString(pub)), 0o600); err != nil {
		t.Fatal(err)
	}
	feedDir := filepath.Join(dist, "feed")
	srv := httptest.NewServer(http.FileServer(http.Dir(feedDir)))
	t.Cleanup(srv.Close)
	if err := os.MkdirAll(feedDir, 0o700); err != nil {
		t.Fatal(err)
	}
	archive, err := os.ReadFile(archives["2.0.0"])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(feedDir, "app.tar.gz"), archive, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := desktop.SignUpdateFeed(feedDir, keyPath, "2.0.0", "", "linux-"+runtime.GOARCH, archives["2.0.0"], srv.URL+"/app.tar.gz", 0); err != nil {
		t.Fatalf("desktop feed refused the Linux archive: %v", err)
	}

	paths := linuxInstallTestHome(t)
	extracted := filepath.Join(t.TempDir(), "v1")
	extractTestTarGz(t, archives["1.0.0"], extracted)
	if err := installLinuxBundle(extracted); err != nil {
		t.Fatalf("install 1.0.0: %v", err)
	}

	reports := t.TempDir()
	cmd := exec.Command(filepath.Join(paths.bin, updateE2EID))
	cmd.Env = append(os.Environ(),
		"UPDATEMAIN_FEED="+srv.URL+"/manifest.json",
		"UPDATEMAIN_KEY="+hex.EncodeToString(pub),
		"UPDATEMAIN_REPORTS="+reports,
		"GOFASTR_DESKTOP_UPDATE_PREDECESSOR=",
	)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Run(); err != nil {
		t.Fatalf("1.0.0 exited with %v:\n%s", err, output.String())
	}
	first := readUpdateReport(t, reports, cmd.Process.Pid, &output)
	if !first.Available || first.FeedVersion != "2.0.0" || first.Error != "" {
		t.Fatalf("1.0.0 saw %+v\n%s", first, output.String())
	}

	var second updateE2EReport
	waitUntil(t, "the relaunched 2.0.0 to report", func() bool {
		entries, _ := os.ReadDir(reports)
		for _, e := range entries {
			if e.Name() != strconv.Itoa(first.PID)+".json" {
				second = readUpdateReport(t, reports, mustAtoi(t, e.Name()[:len(e.Name())-len(".json")]), &output)
				return true
			}
		}
		return false
	})
	if second.Predecessor != strconv.Itoa(first.PID) {
		t.Fatalf("2.0.0 named predecessor %q, want 1.0.0's pid %d", second.Predecessor, first.PID)
	}
	if second.Available || second.FeedVersion != "2.0.0" || second.Error != "" {
		t.Fatalf("2.0.0 saw %+v", second)
	}
	waitUntil(t, "2.0.0 to exit", func() bool { return !procalive.Alive(second.PID) })

	manifest, err := os.ReadFile(filepath.Join(paths.apps, updateE2EID, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m linuxBundleManifest
	if err := json.Unmarshal(manifest, &m); err != nil || m.Version != "2.0.0" {
		t.Fatalf("installed manifest version %q (%v), want 2.0.0", m.Version, err)
	}
	if _, err := os.Lstat(filepath.Join(paths.apps, updateE2EID+".old")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("2.0.0 did not clear the previous installation: %v", err)
	}

	if err := uninstallLinuxBundle(updateE2EID); err != nil {
		t.Fatalf("desktop uninstall refused the updated installation: %v", err)
	}
	for _, p := range []string{filepath.Join(paths.apps, updateE2EID), filepath.Join(paths.bin, updateE2EID), filepath.Join(paths.applications, updateE2EID+".desktop")} {
		if _, err := os.Lstat(p); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s survived uninstall: %v", p, err)
		}
	}
	if left, _ := os.ReadDir(paths.apps); len(left) != 0 {
		t.Errorf("the apps directory still holds %d entries", len(left))
	}
}

func readUpdateReport(t *testing.T, dir string, pid int, output *bytes.Buffer) updateE2EReport {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, strconv.Itoa(pid)+".json"))
	if err != nil {
		t.Fatalf("no report from pid %d: %v\n%s", pid, err, output.String())
	}
	var r updateE2EReport
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func mustAtoi(t *testing.T, s string) int {
	t.Helper()
	n, err := strconv.Atoi(s)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(60 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

// extractTestTarGz unpacks a release archive's one root directory into dest.
func extractTestTarGz(t *testing.T, archive, dest string) {
	t.Helper()
	f, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		_, rel, _ := strings.Cut(hdr.Name, "/")
		target := filepath.Join(dest, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, data, fs.FileMode(hdr.Mode)); err != nil {
			t.Fatal(err)
		}
	}
}
