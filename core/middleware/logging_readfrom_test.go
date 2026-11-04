package middleware

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type firstReadFailure struct{}

func (firstReadFailure) Read([]byte) (int, error) {
	return 0, errors.New("source unavailable")
}

type loggingReadFromRecorder struct {
	http.ResponseWriter
	called bool
}

func (recorder *loggingReadFromRecorder) ReadFrom(source io.Reader) (int64, error) {
	recorder.called = true
	return io.Copy(recorder.ResponseWriter, source)
}

type loggingPushRecorder struct{}

func (loggingPushRecorder) Push(string, *http.PushOptions) error { return http.ErrNotSupported }

func loggingOptionalWriter(writer http.ResponseWriter, recorder *httptest.ResponseRecorder, mask int) http.ResponseWriter {
	hijacker := &hijackableRecorder{ResponseRecorder: recorder}
	pusher := loggingPushRecorder{}
	switch mask {
	case 1:
		return struct {
			http.ResponseWriter
			http.Flusher
		}{writer, recorder}
	case 2:
		return struct {
			http.ResponseWriter
			http.Hijacker
		}{writer, hijacker}
	case 3:
		return struct {
			http.ResponseWriter
			http.Flusher
			http.Hijacker
		}{writer, recorder, hijacker}
	case 4:
		return struct {
			http.ResponseWriter
			http.Pusher
		}{writer, pusher}
	case 5:
		return struct {
			http.ResponseWriter
			http.Flusher
			http.Pusher
		}{writer, recorder, pusher}
	case 6:
		return struct {
			http.ResponseWriter
			http.Hijacker
			http.Pusher
		}{writer, hijacker, pusher}
	case 7:
		return struct {
			http.ResponseWriter
			http.Flusher
			http.Hijacker
			http.Pusher
		}{writer, recorder, hijacker, pusher}
	default:
		return writer
	}
}

func TestLoggingReadFromLazyCommit(t *testing.T) {
	for _, test := range []struct {
		name   string
		source func() io.Reader
		status int
		body   string
	}{
		{"failure", func() io.Reader { return firstReadFailure{} }, http.StatusServiceUnavailable, "unavailable\n"},
		{"empty", func() io.Reader { return fileLike("") }, http.StatusNotFound, ""},
		{"body", func() io.Reader { return fileLike("body") }, http.StatusOK, "body"},
		{"partial-failure", func() io.Reader { return io.MultiReader(fileLike("body"), firstReadFailure{}) }, http.StatusOK, "bodyunavailable\n"},
	} {
		for mask := range 8 {
			for _, fast := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/interfaces-%d/fast-%t", test.name, mask, fast), func(t *testing.T) {
					recorder := httptest.NewRecorder()
					optional := loggingOptionalWriter(struct{ http.ResponseWriter }{recorder}, recorder, mask)
					readerFrom := &loggingReadFromRecorder{ResponseWriter: optional}
					var destination http.ResponseWriter = optional
					if fast {
						destination = struct {
							http.ResponseWriter
							io.ReaderFrom
						}{optional, readerFrom}
					}
					var log bytes.Buffer
					LoggingWithWriter(&log)(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
						if _, err := io.Copy(writer, test.source()); err != nil {
							http.Error(writer, "unavailable", http.StatusServiceUnavailable)
							return
						}
						writer.WriteHeader(http.StatusNotFound)
					})).ServeHTTP(destination, httptest.NewRequest(http.MethodGet, "/", nil))
					if recorder.Code != test.status || recorder.Body.String() != test.body {
						t.Errorf("wire status=%d body=%q; want status=%d body=%q", recorder.Code, recorder.Body.String(), test.status, test.body)
					}
					if !strings.Contains(log.String(), fmt.Sprintf(`"status":%d`, test.status)) {
						t.Errorf("want logged status %d; got %s", test.status, log.String())
					}
					if fast && !readerFrom.called {
						t.Error("underlying ReaderFrom was not reached")
					}
				})
			}
		}
	}
}

func TestLoggingReadFromLazyCommitWire(t *testing.T) {
	for _, test := range []struct {
		name   string
		source io.Reader
		status int
	}{
		{"failure", firstReadFailure{}, http.StatusServiceUnavailable},
		{"empty", fileLike(""), http.StatusNotFound},
		{"body", fileLike("body"), http.StatusOK},
	} {
		t.Run(test.name, func(t *testing.T) {
			var log bytes.Buffer
			server := httptest.NewServer(LoggingWithWriter(&log)(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				if _, err := io.Copy(writer, test.source); err != nil {
					http.Error(writer, "unavailable", http.StatusServiceUnavailable)
					return
				}
				writer.WriteHeader(http.StatusNotFound)
			})))
			defer server.Close()
			response, err := server.Client().Get(server.URL)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			server.Close()
			if response.StatusCode != test.status || !strings.Contains(log.String(), fmt.Sprintf(`"status":%d`, test.status)) {
				t.Fatalf("wire=%d log=%s; want %d", response.StatusCode, log.String(), test.status)
			}
		})
	}
}
