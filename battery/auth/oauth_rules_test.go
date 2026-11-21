package auth

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOAuthTokenWrongKeyFailsToOpen(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver unavailable")
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	a, err := NewSQLOAuthTokenStore(db, SQLOAuthTokenStoreConfig{EncryptionKey: []byte("key-one")})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Save(ctx, OAuthTokenRecord{UserID: "u1", Provider: "google", AccessToken: "acc"}); err != nil {
		t.Fatal(err)
	}
	b, err := NewSQLOAuthTokenStore(db, SQLOAuthTokenStoreConfig{EncryptionKey: []byte("key-two")})
	if err != nil {
		t.Fatal(err)
	}
	if rec, err := b.Get(ctx, "u1", "google"); err == nil {
		t.Errorf("a row sealed under another key opened: %+v", rec)
	}
}

func TestOIDCEcPointOffCurveRejected(t *testing.T) {
	one := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	if _, err := buildECKey("P-256", one, one); err == nil {
		t.Error("an off-curve P-256 point was accepted as a key")
	}
}

func TestGitHubPublicEmailUnconfirmedIsUnverified(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/user", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 42, "email": "alice@example.com"})
	})
	mux.HandleFunc("/user/emails", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	prov := NewGitHubProvider("cid", "csec", "http://localhost/cb")
	prov.userInfoEndpoint = srv.URL + "/user"
	tr := &http.Transport{}
	t.Cleanup(tr.CloseIdleConnections)
	prov.httpClient = &http.Client{Timeout: 10 * time.Second, Transport: tr}
	info, err := prov.FetchUserInfo(context.Background(), "tok")
	if err != nil {
		t.Fatal(err)
	}
	if info.Email != "alice@example.com" || info.EmailVerified {
		t.Errorf("unconfirmed public email = %q verified=%v, want it kept and unverified", info.Email, info.EmailVerified)
	}
}
