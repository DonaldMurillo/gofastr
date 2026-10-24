package service

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

func pagesCtx(tag string) context.Context {
	cat := i18n.NewMapCatalog()
	cat.Set("ar", "service.signing_in", i18n.Message{Text: "جارٍ تسجيل الدخول"})
	cat.Set("ar", "service.sign_in", i18n.Message{Text: "تسجيل الدخول"})
	cat.Set("ar", "service.signed_in", i18n.Message{Text: "تم تسجيل الدخول."})
	cat.Set("ar", "service.continue", i18n.Message{Text: "متابعة"})
	ctx := i18n.WithTranslator(context.Background(), i18n.NewTranslator(cat, "en"))
	return i18n.WithContext(ctx, i18n.Locale{Tag: tag})
}

func TestSignInPageSpeaksLocale(t *testing.T) {
	page := signInPage(pagesCtx("ar"), "http://127.0.0.1:1/__login", "tok")
	for _, want := range []string{
		`<html lang="ar" dir="rtl">`,
		"<title>جارٍ تسجيل الدخول</title>",
		`<button type="submit">تسجيل الدخول</button>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("sign-in page lacks %q:\n%s", want, page)
		}
	}
}

func TestSignedInPageSpeaksLocale(t *testing.T) {
	page := signedInPage(pagesCtx("ar"))
	for _, want := range []string{
		`<html lang="ar" dir="rtl">`,
		"<p>تم تسجيل الدخول. <a href=\"/\">متابعة</a></p>",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("signed-in page lacks %q:\n%s", want, page)
		}
	}
	if en := signedInPage(context.Background()); !strings.Contains(en, `<html lang="en" dir="ltr">`) || !strings.Contains(en, "Signed in.") {
		t.Errorf("English signed-in page:\n%s", en)
	}
}

// `service open` has no request: the page it writes speaks the
// language the environment names.
func TestOpenPageUsesEnvLocale(t *testing.T) {
	t.Setenv("LANGUAGE", "")
	t.Setenv("LC_ALL", "ar_EG.UTF-8")
	cat := i18n.NewMapCatalog()
	cat.Set("ar", "service.sign_in", i18n.Message{Text: "تسجيل الدخول"})
	prev := i18n.Default()
	i18n.SetDefault(i18n.NewTranslator(cat, "en"))
	defer i18n.SetDefault(prev)
	page := signInPage(openCtx(), "http://127.0.0.1:1/__login", "tok")
	if !strings.Contains(page, `<html lang="ar" dir="rtl">`) || !strings.Contains(page, "تسجيل الدخول</button>") {
		t.Fatalf("open page ignored LC_ALL:\n%s", page)
	}
}
