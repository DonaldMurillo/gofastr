package crud

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// CRUD's write gate took multipart/form-data as a JSON stand-in, and a
// browser sends that type cross-site with no preflight and the victim's
// cookies. A sibling subdomain could create records, run _batch (which
// reads the raw body as JSON whatever the label) and fire transitions
// (which read no body) as the signed-in user (2026-10-27 D50). A
// multipart record write now passes the cross-site guard, and _batch and
// transitions take JSON only.

func multipartCreate(t *testing.T, number string, headers map[string]string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("number", number)
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/invoices", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return req
}

func TestCrossSiteMultipartCreateRefused(t *testing.T) {
	ch, db := statesWorld(t)
	for name, h := range map[string]map[string]string{
		"fetch metadata": {"Sec-Fetch-Site": "cross-site"},
		"sibling origin": {"Sec-Fetch-Site": "same-site", "Origin": "https://evil.example.com"},
		"origin only":    {"Origin": "https://evil.example.com"},
	} {
		rr := httptest.NewRecorder()
		ch.Create()(rr, multipartCreate(t, "forged "+name, h))
		if rr.Code != http.StatusForbidden {
			t.Errorf("SECURITY: %s: cross-site multipart create = %d, want 403 (%s)", name, rr.Code, rr.Body)
		}
	}
	if n := countInvoices(t, db); n != 0 {
		t.Fatalf("SECURITY: cross-site forms stored %d invoices", n)
	}
	rr := httptest.NewRecorder()
	ch.Create()(rr, multipartCreate(t, "mine", map[string]string{"Sec-Fetch-Site": "same-origin"}))
	if rr.Code != http.StatusCreated {
		t.Fatalf("same-origin multipart create = %d (%s)", rr.Code, rr.Body)
	}
}

func TestBatchRefusesMultipartLabel(t *testing.T) {
	ch, db := statesWorld(t)
	body := `{"items":[{"number":"forged"}]}`
	req := httptest.NewRequest(http.MethodPost, "/invoices/_batch", strings.NewReader(body))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=x")
	rr := httptest.NewRecorder()
	ch.BatchCreate()(rr, req)
	if rr.Code != http.StatusUnsupportedMediaType {
		t.Errorf("SECURITY: multipart-labelled JSON batch = %d, want 415 (%s)", rr.Code, rr.Body)
	}
	if n := countInvoices(t, db); n != 0 {
		t.Fatalf("SECURITY: a multipart-labelled batch stored %d invoices", n)
	}
}

func TestTransitionRefusesMultipart(t *testing.T) {
	ch, db := statesWorld(t)
	seedStateInvoice(t, db, "i1", "draft", nil)
	move := func(ct string) int {
		req := httptest.NewRequest(http.MethodPost, "/invoices/i1/transitions/issue", nil)
		req.Header.Set("Content-Type", ct)
		req.SetPathValue("id", "i1")
		req.SetPathValue("key", "issue")
		rr := httptest.NewRecorder()
		ch.Transition()(rr, req)
		return rr.Code
	}
	if code := move("multipart/form-data; boundary=x"); code != http.StatusUnsupportedMediaType {
		t.Errorf("SECURITY: multipart transition = %d, want 415", code)
	}
	if status, _ := readStateInvoice(t, db, "i1"); status != "draft" {
		t.Fatalf("SECURITY: a multipart transition moved the invoice to %s", status)
	}
	if code := move("application/json"); code != http.StatusOK {
		t.Fatalf("JSON transition = %d, want 200", code)
	}
}
