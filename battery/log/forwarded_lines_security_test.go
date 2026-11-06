package log

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAccessLogAllForwardedLines(t *testing.T) {
	for _, first := range []string{"203.0.113.9", ""} {
		for _, trusted := range []bool{false, true} {
			var output bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&output, nil))
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			request.RemoteAddr = "192.0.2.1:5000"
			request.Header["X-Forwarded-For"] = []string{first, "198.51.100.7"}
			accessMiddleware(logger, trusted)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			})).ServeHTTP(httptest.NewRecorder(), request)
			var entry struct {
				Remote       string `json:"remote"`
				ForwardedFor string `json:"forwarded_for"`
			}
			if err := json.Unmarshal(output.Bytes(), &entry); err != nil {
				t.Fatal(err)
			}
			wantRemote := request.RemoteAddr
			if trusted {
				wantRemote = "198.51.100.7"
			}
			if entry.Remote != wantRemote || entry.ForwardedFor != strings.Join(request.Header.Values("X-Forwarded-For"), ", ") {
				t.Errorf("first=%q trusted=%v: remote=%q forwarded=%q", first, trusted, entry.Remote, entry.ForwardedFor)
			}
		}
	}
}
