// Package parseform pins the per-handler form-parity posture, reduced
// from the four open probe sites: battery/admin entitySave
// (entity_bodycap_red_test.go), battery/setup handleSubmit
// (body_limit_red_test.go), examples/site servePaletteSearch and
// WizardDemoHandler (formcaps_red_test.go). Each pre-fix spelling is
// reported once per request object, whatever else the function reads
// off the parsed form afterwards.
package parseform

import (
	"net/http"
)

// save is entitySave reduced: the handler is a literal, ParseForm is
// the first body read, and the PostForm scans afterwards are the SAME
// surface, not new ones.
func save(ent string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil { // want `parses the request form with no cap of its own`
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		vals := map[string]string{}
		vals["title"] = r.PostForm.Get("title")
		vals["status"] = r.PostForm.Get("status")
		_ = vals
	}
}

// submit is handleSubmit reduced: a named method, the request spelled
// req, and FormValue reads after the parse (still one finding).
func (rn *runner) submit(w http.ResponseWriter, req *http.Request) {
	if err := req.ParseForm(); err != nil { // want `parses the request form with no cap of its own`
		http.Error(w, "bad form data", http.StatusBadRequest)
		return
	}
	_ = req.FormValue("step")
}

type runner struct{}

// paletteSearch is servePaletteSearch reduced: the ParseForm error is
// discarded outright and FormValue parses again on first use. The two
// calls are one surface: one finding.
func paletteSearch(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()    // want `parses the request form with no cap of its own`
	_ = r.FormValue("q") // same request object, already reported
	_, _ = w.Write(nil)
}

// wizard is WizardDemoHandler reduced: the parse sits under a method
// check, which gates the handler's own logic, not the client's method.
func wizard(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil { // want `parses the request form with no cap of its own`
			http.Error(w, "parse form: "+err.Error(), http.StatusBadRequest)
			return
		}
		_ = r.PostForm.Get("wizard_action")
	}
	_, _ = w.Write(nil)
}

// uploadFormFile pins FormFile: it parses on first use like FormValue.
func uploadFormFile(w http.ResponseWriter, r *http.Request) {
	_, _, err := r.FormFile("file") // want `parses the request form with no cap of its own`
	if err != nil {
		http.Error(w, "missing file", http.StatusBadRequest)
	}
}
