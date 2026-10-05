package island

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

// An island stream re-validates the principal its request carried
// (handler.RecheckPrincipal) before every delivery, so a session revoked
// mid-stream stops receiving updates at the next push instead of at the
// stream bound.
func TestIslandStreamClosesOnRevocation(t *testing.T) {
	mgr := NewManager()
	var revoked atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := handler.WithPrincipalCheck(r.Context(), func(c context.Context) (context.Context, bool) {
			return c, !revoked.Load()
		})
		mgr.ServeSSE(w, r.WithContext(ctx))
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"?session=s-rev", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	mgr.PushUpdate(IslandUpdate{IslandID: "i", HTML: "<p>before-revoke</p>"}, "s-rev")
	br := bufio.NewReader(resp.Body)
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("stream ended before the pre-revocation update: %v", err)
		}
		if strings.Contains(line, "before-revoke") {
			break
		}
	}

	revoked.Store(true)
	mgr.PushUpdate(IslandUpdate{IslandID: "i", HTML: "<p>after-revoke-secret</p>"}, "s-rev")
	rest, err := io.ReadAll(br)
	if ctx.Err() != nil {
		t.Fatalf("SECURITY: island stream still open after the principal was revoked (%v)", err)
	}
	if strings.Contains(string(rest), "after-revoke-secret") {
		t.Fatalf("SECURITY: island stream delivered an update after revocation: %q", rest)
	}
}
