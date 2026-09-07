// Package ns holds the nostore fixtures: per-caller 2xx writes with
// and without Cache-Control credit on the path, in the shapes the repo
// actually spells them (auth helper resolution, a verifying wrapper
// with a shared JSON writer, an identity parameter, a branch-guarded
// no-store, the gate-threaded wrapper credit), plus the silent
// postures.
package ns

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
)

// ---- shared spellings --------------------------------------------------

type sessionKey struct{}

func CurrentUser(ctx context.Context) string { return "u1" }

func verifyToken(r *http.Request) error { return nil }

func writeCredHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, private")
}

// ---- fires -------------------------------------------------------------

// listFire is the auth oracle shape: resolve through a helper, encode
// per-caller data (the store read's result), no Cache-Control.
func listFire(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUser(w, r); !ok {
		return
	}
	toks := listTokens(r)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"tokens": toks}) // want `2xx response for a resolved principal`
}

func listTokens(r *http.Request) []string { return []string{"t1"} }

func requireUser(w http.ResponseWriter, r *http.Request) (string, bool) {
	u := CurrentUser(r.Context())
	if u == "" {
		authErrJSON(w)
		return "", false
	}
	return u, true
}

// wrap is the REST shape: the wrapper resolves (verifyToken), the
// writer is a shared helper one frame down.
func wrap(inner http.HandlerFunc, needToken bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if needToken {
			if err := verifyToken(r); err != nil {
				return
			}
		}
		inner(w, r)
	}
}

func mountWrap() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/sessions", wrap(sessionsHandler, true))
	return mux
}

func sessionsHandler(w http.ResponseWriter, r *http.Request) {
	writeBody(w, http.StatusOK, map[string]any{"n": 1})
}

func writeBody(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)             // want `2xx response for a resolved principal`
	json.NewEncoder(w).Encode(body) // want `2xx response for a resolved principal`
}

// chatFire is the harness chat-page shape: the identity arrives as a
// session-named parameter and lands in the body.
type sessionID string

func chatFire(w http.ResponseWriter, r *http.Request, sess sessionID, token string) {
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte("<p>" + string(sess) + " " + token + "</p>")) // want `2xx response for a resolved principal`
}

// pageFire is the uihost pagecache shape: the no-store exists but only
// one branch executes it.
func pageFire(w http.ResponseWriter, r *http.Request) {
	if verifyToken(r) != nil {
		w.Header().Set("Cache-Control", "no-store")
		return
	}
	w.Write([]byte("page")) // want `2xx response for a resolved principal`
}

// rowsFire is the crud shape: the closure resolves through requireScope,
// the write sits in a helper.
func rowsFire() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireScope(w, r) {
			return
		}
		serveRows(w)
	}
}

func requireScope(w http.ResponseWriter, r *http.Request) bool {
	_, ok := requireUser(w, r)
	return ok
}

func serveRows(w http.ResponseWriter) {
	w.Write([]byte("rows")) // want `2xx response for a resolved principal`
}

// ---- quiet: the credit postures ----------------------------------------

// meQuiet is meHandler's shape: the helper sets the headers at the
// call's top level.
func meQuiet(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUser(w, r); !ok {
		return
	}
	writeCredHeaders(w)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"me": 1})
}

// gateQuiet is the admin battery's shape: the wrapper stamps no-store
// before next(w, r), and the handler is mounted through a local guard
// literal that forwards into the gate.
func gate(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next(w, r)
	})
}

func mountGate() *http.ServeMux {
	guard := func(h http.HandlerFunc) http.Handler { return gate(h) }
	mux := http.NewServeMux()
	mux.Handle("/rows", guard(rowsHandler("t")))
	return mux
}

func rowsHandler(ent string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := requireUser(w, r); !ok {
			return
		}
		w.Write([]byte("rows " + ent))
	}
}

// registerQuiet is an anti-enumeration answer: uniform for every
// caller, no resolver on the path.
func registerQuiet(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte(`{"accepted":true}`))
}

// noopQuiet: 204 carries no body, nothing to store.
func noopQuiet(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUser(w, r); !ok {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func authErrJSON(w http.ResponseWriter) {
	http.Error(w, "unauthorized", http.StatusUnauthorized)
}

// ---- round-2 postures ---------------------------------------------------

// ackPathQuiet: the encoded composite carries only constants and a
// path scalar — an acknowledgement, quiet.
func ackPathQuiet(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUser(w, r); !ok {
		return
	}
	provider := r.PathValue("provider")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"unlinked": provider})
}

// ackDecodedQuiet: the scalar is a field of a struct decoded from the
// request body (the uihost action ack).
type actionBody struct {
	Action string `json:"action"`
}

func ackDecodedQuiet(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUser(w, r); !ok {
		return
	}
	var body actionBody
	decodeBounded(w, r, &body)
	actionName := body.Action
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status":  "ok",
		"action":  actionName,
		"message": "Server action processed",
	})
}

func decodeBounded(w http.ResponseWriter, r *http.Request, dst any) {}

// ackStoreFire: the composite embeds a store read's result — data, and
// the ack posture must not save it.
func ackStoreFire(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUser(w, r); !ok {
		return
	}
	tok := readSecret(r)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"token": tok}) // want `2xx response for a resolved principal`
}

func readSecret(r *http.Request) string { return "live-credential" }

// passthroughQuiet: the response's headers come from a stored/upstream
// map copied onto the writer — the origin decided caching.
func passthroughQuiet(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUser(w, r); !ok {
		return
	}
	replay := storedResponse()
	maps.Copy(w.Header(), replay.Header)
	w.WriteHeader(replay.Status)
	_, _ = w.Write(replay.Body)
}

// passthroughRangeQuiet: the loop spelling of the same copy.
func passthroughRangeQuiet(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUser(w, r); !ok {
		return
	}
	replay := storedResponse()
	for k, vs := range replay.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(replay.Status)
	_, _ = w.Write(replay.Body)
}

type stored struct {
	Header map[string][]string
	Status int
	Body   []byte
}

func storedResponse() stored {
	return stored{Header: map[string][]string{"X-Trace": {"t"}}, Status: 200, Body: []byte("hi")}
}

// bodyless202Quiet: an accepted status with no body after it — nothing
// for a cache to store.
func bodyless202Quiet(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUser(w, r); !ok {
		return
	}
	w.WriteHeader(http.StatusAccepted)
}
