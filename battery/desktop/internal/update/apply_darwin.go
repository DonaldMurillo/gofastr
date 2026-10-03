//go:build darwin && (arm64 || amd64)

package update

import (
	"path/filepath"
	"strings"

	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/objc"
)

// darwinPlatform implements Platform on macOS: the running bundle is
// read through NSBundle (on the main thread, the same hop every other
// AppKit-adjacent call uses), the signature check is codesign, and the
// relaunch is `open -n`.
type darwinPlatform struct {
	onMain func(func()) error
	runner CommandRunner
}

// DefaultPlatform returns the darwin platform. onMain hops to the
// main thread (nil uses objc.Main, the same hop the shell's onMain
// wraps: the run loop services the dispatch main queue between
// events, so it works while [NSApp run] owns the thread); runner runs
// external commands (ExecRunner in production, a fake in tests).
func DefaultPlatform(onMain func(func()) error, runner CommandRunner) Platform {
	if runner == nil {
		runner = ExecRunner{}
	}
	return &darwinPlatform{onMain: onMain, runner: runner}
}

// hop runs fn on the main thread.
func (p *darwinPlatform) hop(fn func()) error {
	if p.onMain != nil {
		return p.onMain(fn)
	}
	return objc.Main(fn)
}
func (p *darwinPlatform) Version() string {
	var v string
	_ = p.hop(func() {
		bundle := objc.ID(objc.Send(objc.Class("NSBundle"), objc.Sel("mainBundle")))
		if bundle == 0 {
			return
		}
		dict := objc.ID(objc.Send(bundle, objc.Sel("infoDictionary")))
		if dict == 0 {
			return
		}
		val := objc.ID(objc.Send(dict, objc.Sel("objectForKey:"),
			uintptr(objc.NSString("CFBundleShortVersionString"))))
		if val == 0 {
			return
		}
		v = objc.GoString(val)
	})
	return v
}

// Bundle implements Platform: mainBundle's bundlePath when it is a
// .app directory, ok=false for an unbundled binary.
func (p *darwinPlatform) Bundle() (string, string, bool) {
	var path string
	_ = p.hop(func() {
		bundle := objc.ID(objc.Send(objc.Class("NSBundle"), objc.Sel("mainBundle")))
		if bundle == 0 {
			return
		}
		bp := objc.Send(bundle, objc.Sel("bundlePath"))
		if bp == 0 {
			return
		}
		path = objc.GoString(objc.ID(bp))
	})
	if path == "" {
		return "", "", false
	}
	name := filepath.Base(path)
	if !strings.HasSuffix(name, ".app") {
		return "", "", false
	}
	return name, filepath.Dir(path), true
}

// VerifySignature implements Platform: codesign --verify --deep
// --strict refuses a tampered or unsigned bundle.
func (p *darwinPlatform) VerifySignature(appDir string) error {
	if err := p.runner.Run("codesign", "--verify", "--deep", "--strict", appDir); err != nil {
		return err
	}
	return nil
}

// Relaunch implements Platform: open -n starts the new bundle in a
// fresh process before the current one quits.
func (p *darwinPlatform) Relaunch(appDir string) error {
	return p.runner.Run("open", "-n", appDir)
}
