//go:build windows

package update

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

// windowsPlatform implements Platform and FileLayout on Windows. The
// running app is the executable `gofastr desktop build` wrote, with
// WebView2Loader.dll beside it; its identity is the version resource
// the build embedded. An install replaces those two files in place: a
// running executable or a loaded DLL can be renamed but not
// overwritten, so each moves aside to <file>.old and the next launch
// removes it.
type windowsPlatform struct {
	once sync.Once
	exe  string // resolved path of the running executable
	info BundleInfo
	ok   bool

	// trust and start are test seams: nil selects VerifyAuthenticode
	// and a detached process start.
	trust func(path string) Trust
	start func(exe string, env []string) error
	// warn receives the note that an install rests on the feed
	// signature alone; nil drops it.
	warn func(msg string)
}

// DefaultPlatform returns the Windows platform. onMain and runner are
// unused: nothing here needs the UI thread or an external command.
func DefaultPlatform(onMain func(func()) error, runner CommandRunner) Platform {
	return &windowsPlatform{}
}

// SetWarn installs the receiver for the feed-signature-only note.
func (p *windowsPlatform) SetWarn(fn func(string)) { p.warn = fn }

// load resolves the running executable and reads its identity once.
func (p *windowsPlatform) load() {
	p.once.Do(func() {
		exe, err := os.Executable()
		if err != nil {
			return
		}
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		info, err := ReadExeInfo(exe)
		if err != nil {
			return
		}
		if st, err := os.Lstat(filepath.Join(filepath.Dir(exe), WebView2LoaderName)); err != nil || !st.Mode().IsRegular() {
			return
		}
		p.exe, p.info, p.ok = exe, info, true
	})
}

// Version implements Platform: the executable's ProductVersion, "" when
// it was not built by `gofastr desktop build`.
func (p *windowsPlatform) Version() string {
	p.load()
	return p.info.ShortVersion
}

// Identifier implements Platform: the executable's GoFastrAppID.
func (p *windowsPlatform) Identifier() string {
	p.load()
	return p.info.Identifier
}

// Bundle implements Platform: the executable's base name without .exe
// (the update archive's root directory) and the directory it runs from.
func (p *windowsPlatform) Bundle() (string, string, bool) {
	p.load()
	if !p.ok {
		return "", "", false
	}
	return exeStem(p.exe), filepath.Dir(p.exe), true
}

// exeStem is the executable's base name with a case-insensitive .exe
// suffix removed.
func exeStem(exe string) string {
	base := filepath.Base(exe)
	if strings.EqualFold(filepath.Ext(base), ".exe") {
		return base[:len(base)-4]
	}
	return base
}

// Units implements FileLayout: the executable first, then the loader.
func (p *windowsPlatform) Units() ([]Unit, bool) {
	name, dir, ok := p.Bundle()
	if !ok {
		return nil, false
	}
	return []Unit{
		{Rel: name + ".exe", Current: p.exe},
		{Rel: WebView2LoaderName, Current: filepath.Join(dir, WebView2LoaderName)},
	}, true
}

// Inspect implements FileLayout.
func (p *windowsPlatform) Inspect(extracted string) (BundleInfo, error) {
	name, _, ok := p.Bundle()
	if !ok {
		return BundleInfo{}, ErrUnsupportedApply
	}
	if err := CheckWindowsBundle(extracted, name); err != nil {
		return BundleInfo{}, err
	}
	return ReadWindowsBundleInfo(extracted, name)
}

// VerifySignature implements Platform with CheckWindowsTrust over the
// running executable and the extracted executable and loader.
func (p *windowsPlatform) VerifySignature(extracted string) error {
	name, _, ok := p.Bundle()
	if !ok {
		return ErrUnsupportedApply
	}
	trust := p.trust
	if trust == nil {
		trust = VerifyAuthenticode
	}
	running := trust(p.exe)
	runningDLL := trust(filepath.Join(filepath.Dir(p.exe), WebView2LoaderName))
	exe := trust(filepath.Join(extracted, name+".exe"))
	dll := trust(filepath.Join(extracted, WebView2LoaderName))
	if err := CheckWindowsTrust(running, runningDLL, exe, dll); err != nil {
		return err
	}
	if running.State != Trusted && p.warn != nil {
		p.warn("the running app is not signed by a trusted publisher; this update rests on the feed signature alone")
	}
	return nil
}

// Relaunch implements Platform: start the installed executable as a
// detached process that first waits for this one to exit (see
// AwaitPredecessor), so it does not hand its launch to the quitting
// instance through the single-instance mutex.
func (p *windowsPlatform) Relaunch(exe string) error {
	env := append(withoutEnv(os.Environ(), PredecessorEnv), PredecessorEnv+"="+strconv.Itoa(os.Getpid()))
	if p.start != nil {
		return p.start(exe, env)
	}
	cmd := exec.Command(exe)
	cmd.Dir = filepath.Dir(exe)
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | 0x00000008, // DETACHED_PROCESS
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start the updated app: %s", LogDetail(err))
	}
	return cmd.Process.Release()
}

// withoutEnv drops every key=value entry for key (case-insensitive, as
// Windows environment names are).
func withoutEnv(env []string, key string) []string {
	out := env[:0:0]
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		if !strings.EqualFold(k, key) {
			out = append(out, kv)
		}
	}
	return out
}
