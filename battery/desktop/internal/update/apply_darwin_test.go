//go:build darwin && (arm64 || amd64)

package update

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/objc"
)

// fakeRunner records each command and answers a scripted error. out is
// what Output returns: the `codesign -d -r-` text of the running bundle.
type fakeRunner struct {
	runs   []string
	err    error
	out    string
	outErr error
}

func (f *fakeRunner) Run(name string, args ...string) error {
	f.runs = append(f.runs, name+" "+strings.Join(args, " "))
	return f.err
}

func (f *fakeRunner) Output(name string, args ...string) ([]byte, error) {
	f.runs = append(f.runs, name+" "+strings.Join(args, " "))
	return []byte(f.out), f.outErr
}

func inlineMain(fn func()) error {
	fn()
	return nil
}

// A Developer ID signature's designated requirement, as codesign prints
// it, and an ad-hoc one's.
const (
	developerIDReq = `identifier "dev.gofastr.notes" and anchor apple generic and certificate 1[field.1.2.840.113635.100.6.2.6] /* exists */ and certificate leaf[field.1.2.840.113635.100.6.1.13] /* exists */ and certificate leaf[subject.OU] = ABCDE12345`
	adHocOutput    = "# designated => cdhash H\"a6add051d8c3d403a16f17a251f8a00ee5b28d0d\"\n"
)

func TestDarwinVerifyPinsRunningIdentity(t *testing.T) {
	runner := &fakeRunner{out: "designated => " + developerIDReq + "\n"}
	p := &darwinPlatform{onMain: inlineMain, runner: runner, running: "/Applications/Notes.app"}
	if err := p.VerifySignature("/tmp/Notes.app"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"codesign -d -r- /Applications/Notes.app",
		"codesign --verify --deep --strict -R=" + developerIDReq + " /tmp/Notes.app",
	}
	if !slices.Equal(runner.runs, want) {
		t.Fatalf("commands = %q\nwant %q", runner.runs, want)
	}
	// A failing codesign is reported, not swallowed.
	failing := &fakeRunner{out: runner.out, err: errFakeCommand}
	p = &darwinPlatform{onMain: inlineMain, runner: failing, running: "/Applications/Notes.app"}
	if err := p.VerifySignature("/tmp/Notes.app"); err == nil {
		t.Fatal("failing codesign accepted")
	}
}

func TestDarwinVerifyPinsTeamID(t *testing.T) {
	// The configured team wins over the running bundle's requirement,
	// which is not even read.
	runner := &fakeRunner{out: "designated => " + developerIDReq + "\n"}
	p := &darwinPlatform{onMain: inlineMain, runner: runner, running: "/Applications/Notes.app"}
	p.SetSignaturePolicy(SignaturePolicy{TeamID: "ZYXWV98765"})
	if err := p.VerifySignature("/tmp/Notes.app"); err != nil {
		t.Fatal(err)
	}
	want := []string{`codesign --verify --deep --strict -R=anchor apple generic and certificate leaf[subject.OU] = "ZYXWV98765" /tmp/Notes.app`}
	if !slices.Equal(runner.runs, want) {
		t.Fatalf("commands = %q\nwant %q", runner.runs, want)
	}
}

func TestDarwinVerifyRefusesNoIdentity(t *testing.T) {
	for name, runner := range map[string]*fakeRunner{
		"ad-hoc":    {out: adHocOutput},
		"unsigned":  {outErr: errFakeCommand},
		"no anchor": {out: "designated => identifier \"dev.gofastr.notes\"\n"},
	} {
		t.Run(name, func(t *testing.T) {
			p := &darwinPlatform{onMain: inlineMain, runner: runner, running: "/Applications/Notes.app"}
			err := p.VerifySignature("/tmp/Notes.app")
			if !errors.Is(err, ErrSignature) {
				t.Fatalf("err = %v, want ErrSignature", err)
			}
			for _, run := range runner.runs {
				if strings.Contains(run, "--verify") {
					t.Fatalf("the bundle was verified with nothing to hold it to: %q", runner.runs)
				}
			}
		})
	}
}

