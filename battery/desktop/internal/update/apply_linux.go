//go:build linux

package update

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/DonaldMurillo/gofastr/internal/linuxbundle"
)

// linuxPlatform updates an app `gofastr desktop install` installed: the
// running executable is <apps>/<id>/bin/<id>, and the unit an update
// swaps is the whole <apps>/<id> directory, replaced by one staged in
// the installed form with a receipt reissued for its manifest.
type linuxPlatform struct {
	once sync.Once
	exe  string // running executable; os.Executable when empty

	apps     string
	manifest linuxbundle.Manifest
	receipt  linuxbundle.Receipt
	ok       bool

	start func(exe string, env []string) error
	warn  func(msg string)
}

// SideSep joins an installation's directory to "old" and "new" beside it (linuxbundle.SideSep).
const SideSep = linuxbundle.SideSep

// DefaultPlatform returns the Linux platform.
func DefaultPlatform(onMain func(func()) error, runner CommandRunner) Platform {
	return &linuxPlatform{}
}

// NewInstalledPlatform is the Linux platform for the installation exe
// runs from, with start, when set, in place of starting a process in
// Relaunch.
func NewInstalledPlatform(exe string, start func(exe string, env []string) error) Platform {
	return &linuxPlatform{exe: exe, start: start}
}

func (p *linuxPlatform) SetWarn(fn func(string)) { p.warn = fn }

func (p *linuxPlatform) load() {
	p.once.Do(func() {
		exe := p.exe
		if exe == "" {
			var err error
			if exe, err = os.Executable(); err != nil {
				return
			}
		}
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		id := filepath.Base(exe)
		bin := filepath.Dir(exe)
		dir := filepath.Dir(bin)
		if filepath.Base(bin) != "bin" || filepath.Base(dir) != id || !linuxbundle.ValidID(id) {
			return
		}
		apps := filepath.Dir(dir)
		root, err := linuxbundle.OpenDirectory(apps, false)
		if err != nil {
			p.warnf("the directory this app is installed in is not private to this user, so updates are off")
			return
		}
		_ = root.Close()
		m, receipt, err := linuxbundle.ReadInstalled(dir, id)
		if err != nil {
			p.warnf("this installation's manifest or receipt is unreadable (" + bundleDetail(err) + "), so updates are off until it is reinstalled with gofastr desktop install")
			return
		}
		p.apps, p.manifest, p.receipt, p.ok = apps, m, receipt, true
	})
}

func (p *linuxPlatform) warnf(msg string) {
	if p.warn != nil {
		p.warn(msg)
	}
}

func (p *linuxPlatform) Version() string {
	p.load()
	return p.manifest.Version
}

func (p *linuxPlatform) Identifier() string {
	p.load()
	return p.manifest.ID
}

func (p *linuxPlatform) Bundle() (string, string, bool) {
	p.load()
	if !p.ok {
		return "", "", false
	}
	return p.manifest.ID, p.apps, true
}

func (p *linuxPlatform) LockPath() (string, bool) {
	id, apps, ok := p.Bundle()
	if !ok {
		return "", false
	}
	return filepath.Join(apps, linuxbundle.LockName(id)), true
}

func (p *linuxPlatform) Units() ([]Unit, bool) {
	id, apps, ok := p.Bundle()
	if !ok {
		return nil, false
	}
	return []Unit{{Rel: id, Current: filepath.Join(apps, id), Sep: SideSep}}, true
}

