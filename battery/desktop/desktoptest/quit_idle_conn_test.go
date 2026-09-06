package desktoptest_test

import (
	"net"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/desktoptest"
)

// A connection that reached the listener but never sent a request (a
// WebView's speculative preconnect, an http.Transport's spare dial)
// must not hold the battery's 5 s drain on quit. Before the framework
// closed StateNew connections at the start of its drain, this shape
// made Quit report "draining http server: context deadline exceeded"
// once in about a dozen -race runs of this package.
func TestQuitIgnoresAConnThatNeverSentARequest(t *testing.T) {
	ta := newTestApp(t, desktop.Config{Title: "Harness"})
	h := desktoptest.Run(t, ta.app, ta.d)
	c, err := net.Dial("tcp", h.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	// Let Serve accept it: a connection still in the listen backlog is
	// never tracked and proves nothing.
	time.Sleep(100 * time.Millisecond)

	started := time.Now()
	if err := h.Quit(); err != nil {
		t.Fatalf("Quit with an idle new connection open: %v", err)
	}
	if took := time.Since(started); took > 2*time.Second {
		t.Fatalf("Quit took %v with an idle new connection open", took)
	}
}
