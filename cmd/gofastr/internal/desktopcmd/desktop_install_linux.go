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
	"syscall"
	"time"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
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
	bundle, receipt, err := linuxbundle.OpenInstalled(filepath.Join(paths.apps, id), id)
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
			return err
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
	for _, entry := range bundle.Manifest.Files {
		if err = bundle.Root.Remove(entry.Path); err != nil {
			return fmt.Errorf("partial uninstall; retained remaining installation files: %w", err)
		}
	}
	for _, name := range []string{"manifest.json", linuxInstallReceiptName, "bin"} {
		if err = bundle.Root.Remove(name); err != nil {
			return err
		}
	}
	if err = apps.Remove(id); err != nil {
		return err
	}
	cli.Success("Uninstalled %s; application data and default-handler preferences were not changed", id)
	refreshLinuxDesktopDatabase(paths.applications)
	return nil
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

// Walk using root-relative handles, rejecting symlinks in the selected chain.
// Existing directory modes are never changed. This does not defend against a
// hostile process running as the same UID changing this user's files.
func openLinuxInstallDirectory(name string, create bool) (*os.Root, error) {
	if !filepath.IsAbs(name) {
		return nil, fmt.Errorf("installation path must be absolute")
	}
	root, err := os.OpenRoot("/")
	if err != nil {
		return nil, err
	}
	parts := strings.Split(strings.TrimPrefix(filepath.Clean(name), "/"), "/")
	for _, part := range parts {
		st, e := root.Lstat(part)
		if os.IsNotExist(e) && create {
			if e = root.Mkdir(part, 0700); e == nil {
				st, e = root.Lstat(part)
			}
		}
		if e != nil {
			root.Close()
			return nil, e
		}
		if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			root.Close()
			return nil, fmt.Errorf("refusing non-directory or symlink in installation path %q", name)
		}
		stat, ok := st.Sys().(*syscall.Stat_t)
		if !ok || !linuxInstallDirectoryPolicy(stat.Uid, st.Mode(), false) {
			root.Close()
			return nil, fmt.Errorf("refusing foreign-owned or writable ancestor in installation path %q", name)
		}
		next, e := root.OpenRoot(part)
		root.Close()
		if e != nil {
			return nil, e
		}
		root = next
	}
	st, err := root.Stat(".")
	if err != nil {
		root.Close()
		return nil, err
	}
	stat, ok := st.Sys().(*syscall.Stat_t)
	if !ok || !linuxInstallDirectoryPolicy(stat.Uid, st.Mode(), true) {
		root.Close()
		return nil, fmt.Errorf("installation directory %q must be owned by the current user and not writable by group/others", name)
	}
	return root, nil
}

// An owner can replace descendants even when mode is 0755. Every ancestor
// must therefore belong to this user or root. A trusted sticky shared root
// (such as /tmp) is allowed, but the final selected directory must be private
// from other writers and belong to this user.
func linuxInstallDirectoryPolicy(owner uint32, mode fs.FileMode, leaf bool) bool {
	uid := uint32(os.Geteuid())
	if leaf {
		return owner == uid && mode.Perm()&0022 == 0
	}
	return (owner == uid || owner == 0) && (mode.Perm()&0022 == 0 || mode&os.ModeSticky != 0)
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
