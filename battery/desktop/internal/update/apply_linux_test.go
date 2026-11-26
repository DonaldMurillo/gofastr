//go:build linux

package update

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/linuxbundle"
	"github.com/DonaldMurillo/gofastr/internal/linuxbundle/linuxbundletest"
)

var (
	testLauncher = []byte("#!/bin/sh\nexec launcher\n")
	testDesktop  = []byte("[Desktop Entry]\nName=Notes\n")
)

// installedPlatform installs spec under a temp apps directory and
// returns the platform running from its executable.
func installedPlatform(t *testing.T, spec linuxbundletest.Spec) (*linuxPlatform, string) {
	t.Helper()
	apps := filepath.Join(t.TempDir(), "apps")
	dir := linuxbundletest.Install(t, apps, spec, testLauncher, testDesktop)
	return &linuxPlatform{exe: filepath.Join(dir, "bin", spec.ID)}, apps
}

func TestLinuxPlatformReadsTheInstallation(t *testing.T) {
	p, apps := installedPlatform(t, linuxbundletest.Default("1.0.0"))
	if got := p.Version(); got != "1.0.0" {
		t.Fatalf("Version = %q", got)
	}
	if got := p.Identifier(); got != "dev.gofastr.notes" {
		t.Fatalf("Identifier = %q", got)
	}
	name, dir, ok := p.Bundle()
	if !ok || name != "dev.gofastr.notes" || dir != apps {
		t.Fatalf("Bundle = %q %q %v", name, dir, ok)
	}
	units, ok := p.Units()
	if !ok || len(units) != 1 || units[0].Rel != name || units[0].Current != filepath.Join(apps, name) {
		t.Fatalf("Units = %+v %v", units, ok)
	}
}

func TestLinuxPlatformNeedsAnIntactInstallation(t *testing.T) {
	spec := linuxbundletest.Default("1.0.0")
	rewriteReceipt := func(t *testing.T, dir string, r linuxbundle.Receipt) {
		root, err := os.OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer root.Close()
		if err := root.Remove(linuxbundle.ReceiptName); err != nil {
			t.Fatal(err)
		}
		if err := linuxbundle.WriteReceipt(root, r); err != nil {
			t.Fatal(err)
		}
	}
	manifestOf := func(t *testing.T, dir string) []byte {
		raw, err := os.ReadFile(filepath.Join(dir, linuxbundle.ManifestName))
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	cases := []struct {
		name  string
		setup func(t *testing.T, dir string) string
		warns bool
	}{
		{"a plain executable", func(t *testing.T, dir string) string {
			exe := filepath.Join(t.TempDir(), spec.ID)
			if err := os.WriteFile(exe, linuxbundletest.ELF(runtime.GOARCH, ""), 0o700); err != nil {
				t.Fatal(err)
			}
			return exe
		}, false},
		{"an installation directory not named for its id", func(t *testing.T, dir string) string {
			moved := filepath.Join(filepath.Dir(dir), "dev.gofastr.renamed")
			if err := os.Rename(dir, moved); err != nil {
				t.Fatal(err)
			}
			return filepath.Join(moved, "bin", spec.ID)
		}, false},
		{"a receipt for another manifest", func(t *testing.T, dir string) string {
			rewriteReceipt(t, dir, linuxbundle.NewReceipt(spec.ID, []byte("{}"), testLauncher, testDesktop))
			return filepath.Join(dir, "bin", spec.ID)
		}, true},
		{"a receipt naming another app", func(t *testing.T, dir string) string {
			rewriteReceipt(t, dir, linuxbundle.NewReceipt("dev.gofastr.other", manifestOf(t, dir), testLauncher, testDesktop))
			return filepath.Join(dir, "bin", spec.ID)
		}, true},
		{"an apps directory others can write", func(t *testing.T, dir string) string {
			if err := os.Chmod(filepath.Dir(dir), 0o777); err != nil {
				t.Fatal(err)
			}
			return filepath.Join(dir, "bin", spec.ID)
		}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := linuxbundletest.Install(t, filepath.Join(t.TempDir(), "apps"), spec, testLauncher, testDesktop)
			var warned []string
			p := &linuxPlatform{exe: c.setup(t, dir), warn: func(m string) { warned = append(warned, m) }}
			if _, _, ok := p.Bundle(); ok {
				t.Fatal("Bundle reported an installation")
			}
			if p.Version() != "" || p.Identifier() != "" {
				t.Fatalf("identity %q %q from no installation", p.Version(), p.Identifier())
			}
			if c.warns != (len(warned) == 1) {
				t.Fatalf("warnings %q, want one: %v", warned, c.warns)
			}
			for _, w := range warned {
				if strings.Contains(w, dir) || strings.Contains(w, filepath.Dir(dir)) {
					t.Fatalf("the warning names a path: %q", w)
				}
			}
		})
	}
}