// Extract refuses with ErrInstallationChanged when the installation on
// disk is no longer the one this process read at start, then unpacks
// the release archive under stage, verifies it as a
// bundle for this host, and stages its installed form in stage/bundle,
// named by the id its manifest declares, with the running install's
// launcher and desktop entry digests carried into a receipt for the
// new manifest. It returns stage/bundle.
func (p *linuxPlatform) Extract(data []byte, stage string) (string, error) {
	id, apps, ok := p.Bundle()
	if !ok {
		return "", ErrUnsupportedApply
	}
	if _, now, err := linuxbundle.ReadInstalled(filepath.Join(apps, id), id); err != nil || now.ManifestSHA256 != p.receipt.ManifestSHA256 {
		return "", ErrInstallationChanged
	}
	unpacked := filepath.Join(stage, "archive")
	if err := os.Mkdir(unpacked, 0o700); err != nil {
		return "", fmt.Errorf("%w: %s", ErrBadArchive, LogDetail(err))
	}
	root, err := ExtractTarGz(data, unpacked)
	if err != nil {
		return "", err
	}
	src, err := linuxbundle.Open(root, false, true)
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrBadArchive, bundleDetail(err))
	}
	defer src.Close()
	prepared := filepath.Join(stage, "bundle")
	if err := os.Mkdir(prepared, 0o700); err != nil {
		return "", fmt.Errorf("%w: %s", ErrBadArchive, LogDetail(err))
	}
	if err := os.Mkdir(filepath.Join(prepared, src.Manifest.ID), 0o700); err != nil {
		return "", fmt.Errorf("%w: %s", ErrBadArchive, LogDetail(err))
	}
	dest, err := os.OpenRoot(filepath.Join(prepared, src.Manifest.ID))
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrBadArchive, LogDetail(err))
	}
	defer dest.Close()
	if _, err := linuxbundle.Populate(src, dest); err != nil {
		return "", fmt.Errorf("%w: %s", ErrBadArchive, bundleDetail(err))
	}
	if err := linuxbundle.WriteReceipt(dest, p.receipt.Reissued(src.RawManifest)); err != nil {
		return "", fmt.Errorf("%w: %s", ErrBadArchive, LogDetail(err))
	}
	return prepared, nil
}

// Inspect opens the staged installed form for the running app's id as
// uninstall would and returns the identity its manifest declares.
func (p *linuxPlatform) Inspect(extracted string) (BundleInfo, error) {
	m, err := p.staged(extracted)
	if err != nil {
		return BundleInfo{}, err
	}
	return BundleInfo{Identifier: m.ID, ShortVersion: m.Version}, nil
}

// VerifySignature is the Linux trust policy. The archive was pinned by
// the signed feed before extraction and every payload file by the
// archive's manifest; this refuses a staged bundle that no longer opens
// as an installation, or whose display name or embedded URL scheme
// differs from the running app's, since the launcher and desktop entry
// the receipt vouches for are rendered from them and the update leaves
// both in place.
func (p *linuxPlatform) VerifySignature(extracted string) error {
	m, err := p.staged(extracted)
	if err != nil {
		return err
	}
	if m.Name != p.manifest.Name || m.EmbeddedScheme != p.manifest.EmbeddedScheme {
		return fmt.Errorf("%w: the update changes the app's name or URL scheme, which its installed desktop entry carries; reinstall it with gofastr desktop install", ErrSignature)
	}
	return nil
}

func (p *linuxPlatform) staged(extracted string) (linuxbundle.Manifest, error) {
	id, _, ok := p.Bundle()
	if !ok {
		return linuxbundle.Manifest{}, ErrUnsupportedApply
	}
	b, _, err := linuxbundle.OpenInstalled(filepath.Join(extracted, id), id)
	if err != nil {
		return linuxbundle.Manifest{}, fmt.Errorf("%w: %s", ErrBadBundleInfo, bundleDetail(err))
	}
	defer b.Close()
	return b.Manifest, nil
}

// Relaunch starts <dir>/bin/<id> in a session of its own with
// PredecessorEnv naming this process, so the new instance waits for this
// one to quit before it claims the app's launch.
func (p *linuxPlatform) Relaunch(dir string) error {
	id, _, ok := p.Bundle()
	if !ok {
		return ErrUnsupportedApply
	}
	exe := filepath.Join(dir, "bin", id)
	env := relaunchEnv(os.Environ(), func(a, b string) bool { return a == b })
	if p.start != nil {
		return p.start(exe, env)
	}
	cmd := exec.Command(exe)
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start the updated app: %s", LogDetail(err))
	}
	return cmd.Process.Release()
}

// bundleDetail is err as a log line may carry it: the operation and OS
// error of a file-system error, which would name the user's home
// directory, and otherwise linuxbundle's own text, which names only
// entries relative to the bundle.
func bundleDetail(err error) string {
	var pe *fs.PathError
	var le *os.LinkError
	var se *os.SyscallError
	if errors.As(err, &pe) || errors.As(err, &le) || errors.As(err, &se) {
		return LogDetail(err)
	}
	return err.Error()
}