func TestDarwinAllowAdHocWarns(t *testing.T) {
	runner := &fakeRunner{out: adHocOutput}
	var warned []string
	p := &darwinPlatform{onMain: inlineMain, runner: runner, running: "/Applications/Notes.app"}
	p.SetSignaturePolicy(SignaturePolicy{AllowAdHoc: true})
	p.SetWarn(func(msg string) { warned = append(warned, msg) })
	if err := p.VerifySignature("/tmp/Notes.app"); err != nil {
		t.Fatal(err)
	}
	if got := runner.runs[len(runner.runs)-1]; got != "codesign --verify --deep --strict /tmp/Notes.app" {
		t.Fatalf("verify = %q", got)
	}
	if len(warned) != 1 || !strings.Contains(warned[0], "feed signature alone") {
		t.Fatalf("warnings = %q", warned)
	}
	// The opt-out never loosens a signed app: its identity still pins.
	signed := &fakeRunner{out: "designated => " + developerIDReq + "\n"}
	p = &darwinPlatform{onMain: inlineMain, runner: signed, running: "/Applications/Notes.app"}
	p.SetSignaturePolicy(SignaturePolicy{AllowAdHoc: true})
	if err := p.VerifySignature("/tmp/Notes.app"); err != nil {
		t.Fatal(err)
	}
	if got := signed.runs[len(signed.runs)-1]; !strings.Contains(got, "-R="+developerIDReq) {
		t.Fatalf("a signed app under AllowAdHoc verified unpinned: %q", got)
	}
}

func TestDarwinVerifyUnbundledUnsupported(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if _, err := objc.DlopenGlobal("/System/Library/Frameworks/Foundation.framework/Foundation"); err != nil {
		t.Skipf("Foundation unavailable: %v", err)
	}
	runner := &fakeRunner{}
	err := DefaultPlatform(inlineMain, runner).VerifySignature("/tmp/Notes.app")
	if !errors.Is(err, ErrUnsupportedApply) || len(runner.runs) != 0 {
		t.Fatalf("err = %v, runs = %q; want ErrUnsupportedApply and no command", err, runner.runs)
	}
}

