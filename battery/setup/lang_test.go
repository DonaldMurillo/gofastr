package setup

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

func localeCtx(ctx context.Context, tag string) context.Context {
	cat := i18n.NewMapCatalog()
	for _, l := range []string{"fr", "ar"} {
		cat.Set(l, "setup.title", i18n.Message{Text: "Setup " + l})
	}
	cat.Set("ar", "setup.admin_step", i18n.Message{Text: "إنشاء المسؤول"})
	cat.Set("ar", "setup.admin_email", i18n.Message{Text: "بريد المسؤول"})
	cat.Set("ar", "setup.email_required", i18n.Message{Text: "البريد مطلوب"})
	ctx = i18n.WithTranslator(ctx, i18n.NewTranslator(cat, "en"))
	return i18n.WithContext(ctx, i18n.Locale{Tag: tag})
}

// The wizard says its phrases in the reader's locale, so its document
// declares that language and direction rather than a fixed English.
func TestWizardDeclaresReaderLang(t *testing.T) {
	done := false
	h := buildTestRunner(t, true, &done).Handler(func() {}, nil, nil)
	for _, tc := range []struct{ tag, want string }{
		{"", `<html dir="ltr" lang="en">`},
		{"fr", `<html dir="ltr" lang="fr">`},
		{"ar", `<html dir="rtl" lang="ar">`},
	} {
		req := httptest.NewRequest(http.MethodGet, "/setup", nil)
		if tc.tag != "" {
			req = req.WithContext(localeCtx(req.Context(), tc.tag))
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if body := w.Body.String(); !strings.Contains(body, tc.want) {
			t.Errorf("locale %q: want %s in %.120q", tc.tag, tc.want, body)
		}
	}
}

// AdminStep's title, labels and validation messages are phrases, and so
// is the wizard title.
func TestAdminStepSpeaksLocale(t *testing.T) {
	step := Step{
		Name:   "admin",
		Title:  adminStepTitle,
		Fields: []Field{{Name: "ADMIN_EMAIL", Label: adminEmailLabel, Validate: validateAdminEmail}},
		Run:    func(context.Context, map[string]string) error { return nil },
	}
	r := New(Config{
		DisableToken: true,
		Complete:     func(context.Context) (bool, error) { return false, nil },
		Steps:        []Step{step},
	})
	h := r.Handler(func() {}, nil, nil)

	get := httptest.NewRequest(http.MethodGet, "/setup", nil)
	get = get.WithContext(localeCtx(get.Context(), "ar"))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, get)
	body := w.Body.String()
	for _, want := range []string{"<title>Setup ar</title>", "إنشاء المسؤول", "بريد المسؤول"} {
		if !strings.Contains(body, want) {
			t.Errorf("wizard lacks %q", want)
		}
	}

	post := httptest.NewRequest(http.MethodPost, "/setup", strings.NewReader("ADMIN_EMAIL="))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	post.Header.Set("Sec-Fetch-Site", "same-origin")
	post = post.WithContext(localeCtx(post.Context(), "ar"))
	w = httptest.NewRecorder()
	h.ServeHTTP(w, post)
	if !strings.Contains(w.Body.String(), "البريد مطلوب") {
		t.Errorf("validation error not in the reader's locale:\n%s", w.Body.String())
	}
}
