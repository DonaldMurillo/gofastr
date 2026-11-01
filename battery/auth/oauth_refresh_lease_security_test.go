package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// Decision 2026-11-01: refresh across replicas fails closed. Two replicas
// refreshing one grant at once both spent its refresh token; a provider
// that detects reuse revokes the whole grant. A replica now leases the
// grant in the store before spending, and one that finds another's lease
// waits for that refresh to land and never spends the token itself.

func leaseRig(t *testing.T) (*SQLOAuthTokenStore, *GitHubProvider, *atomic.Int32) {
	t.Helper()
	var spends atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		spends.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "at-new", "refresh_token": "rt-new", "expires_in": 3600})
	}))
	t.Cleanup(srv.Close)
	gh := NewGitHubProvider("id", "secret", "https://app/cb")
	gh.tokenEndpoint, gh.httpClient = srv.URL, srv.Client()
	ts := newSQLOAuthTokenStore(t)
	if err := ts.Save(context.Background(), OAuthTokenRecord{UserID: "u", Provider: "github",
		AccessToken: "at-old", RefreshToken: "rt-0", Expiry: time.Now().Add(-time.Minute)}); err != nil {
		t.Fatal(err)
	}
	return ts, gh, &spends
}

func TestRefreshWaitsForAnotherReplicasLease(t *testing.T) {
	ts, gh, spends := leaseRig(t)
	ctx := context.Background()
	// Replica B holds the lease and is mid-exchange.
	if ok, err := ts.LeaseRefresh(ctx, "u", "github", "rt-0", time.Now().Add(oauthRefreshLease)); err != nil || !ok {
		t.Fatalf("setup lease: %v %v", ok, err)
	}
	go func() {
		time.Sleep(150 * time.Millisecond)
		_, _ = ts.SaveIfRefreshUnchanged(ctx, OAuthTokenRecord{UserID: "u", Provider: "github",
			AccessToken: "at-b", RefreshToken: "rt-b", Expiry: time.Now().Add(time.Hour)}, "rt-0")
	}()
	rec, err := RefreshOAuthToken(ctx, ts, gh, "u")
	if err != nil {
		t.Fatalf("refresh behind another replica's lease: %v", err)
	}
	if rec.AccessToken != "at-b" {
		t.Errorf("got %q, want replica B's grant", rec.AccessToken)
	}
	if n := spends.Load(); n != 0 {
		t.Errorf("SECURITY: the refresh token was spent %d times while another replica held the lease", n)
	}
}

func TestRefreshFailsClosedWhenTheLeaseHolderNeverFinishes(t *testing.T) {
	ts, gh, spends := leaseRig(t)
	if ok, _ := ts.LeaseRefresh(context.Background(), "u", "github", "rt-0", time.Now().Add(oauthRefreshLease)); !ok {
		t.Fatal("setup lease")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if _, err := RefreshOAuthToken(ctx, ts, gh, "u"); !errors.Is(err, ErrOAuthRefreshInProgress) {
		t.Errorf("err = %v, want ErrOAuthRefreshInProgress", err)
	}
	if n := spends.Load(); n != 0 {
		t.Errorf("SECURITY: the refresh token was spent %d times under another replica's lease", n)
	}
}

// A lapsed lease (its holder died) frees the grant, and a successful
// refresh clears the lease so the next one is not held off.
func TestRefreshLeaseLapsesAndClears(t *testing.T) {
	ts, gh, spends := leaseRig(t)
	ctx := context.Background()
	if ok, _ := ts.LeaseRefresh(ctx, "u", "github", "rt-0", time.Now().Add(-time.Second)); !ok {
		t.Fatal("setup lease")
	}
	rec, err := RefreshOAuthToken(ctx, ts, gh, "u")
	if err != nil || rec.AccessToken != "at-new" || spends.Load() != 1 {
		t.Fatalf("refresh after a lapsed lease: %+v %v (spends %d)", rec, err, spends.Load())
	}
	if ok, err := ts.LeaseRefresh(ctx, "u", "github", "rt-new", time.Now().Add(time.Minute)); err != nil || !ok {
		t.Errorf("the lease was not cleared by the successful refresh: %v %v", ok, err)
	}
}
