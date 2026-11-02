//go:build windows

package desktopcmd

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// defaultTimestampURL is the RFC 3161 server --timestamp-url defaults
// to. A timestamped signature stays valid after the certificate
// expires; an untimestamped one does not.
const defaultTimestampURL = "http://timestamp.digicert.com"

// thumbprintRe matches a certificate's SHA-1 thumbprint as the
// certificate manager and Get-ChildItem Cert:\ print it.
var thumbprintRe = regexp.MustCompile(`^[0-9A-Fa-f]{40}$`)

// signtoolArgs builds the signtool command line for one file. cert is a
// SHA-1 thumbprint (/sha1) or a subject-name substring (/n) of a code
// signing certificate in the current user's personal store;
// timestampURL "none" skips the timestamp.
func signtoolArgs(path, cert, timestampURL string) []string {
	args := []string{"sign", "/fd", "sha256"}
	if thumbprintRe.MatchString(cert) {
		args = append(args, "/sha1", cert)
	} else {
		args = append(args, "/n", cert)
	}
	if timestampURL != "none" {
		args = append(args, "/tr", timestampURL, "/td", "sha256")
	}
	return append(args, path)
}

// signWindowsExecutable signs the executable with signtool. It is the
// build's last write to the file: a resource update after it would
// break the signature. WebView2Loader.dll is left alone; it already
// carries Microsoft's signature, which the updater checks.
func signWindowsExecutable(path, cert, timestampURL string) error {
	tool, err := findSigntool()
	if err != nil {
		return err
	}
	out, err := runTool(tool, signtoolArgs(path, cert, timestampURL)...)
	if err != nil {
		return fmt.Errorf("signtool: %w\n%s", err, strings.TrimSpace(out))
	}
	return nil
}

// lookSigntool is the PATH lookup, a test seam.
var lookSigntool = func() (string, error) { return exec.LookPath("signtool.exe") }

// findSigntool finds signtool.exe on PATH, then in the newest Windows
// 10/11 SDK under Program Files (x86)\Windows Kits\10\bin for this
// host's architecture.
func findSigntool() (string, error) {
	if p, err := lookSigntool(); err == nil {
		return p, nil
	}
	arch := "x64"
	if strings.EqualFold(os.Getenv("PROCESSOR_ARCHITECTURE"), "ARM64") {
		arch = "arm64"
	}
	root := filepath.Join(os.Getenv("ProgramFiles(x86)"), "Windows Kits", "10", "bin")
	matches, _ := filepath.Glob(filepath.Join(root, "10.*", arch, "signtool.exe"))
	if len(matches) > 0 {
		slices.SortFunc(matches, compareSDKPaths)
		return matches[len(matches)-1], nil
	}
	return "", fmt.Errorf("signtool.exe is not on PATH or in %s; install the Windows SDK signing tools", root)
}

// compareSDKPaths orders ...\bin\<version>\<arch>\signtool.exe paths
// by their dotted version, numerically.
func compareSDKPaths(a, b string) int {
	va := strings.Split(filepath.Base(filepath.Dir(filepath.Dir(a))), ".")
	vb := strings.Split(filepath.Base(filepath.Dir(filepath.Dir(b))), ".")
	for i := 0; i < len(va) && i < len(vb); i++ {
		if c := compareNumeric(va[i], vb[i]); c != 0 {
			return c
		}
	}
	return len(va) - len(vb)
}

func compareNumeric(a, b string) int {
	a, b = strings.TrimLeft(a, "0"), strings.TrimLeft(b, "0")
	if len(a) != len(b) {
		return len(a) - len(b)
	}
	return strings.Compare(a, b)
}

// zipWindowsBundle writes the update archive for a Windows build: one
// <name>/ directory holding <name>.exe and WebView2Loader.dll, the shape
// the updater and `gofastr desktop feed` accept. Entries carry no
// timestamps, so two archives of the same build are byte-identical.
func zipWindowsBundle(outDir, name, dest string) error {
	// The name becomes the archive's top-level directory: one plain
	// path element, never a separator or a dot segment.
	root := path.Clean(name)
	if root != name || root == "." || root == ".." || strings.ContainsAny(root, `/\:`) {
		return fmt.Errorf("app name %q cannot name an archive directory", name)
	}
	//gofastr:allow(worldreadable) a distributed archive is a public artifact: it is the update download, holds no state or secret
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(f)
	write := func() error {
		dir := &zip.FileHeader{Name: root + "/"}
		dir.SetMode(os.ModeDir | 0o755)
		if _, err := zw.CreateHeader(dir); err != nil {
			return err
		}
		for _, file := range []string{root + ".exe", "WebView2Loader.dll"} {
			hdr := &zip.FileHeader{Name: path.Join(root, file), Method: zip.Deflate}
			hdr.SetMode(0o755)
			w, err := zw.CreateHeader(hdr)
			if err != nil {
				return err
			}
			src, err := os.Open(filepath.Join(outDir, file))
			if err != nil {
				return err
			}
			_, err = io.Copy(w, src)
			src.Close()
			if err != nil {
				return err
			}
		}
		return zw.Close()
	}
	if err := write(); err != nil {
		f.Close()
		os.Remove(dest)
		return err
	}
	return f.Close()
}
