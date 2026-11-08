package setup

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/internal/browserpath"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

func TestWizardRetryKeepsPublicInput(t *testing.T) {
	if testing.Short() {
		t.Skip("browser E2E disabled in short mode")
	}
	browser, ok := browserpath.Find()
	if !ok {
		t.Skip("browser E2E requires Chrome, Chromium, or Edge")
	}
	r := New(Config{DisableToken: true, Complete: func(context.Context) (bool, error) { return false, nil }, Steps: []Step{{
		Name: "admin", Title: pageContinue, Fields: []Field{
			{Name: "email", Label: pageContinue},
			{Name: "password", Label: pageContinue, Secret: true, Validate: func(string) error { return i18n.NewError(pageStepFailed) }},
		},
	}}})
	srv := httptest.NewServer(r.Handler(func() {}, nil, nil))
	defer srv.Close()
	ctx := chromedptest.Context(t, chromedptest.AllocatorOptions(chromedp.ExecPath(browser)))
	var email, secret string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/setup"),
		chromedp.WaitVisible(`#setup-field-email`, chromedp.ByID),
		chromedp.SetValue(`#setup-field-email`, "operator@example.com", chromedp.ByID),
		chromedp.SetValue(`#setup-field-password`, "weak", chromedp.ByID),
		chromedp.Click(`button[type=submit]`, chromedp.ByQuery),
		chromedp.WaitVisible(`[aria-invalid=true]`, chromedp.ByQuery),
		chromedp.Value(`#setup-field-email`, &email, chromedp.ByID),
		chromedp.Value(`#setup-field-password`, &secret, chromedp.ByID),
	); err != nil {
		t.Fatal(err)
	}
	if email != "operator@example.com" || secret != "" {
		t.Fatalf("retry email=%q password=%q", email, secret)
	}
	if dir := os.Getenv("GOFASTR_UI_PROOF_DIR"); dir != "" {
		var png []byte
		if err := chromedp.Run(ctx, chromedp.FullScreenshot(&png, 90)); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "setup-retry.png"), png, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
