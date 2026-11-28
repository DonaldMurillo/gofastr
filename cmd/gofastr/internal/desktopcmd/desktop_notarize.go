package desktopcmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
)

var errNotarizeRequiresIdentity = errors.New(
	"--notarize requires --sign <Developer ID Application identity>: " +
		"an ad-hoc signature cannot be notarized")

func validateNotarizeFlags(f desktopBuildFlags) error {
	if f.notarize && (f.sign == "" || f.sign == "-") {
		return errNotarizeRequiresIdentity
	}
	return nil
}

var runTool = func(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).CombinedOutput()
	//gofastr:allow(combinedparse) callers only quote the output in an error (toolDetail); none parses it
	return string(out), err
}

func toolDetail(out string, err error) error {
	if msg := strings.TrimSpace(out); msg != "" {
		return fmt.Errorf("%v\n%s", err, msg)
	}
	return err
}

const generatedEntitlements = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict/>
</plist>
`

func notarizeDesktopBundle(f desktopBuildFlags, name, appDir string) error {
	zipPath := filepath.Join(f.out, name+".zip")

	plistPath, cleanup, err := entitlementsPath(f)
	if err != nil {
		return err
	}
	defer cleanup()

	cli.Info("Signing %s with %q (hardened runtime, timestamped)...", appDir, f.sign)
	out, err := runTool("codesign", "--force", "--deep", "--sign", f.sign,
		"--options", "runtime", "--timestamp", "--entitlements", plistPath, appDir)
	if err != nil {
		return fmt.Errorf("signing: %w", toolDetail(out, err))
	}
	cli.Success("Signed %s (hardened runtime, timestamped).", appDir)

	cli.Info("Zipping %s for submission...", zipPath)
	if err := zipAppBundle(appDir, zipPath); err != nil {
		return fmt.Errorf("zip for submission: %w", err)
	}
	cli.Success("Wrote %s", zipPath)

	cli.Info("Submitting %s to Apple's notary service (profile %q, waiting)...", zipPath, f.notaryProfile)
	out, err = runTool("xcrun", "notarytool", "submit", zipPath,
		"--keychain-profile", f.notaryProfile, "--wait")
	if err != nil {
		return fmt.Errorf("notarytool submit: %w", toolDetail(out, err))
	}
	cli.Success("Notarized %s.", name)

	cli.Info("Stapling the notary ticket to %s...", appDir)
	out, err = runTool("xcrun", "stapler", "staple", appDir)
	if err != nil {
		return fmt.Errorf("stapler staple: %w", toolDetail(out, err))
	}
	cli.Success("Stapled %s.", appDir)

	cli.Info("Re-zipping %s so the archive carries the staple...", zipPath)
	if err := zipAppBundle(appDir, zipPath); err != nil {
		return fmt.Errorf("re-zip after stapling: %w", err)
	}
	cli.Success("Wrote %s (staple included); distribute this zip.", zipPath)
	return nil
}

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
