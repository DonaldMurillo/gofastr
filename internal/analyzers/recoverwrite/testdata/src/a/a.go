package a

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"lib"
)

type Error struct{ Code int }

func (e *Error) Error() string { return "error" }

// A same-package respond helper.
func WriteError(w http.ResponseWriter, r *http.Request, err error) { // want WriteError:`writesResponse\[0\]`
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

type ResponseType interface{ WriteBody(w http.ResponseWriter) }

// ---- fires ------------------------------------------------------------

// 8708ddd5, pre-fix: core/handler HandlerAdapter. A panic in WriteBody,
// after the 200 went out, gets a JSON error spliced onto the body.
func HandlerAdapter(h func(*http.Request) (ResponseType, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic recovered in handler")
				WriteError(w, r, &Error{Code: 500}) // want `recoverwrite: a deferred recover writes a response with no check that the response had not started`
			}
		}()
		out, err := h(r)
		if err != nil {
			WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusOK)
		out.WriteBody(w)
	}
}

// c0d6e99e, pre-fix: core/middleware RecoveryFn, through an imported
// respond helper (the fact crosses the package boundary).
func RecoveryFn(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				if err == http.ErrAbortHandler {
					panic(err)
				}
				lib.HTTPError(w, r, http.StatusInternalServerError, "internal") // want `recoverwrite: a deferred recover writes a response`
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// The kiln catalog recover: http.Error directly.
func KilnRecover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				http.Error(w, "internal error", http.StatusInternalServerError) // want `recoverwrite: a deferred recover writes a response`
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// Raw writes and an encoder, in a deferred named function.
func recoverTo(w http.ResponseWriter) { // want recoverTo:`writesResponse\[0\]`
	if recover() != nil {
		w.WriteHeader(http.StatusInternalServerError)              // want `recoverwrite: a deferred recover writes a response`
		_ = json.NewEncoder(w).Encode(map[string]string{"e": "x"}) // want `recoverwrite: a deferred recover writes a response`
	}
}

func Named(w http.ResponseWriter, r *http.Request) { // want Named:`writesResponse\[0\]`
	defer recoverTo(w)
	panic("x")
}

// Near miss for the guard: `start` is a time, not response state; a
// matcher on the substring would take it for one.
func Timed(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		restarted := false
		defer func() {
			if v := recover(); v != nil {
				slog.Info("panic", "took", time.Since(start), "restarted", restarted)
				w.Write([]byte("oops")) // want `recoverwrite: a deferred recover writes a response`
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// ---- quiet ------------------------------------------------------------

// The fixed HandlerAdapter: once started, log and abort.
func FixedAdapter(h func(*http.Request) (ResponseType, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var started bool
		defer func() {
			if rec := recover(); rec != nil {
				if started {
					panic(http.ErrAbortHandler)
				}
				WriteError(w, r, &Error{Code: 500})
			}
		}()
		out, _ := h(r)
		started = true
		w.WriteHeader(http.StatusOK)
		out.WriteBody(w)
	}
}

type recoveryWriter struct {
	http.ResponseWriter
	started bool
}

func (w *recoveryWriter) Started() bool { return w.started }

// The fixed Recovery: the started read reaches the write through a
// local, and a method named for the state counts as well.
func FixedRecovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rw := &recoveryWriter{ResponseWriter: w}
		defer func() {
			if err := recover(); err != nil {
				cut := false
				cut = cut || rw.started
				if cut {
					panic(http.ErrAbortHandler)
				}
				lib.HTTPError(w, r, http.StatusInternalServerError, "internal")
			}
		}()
		defer func() {
			if recover() != nil && !rw.Started() {
				http.Error(w, "internal", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(rw, r)
	})
}

// A recover that only records and re-raises (metrics, tracing).
func Metrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				slog.Info("panicked", "ct", lib.ContentType(w))
				panic(v)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// A deferred write with no recover: not this rule (doublewrite's).
func NoRecover(w http.ResponseWriter) { // want NoRecover:`writesResponse\[0\]`
	defer w.WriteHeader(http.StatusOK)
}

// A write in a callback built inside the deferred body runs later and
// is not this recover's write.
func Callback(w http.ResponseWriter, later func(func())) {
	defer func() {
		if recover() != nil {
			later(func() { w.WriteHeader(http.StatusInternalServerError) })
		}
	}()
}

// A deferred literal that writes but never recovers: no panic answered.
func DeferredLiteralNoRecover(w http.ResponseWriter) {
	defer func() { w.WriteHeader(http.StatusOK) }()
}
