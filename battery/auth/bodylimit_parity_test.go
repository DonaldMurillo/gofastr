package auth

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/internal/bodylimit"
)

// auth keeps its own body cap and refusal answers so it stays
// unmountable (a copy may not import gofastr internal/ packages); this
// pins them to internal/bodylimit, which the other surfaces call.
func TestBodyRefusalsMatchBodylimit(t *testing.T) {
	if maxAuthBodyBytes != bodylimit.MaxJSONBodyBytes {
		t.Errorf("maxAuthBodyBytes = %d, bodylimit says %d", maxAuthBodyBytes, bodylimit.MaxJSONBodyBytes)
	}
	big := strings.Repeat("a", int(maxAuthBodyBytes)+1)
	overCap := &http.MaxBytesError{Limit: maxAuthBodyBytes}
	other := errors.New("other")
	for _, tc := range []struct {
		name string
		body io.Reader
		form bool
		want func() (int, i18n.Phrase)
	}{
		{"json over cap", strings.NewReader(`"` + big + `"`), false, func() (int, i18n.Phrase) { return bodylimit.ReadRefusal(overCap) }},
		{"json unreadable", iotest.ErrReader(other), false, func() (int, i18n.Phrase) { return bodylimit.ReadRefusal(other) }},
		{"json malformed", strings.NewReader(`{"email":`), false, func() (int, i18n.Phrase) { return bodylimit.DecodeRefusal(other) }},
		{"form over cap", strings.NewReader("a=" + big), true, func() (int, i18n.Phrase) { return bodylimit.FormRefusal(overCap) }},
		{"form malformed", strings.NewReader("a=%zz"), true, func() (int, i18n.Phrase) { return bodylimit.FormRefusal(other) }},
	} {
		req := httptest.NewRequest(http.MethodPost, "/login", tc.body)
		rec := httptest.NewRecorder()
		if tc.form {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Body = http.MaxBytesReader(rec, req.Body, maxAuthBodyBytes)
			parseAuthFormOrRefuse(rec, req)
		} else {
			req.Header.Set("Content-Type", "application/json")
			var dst struct {
				Email string `json:"email"`
			}
			decodeJSONLimited(rec, req, &dst)
		}
		want := httptest.NewRecorder()
		status, phrase := tc.want()
		writeAuthError(want, req, status, phrase)
		if rec.Code != want.Code || rec.Body.String() != want.Body.String() {
			t.Errorf("%s: got %d %s, bodylimit says %d %s", tc.name, rec.Code, rec.Body, want.Code, want.Body)
		}
	}
}
