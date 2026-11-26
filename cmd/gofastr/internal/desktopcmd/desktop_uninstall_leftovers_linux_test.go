package desktopcmd

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/filelock"
	"github.com/DonaldMurillo/gofastr/internal/linuxbundle"
)

// An update keeps the replaced installation beside the new one as
// <id>~old until the app's next launch, and a crash between its steps
// can leave <id>~new. Uninstall removes either when it is an
// installation of the same app, and leaves anything else in place.
func TestLinuxUninstallClearsUpdateLeftovers(t *testing.T) {
	paths := linuxInstallTestHome(t)
	source, manifest := linuxInstallFixture(t, "")
	if err := installLinuxBundle(source); err != nil {
		t.Fatal(err)
	}
	installed := filepath.Join(paths.apps, manifest.ID)
	old := installed + "~old"
	if out, err := exec.Command("cp", "-a", installed, old).CombinedOutput(); err != nil {
		t.Fatalf("cp: %v\n%s", err, out)
	}
	stray := installed + "~new"
	if err := os.Mkdir(stray, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stray, "notes.txt"), []byte("not an installation"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := uninstallLinuxBundle(manifest.ID); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{installed, old} {
		if _, err := os.Lstat(p); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s survived uninstall: %v", filepath.Base(p), err)
		}
	}
	if _, err := os.Stat(filepath.Join(stray, "notes.txt")); err != nil {
		t.Fatalf("uninstall removed a directory that is not an installation: %v", err)
	}
}

// Uninstall refuses, and keeps every file, when the launcher or the
// desktop entry install wrote no longer has the mode install gave it.
func TestLinuxUninstallRefusesAChangedLauncherOrEntryMode(t *testing.T) {
	for _, target := range []string{"launcher", "desktop entry"} {
		t.Run(target, func(t *testing.T) {
			paths := linuxInstallTestHome(t)
			source, manifest := linuxInstallFixture(t, "")
			if err := installLinuxBundle(source); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(paths.bin, manifest.ID)
			if target == "desktop entry" {
				path = filepath.Join(paths.applications, manifest.ID+".desktop")
			}
			if err := os.Chmod(path, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := uninstallLinuxBundle(manifest.ID); err == nil {
				t.Fatal("uninstall accepted a launcher or desktop entry with a changed mode")
			}
			assertInstallationKept(t, paths, manifest.ID)
		})
	}
}

// assertInstallationKept fails unless the receipt, the launcher and the
// desktop entry of app id are all still in place.
func assertInstallationKept(t *testing.T, paths linuxInstallPaths, id string) {
	t.Helper()
	for _, p := range []string{
		filepath.Join(paths.apps, id, linuxInstallReceiptName),
		filepath.Join(paths.bin, id),
		filepath.Join(paths.applications, id+".desktop"),
	} {
		if _, err := os.Lstat(p); err != nil {
			t.Fatalf("a refused uninstall removed %s: %v", filepath.Base(p), err)
		}
	}
}

// Uninstall and an update of the app hold one lock beside the
// installation: while an update holds it, uninstall refuses before it
// removes anything, and once it is free uninstall runs and takes the
// lock file with it.
func TestLinuxUninstallWaitsForAnUpdate(t *testing.T) {
	paths := linuxInstallTestHome(t)
	source, manifest := linuxInstallFixture(t, "")
	if err := installLinuxBundle(source); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(paths.apps, linuxbundle.LockName(manifest.ID))
	held, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := filelock.TryLock(held); err != nil || !ok {
		t.Fatalf("TryLock = %v, %v", ok, err)
	}
	if err := uninstallLinuxBundle(manifest.ID); err == nil || !strings.Contains(err.Error(), "installing an update") {
		t.Fatalf("uninstall during an update = %v, want the retry message", err)
	}
	assertInstallationKept(t, paths, manifest.ID)
	_ = held.Close()
	if err := uninstallLinuxBundle(manifest.ID); err != nil {
		t.Fatal(err)
	}
	if left, _ := os.ReadDir(paths.apps); len(left) != 0 {
		t.Fatalf("uninstall left %d entries in the apps directory", len(left))
	}
}

// An update that stopped after moving the installation aside and before
// moving its replacement in leaves only <id>~old; uninstall removes that.
func TestLinuxUninstallAfterAnInterruptedUpdate(t *testing.T) {
	paths := linuxInstallTestHome(t)
	source, manifest := linuxInstallFixture(t, "")
	if err := installLinuxBundle(source); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(paths.apps, manifest.ID)
	if err := os.Rename(dir, dir+"~old"); err != nil {
		t.Fatal(err)
	}
	if err := uninstallLinuxBundle(manifest.ID); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{dir + "~old", filepath.Join(paths.bin, manifest.ID), filepath.Join(paths.applications, manifest.ID+".desktop")} {
		if _, err := os.Lstat(p); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s survived uninstall: %v", filepath.Base(p), err)
		}
	}
}
