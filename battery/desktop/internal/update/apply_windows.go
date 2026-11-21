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

type windowsPlatform struct {
	once sync.Once
	exe  string // resolved path of the running executable
	info BundleInfo
	ok   bool

	trust func(path string) Trust
	start func(exe string, env []string) error
	warn  func(msg string)
}

// DefaultPlatform returns the Windows platform.
func DefaultPlatform(onMain func(func()) error, runner CommandRunner) Platform {
	return &windowsPlatform{}
}

func (p *windowsPlatform) SetWarn(fn func(string)) { p.warn = fn }

func (p *windowsPlatform) load() {
	p.once.Do(func() {
		exe, err := os.Executable()
		if err != nil {
			return
		}
		p.exe, p.info, p.ok = readRunningBuild(exe)
	})
}

func readRunningBuild(exe string) (string, BundleInfo, bool) {
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	info, err := ReadExeInfo(exe)
	if err != nil {
		return "", BundleInfo{}, false
	}
	if st, err := os.Lstat(filepath.Join(filepath.Dir(exe), WebView2LoaderName)); err != nil || !st.Mode().IsRegular() {
		return "", BundleInfo{}, false
	}
	return exe, info, true
}

func (p *windowsPlatform) Version() string {
	p.load()
	return p.info.ShortVersion
}

func (p *windowsPlatform) Identifier() string {
	p.load()
	return p.info.Identifier
}

func (p *windowsPlatform) Bundle() (string, string, bool) {
	p.load()
	if !p.ok {
		return "", "", false
	}
	return exeStem(p.exe), filepath.Dir(p.exe), true
}

func exeStem(exe string) string {
	base := filepath.Base(exe)
	if strings.EqualFold(filepath.Ext(base), ".exe") {
		return base[:len(base)-4]
	}
	return base
}

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
