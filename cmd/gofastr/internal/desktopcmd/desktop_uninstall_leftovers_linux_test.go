package desktopcmd

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
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
			if _, err := os.Stat(filepath.Join(paths.apps, manifest.ID, linuxInstallReceiptName)); err != nil {
				t.Fatalf("a refused uninstall removed files: %v", err)
			}
		})
	}
}
