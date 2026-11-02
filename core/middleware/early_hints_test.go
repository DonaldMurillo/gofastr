package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// earlyHints sends 103 Early Hints, then its real answer.
var earlyHints = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Link", "</app.css>; rel=preload; as=style")
	w.WriteHeader(http.StatusEarlyHints)
	w.WriteHeader(http.StatusCreated)
	_, _ = io.WriteString(w, "made")
})

// getFinal serves h and returns the final status and body a client sees.
func getFinal(t *testing.T, h http.Handler) (int, string) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	resp, err := srv.Client().Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(body)
}

// A wrapper that latched the 103 as the response dropped the handler's
// 201, and the client got 200.
func TestWrappersKeepStatusAfterHints(t *testing.T) {
	cases := map[string]http.Handler{
		"metrics": MetricsMiddleware(NewMetrics())(earlyHints),
		"tracing": Tracing()(earlyHints),
		"timeout": Timeout(time.Minute)(earlyHints),
		"logging": Logging()(earlyHints),
	}
	for name, h := range cases {
		t.Run(name, func(t *testing.T) {
			if code, body := getFinal(t, h); code != http.StatusCreated || body != "made" {
				t.Fatalf("got %d %q, want 201 \"made\"", code, body)
			}
		})
	}
}

// TestLoggingRecordsStatusSent: the logging wrapper latched the LAST
// WriteHeader, informational and superfluous calls included, and did not
// latch the implicit 200 of a first Write. A 103 followed by a plain
// Write logged 103; a 500 after the body had gone out as 200 logged 500.
// It now records what the client received, like the metrics and tracing
// wrappers.
func TestLoggingRecordsStatusSent(t *testing.T) {
	cases := map[string]http.HandlerFunc{
		"103 then implicit 200": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Link", "</a.css>; rel=preload")
			w.WriteHeader(http.StatusEarlyHints)
			_, _ = io.WriteString(w, "ok")
		},
		"superfluous WriteHeader after the body": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, "ok")
			w.WriteHeader(http.StatusInternalServerError) // dropped by net/http
		},
		"WriteHeader after a flush": func(w http.ResponseWriter, _ *http.Request) {
			http.NewResponseController(w).Flush()
			w.WriteHeader(http.StatusInternalServerError) // dropped by net/http
		},
	}
	for name, h := range cases {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			srv := httptest.NewUnstartedServer(LoggingWithWriter(&buf)(h))
			srv.Config.ErrorLog = log.New(io.Discard, "", 0)
			srv.Start()
			resp, err := srv.Client().Get(srv.URL + "/")
			if err != nil {
				t.Fatal(err)
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			srv.Close() // waits for the handler, so the log line is written
			var entry struct {
				Status int `json:"status"`
			}
			if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
				t.Fatalf("%v: %q", err, buf.String())
			}
			if entry.Status != resp.StatusCode {
				t.Fatalf("logged status %d, client got %d", entry.Status, resp.StatusCode)
			}
		})
	}
}

// TestWrappersLatchStatusOnFlush: a Flush before any WriteHeader sends
// the headers with 200. The metrics and tracing wrappers did not latch
// it, so an error status written after the flush, which net/http drops,
// was recorded as the response's status (an SSE stream that failed
// mid-way counted as a 500 the client never got).
func TestWrappersLatchStatusOnFlush(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		http.NewResponseController(w).Flush() // commits 200
		http.Error(w, "stream failed", http.StatusInternalServerError)
	})
	t.Run("metrics", func(t *testing.T) {
		m := NewMetrics()
		rec := httptest.NewRecorder()
		MetricsMiddleware(m)(h).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/events", nil))
		scrape := httptest.NewRecorder()
		MetricsHandler(m).ServeHTTP(scrape, httptest.NewRequest(http.MethodGet, "/metrics", nil))
		if rec.Code != http.StatusOK || !strings.Contains(scrape.Body.String(), `status="200"`) {
			t.Fatalf("client got %d, metrics recorded:\n%s", rec.Code, scrape.Body.String())
		}
	})
	t.Run("tracing", func(t *testing.T) {
		prev := otel.GetTracerProvider()
		sr := tracetest.NewSpanRecorder()
		otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr)))
		t.Cleanup(func() { otel.SetTracerProvider(prev) })
		rec := httptest.NewRecorder()
		Tracing()(h).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/events", nil))
		var got int64
		for _, kv := range sr.Ended()[0].Attributes() {
			if kv.Key == "http.status_code" {
				got = kv.Value.AsInt64()
			}
		}
		if got != int64(rec.Code) {
			t.Fatalf("span http.status_code = %d, client got %d", got, rec.Code)
		}
	})
}