// A payload file changed after install does not keep the app from
// updating: the update replaces the whole directory with verified files.
func TestLinuxPlatformReadsAChangedInstallation(t *testing.T) {
	p, apps := installedPlatform(t, linuxbundletest.Default("1.0.0"))
	if err := os.WriteFile(filepath.Join(apps, "dev.gofastr.notes", "README.txt"), []byte("edited"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := p.Bundle(); !ok || p.Version() != "1.0.0" {
		t.Fatalf("Bundle ok=%v version %q", ok, p.Version())
	}
}

func TestLinuxExtractStagesAnInstallationUninstallAccepts(t *testing.T) {
	p, _ := installedPlatform(t, linuxbundletest.Default("1.0.0"))
	next := linuxbundletest.Default("1.2.0")
	stage := t.TempDir()
	prepared, err := p.Extract(linuxbundletest.TarGz(t, next, linuxbundletest.Entries(next)), stage)
	if err != nil {
		t.Fatal(err)
	}
	info, err := p.Inspect(prepared)
	if err != nil {
		t.Fatal(err)
	}
	if info != (BundleInfo{Identifier: "dev.gofastr.notes", ShortVersion: "1.2.0"}) {
		t.Fatalf("Inspect = %+v", info)
	}
	if err := p.VerifySignature(prepared); err != nil {
		t.Fatal(err)
	}
	b, receipt, err := linuxbundle.OpenInstalled(filepath.Join(prepared, next.ID), next.ID)
	if err != nil {
		t.Fatalf("the staged bundle is not an installation uninstall accepts: %v", err)
	}
	defer b.Close()
	if receipt.LauncherSHA256 != linuxbundle.Digest(testLauncher) || receipt.DesktopSHA256 != linuxbundle.Digest(testDesktop) {
		t.Fatal("the reissued receipt does not carry the running install's launcher and desktop entry digests")
	}
	if receipt.ManifestSHA256 != linuxbundle.Digest(b.RawManifest) {
		t.Fatal("the reissued receipt does not name the new manifest")
	}
}

func TestLinuxVerifyRefusesANameOrSchemeChange(t *testing.T) {
	for name, edit := range map[string]func(*linuxbundletest.Spec){
		"name":   func(s *linuxbundletest.Spec) { s.Name = "Notes Pro" },
		"scheme": func(s *linuxbundletest.Spec) { s.Scheme = "notes" },
	} {
		t.Run(name, func(t *testing.T) {
			p, _ := installedPlatform(t, linuxbundletest.Default("1.0.0"))
			next := linuxbundletest.Default("1.2.0")
			edit(&next)
			prepared, err := p.Extract(linuxbundletest.TarGz(t, next, linuxbundletest.Entries(next)), t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if err := p.VerifySignature(prepared); !errors.Is(err, ErrSignature) {
				t.Fatalf("VerifySignature = %v, want ErrSignature", err)
			}
		})
	}
}

func TestLinuxExtractRefusesABadBundle(t *testing.T) {
	other := "arm64"
	if runtime.GOARCH == "arm64" {
		other = "amd64"
	}
	cases := map[string]func() (linuxbundletest.Spec, []linuxbundletest.Entry){
		"another architecture": func() (linuxbundletest.Spec, []linuxbundletest.Entry) {
			s := linuxbundletest.Default("1.2.0")
			s.Arch = other
			return s, linuxbundletest.Entries(s)
		},
		"a file off its checksum": func() (linuxbundletest.Spec, []linuxbundletest.Entry) {
			s := linuxbundletest.Default("1.2.0")
			entries := linuxbundletest.Entries(s)
			for i := range entries {
				if entries[i].Name == "README.txt" {
					entries[i].Data = []byte("swapped")
				}
			}
			return s, entries
		},
		"an unlisted file": func() (linuxbundletest.Spec, []linuxbundletest.Entry) {
			s := linuxbundletest.Default("1.2.0")
			return s, append(linuxbundletest.Entries(s), linuxbundletest.Entry{Name: "extra.so", Mode: 0o644, Data: []byte("x")})
		},
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			p, _ := installedPlatform(t, linuxbundletest.Default("1.0.0"))
			s, entries := build()
			stage := t.TempDir()
			_, err := p.Extract(linuxbundletest.TarGz(t, s, entries), stage)
			if !errors.Is(err, ErrBadArchive) {
				t.Fatalf("Extract = %v, want ErrBadArchive", err)
			}
			if strings.Contains(err.Error(), stage) {
				t.Fatalf("the error names the staging path: %v", err)
			}
		})
	}
}

func TestLinuxInspectRefusesAnotherApp(t *testing.T) {
	p, _ := installedPlatform(t, linuxbundletest.Default("1.0.0"))
	next := linuxbundletest.Default("1.2.0")
	next.ID = "dev.gofastr.other"
	prepared, err := p.Extract(linuxbundletest.TarGz(t, next, linuxbundletest.Entries(next)), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Inspect(prepared); !errors.Is(err, ErrBadBundleInfo) {
		t.Fatalf("Inspect = %v, want ErrBadBundleInfo", err)
	}
}

func TestLinuxRelaunchNamesThePredecessor(t *testing.T) {
	t.Setenv(PredecessorEnv, "1")
	p, apps := installedPlatform(t, linuxbundletest.Default("1.0.0"))
	var gotExe string
	var gotEnv []string
	p.start = func(exe string, env []string) error { gotExe, gotEnv = exe, env; return nil }
	dir := filepath.Join(apps, "dev.gofastr.notes")
	if err := p.Relaunch(dir); err != nil {
		t.Fatal(err)
	}
	if gotExe != filepath.Join(dir, "bin", "dev.gofastr.notes") {
		t.Fatalf("relaunched %q", gotExe)
	}
	var named []string
	for _, kv := range gotEnv {
		if strings.HasPrefix(kv, PredecessorEnv+"=") {
			named = append(named, kv)
		}
	}
	if want := PredecessorEnv + "=" + strconv.Itoa(os.Getpid()); len(named) != 1 || named[0] != want {
		t.Fatalf("predecessor entries %q, want only %q", named, want)
	}
}
