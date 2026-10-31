package relay

import (
	"bytes"
	"io"
	"net/http"
	"testing"
	"time"
)

// TestOversizedResponseAbortsStream: an upstream body past the route's
// budget must reach the client as a broken stream, never as a clean
// 200 that ends early, which a client (or a cache) takes for the whole
// response. A body of exactly the budget still arrives whole.
func TestOversizedResponseAbortsStream(t *testing.T) {
	const budget = 4 << 10
	for _, tc := range []struct {
		name    string
		size    int
		wantErr bool
	}{
		{"at budget", budget, false},
		{"past budget", budget + 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := startRelay(t, func(up string) Config {
				cfg := defaultCfg(up)
				cfg.Routes[0].MaxBodyBytes = budget
				return cfg
			}, func(w http.ResponseWriter, _ *http.Request) {
				// Chunked (flushed, no Content-Length): a clean
				// terminating chunk is all a client has to judge by.
				_, _ = w.Write(bytes.Repeat([]byte("x"), tc.size/2))
				w.(http.Flusher).Flush()
				_, _ = w.Write(bytes.Repeat([]byte("x"), tc.size-tc.size/2))
				// Hold the end back, so the relay reads the full budget
				// before it can see EOF: a body of exactly the budget is
				// told apart from a longer one only by reading on.
				w.(http.Flusher).Flush()
				time.Sleep(30 * time.Millisecond)
			})
			client := &http.Client{Timeout: 10 * time.Second}
			res, err := client.Get(base + "/__gofastr/t/e/body")
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			defer res.Body.Close()
			body, readErr := io.ReadAll(res.Body)
			if tc.wantErr && readErr == nil {
				t.Fatalf("read %d of %d bytes and a clean end: the cut-off body looks whole", len(body), tc.size)
			}
			if !tc.wantErr && (readErr != nil || len(body) != tc.size) {
				t.Fatalf("read %d of %d bytes, err %v: a body at the budget must arrive whole", len(body), tc.size, readErr)
			}
		})
	}
}
