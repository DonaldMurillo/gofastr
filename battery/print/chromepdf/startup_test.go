package chromepdf

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

	"github.com/DonaldMurillo/gofastr/battery/print"
)

// TestSlowStartWithinTimeout: Options.Timeout bounds the render, Chrome's
// start included, so chromedp's own shorter start waits (20s for the
// DevTools URL, 10s for the websocket dial) must not fail a start that
// fits inside it. A fake Chrome stands in for a slow one: it prints the
// URL late, or prints the URL of a listener that holds the dial past 10s
// and then closes it, so the render fails on its own once the wait is
// over instead of on chromedp's deadline.
func TestSlowStartWithinTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("waits out a 21s start")
	}
	if runtime.GOOS == "windows" {
		t.Skip("the fake Chrome is a shell script")
	}
	for _, tc := range []struct {
		name            string
		urlDelay, dial  time.Duration
		chromedpRefusal string
	}{
		{"late url", 21 * time.Second, 0, "websocket url timeout reached"},
		{"slow dial", 0, 11 * time.Second, "context deadline exceeded"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
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
						time.Sleep(tc.dial)
						c.Close()
					}()
				}
			}()
			fake := filepath.Join(t.TempDir(), "chrome")
			script := fmt.Sprintf("#!/bin/sh\nsleep %d\necho 'DevTools listening on ws://%s/devtools/browser/fake' >&2\nexec sleep 60\n",
				int(tc.urlDelay/time.Second), ln.Addr())
			if err := os.WriteFile(fake, []byte(script), 0o700); err != nil {
				t.Fatal(err)
			}

			r := New(Options{ExecPath: fake, Timeout: 40 * time.Second})
			start := time.Now()
			_, err = r.RenderPDF(context.Background(), "<p>x</p>", print.A4Portrait(print.MM(10)), "")
			if err == nil {
				t.Fatal("render against a Chrome that never answers succeeded")
			}
			if errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), tc.chromedpRefusal) {
				t.Fatalf("render gave up after %v (%v): the start must get the whole 40s Timeout", time.Since(start).Round(time.Second), err)
			}
		})
	}
}
