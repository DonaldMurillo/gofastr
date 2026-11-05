package middleware

import (
	"compress/gzip"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCompressKeepsIdentityReadFrom(t *testing.T) {
	for _, test := range []struct {
		name, accepts, contentType, prefix string
	}{
		{name: "identity text", contentType: "text/plain"},
		{name: "sniffed text"},
		{name: "refused gzip", accepts: "gzip;q=0", contentType: "text/html"},
		{name: "binary", accepts: "gzip", contentType: "application/octet-stream"},
		{name: "sniffed image", accepts: "gzip", prefix: "\x89PNG\r\n\x1a\n"},
	} {
		for _, initialWrite := range []int{0, 3} {
			t.Run(test.name+string(rune('0'+initialWrite)), func(t *testing.T) {
				body := test.prefix + strings.Repeat("response bytes\n", 4096)
				recorder := &readerFromRecorder{ResponseRecorder: httptest.NewRecorder()}
				request := httptest.NewRequest(http.MethodGet, "/file", nil)
				request.Header.Set("Accept-Encoding", test.accepts)
				serve := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					if test.contentType != "" {
						w.Header().Set("Content-Type", test.contentType)
					}
					w.WriteHeader(http.StatusCreated)
					if initialWrite != 0 {
						if _, err := io.WriteString(w, body[:initialWrite]); err != nil {
							t.Fatal(err)
						}
					}
					copied, err := io.Copy(w, fileLike(body[initialWrite:]))
					if err != nil || copied != int64(len(body)-initialWrite) {
						t.Fatalf("copy = %d, %v", copied, err)
					}
					w.WriteHeader(http.StatusBadGateway)
				})
				Compress()(serve).ServeHTTP(recorder, request)
				if !recorder.readFrom {
					t.Error("identity response hid the underlying ReadFrom")
				}
				if recorder.Body.String() != body || recorder.Code != http.StatusCreated {
					t.Errorf("identity copy changed body or status: bytes=%d status=%d", recorder.Body.Len(), recorder.Code)
				}
				if recorder.Header().Get("Content-Encoding") != "" {
					t.Error("identity response was compressed")
				}
			})
		}
	}
}

func TestCompressReadFromKeepsGzip(t *testing.T) {
	for _, etag := range []string{"", `"file"`} {
		body := strings.Repeat("compressible response\n", 2048)
		recorder := &readerFromRecorder{ResponseRecorder: httptest.NewRecorder()}
		request := httptest.NewRequest(http.MethodGet, "/file", nil)
		request.Header.Set("Accept-Encoding", "gzip")
		Compress()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("ETag", etag)
			if _, err := io.Copy(w, fileLike(body)); err != nil {
				t.Fatal(err)
			}
		})).ServeHTTP(recorder, request)
		if recorder.readFrom || recorder.Header().Get("Content-Encoding") != "gzip" {
			t.Fatalf("gzip response bypassed compression: ReadFrom=%v", recorder.readFrom)
		}
		decoder, err := gzip.NewReader(recorder.Body)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := io.ReadAll(decoder)
		closeErr := decoder.Close()
		if err != nil || closeErr != nil || string(decoded) != body {
			t.Fatalf("gzip copy lost bytes: read=%v close=%v bytes=%d", err, closeErr, len(decoded))
		}
	}
}

type compressReadError struct{ err error }

func (source compressReadError) Read([]byte) (int, error) { return 0, source.err }

func TestCompressEmptyCopyKeepsStatus(t *testing.T) {
	want := errors.New("source failed")
	for _, contentType := range []string{"text/plain", "application/octet-stream", ""} {
		for _, accepts := range []string{"", "gzip", "gzip;q=0"} {
			for _, sourceErr := range []error{io.EOF, want} {
				t.Run(contentType+"/"+accepts+"/"+sourceErr.Error(), func(t *testing.T) {
					recorder := &readerFromRecorder{ResponseRecorder: httptest.NewRecorder()}
					request := httptest.NewRequest(http.MethodGet, "/file", nil)
					request.Header.Set("Accept-Encoding", accepts)
					Compress()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						if contentType != "" {
							w.Header().Set("Content-Type", contentType)
						}
						copied, err := io.Copy(w, compressReadError{err: sourceErr})
						if copied != 0 || (sourceErr == io.EOF && err != nil) || (sourceErr != io.EOF && !errors.Is(err, sourceErr)) {
							t.Fatalf("empty copy = %d, %v", copied, err)
						}
						w.WriteHeader(http.StatusServiceUnavailable)
						if _, err := io.WriteString(w, "unavailable"); err != nil {
							t.Fatal(err)
						}
					})).ServeHTTP(recorder, request)
					if recorder.Code != http.StatusServiceUnavailable || recorder.Body.String() != "unavailable" {
						t.Fatalf("response = %d %q, want 503 unavailable", recorder.Code, recorder.Body.String())
					}
				})
			}
		}
	}
}

func TestCompressReadFromReturnsErrors(t *testing.T) {
	want := errors.New("source failed")
	for _, sniff := range []bool{false, true} {
		recorder := httptest.NewRecorder()
		Compress()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if !sniff {
				w.Header().Set("Content-Type", "text/plain")
			}
			source := io.MultiReader(fileLike("partial"), compressReadError{err: want})
			copied, err := io.Copy(w, struct{ io.Reader }{source})
			if copied != 7 || !errors.Is(err, want) {
				t.Fatalf("copy = %d, %v, want partial bytes and source error", copied, err)
			}
		})).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/file", nil))
		if recorder.Body.String() != "partial" {
			t.Fatalf("partial response = %q", recorder.Body.String())
		}
	}
}

func TestCompressServeContentDirect(t *testing.T) {
	body := strings.Repeat("file contents\n", 4096)
	recorder := &readerFromRecorder{ResponseRecorder: httptest.NewRecorder()}
	Compress()(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		http.ServeContent(w, request, "file.txt", time.Time{}, strings.NewReader(body))
	})).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/file.txt", nil))
	if !recorder.readFrom || recorder.Body.String() != body {
		t.Fatalf("ServeContent lost direct copying: ReadFrom=%v, bytes=%d", recorder.readFrom, recorder.Body.Len())
	}
}

func TestCompressReadFromAfterHijack(t *testing.T) {
	writer := &compressWriter{hijacked: true}
	reader, ok := any(writer).(io.ReaderFrom)
	if !ok {
		t.Fatal("compression writer does not implement ReadFrom")
	}
	copied, err := reader.ReadFrom(strings.NewReader("unused"))
	if copied != 0 || !errors.Is(err, http.ErrHijacked) {
		t.Fatalf("hijacked ReadFrom = %d, %v", copied, err)
	}
}
