package desktopcmd

import (
	"bytes"
	"fmt"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/internal/linuxbundle"
)

type linuxArchiveEntry = linuxbundle.Entry

type linuxBundleFile = linuxbundle.File

type linuxBundleManifest = linuxbundle.Manifest

func linuxBundleRoot(f desktopBuildFlags, name, arch string) (string, error) {
	if _, err := linuxDesktopLinkerFlags(f.scheme); err != nil {
		return "", err
	}
	if err := validateDesktopID(f.id); err != nil {
		return "", err
	}
	if err := validateDesktopName(name); err != nil {
		return "", err
	}
	if !utf8.ValidString(name) {
		return "", fmt.Errorf("--name must be valid UTF-8 for a desktop entry")
	}
	if !linuxbundle.ValidVersion(f.version) {
		return "", fmt.Errorf("--version must match %s and be at most 64 characters", desktopVersionRe.String())
	}
	if err := linuxbundle.CheckIdentity(linuxbundle.Manifest{ID: f.id, Name: name, Version: f.version, Platform: "linux-" + arch, EmbeddedScheme: f.scheme}); err != nil {
		return "", err
	}
	return linuxbundle.RootName(f.id, f.version, arch)
}

func linuxUpdateVersionWarning(version string) string {
	if desktop.ValidateAppVersion(version) == nil {
		return ""
	}
	return fmt.Sprintf("--version %q is not major.minor.patch: the archive installs, but it cannot be published to an update feed, and the app it installs cannot update itself. Use a version such as 1.2.0.", version)
}

