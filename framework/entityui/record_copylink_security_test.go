package entityui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/handler"
)

// copyLinkPage renders invoice inv-1's record page for a request that
// carries the given X-Forwarded-Proto.
func copyLinkPage(t *testing.T, x *testUI, proto string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/rec/invoices/inv-1", nil)
	req.Header.Set("X-Forwarded-Proto", proto)
	ctx := handler.SetUser(app.WithRequest(context.Background(), req), &testUser{id: "u1"})
	return string(x.ui.Record("invoices", "inv-1").Base("/rec/invoices").RenderCtx(ctx))
}

// The copy link is built from the request, and X-Forwarded-Proto is
// request input: only an exact http or https may pick the scheme, so a
// forged value cannot put another origin in the link the reader copies.
func TestCopyLinkRefusesForgedScheme(t *testing.T) {
	x := newInvoiceUI(t)
	for _, proto := range []string{"https://evil.example/p", "https,http", "javascript"} {
		body := copyLinkPage(t, x, proto)
		if strings.Contains(body, "evil.example") || strings.Contains(body, proto+"://") {
			t.Fatalf("X-Forwarded-Proto %q reached the copy link:\n%s", proto, body)
		}
		if !strings.Contains(body, ">http://example.com/rec/invoices/inv-1<") {
			t.Fatalf("X-Forwarded-Proto %q: want the request's own http origin:\n%s", proto, body)
		}
	}
	if body := copyLinkPage(t, x, "https"); !strings.Contains(body, ">https://example.com/rec/invoices/inv-1<") {
		t.Fatalf("an exact https forwarded scheme is honored:\n%s", body)
	}
}
