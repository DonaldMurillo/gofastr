package a

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"log/slog"
	"net/http"
	"slices"
)

type Session struct{ User string }

type Store interface {
	Get(ctx context.Context, token string) (*Session, error)
}

const maxCandidates = 8

// preFixCandidates is sessionCookieCandidates before a81e9c2d: every
// same-name cookie, no cap.
func preFixCandidates(r *http.Request, name string) []string {
	var out []string
	for _, c := range r.Cookies() {
		if c.Name == name && c.Value != "" {
			out = append(out, c.Value)
		}
	}
	return out
}

// preFixMiddleware judges each candidate with a store query.
func preFixMiddleware(store Store, r *http.Request) *Session {
	for _, tok := range preFixCandidates(r, "session") { // want `fanoutreq: store.Get runs once per element of preFixCandidates\(r, "session"\)`
		if s, err := store.Get(r.Context(), tok); err == nil {
			return s
		}
	}
	return nil
}

// fixedCandidates is a81e9c2d's fix spelling.
func fixedCandidates(r *http.Request, name string) []string {
	var out []string
	for _, c := range r.CookiesNamed(name) {
		if c.Value == "" || slices.Contains(out, c.Value) {
			continue
		}
		out = append(out, c.Value)
		if len(out) == maxCandidates {
			break
		}
	}
	return out
}

func fixedMiddleware(store Store, r *http.Request) *Session {
	for _, tok := range fixedCandidates(r, "session") {
		if s, err := store.Get(r.Context(), tok); err == nil {
			return s
		}
	}
	return nil
}

// direct ranges the cookies itself.
func direct(store Store, r *http.Request) {
	for _, c := range r.CookiesNamed("session") { // want `fanoutreq: store.Get runs once per element of r.CookiesNamed\("session"\)`
		_, _ = store.Get(r.Context(), c.Value)
	}
}

// Near miss: a break on success is no cap; the attacker sends misses.
func breakOnSuccess(store Store, r *http.Request) {
	for _, c := range r.Cookies() { // want `fanoutreq: store.Get runs once per element of r.Cookies\(\)`
		if s, err := store.Get(r.Context(), c.Value); err == nil && s != nil {
			break
		}
	}
}

// Near miss: an integer-counter break inside a nested switch breaks the
// switch, not the loop.
func breakInSwitch(store Store, r *http.Request) {
	n := 0
	for _, c := range r.Cookies() { // want `fanoutreq: store.Get runs once per element of r.Cookies\(\)`
		switch {
		case c.Name == "x":
			n++
			if n > maxCandidates {
				break
			}
		}
		_, _ = store.Get(r.Context(), c.Value)
	}
}

// counterReturn caps with a return.
func counterReturn(store Store, r *http.Request) {
	for i, c := range r.Cookies() {
		if i >= maxCandidates {
			return
		}
		_, _ = store.Get(r.Context(), c.Value)
	}
}

// resliced caps by slicing.
func resliced(store Store, r *http.Request) {
	vals := r.Header.Values("X-Token")
	for _, v := range vals[:min(len(vals), maxCandidates)] {
		_, _ = store.Get(r.Context(), v)
	}
}

// formValues fans out a database query per form value.
func formValues(db *sql.DB, r *http.Request) {
	for _, id := range r.Form["id"] { // want `fanoutreq: db.Exec runs once per element of r.Form\["id"\]`
		_, _ = db.Exec("DELETE FROM t WHERE id = ?", id)
	}
}

// queryValues fans out an HTTP request per query value.
func queryValues(c *http.Client, r *http.Request) {
	vals := r.URL.Query()["url"]
	for _, u := range vals { // want `fanoutreq: c.Get runs once per element of r.URL.Query\(\)\["url"\]`
		_, _ = c.Get(u)
	}
}

// constantTime compares bytes already read: no backend call.
func constantTime(r *http.Request, secret string) bool {
	for _, c := range r.Cookies() {
		if subtle.ConstantTimeCompare([]byte(c.Value), []byte(secret)) == 1 {
			return true
		}
	}
	return false
}

// logs only: slog's context-taking calls do no backend work.
func logsOnly(r *http.Request, log *slog.Logger) {
	for _, v := range r.Header.Values("X-Trace") {
		log.InfoContext(r.Context(), "trace", "v", v)
		_ = context.WithValue(r.Context(), struct{}{}, v)
	}
}

// Near miss: a locally built url.Values is not request-sized.
func localValues(store Store, ctx context.Context) {
	vals := map[string][]string{"id": {"a", "b"}}
	for _, v := range vals["id"] {
		_, _ = store.Get(ctx, v)
	}
}

// Near miss: a fixed list of header names, not the request's values.
func fixedNames(store Store, r *http.Request) {
	for _, name := range []string{"Authorization", "Cookie"} {
		_, _ = store.Get(r.Context(), r.Header.Get(name))
	}
}

// funcField judges through a func-typed field that takes a context.
type gate struct {
	check func(ctx context.Context, tok string) bool
}

func (g *gate) any(r *http.Request) bool {
	for _, c := range r.Cookies() { // want `fanoutreq: g.check runs once per element of r.Cookies\(\)`
		if g.check(r.Context(), c.Value) {
			return true
		}
	}
	return false
}
