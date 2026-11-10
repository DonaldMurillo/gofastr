package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// GitHub's token endpoint answers a refused refresh grant with HTTP 200
// and an error document ({"error":"bad_refresh_token",...}), not a 4xx.
// GitHubProvider.RefreshToken read only the status, so it returned an
// OAuth2Token with an empty AccessToken and a nil error, and refreshFrom
// stored it: the access token the row held was overwritten with "" and
// the caller was handed an empty token as a success.
func TestGitHubRefusedRefreshKeepsTheStoredToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"error":"bad_refresh_token","error_description":"The refresh token passed is incorrect or expired.","error_uri":"https://docs.github.com"}`))
	}))
	t.Cleanup(srv.Close)
	gh := NewGitHubProvider("id", "secret", "https://app/cb")
	gh.tokenEndpoint, gh.httpClient = srv.URL, srv.Client()

	ts := newSQLOAuthTokenStore(t)
	ctx := context.Background()
	// A token that is still good for an hour: a forced refresh that the
	// provider refuses must leave it in place.
	stillValid := time.Now().Add(time.Hour).Truncate(time.Second)
	if err := ts.Save(ctx, OAuthTokenRecord{UserID: "u", Provider: "github",
		AccessToken: "at-still-valid", RefreshToken: "rt-0", Expiry: stillValid}); err != nil { // not-a-secret: fixture token for a fake provider
		t.Fatal(err)
	}

	rec, err := RefreshOAuthToken(ctx, ts, gh, "u")
	if err == nil {
		t.Errorf("RefreshOAuthToken returned no error for a refused refresh grant; record access token = %q", rec.AccessToken)
	}
	stored, gerr := ts.Get(ctx, "u", "github")
	if gerr != nil {
		t.Fatal(gerr)
	}
	if stored.AccessToken != "at-still-valid" {
		t.Errorf("stored access token = %q after a refused refresh, want the still-valid %q kept", stored.AccessToken, "at-still-valid")
	}
	if tok, verr := ValidOAuthToken(ctx, ts, gh, "u"); verr == nil && tok == "" {
		t.Errorf("ValidOAuthToken returned an empty token and a nil error")
	}
}

// rotatingRefresher is a provider that rotates the refresh token on every
// refresh grant (single-use refresh tokens), and whose caller goes away
// the moment the provider has answered.
type rotatingRefresher struct {
	hangUp context.CancelFunc
	spends int
}

func (p *rotatingRefresher) Name() string          { return "rot" }
func (p *rotatingRefresher) AuthURL(string) string { return "" }
func (p *rotatingRefresher) ExchangeCode(context.Context, string) (*OAuth2Token, error) {
	return nil, errors.New("unused")
}
func (p *rotatingRefresher) FetchUserInfo(context.Context, string) (*OAuth2UserInfo, error) {
	return nil, errors.New("unused")
}
func (p *rotatingRefresher) RefreshToken(_ context.Context, refreshToken string) (*OAuth2Token, error) {
	p.spends++
	if refreshToken != "rt-0" {
		return nil, errors.New("invalid_grant: refresh token already used")
	}
	// The provider has spent rt-0 and issued rt-1. The request that
	// asked for the token is abandoned right here (client hung up, or the
	// request deadline passed).
	p.hangUp()
	return &OAuth2Token{AccessToken: "at-1", RefreshToken: "rt-1", Expiry: time.Now().Add(time.Hour)}, nil
}

// A refresh whose request context ends between the provider's answer and
// the store write must still store the rotated grant: the old refresh
// token is spent at the provider, so a write the hang-up cancelled left
// the row holding a dead token (and a live lease) and the user had to
// reconnect.
func TestRefreshStoresRotatedGrantWhenCallerHangsUp(t *testing.T) {
	ts := newSQLOAuthTokenStore(t)
	bg := context.Background()
	if err := ts.Save(bg, OAuthTokenRecord{UserID: "u", Provider: "rot",
		AccessToken: "at-0", RefreshToken: "rt-0", Expiry: time.Now().Add(-time.Minute)}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(bg)
	defer cancel()
	p := &rotatingRefresher{hangUp: cancel}

	_, _ = RefreshOAuthToken(ctx, ts, p, "u")
	if p.spends != 1 {
		t.Fatalf("setup: the refresh token was spent %d times, want 1", p.spends)
	}

	stored, err := ts.Get(bg, "u", "rot")
	if err != nil {
		t.Fatal(err)
	}
	if stored.RefreshToken != "rt-1" {
		t.Errorf("stored refresh token = %q after the provider rotated it to %q: the grant the provider just issued was dropped, and %q is already spent",
			stored.RefreshToken, "rt-1", stored.RefreshToken)
	}
	// The next caller must be able to use the grant.
	next, cancelNext := context.WithTimeout(bg, 500*time.Millisecond)
	defer cancelNext()
	if tok, err := ValidOAuthToken(next, ts, p, "u"); err != nil || tok != "at-1" {
		t.Errorf("next ValidOAuthToken = %q, %v; want the access token the provider issued (%q)", tok, err, "at-1")
	}
}

// The code exchange reads the same 200-with-an-error document. A spent or
// wrong code must fail the exchange, naming GitHub's code and nothing
// else from the body, instead of yielding an empty token that fails
// later as a user-info error.
func TestGitHubRefusedExchangeIsAnError(t *testing.T) {
	for body, want := range map[string]string{
		`{"error":"bad_verification_code","error_description":"The code passed is incorrect or expired."}`: "github: token exchange was refused: bad_verification_code",
		`{"error":"<b>not a code</b>\r\nX-Injected: 1"}`:                                                   "github: token exchange was refused",
		`{}`: "github: token exchange returned no access token",
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		}))
		gh := NewGitHubProvider("id", "secret", "https://app/cb")
		gh.tokenEndpoint, gh.httpClient = srv.URL, srv.Client()
		tok, err := gh.ExchangeCode(context.Background(), "code")
		srv.Close()
		if err == nil || err.Error() != want {
			t.Errorf("ExchangeCode on %s = %+v, %v; want the error %q", body, tok, err, want)
		}
	}
}

// refusingRefresher reports a successful refresh that carries no access
// token, the shape a custom provider reading only the status produces.
type refusingRefresher struct{ rotatingRefresher }

func (p *refusingRefresher) RefreshToken(context.Context, string) (*OAuth2Token, error) {
	return &OAuth2Token{}, nil
}

// Whatever the provider, a refresh that returns no access token stores
// nothing and gives the lease back, so the next caller can try again.
func TestRefreshWithoutAnAccessTokenStoresNothing(t *testing.T) {
	ts := newSQLOAuthTokenStore(t)
	ctx := context.Background()
	if err := ts.Save(ctx, OAuthTokenRecord{UserID: "u", Provider: "rot",
		AccessToken: "at-0", RefreshToken: "rt-0", Expiry: time.Now().Add(-time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if _, err := RefreshOAuthToken(ctx, ts, &refusingRefresher{}, "u"); !errors.Is(err, errOAuthRefreshNoToken) {
		t.Fatalf("RefreshOAuthToken = %v, want errOAuthRefreshNoToken", err)
	}
	stored, err := ts.Get(ctx, "u", "rot")
	if err != nil || stored.AccessToken != "at-0" || stored.RefreshToken != "rt-0" {
		t.Fatalf("stored grant = %+v, %v; want it untouched", stored, err)
	}
	// The lease was released: a working provider refreshes at once.
	next, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	if tok, err := ValidOAuthToken(next, ts, &rotatingRefresher{hangUp: func() {}}, "u"); err != nil || tok != "at-1" {
		t.Fatalf("the refresh after a refused one = %q, %v; want %q", tok, err, "at-1")
	}
}
