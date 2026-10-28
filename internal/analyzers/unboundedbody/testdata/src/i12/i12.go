// Package i12 pins bug bash 2026-10-27 I12: unboundedbody skipped a
// whole file once any function in it mentioned a cap (fileHasCap), so
// one io.LimitReader anywhere silenced an uncapped io.ReadAll(r.Body)
// in a separately mounted handler. The cap must be on the path of the
// request being read.
package i12

import (
	"encoding/json"
	"io"
	"net/http"
)

const maxBody = 1 << 20

// limitBody is the battery/semantic routes.go middleware.
func limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		next.ServeHTTP(w, r)
	})
}

// Handler mounts the factories. indexHandler and queryHandler sit under
// limitBody; statsHandler and rawHandler do not.
func Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("POST /index", limitBody(indexHandler()))
	mux.Handle("POST /query", limitBody(requireJSON(queryHandler())))
	mux.Handle("POST /stats", statsHandler())
	mux.Handle("POST /raw", limitBody(rawHandler()))
	mux.Handle("POST /raw2", rawHandler())
	mux.Handle("POST /method", limitBody(http.HandlerFunc(new(svc).serve)))
	return mux
}

func requireJSON(next http.Handler) http.Handler { return next }

// indexHandler is wrapped at its only mount: quiet.
func indexHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
	})
}

// queryHandler is wrapped through an intermediate middleware: quiet.
func queryHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var v map[string]any
		_ = json.NewDecoder(r.Body).Decode(&v)
	})
}

// statsHandler is mounted bare in the same file as the cap: the I12
// shape, which fileHasCap silenced.
func statsHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body) // want `reads an inbound request body with no size cap`
	})
}

// rawHandler has one wrapped mount and one bare one: the bare mount
// reads uncapped.
func rawHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body) // want `reads an inbound request body with no size cap`
	})
}

type svc struct{}

// serve is mounted as a method value under limitBody: quiet.
func (s *svc) serve(w http.ResponseWriter, r *http.Request) {
	_, _ = io.ReadAll(r.Body)
}

// Upload is exported and mounted nowhere in this package: its cap is
// not visible, and the file's other cap is not its cap.
func Upload(w http.ResponseWriter, r *http.Request) {
	_, _ = io.ReadAll(r.Body) // want `reads an inbound request body with no size cap`
}

// rebound caps its own request before the read: quiet.
func rebound(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	_, _ = io.ReadAll(r.Body)
}

// reboundAfter caps after reading: the read was uncapped.
func reboundAfter(w http.ResponseWriter, r *http.Request) {
	_, _ = io.ReadAll(r.Body) // want `reads an inbound request body with no size cap`
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
}

// limitedOther caps a reader built from SOMETHING ELSE: the body read
// beside it is uncapped.
func limitedOther(w http.ResponseWriter, r *http.Request, src io.Reader) {
	_, _ = io.ReadAll(io.LimitReader(src, maxBody))
	_, _ = io.ReadAll(r.Body) // want `reads an inbound request body with no size cap`
}

// capRequest is a cap-establishing helper (the crud limitRequestBody
// spelling).
func capRequest(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
}

// helperFirst calls the capping helper before the read: quiet.
func helperFirst(w http.ResponseWriter, r *http.Request) {
	capRequest(w, r)
	_, _ = io.ReadAll(r.Body)
}

// readJSON reads a request its callers pass in; every caller capped it
// first (the parse-helper credit). Quiet.
func readJSON(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}

func createThing(w http.ResponseWriter, r *http.Request) {
	capRequest(w, r)
	var v map[string]any
	_ = readJSON(r, &v)
}

// readJSONMounted's only caller is a handler wrapped at its mount:
// quiet through the caller's mount credit.
func readJSONMounted(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}

func (s *svc) update(w http.ResponseWriter, r *http.Request) {
	var v map[string]any
	_ = readJSONMounted(r, &v)
}

func mountUpdate(mux *http.ServeMux, s *svc) {
	mux.Handle("PUT /thing", limitBody(http.HandlerFunc(s.update)))
}

// readJSONLoose has one capped caller and one uncapped one.
func readJSONLoose(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v) // want `reads an inbound request body with no size cap`
}

func looseCapped(w http.ResponseWriter, r *http.Request) {
	capRequest(w, r)
	_ = readJSONLoose(r, nil)
}

func looseBare(w http.ResponseWriter, r *http.Request) {
	_ = readJSONLoose(r, nil)
}

// notAMiddleware has a LimitReader in its body that wraps no request:
// passing a handler through it caps nothing.
func notAMiddleware(next http.Handler, src io.Reader) http.Handler {
	_, _ = io.ReadAll(io.LimitReader(src, maxBody))
	return next
}

func fakeWrapped() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body) // want `reads an inbound request body with no size cap`
	})
}

func mountFake(mux *http.ServeMux) {
	mux.Handle("POST /fake", notAMiddleware(fakeWrapped(), nil))
}
