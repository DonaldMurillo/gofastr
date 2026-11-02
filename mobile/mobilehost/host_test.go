package mobilehost

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/owner"
)

func testHost(t *testing.T, routes func(app *framework.App)) *Host {
	t.Helper()
	t.Cleanup(func() { owner.SetExtractor(nil) })
	app := framework.NewApp(framework.WithoutDefaultMiddleware())
	routes(app)
	h, err := New(app, Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = h.Stop(ctx)
	})
	return h
}

// Set-Cookie never reaches the WebView; the jar replays it on the next
// request, Secure attribute included.
func TestCookiesStayInTheJar(t *testing.T) {
	h := testHost(t, func(app *framework.App) {
		app.Router().Get("/set", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.SetCookie(w, &http.Cookie{Name: "s", Value: "v1", Path: "/", Secure: true, HttpOnly: true})
		}))
		app.Router().Get("/echo", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, err := r.Cookie("s")
			if err != nil {
				_, _ = io.WriteString(w, "none")
				return
			}
			_, _ = io.WriteString(w, c.Value)
		}))
	})
	r, err := h.Do(context.Background(), &Request{URL: DefaultOrigin + "/set"})
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Header.Values("Set-Cookie"); len(got) != 0 {
		t.Fatalf("Set-Cookie leaked to the WebView: %v", got)
	}
	r, err = h.Do(context.Background(), &Request{URL: DefaultOrigin + "/echo"})
	if err != nil {
		t.Fatal(err)
	}
	if string(r.Body) != "v1" {
		t.Fatalf("jar did not replay the Secure cookie: got %q", r.Body)
	}
}

// The forwarded scheme is the host's, never the WebView's.
func TestForwardedProtoIsTheHosts(t *testing.T) {
	h := testHost(t, func(app *framework.App) {
		app.Router().Get("/proto", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, strings.Join(r.Header.Values("X-Forwarded-Proto"), ","))
		}))
	})
	r, err := h.Do(context.Background(), &Request{URL: DefaultOrigin + "/proto", Header: http.Header{"X-Forwarded-Proto": {"http"}}})
	if err != nil {
		t.Fatal(err)
	}
	if string(r.Body) != "https" {
		t.Fatalf("X-Forwarded-Proto = %q, want exactly https", r.Body)
	}
}

// Open hands back headers before the body is complete and streams each
// flushed chunk as it is written: the SSE shape.
func TestOpenStreams(t *testing.T) {
	release := make(chan struct{})
	h := testHost(t, func(app *framework.App) {
		app.Router().Get("/events", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: one\n\n")
			w.(http.Flusher).Flush()
			<-release
			_, _ = io.WriteString(w, "data: two\n\n")
		}))
	})
	s, err := h.Open(context.Background(), &Request{URL: DefaultOrigin + "/events"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Status != 200 || s.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("stream head = %d %q", s.Status, s.Header.Get("Content-Type"))
	}
	first, err := s.Read(0)
	if err != nil || string(first) != "data: one\n\n" {
		t.Fatalf("first chunk = %q, %v (want it before the handler finishes)", first, err)
	}
	close(release)
	var rest []byte
	for {
		b, err := s.Read(0)
		rest = append(rest, b...)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if string(rest) != "data: two\n\n" {
		t.Fatalf("rest = %q", rest)
	}
}

func TestParseHeaderLinesDropsControlBytes(t *testing.T) {
	h := ParseHeaderLines("Accept: text/html\r\nX-Bad: a\x00b\nnocolon\nX-Ok:  v \n")
	if h.Get("Accept") != "text/html" || h.Get("X-Ok") != "v" {
		t.Fatalf("parsed %v", h)
	}
	if _, ok := h["X-Bad"]; ok {
		t.Fatalf("a value with a NUL byte survived: %v", h)
	}
	if got := FormatHeaderLines(h); got != "Accept: text/html\nX-Ok: v\n" {
		t.Fatalf("format = %q", got)
	}
}
