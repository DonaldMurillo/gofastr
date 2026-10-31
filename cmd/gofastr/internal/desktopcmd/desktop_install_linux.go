package desktopcmd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"debug/elf"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/core/handler"
)

const linuxInstallReceiptName = ".gofastr-install.json"
const linuxInstallMaxExecutable = 1 << 30

type linuxInstallReceipt struct {
	SchemaVersion  int    `json:"schema_version"`
	ID             string `json:"id"`
	ManifestSHA256 string `json:"manifest_sha256"`
	LauncherSHA256 string `json:"launcher_sha256"`
	DesktopSHA256  string `json:"desktop_sha256"`
}
type linuxInstallPaths struct{ data, bin, apps, applications string }
type linuxInstallBundle struct {
	root        *os.Root
	manifest    linuxBundleManifest
	rawManifest []byte
}

// Install only a verified fixed-format bundle. Checksums are integrity checks,
// not authenticity: the user must trust the executable's publisher separately.
func installLinuxBundle(source string) error {
	bundle, err := openLinuxInstallBundle(source, false, true)
	if err != nil {
		return err
	}
	defer bundle.root.Close()
	paths, err := linuxUserInstallPaths()
	if err != nil {
		return err
	}
	return installVerifiedLinuxBundle(bundle, paths)
}
func installVerifiedLinuxBundle(bundle *linuxInstallBundle, paths linuxInstallPaths) (err error) {
	desktopEntry, err := linuxInstalledDesktopEntry(bundle.manifest, filepath.Join(paths.apps, bundle.manifest.ID), filepath.Join(paths.bin, bundle.manifest.ID))
	if err != nil {
		return err
	}
	launcher := linuxInstalledLauncher(filepath.Join(paths.apps, bundle.manifest.ID, "bin", bundle.manifest.ID))
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
	id := bundle.manifest.ID
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
	if err = installed.Mkdir("bin", 0700); err != nil {
		return err
	}
	created = append(created, "bin")
	for _, entry := range bundle.manifest.Files {
		if err = copyLinuxInstallFile(bundle.root, installed, entry); err != nil {
			return err
		}
		created = append(created, entry.Path)
	}
	if err = writeLinuxInstallFile(installed, "manifest.json", bundle.rawManifest, 0600); err != nil {
		return err
	}
	created = append(created, "manifest.json")
	if err = writeLinuxInstallFile(bin, id, launcher, 0700); err != nil {
		return err
	}
	launcherCreated = true
	if err = writeLinuxInstallFile(applications, id+".desktop", desktopEntry, 0600); err != nil {
		return err
	}
	desktopCreated = true
	receipt := linuxInstallReceipt{1, id, linuxInstallDigest(bundle.rawManifest), linuxInstallDigest(launcher), linuxInstallDigest(desktopEntry)}
	data, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return err
	}
	if err = writeLinuxInstallFile(installed, linuxInstallReceiptName, append(data, '\n'), 0600); err != nil {
		return err
	}
	created = append(created, linuxInstallReceiptName)
	committed = true
	cli.Success("Installed %s for the current user", id)
	cli.Info("Launcher: %q; desktop entry: %q", filepath.Join(paths.bin, id), filepath.Join(paths.applications, id+".desktop"))
	if bundle.manifest.EmbeddedScheme != "" {
		cli.Info("Advertised URL handler for %s; default associations are unchanged", bundle.manifest.EmbeddedScheme)
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
	bundle, err := openLinuxInstallBundle(filepath.Join(paths.apps, id), true, false)
	if err != nil {
		return fmt.Errorf("refusing uninstall; installation is missing or changed: %w", err)
	}
	defer bundle.root.Close()
	for _, target := range []struct {
		root *os.Root
		name string
		mode fs.FileMode
	}{{bundle.root, ".", 0700}, {bundle.root, "bin", 0700}, {bundle.root, "manifest.json", 0600}, {bundle.root, linuxInstallReceiptName, 0600}, {bin, id, 0700}, {applications, id + ".desktop", 0600}} {
		if err = verifyLinuxInstalledMode(target.root, target.name, target.mode); err != nil {
			return err
		}
	}
	raw, err := readLinuxInstallFile(bundle.root, linuxInstallReceiptName, 64<<10)
	if err != nil {
		return err
	}
	var receipt linuxInstallReceipt
	if err = handler.UnmarshalStrict(raw, &receipt); err != nil {
		return fmt.Errorf("invalid installation receipt: %w", err)
	}
	if receipt.SchemaVersion != 1 || receipt.ID != id || bundle.manifest.ID != id || receipt.ManifestSHA256 != linuxInstallDigest(bundle.rawManifest) {
		return fmt.Errorf("refusing uninstall: installation receipt does not match the bundle")
	}
	launcher, err := readLinuxInstallFile(bin, id, 64<<10)
	if err != nil {
		return fmt.Errorf("refusing uninstall: launcher is missing or changed: %w", err)
	}
	desktopEntry, err := readLinuxInstallFile(applications, id+".desktop", 64<<10)
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
		return errors.Join(err, writeLinuxInstallFile(applications, id+".desktop", desktopEntry, 0600))
	}
	for _, entry := range bundle.manifest.Files {
		if err = bundle.root.Remove(entry.Path); err != nil {
			return fmt.Errorf("partial uninstall; retained remaining installation files: %w", err)
		}
	}
	for _, name := range []string{"manifest.json", linuxInstallReceiptName, "bin"} {
		if err = bundle.root.Remove(name); err != nil {
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
		if !filepath.IsAbs(value) || !utf8.ValidString(value) || strings.ContainsAny(value, "=\x00\n\r\t") {
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

func openLinuxInstallBundle(name string, installed, requireHost bool) (*linuxInstallBundle, error) {
	absolute, err := filepath.Abs(name)
	if err != nil {
		return nil, err
	}
	// Source roots may be read-only/public, but never a symlink. Root confines
	// descendants; each inventory member is additionally refused if non-regular.
	st, err := os.Lstat(absolute)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("bundle must be a directory, not a symlink")
	}
	root, err := os.OpenRoot(absolute)
	if err != nil {
		return nil, err
	}
	fail := func(e error) (*linuxInstallBundle, error) { root.Close(); return nil, e }
	raw, err := readLinuxInstallFile(root, "manifest.json", 512<<10)
	if err != nil {
		return fail(err)
	}
	var manifest linuxBundleManifest
	if err = handler.UnmarshalStrict(raw, &manifest); err != nil {
		return fail(fmt.Errorf("invalid bundle manifest: %w", err))
	}
	var fields map[string]json.RawMessage
	if err = handler.UnmarshalStrict(raw, &fields); err != nil {
		return fail(err)
	}
	var fileEntries []json.RawMessage
	if err = json.Unmarshal(fields["files"], &fileEntries); err != nil {
		return fail(err)
	}
	for _, rawEntry := range fileEntries {
		var entry linuxBundleFile
		if err = handler.UnmarshalStrict(rawEntry, &entry); err != nil {
			return fail(fmt.Errorf("invalid inventory fields: %w", err))
		}
	}
	arch := strings.TrimPrefix(manifest.Platform, "linux-")
	if manifest.SchemaVersion != 1 || manifest.EntryPoint != "launch.sh" || (manifest.Platform != "linux-amd64" && manifest.Platform != "linux-arm64") {
		return fail(fmt.Errorf("unsupported bundle schema, entry point, or platform"))
	}
	if requireHost && arch != runtime.GOARCH {
		return fail(fmt.Errorf("bundle architecture %s does not match this host's %s", arch, runtime.GOARCH))
	}
	if _, err = linuxBundleRoot(desktopBuildFlags{id: manifest.ID, version: manifest.Version, scheme: manifest.EmbeddedScheme}, manifest.Name, arch); err != nil {
		return fail(err)
	}
	expected := linuxInstallExpectedFiles(manifest.ID)
	if len(manifest.Files) != len(expected) {
		return fail(fmt.Errorf("bundle must contain the fixed five-file payload"))
	}
	seen := make(map[string]bool)
	for _, entry := range manifest.Files {
		mode, ok := expected[entry.Path]
		sum, e := hex.DecodeString(entry.SHA256)
		maxSize := int64(16 << 20)
		if entry.Path == "bin/"+manifest.ID {
			maxSize = linuxInstallMaxExecutable
		}
		if !ok || seen[entry.Path] || mode != entry.Mode || entry.Size < 0 || entry.Size > maxSize || e != nil || len(sum) != sha256.Size || entry.SHA256 != strings.ToLower(entry.SHA256) {
			return fail(fmt.Errorf("invalid or duplicate bundle inventory entry %q", entry.Path))
		}
		seen[entry.Path] = true
		if err = verifyLinuxInstallFile(root, entry); err != nil {
			return fail(err)
		}
		if installed {
			mode := fs.FileMode(0600)
			if entry.Mode == "0755" {
				mode = 0700
			}
			if err = verifyLinuxInstalledMode(root, entry.Path, mode); err != nil {
				return fail(err)
			}
		}
	}
	err = fs.WalkDir(root.FS(), ".", func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if path == "." || path == "bin" {
			if !d.IsDir() {
				return fmt.Errorf("invalid bundle directory")
			}
			return nil
		}
		if (path == "manifest.json" || installed && path == linuxInstallReceiptName || seen[path]) && d.Type().IsRegular() {
			return nil
		}
		return fmt.Errorf("unexpected bundle entry %q; no unlisted files, links, or directories are installed or removed", path)
	})
	if err != nil {
		return fail(err)
	}
	exe, err := openLinuxInstallRegular(root, "bin/"+manifest.ID)
	if err != nil {
		return fail(err)
	}
	object, err := elf.NewFile(exe)
	if err == nil {
		expectedMachine := elf.EM_X86_64
		if arch == "arm64" {
			expectedMachine = elf.EM_AARCH64
		}
		entryInCode := false
		for _, program := range object.Progs {
			if program.Type == elf.PT_LOAD && program.Flags&elf.PF_X != 0 && object.Entry >= program.Vaddr && object.Entry-program.Vaddr < program.Memsz {
				entryInCode = true
			}
		}
		if object.Class != elf.ELFCLASS64 || object.Data != elf.ELFDATA2LSB || object.Machine != expectedMachine || (object.OSABI != elf.ELFOSABI_NONE && object.OSABI != elf.ELFOSABI_LINUX) || (object.Type != elf.ET_EXEC && object.Type != elf.ET_DYN) || object.Entry == 0 || !entryInCode {
			err = fmt.Errorf("ELF executable does not match bundle platform or has no executable entry point")
		}
		_ = object.Close()
	}
	_ = exe.Close()
	if err != nil {
		return fail(fmt.Errorf("invalid bundle executable: %w", err))
	}
	icon, err := readLinuxInstallFile(root, "icon.png", 16<<20)
	if err != nil {
		return fail(err)
	}
	if err = validateLinuxInstallIcon(icon); err != nil {
		return fail(err)
	}
	return &linuxInstallBundle{root: root, manifest: manifest, rawManifest: raw}, nil
}
func linuxInstallExpectedFiles(id string) map[string]string {
	return map[string]string{"README.txt": "0644", "bin/" + id: "0755", id + ".desktop.in": "0644", "icon.png": "0644", "launch.sh": "0755"}
}
func openLinuxInstallRegular(root *os.Root, name string) (*os.File, error) {
	// Nonblocking plus no-follow also prevents a FIFO/symlink replacement between
	// validation and open from hanging or following the leaf.
	f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		f.Close()
		return nil, fmt.Errorf("bundle/installation entry %q must be a regular file", name)
	}
	return f, nil
}
func readLinuxInstallFile(root *os.Root, name string, limit int64) ([]byte, error) {
	f, err := openLinuxInstallRegular(root, name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("entry %q exceeds its size limit", name)
	}
	return b, nil
}
func verifyLinuxInstallFile(root *os.Root, entry linuxBundleFile) error {
	f, err := openLinuxInstallRegular(root, entry.Path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, entry.Size+1))
	if err != nil {
		return err
	}
	if n != entry.Size || hex.EncodeToString(h.Sum(nil)) != entry.SHA256 {
		return fmt.Errorf("bundle checksum or size mismatch for %q", entry.Path)
	}
	return nil
}
func copyLinuxInstallFile(source, dest *os.Root, entry linuxBundleFile) (err error) {
	in, err := openLinuxInstallRegular(source, entry.Path)
	if err != nil {
		return err
	}
	defer in.Close()
	mode := fs.FileMode(0600)
	if entry.Mode == "0755" {
		mode = 0700
	}
	out, err := dest.OpenFile(entry.Path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = dest.Remove(entry.Path)
		}
	}()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(out, h), io.LimitReader(in, entry.Size+1))
	err = errors.Join(err, out.Close())
	if err != nil {
		return err
	}
	if n != entry.Size || hex.EncodeToString(h.Sum(nil)) != entry.SHA256 {
		return fmt.Errorf("bundle changed while copying %q", entry.Path)
	}
	return nil
}
func writeLinuxInstallFile(root *os.Root, name string, data []byte, mode fs.FileMode) (err error) {
	f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	err = errors.Join(err, f.Close())
	if err != nil {
		_ = root.Remove(name)
	}
	return err
}
func linuxInstallDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func linuxInstalledLauncher(executable string) []byte {
	return []byte("#!/bin/sh\nexec " + linuxInstallShellQuote(executable) + " \"$@\"\n")
}
func linuxInstallShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
func verifyLinuxInstalledMode(root *os.Root, name string, mode fs.FileMode) error {
	st, err := root.Lstat(name)
	if err != nil {
		return err
	}
	stat, ok := st.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) || st.Mode().Perm() != mode || st.Mode()&(os.ModeSymlink|os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return fmt.Errorf("refusing uninstall: owner or permissions changed for %q", name)
	}
	return nil
}
func linuxInstalledDesktopEntry(manifest linuxBundleManifest, root, launcher string) ([]byte, error) {
	if !utf8.ValidString(launcher) || strings.ContainsAny(launcher, "=\x00\n\r\t") {
		return nil, fmt.Errorf("desktop executable path contains unsupported characters")
	}
	quoted := `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "`", "\\`", `$`, `\$`, `%`, `%%`).Replace(launcher) + `"`
	// GIO checks Exec's executable before expanding %% into literal %. A
	// fixed shell executable keeps arbitrary installation paths in an argument.
	// There is no -c: the file is our generated launcher; URLs remain argv.
	command := "/bin/sh " + escapeDesktopEntryString(quoted)
	mime := ""
	if manifest.EmbeddedScheme != "" {
		command += " %u"
		mime = "MimeType=x-scheme-handler/" + manifest.EmbeddedScheme + ";\n"
	}
	return []byte("[Desktop Entry]\nType=Application\nVersion=1.0\nName=" + escapeDesktopEntryString(manifest.Name) + "\nExec=" + command + "\nIcon=" + escapeDesktopEntryString(filepath.Join(root, "icon.png")) + "\nTerminal=false\n" + mime), nil
}
func refreshLinuxDesktopDatabase(applications string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "update-desktop-database", applications)
	cmd.WaitDelay = time.Second
	if err := cmd.Run(); err != nil {
		cli.Warn("Desktop entry changed, but MIME cache refresh did not complete. Run update-desktop-database %s after installing desktop-file-utils; default handlers were not changed.", linuxInstallShellQuote(applications))
	}
}

// Validate already-checksummed bytes without decoding an unbounded source.
func validateLinuxInstallIcon(data []byte) error {
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("invalid bundle icon: %w", err)
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > 4096 || config.Height > 4096 {
		return fmt.Errorf("bundle icon dimensions must be between 1 and 4096")
	}
	if _, err = png.Decode(bytes.NewReader(data)); err != nil {
		return fmt.Errorf("invalid bundle icon: %w", err)
	}
	return nil
}
