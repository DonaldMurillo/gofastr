package renderdiag

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The test writer latched a 103 Early Hints as the response, so the
// handler's 201 never reached the client.
func TestResponseKeepsStatusAfterHints(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w, _ = TestResponse(w, r)
		w.WriteHeader(http.StatusEarlyHints)
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, "made")
	}))
	defer srv.Close()
	resp, err := srv.Client().Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
}
