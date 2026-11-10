//go:build darwin && (arm64 || amd64)

package update

import (
	"errors"
	"fmt"
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
	policy SignaturePolicy
	// warn receives the note that an install rests on the feed
	// signature alone; nil drops it.
	warn func(msg string)
	// running is a test seam: the running bundle's path, when the test
	// binary has no bundle of its own. "" asks NSBundle.
	running string
}

// SetSignaturePolicy installs the code-signing pin VerifySignature
// applies. Without a call the zero policy holds.
func (p *darwinPlatform) SetSignaturePolicy(policy SignaturePolicy) { p.policy = policy }

// SetWarn installs the receiver for the feed-signature-only note.
func (p *darwinPlatform) SetWarn(fn func(string)) { p.warn = fn }

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

// Identifier implements Platform: mainBundle's bundleIdentifier (the
// CFBundleIdentifier the running Info.plist declares), "" when
// unbundled.
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

// VerifySignature implements Platform. codesign --verify --deep
// --strict alone proves the bundle carries a valid signature, not whose:
// an ad-hoc signature (codesign --sign -) passes it. So the check also
// names a requirement (-R) the signature must satisfy:
//
//   - policy.TeamID set: a Developer ID certificate of that team.
//   - otherwise the running bundle's own designated requirement, the
//     rule macOS itself uses to call two builds the same app. It names
//     the bundle identifier and the signing identity.
//   - a running bundle with no signing identity (ad-hoc signed, or not
//     signed) has nothing to pin, and the update is refused, unless
//     policy.AllowAdHoc accepts any valid signature.
//
// When codesign cannot be run to read the running bundle, the update is
// refused under every policy: "could not ask" is not "has no identity".
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

// requirement returns the code requirement an update must satisfy, or
// "" when the policy accepts any valid signature.
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
	// A bundle that is not signed at all and an ad-hoc signature are
	// the same answer: no identity. codesign says the first in words
	// only. Its exit status is 1 for that and for a path that is gone
	// or unreadable alike, so the words are the evidence. Anything else
	// that goes wrong leaves the identity unknown, and an unknown
	// identity pins nothing safely: an app the user moved while it ran
	// must not lose its pin. If Apple rewords the message, an unsigned
	// app refuses its updates, which is the safe way to be wrong.
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

// notSignedMessage is what codesign prints for a bundle that carries no
// signature.
const notSignedMessage = "code object is not signed at all"

// teamRequirement is the code requirement for "signed with this team's
// Developer ID certificate": the rule codesign writes into a Developer
// ID signature's own designated requirement, without the bundle
// identifier (the install checks that against Info.plist). The two
// certificate fields are Apple's Developer ID intermediate and leaf
// markers. Without them any certificate Apple issued to the team would
// pass, including the Apple Development ones every team member can
// mint. teamID is validated at construction (ten ASCII letters and
// digits), so it needs no quoting beyond the literal's own.
func teamRequirement(teamID string) string {
	return `anchor apple generic and certificate 1[field.1.2.840.113635.100.6.2.6] exists` +
		` and certificate leaf[field.1.2.840.113635.100.6.1.13] exists` +
		` and certificate leaf[subject.OU] = "` + teamID + `"`
}

// designatedRequirement picks the requirement out of `codesign -d -r-`
// output, or "" when the signature names no identity. An explicit
// requirement prints as "designated => <text>". An ad-hoc signature has
// none, and codesign prints the one it would synthesize behind a "#":
// a list of code hashes no other build satisfies. A requirement with no
// certificate anchor is treated the same way, since it pins no signer.
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

// Relaunch implements Platform: open -n starts the new bundle in a
// fresh process before the current one quits.
func (p *darwinPlatform) Relaunch(appDir string) error {
	return p.runner.Run("open", "-n", appDir)
}
