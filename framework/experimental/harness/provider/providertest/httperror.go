package providertest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/provider"
)

// AssertChatHTTPError401 verifies the shared client's 401 path surfaces as
// an error carrying the adapter's own provider name (name prefixes the
// message). It replaces the identical TestChatHTTPError401 bodies formerly
// kept in the openrouter and zai adapter test packages.
func AssertChatHTTPError401(t testing.TB, newProvider func(baseURL string) provider.Provider, name string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("invalid key"))
	}))
	defer srv.Close()

	p := newProvider(srv.URL)
	_, err := p.Chat(context.Background(), &provider.Request{Model: "m"})
	if want := name + ": HTTP 401: invalid key"; err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}
