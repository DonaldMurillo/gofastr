package evalrunner

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// TestStartBrowserGivesUpOnSilentChrome: a Chrome that completes the
// websocket handshake and then never answers a CDP command must not hang
// the capture run. The URL read and dial deadlines are behind it by then,
// and the CLI hands captureCandidate a context with no deadline, so only
// startBrowser's watchdog ends the wait. The fake Chrome's listener
// upgrades every connection and then stays silent.
func TestStartBrowserGivesUpOnSilentChrome(t *testing.T) {
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
			go upgradeAndHold(c)
		}
	}()
	fake := filepath.Join(t.TempDir(), "chrome")
	script := fmt.Sprintf("#!/bin/sh\necho 'DevTools listening on ws://%s/devtools/browser/fake' >&2\nexec sleep 60\n", ln.Addr())
	if err := os.WriteFile(fake, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	alloc, allocCancel := chromedp.NewExecAllocator(context.Background(), chromedp.ExecPath(fake), chromedp.WSURLReadTimeout(90*time.Second))
	defer allocCancel()
	browser, browserCancel := chromedp.NewContext(alloc, chromedp.WithBrowserOption(chromedp.WithDialTimeout(90*time.Second)))
	defer browserCancel()

	done := make(chan error, 1)
	go func() { done <- startBrowser(browser, 2*time.Second) }()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "no answer within") {
			t.Fatalf("startBrowser = %v, want the watchdog's no-answer error", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("startBrowser still waiting on a silent Chrome after 30s")
	}
}

// upgradeAndHold answers c's websocket upgrade request and then reads and
// drops whatever arrives, so every CDP command goes unanswered.
func upgradeAndHold(c net.Conn) {
	defer c.Close()
	br := bufio.NewReader(c)
	req, err := http.ReadRequest(br)
	if err != nil {
		return
	}
	sum := sha1.Sum([]byte(req.Header.Get("Sec-WebSocket-Key") + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	fmt.Fprintf(c, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n",
		base64.StdEncoding.EncodeToString(sum[:]))
	_, _ = br.WriteTo(io.Discard)
}
