package desktopcmd

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/internal/filelock"
	"github.com/DonaldMurillo/gofastr/internal/linuxbundle"
)

const linuxInstallReceiptName = linuxbundle.ReceiptName

type linuxInstallPaths struct{ data, bin, apps, applications string }

// Install only a verified fixed-format bundle. Checksums are integrity checks,
// not authenticity: the user must trust the executable's publisher separately.
func installLinuxBundle(source string) error {
	bundle, err := openLinuxInstallBundle(source, false, true)
	if err != nil {
		return err
	}
	defer bundle.Root.Close()
	paths, err := linuxUserInstallPaths()
	if err != nil {
		return err
	}
	return installVerifiedLinuxBundle(bundle, paths)
}
func installVerifiedLinuxBundle(bundle *linuxbundle.Bundle, paths linuxInstallPaths) (err error) {
	desktopEntry, err := linuxInstalledDesktopEntry(bundle.Manifest, filepath.Join(paths.apps, bundle.Manifest.ID), filepath.Join(paths.bin, bundle.Manifest.ID))
	if err != nil {
		return err
	}
	launcher := linuxbundle.Launcher(filepath.Join(paths.apps, bundle.Manifest.ID, "bin", bundle.Manifest.ID))
	apps, err := openLinuxInstallDirectory(paths.apps, true)
	if err != nil {
		return err
	}
	defer apps.Close()
	applications, err := openLinuxInstallDirectory(paths.applications, true)
	if err != nil {
		return err
	}
	defer applications.Close()
	bin, err := openLinuxInstallDirectory(paths.bin, true)
	if err != nil {
		return err
	}
	defer bin.Close()
	id := bundle.Manifest.ID
	for _, target := range []struct {
		root *os.Root
		name string
	}{{apps, id}, {applications, id + ".desktop"}, {bin, id}} {
		if _, e := target.root.Lstat(target.name); !os.IsNotExist(e) {
			if e != nil {
				return e
			}
			return fmt.Errorf("installation target %q already exists; refusing replacement (uninstall the unchanged installation first)", target.name)
		}
	}
	// Mkdir and O_EXCL reserve new destinations; rollback touches only objects
	// created by this attempt. This is not a cross-directory crash transaction.
	if err = apps.Mkdir(id, 0700); err != nil {
		return err
	}
	installed, err := apps.OpenRoot(id)
	if err != nil {
		_ = apps.Remove(id)
		return err
	}
	defer installed.Close()
	var created []string
	launcherCreated, desktopCreated, committed := false, false, false
	defer func() {
		if committed {
			return
		}
		if desktopCreated {
			err = errors.Join(err, applications.Remove(id+".desktop"))
		}
		if launcherCreated {
			err = errors.Join(err, bin.Remove(id))
		}
		for i := len(created) - 1; i >= 0; i-- {
			err = errors.Join(err, installed.Remove(created[i]))
		}
		err = errors.Join(err, apps.Remove(id))
	}()
	created, err = linuxbundle.Populate(bundle, installed)
	if err != nil {
		return err
	}
	if err = linuxbundle.WriteFile(bin, id, launcher, 0700); err != nil {
		return err
	}
	launcherCreated = true
	if err = linuxbundle.WriteFile(applications, id+".desktop", desktopEntry, 0600); err != nil {
		return err
	}
	desktopCreated = true
	if err = linuxbundle.WriteReceipt(installed, linuxbundle.NewReceipt(id, bundle.RawManifest, launcher, desktopEntry)); err != nil {
		return err
	}
	created = append(created, linuxInstallReceiptName)
	committed = true
	cli.Success("Installed %s for the current user", id)
	cli.Info("Launcher: %q; desktop entry: %q", filepath.Join(paths.bin, id), filepath.Join(paths.applications, id+".desktop"))
	if bundle.Manifest.EmbeddedScheme != "" {
		cli.Info("Advertised URL handler for %s; default associations are unchanged", bundle.Manifest.EmbeddedScheme)
	}
	refreshLinuxDesktopDatabase(paths.applications)
	return nil
}

