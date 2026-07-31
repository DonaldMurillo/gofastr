package middleware

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// sseHandler flushes a subscribed line, then waits past the request
// deadline before emitting a late event — unless the request context is
// cancelled first, in which case it exits like every real SSE loop does.
func sseHandler(wait time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, ": subscribed\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(wait):
		}
		fmt.Fprint(w, "data: late-event\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	})
}

func TestStreamOutlivesRequestTimeout(t *testing.T) {
	h := Timeout(200 * time.Millisecond)(sseHandler(600 * time.Millisecond))
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	r := bufio.NewReader(resp.Body)
	first, err := r.ReadString('\n')
	if err != nil || !strings.HasPrefix(first, ": subscribed") {
		t.Fatalf("first line = %q, err = %v", first, err)
	}
	deadline := time.Now().Add(2 * time.Second)
	var got string
	for time.Now().Before(deadline) {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("stream died before late event: %v", err)
		}
		if strings.HasPrefix(line, "data: late-event") {
			got = line
			break
		}
	}
	if got == "" {
		t.Fatal("late event never arrived")
	}
}

func TestStreamOutlivesServerWriteTimeout(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &http.Server{
		Handler:      Timeout(5 * time.Second)(sseHandler(700 * time.Millisecond)),
		WriteTimeout: 300 * time.Millisecond,
		ReadTimeout:  300 * time.Millisecond,
	}
	go srv.Serve(ln)
	defer srv.Close()

	resp, err := http.Get("http://" + ln.Addr().String())
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	r := bufio.NewReader(resp.Body)
	if _, err := r.ReadString('\n'); err != nil {
		t.Fatalf("first line: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("write deadline killed the stream: %v", err)
		}
		if strings.HasPrefix(line, "data: late-event") {
			return
		}
	}
	t.Fatal("late event never arrived")
}

// A handler that never flushes must still be cut off with a 504.
func TestBufferedHandlerStill504s(t *testing.T) {
	slow := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
		fmt.Fprint(w, "too late")
	})
	srv := httptest.NewServer(Timeout(150 * time.Millisecond)(slow))
	defer srv.Close()

	start := time.Now()
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want 504", resp.StatusCode)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("504 took %v, want ~150ms", elapsed)
	}
}
