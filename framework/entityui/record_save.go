package entityui

import (
	"bytes"
	"maps"
	"net/http"
	"net/url"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/framework/crud"
)

// WithScriptlessEdits returns a UI whose record edit form and masked-field
// Replace forms natively post to <write base>/<id>/_save, which
// SaveHandler serves, so a browser without the runtime can save a record.
// The runtime still PUTs to the CRUD route. The host mounts SaveHandler
// beside the entity's write routes.
func (u *UI) WithScriptlessEdits() *UI {
	c := *u
	c.scriptlessEdits = true
	return &c
}

// ScriptlessEdits reports whether WithScriptlessEdits turned the native
// save route on, so a host knows to mount SaveHandler.
func (u *UI) ScriptlessEdits() bool { return u.scriptlessEdits }

func (u *UI) saveAction(item, back string) string {
	if !u.scriptlessEdits {
		return item
	}
	return item + "/_save?" + url.Values{"back": {back}}.Encode()
}

// SaveHandler serves POST <write base>/<id>/_save, the native submit of a
// record edit form drawn by a UI WithScriptlessEdits built. It runs the
// entity's CRUD Update and answers a success with a 303 to the back query
// parameter; a refused write answers what the CRUD route answers, and a
// missing or unsafe back answers 400.
func (u *UI) SaveHandler(entityName string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodPost {
			writeBulkError(w, r, http.StatusMethodNotAllowed, i18n.StatusPhrase(http.StatusMethodNotAllowed))
			return
		}
		m, err := u.meta(entityName)
		if err != nil || !m.hasAPI || crud.PathRecordID(r, m.keys) == "" {
			writeBulkError(w, r, http.StatusNotFound, i18n.StatusPhrase(http.StatusNotFound))
			return
		}
		back, ok := scrubBackPath(r.URL.Query().Get("back"))
		if !ok {
			writeBulkError(w, r, http.StatusBadRequest, reasonMissingReturnPath)
			return
		}
		put := r.Clone(r.Context())
		put.Method = http.MethodPut
		put.URL.RawQuery = ""
		put.RequestURI = ""
		rec := &saveRecorder{header: http.Header{}}
		m.ch.Update()(rec, put)
		if rec.status >= 200 && rec.status < 300 {
			http.Redirect(w, r, back, http.StatusSeeOther)
			return
		}
		maps.Copy(w.Header(), rec.header)
		w.WriteHeader(rec.status)
		_, _ = w.Write(rec.body.Bytes())
	})
}

type saveRecorder struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (s *saveRecorder) Header() http.Header { return s.header }

func (s *saveRecorder) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
}

func (s *saveRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	return s.body.Write(b)
}
