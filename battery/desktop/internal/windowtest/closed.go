package windowtest

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
)

// Closed checks every error-returning Window method and its cached state.
func Closed(t *testing.T, window desktop.Window) {
	t.Helper()
	title, id := window.Title(), window.ID()
	checks := map[string]func() error{
		"Navigate":        func() error { return window.Navigate("https://example.test/") },
		"Eval":            func() error { return window.Eval("1 + 1") },
		"Snapshot":        func() error { _, err := window.Snapshot(context.Background()); return err },
		"SetTitle":        func() error { return window.SetTitle("Changed") },
		"Focus":           window.Focus,
		"Frame":           func() error { _, err := window.Frame(); return err },
		"SetFrame":        func() error { return window.SetFrame(desktop.Frame{Width: 320, Height: 240}) },
		"SetSidebarWidth": func() error { return window.SetSidebarWidth(256) },
		"Close":           window.Close,
	}
	contract := reflect.TypeFor[desktop.Window]()
	for index := range contract.NumMethod() {
		method := contract.Method(index)
		if method.Type.NumOut() == 0 || method.Type.Out(method.Type.NumOut()-1) != reflect.TypeFor[error]() {
			continue
		}
		t.Run(method.Name, func(t *testing.T) {
			check, found := checks[method.Name]
			if !found {
				t.Fatalf("Window.%s needs a closed-window check", method.Name)
			}
			want := error(desktop.ErrWindowClosed)
			if method.Name == "Close" {
				want = nil
			}
			if err := check(); !errors.Is(err, want) {
				t.Errorf("closed Window.%s = %v, want %v", method.Name, err, want)
			}
		})
	}
	if evaluator, ok := window.(desktop.PageEvaluator); ok {
		if _, err := evaluator.EvalAsync(context.Background(), "return 1"); !errors.Is(err, desktop.ErrWindowClosed) {
			t.Errorf("closed EvalAsync = %v, want ErrWindowClosed", err)
		}
	}
	if window.Title() != title || window.ID() != id || window.Native() != 0 {
		t.Errorf("closed window changed cached state: title=%q id=%q native=%d", window.Title(), window.ID(), window.Native())
	}
	if err := window.Close(); err != nil {
		t.Errorf("repeated Close = %v", err)
	}
}
