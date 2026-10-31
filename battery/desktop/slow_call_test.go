package desktop_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/desktoptest"
	"github.com/DonaldMurillo/gofastr/framework"
)

// A bridge call that waits on a person (a file picker, a permission
// alert) outlived the app's request timeout, which cancelled the
// handler's context (the Windows and Linux pickers close on it), and
// its write timeout, which dropped the answer. The user's pick was then
// lost. The app here gets budgets far shorter than the call so both
// would fire.
func TestSlowBridgeCallStillAnswers(t *testing.T) {
	app, d := uiAppWith(t, desktop.Config{Title: "Slow"}, framework.AppConfig{
		Name:           "slowcall",
		RequestTimeout: 50 * time.Millisecond,
		HTTPServerTimeouts: framework.HTTPServerTimeoutsConfig{
			WriteTimeout: new(100 * time.Millisecond),
		},
	})
	if err := d.Register(desktop.Capability{
		Name:    "slow",
		Version: 1,
		Methods: []desktop.Method{{
			Name: "wait",
			Handler: func(ctx context.Context, _ json.RawMessage) (any, error) {
				select {
				case <-time.After(300 * time.Millisecond):
					return map[string]any{"picked": "/a/b.txt"}, nil
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	h := desktoptest.Run(t, app, d)

	var out struct {
		Picked string `json:"picked"`
	}
	h.Call("slow", "wait", nil).MustResult(t, &out)
	if out.Picked != "/a/b.txt" {
		t.Fatalf("result = %+v, want the pick", out)
	}
}
