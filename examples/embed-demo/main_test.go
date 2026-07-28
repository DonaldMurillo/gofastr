package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// buildDemo wires the same two halves main() does, against test servers.
func buildDemo(t *testing.T) (appSrv, customerSrv *httptest.Server, embeds *fembed.Host) {
	t.Helper()
	application := app.NewApp("Embed demo")
	application.Register("/reports", &reportsScreen{}, app.EmbedLayout())

	var err error
	embeds, err = fembed.New(fembed.Config{
		Surfaces: []fembed.Surface{{
			Name:    "reports",
			Path:    "/reports",
			Origins: []string{customerOrigin},
			Theme:   fembed.ThemeConfig{AllowTokens: []string{"color-primary"}},
		}},
		BurnStore: fembed.NewMemoryBurnStore(),
		Resolve:   func(_ context.Context, subject string) (any, error) { return subject, nil },
	})
	if err != nil {
		t.Fatalf("embed.New: %v", err)
	}
	embeds.SetKeys(demoKey("test-secret", "nonce"), demoKey("test-secret", "grant"))

	appSrv = httptest.NewServer(uihost.New(application, uihost.WithEmbed(embeds)))
	t.Cleanup(appSrv.Close)
	customerSrv = httptest.NewServer(customerSite(embeds))
	t.Cleanup(customerSrv.Close)
	return appSrv, customerSrv, embeds
}

// The customer's page must carry a FRESH nonce on every load. A nonce baked
// into a template is spent by the first visitor, and every visitor after them
// arrives as the same identity — the failure single-use exists to prevent.
func TestCustomerPageMintsAFreshNoncePerLoad(t *testing.T) {
	_, customer, _ := buildDemo(t)

	first := fetchNonce(t, customer.URL)
	second := fetchNonce(t, customer.URL)
	if first == "" || second == "" {
		t.Fatal("the customer page carries no nonce")
	}
	if first == second {
		t.Fatal("two page loads served the SAME nonce — it would be spent by the first visitor")
	}
}

func TestDemoHandshakeYieldsAnAuthenticatedPanel(t *testing.T) {
	appSrv, customer, _ := buildDemo(t)
	nonce := fetchNonce(t, customer.URL)

	body, _ := json.Marshal(map[string]string{"token": nonce, "origin": customerOrigin})
	resp, err := http.Post(appSrv.URL+"/__gofastr/embed-exchange", "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("exchange: status %d", resp.StatusCode)
	}
	var out struct {
		Grant string `json:"grant"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode grant: %v", err)
	}

	req, _ := http.NewRequest(http.MethodGet, appSrv.URL+"/__gofastr/embed/reports/content", nil)
	req.Header.Set("X-Gofastr-Embed", out.Grant)
	content, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("content: %v", err)
	}
	defer func() { _ = content.Body.Close() }()
	html, _ := io.ReadAll(content.Body)
	if content.StatusCode != http.StatusOK {
		t.Fatalf("content: status %d: %s", content.StatusCode, html)
	}
	if !strings.Contains(string(html), "avery@acme.example") {
		t.Fatalf("the panel did not render as the granted subject:\n%s", html)
	}
}

func fetchNonce(t *testing.T, base string) string {
	t.Helper()
	resp, err := http.Get(base + "/")
	if err != nil {
		t.Fatalf("get customer page: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	page, _ := io.ReadAll(resp.Body)
	const marker = `data-token="`
	i := strings.Index(string(page), marker)
	if i < 0 {
		return ""
	}
	rest := string(page)[i+len(marker):]
	return rest[:strings.IndexByte(rest, '"')]
}
