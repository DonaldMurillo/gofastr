//go:build !(darwin && arm64)

package desktop

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

func TestDefaultShellUnsupportedNamesPlatform(t *testing.T) {
	s := newDefaultShell()
	err := s.Run(context.Background(), WindowConfig{}, func(Window) {})
	if err == nil {
		t.Fatal("Run on the default shell must fail")
	}
	if !strings.Contains(err.Error(), runtime.GOOS) || !strings.Contains(err.Error(), runtime.GOARCH) {
		t.Fatalf("error must name GOOS/GOARCH: %v", err)
	}
	if de, ok := err.(*Error); !ok || de.Code != CodeUnsupported {
		t.Fatalf("err = %v, want unsupported", err)
	}
	// Quit/Main are safe to call. Main is not on the Shell interface -
	// only the implementations that have a UI thread need it, so it is
	// reached through the optional interface a caller would use.
	s.Quit()
	mainer, ok := s.(interface{ Main(func()) error })
	if !ok {
		t.Fatal("the default shell must still offer Main for callers that hop to the UI thread")
	}
	ran := false
	if err := mainer.Main(func() { ran = true }); err != nil || !ran {
		t.Fatalf("Main: ran=%v err=%v", ran, err)
	}
	if _, err := s.Clipboard().ReadText(context.Background()); err == nil {
		t.Fatal("clipboard read must be unsupported")
	}
	if _, err := s.Dialogs().OpenFile(context.Background(), OpenOptions{}); err == nil {
		t.Fatal("dialog must be unsupported")
	}
	if err := s.Notifier().Show(context.Background(), Notification{Title: "x"}); err == nil {
		t.Fatal("notify must be unsupported")
	}
	if _, err := s.Prompt(context.Background(), PermissionRequest{}); err == nil {
		t.Fatal("prompt must be unsupported")
	}
}

func TestErrSentinels(t *testing.T) {
	if ErrUnsupported.Code != CodeUnsupported || ErrCancelled.Code != CodeCancelled {
		t.Fatal("sentinels carry the wrong codes")
	}
	if !strings.Contains(ErrUnsupported.Error(), "desktop:") {
		t.Fatalf("Error() = %q", ErrUnsupported.Error())
	}
}

// Silence unused imports when fixtures move.
var (
	_ = sql.Open
	_ = json.Marshal
	_ = filepath.Join
)
