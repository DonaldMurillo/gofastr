//go:build linux && (amd64 || arm64)

package linux

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"testing"
	"time"
	"unsafe"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/ffi"
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/gtk"
)

func TestCapabilitiesCancelledBeforeNativeCalls(t *testing.T) {
	s := New().(*linuxShell)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Prompt(ctx, desktop.PermissionRequest{}); !errors.Is(err, desktop.ErrCancelled) {
		t.Fatalf("Prompt: %v", err)
	}
	if _, err := s.ReadText(ctx); !errors.Is(err, desktop.ErrCancelled) {
		t.Fatalf("ReadText: %v", err)
	}
	if err := s.WriteText(ctx, "text"); !errors.Is(err, desktop.ErrCancelled) {
		t.Fatalf("WriteText: %v", err)
	}
	if _, err := s.OpenFile(ctx, desktop.OpenOptions{}); !errors.Is(err, desktop.ErrCancelled) {
		t.Fatalf("OpenFile: %v", err)
	}
	if _, err := s.SaveFile(ctx, desktop.SaveOptions{}); !errors.Is(err, desktop.ErrCancelled) {
		t.Fatalf("SaveFile: %v", err)
	}
	if _, err := s.OpenFolder(ctx); !errors.Is(err, desktop.ErrCancelled) {
		t.Fatalf("OpenFolder: %v", err)
	}
	if err := s.Show(ctx, desktop.Notification{}); !errors.Is(err, desktop.ErrCancelled) {
		t.Fatalf("Show: %v", err)
	}
}

func TestCapabilityInvalidInputBeforeNativeCalls(t *testing.T) {
	s := New().(*linuxShell)
	check := func(err error) {
		t.Helper()
		var e *desktop.Error
		if !errors.As(err, &e) || e.Code != desktop.CodeInvalidInput {
			t.Fatalf("want invalid_input, got %v", err)
		}
	}
	check(s.WriteText(nil, "has\x00nul"))
	check(s.WriteText(nil, string([]byte{0xff})))
	_, err := s.SaveFile(nil, desktop.SaveOptions{DefaultName: "dir/file.txt"})
	check(err)
	_, err = s.OpenFile(nil, desktop.OpenOptions{Filters: []desktop.FileFilter{{Extensions: []string{"a?"}}}})
	check(err)
	_, err = s.Prompt(nil, desktop.PermissionRequest{Description: "bad\x00text"})
	check(err)
	check(s.Show(nil, desktop.Notification{Body: "bad\x00body"}))
	check(s.ClickPrompt("anything"))
}

func TestScriptedPermissionsFailClosed(t *testing.T) {
	s := New().(*linuxShell)
	s.ScriptPrompts(desktop.DecisionAllow, desktop.DecisionAllowOnce, desktop.DecisionDeny, desktop.Decision(500))
	for _, want := range []desktop.Decision{desktop.DecisionAllow, desktop.DecisionAllowOnce, desktop.DecisionDeny, desktop.DecisionDeny} {
		got, err := s.Prompt(nil, desktop.PermissionRequest{})
		if err != nil || got != want {
			t.Fatalf("decision = %v, %v; want %v", got, err, want)
		}
	}
	for response, want := range map[int32]desktop.Decision{
		capsResponseAllow: desktop.DecisionAllow, capsResponseOnce: desktop.DecisionAllowOnce,
		capsResponseDeny: desktop.DecisionDeny, -4: desktop.DecisionDeny, 0: desktop.DecisionDeny, 900: desktop.DecisionDeny,
	} {
		if got := capsDecision(response); got != want {
			t.Fatalf("response %d = %v; want %v", response, got, want)
		}
	}
}

func TestClipboardCallbackReusesSlotAndRejectsStaleTokens(t *testing.T) {
	s := New().(*linuxShell)
	buffer := capsCString("clipboard π\ntext")
	for i := 0; i < 256; i++ {
		op := newCapsOperation(s, false, false)
		capsCall(capsClipboardCallback, 0, uintptr(unsafe.Pointer(&buffer[0])), op.id)
		<-op.done
		if op.result.err != nil || op.result.text != "clipboard π\ntext" {
			t.Fatalf("result = %#v", op.result)
		}
		if _, found := capsOperations.Load(op.id); found {
			t.Fatal("completed clipboard token retained")
		}
		// A duplicate callback must not publish again or dereference its text.
		capsCall(capsClipboardCallback, 0, 1, op.id)
	}
	runtime.KeepAlive(buffer)
	op := newCapsOperation(s, false, false)
	op.cancelled.Store(true)
	capsCall(capsClipboardCallback, 0, 0, op.id)
	<-op.done
	if !errors.Is(op.result.err, desktop.ErrCancelled) {
		t.Fatalf("cancelled result: %#v", op.result)
	}
}

