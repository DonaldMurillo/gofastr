package desktopcmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
)

func signDesktopBundle(f desktopBuildFlags, appDir string) {
	if f.noSign {
		cli.Info("The bundle is unsigned (--no-sign); macOS notifications will not work.")
		return
	}
	identity := f.sign
	if identity == "" {
		if _, err := exec.LookPath("codesign"); err != nil {
			cli.Info("The bundle is unsigned: no codesign on this host; macOS notifications will not work.")
			return
		}
		identity = "-" // ad-hoc
	}
	out, err := runTool("codesign", "--force", "--deep", "--sign", identity, appDir)
	if err != nil {
		cli.Fail("Signing failed; the bundle stays unsigned: %v", toolDetail(out, err))
		return
	}
	if f.sign == "" {
		cli.Success("Signed %s ad-hoc (codesign --sign -); notifications work after the first-run Allow.", appDir)
	} else {
		cli.Success("Signed %s with identity %q.", appDir, f.sign)
	}
}

var lookTool = exec.LookPath

func macDesktopArchs(envArch string) ([]string, error) {
	switch envArch {
	case "":
		return []string{"arm64", "amd64"}, nil
	case "arm64", "amd64":
		return []string{envArch}, nil
	}
	return nil, fmt.Errorf("macOS desktop builds target arm64 (Apple silicon), amd64 (Intel), or both when GOARCH is unset, not GOARCH=%s", envArch)
}

func buildMacExecutable(pkg, ldflags, exe string, archs []string) error {
	build := func(arch, out string) error {
		cli.Info("Building %s (darwin/%s, CGO_ENABLED=0)...", pkg, arch)
		cmd := exec.Command("go", "build", "-trimpath", "-ldflags", ldflags, "-o", out, pkg)
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=darwin", "GOARCH="+arch)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}
	if len(archs) == 1 {
		return build(archs[0], exe)
	}
	tmp, err := os.MkdirTemp("", "gofastr-desktop-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	args := []string{"-create", "-output", exe}
	for _, arch := range archs {
		slice := filepath.Join(tmp, arch)
		if err := build(arch, slice); err != nil {
			return err
		}
		args = append(args, slice)
	}
	cli.Info("Merging %s into a universal executable (lipo)...", strings.Join(archs, " and "))
	if out, err := runTool("lipo", args...); err != nil {
		return fmt.Errorf("lipo: %w", toolDetail(out, err))
	}
	return nil
}