func uninstallLinuxBundle(id string) error {
	if err := validateDesktopID(id); err != nil {
		return err
	}
	paths, err := linuxUserInstallPaths()
	if err != nil {
		return err
	}
	apps, err := openLinuxInstallDirectory(paths.apps, false)
	if err != nil {
		return err
	}
	defer apps.Close()
	applications, err := openLinuxInstallDirectory(paths.applications, false)
	if err != nil {
		return err
	}
	defer applications.Close()
	bin, err := openLinuxInstallDirectory(paths.bin, false)
	if err != nil {
		return err
	}
	defer bin.Close()
	// An update of the app holds this lock while it swaps the installation;
	// uninstall holds it until every file is gone, so neither runs while
	// the other is part way through.
	lock, err := apps.OpenFile(linuxbundle.LockName(id), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if held, err := filelock.TryLock(lock); err != nil {
		return err
	} else if !held {
		return fmt.Errorf("%s is installing an update; run uninstall again once it has restarted", id)
	}
	name := id
	bundle, receipt, err := linuxbundle.OpenInstalled(filepath.Join(paths.apps, id), id)
	if _, lerr := apps.Lstat(id); err != nil && errors.Is(lerr, fs.ErrNotExist) {
		// An update stopped between moving the installation aside and moving
		// its replacement in leaves only <id>~old: uninstall that, when it is
		// an unchanged installation the external files below also vouch for.
		old := id + linuxbundle.SideSep + "old"
		if b, r, oerr := linuxbundle.OpenInstalled(filepath.Join(paths.apps, old), id); oerr == nil {
			bundle, receipt, err, name = b, r, nil, old
			cli.Info("Found %s only as %s, left by an interrupted update; uninstalling that", id, old)
		}
	}
	if err != nil {
		return fmt.Errorf("refusing uninstall; installation is missing or changed: %w", err)
	}
	defer bundle.Close()
	for _, target := range []struct {
		root *os.Root
		name string
		mode fs.FileMode
	}{{bin, id, 0700}, {applications, id + ".desktop", 0600}} {
		if err = linuxbundle.VerifyInstalledMode(target.root, target.name, target.mode); err != nil {
			return fmt.Errorf("refusing uninstall: %w", err)
		}
	}
	launcher, err := linuxbundle.ReadFile(bin, id, 64<<10)
	if err != nil {
		return fmt.Errorf("refusing uninstall: launcher is missing or changed: %w", err)
	}
	desktopEntry, err := linuxbundle.ReadFile(applications, id+".desktop", 64<<10)
	if err != nil {
		return fmt.Errorf("refusing uninstall: desktop entry is missing or changed: %w", err)
	}
	if receipt.LauncherSHA256 != linuxInstallDigest(launcher) || receipt.DesktopSHA256 != linuxInstallDigest(desktopEntry) {
		return fmt.Errorf("refusing uninstall: launcher or desktop entry was modified; all files were retained")
	}
	// Every payload and both external files have been checked before deletion.
	// Remove only the fixed inventory: never RemoveAll an installation directory.
	if err = applications.Remove(id + ".desktop"); err != nil {
		return err
	}
	if err = bin.Remove(id); err != nil {
		return errors.Join(err, linuxbundle.WriteFile(applications, id+".desktop", desktopEntry, 0600))
	}
	if err = removeLinuxInstallation(apps, name, bundle); err != nil {
		return err
	}
	removeLinuxUpdateLeftovers(apps, filepath.Join(paths.apps, id), id)
	_ = apps.Remove(linuxbundle.LockName(id))
	cli.Success("Uninstalled %s; application data and default-handler preferences were not changed", id)
	refreshLinuxDesktopDatabase(paths.applications)
	return nil
}

// removeLinuxInstallation removes an opened installation's fixed
// inventory, then its directory under apps; never RemoveAll.
func removeLinuxInstallation(apps *os.Root, name string, bundle *linuxbundle.Bundle) error {
	for _, entry := range bundle.Manifest.Files {
		if err := bundle.Root.Remove(entry.Path); err != nil {
			return fmt.Errorf("partial uninstall; retained remaining installation files: %w", err)
		}
	}
	for _, entry := range []string{linuxbundle.ManifestName, linuxInstallReceiptName, "bin"} {
		if err := bundle.Root.Remove(entry); err != nil {
			return err
		}
	}
	return apps.Remove(name)
}

// removeLinuxUpdateLeftovers removes the copies an update keeps beside
// the installation at dir, <id>~old and <id>~new, each only when it is
// an installation of id; anything else there is reported and kept.
func removeLinuxUpdateLeftovers(apps *os.Root, dir, id string) {
	for _, side := range []string{"old", "new"} {
		name := id + linuxbundle.SideSep + side
		if _, err := apps.Lstat(name); err != nil {
			continue
		}
		bundle, _, err := linuxbundle.OpenInstalled(dir+linuxbundle.SideSep+side, id)
		if err != nil {
			cli.Warn("Kept %s beside the installation: it is not an unchanged installation of %s (%v)", name, id, err)
			continue
		}
		err = removeLinuxInstallation(apps, name, bundle)
		_ = bundle.Close()
		if err != nil {
			cli.Warn("Could not remove the previous version kept as %s: %v", name, err)
		}
	}
}

func linuxUserInstallPaths() (linuxInstallPaths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return linuxInstallPaths{}, err
	}
	data := os.Getenv("XDG_DATA_HOME")
	// The XDG specification says relative values are invalid and ignored.
	if data == "" || !filepath.IsAbs(data) {
		data = filepath.Join(home, ".local", "share")
	}
	for _, value := range []string{home, data} {
		if !filepath.IsAbs(value) || !textsafe.Storable(value) || strings.ContainsAny(value, "=\n\r\t") {
			return linuxInstallPaths{}, fmt.Errorf("HOME and XDG_DATA_HOME must resolve to absolute UTF-8 paths without '=', NUL, or line/control separators")
		}
		for _, b := range []byte(value) {
			if b < 32 || b == 127 {
				return linuxInstallPaths{}, fmt.Errorf("installation paths must not contain control bytes")
			}
		}
	}
	data = filepath.Clean(data)
	return linuxInstallPaths{data: data, bin: filepath.Join(home, ".local", "bin"), apps: filepath.Join(data, "gofastr", "apps"), applications: filepath.Join(data, "applications")}, nil
}

func openLinuxInstallDirectory(name string, create bool) (*os.Root, error) {
	return linuxbundle.OpenDirectory(name, create)
}

func linuxInstallDirectoryPolicy(owner uint32, mode fs.FileMode, leaf bool) bool {
	return linuxbundle.DirectoryPolicy(owner, mode, leaf)
}

func openLinuxInstallBundle(name string, installed, requireHost bool) (*linuxbundle.Bundle, error) {
	return linuxbundle.Open(name, installed, requireHost)
}
func linuxInstallDigest(data []byte) string { return linuxbundle.Digest(data) }
func linuxInstalledDesktopEntry(manifest linuxBundleManifest, root, launcher string) ([]byte, error) {
	return linuxbundle.DesktopEntry(manifest, root, launcher)
}
func refreshLinuxDesktopDatabase(applications string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "update-desktop-database", applications)
	cmd.WaitDelay = time.Second
	if err := cmd.Run(); err != nil {
		cli.Warn("Desktop entry changed, but MIME cache refresh did not complete. Run update-desktop-database %s after installing desktop-file-utils; default handlers were not changed.", linuxbundle.ShellQuote(applications))
	}
}
