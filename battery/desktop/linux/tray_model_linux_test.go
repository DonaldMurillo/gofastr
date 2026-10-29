//go:build linux && (amd64 || arm64)

package linux

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
)

func TestTrayModelRecoveryAndBounds(t *testing.T) {
	model, err := newTrayModel(context.Background(), desktop.WindowConfig{AppID: "dev.gofastr.tray", Title: "App", Tray: &desktop.Tray{Title: "App"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(model.root.children) != 2 || model.root.children[0].plan.Role != desktop.RoleShow || model.root.children[1].plan.Role != desktop.RoleQuit {
		t.Fatal("missing top-level recovery actions")
	}
	if len(model.argb) == 0 || model.width != 24 || model.height != 24 {
		t.Fatal("title-only tray has no visible fallback icon")
	}
	deep := desktop.MenuItem{}
	for range 12 {
		deep = desktop.MenuItem{Children: []desktop.MenuItem{deep}}
	}
	for _, tray := range []*desktop.Tray{{Title: strings.Repeat("x", trayTextLimit+1)}, {Title: "nul\x00"}, {Menu: &desktop.Menu{Items: make([]desktop.MenuItem, trayNodeLimit)}}, {Menu: &desktop.Menu{Items: []desktop.MenuItem{deep}}}} {
		if _, err := newTrayModel(context.Background(), desktop.WindowConfig{Tray: tray}); err == nil {
			t.Fatal("unbounded or malformed tray accepted")
		}
	}
}
func TestTrayIconARGB(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	img.SetNRGBA(0, 0, color.NRGBA{R: 200, G: 100, B: 50, A: 128})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	w, h, argb, err := trayIcon(buf.Bytes())
	if err != nil || w != 1 || h != 1 || !bytes.Equal(argb, []byte{128, 200, 100, 50}) {
		t.Fatalf("ARGB=%v size=%dx%d err=%v", argb, w, h, err)
	}
	for _, data := range [][]byte{[]byte("not png"), make([]byte, trayIconLimit+1), buf.Bytes()[:12]} {
		if _, _, _, err := trayIcon(data); err == nil {
			t.Fatal("invalid icon accepted")
		}
	}
}
func TestTrayRecoveryState(t *testing.T) {
	b := newTrayBroker(&trayModel{}, nil, nil)
	b.setAvailable(false)
	if len(b.recovery) != 0 {
		t.Fatal("initial no-host state was a loss")
	}
	b.setAvailable(true)
	if !b.available.Load() {
		t.Fatal("host not available")
	}
	b.setAvailable(false)
	if b.available.Load() || len(b.recovery) != 1 {
		t.Fatal("host loss did not queue recovery")
	}
	b.setAvailable(false)
	if len(b.recovery) != 1 {
		t.Fatal("recovery queue is unbounded")
	}
	b.stopping.Store(true)
	b.setAvailable(true)
	if b.available.Load() {
		t.Fatal("stale proof resurrected stopped tray")
	}
	for range 16 {
		if b.enqueue(trayAction{}) {
			t.Fatal("stopped broker accepted action")
		}
	}
	b.stopping.Store(false)
	for range 16 {
		if !b.enqueue(trayAction{}) {
			t.Fatal("bounded queue rejected capacity")
		}
	}
	if b.enqueue(trayAction{}) {
		t.Fatal("unbounded action queue")
	}
}

func TestTrayRecoveryRetries(t *testing.T) {
	var tries atomic.Int32
	revealed := make(chan struct{}, 1)
	b := newTrayBroker(&trayModel{}, nil, func() bool {
		if tries.Add(1) == 1 {
			return false
		}
		revealed <- struct{}{}
		return true
	})
	go b.deliver()
	defer close(b.stop)
	b.setAvailable(true)
	b.setAvailable(false)
	select {
	case <-revealed:
	case <-time.After(time.Second):
		t.Fatal("failed GTK dispatch permanently lost recovery")
	}
	if tries.Load() != 2 {
		t.Fatalf("recovery tries=%d", tries.Load())
	}
}

func TestTrayCallbackPanicContained(t *testing.T) {
	var actions, recoveries atomic.Int32
	completed := make(chan struct{}, 1)
	b := newTrayBroker(&trayModel{}, func(trayAction) {
		if actions.Add(1) == 1 {
			panic("fixture action")
		}
		completed <- struct{}{}
	}, func() bool {
		if recoveries.Add(1) == 1 {
			panic("fixture recovery")
		}
		return true
	})
	go b.deliver()
	defer close(b.stop)
	b.setAvailable(true)
	b.setAvailable(false)
	b.enqueue(trayAction{})
	b.enqueue(trayAction{})
	select {
	case <-completed:
	case <-time.After(2 * time.Second):
		t.Fatal("callback panic stopped subsequent delivery")
	}
	deadline := time.Now().Add(time.Second)
	for recoveries.Load() < 2 {
		if time.Now().After(deadline) {
			t.Fatal("panic stopped recovery retry")
		}
		time.Sleep(time.Millisecond)
	}
}
