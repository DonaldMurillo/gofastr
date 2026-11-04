package a

import (
	"errors"
	"log"
	"net/http"
	"strconv"

	"lib"
)

// Error is exported with an exported Code: any importer builds one.
type Error struct {
	Code    int
	Message string
}

func (e *Error) Error() string { return e.Message }

// ---- fires ------------------------------------------------------------

// 8708ddd5, pre-fix: core/handler WriteError. An Error{} sends code 0
// (a panic in the error path), an Error{Code: 200} an error as success.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	var herr *Error
	if !errors.As(err, &herr) {
		herr = &Error{Code: http.StatusInternalServerError}
	}
	w.WriteHeader(herr.Code) // want `statusrange: a status that is neither constant nor range-checked reaches WriteHeader`
}

// http.Error's code argument.
func PlainError(w http.ResponseWriter, e *Error) {
	http.Error(w, e.Message, e.Code) // want `statusrange: a status that is neither constant nor range-checked reaches http.Error`
}

// A status sink: the call inside writeJSON is not reported; its callers
// are, through the sink, and across packages through the fact.
func writeJSON(w http.ResponseWriter, status int, body string) { // want writeJSON:`status\(safe=\[\] sinks=\[1\] checks=\[\]\)`
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

type Config struct{ StatusCode int }

func Sinks(w http.ResponseWriter, r *http.Request, cfg Config) {
	writeJSON(w, http.StatusOK, "{}")
	writeJSON(w, cfg.StatusCode, "{}")                 // want `statusrange: a status that is neither constant nor range-checked reaches a status sink \(writeJSON\)`
	lib.HTTPError(w, r, cfg.StatusCode, "x")           // want `reaches a status sink \(HTTPError\)`
	lib.HTTPError(w, r, http.StatusForbidden, "x")     // quiet: a constant
	lib.Clamped(w, r, cfg.StatusCode, "x")             // quiet: not a sink
	writeJSON(w, lib.StatusFor(errors.New("x")), "{}") // quiet: a safe result
	begin := func(code int) { w.WriteHeader(code) }    // the literal is a sink of begin
	begin(http.StatusNoContent)                        // quiet
	begin(cfg.StatusCode)                              // want `reaches a status sink \(begin\)`
}

// A status call in a closure that captures the helper's parameter: the
// parameter is the helper's sink (its callers are checked), and the
// call inside the closure is not reported.
func writeDefer(w http.ResponseWriter, code int) { // want writeDefer:`status\(safe=\[\] sinks=\[1\] checks=\[\]\)`
	defer func() { w.WriteHeader(code) }()
}

func writeDirect(w http.ResponseWriter, code int) { // want writeDirect:`status\(safe=\[\] sinks=\[1\] checks=\[\]\)`
	w.WriteHeader(code)
}

func ClosureSinks(w http.ResponseWriter, r *http.Request) {
	writeDefer(w, 200)              // quiet: a constant
	writeDefer(w, len(r.URL.Path))  // want `statusrange: a status that is neither constant nor range-checked reaches a status sink \(writeDefer\)`
	writeDirect(w, len(r.URL.Path)) // want `reaches a status sink \(writeDirect\)`
}

// A near miss for the range check: equality guards 0, not the class.
func NotZero(w http.ResponseWriter, e *Error) {
	if e.Code != 0 {
		w.WriteHeader(e.Code) // want `reaches WriteHeader`
	}
}

// A log-only lower bound is not a range check: code can still be 0
// (a panic) or 50.
func LogOnlyLower(w http.ResponseWriter, raw string) {
	code, _ := strconv.Atoi(raw)
	if code > 0 {
		log.Printf("code %d", code)
	}
	w.WriteHeader(code) // want `statusrange: a status that is neither constant nor range-checked reaches WriteHeader`
}

// A log-only bound that proves no upper limit: code 1000 still panics.
func LogOnlyUpper(w http.ResponseWriter, raw string) {
	code, _ := strconv.Atoi(raw)
	if code > 60000 {
		log.Print("absurd")
	}
	w.WriteHeader(code) // want `reaches WriteHeader`
}

// A status field: unexported, read unchecked at a status call, so its
// stores are checked instead of its reads.
type refusal struct {
	code   int
	reason string
}

func refuse(code int, reason string) *refusal { // want refuse:`status\(safe=\[\] sinks=\[0\] checks=\[\]\)`
	return &refusal{code: code, reason: reason}
}

func Refusals(w http.ResponseWriter, raw string) {
	n, _ := strconv.Atoi(raw)
	bad := &refusal{code: n, reason: "parsed"} // want `statusrange: a status that is neither constant nor range-checked is stored in code`
	zero := &refusal{reason: "forgot"}         // want `statusrange: this literal leaves code zero`
	ok := refuse(http.StatusConflict, "dup")
	worse := refuse(n, "parsed") // want `reaches a status sink \(refuse\)`
	for _, f := range []*refusal{bad, zero, ok, worse} {
		w.WriteHeader(f.code)
	}
}

// A positional literal sets the fields by index: code is element 0.
func Positional(w http.ResponseWriter, raw string) {
	n, _ := strconv.Atoi(raw)
	bad := &refusal{n, "parsed"}                 // want `statusrange: a status that is neither constant nor range-checked is stored in code`
	fine := &refusal{http.StatusConflict, "dup"} // quiet: a constant
	_ = fine
	_ = bad
}

// Package-level stores are checked like any other: a keyed unchecked
// store, a positional one, and a literal that leaves code out.
func parseCode(raw string) int {
	n, _ := strconv.Atoi(raw)
	return n
}

var startupRefusal = &refusal{code: parseCode("302"), reason: "boot"} // want `statusrange: a status that is neither constant nor range-checked is stored in code`

var positionalRefusal = refusal{parseCode("418"), "teapot"} // want `statusrange: a status that is neither constant nor range-checked is stored in code`

var constantRefusal = &refusal{code: http.StatusCreated, reason: "ok"} // quiet: a constant

// ---- quiet ------------------------------------------------------------

// The fixed WriteError: clamp before the call.
func FixedWriteError(w http.ResponseWriter, herr *Error) {
	code := herr.Code
	if code < 400 || code > 599 {
		code = http.StatusInternalServerError
	}
	w.WriteHeader(code)
}

// A range check on the field path itself.
func Checked(w http.ResponseWriter, e *Error) {
	if e.Code >= 400 && e.Code <= 599 {
		w.WriteHeader(e.Code)
	}
}

// A checker function (kiln's validStatus).
func validStatus(c int) bool { return c >= 100 && c <= 999 } // want validStatus:`status\(safe=\[\] sinks=\[\] checks=\[0\]\)`

func Validated(w http.ResponseWriter, e *Error) error {
	if !validStatus(e.Code) {
		return errors.New("bad status")
	}
	w.WriteHeader(e.Code)
	return nil
}

// A clamp helper's result.
func clamp(c int) int { // want clamp:`status\(safe=\[0\] sinks=\[\] checks=\[\]\)`
	if c < 400 || c > 599 {
		return http.StatusInternalServerError
	}
	return c
}

func Clamped(w http.ResponseWriter, e *Error) { w.WriteHeader(clamp(e.Code)) }

// A local assigned only constants.
func Locals(w http.ResponseWriter, found bool) {
	status := http.StatusOK
	if !found {
		status = http.StatusNotFound
	}
	w.WriteHeader(status)
}

// A safe result through a multi-value call.
func apply(cmd string) (string, int, error) { // want apply:`status\(safe=\[1\] sinks=\[\] checks=\[\]\)`
	if cmd == "" {
		return "", http.StatusBadRequest, errors.New("empty")
	}
	return "ok", http.StatusOK, nil
}

func Multi(w http.ResponseWriter, cmd string) {
	if _, status, err := apply(cmd); err != nil {
		http.Error(w, err.Error(), status)
	}
}

// A received status (a proxy).
func Proxy(w http.ResponseWriter, resp *http.Response) { w.WriteHeader(resp.StatusCode) }

// A wrapper: WriteHeader forwards its own parameter, and a buffering
// wrapper replays a recorded status its literal leaves zero.
type holdWriter struct {
	http.ResponseWriter
	code int
	held bool
}

func (h *holdWriter) WriteHeader(code int) {
	h.code = code
	h.held = true
	h.ResponseWriter.WriteHeader(code)
}

func (h *holdWriter) finish() {
	if h.held {
		h.ResponseWriter.WriteHeader(h.code)
	}
}

func Hold(w http.ResponseWriter, next http.Handler, r *http.Request) {
	hw := &holdWriter{ResponseWriter: w}
	next.ServeHTTP(hw, r)
	hw.finish()
}
