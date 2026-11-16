package entityui

import (
	"bytes"
	"html"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

var actionRe = regexp.MustCompile(`action="([^"]*)"`)

// formAction is the native action of the form whose id is id.
func formAction(t *testing.T, body, id string) string {
	t.Helper()
	m := actionRe.FindStringSubmatch(openTagWithID(t, body, id))
	if m == nil {
		t.Fatalf("form %q has no action", id)
	}
	return html.UnescapeString(m[1])
}

// postNative submits fields the way a browser without the runtime does:
// a multipart POST to the form's own action, against the save route
// mounted the way the host mounts it.
func postNative(t *testing.T, x *testUI, action string, fields map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for k, v := range fields {
		_ = mw.WriteField(k, v)
	}
	_ = mw.Close()
	r := httptest.NewRequest(http.MethodPost, action, &body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r = withUserRequest(r, "u1")
	mux := http.NewServeMux()
	mux.Handle("POST /api/invoices/{id}/_save", x.ui.SaveHandler("invoices"))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

// TestScriptlessEditSavesAndRedirects: with WithScriptlessEdits the edit
// form and a masked field's Replace form natively post to the _save
// route, which saves the record and answers 303 back to it; the runtime
// still PUTs to the CRUD route.
func TestScriptlessEditSavesAndRedirects(t *testing.T) {
	x := newInvoiceUI(t)
	maskToken(t, x)
	x.ui = x.ui.WithScriptlessEdits()
	body := renderRecord(t, x, "inv-1", nil)

	action := formAction(t, body, "eui-invoices-form")
	if action != "/api/invoices/inv-1/_save?back=%2Frec%2Finvoices%2Finv-1" {
		t.Fatalf("edit form action = %q, want the _save route", action)
	}
	if tag := openTagWithID(t, body, "eui-invoices-form"); !strings.Contains(tag, `data-cui-rpc="/api/invoices/inv-1"`) {
		t.Fatalf("the runtime no longer PUTs to the CRUD route: %s", tag)
	}
	fields := formFields(body, "eui-invoices-form")
	fields["number"] = "INV-1b"
	w := postNative(t, x, action, fields)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/rec/invoices/inv-1" {
		t.Fatalf("scriptless save = %d Location %q (%s), want 303 to the record", w.Code, w.Header().Get("Location"), w.Body)
	}
	if got := renderRecord(t, x, "inv-1", nil); !strings.Contains(got, "INV-1b") {
		t.Fatalf("the scriptless save did not store the edit")
	}

	const replace = "eui-invoices-replace-token"
	if got := formAction(t, body, replace); got != action {
		t.Fatalf("Replace form action = %q, want %q", got, action)
	}
	w = postNative(t, x, action, map[string]string{"token": "tok-new"})
	if w.Code != http.StatusSeeOther {
		t.Fatalf("scriptless replace = %d (%s), want 303", w.Code, w.Body)
	}
	if got := storedToken(t, x); got != "tok-new" {
		t.Fatalf("the scriptless Replace did not write the new value: %q", got)
	}
}

// TestScriptlessSaveRefusals: the _save route writes nothing for a
// cross-site post, an unsafe or missing return path, or a body that is
// not multipart, and passes a refused write's status through.
func TestScriptlessSaveRefusals(t *testing.T) {
	x := newInvoiceUI(t)
	x.ui = x.ui.WithScriptlessEdits()
	fields := map[string]string{"number": "INV-evil"}
	for name, tc := range map[string]struct {
		action string
		tune   func(*http.Request)
		want   int
	}{
		"cross-site":    {"/api/invoices/inv-1/_save?back=%2Frec", func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }, http.StatusForbidden},
		"absolute back": {"/api/invoices/inv-1/_save?back=https%3A%2F%2Fevil.example%2F", nil, http.StatusBadRequest},
		"no back":       {"/api/invoices/inv-1/_save", nil, http.StatusBadRequest},
		"urlencoded":    {"/api/invoices/inv-1/_save?back=%2Frec", func(r *http.Request) { r.Header.Set("Content-Type", "application/x-www-form-urlencoded") }, http.StatusUnsupportedMediaType},
		"unknown id":    {"/api/invoices/nope/_save?back=%2Frec", nil, http.StatusNotFound},
	} {
		t.Run(name, func(t *testing.T) {
			var body bytes.Buffer
			mw := multipart.NewWriter(&body)
			for k, v := range fields {
				_ = mw.WriteField(k, v)
			}
			_ = mw.Close()
			r := httptest.NewRequest(http.MethodPost, tc.action, &body)
			r.Header.Set("Content-Type", mw.FormDataContentType())
			r.Header.Set("Sec-Fetch-Site", "same-origin")
			if tc.tune != nil {
				tc.tune(r)
			}
			r = withUserRequest(r, "u1")
			mux := http.NewServeMux()
			mux.Handle("POST /api/invoices/{id}/_save", x.ui.SaveHandler("invoices"))
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("answered %d (%s), want %d", w.Code, w.Body, tc.want)
			}
		})
	}
	if strings.Contains(renderRecord(t, x, "inv-1", nil), "INV-evil") {
		t.Fatal("a refused scriptless save still wrote the record")
	}
}

// TestEditFormActionUnchangedWithoutOptIn: without WithScriptlessEdits
// the edit form's native action stays the CRUD item route.
func TestEditFormActionUnchangedWithoutOptIn(t *testing.T) {
	x := newInvoiceUI(t)
	maskToken(t, x)
	body := renderRecord(t, x, "inv-1", nil)
	for _, id := range []string{"eui-invoices-form", "eui-invoices-replace-token"} {
		if got := formAction(t, body, id); got != "/api/invoices/inv-1" {
			t.Errorf("%s action = %q, want the CRUD item route", id, got)
		}
	}
	if x.ui.ScriptlessEdits() {
		t.Error("a UI not built WithScriptlessEdits reports scriptless edits")
	}
}
