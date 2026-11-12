package crud

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// ?fields=title&fields=bogus answered 200 with the title projection while
// ?fields=bogus alone answered 400: the handler read the first value only,
// so a later one was never validated. ?where= and ?q= had the same shape.
// Each is read as one value, so a repeated key is refused like a repeated
// include, on the list and on read-one.
func TestRepeatedSingleValueKeysRefused(t *testing.T) {
	ch := thingsHandler(t, entity.EntityConfig{
		Exposure:     &entity.ExposureConfig{Public: true},
		SearchFields: []string{"title"},
	})
	if _, err := ch.DB.ExecContext(context.Background(), `INSERT INTO things (id, title) VALUES ('1', 'one')`); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		"fields=title&fields=bogus",
		"fields=title&fields=title",
		`where={"field":"title","op":"eq","value":"one"}&where=NOTJSON`,
		"q=one&q=zzz",
		"q=&q=one",
	} {
		lw := httptest.NewRecorder()
		ch.List()(lw, httptest.NewRequest(http.MethodGet, "/api/things?"+raw, nil))
		if lw.Code != http.StatusBadRequest || !strings.Contains(lw.Body.String(), "query_key_repeated") {
			t.Errorf("list %s = %d %s, want 400 query_key_repeated", raw, lw.Code, lw.Body)
		}
	}
	gw := httptest.NewRecorder()
	gr := httptest.NewRequest(http.MethodGet, "/api/things/1?fields=title&fields=bogus", nil)
	gr.SetPathValue("id", "1")
	ch.Get()(gw, gr)
	if gw.Code != http.StatusBadRequest || !strings.Contains(gw.Body.String(), "query_key_repeated") {
		t.Errorf("read-one repeated fields = %d %s, want 400 query_key_repeated", gw.Code, gw.Body)
	}
	ok := httptest.NewRecorder()
	ch.List()(ok, httptest.NewRequest(http.MethodGet, "/api/things?fields=title&q=one", nil))
	if ok.Code != http.StatusOK {
		t.Errorf("single fields and q = %d %s, want 200", ok.Code, ok.Body)
	}
}
