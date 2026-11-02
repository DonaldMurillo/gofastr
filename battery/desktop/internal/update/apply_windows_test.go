//go:build windows

package update

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/DonaldMurillo/gofastr/internal/procalive"
	"github.com/DonaldMurillo/gofastr/internal/winversion"
)

// embedVersion writes a version resource into the PE at path, the way
// `gofastr desktop build` does (BeginUpdateResource / UpdateResource /
// EndUpdateResource on RT_VERSION 1).
func embedVersion(t *testing.T, path string, info winversion.Info) {
	t.Helper()
	k32 := syscall.NewLazyDLL("kernel32.dll")
	begin, update, end := k32.NewProc("BeginUpdateResourceW"), k32.NewProc("UpdateResourceW"), k32.NewProc("EndUpdateResourceW")
	wpath, _ := syscall.UTF16PtrFromString(path)
	h, _, err := begin.Call(uintptr(unsafe.Pointer(wpath)), 0)
	if h == 0 {
		t.Fatalf("BeginUpdateResourceW: %v", err)
	}
	data := winversion.Render(info)
	ok, _, err := update.Call(h, 16, 1, 0x0409, uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)))
	runtime.KeepAlive(data)
	if ok == 0 {
		t.Fatalf("UpdateResourceW: %v", err)
	}
	if ok, _, err := end.Call(h, 0); ok == 0 {
		t.Fatalf("EndUpdateResourceW: %v", err)
	}
	runtime.KeepAlive(wpath)
}

// fakeBuild copies this test binary to dir/<name>.exe with a version
// resource and writes a loader DLL beside it: the layout
// `gofastr desktop build` produces.
func fakeBuild(t *testing.T, dir, name, id, version string) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, name+".exe")
	if err := os.WriteFile(exe, data, 0o700); err != nil {
		t.Fatal(err)
	}
	embedVersion(t, exe, winversion.Info{Name: name, ID: id, Version: version})
	if err := os.WriteFile(filepath.Join(dir, WebView2LoaderName), []byte("loader "+version), 0o600); err != nil {
		t.Fatal(err)
	}
	return exe
}

// bundledPlatform is a windowsPlatform pinned to exe instead of the
// test binary.
func bundledPlatform(t *testing.T, exe string) *windowsPlatform {
	t.Helper()
	info, err := ReadExeInfo(exe)
	if err != nil {
		t.Fatal(err)
	}
	p := &windowsPlatform{}
	p.once.Do(func() {})
	p.exe, p.info, p.ok = exe, info, true
	return p
}

func TestWindowsPlatformUnbundledTestBinary(t *testing.T) {
	p := DefaultPlatform(nil, nil)
	if v := p.Version(); v != "" {
		t.Fatalf("a test binary reports version %q", v)
	}
	if _, _, ok := p.Bundle(); ok {
		t.Fatal("a test binary reports a bundle")
	}
	if _, ok := p.(FileLayout).Units(); ok {
		t.Fatal("a test binary reports install units")
	}
	if err := p.VerifySignature(t.TempDir()); !errors.Is(err, ErrUnsupportedApply) {
		t.Fatalf("VerifySignature = %v, want ErrUnsupportedApply", err)
	}
}

func TestWindowsPlatformReadsTheBuild(t *testing.T) {
	dir := t.TempDir()
	exe := fakeBuild(t, dir, "Notes", "dev.gofastr.notes", "1.2.0")
	p := bundledPlatform(t, exe)
	if p.Version() != "1.2.0" || p.Identifier() != "dev.gofastr.notes" {
		t.Fatalf("identity = %q %q", p.Version(), p.Identifier())
	}
	name, bdir, ok := p.Bundle()
	if !ok || name != "Notes" || bdir != dir {
		t.Fatalf("Bundle = %q %q %v", name, bdir, ok)
	}
	units, ok := p.Units()
	if !ok || len(units) != 2 || units[0] != (Unit{Rel: "Notes.exe", Current: exe}) ||
		units[1] != (Unit{Rel: WebView2LoaderName, Current: filepath.Join(dir, WebView2LoaderName)}) {
		t.Fatalf("Units = %+v %v", units, ok)
	}
}

