package parseform

import (
	"net/http"
)

const maxBody = 1 << 20

// decodeForm is battery/auth's decodeAuthCredentials reduced: the
// rebind precedes the parse in the same function, and the PostFormValue
// reads after it are covered by the same wrap. Quiet.
func decodeForm(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form body", http.StatusBadRequest)
		return
	}
	_ = r.PostFormValue("email")
}

// sortableMove is examples/site's sortable move endpoint reduced
// (main.go:439-447): wrap, then parse, then the FormValue reads. Quiet.
func sortableMove(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	_, _, _, _ = r.FormValue("container"), r.FormValue("moved"), r.FormValue("order"), r.FormValue("version")
}

// limitBody is the named-helper spelling of the wrap.
func limitBody(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
}

// viaHelper caps through the named helper first, then parses. Quiet.
func viaHelper(w http.ResponseWriter, r *http.Request) {
	limitBody(w, r)
	_ = r.ParseForm()
}

// limitRequestBody is crud's spelling: a helper whose own body wraps,
// content-type dispatched, not used before the parse here.
func limitRequestBody(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
}

// readRequestBody is crud's readRequestBody reduced: it does not wrap
// anything itself, its documented pre-condition is that the caller did.
func readRequestBody(r *http.Request) error {
	return parseMultipartBody(r)
}

// parseMultipartBody is crud's parseMultipartBody reduced. Quiet ONLY
// through the caller credit: every same-package caller caps before the
// call, which is the function's own contract.
func parseMultipartBody(r *http.Request) error {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		return err
	}
	_ = r.MultipartForm
	return nil
}

// cappedCreate is crud.go's Create arm reduced: wrap, then hand the
// request to the parsing helpers.
func cappedCreate(w http.ResponseWriter, r *http.Request) {
	limitRequestBody(w, r)
	if err := readRequestBody(r); err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
	}
}

// The uncapped twin: bareCreate never caps, so the same helper chain
// loses its credit and the parse inside the helper is the finding.
func bareRead(r *http.Request) error {
	return parseBare(r)
}

func parseBare(r *http.Request) error {
	if err := r.ParseMultipartForm(32 << 20); err != nil { // want `parses the request form with no cap of its own`
		return err
	}
	return nil
}

func bareCreate(w http.ResponseWriter, r *http.Request) {
	if err := bareRead(r); err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
	}
}
