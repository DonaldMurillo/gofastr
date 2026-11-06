package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

func TestBFFAcceptsConfiguredTokenBrand(t *testing.T) {
	for _, brand := range []string{"", "acme_"} {
		cfg := BFFPostureConfig{AllowedOrigins: []string{"https://app.example.com"}}
		if err := json.Unmarshal([]byte(`{"TokenPrefix":"`+brand+`"}`), &cfg); err != nil {
			t.Fatal(err)
		}
		mgr := New(AuthConfig{DevMode: true, UserStore: newMemoryUserStore()})
		app := framework.NewApp(WithBFFPosture(mgr, cfg))
		app.Router().Get("/api/check", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))
		wantPrefix := brand
		if wantPrefix == "" {
			wantPrefix = TokenPrefix
		}
		for _, credential := range []string{"gfsk_example", "acme_example", "jwt.example.signature"} {
			req := httptest.NewRequest(http.MethodGet, "/api/check", nil)
			req.Header.Set("Authorization", "Bearer "+credential)
			rec := httptest.NewRecorder()
			app.Router().ServeHTTP(rec, req)
			want := http.StatusUnauthorized
			if credential == wantPrefix+"example" {
				want = http.StatusNoContent
			}
			if rec.Code != want {
				t.Errorf("brand %q credential %q: status %d; want %d", brand, credential, rec.Code, want)
			}
		}
	}
}

func TestBFFRejectsInvalidTokenBrand(t *testing.T) {
	cfg := BFFPostureConfig{AllowedOrigins: []string{"https://app.example.com"}}
	if err := json.Unmarshal([]byte(`{"TokenPrefix":"/"}`), &cfg); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if recover() == nil {
			t.Error("invalid token prefix was accepted")
		}
	}()
	WithBFFPosture(New(AuthConfig{DevMode: true, UserStore: newMemoryUserStore()}), cfg)
}
