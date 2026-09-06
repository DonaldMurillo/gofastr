//go:build darwin && (arm64 || amd64)

package update

import (
	"runtime"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/objc"
)

// fakeRunner records one command and answers a scripted error.
type fakeRunner struct {
	runs []string
	err  error
}

func (f *fakeRunner) Run(name string, args ...string) error {
	f.runs = append(f.runs, name+" "+strings.Join(args, " "))
	return f.err
}

func inlineMain(fn func()) error {
	fn()
	return nil
}

func TestDarwinPlatformCodesignInvocation(t *testing.T) {
	runner := &fakeRunner{}
	p := DefaultPlatform(inlineMain, runner)
	if err := p.VerifySignature("/tmp/Notes.app"); err != nil {
		t.Fatal(err)
	}
	if len(runner.runs) != 1 || runner.runs[0] != "codesign --verify --deep --strict /tmp/Notes.app" {
		t.Fatalf("codesign invocation = %v", runner.runs)
	}
	// A failing codesign is reported, not swallowed.
	failing := &fakeRunner{err: errFakeCommand}
	if err := DefaultPlatform(inlineMain, failing).VerifySignature("/tmp/Notes.app"); err == nil {
		t.Fatal("failing codesign accepted")
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
	name, dir, ok := p.Bundle()
	if ok {
		t.Fatalf("unbundled test binary reports bundle %q in %q", name, dir)
	}
}

// errFakeCommand is a stand-in error for scripted runner failures.
var errFakeCommand = &fakeCmdError{}

type fakeCmdError struct{}

func (*fakeCmdError) Error() string { return "codesign failed" }
