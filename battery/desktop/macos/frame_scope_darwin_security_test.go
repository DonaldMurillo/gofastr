//go:build darwin && (arm64 || amd64) && !desktop_e2e

package macos

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/objc"
)

func TestNativeFramesHaveNoBridge(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-test.run=^$")
	command.Env = append(os.Environ(), "GOFASTR_TEST_FRAME_SCOPE=1")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("native frame confinement: %v\n%s", err, output)
	} else {
		t.Logf("%s", output)
	}
}

func frameScopeCheck() int {
	child := `<script>const marker=!!window.__gofastr_desktop;window.webkit.messageHandlers.gofastr.postMessage('child');parent.postMessage({marker},'*');</script>`
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, child)
	}))
	defer foreign.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if r.URL.Path == "/child" {
			fmt.Fprint(w, child)
			return
		}
		fmt.Fprintf(w, `<script>window.reports=[];addEventListener('message',event=>reports.push(event.data));</script><iframe src="/child"></iframe><iframe src="%s"></iframe>`, foreign.URL)
	}))
	defer server.Close()
	shell := New().(*darwinShell)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	result := make(chan error, 1)
	err := shell.Run(ctx, desktop.WindowConfig{Title: "Frame confinement", Width: 500, Height: 350}, func(main desktop.Window) {
		go func() {
			defer shell.Quit()
			if err := main.Navigate(server.URL); err != nil {
				result <- err
				return
			}
			secondary, err := shell.OpenWindow("frame-test", desktop.WindowSpec{Width: 400, Height: 300}, server.URL)
			if err != nil {
				result <- err
				return
			}
			for _, window := range []desktop.Window{main, secondary} {
				evaluator := window.(desktop.PageEvaluator)
				for ctx.Err() == nil {
					ready, err := evaluator.EvalAsync(ctx, `return !!window.reports && reports.length===2`)
					if err == nil && string(ready) == "true" {
						break
					}
					time.Sleep(20 * time.Millisecond)
				}
				var reports struct {
					Marker bool
					Frames []struct{ Marker bool }
				}
				raw, err := evaluator.EvalAsync(ctx, `window.webkit.messageHandlers.gofastr.postMessage('top');return {Marker:!!window.__gofastr_desktop,Frames:reports};`)
				if err != nil {
					result <- err
					return
				}
				if err := json.Unmarshal(raw, &reports); err != nil || !reports.Marker || len(reports.Frames) != 2 || reports.Frames[0].Marker || reports.Frames[1].Marker {
					result <- fmt.Errorf("%s frame markers: %s (%v)", window.ID(), raw, err)
					return
				}
				var scopeErr error
				if err := shell.Main(func() {
					webView := window.(*darwinWindow).webView
					config := objc.ID(objc.Send(webView, objc.Sel("configuration")))
					ucc := objc.ID(objc.Send(config, objc.Sel("userContentController")))
					scripts := objc.ID(objc.Send(ucc, objc.Sel("userScripts")))
					for index := uintptr(0); index < objc.Send(scripts, objc.Sel("count")); index++ {
						script := objc.ID(objc.Send(scripts, objc.Sel("objectAtIndex:"), index))
						if objc.Send(script, objc.Sel("isForMainFrameOnly"))&0xff == 0 {
							scopeErr = fmt.Errorf("%s script injected in child frames", window.ID())
						}
					}
				}); err != nil {
					scopeErr = err
				}
				if scopeErr != nil {
					result <- scopeErr
					return
				}
				select {
				case message := <-shell.scriptMsgs:
					if message != "top" {
						result <- fmt.Errorf("%s accepted child native message %q", window.ID(), message)
						return
					}
				case <-ctx.Done():
					result <- ctx.Err()
					return
				}
			}
			result <- nil
		}()
	})
	if err == nil {
		err = <-result
	}
	if err != nil {
		fmt.Println(err)
		return 1
	}
	fmt.Println("main and secondary: same-origin and foreign frames have no bootstrap; child native posts refused; top posts delivered")
	return 0
}
