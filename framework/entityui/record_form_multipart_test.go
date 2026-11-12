package entityui

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

// A record form carried no enctype, so a browser without the runtime
// submitted it urlencoded and the CRUD route refused it with 415. The
// form now asks for multipart/form-data, the body the route reads
// natively behind its cross-site guard; the runtime sends FormData for
// it, as it already did for a form with a file input.
func TestRecordFormIsMultipart(t *testing.T) {
	x := uploadsUI(t, false)
	for name, body := range map[string]string{
		"edit":   string(x.ui.Record("products", "p1").RenderCtx(x.userCtx("/products/p1", "", "u1"))),
		"create": string(x.ui.Create("products").RenderCtx(x.userCtx("/products/create", "", "u1"))),
	} {
		tag := openTagWithID(t, body, "eui-products-form")
		if !strings.Contains(tag, `enctype="multipart/form-data"`) {
			t.Errorf("%s: the record form posts no multipart body: %s", name, tag)
		}
	}

	// What a scriptless browser then sends lands on the create route;
	// the urlencoded body it used to send does not.
	ch, err := x.host.Crud(x.host.reg.ents["products"])
	if err != nil {
		t.Fatal(err)
	}
	post := func(ct string, body *bytes.Buffer) int {
		r := httptest.NewRequest(http.MethodPost, "/api/products", body)
		r.Header.Set("Content-Type", ct)
		r.Header.Set("Sec-Fetch-Site", "same-origin")
		r = r.WithContext(handler.SetUser(r.Context(), &testUser{id: "u1"}))
		w := httptest.NewRecorder()
		ch.Create()(w, r)
		return w.Code
	}
	var mp bytes.Buffer
	mw := multipart.NewWriter(&mp)
	_ = mw.WriteField("name", "Saw")
	_ = mw.Close()
	if code := post(mw.FormDataContentType(), &mp); code != http.StatusCreated {
		t.Errorf("a native multipart create = %d, want 201", code)
	}
	form := bytes.NewBufferString(url.Values{"name": {"Saw"}}.Encode())
	if code := post("application/x-www-form-urlencoded", form); code != http.StatusUnsupportedMediaType {
		t.Errorf("a urlencoded create = %d, want 415", code)
	}
}

// openTagWithID is the start tag of the element whose id is id.
func openTagWithID(t *testing.T, body, id string) string {
	t.Helper()
	i := strings.Index(body, `id="`+id+`"`)
	if i < 0 {
		t.Fatalf("no element with id %q", id)
	}
	start := strings.LastIndex(body[:i], "<")
	end := strings.Index(body[i:], ">")
	return body[start : i+end+1]
}