func TestWindowsPlatformInspect(t *testing.T) {
	p := bundledPlatform(t, fakeBuild(t, t.TempDir(), "Notes", "dev.gofastr.notes", "1.2.0"))

	good := t.TempDir()
	fakeBuild(t, good, "Notes", "dev.gofastr.notes", "1.3.0")
	info, err := p.Inspect(good)
	if err != nil || info.ShortVersion != "1.3.0" || info.Identifier != "dev.gofastr.notes" {
		t.Fatalf("Inspect = %+v, %v", info, err)
	}

	stray := t.TempDir()
	fakeBuild(t, stray, "Notes", "dev.gofastr.notes", "1.3.0")
	if err := os.WriteFile(filepath.Join(stray, "README.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Inspect(stray); !errors.Is(err, ErrBadArchive) {
		t.Fatalf("a stray file: err = %v, want ErrBadArchive", err)
	}

	noLoader := t.TempDir()
	fakeBuild(t, noLoader, "Notes", "dev.gofastr.notes", "1.3.0")
	if err := os.Remove(filepath.Join(noLoader, WebView2LoaderName)); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Inspect(noLoader); !errors.Is(err, ErrBadArchive) {
		t.Fatalf("no loader: err = %v, want ErrBadArchive", err)
	}

	renamed := t.TempDir()
	fakeBuild(t, renamed, "Other", "dev.gofastr.notes", "1.3.0")
	if _, err := p.Inspect(renamed); !errors.Is(err, ErrBadArchive) {
		t.Fatalf("another executable name: err = %v, want ErrBadArchive", err)
	}

	// A plain exe with no GoFastrAppID: not a desktop build.
	bare := t.TempDir()
	self, _ := os.Executable()
	data, _ := os.ReadFile(self)
	if err := os.WriteFile(filepath.Join(bare, "Notes.exe"), data, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bare, WebView2LoaderName), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Inspect(bare); !errors.Is(err, ErrBadBundleInfo) {
		t.Fatalf("no version resource: err = %v, want ErrBadBundleInfo", err)
	}
}

func TestWindowsPlatformVerifySignatureAppliesThePolicy(t *testing.T) {
	dir := t.TempDir()
	exe := fakeBuild(t, dir, "Notes", "dev.gofastr.notes", "1.2.0")
	extracted := t.TempDir()
	fakeBuild(t, extracted, "Notes", "dev.gofastr.notes", "1.3.0")

	newLoader := filepath.Join(extracted, WebView2LoaderName)
	results := map[string]Trust{
		exe:                                    {State: Trusted, Subject: []byte("CN=Acme")},
		filepath.Join(dir, WebView2LoaderName): {State: Trusted, Subject: []byte("CN=Microsoft")},
		filepath.Join(extracted, "Notes.exe"):  {State: Trusted, Subject: []byte("CN=Other")},
		newLoader:                              {State: Trusted, Subject: []byte("CN=Microsoft")},
	}
	var checked []string
	p := bundledPlatform(t, exe)
	p.trust = func(path string) Trust {
		checked = append(checked, path)
		return results[path]
	}
	if err := p.VerifySignature(extracted); !errors.Is(err, ErrSignature) {
		t.Fatalf("another publisher: err = %v, want ErrSignature", err)
	}
	if len(checked) != 4 {
		t.Fatalf("checked %v, want both executables and both loaders", checked)
	}

	// Same publisher on the exe, but a loader signed by someone else:
	// the installed loader pins it.
	results[filepath.Join(extracted, "Notes.exe")] = Trust{State: Trusted, Subject: []byte("CN=Acme")}
	results[newLoader] = Trust{State: Trusted, Subject: []byte("CN=Other")}
	if err := p.VerifySignature(extracted); !errors.Is(err, ErrSignature) {
		t.Fatalf("loader from another publisher: err = %v, want ErrSignature", err)
	}
	results[newLoader] = Trust{State: Trusted, Subject: []byte("CN=Microsoft")}

	results[exe] = Trust{State: Unsigned}
	results[filepath.Join(extracted, "Notes.exe")] = Trust{State: Unsigned}
	var warned []string
	p.SetWarn(func(msg string) { warned = append(warned, msg) })
	if err := p.VerifySignature(extracted); err != nil {
		t.Fatalf("unsigned to unsigned: %v", err)
	}
	if len(warned) != 1 || !strings.Contains(warned[0], "feed signature alone") {
		t.Fatalf("warned = %q", warned)
	}
}

func TestWindowsRelaunchNamesThePredecessor(t *testing.T) {
	t.Setenv(PredecessorEnv, "999999")
	exe := fakeBuild(t, t.TempDir(), "Notes", "dev.gofastr.notes", "1.2.0")
	p := bundledPlatform(t, exe)
	var gotExe string
	var gotEnv []string
	p.start = func(e string, env []string) error { gotExe, gotEnv = e, env; return nil }
	if err := p.Relaunch(exe); err != nil {
		t.Fatal(err)
	}
	if gotExe != exe {
		t.Fatalf("started %q, want %q", gotExe, exe)
	}
	var found []string
	for _, kv := range gotEnv {
		if strings.HasPrefix(strings.ToUpper(kv), PredecessorEnv+"=") {
			found = append(found, kv)
		}
	}
	if want := PredecessorEnv + "=" + strconv.Itoa(os.Getpid()); len(found) != 1 || found[0] != want {
		t.Fatalf("predecessor entries = %q, want exactly %q", found, want)
	}
}

// TestHelperProcessSleeps is the child the predecessor test waits on.
func TestHelperProcessSleeps(t *testing.T) {
	if os.Getenv("GOFASTR_UPDATE_HELPER_SLEEP") == "" {
		t.Skip("helper process only")
	}
	time.Sleep(700 * time.Millisecond)
}

func TestAwaitPredecessorWaitsForExit(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(self, "-test.run=^TestHelperProcessSleeps$")
	cmd.Env = append(os.Environ(), "GOFASTR_UPDATE_HELPER_SLEEP=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Wait() }()

	t.Setenv(PredecessorEnv, strconv.Itoa(cmd.Process.Pid))
	start := time.Now()
	if !AwaitPredecessor() {
		t.Fatal("AwaitPredecessor did not wait on a live predecessor")
	}
	// The child sleeps 700 ms; returning sooner, or with it still
	// alive, means the wait did not happen.
	if procalive.Alive(cmd.Process.Pid) {
		t.Fatal("AwaitPredecessor returned while the predecessor was still running")
	}
	if elapsed := time.Since(start); elapsed < 300*time.Millisecond {
		t.Fatalf("AwaitPredecessor returned after %v, before the predecessor could exit", elapsed)
	}
	if _, ok := os.LookupEnv(PredecessorEnv); ok {
		t.Fatal("the variable is still set for the app's children")
	}
}

func TestAwaitPredecessorIgnoresJunk(t *testing.T) {
	for _, v := range []string{"", "x", "-4", "0", strconv.Itoa(os.Getpid())} {
		t.Setenv(PredecessorEnv, v)
		if AwaitPredecessor() {
			t.Errorf("waited on %q", v)
		}
		if _, ok := os.LookupEnv(PredecessorEnv); ok {
			t.Errorf("%q: the variable is still set", v)
		}
	}
}

// signedFixture finds a file with an embedded, trusted Authenticode
// signature: the WebView2 loader from the build's package cache, or
// the WebView2 runtime's own executable.
func signedFixture(t *testing.T) string {
	t.Helper()
	if cache, err := os.UserCacheDir(); err == nil {
		pkgs, _ := filepath.Glob(filepath.Join(cache, "gofastr", "webview2", "*", "microsoft.web.webview2.nupkg"))
		for _, pkg := range pkgs {
			if dll := extractLoader(t, pkg); dll != "" {
				return dll
			}
		}
	}
	matches, _ := filepath.Glob(`C:\Program Files (x86)\Microsoft\EdgeWebView\Application\*\msedgewebview2.exe`)
	if len(matches) > 0 {
		return matches[0]
	}
	t.Skip("no embedded-signed fixture on this host (build a Windows desktop app once, or install the WebView2 runtime)")
	return ""
}

func extractLoader(t *testing.T, pkg string) string {
	zr, err := zip.OpenReader(pkg)
	if err != nil {
		return ""
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name != "build/native/x64/"+WebView2LoaderName {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return ""
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return ""
		}
		out := filepath.Join(t.TempDir(), WebView2LoaderName)
		if err := os.WriteFile(out, data, 0o600); err != nil {
			return ""
		}
		return out
	}
	return ""
}

func TestVerifyAuthenticodeOnRealFiles(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if got := VerifyAuthenticode(self); got.State != Unsigned {
		t.Fatalf("the unsigned test binary: %+v", got)
	}

	signed := signedFixture(t)
	good := VerifyAuthenticode(signed)
	if good.State != Trusted || !bytes.Contains(good.Subject, []byte("Microsoft Corporation")) {
		t.Fatalf("a Microsoft-signed file: state %d, subject %q, detail %q", good.State, good.Subject, good.Detail)
	}

	// Flip one byte in the middle of the image: the digest no longer
	// matches, the signer is still readable.
	data, err := os.ReadFile(signed)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)/3] ^= 0xff
	tampered := filepath.Join(t.TempDir(), "tampered"+filepath.Ext(signed))
	if err := os.WriteFile(tampered, data, 0o600); err != nil {
		t.Fatal(err)
	}
	bad := VerifyAuthenticode(tampered)
	if bad.State != Untrusted {
		t.Fatalf("a tampered file: state %d, detail %q", bad.State, bad.Detail)
	}
	if err := CheckWindowsTrust(Trust{State: Unsigned}, good, bad, good); !errors.Is(err, ErrSignature) {
		t.Fatalf("the policy accepted a tampered executable: %v", err)
	}
}