func TestCapabilitiesShutdownCancelsOnlyOwningShell(t *testing.T) {
	s, other := New().(*linuxShell), New().(*linuxShell)
	first := newCapsOperation(s, false, false)
	second := newCapsOperation(other, false, false)
	defer other.closeCapabilities()
	s.closeCapabilities()
	select {
	case <-first.done:
	default:
		t.Fatal("pending operation did not finish")
	}
	if !errors.Is(first.result.err, desktop.ErrCancelled) {
		t.Fatalf("shutdown result: %v", first.result.err)
	}
	if _, found := capsOperations.Load(first.id); found {
		t.Fatal("shutdown retained token")
	}
	select {
	case <-second.done:
		t.Fatal("shutdown cancelled another shell")
	default:
	}
	// A response after cancellation has no retained Go/native references.
	capsCall(capsClipboardCallback, 0, 1, first.id)
}

func TestCapabilitiesResultPublishedOnce(t *testing.T) {
	op := newCapsOperation(New().(*linuxShell), false, false)
	defer capsOperations.Delete(op.id)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); op.publish(capsResult{text: "done"}) }()
	}
	<-op.done
	if op.result.text != "done" {
		t.Fatalf("result = %#v", op.result)
	}
	wg.Wait()
}

func TestCapabilityDialogGateCancellation(t *testing.T) {
	s := New().(*linuxShell)
	release, err := s.acquireCaps(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := s.acquireCaps(ctx); !errors.Is(err, desktop.ErrCancelled) {
		t.Fatalf("queued dialog cancellation = %v", err)
	}
	release()
	release, err = s.acquireCaps(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	release()
}

func TestCapabilitiesStoppedShellDoesNotRetainOperations(t *testing.T) {
	s := New().(*linuxShell)
	if _, err := s.ReadText(nil); err == nil {
		t.Fatal("clipboard read succeeded before Run")
	}
	if err := s.WriteText(nil, "text"); err == nil {
		t.Fatal("clipboard write succeeded before Run")
	}
	capsOperations.Range(func(_, value any) bool {
		if value.(*capsOperation).shell == s {
			t.Error("failed dispatch retained operation")
		}
		return true
	})
}

// One fake idle dispatcher for the whole test process, just like production's
// fixed callback. The callback is deliberately held until the test resumes it.
var capsTestIdleQueue = make(chan [2]uintptr, 4)
var capsTestIdleCallback = ffi.NewCallback(func(a *ffi.Args) uintptr {
	capsTestIdleQueue <- [2]uintptr{a.Int[0], a.Int[1]}
	return 1
})

func TestCancellationCleanupSurvivesAbandonedMainDispatch(t *testing.T) {
	previous := gtk.Sym.GIdleAdd
	gtk.Sym.GIdleAdd = capsTestIdleCallback
	defer func() { gtk.Sym.GIdleAdd = previous }()
	s := New().(*linuxShell)
	s.running.Store(true)
	op := newCapsOperation(s, false, true)
	defer capsOperations.Delete(op.id)
	op.started.Store(true) // an already-shown dialog, not an in-flight constructor
	op.queueCleanup()
	op.queueCleanup() // duplicate cancellation must not allocate more idle work
	var queued [2]uintptr
	select {
	case queued = <-capsTestIdleQueue:
	default:
		t.Fatal("cleanup was not queued")
	}
	if queued != [2]uintptr{capsCleanupCallback, op.id} {
		t.Fatalf("unexpected idle callback: %v", queued)
	}
	select {
	case <-capsTestIdleQueue:
		t.Fatal("cleanup queued twice")
	default:
	}
	// Cancelling ordinary shell jobs does not remove this cleanup. Dispatch it
	// after the simulated stall and verify that its operation is still found.
	cancelShellJobs(s)
	capsCall(queued[0], queued[1])
	select {
	case <-op.done:
	default:
		t.Fatal("delayed cleanup did not complete")
	}
	if !errors.Is(op.result.err, desktop.ErrCancelled) {
		t.Fatalf("result: %v", op.result.err)
	}
	if _, found := capsOperations.Load(op.id); found {
		t.Fatal("cleanup retained token")
	}
	capsCall(queued[0], queued[1]) // a stale native callback is harmless
}