func buildLinuxDesktopArchive(f desktopBuildFlags, name, arch string) error {
	ldflags, err := linuxDesktopLinkerFlags(f.scheme)
	if err != nil {
		return err
	}
	extra, err := extraLinkerFlags(f)
	if err != nil {
		return err
	}
	ldflags += extra
	bundleRoot, err := linuxBundleRoot(f, name, arch)
	if err != nil {
		return err
	}
	if warning := linuxUpdateVersionWarning(f.version); warning != "" {
		cli.Warn("%s", warning)
	}
	icon, err := linuxBundleIcon(f.icon)
	if err != nil {
		return err
	}
	if strings.ContainsRune(f.out, 0) {
		return fmt.Errorf("invalid output directory")
	}
	//gofastr:allow(worldreadable) dist holds public release artifacts, not state or secrets
	if err = os.MkdirAll(f.out, 0755); err != nil {
		return err
	}
	output, err := os.OpenRoot(f.out)
	if err != nil {
		return err
	}
	defer output.Close()
	destination := bundleRoot + ".tar.gz"
	if st, e := output.Lstat(destination); e == nil && !st.Mode().IsRegular() {
		return fmt.Errorf("refusing non-regular archive destination %s", destination)
	} else if e != nil && !os.IsNotExist(e) {
		return e
	}
	stage, err := os.MkdirTemp(f.out, ".linux-desktop-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	executable := filepath.Join(stage, "app")
	cli.Info("Building %s (linux/%s, CGO_ENABLED=0)...", f.pkg, arch)
	cmd := exec.Command("go", "build", "-trimpath", "-ldflags", ldflags, "-o", executable, f.pkg)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH="+arch)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err = cmd.Run(); err != nil {
		return fmt.Errorf("build: %w", err)
	}
	entries, err := linuxBundleEntries(f, name, arch, executable, icon)
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(f.out, ".linux-archive-*.tar.gz")
	if err != nil {
		return err
	}
	tempName := filepath.Base(temp.Name())
	defer output.Remove(tempName)
	if err = writeLinuxArchive(temp, bundleRoot, entries); err != nil {
		_ = temp.Close()
		return err
	}
	//gofastr:allow(worldreadable) the compressed release artifact is intentionally public
	if err = temp.Chmod(0644); err != nil {
		_ = temp.Close()
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	if err = output.Rename(tempName, destination); err != nil {
		return err
	}
	cli.Success("Wrote %s", filepath.Join(f.out, destination))
	cli.Info("Artifact only: extract and run launch.sh; desktop entry template is not installed. GTK 3 and WebKitGTK 4.1 remain system dependencies.")
	return nil
}

func linuxBundleIcon(filename string) ([]byte, error) {
	var data []byte
	if filename == "" {
		data, _ = loadIconSource("")
	} else {
		info, err := os.Stat(filename)
		if err != nil {
			return nil, fmt.Errorf("read --icon: %w", err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("--icon must be a regular PNG file")
		}
		f, err := os.Open(filename)
		if err != nil {
			return nil, fmt.Errorf("read --icon: %w", err)
		}
		defer f.Close()
		st, err := f.Stat()
		if err != nil {
			return nil, err
		}
		if !st.Mode().IsRegular() {
			return nil, fmt.Errorf("--icon must be a regular PNG file")
		}
		const maxIcon = 16 << 20
		data, err = io.ReadAll(io.LimitReader(f, maxIcon+1))
		if err != nil {
			return nil, err
		}
		if len(data) > maxIcon {
			return nil, fmt.Errorf("--icon exceeds 16 MiB")
		}
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode --icon PNG: %w", err)
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > 4096 || config.Height > 4096 {
		return nil, fmt.Errorf("--icon dimensions must be between 1 and 4096 pixels")
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode --icon PNG: %w", err)
	}
	var out bytes.Buffer
	if err = png.Encode(&out, img); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func linuxBundleEntries(f desktopBuildFlags, name, arch, executable string, icon []byte) ([]linuxArchiveEntry, error) {
	if _, err := linuxBundleRoot(f, name, arch); err != nil {
		return nil, err
	}
	launcher := "#!/bin/sh\nset -eu\ncase $0 in\n  */*) launcher=$0 ;;\n  *) launcher=$(command -v -- \"$0\") ;;\nesac\ncase $launcher in\n  */*) bundle_dir=${launcher%/*} ;;\n  *) bundle_dir=. ;;\nesac\nexec \"$bundle_dir/bin/" + f.id + "\" \"$@\"\n"
	desktopEntry := "# TEMPLATE ONLY. Not an installed launcher. See README.txt.\n[Desktop Entry]\nType=Application\nVersion=1.0\nName=" + escapeDesktopEntryString(name) + "\nExec=\"@BUNDLE_ROOT@/launch.sh\"\nIcon=@BUNDLE_ROOT@/icon.png\nTerminal=false\n"
	readme := `Linux desktop release artifacts
-------------------------------
Extract the whole archive into one directory and run ./launch.sh there.
The launcher may also be invoked by its absolute or relative path. Keep it
beside bin/; do not move it alone or invoke it through a symlink elsewhere.
Arguments are passed through unchanged. The app uses its normal data dir.

Runtime prerequisites (not bundled): Linux amd64 or arm64 as named in
manifest.json, glibc >= 2.34, GTK 3, WebKitGTK 4.1 >= 2.40, and an X11 or
Wayland graphical session. Optional system notifications need libnotify
and a notification daemon. Install runtime packages using your distribution;
no C compiler or development headers are needed for the GoFastr shell.

The .desktop.in file is an INERT TEMPLATE, not a usable desktop entry.
It contains @BUNDLE_ROOT@ placeholders. For current-user installation, run
'gofastr desktop install --bundle=<extracted-directory>'. The CLI validates
this fixed inventory, ELF architecture, image and checksums, then writes its
own real desktop entry and a ~/.local/bin/<app-id> launcher. It never executes
bundle scripts during installation. Trust the publisher separately: checksums
are not signatures. Install refuses existing destinations; remove an unchanged
installation with 'gofastr desktop uninstall --id=<app-id>' before replacing it.
Uninstall preserves application data and refuses changed or unlisted files.
The install commands do not set default URL handlers or enable autostart.

For a different packaging system, render absolute installed paths using the
Desktop Entry Specification's string escaping AND Exec argument quoting
(two separate escaping layers, including literal percent as %% in Exec).
Do not substitute paths with a naive sed command. Exec executable paths
containing '=' are not supported by the specification. Icon paths use only
the string escaping layer, not Exec quoting. Never mark the template as
trusted or install it without rendering and validating it first.

This build writes release artifacts only: no menu installation, default
protocol association, scheme registration or signing. An app installed
with 'gofastr desktop install' whose Config.Update names a signed feed
can update itself: the scheduled check sends the page update_available
when the feed offers a newer archive, and the app installs it by
calling InstallUpdate (the page's updates.install). 'gofastr desktop
feed --platform linux-<arch>' signs the feed; its version must be
major.minor.patch.
The archive contains no GTK/WebKit/shared-library copies.
manifest.json inventories the payload files with SHA-256 checksums; these
checksums detect changes, not publisher authenticity. It does not inventory
itself. Preserve your application's own licenses and distribution notices.
Archive ordering, permissions and timestamps are deterministic; identical
payload bytes and metadata produce an identical tar.gz. Go toolchain,
source/module versions, build settings and VCS metadata still affect the
compiled executable, so pin those separately for reproducible builds.
`
	if f.scheme != "" {
		readme += "\nThis executable embeds the URL scheme " + f.scheme + ". Invoke ./launch.sh '" + f.scheme + "://route'\n" +
			"to deliver a link to this AppID's running instance on the SAME session bus.\n" +
			"URL activation requires an existing local Unix D-Bus session bus through\n" +
			"DBUS_SESSION_BUS_ADDRESS; missing, TCP, and autolaunch buses are refused.\n" +
			"No browser/OS protocol association or D-Bus activation service is installed.\n" +
			"If the bus disconnects, the app stays open but URL activation stops until\n" +
			"restart. A later different bus may admit a new primary while that app remains\n" +
			"open. This is bus-scoped delivery, not a cross-session/global process lock.\n"
	}
	entries := []linuxArchiveEntry{
		{Name: "README.txt", Mode: 0644, Data: []byte(readme)},
		{Name: "bin/" + f.id, Mode: 0755, Source: executable},
		{Name: f.id + ".desktop.in", Mode: 0644, Data: []byte(desktopEntry)},
		{Name: "icon.png", Mode: 0644, Data: icon},
		{Name: "launch.sh", Mode: 0755, Data: []byte(launcher)},
	}
	manifest := linuxBundleManifest{SchemaVersion: 1, ID: f.id, Name: name, Version: f.version, Platform: "linux-" + arch, EntryPoint: "launch.sh", Runtime: []string{"glibc >= 2.34", "GTK 3", "WebKitGTK 4.1 >= 2.40", "X11 or Wayland graphical session"}}
	if f.scheme != "" {
		manifest.EmbeddedScheme = f.scheme
		manifest.Runtime = append(manifest.Runtime, "existing local Unix D-Bus session bus (URL activation)")
	}
	return linuxbundle.WithManifest(manifest, entries)
}

func escapeDesktopEntryString(value string) string { return linuxbundle.EscapeDesktopString(value) }

func copyLinuxArchiveEntry(dst io.Writer, entry linuxArchiveEntry) (int64, error) {
	return entry.CopyTo(dst)
}

func writeLinuxArchive(dst io.Writer, root string, entries []linuxArchiveEntry) error {
	return linuxbundle.WriteArchive(dst, root, entries)
}
