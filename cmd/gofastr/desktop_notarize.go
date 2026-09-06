package main

// The notarization seam and pipeline behind `gofastr desktop build
// --notarize`: hardened-runtime signing, a pure-Go zip of the bundle,
// notarytool submission, stapling, and a re-zip so the distributed
// archive carries the staple. Every external command of the signing
// and notarization path goes through runTool, one package-level func
// variable, so the tests assert the exact argv of codesign, notarytool,
// and stapler without any of them (or an Apple account) being real.

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// errNotarizeRequiresIdentity is the named refusal for --notarize
// without a notarizable identity: Apple's notary service rejects
// anything not signed by a real certificate, and an ad-hoc signature
// (`--sign -`, the default when --sign is absent) is exactly that.
var errNotarizeRequiresIdentity = errors.New(
	"--notarize requires --sign <Developer ID Application identity>: " +
		"an ad-hoc signature cannot be notarized")

// validateNotarizeFlags refuses --notarize without a real identity
// before the build starts, so no tool ever runs for a doomed request.
// "--notarize --no-sign" lands here too: --no-sign leaves --sign empty.
func validateNotarizeFlags(f desktopBuildFlags) error {
	if f.notarize && (f.sign == "" || f.sign == "-") {
		return errNotarizeRequiresIdentity
	}
	return nil
}

// runTool runs one external tool of the desktop build pipeline and
// returns its combined output. Signing and notarization call only
// through this seam; tests swap it for a fake that records argv.
var runTool = func(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).CombinedOutput()
	return string(out), err
}

// toolDetail returns err with the tool's own output appended when it
// printed anything: Apple's tools put the real reason in their output,
// not in the exit status.
func toolDetail(out string, err error) error {
	if msg := strings.TrimSpace(out); msg != "" {
		return fmt.Errorf("%v\n%s", err, msg)
	}
	return err
}

// generatedEntitlements is the plist written to a temp file when
// --entitlements is absent: an empty dict. A Go binary hosting a
// WKWebView needs no JIT entitlement, because the JavaScript compiler
// runs inside WebKit's own processes, which hold that entitlement
// themselves; the app process never JITs. An empty dict is therefore
// the correct hardened-runtime baseline for these bundles.
const generatedEntitlements = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict/>
</plist>
`

// notarizeDesktopBundle runs the distribute-ready path: hardened-runtime
// signing, a zip for submission, the notary wait, the staple, and a
// re-zip so the archive carries the staple. Each step prints the way
// the build verb prints its steps, and any failure is returned so the
// build fails (unlike the best-effort default ad-hoc signing).
func notarizeDesktopBundle(f desktopBuildFlags, name, appDir string) error {
	zipPath := filepath.Join(f.out, name+".zip")

	plistPath, cleanup, err := entitlementsPath(f)
	if err != nil {
		return err
	}
	defer cleanup()

	info("Signing %s with %q (hardened runtime, timestamped)...", appDir, f.sign)
	out, err := runTool("codesign", "--force", "--deep", "--sign", f.sign,
		"--options", "runtime", "--timestamp", "--entitlements", plistPath, appDir)
	if err != nil {
		return fmt.Errorf("signing: %w", toolDetail(out, err))
	}
	success("Signed %s (hardened runtime, timestamped).", appDir)

	info("Zipping %s for submission...", zipPath)
	if err := zipAppBundle(appDir, zipPath); err != nil {
		return fmt.Errorf("zip for submission: %w", err)
	}
	success("Wrote %s", zipPath)

	info("Submitting %s to Apple's notary service (profile %q, waiting)...", zipPath, f.notaryProfile)
	out, err = runTool("xcrun", "notarytool", "submit", zipPath,
		"--keychain-profile", f.notaryProfile, "--wait")
	if err != nil {
		return fmt.Errorf("notarytool submit: %w", toolDetail(out, err))
	}
	success("Notarized %s.", name)

	info("Stapling the notary ticket to %s...", appDir)
	out, err = runTool("xcrun", "stapler", "staple", appDir)
	if err != nil {
		return fmt.Errorf("stapler staple: %w", toolDetail(out, err))
	}
	success("Stapled %s.", appDir)

	info("Re-zipping %s so the archive carries the staple...", zipPath)
	if err := zipAppBundle(appDir, zipPath); err != nil {
		return fmt.Errorf("re-zip after stapling: %w", err)
	}
	success("Wrote %s (staple included); distribute this zip.", zipPath)
	return nil
}

// entitlementsPath resolves the entitlements plist for notarized
// signing: --entitlements' file, or a generated empty-dict plist in a
// temp file. The returned cleanup removes the generated file and is a
// no-op for a caller-provided path.
func entitlementsPath(f desktopBuildFlags) (plistPath string, cleanup func(), err error) {
	if f.entitlements != "" {
		return f.entitlements, func() {}, nil
	}
	tf, err := os.CreateTemp("", "gofastr-entitlements-*.plist")
	if err != nil {
		return "", nil, fmt.Errorf("write generated entitlements: %w", err)
	}
	if _, err := tf.WriteString(generatedEntitlements); err != nil {
		tf.Close()
		_ = os.Remove(tf.Name())
		return "", nil, fmt.Errorf("write generated entitlements: %w", err)
	}
	if err := tf.Close(); err != nil {
		_ = os.Remove(tf.Name())
		return "", nil, fmt.Errorf("write generated entitlements: %w", err)
	}
	return tf.Name(), func() { _ = os.Remove(tf.Name()) }, nil
}
