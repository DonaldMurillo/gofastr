package admin

import (
	"net/http"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// inSpanish serves h with a Spanish locale and a translator over cat,
// the context the app's i18n middleware hands a request.
func inSpanish(h http.Handler, cat i18n.Catalog) http.Handler {
	tr := i18n.NewTranslator(cat, "en")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := i18n.WithTranslator(i18n.WithContext(r.Context(), i18n.Locale{Tag: "es"}), tr)
		h.ServeHTTP(w, r.WithContext(ctx))
	})
}

// The words an app hands the admin (a page's title, a card's heading, a
// link's label, a figure and its detail) are drawn in the reader's
// language.
func TestConfigWordsTranslate(t *testing.T) {
	words := map[i18n.Phrase]string{
		testPhrase("Reports"):     "Informes",
		testPhrase("Revenue"):     "Ingresos",
		testPhrase("Help center"): "Centro de ayuda",
		testPhrase("Open orders"): "Pedidos abiertos",
		testPhrase("owed"):        "adeudado",
	}
	cat := i18n.NewMapCatalog()
	for ph, es := range words {
		cat.Set("es", ph.Key(), i18n.Message{Text: es})
	}
	reports := page("/reports", "Reports", says("report body"))
	reports.Nav = &entity.EntityNav{Group: "insights", Icon: "chart"}
	x := ordersEnv(t, Config{
		Entities: []string{"orders"},
		Pages:    []Page{reports},
		Cards:    []Card{{Key: "revenue", Title: testPhrase("Revenue"), Build: says("$42")}},
		Links:    []Link{{Label: testPhrase("Help center"), Href: "/help", Group: "help"}},
		Metrics: []Metric{{Label: testPhrase("Open orders"), Entity: "orders", Where: `status = "open"`,
			Detail: &Metric{Label: testPhrase("owed"), Agg: "sum", Field: "amount", Where: `status = "open"`, Format: "money"}}},
	})
	h := inSpanish(x.as(theAdmin), cat)
	dash := get(h, "/admin").Body.String()
	for _, want := range []string{"Informes", "Ingresos", "Centro de ayuda", "Pedidos abiertos", "adeudado"} {
		if !strings.Contains(dash, want) {
			t.Errorf("the dashboard lacks %q:\n%s", want, dash)
		}
	}
	for _, en := range []string{">Reports<", ">Revenue<", "Help center", "Open orders", " owed"} {
		if strings.Contains(dash, en) {
			t.Errorf("the dashboard still says %q", en)
		}
	}
	if body := get(h, "/admin/reports").Body.String(); !strings.Contains(body, "<title>Informes") {
		t.Errorf("the page's title is not translated:\n%s", body)
	}
	if body := get(h, "/admin/_metric/0").Body.String(); !strings.Contains(body, "Pedidos abiertos") {
		t.Errorf("a polled figure is not translated:\n%s", body)
	}
}
