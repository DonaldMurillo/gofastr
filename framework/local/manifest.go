package local

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
)

// The browser reads the declaration from window.__gofastr_local[<app>],
// which this script assigns. It is served on the host's extra-script
// rail (uihost.WithExtraScripts with the URL Store.Script returns, plus
// the handler its mount step registers), the rail computed reducers and
// migration functions already use, so it loads after runtime.js on
// every full shell render and never inline: the page stays CSP-clean.
// A declaration that came from the DOM instead would let markup
// planted in an island response redefine a collection's caps or
// migrations; this rail is same-origin script the app serves.

// ScriptPath is the path Script's mount step registers ScriptHandler
// at: /__gofastr/local/<app>.js. Exported for a host that mounts its
// own routes (a subrouter, an asset CDN, a test server).
func (s *Store) ScriptPath() string { return "/__gofastr/local/" + s.app + ".js" }

// scriptJS returns the manifest script. The first call freezes the
// declaration: a Define after it panics.
func (s *Store) scriptJS() []byte {
	s.freeze()
	s.mu.Lock()
	defer s.mu.Unlock()
	cols := map[string]manifestEntry{}
	for n, d := range s.colls {
		cols[n] = d.manifest()
	}
	// mirrorMax travels with the declaration: the browser enforces the
	// same aggregate budget on the cookies it holds, which are
	// larger than the records they carry (component encoding).
	body, err := json.Marshal(map[string]any{"collections": cols, "mirrorMax": MirrorStoreMaxBytes})
	if err != nil {
		panic("local: manifest does not encode: " + err.Error())
	}
	appKey, _ := json.Marshal(s.app)
	var b bytes.Buffer
	b.WriteString("// framework/local: the browser manifest for store ")
	b.WriteString(s.app)
	b.WriteString(". Generated from the Go declaration; do not edit.\n")
	b.WriteString("window.__gofastr_local = window.__gofastr_local || {};\n")
	b.WriteString("window.__gofastr_local[")
	b.Write(appKey)
	b.WriteString("] = ")
	b.Write(body)
	b.WriteString(";\n")
	return b.Bytes()
}

// scriptURL is ScriptPath plus ?v=<content hash>, the URL Script hands
// out so the script caches immutably and busts when the declaration
// changes.
func (s *Store) scriptURL() string {
	js := s.scriptJS()
	return s.ScriptPath() + "?v=" + scriptHash(js)
}

func scriptHash(js []byte) string {
	sum := sha256.Sum256(js)
	return hex.EncodeToString(sum[:8])
}

// ScriptRouter is the single method Script's mount step needs: a GET
// route. The framework's *router.Router satisfies it, and so does
// anything else that mounts an http.Handler on a path.
type ScriptRouter interface {
	Get(pattern string, handler http.Handler)
}

// Script serves the declaration: it returns the URL to hand
// uihost.WithExtraScripts and the step that mounts ScriptHandler at
// ScriptPath, so the two halves are one expression and can be taken in
// the order every uihost app builds in (the host first, the router
// from the app the host went into):
//
//	url, mount := Site.Script()
//	host := uihost.New(site, uihost.WithExtraScripts(url))
//	app := framework.New(..., host)
//	mount(app.Router())
//
// The route and the extra script are two halves of one thing, and doing
// only one of them fails SILENTLY: without the route the manifest 404s,
// window.__gofastr_local stays undefined, and localStore(app) answers
// null. The forgettable half is visible in the signature rather than
// absent from it, and an app that never calls mount hears it from the
// browser by name ("no manifest for app <id> - is
// /__gofastr/local/<id>.js served?"). The first call freezes the
// declaration.
func (s *Store) Script() (string, func(ScriptRouter)) {
	return s.scriptURL(), func(rt ScriptRouter) {
		rt.Get(s.ScriptPath(), s.ScriptHandler())
	}
}

// ScriptHandler serves the manifest as JavaScript with a strong ETag
// and immutable caching when the request's ?v= matches the content
// hash (the policy every /__gofastr script follows). Script's mount
// step registers it at ScriptPath; it is exported for a host that
// mounts its own routes.
func (s *Store) ScriptHandler() http.Handler {
	var once sync.Once
	var body []byte
	var hash string
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() {
			body = s.scriptJS()
			hash = scriptHash(body)
		})
		h := w.Header()
		h.Set("Content-Type", "application/javascript; charset=utf-8")
		h.Set("X-Content-Type-Options", "nosniff")
		etag := `"` + hash + `"`
		h.Set("ETag", etag)
		if r.URL.Query().Get("v") == hash {
			h.Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			h.Set("Cache-Control", "no-cache")
		}
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		h.Set("Content-Length", strconv.Itoa(len(body)))
		if r.Method == http.MethodHead {
			return
		}
		_, _ = w.Write(body)
	})
}