// signedBundle writes a minimal .app and signs it with identity ("-"
// is ad-hoc) using the real codesign.
func signedBundle(t *testing.T, identifier, identity string) string {
	t.Helper()
	app := filepath.Join(t.TempDir(), "Notes.app")
	if err := os.MkdirAll(filepath.Join(app, "Contents", "MacOS"), 0o755); err != nil {
		t.Fatal(err)
	}
	bin, err := os.ReadFile("/usr/bin/true")
	if err != nil {
		t.Skipf("no binary to bundle: %v", err)
	}
	if err := os.WriteFile(filepath.Join(app, "Contents", "MacOS", "Notes"), bin, 0o755); err != nil {
		t.Fatal(err)
	}
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict>
<key>CFBundleIdentifier</key><string>` + identifier + `</string>
<key>CFBundleExecutable</key><string>Notes</string>
<key>CFBundleShortVersionString</key><string>1.0.0</string>
</dict></plist>`
	if err := os.WriteFile(filepath.Join(app, "Contents", "Info.plist"), []byte(plist), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("codesign", "--force", "--sign", identity, app).CombinedOutput(); err != nil {
		t.Skipf("codesign --sign %s unavailable: %v: %s", identity, err, out)
	}
	return app
}

// The real codesign, on real bundles: an ad-hoc signed download passes
// the bare check the updater used to run, and fails every pin.
func TestDarwinAdHocBundleFailsThePin(t *testing.T) {
	if _, err := exec.LookPath("codesign"); err != nil {
		t.Skip("codesign not on PATH")
	}
	running := signedBundle(t, "dev.gofastr.notes", "-")
	download := signedBundle(t, "dev.gofastr.notes", "-")

	// What the old check ran: any valid signature passes.
	if err := (ExecRunner{}).Run("codesign", "--verify", "--deep", "--strict", download); err != nil {
		t.Fatalf("the ad-hoc bundle should carry a valid signature: %v", err)
	}

	pinned := &darwinPlatform{onMain: inlineMain, runner: ExecRunner{}, running: running}
	pinned.SetSignaturePolicy(SignaturePolicy{TeamID: "ABCDE12345"})
	err := pinned.VerifySignature(download)
	if err == nil {
		t.Fatal("an ad-hoc bundle passed a Team ID pin")
	}
	// codesign must have judged the signature, not choked on the
	// requirement text.
	if !strings.Contains(err.Error(), "failed to satisfy") {
		t.Fatalf("codesign did not evaluate the requirement: %v", err)
	}

	// Default policy, ad-hoc running app: nothing to pin, refused.
	def := &darwinPlatform{onMain: inlineMain, runner: ExecRunner{}, running: running}
	if err := def.VerifySignature(download); !errors.Is(err, ErrSignature) {
		t.Fatalf("default policy on an ad-hoc app: err = %v, want ErrSignature", err)
	}

	// The opt-out accepts it, and says so.
	var warned int
	open := &darwinPlatform{onMain: inlineMain, runner: ExecRunner{}, running: running}
	open.SetSignaturePolicy(SignaturePolicy{AllowAdHoc: true})
	open.SetWarn(func(string) { warned++ })
	if err := open.VerifySignature(download); err != nil || warned != 1 {
		t.Fatalf("AllowAdHoc: err = %v, warnings = %d", err, warned)
	}
}

// The real codesign on bundles that carry a real identity: two apps
// macOS ships. An app's designated requirement, read back and passed
// to codesign -R, admits that app and refuses a different one and an
// ad-hoc build claiming its bundle id.
func TestDarwinIdentityPinRoundTrip(t *testing.T) {
	const running, other = "/System/Applications/Calculator.app", "/System/Applications/TextEdit.app"
	for _, app := range []string{running, other} {
		if _, err := os.Stat(app); err != nil {
			t.Skipf("%s is not on this host", app)
		}
	}
	p := &darwinPlatform{onMain: inlineMain, runner: ExecRunner{}, running: running}
	pin, err := p.requirement()
	if err != nil || !strings.Contains(pin, "com.apple.calculator") {
		t.Fatalf("requirement = %q, %v; want Calculator's designated requirement", pin, err)
	}
	if err := p.VerifySignature(running); err != nil {
		t.Fatalf("the app failed its own designated requirement: %v", err)
	}
	if err := p.VerifySignature(other); err == nil {
		t.Fatal("another signed app passed the pin")
	}
	if err := p.VerifySignature(signedBundle(t, "com.apple.calculator", "-")); err == nil {
		t.Fatal("an ad-hoc build claiming the bundle id passed the pin")
	}
}

func TestDarwinPlatformRelaunchInvocation(t *testing.T) {
	runner := &fakeRunner{}
	p := DefaultPlatform(inlineMain, runner)
	if err := p.Relaunch("/Applications/Notes.app"); err != nil {
		t.Fatal(err)
	}
	if len(runner.runs) != 1 || runner.runs[0] != "open -n /Applications/Notes.app" {
		t.Fatalf("relaunch invocation = %v", runner.runs)
	}
}

func TestDarwinPlatformUnbundledVersionEmpty(t *testing.T) {
	// The msgsend test's recipe: Foundation is thread-safe, so lock
	// the test thread and run the platform inline instead of through
	// objc.Main (a Go test never drains the dispatch main queue, so
	// Main would time out).
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if _, err := objc.DlopenGlobal("/System/Library/Frameworks/Foundation.framework/Foundation"); err != nil {
		t.Skipf("Foundation unavailable: %v", err)
	}
	p := DefaultPlatform(inlineMain, &fakeRunner{})
	if v := p.Version(); v != "" {
		t.Fatalf("unbundled test binary reports version %q, want empty (never updates)", v)
	}
	if id := p.Identifier(); id != "" {
		t.Fatalf("unbundled test binary reports identifier %q, want empty", id)
	}
	name, dir, ok := p.Bundle()
	if ok {
		t.Fatalf("unbundled test binary reports bundle %q in %q", name, dir)
	}
}

// errFakeCommand is a stand-in error for scripted runner failures.
var errFakeCommand = &fakeCmdError{}

type fakeCmdError struct{}

func (*fakeCmdError) Error() string { return "codesign failed" }
