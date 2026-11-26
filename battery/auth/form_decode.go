package auth

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"unicode"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"golang.org/x/text/unicode/norm"
)

func bareContentType(r *http.Request) string {
	ct := r.Header.Get("Content-Type")
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = ct[:i]
	}
	return strings.TrimSpace(strings.ToLower(ct))
}

func isFormRequest(r *http.Request) bool {
	ct := bareContentType(r)
	return ct == "application/x-www-form-urlencoded" || ct == "multipart/form-data"
}

func decodeAuthCredentials(w http.ResponseWriter, r *http.Request, canonicalize func(string) (string, error)) (email, password string, form bool, ok bool) {
	if isFormRequest(r) {
		r.Body = http.MaxBytesReader(w, r.Body, maxAuthBodyBytes)
		if !parseAuthFormOrRefuse(w, r) {
			return "", "", true, false
		}
		for _, field := range []string{"email", "password"} {
			if len(r.PostForm[field]) > 1 {
				writeAuthError(w, r, http.StatusBadRequest, i18n.ReasonInvalidForm)
				return "", "", true, false
			}
		}
		email, ok = canonicalizeDecodedEmail(w, r, canonicalize, r.PostFormValue("email"))
		if !ok {
			return "", "", true, false
		}
		password = r.PostFormValue("password")
		return email, password, true, true
	}

	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decodeJSONLimited(w, r, &body) {
		return "", "", false, false
	}
	email, ok = canonicalizeDecodedEmail(w, r, canonicalize, body.Email)
	if !ok {
		return "", "", false, false
	}
	return email, body.Password, false, true
}

var errAuthFormFile = errors.New("auth: form carries a file part")

func parseAuthForm(r *http.Request) error {
	if bareContentType(r) != "multipart/form-data" {
		return r.ParseForm()
	}
	if err := r.ParseMultipartForm(maxAuthBodyBytes); err != nil {
		return err
	}
	if r.MultipartForm != nil && len(r.MultipartForm.File) > 0 {
		_ = r.MultipartForm.RemoveAll()
		return errAuthFormFile
	}
	return nil
}

func parseAuthFormOrRefuse(w http.ResponseWriter, r *http.Request) bool {
	err := parseAuthForm(r)
	if err == nil {
		return true
	}
	if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
		writeAuthError(w, r, http.StatusRequestEntityTooLarge, i18n.StatusPhrase(http.StatusRequestEntityTooLarge))
		return false
	}
	writeAuthError(w, r, http.StatusBadRequest, i18n.ReasonInvalidForm)
	return false
}

func decodeFormFields(w http.ResponseWriter, r *http.Request, fields ...string) (map[string]string, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxAuthBodyBytes)
	if !parseAuthFormOrRefuse(w, r) {
		return nil, false
	}
	out := make(map[string]string, len(fields))
	for _, field := range fields {
		if len(r.PostForm[field]) > 1 {
			writeAuthError(w, r, http.StatusBadRequest, i18n.ReasonInvalidForm)
			return nil, false
		}
		out[field] = r.PostFormValue(field)
	}
	return out, true
}

func canonicalizeDecodedEmail(w http.ResponseWriter, r *http.Request, canonicalize func(string) (string, error), raw string) (string, bool) {
	email, err := canonicalizeEmailWith(canonicalize, raw)
	if err != nil {
		writeAuthErrorFor(w, r, http.StatusBadRequest, err, reasonComposedEmail)
		return "", false
	}
	if !emailWithinLimit(w, r, email) {
		return "", false
	}
	return email, true
}

// ErrEmailNotComposed is the error a CanonicalizeEmail override returns to refuse an address; handlers answer it with 400.
var ErrEmailNotComposed = errors.New("auth: email address is not in composed Unicode form (NFC)")

// CanonicalEmail is the battery's single definition of email identity.
func CanonicalEmail(email string) (string, error) {
	return canonicalizeEmailWith(nil, email)
}

func canonicalizeEmailWith(canonicalize func(string) (string, error), email string) (string, error) {
	if !textsafe.Storable(email) {
		return "", i18n.NewError(reasonInvalidEmail)
	}
	if canonicalize != nil {
		canonical, err := canonicalize(email)
		if err != nil {
			return "", err
		}
		if !textsafe.Storable(canonical) {
			return "", i18n.NewError(reasonInvalidEmail)
		}
		if again, err := canonicalize(canonical); err != nil || again != canonical {
			slog.Error("auth: AuthConfig.CanonicalizeEmail is not idempotent; refusing the address")
			return "", i18n.NewError(reasonInvalidEmail)
		}
		return canonical, nil
	}
	canonical := norm.NFC.String(strings.ToLower(strings.TrimSpace(stripInvisibleEmailRunes(email))))
	if canonical != "" && !addressShaped(canonical) {
		return "", i18n.NewError(reasonInvalidEmail)
	}
	return canonical, nil
}

var canonicalizerProbes = []string{
	"Alice@Example.COM", "  bob@example.org ", "first.last+news@gmail.com", "F.I.R.S.T@GoogleMail.com",
	"zoe\u0308@example.com", "ow\u200bner@example.com", "user@xn--bcher-kva.example", "MiXeD.Case+Tag@Sub.Example.co.uk",
}

