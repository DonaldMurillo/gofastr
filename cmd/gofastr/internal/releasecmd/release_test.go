package releasecmd

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestOptionsRequireVersion(t *testing.T) {
	if _, err := parseOptions(nil); err == nil || !strings.Contains(err.Error(), "--version is required") {
		t.Fatalf("err = %v", err)
	}
}

func TestOptionsDefaults(t *testing.T) {
	opts, err := parseOptions([]string{"--version=v1.2.0-rc.1"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.version != "1.2.0-rc.1" || opts.pkg != "." || opts.versionVar != "main.version" {
		t.Errorf("opts = %+v", opts)
	}
	if opts.out != filepath.Join("dist", "release", "1.2.0-rc.1") {
		t.Errorf("out = %q", opts.out)
	}
	if !slices.Equal(opts.targets, supportedTargets) {
		t.Errorf("targets = %v", opts.targets)
	}
}

func TestOptionsAcceptSemverPrerelease(t *testing.T) {
	for _, v := range []string{"1.2.3-rc-1", "1.2.3-0.alpha", "v1.2.3-x.7.z-9", "1.2.3-" + strings.Repeat("a", 58)} {
		if _, err := parseOptions([]string{"--version=" + v}); err != nil {
			t.Errorf("%s: %v", v, err)
		}
	}
}

func TestOptionsRefuseBadValues(t *testing.T) {
	for _, args := range [][]string{
		{"--version=1.2"},
		{"--version=01.2.0"},
		{"--version=1.2.0+build"},
		{"--version=1.2.3-01"},
		{"--version=1.2.3-" + strings.Repeat("a", 59)}, // 65 bytes
		{"--version=1.2.0", "--name=a" + strings.Repeat("b", 64)},
		{"--version=1.2.0", "--name=my app"},
		{"--version=1.2.0", "--name=-x"},
		{"--version=1.2.0", "--targets=plan9/amd64"},
		{"--version=1.2.0", "--targets=linux/amd64,linux/amd64"},
		{"--version=1.2.0", "--version-var=version"},
		{"--version=1.2.0", "--include=a,,b"},
		{"--version=1.2.0", "--bogus"},
	} {
		if _, err := parseOptions(args); err == nil {
			t.Errorf("%v: accepted", args)
		}
	}
}

func TestURLRefusesUnquotableText(t *testing.T) {
	for _, u := range []string{
		"http://example.com/x",
		"https://example.com/it's",
		"https://example.com/$(id)",
		"https://example.com/`id`",
		"https://example.com/a b",
		"https://example.com/\"x\"",
		"https://exämple.com/x",
		"https://user:pw@example.com/x",
		"https://example.com/x?y=1",
		"https://example.com/x#y",
		"https://example.com/x?",
		"https://example.com/x#",
		"HTTPS://example.com/x",
		"https:///x",
		"ftp://example.com/x",
	} {
		if _, err := checkBaseURL(u); err == nil {
			t.Errorf("%q: accepted", u)
		}
	}
	got, err := checkBaseURL("https://github.com/o/r/releases/download/v1.0.0/")
	if err != nil || got != "https://github.com/o/r/releases/download/v1.0.0" {
		t.Errorf("got %q, %v", got, err)
	}
}

func TestEmitterRefusesQuoteInURL(t *testing.T) {
	v := scriptValues{Name: "app", Version: "1.0.0", BaseURL: "https://x.example/'; rm -rf ~; '", EnvPrefix: "APP"}
	if err := checkScriptValues(v); err == nil {
		t.Fatal("a quote in the base URL reached the templates")
	}
	v.BaseURL = "https://x.example/r"
	v.Unix = []scriptBuild{{Platform: "linux/amd64", Arch: "amd64", File: "a'b.tar.gz", SHA256: strings.Repeat("0", 64)}}
	if err := checkScriptValues(v); err == nil {
		t.Fatal("a quote in an archive name reached the templates")
	}
}

func TestEnvPrefixFromName(t *testing.T) {
	for in, want := range map[string]string{"myapp": "MYAPP", "my-app": "MY_APP", "my.app_2": "MY_APP_2", "myapp-": "MYAPP"} {
		if got := envPrefix(in); got != want {
			t.Errorf("envPrefix(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDefaultNameSkipsMajorSuffix(t *testing.T) {
	for in, want := range map[string]string{
		"example.com/app":            "app",
		"example.com/app/v2":         "app",
		"example.com/app/v10":        "app",
		"example.com/app/v1":         "v1",
		"example.com/app/v0":         "v0",
		"example.com/app/v02":        "v02",
		"example.com/app/cmd/server": "server",
	} {
		if got := defaultName(in); got != want {
			t.Errorf("defaultName(%q) = %q, want %q", in, got, want)
		}
	}
}

// writeModule writes a module whose main package prints its version.
func writeModule(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	if _, ok := files["go.mod"]; !ok {
		files["go.mod"] = "module example.com/hello\n\ngo 1.24\n"
	}
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	return dir
}

const helloMain = `package main

import "fmt"

var version = "dev"

func main() { fmt.Println("hello " + version) }
`

func hostTarget(t *testing.T) target {
	t.Helper()
	ht := target{runtime.GOOS, runtime.GOARCH}
	if !slices.Contains(supportedTargets, ht) {
		t.Skipf("host %s is not a release target", ht)
	}
	return ht
}

func TestReleaseRefusesLibraryPackage(t *testing.T) {
	dir := writeModule(t, map[string]string{"lib.go": "package hello\n"})
	opts, _ := parseOptions([]string{"--version=1.0.0", "-o", t.TempDir()})
	if _, err := release(dir, opts, io.Discard); err == nil || !strings.Contains(err.Error(), "not a main package") {
		t.Fatalf("err = %v", err)
	}
}

func TestReleaseRefusesServiceWithoutBattery(t *testing.T) {
	dir := writeModule(t, map[string]string{"main.go": helloMain})
	opts, _ := parseOptions([]string{"--version=1.0.0", "--service", "--targets=linux/amd64", "-o", t.TempDir()})
	if _, err := release(dir, opts, io.Discard); err == nil || !strings.Contains(err.Error(), "does not use battery/service") {
		t.Fatalf("err = %v", err)
	}
}

// The archives unpack onto case-insensitive file systems, where an
// include named HELLO overwrites the binary hello.
func TestIncludeCannotAliasBinary(t *testing.T) {
	dir := writeModule(t, map[string]string{"main.go": helloMain, "HELLO": "x", "HELLO.EXE": "x", "readme.md": "x", "README.md": "x", "aux.txt": "x"})
	for _, c := range []struct{ include, targets string }{
		{"HELLO", "darwin/arm64"},
		{"HELLO.EXE", "windows/amd64"},
		{"readme.md,README.md", "linux/amd64"},
		{"aux.txt", "windows/amd64"},
	} {
		opts, err := parseOptions([]string{"--version=1.0.0", "--name=hello", "--targets=" + c.targets, "--include=" + c.include, "-o", t.TempDir()})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := release(dir, opts, io.Discard); err == nil || !strings.Contains(err.Error(), "--include") {
			t.Errorf("--include=%s for %s: err = %v", c.include, c.targets, err)
		}
	}
}

func TestIncludeRefusesNonRegularOrOddName(t *testing.T) {
	dir := writeModule(t, map[string]string{"main.go": helloMain, "docs/a.md": "x", "notes v2.txt": "x", "café.txt": "x"})
	includes := []string{"docs", "notes v2.txt", "café.txt"}
	if err := os.Symlink("main.go", filepath.Join(dir, "link.go")); err == nil {
		includes = append(includes, "link.go")
	} else if runtime.GOOS != "windows" {
		t.Fatal(err)
	}
	for _, inc := range includes {
		if _, err := resolveIncludes(dir, []string{inc}, "hello", false); err == nil {
			t.Errorf("--include=%s: accepted", inc)
		}
	}
	if _, err := resolveIncludes(dir, []string{"main.go"}, "hello", false); err != nil {
		t.Errorf("a plain file was refused: %v", err)
	}
}

func TestReleaseRefusesUnusableDefaultName(t *testing.T) {
	dir := writeModule(t, map[string]string{"main.go": helloMain, "go.mod": "module example.com/9lives\n\ngo 1.24\n"})
	opts, _ := parseOptions([]string{"--version=1.0.0", "--targets=linux/amd64", "-o", t.TempDir()})
	if _, err := release(dir, opts, io.Discard); err == nil || !strings.Contains(err.Error(), "pass --name") {
		t.Fatalf("err = %v", err)
	}
}

// --service needs battery/service on every target; one target without it
// is enough to refuse. A warning fires when any target imports it.
func TestServiceNeedsEveryTarget(t *testing.T) {
	both := pkgInfo{usesService: true, importsService: true}
	none := pkgInfo{}
	if got := mergeTargetInfo(both, none); got.usesService || !got.importsService {
		t.Errorf("service on one target only: %+v", got)
	}
	if got := mergeTargetInfo(none, pkgInfo{importsDesktop: true}); !got.importsDesktop {
		t.Errorf("desktop on one target was dropped: %+v", got)
	}
	if got := mergeTargetInfo(both, both); !got.usesService {
		t.Errorf("service on every target: %+v", got)
	}
}

func TestReleaseRefusesWindowsDeviceName(t *testing.T) {
	dir := writeModule(t, map[string]string{"main.go": helloMain})
	opts, _ := parseOptions([]string{"--version=1.0.0", "--name=aux", "--targets=windows/amd64", "-o", t.TempDir()})
	if _, err := release(dir, opts, io.Discard); err == nil || !strings.Contains(err.Error(), "reserved name on Windows") {
		t.Fatalf("err = %v", err)
	}
}

// A package whose only file builds for its target releases from any host.
func TestReleaseBuildsForAnotherOS(t *testing.T) {
	other := target{"linux", "amd64"}
	if runtime.GOOS == "linux" {
		other = target{"darwin", "arm64"}
	}
	dir := writeModule(t, map[string]string{"main_" + other.goos + ".go": helloMain})
	opts, _ := parseOptions([]string{"--version=1.0.0", "--targets=" + other.String(), "-o", t.TempDir()})
	if _, err := release(dir, opts, io.Discard); err != nil {
		t.Fatal(err)
	}
}

// A failed run removes only what it created: a second release writing
// into the same directory keeps its files.
func TestCleanupKeepsOtherFiles(t *testing.T) {
	dir := t.TempDir()
	r, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	o := &outDir{root: r}
	if err := writeNew(o, "mine.tar.gz", []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "theirs.tar.gz"), []byte("y"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeNew(o, "theirs.tar.gz", []byte("x"), 0o600); err == nil {
		t.Fatal("overwrote a file this run did not create")
	}
	o.removeCreated()
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != "theirs.tar.gz" {
		t.Fatalf("left %v; want only theirs.tar.gz", entries)
	}
}

func TestReleaseRefusesNonEmptyOut(t *testing.T) {
	dir := writeModule(t, map[string]string{"main.go": helloMain})
	out := t.TempDir()
	if err := os.WriteFile(filepath.Join(out, "old.tar.gz"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	opts, _ := parseOptions([]string{"--version=1.0.0", "--targets=linux/amd64", "-o", out})
	if _, err := release(dir, opts, io.Discard); err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Fatalf("err = %v", err)
	}
}

func TestFailedReleaseLeavesOutEmpty(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"main.go":       helloMain,
		"broken_win.go": "//go:build windows\n\npackage main\n\nvar broken int = \"not an int\"\n",
	})
	out := filepath.Join(t.TempDir(), "out")
	opts, _ := parseOptions([]string{"--version=1.0.0", "--targets=linux/amd64,windows/amd64", "--url=https://example.com/dl", "-o", out})
	if _, err := release(dir, opts, io.Discard); err == nil || !strings.Contains(err.Error(), "windows/amd64") {
		t.Fatalf("err = %v, want the windows build to fail", err)
	}
	if entries, _ := os.ReadDir(out); len(entries) > 0 {
		t.Errorf("a failed release left %d files in %s", len(entries), out)
	}
}

func TestReleaseWritesArchivesAndSums(t *testing.T) {
	dir := writeModule(t, map[string]string{"main.go": helloMain, "README.md": "# hello\n"})
	run := func(out string) result {
		opts, err := parseOptions([]string{
			"--version=1.4.0", "--targets=linux/amd64,windows/arm64", "--include=README.md",
			"--url=https://example.com/dl", "-o", out,
		})
		if err != nil {
			t.Fatal(err)
		}
		res, err := release(dir, opts, io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	outA, outB := filepath.Join(t.TempDir(), "a"), filepath.Join(t.TempDir(), "b")
	res := run(outA)
	run(outB)

	want := []string{"checksums.txt", "hello_1.4.0_linux_amd64.tar.gz", "hello_1.4.0_windows_arm64.zip", "install.ps1", "install.sh"}
	entries, _ := os.ReadDir(outA)
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	if !slices.Equal(got, want) {
		t.Fatalf("files = %v, want %v", got, want)
	}
	if !slices.Equal(res.scripts, []string{"install.sh", "install.ps1"}) {
		t.Errorf("scripts = %v", res.scripts)
	}

	// Two releases of the same tree are byte-identical.
	for _, name := range want {
		a, _ := os.ReadFile(filepath.Join(outA, name))
		b, _ := os.ReadFile(filepath.Join(outB, name))
		if !bytes.Equal(a, b) {
			t.Errorf("%s differs between two releases of the same tree", name)
		}
	}

	// checksums.txt matches every file it lists, and lists every file
	// but itself.
	sums, _ := os.ReadFile(filepath.Join(outA, "checksums.txt"))
	var listed []string
	for _, line := range strings.Split(strings.TrimSpace(string(sums)), "\n") {
		sum, name, ok := strings.Cut(line, "  ")
		if !ok {
			t.Fatalf("checksums.txt line %q", line)
		}
		data, _ := os.ReadFile(filepath.Join(outA, name))
		h := sha256.Sum256(data)
		if hex.EncodeToString(h[:]) != sum {
			t.Errorf("checksum of %s is wrong", name)
		}
		listed = append(listed, name)
	}
	if !slices.Equal(listed, want[1:]) {
		t.Errorf("checksums.txt lists %v", listed)
	}

	tgz := readTarGz(t, filepath.Join(outA, "hello_1.4.0_linux_amd64.tar.gz"))
	if tgz["hello"] != 0o755 || tgz["README.md"] != 0o644 || len(tgz) != 2 {
		t.Errorf("tar entries = %v", tgz)
	}
	zr, err := zip.OpenReader(filepath.Join(outA, "hello_1.4.0_windows_arm64.zip"))
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	var zipNames []string
	for _, f := range zr.File {
		zipNames = append(zipNames, f.Name)
	}
	if !slices.Equal(zipNames, []string{"hello.exe", "README.md"}) {
		t.Errorf("zip entries = %v", zipNames)
	}

	ps1, _ := os.ReadFile(filepath.Join(outA, "install.ps1"))
	if !bytes.Contains(ps1, []byte("\r\n")) || bytes.Contains(bytes.ReplaceAll(ps1, []byte("\r\n"), nil), []byte("\n")) {
		t.Error("install.ps1 must use CRLF line endings throughout")
	}
	if !bytes.Contains(ps1, []byte("'arm64' = @('hello_1.4.0_windows_arm64.zip', '"+res.artifacts[1].sha256+"')")) {
		t.Errorf("install.ps1 does not carry the windows/arm64 archive and hash:\n%s", ps1)
	}
	sh, _ := os.ReadFile(filepath.Join(outA, "install.sh"))
	if !bytes.Contains(sh, []byte("sum='"+res.artifacts[0].sha256+"'")) {
		t.Error("install.sh does not carry the linux/amd64 hash")
	}
	if bytes.Contains(sh, []byte("proc_translated")) {
		t.Error("install.sh switches Rosetta shells to arm64 in a release with no darwin/arm64 build")
	}
	if runtime.GOOS != "windows" {
		if err := exec.Command("sh", "-n", filepath.Join(outA, "install.sh")).Run(); err != nil {
			t.Errorf("install.sh does not parse: %v", err)
		}
	}
}

func readTarGz(t *testing.T, p string) map[string]int64 {
	t.Helper()
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	modes := map[string]int64{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return modes
		}
		if err != nil {
			t.Fatal(err)
		}
		if h.Uid != 0 || h.Gid != 0 || !h.ModTime.Equal(epoch) {
			t.Errorf("%s carries an owner or a timestamp", h.Name)
		}
		modes[h.Name] = h.Mode
	}
}

// serveRelease starts an HTTPS server over a directory a release will be
// written into, and returns its URL and a CA bundle curl trusts it with.
func serveRelease(t *testing.T, root string) (string, string) {
	t.Helper()
	srv := httptest.NewTLSServer(http.FileServer(http.Dir(root)))
	t.Cleanup(srv.Close)
	ca := filepath.Join(t.TempDir(), "ca.pem")
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	if err := os.WriteFile(ca, pemBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	return srv.URL, ca
}

func runInstallSh(t *testing.T, script, ca, installDir string) ([]byte, error) {
	t.Helper()
	cmd := exec.Command("sh", script)
	cmd.Env = append(os.Environ(),
		"HOME="+t.TempDir(),
		"HELLO_INSTALL_DIR="+installDir,
		"CURL_CA_BUNDLE="+ca,
		"SSL_CERT_FILE="+ca,
	)
	return cmd.CombinedOutput()
}

func TestInstallShInstallsVerifiedBuild(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("install.sh is for macOS and Linux")
	}
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("needs curl")
	}
	ht := hostTarget(t)
	dir := writeModule(t, map[string]string{"main.go": helloMain})
	served := t.TempDir()
	url, ca := serveRelease(t, served)
	opts, err := parseOptions([]string{"--version=2.0.1", "--targets=" + ht.String(), "--url=" + url + "/v2.0.1", "-o", filepath.Join(served, "v2.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	res, err := release(dir, opts, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(res.out, "install.sh")

	installDir := filepath.Join(t.TempDir(), "bin")
	out, err := runInstallSh(t, script, ca, installDir)
	if err != nil {
		t.Fatalf("install.sh: %v\n%s", err, out)
	}
	got, err := exec.Command(filepath.Join(installDir, "hello")).Output()
	if err != nil || string(got) != "hello 2.0.1\n" {
		t.Fatalf("installed binary printed %q, %v", got, err)
	}
	if left, _ := filepath.Glob(filepath.Join(installDir, ".hello.install.*")); len(left) > 0 {
		t.Errorf("staged copies left behind: %v", left)
	}

	// A download whose bytes differ from the build is refused, and
	// nothing is installed.
	archive := filepath.Join(res.out, res.artifacts[0].file)
	data, _ := os.ReadFile(archive)
	data[len(data)/2] ^= 0xff
	if err := os.WriteFile(archive, data, 0o600); err != nil {
		t.Fatal(err)
	}
	fresh := filepath.Join(t.TempDir(), "bin")
	out, err = runInstallSh(t, script, ca, fresh)
	if err == nil || !bytes.Contains(out, []byte("checksum mismatch")) {
		t.Fatalf("tampered archive: err = %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(fresh, "hello")); !os.IsNotExist(err) {
		t.Errorf("a tampered archive installed a binary (stat err %v)", err)
	}
}

// powershell runs a command in Windows PowerShell 5.1, the one every
// Windows 10 machine has.
func powershell(t *testing.T, env []string, command string) ([]byte, error) {
	t.Helper()
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", command)
	cmd.Env = append(os.Environ(), env...)
	return cmd.CombinedOutput()
}

const userPathCmd = `(Get-Item 'HKCU:\Environment').GetValue('Path', '', 'DoNotExpandEnvironmentNames')`

// ps1TrustCert trusts the certificate with DER bytes der, and only it,
// for one PowerShell process. The callback is compiled C#: a PowerShell
// scriptblock callback runs on a .NET thread with no runspace and fails
// the handshake ("An unexpected error occurred on a send").
func ps1TrustCert(der []byte) string {
	sum := sha1.Sum(der)
	return `Add-Type -TypeDefinition 'public static class TestTrust { public static string Hash; public static bool Check(object s, System.Security.Cryptography.X509Certificates.X509Certificate c, System.Security.Cryptography.X509Certificates.X509Chain ch, System.Net.Security.SslPolicyErrors e) { return c != null && c.GetCertHashString() == Hash; } }'; ` +
		`[TestTrust]::Hash = '` + strings.ToUpper(hex.EncodeToString(sum[:])) + `'; ` +
		`[Net.ServicePointManager]::ServerCertificateValidationCallback = [Delegate]::CreateDelegate([Net.Security.RemoteCertificateValidationCallback], [TestTrust].GetMethod('Check')); `
}

// ps1ErrorChain prints a caught exception and every inner one, so a
// failed download names its cause instead of only the outer message.
const ps1ErrorChain = `$e = $_.Exception; while ($e) { [Console]::Error.WriteLine($e.GetType().FullName + ': ' + $e.Message); $e = $e.InnerException }`

func TestInstallPs1InstallsVerifiedBuild(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("install.ps1 is for Windows")
	}
	if _, err := exec.LookPath("powershell.exe"); err != nil {
		t.Skip("needs powershell.exe")
	}
	ht := hostTarget(t)
	dir := writeModule(t, map[string]string{"main.go": helloMain})
	served := t.TempDir()
	srv := httptest.NewTLSServer(http.FileServer(http.Dir(served)))
	t.Cleanup(srv.Close)
	opts, err := parseOptions([]string{"--version=2.0.1", "--targets=" + ht.String(), "--url=" + srv.URL + "/v2.0.1", "-o", filepath.Join(served, "v2.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	res, err := release(dir, opts, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	run := ps1TrustCert(srv.Certificate().Raw) + "try { & '" + filepath.Join(res.out, "install.ps1") + "' } catch { " + ps1ErrorChain + "; exit 1 }"

	installDir := filepath.Join(t.TempDir(), "bin")
	env := []string{"HELLO_INSTALL_DIR=" + installDir, "HELLO_NO_MODIFY_PATH=1"}
	for i := range 2 { // the second run replaces the installed copy
		if out, err := powershell(t, env, run); err != nil {
			t.Fatalf("install.ps1 run %d: %v\n%s", i+1, err, out)
		}
	}
	got, err := exec.Command(filepath.Join(installDir, "hello.exe")).Output()
	if err != nil || string(got) != "hello 2.0.1\n" {
		t.Fatalf("installed binary printed %q, %v", got, err)
	}
	if left, _ := filepath.Glob(filepath.Join(installDir, "hello.exe.*")); len(left) > 0 {
		t.Errorf("the moved-aside copy was left behind: %v", left)
	}

	// On a CI runner, also let the script add the directory to the user's
	// Path, then put the old value back. A developer machine keeps its Path.
	if os.Getenv("GITHUB_ACTIONS") == "true" {
		before, err := powershell(t, nil, userPathCmd)
		if err != nil {
			t.Fatalf("read user Path: %v\n%s", err, before)
		}
		orig := strings.TrimRight(string(before), "\r\n")
		t.Cleanup(func() {
			restore := `$k = [Microsoft.Win32.Registry]::CurrentUser.CreateSubKey('Environment'); $k.SetValue('Path', $env:ORIG_PATH, 'ExpandString'); $k.Close()`
			if out, err := powershell(t, []string{"ORIG_PATH=" + orig}, restore); err != nil {
				t.Errorf("restore user Path: %v\n%s", err, out)
			}
		})
		pathDir := filepath.Join(t.TempDir(), "pathbin")
		if out, err := powershell(t, []string{"HELLO_INSTALL_DIR=" + pathDir}, run); err != nil {
			t.Fatalf("install.ps1 with Path edit: %v\n%s", err, out)
		}
		after, _ := powershell(t, nil, userPathCmd)
		if !strings.Contains(string(after), pathDir) || (orig != "" && !strings.Contains(string(after), orig)) {
			t.Errorf("user Path after install = %q; want the old value plus %s", after, pathDir)
		}
	}

	// A download whose bytes differ from the build is refused, and
	// nothing is installed.
	archive := filepath.Join(res.out, res.artifacts[0].file)
	data, _ := os.ReadFile(archive)
	data[len(data)/2] ^= 0xff
	if err := os.WriteFile(archive, data, 0o600); err != nil {
		t.Fatal(err)
	}
	fresh := filepath.Join(t.TempDir(), "bin")
	out, err := powershell(t, []string{"HELLO_INSTALL_DIR=" + fresh, "HELLO_NO_MODIFY_PATH=1"}, run)
	if err == nil || !bytes.Contains(out, []byte("checksum mismatch")) {
		t.Fatalf("tampered archive: err = %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(fresh, "hello.exe")); !os.IsNotExist(err) {
		t.Errorf("a tampered archive installed a binary (stat err %v)", err)
	}
}

// TestReleaseFileArgNeedsName: --pkg=./main.go lists as the synthetic
// package command-line-arguments, which the release took as the binary
// name for archives, checksums and the install script.
func TestReleaseFileArgNeedsName(t *testing.T) {
	dir := writeModule(t, map[string]string{"main.go": helloMain})
	opts, _ := parseOptions([]string{"--version=1.0.0", "--targets=linux/amd64", "--pkg=./main.go", "-o", t.TempDir()})
	if _, err := release(dir, opts, io.Discard); err == nil || !strings.Contains(err.Error(), "pass --name") {
		t.Fatalf("a .go file --pkg with no --name: err = %v, want a pass --name refusal", err)
	}
}
