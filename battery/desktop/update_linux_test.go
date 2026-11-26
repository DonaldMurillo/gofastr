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
	"github.com/DonaldMurillo/gofastr/internal/filelock"
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
	old, _, err := linuxbundle.OpenInstalled(dir+update.SideSep+"old", current.ID)
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
	if _, err := os.Lstat(dir + update.SideSep + "old"); !errors.Is(err, fs.ErrNotExist) {
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

// An installed app whose id is another's id plus ".old" or ".new" is a
// separate app: the other's launch cleanup and update leave it alone.
func TestLinuxUpdateLeavesOtherAppsAlone(t *testing.T) {
	for _, suffix := range []string{".old", ".new"} {
		t.Run(suffix, func(t *testing.T) {
			current, next, other := linuxbundletest.Default("1.0.0"), linuxbundletest.Default("1.2.0"), linuxbundletest.Default("1.0.0")
			other.ID = current.ID + suffix
			b, _, _ := installFixture(t, "1.2.0", linuxbundletest.TarGz(t, next, linuxbundletest.Entries(next)))
			apps := filepath.Join(t.TempDir(), "apps")
			dir := linuxbundletest.Install(t, apps, current, []byte("l"), []byte("d"))
			otherDir := linuxbundletest.Install(t, apps, other, []byte("l2"), []byte("d2"))
			b.updaterFor().platform = update.NewInstalledPlatform(filepath.Join(dir, "bin", current.ID), func(string, []string) error { return nil })
			b.updaterFor().removeStaleOld()
			if err := b.InstallUpdate(context.Background()); err != nil {
				t.Fatal(err)
			}
			b.updaterFor().removeStaleOld()
			o, _, err := linuxbundle.OpenInstalled(otherDir, other.ID)
			if err != nil {
				t.Fatalf("%s's cleanup or update removed or damaged the separate app %s: %v", current.ID, other.ID, err)
			}
			_ = o.Close()
		})
	}
}

// Two running instances of one installed app: the second to install
// finds the installation is no longer the one it started from and
// refuses, instead of installing the same release again and starting a
// second copy of it.
func TestLinuxSecondInstanceDoesNotReinstall(t *testing.T) {
	current, next := linuxbundletest.Default("1.0.0"), linuxbundletest.Default("1.2.0")
	archive := linuxbundletest.TarGz(t, next, linuxbundletest.Entries(next))
	first, _, _ := installFixture(t, "1.2.0", archive)
	second, _, _ := installFixture(t, "1.2.0", archive)
	second.dataDir = first.dataDir
	dir := linuxbundletest.Install(t, filepath.Join(t.TempDir(), "apps"), current, []byte("l"), []byte("d"))
	exe := filepath.Join(dir, "bin", current.ID)
	var mu sync.Mutex
	starts := 0
	start := func(string, []string) error { mu.Lock(); starts++; mu.Unlock(); return nil }
	first.updaterFor().platform = update.NewInstalledPlatform(exe, start)
	second.updaterFor().platform = update.NewInstalledPlatform(exe, start)
	if v := second.updaterFor().platformOrDefault().Version(); v != "1.0.0" {
		t.Fatalf("second instance runs %q", v)
	}
	if err := first.InstallUpdate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := second.InstallUpdate(context.Background()); !errors.Is(err, errUpdateRestart) {
		t.Fatalf("second instance's install = %v, want %v", err, errUpdateRestart)
	}
	if starts != 1 {
		t.Fatalf("the update started %d new instances, want 1", starts)
	}
}

// An install another process holds the lock for is refused at once.
func TestInstallRefusedWhileAnotherProcessInstalls(t *testing.T) {
	next := linuxbundletest.Default("1.2.0")
	b, _, _ := installFixture(t, "1.2.0", linuxbundletest.TarGz(t, next, linuxbundletest.Entries(next)))
	dir := linuxbundletest.Install(t, filepath.Join(t.TempDir(), "apps"), linuxbundletest.Default("1.0.0"), []byte("l"), []byte("d"))
	b.updaterFor().platform = update.NewInstalledPlatform(filepath.Join(dir, "bin", next.ID), func(string, []string) error {
		t.Error("relaunched while another process held the install lock")
		return nil
	})
	lockPath := filepath.Join(filepath.Dir(dir), linuxbundle.LockName(next.ID))
	held, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	if ok, err := filelock.TryLock(held); err != nil || !ok {
		t.Fatalf("TryLock = %v, %v", ok, err)
	}
	other, err := os.OpenFile(lockPath, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if ok, _ := filelock.TryLock(other); ok {
		t.Skip("this system's flock does not exclude a second open file in one process")
	}
	if err := b.InstallUpdate(context.Background()); !errors.Is(err, errUpdateInProgress) {
		t.Fatalf("InstallUpdate = %v, want %v", err, errUpdateInProgress)
	}
}

// Launch cleanup takes the install lock: while another process holds it
// (part way through an install, its rollback needing the copy it moved
// aside) the leftover stays, and the next launch with the lock free
// removes it.
func TestLinuxStaleCleanupWaitsForTheInstallLock(t *testing.T) {
	current := linuxbundletest.Default("1.0.0")
	b, _, _ := installFixture(t, "1.2.0", nil)
	apps := filepath.Join(t.TempDir(), "apps")
	dir := linuxbundletest.Install(t, apps, current, []byte("l"), []byte("d"))
	if err := os.MkdirAll(filepath.Join(dir+update.SideSep+"old", "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	b.updaterFor().platform = update.NewInstalledPlatform(filepath.Join(dir, "bin", current.ID), nil)
	held, err := os.OpenFile(filepath.Join(apps, linuxbundle.LockName(current.ID)), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := filelock.TryLock(held); err != nil || !ok {
		t.Fatalf("TryLock = %v, %v", ok, err)
	}
	b.updaterFor().removeStaleOld()
	if _, err := os.Lstat(dir + update.SideSep + "old"); err != nil {
		t.Fatalf("cleanup removed the moved-aside copy while another process held the install lock: %v", err)
	}
	_ = held.Close()
	b.updaterFor().removeStaleOld()
	if _, err := os.Lstat(dir + update.SideSep + "old"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("cleanup with the lock free kept the leftover: %v", err)
	}
}
