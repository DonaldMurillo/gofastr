package uihost

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// frenchRequest is a request whose reader reads French, with each phrase
// in tr translated to its value.
func frenchRequest(path string, tr map[i18n.Phrase]string) *http.Request {
	cat := i18n.NewMapCatalog()
	for p, text := range tr {
		cat.Set("fr", p.Key(), i18n.Message{Text: text})
	}
	ctx := i18n.WithContext(context.Background(), i18n.Locale{Tag: "fr"})
	ctx = i18n.WithTranslator(ctx, i18n.NewTranslator(cat, "en"))
	return httptest.NewRequest(http.MethodGet, path, nil).WithContext(ctx)
}

// A host with no root layout writes a bare error document; its words
// follow the reader's locale like the layout-rendered pages do.
func TestBareErrorPagesInLocale(t *testing.T) {
	ds := New(app.NewApp("x"))
	tr := map[i18n.Phrase]string{
		pageNotFoundTitle:       "404 : page introuvable",
		pageNoRoute:             "Aucune route pour {{path}}.",
		pageBackHome:            "Retour à l'accueil",
		pageNotFoundDocTitle:    "Introuvable : {{app}}",
		pageErrorTitle:          "Une erreur est survenue",
		pageServerErrorTitle:    "500 : erreur serveur",
		pageServerErrorDocTitle: "Erreur serveur : {{app}}",
		problemRenderFailed:     "La page n'a pas pu s'afficher.",
	}

	nf := httptest.NewRecorder()
	ds.serveNotFound(nf, frenchRequest("/nope", tr), "/nope")
	for _, want := range []string{"<title>Introuvable : x</title>", "404 : page introuvable", "Aucune route pour <code>/nope</code>.", "Retour à l&#39;accueil"} {
		if !strings.Contains(nf.Body.String(), want) {
			t.Errorf("bare 404 missing %q:\n%s", want, nf.Body.String())
		}
	}

	er := httptest.NewRecorder()
	ds.serveError(er, frenchRequest("/boom", tr), "/boom")
	if !strings.Contains(er.Body.String(), "Une erreur est survenue") {
		t.Errorf("bare 500 must be French:\n%s", er.Body.String())
	}

	se := httptest.NewRecorder()
	ds.serveServerError(se, frenchRequest("/boom", tr), "/boom")
	for _, want := range []string{"<title>Erreur serveur : x</title>", "500 : erreur serveur", "La page n&#39;a pas pu s&#39;afficher."} {
		if !strings.Contains(se.Body.String(), want) {
			t.Errorf("bare server error missing %q:\n%s", want, se.Body.String())
		}
	}
}

// A tab opened before a deploy gets the reload notice in its reader's
// language, on both the refresh body and the bare one. The sentence's
// punctuation is the translation's: Japanese ends on 。, not a period.
func TestStaleMarkupNoticeInLocale(t *testing.T) {
	tr := map[i18n.Phrase]string{
		pageAppUpdated: "アプリが更新されました。",
		pageReloadPage: "ページを再読み込みしてください。",
	}
	w := httptest.NewRecorder()
	writeStaleMarkupReload(w, frenchRequest("/a", tr), "/a", "")
	if want := `<p role="alert">アプリが更新されました。 <a href="/a">ページを再読み込みしてください。</a></p>`; !strings.Contains(w.Body.String(), want) {
		t.Errorf("refresh body missing %q:\n%s", want, w.Body.String())
	}
	w = httptest.NewRecorder()
	writeStaleMarkupReload(w, frenchRequest("/a", tr), "//evil", "")
	if want := `<p role="alert">アプリが更新されました。 ページを再読み込みしてください。</p>`; w.Body.String() != want {
		t.Errorf("bare body = %q, want %q", w.Body.String(), want)
	}
}
