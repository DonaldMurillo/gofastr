//go:build linux

package desktop

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/update"
	"github.com/DonaldMurillo/gofastr/internal/linuxbundle"
	"github.com/DonaldMurillo/gofastr/internal/linuxbundle/linuxbundletest"
)

// The install step on the real Linux platform: an installation
// `gofastr desktop install` made under apps/<id> is replaced as one
// directory by the release archive's installed form, and what is left
// in place is an installation uninstall accepts.

func TestLinuxInstallSwapsTheInstallation(t *testing.T) {
	current, next := linuxbundletest.Default("1.0.0"), linuxbundletest.Default("1.2.0")
	b, _, _ := installFixture(t, "1.2.0", linuxbundletest.TarGz(t, next, linuxbundletest.Entries(next)))
	apps := filepath.Join(t.TempDir(), "apps")
	launcher, desktopEntry := []byte("launcher\n"), []byte("desktop entry\n")
	dir := linuxbundletest.Install(t, apps, current, launcher, desktopEntry)

	var mu sync.Mutex
	var started []string
	b.updaterFor().platform = update.NewInstalledPlatform(filepath.Join(dir, "bin", current.ID), func(exe string, env []string) error {
		mu.Lock()
		defer mu.Unlock()
		started = append(started, exe)
		return nil
	})

	if err := b.InstallUpdate(context.Background()); err != nil {
		t.Fatal(err)
	}
	installed, receipt, err := linuxbundle.OpenInstalled(dir, current.ID)
	if err != nil {
		t.Fatalf("the swapped-in directory is not an installation uninstall accepts: %v", err)
	}
	version := installed.Manifest.Version
	_ = installed.Close()
	if version != "1.2.0" {
		t.Fatalf("installed version %q, want 1.2.0", version)
	}
	if receipt.LauncherSHA256 != linuxbundle.Digest(launcher) || receipt.DesktopSHA256 != linuxbundle.Digest(desktopEntry) {
		t.Fatal("the receipt no longer vouches for the launcher and desktop entry install wrote")
	}
	old, _, err := linuxbundle.OpenInstalled(dir+".old", current.ID)
	if err != nil {
		t.Fatalf("the previous installation was not kept beside it: %v", err)
	}
	_ = old.Close()
	mu.Lock()
	got := started
	mu.Unlock()
	if want := filepath.Join(dir, "bin", current.ID); len(got) != 1 || got[0] != want {
		t.Fatalf("relaunched %q, want %q", got, want)
	}

	b.updaterFor().removeStaleOld()
	if _, err := os.Lstat(dir + ".old"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the previous installation survived the next launch's cleanup: %v", err)
	}
	if _, _, err := linuxbundle.OpenInstalled(dir, current.ID); err != nil {
		t.Fatalf("cleanup disturbed the installation: %v", err)
	}
}

func TestLinuxInstallRefusesARenamedApp(t *testing.T) {
	current, next := linuxbundletest.Default("1.0.0"), linuxbundletest.Default("1.2.0")
	next.Name = "Notes Pro"
	b, _, _ := installFixture(t, "1.2.0", linuxbundletest.TarGz(t, next, linuxbundletest.Entries(next)))
	dir := linuxbundletest.Install(t, filepath.Join(t.TempDir(), "apps"), current, []byte("l"), []byte("d"))
	b.updaterFor().platform = update.NewInstalledPlatform(filepath.Join(dir, "bin", current.ID), func(string, []string) error {
		t.Error("relaunched after a refused update")
		return nil
	})
	if err := b.InstallUpdate(context.Background()); !errors.Is(err, errUpdateArchive) {
		t.Fatalf("InstallUpdate = %v, want the archive error", err)
	}
	installed, _, err := linuxbundle.OpenInstalled(dir, current.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer installed.Close()
	if installed.Manifest.Version != "1.0.0" {
		t.Fatalf("installed version %q after a refused update", installed.Manifest.Version)
	}
	assertNoStaging(t, b)
}
