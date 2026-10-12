package rtc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/embed"
)

var _ framework.EmbedReserving = (*Signaler)(nil)

type embedProbeScreen struct{ p string }

func (s embedProbeScreen) RoutePath() string { return s.p }

// An embed grant installs its subject as the request user, and Authorize
// joins whoever the request user is. A grant for a surface with no Reach
// used to open an rtc room as its subject. The Signaler reserves its
// mounted path, and the embed gate refuses it before Authorize runs.
func TestEmbedGrantCannotJoinRTC(t *testing.T) {
	h, err := embed.New(embed.Config{
		Surfaces: []embed.Surface{{
			Name:    "reports",
			Screen:  embedProbeScreen{"/reports"},
			Origins: []string{"https://customer.example"},
		}},
		BurnStore: embed.NewMemoryBurnStore(),
		Resolve:   func(_ context.Context, subject string) (any, error) { return subject, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	h.SetKeys([]byte("nonce-key-0123456789abcdef012345"), []byte("grant-key-0123456789abcdef012345"))

	authorized := false
	sig := New(Config{
		ICEServers: []ICEServer{{URLs: []string{"stun:stun.example.net:3478"}}},
		Authorize: func(*http.Request) (Join, error) {
			authorized = true
			return Join{}, &HTTPError{Status: http.StatusUnauthorized, Message: "no"}
		},
	})
	defer sig.Close()
	if got := sig.ReservedEmbedPrefixes(); len(got) != 1 || got[0] != DefaultPath {
		t.Fatalf("ReservedEmbedPrefixes = %v, want [%s]", got, DefaultPath)
	}
	if err := h.AddReservedPrefixes(sig.ReservedEmbedPrefixes()...); err != nil {
		t.Fatal(err)
	}

	nonce, err := h.MintNonce(context.Background(), "reports", "viewer-42", "https://customer.example", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := h.Exchange(context.Background(), nonce, "https://customer.example")
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.Handle("GET "+DefaultPath, sig)
	req := httptest.NewRequest(http.MethodGet, DefaultPath+"?room=support-17", nil)
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	req.Header.Set(embed.GrantHeader, res.Grant)
	rec := httptest.NewRecorder()
	h.Middleware()(mux).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden || authorized {
		t.Fatalf("grant at %s: status %d, Authorize ran=%v; want 403 before Authorize", DefaultPath, rec.Code, authorized)
	}
}

// A relocated Signaler reserves where it actually lives.
func TestRTCReservesConfiguredPath(t *testing.T) {
	sig := New(Config{
		Path:       "/live/signal",
		ICEServers: []ICEServer{{URLs: []string{"stun:stun.example.net:3478"}}},
		Authorize:  func(*http.Request) (Join, error) { return Join{}, nil },
	})
	defer sig.Close()
	if got := sig.ReservedEmbedPrefixes(); len(got) != 1 || got[0] != "/live/signal" {
		t.Fatalf("ReservedEmbedPrefixes = %v, want [/live/signal]", got)
	}
}
