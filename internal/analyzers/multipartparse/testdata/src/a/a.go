package a

import (
	"mime"
	"mime/multipart"
	"net/http"
	"strings"

	"lib"
)

const maxBody = 1 << 16

func bareContentType(r *http.Request) string {
	ct := r.Header.Get("Content-Type")
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = ct[:i]
	}
	return strings.TrimSpace(strings.ToLower(ct))
}

// isFormRequest is the battery/auth predicate 84e4d4b1 left in place.
func isFormRequest(r *http.Request) bool { // want isFormRequest:"acceptsMultipart"
	ct := bareContentType(r)
	return ct == "application/x-www-form-urlencoded" || ct == "multipart/form-data"
}

// preFix84e4d4b1 is decodeAuthCredentials before 84e4d4b1: the predicate
// accepts multipart, but only ParseForm runs.
func preFix84e4d4b1(w http.ResponseWriter, r *http.Request) (string, bool) {
	if isFormRequest(r) {
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		if err := r.ParseForm(); err != nil { // want `multipartparse: preFix84e4d4b1 accepts multipart/form-data but only calls ParseForm`
			return "", false
		}
		return r.PostForm.Get("email"), true
	}
	return "", false
}

// parseAuthForm is 84e4d4b1's fix spelling: parse the encoding accepted.
func parseAuthForm(w http.ResponseWriter, r *http.Request) error { // want parseAuthForm:"readsMultipart"
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	if bareContentType(r) != "multipart/form-data" {
		return r.ParseForm()
	}
	return r.ParseMultipartForm(maxBody)
}

// fixed84e4d4b1 accepts multipart and parses through the fixed helper.
func fixed84e4d4b1(w http.ResponseWriter, r *http.Request) (string, bool) { // want fixed84e4d4b1:"readsMultipart"
	if isFormRequest(r) {
		if err := parseAuthForm(w, r); err != nil {
			return "", false
		}
		return r.PostForm.Get("email"), true
	}
	return "", false
}

// Near miss: FormValue after ParseForm never parses multipart, so it is
// no credit.
func formValueAfterParseForm(r *http.Request) string {
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") ||
		r.Header.Get("Content-Type") == "application/x-www-form-urlencoded" {
		_ = r.ParseForm() // want `multipartparse: formValueAfterParseForm accepts`
		return r.FormValue("q")
	}
	return ""
}

// crossPackagePredicate accepts through another package's predicate.
func crossPackagePredicate(r *http.Request) string {
	if !lib.IsForm(r) {
		return ""
	}
	_ = r.ParseForm() // want `multipartparse: crossPackagePredicate accepts`
	return r.PostForm.Get("q")
}

// crossPackageReader parses through another package's reader.
func crossPackageReader(r *http.Request) string { // want crossPackageReader:"readsMultipart"
	if !lib.IsForm(r) {
		return ""
	}
	if err := lib.ParseAny(r); err != nil {
		return ""
	}
	_ = r.ParseForm() // re-parse is a no-op once the multipart form ran
	return r.PostForm.Get("q")
}

// switchDispatch reads multipart in its own case.
func switchDispatch(r *http.Request) string { // want switchDispatch:"readsMultipart"
	mt, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	switch mt {
	case "multipart/form-data":
		mr := multipart.NewReader(r.Body, params["boundary"])
		_ = mr
		return ""
	default:
		_ = r.ParseForm()
		return r.PostForm.Get("q")
	}
}

// switchAccepts lists multipart in the urlencoded case: the bug.
func switchAccepts(r *http.Request) string {
	switch bareContentType(r) {
	case "application/x-www-form-urlencoded", "multipart/form-data":
		_ = r.ParseForm() // want `multipartparse: switchAccepts accepts`
		return r.PostForm.Get("q")
	}
	return ""
}

// refusesMultipart turns multipart away, then parses urlencoded.
func refusesMultipart(w http.ResponseWriter, r *http.Request) string {
	if bareContentType(r) == "multipart/form-data" {
		http.Error(w, "unsupported", http.StatusUnsupportedMediaType)
		return ""
	}
	_ = r.ParseForm()
	return r.PostForm.Get("q")
}

// refusesInSwitch turns multipart away in its case.
func refusesInSwitch(w http.ResponseWriter, r *http.Request) string {
	switch bareContentType(r) {
	case "multipart/form-data":
		w.WriteHeader(http.StatusUnsupportedMediaType)
		return ""
	}
	_ = r.ParseForm()
	return r.PostForm.Get("q")
}

// setsEnctype assigns the constant, it does not compare it.
func setsEnctype(w http.ResponseWriter, r *http.Request) {
	attrs := map[string]string{"enctype": "multipart/form-data"}
	_ = attrs
	_ = r.ParseForm()
}

// urlencodedOnly never mentions multipart.
func urlencodedOnly(r *http.Request) string {
	if r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
		return ""
	}
	_ = r.ParseForm()
	return r.PostForm.Get("q")
}

// formFile reads the file part, which parses multipart.
func formFile(r *http.Request) { // want formFile:"readsMultipart"
	if !isFormRequest(r) {
		return
	}
	_ = r.ParseForm()
	f, _, err := r.FormFile("avatar")
	if err == nil {
		f.Close()
	}
}
