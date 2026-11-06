package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// readerFromRecorder is a recorder that, like net/http's own response
// writer, implements io.ReaderFrom (its sendfile path) and says whether
// a body arrived through it.
type readerFromRecorder struct {
	*httptest.ResponseRecorder
	readFrom bool
}

func (r *readerFromRecorder) ReadFrom(src io.Reader) (int64, error) {
	r.readFrom = true
	return io.Copy(r.ResponseRecorder, src)
}

// fileLike is a body with no WriteTo, so io.Copy asks the destination
// for ReadFrom (http.ServeContent hands io.CopyN a LimitedReader).
func fileLike(s string) io.Reader { return io.LimitReader(strings.NewReader(s), int64(len(s))) }

// TestWrappersKeepReadFrom: the metrics, tracing and CORS writers
// embedded http.ResponseWriter, which promotes Header, Write and
// WriteHeader but not ReadFrom, so a file served behind any of them lost
// the connection's sendfile path. Each now forwards ReadFrom, after
// doing what its Write does first.
func TestWrappersKeepReadFrom(t *testing.T) {
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(tracetest.NewSpanRecorder())))
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	serve := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		if _, err := io.Copy(w, fileLike("file body")); err != nil {
			t.Errorf("copy: %v", err)
		}
	}
	for name, mw := range map[string]Middleware{
		"metrics": MetricsMiddleware(NewMetrics()),
		"tracing": Tracing(),
		"cors":    CORS(CORSConfig{AllowedOrigins: []string{"*"}}),
	} {
		t.Run(name, func(t *testing.T) {
			rec := &readerFromRecorder{ResponseRecorder: httptest.NewRecorder()}
			req := httptest.NewRequest(http.MethodGet, "/f", nil)
			req.Header.Set("Origin", "https://a.example")
			mw(http.HandlerFunc(serve)).ServeHTTP(rec, req)
			if !rec.readFrom {
				t.Fatalf("the body reached the client writer through Write: ReadFrom (sendfile) was hidden")
			}
			if rec.Body.String() != "file body" {
				t.Fatalf("body %q", rec.Body.String())
			}
			if name == "cors" && rec.Header().Get("Access-Control-Allow-Credentials") != "" {
				t.Fatalf("ReadFrom skipped the wildcard credentials strip Write does")
			}
		})
	}
}

// TestMetricsReadFromLatchesStatus: a body sent through ReadFrom is the
// implicit 200, so a WriteHeader after it (dropped by net/http) is not
// recorded.
func TestMetricsReadFromLatchesStatus(t *testing.T) {
	m := NewMetrics()
	h := MetricsMiddleware(m)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.Copy(w, fileLike("x"))
		w.WriteHeader(http.StatusInternalServerError)
	}))
	h.ServeHTTP(&readerFromRecorder{ResponseRecorder: httptest.NewRecorder()}, httptest.NewRequest(http.MethodGet, "/f", nil))
	scrape := httptest.NewRecorder()
	MetricsHandler(m).ServeHTTP(scrape, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if !strings.Contains(scrape.Body.String(), `status="200"`) || strings.Contains(scrape.Body.String(), `status="500"`) {
		t.Fatalf("want the 200 the client got:\n%s", scrape.Body.String())
	}
}

type panicAfterChunk struct{ sent bool }

func (reader *panicAfterChunk) Read(buffer []byte) (int, error) {
	if reader.sent {
		panic("source failed after writing")
	}
	reader.sent = true
	return copy(buffer, "x"), nil
}

func TestPartialReadFromPanicPreservesCommittedStatus(t *testing.T) {
	previous := otel.GetTracerProvider()
	spans := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans)))
	t.Cleanup(func() { otel.SetTracerProvider(previous) })
	metrics := NewMetrics()
	handler := MetricsMiddleware(metrics)(Tracing()(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.Copy(writer, &panicAfterChunk{})
	})))
	recorder, recovered := serveRecovered(handler)
	if recovered == nil || recorder.Code != http.StatusOK || recorder.Body.String() != "x" {
		t.Fatalf("panic=%v status=%d body=%q", recovered, recorder.Code, recorder.Body.String())
	}
	scrape := httptest.NewRecorder()
	MetricsHandler(metrics).ServeHTTP(scrape, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if !strings.Contains(scrape.Body.String(), `status="200"`) || strings.Contains(scrape.Body.String(), `status="500"`) {
		t.Errorf("metrics lost the committed status:\n%s", scrape.Body.String())
	}
	ended := spans.Ended()
	if len(ended) != 1 {
		t.Fatalf("spans=%d, want 1", len(ended))
	}
	found := false
	for _, attribute := range ended[0].Attributes() {
		if attribute.Key == "http.status_code" {
			found = true
			if attribute.Value.AsInt64() != http.StatusOK {
				t.Errorf("span status=%d, want 200", attribute.Value.AsInt64())
			}
		}
	}
	if !found {
		t.Error("span has no status attribute")
	}
}

func TestReadFromPreservesContentTypeSniffing(t *testing.T) {
	for _, middleware := range []Middleware{MetricsMiddleware(NewMetrics()), Tracing()} {
		server := httptest.NewServer(middleware(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			_, _ = io.Copy(writer, fileLike("<!doctype html><html><body>asset</body></html>"))
		})))
		client := server.Client()
		client.Timeout = 5 * time.Second
		response, err := client.Get(server.URL)
		if err != nil {
			server.Close()
			t.Fatal(err)
		}
		response.Body.Close()
		server.Close()
		if contentType := response.Header.Get("Content-Type"); contentType != "text/html; charset=utf-8" {
			t.Errorf("Content-Type=%q, want HTML", contentType)
		}
	}
}

// TestInvalidCodePanicRecordedAs500: WriteHeader(0) panics inside
// net/http before anything is sent. The metrics and tracing writers set
// wroteHeader before forwarding, so their recover took the response as
// started and recorded status 0 for the 500 the client gets.
func TestInvalidCodePanicRecordedAs500(t *testing.T) {
	invalid := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(0) })

	t.Run("metrics", func(t *testing.T) {
		m := NewMetrics()
		if _, v := serveRecovered(MetricsMiddleware(m)(invalid)); v == nil {
			t.Fatal("WriteHeader(0) did not panic")
		}
		scrape := httptest.NewRecorder()
		MetricsHandler(m).ServeHTTP(scrape, httptest.NewRequest(http.MethodGet, "/metrics", nil))
		if !strings.Contains(scrape.Body.String(), `status="500"`) {
			t.Fatalf("want the panicked request under 500:\n%s", scrape.Body.String())
		}
	})
	t.Run("tracing", func(t *testing.T) {
		prev := otel.GetTracerProvider()
		sr := tracetest.NewSpanRecorder()
		otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr)))
		t.Cleanup(func() { otel.SetTracerProvider(prev) })
		if _, v := serveRecovered(Tracing()(invalid)); v == nil {
			t.Fatal("WriteHeader(0) did not panic")
		}
		spans := sr.Ended()
		if len(spans) != 1 {
			t.Fatalf("spans = %d, want 1", len(spans))
		}
		for _, kv := range spans[0].Attributes() {
			if kv.Key == "http.status_code" && kv.Value.AsInt64() != http.StatusInternalServerError {
				t.Fatalf("span http.status_code = %d, want 500", kv.Value.AsInt64())
			}
		}
	})
}
