package chromedptest

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// TestSlowDialWithinStartTimeout: the DevTools websocket dial gets the
// whole startup allowance, not chromedp's 10s dial default. A cold
// Chrome on a loaded runner printed its websocket URL and then took past
// 10s to answer the dial, and core-ui-runtime-hosts failed on main with
// "could not dial …: context deadline exceeded" inside a 90s
// StartTimeout. A fake Chrome prints the URL of a listener that holds
// every connection 11s and then closes it, so the dial fails on its own
// after 11s instead of on chromedp's deadline at 10s.
func TestSlowDialWithinStartTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("holds a dial for 11s")
	}
	if runtime.GOOS == "windows" {
		t.Skip("the fake Chrome is a shell script")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				time.Sleep(11 * time.Second)
				c.Close()
			}()
		}
	}()
	fake := filepath.Join(t.TempDir(), "chrome")
	script := fmt.Sprintf("#!/bin/sh\necho 'DevTools listening on ws://%s/devtools/browser/fake' >&2\nexec sleep 60\n", ln.Addr())
	if err := os.WriteFile(fake, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}

	cfg := newConfig(StartTimeout(30*time.Second), AllocatorOptions(chromedp.ExecPath(fake)))
	start := time.Now()
	b, err := launch(cfg, "dial-timeout-test", false)
	if err == nil {
		b.close()
		t.Fatal("launch against a listener that never upgrades succeeded")
	}
	if errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "no answer within") {
		t.Fatalf("launch gave up after %v (%v): the dial must wait out the 30s start allowance", time.Since(start).Round(time.Second), err)
	}
}
