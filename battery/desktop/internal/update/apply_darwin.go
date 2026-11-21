//go:build darwin && (arm64 || amd64)

package update

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/objc"
)

type darwinPlatform struct {
	onMain  func(func()) error
	runner  CommandRunner
	policy  SignaturePolicy
	warn    func(msg string)
	running string
}

func (p *darwinPlatform) SetSignaturePolicy(policy SignaturePolicy) { p.policy = policy }

func (p *darwinPlatform) SetWarn(fn func(string)) { p.warn = fn }

// DefaultPlatform returns the darwin platform.
func DefaultPlatform(onMain func(func()) error, runner CommandRunner) Platform {
	if runner == nil {
		runner = ExecRunner{}
	}
	return &darwinPlatform{onMain: onMain, runner: runner}
}

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

func (p *darwinPlatform) Identifier() string {
	var id string
	_ = p.hop(func() {
		bundle := objc.ID(objc.Send(objc.Class("NSBundle"), objc.Sel("mainBundle")))
		if bundle == 0 {
			return
		}
		val := objc.ID(objc.Send(bundle, objc.Sel("bundleIdentifier")))
		if val == 0 {
			return
		}
		id = objc.GoString(val)
	})
	return id
}

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

func (p *darwinPlatform) VerifySignature(appDir string) error {
	requirement, err := p.requirement()
	if err != nil {
		return err
	}
	args := []string{"--verify", "--deep", "--strict"}
	if requirement != "" {
		args = append(args, "-R="+requirement)
	}
	return p.runner.Run("codesign", append(args, appDir)...)
}

func (p *darwinPlatform) requirement() (string, error) {
	if p.policy.TeamID != "" {
		return teamRequirement(p.policy.TeamID), nil
	}
	running := p.running
	if running == "" {
		name, dir, ok := p.Bundle()
		if !ok {
			return "", ErrUnsupportedApply
		}
		running = filepath.Join(dir, name)
	}
	out, err := p.runner.Output("codesign", "-d", "-r-", running)
	var exit *ExitError
	switch {
	case err == nil:
		if pin := designatedRequirement(string(out)); pin != "" {
			return pin, nil
		}
	case errors.As(err, &exit) && exit.Code == 1 && strings.Contains(exit.Stderr, notSignedMessage):
	default:
		return "", fmt.Errorf("%w: the running app's signing identity could not be read: %v", ErrSignature, err)
	}
	if !p.policy.AllowAdHoc {
		return "", fmt.Errorf("%w: the running app has no signing identity to hold the update to (it is ad-hoc signed or unsigned); sign releases with a Developer ID, or set UpdateConfig.MacOSTeamID or MacOSAllowAdHoc", ErrSignature)
	}
	if p.warn != nil {
		p.warn("the running app has no signing identity; this update rests on the feed signature alone")
	}
	return "", nil
}

const notSignedMessage = "code object is not signed at all"

func teamRequirement(teamID string) string {
	return `anchor apple generic and certificate 1[field.1.2.840.113635.100.6.2.6] exists` +
		` and certificate leaf[field.1.2.840.113635.100.6.1.13] exists` +
		` and certificate leaf[subject.OU] = "` + teamID + `"`
}

func designatedRequirement(out string) string {
	for _, line := range strings.Split(out, "\n") {
		text, ok := strings.CutPrefix(strings.TrimSpace(line), "designated => ")
		if !ok {
			continue
		}
		if !strings.Contains(text, "anchor ") {
			return ""
		}
		return text
	}
	return ""
}

func (p *darwinPlatform) Relaunch(appDir string) error {
	return p.runner.Run("open", "-n", appDir)
}
