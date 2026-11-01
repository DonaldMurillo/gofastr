package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// The default confirmation page speaks the caller's language and
// declares it, direction included.
func TestConfirmPageSpeaksLocale(t *testing.T) {
	r, plugin := magicLinkCSRFRouter(t)
	cat := i18n.NewMapCatalog()
	cat.Set("ar", "auth.confirm_title", i18n.Message{Text: "تأكيد تسجيل الدخول"})
	cat.Set("ar", "auth.confirm_as", i18n.Message{Text: "متابعة تسجيل الدخول باسم {{who}}؟"})
	cat.Set("ar", "auth.confirm_submit", i18n.Message{Text: "تسجيل الدخول"})
	h := i18n.Middleware(i18n.NewTranslator(cat, "en"))(r)

	token, err := createPurposeToken(context.Background(), plugin.tokenStore, purposeMagicLink, "a<b>@example.com", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/auth/magic-link/verify?token="+token, nil)
	req.Header.Set("Accept-Language", "ar-EG")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body := rec.Body.String()
	for _, want := range []string{
		`<html lang="ar" dir="rtl">`,
		"<title>تأكيد تسجيل الدخول</title>",
		"متابعة تسجيل الدخول باسم <strong>a&lt;b&gt;@example.com</strong>؟",
		`<button type="submit">تسجيل الدخول</button>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q:\n%s", want, body)
		}
	}
}

// With no translator the page is English and says so.
func TestConfirmPageEnglishByDefault(t *testing.T) {
	r, plugin := magicLinkCSRFRouter(t)
	token, err := createPurposeToken(context.Background(), plugin.tokenStore, purposeMagicLink, "alice@example.com", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/auth/magic-link/verify?token="+token, nil))
	body := rec.Body.String()
	for _, want := range []string{
		`<html lang="en" dir="ltr">`,
		"Continue to sign in as <strong>alice@example.com</strong>?",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q:\n%s", want, body)
		}
	}
}

// The password-strength refusals are phrases, so a form that shows
// them (the setup wizard's admin step) shows them in the reader's
// language, and errors.Is still matches the sentinel.
func TestPasswordStrengthErrorsArePhrased(t *testing.T) {
	cat := i18n.NewMapCatalog()
	cat.Set("fr", "auth.password_too_short", i18n.Message{Text: "mot de passe trop court"})
	cat.Set("fr", "auth.password_empty", i18n.Message{Text: "mot de passe vide"})
	ctx := i18n.WithContext(i18n.WithTranslator(context.Background(), i18n.NewTranslator(cat, "en")), i18n.Locale{Tag: "fr"})
	for in, want := range map[string]string{"x": "mot de passe trop court", "": "mot de passe vide"} {
		err := ValidatePasswordStrength(in)
		if got, _ := i18n.ErrorText(ctx, err, i18n.Phrase{}); got != want {
			t.Errorf("ValidatePasswordStrength(%q) renders %q; want %q", in, got, want)
		}
	}
	if !errors.Is(ValidatePasswordStrength("x"), ErrPasswordTooShort) {
		t.Error("errors.Is lost the sentinel")
	}
}

// BodyTemplate gets the request context, so an email body built from
// phrases speaks the requester's language.
func TestBodyTemplateGetsRequestLocale(t *testing.T) {
	sender := &mockEmailSender{}
	mgr, _, plugin := newMagicLinkManager(t, sender)
	plugin.config.BodyTemplate = func(ctx context.Context, url string) string {
		return i18n.FromContext(ctx).Tag + " " + url
	}
	r := mountMagicLinkRoutes(mgr)
	req := httptest.NewRequest(http.MethodPost, "/auth/magic-link/send", strings.NewReader(`{"email":"alice@example.com"}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(i18n.WithContext(req.Context(), i18n.Locale{Tag: "fr"}))
	r.ServeHTTP(httptest.NewRecorder(), req)
	if !strings.HasPrefix(sender.lastURL, "fr http://") {
		t.Fatalf("body = %q; want the template to see locale fr", sender.lastURL)
	}
}