func checkCanonicalizerIdempotent(canonicalize func(string) (string, error)) error {
	if canonicalize == nil {
		return nil
	}
	for _, in := range canonicalizerProbes {
		once, err := canonicalize(in)
		if err != nil {
			continue
		}
		twice, err := canonicalize(once)
		if err != nil || twice != once {
			return fmt.Errorf("auth: AuthConfig.CanonicalizeEmail is not idempotent: %q canonicalizes to %q, and that to %q (err %v); a canonical address must map to itself, since erasure and password reset canonicalize the stored address again", in, once, twice, err)
		}
	}
	return nil
}

func addressShaped(email string) bool {
	local, domain, ok := strings.Cut(email, "@")
	return ok && local != "" && domain != "" &&
		!strings.Contains(domain, "@") &&
		!strings.ContainsFunc(email, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) })
}

func stripInvisibleEmailRunes(email string) string {
	if !strings.ContainsFunc(email, func(r rune) bool { return textsafe.IsInvisible(r) || textsafe.IsC1(r) }) {
		return email
	}
	return strings.Map(func(r rune) rune {
		if textsafe.IsInvisible(r) || textsafe.IsC1(r) {
			return -1
		}
		return r
	}, email)
}

const maxEmailLen = 254

func emailWithinLimit(w http.ResponseWriter, r *http.Request, email string) bool {
	if len(email) > maxEmailLen {
		writeAuthError(w, r, http.StatusBadRequest, reasonInvalidEmail)
		return false
	}
	return true
}

func successRedirect(w http.ResponseWriter, r *http.Request, fallback string) string {
	next := r.URL.Query().Get("next")
	if next == "" {
		r.Body = http.MaxBytesReader(w, r.Body, maxAuthBodyBytes)
		if err := parseAuthForm(r); err == nil {
			next = r.PostFormValue("next")
		}
	}
	return handler.SafeRelativePathOr(next, fallback)
}

var defaultLoginErrorPath atomic.Pointer[string]

// SetDefaultLoginErrorPath sets where a form auth error with no usable same-origin Referer redirects; empty answers JSON.
func SetDefaultLoginErrorPath(path string) {
	if path == "" {
		defaultLoginErrorPath.Store(nil)
		return
	}
	defaultLoginErrorPath.Store(&path)
}

func getDefaultLoginErrorPath() string {
	if p := defaultLoginErrorPath.Load(); p != nil {
		return *p
	}
	return ""
}

var defaultRegisterErrorPath atomic.Pointer[string]

// SetDefaultRegisterErrorPath is SetDefaultLoginErrorPath for the register form.
func SetDefaultRegisterErrorPath(path string) {
	if path == "" {
		defaultRegisterErrorPath.Store(nil)
		return
	}
	defaultRegisterErrorPath.Store(&path)
}

func getDefaultRegisterErrorPath() string {
	if p := defaultRegisterErrorPath.Load(); p != nil {
		return *p
	}
	return ""
}

type registerFormKey struct{}

func asRegisterForm(r *http.Request) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), registerFormKey{}, true))
}

func errorFallbackPath(r *http.Request) string {
	if on, _ := r.Context().Value(registerFormKey{}).(bool); on {
		if p := getDefaultRegisterErrorPath(); p != "" {
			return p
		}
	}
	return getDefaultLoginErrorPath()
}

func writeFormAuthError(w http.ResponseWriter, r *http.Request, status int, reason i18n.Phrase) {
	redirectFormAuthError(w, r, status, reason, "")
}

func writeLoginFormError(w http.ResponseWriter, r *http.Request, status int, reason i18n.Phrase) {
	next := cmp.Or(r.URL.Query().Get("next"), r.PostForm.Get("next"))
	redirectFormAuthError(w, r, status, reason, handler.SafeRelativePathOr(next, ""))
}

func redirectFormAuthError(w http.ResponseWriter, r *http.Request, status int, reason i18n.Phrase, next string) {
	target := safeReferer(r)
	if target == "" {
		fallback := errorFallbackPath(r)
		if fallback != "" && handler.IsSafeRelativePath(fallback) {
			target = fallback
		}
	}
	if target == "" {
		writeAuthError(w, r, status, reason)
		return
	}
	target = handler.WithQuery(target, func(q url.Values) {
		q.Set("error", sanitiseErr(reason.Code()))
		if next != "" {
			q.Set("next", next)
		}
	})
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func safeReferer(r *http.Request) string {
	ref := r.Referer()
	if ref == "" {
		return ""
	}
	u, err := url.Parse(ref)
	if err != nil {
		return ""
	}
	if u.Host == "" {
		if !handler.IsSafeRelativePath(u.EscapedPath()) {
			return ""
		}
		return u.EscapedPath()
	}
	if !strings.EqualFold(u.Host, r.Host) {
		return ""
	}
	if u.Scheme != "" && u.Scheme != "http" && u.Scheme != "https" {
		return ""
	}
	out := u.EscapedPath()
	if out == "" {
		out = "/"
	}
	if !handler.IsSafeRelativePath(out) {
		return ""
	}
	return out
}

func sanitiseErr(s string) string {
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == ' ' || c < 0x20 || c > 0x7e {
			b = append(b, '+')
			continue
		}
		switch c {
		case '&', '=', '#', '?', '%', '+',
			'<', '>', '"', '\'':
			b = append(b, '+')
			continue
		}
		b = append(b, c)
	}
	return string(b)
}

func writeCredentialHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, private")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
}
