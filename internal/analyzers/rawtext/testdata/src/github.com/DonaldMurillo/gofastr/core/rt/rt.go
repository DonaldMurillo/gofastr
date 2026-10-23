// Package rt holds the rawtext fixtures: one firing site per arm, then
// the fixed spelling of each, quiet.
package rt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

var reasonGone = i18n.Define("rt.gone", "gone")

var computedKey = "rt." + "late"

var reasonComputed = i18n.Define(computedKey, "late") // want `constant key and constant English`

// httperror arm.
func refuse(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "gone", http.StatusGone) // want `http.Error writes untranslated text`
}

func refuseFixed(w http.ResponseWriter, r *http.Request) {
	i18n.HTTPError(w, r, http.StatusGone, reasonGone)
}

// define arm: minted at call time, in a func and in a func literal.
func mint() i18n.Phrase {
	return i18n.Define("rt.minted", "minted") // want `i18n.Define inside a function`
}

var mintLater = func() i18n.Phrase {
	return i18n.Define("rt.later", "later") // want `i18n.Define inside a function`
}

// handlermsg arm.
func fail() error {
	return handler.Errorf(http.StatusConflict, "taken") // want `handler.Errorf sends text as written`
}

func failLit() error {
	return &handler.Error{Code: http.StatusConflict, Message: "taken"} // want `handler.Error.Message sends text as written`
}

func failFixed() error {
	return handler.Fail(http.StatusConflict, reasonGone)
}

// msgparam arm.
func writeErr(w http.ResponseWriter, status int, msg string) { // want `writeErr writes a response from a msg string`
	w.WriteHeader(status)
}

func writeErrFixed(w http.ResponseWriter, r *http.Request, status int, reason i18n.Phrase) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": reason.Text(r.Context()), "reason": reason.Code()})
}

// The same helper as a closure.
func closureHelper() {
	fail := func(w http.ResponseWriter, msg string) { // want `func literal writes a response from a msg string`
		w.WriteHeader(http.StatusBadRequest)
	}
	_ = fail
}

// rawbody arm.
func rawBodies(w http.ResponseWriter, detail string) {
	fmt.Fprintf(w, `{"title":"verification unavailable","detail":%q}`, detail) // want `a raw JSON body's title is English prose`
	io.WriteString(w, `{"error":"not allowed here"}`)                          // want `a raw JSON body's error is English prose`
	w.Write([]byte(`{"message": "try again later"}`))                          // want `a raw JSON body's message is English prose`
	fmt.Fprintf(w, `{"error":"rate_limited","detail":%q}`, detail)             // a code, not prose: quiet
	fmt.Fprintf(w, "<p>%s</p>", detail)                                        // not a JSON message: quiet
}

// A string param under another name is not a message: quiet.
func writeFile(w http.ResponseWriter, name string) {}

// A msg string with no writer is not a response helper: quiet.
func logIt(ctx context.Context, msg string) {}

//gofastr:allow(rawtext) an agent contract: msg is documented English detail beside its own code
func writeAgentError(w http.ResponseWriter, code, msg string) {}

// envelope arm.
func envelope(w http.ResponseWriter, err error) {
	_ = json.NewEncoder(w).Encode(map[string]any{"error": "not allowed here"})  // want `envelope's error is English prose`
	_ = json.NewEncoder(w).Encode(map[string]any{"message": "magic link sent"}) // want `envelope's message is English prose`
	_ = json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})         // want `envelope's error is an error's raw text`
	// A constant with no space is a code: quiet.
	_ = json.NewEncoder(w).Encode(map[string]any{"error": "E_CAPABILITY_DENIED"})
	// Other keys are not the message: quiet.
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "all good here"})
	_ = errors.New("x")
}

// envelope arm, struct bodies: a status-bearing body is an HTTP error
// document; a struct with no status is a protocol message: quiet.
func problemDoc(w http.ResponseWriter) {
	_ = json.NewEncoder(w).Encode(struct {
		Title  string `json:"title"`
		Status int    `json:"status"`
		Detail string `json:"detail"`
	}{
		Title:  "Not Found", // want `a body's title is English prose`
		Status: http.StatusNotFound,
		Detail: "no_route",
	})
	// The positional spelling writes the same body.
	_ = json.NewEncoder(w).Encode(struct {
		Title  string `json:"title"`
		Status int    `json:"status"`
	}{"Not Found", http.StatusNotFound}) // want `a body's title is English prose`
	_ = json.NewEncoder(w).Encode(struct {
		Message string `json:"message"`
	}{Message: "method not found"})
}

// uitext arm.
type localConfig struct{ Label string }

func page(ctx context.Context, method string) {
	_ = render.Text("Page not found")                       // want `render.Text\("Page not found"\) writes English`
	_ = render.Text("Method " + method + " is not allowed") // want `render.Text\("Method"\) writes English`
	_ = render.Text("Saved")                                // want `render.Text\("Saved"\) writes English`
	_ = map[string]string{"aria-label": "Close dialog"}     // want `aria-label "Close dialog" is English written`
	_ = ui.ButtonConfig{Label: "Save changes"}              // want `Label: "Save changes" is English written`
	_ = ui.ButtonConfig{SubmitLabel: "Continue"}            // want `SubmitLabel: "Continue" is English written`
	_ = ui.ButtonConfig{"Save changes", "", "fui-btn"}      // want `Label: "Save changes" is English written`
	// Tokens, classes, keys and symbols are not words: quiet.
	_ = render.Text("GET, POST")
	_ = render.Text("fui-card")
	_ = render.Text("⌘")
	_ = map[string]string{"class": "Close dialog"}
	_ = ui.ButtonConfig{Class: "Primary button"}
	// The fixed spelling: quiet.
	_ = render.Text(reasonGone.Text(ctx))
	_ = ui.ButtonConfig{Label: reasonGone.Text(ctx)}
	// Not a UI package's type, the defaults table, and demo content: quiet.
	_ = localConfig{Label: "Save changes"}
	_ = headless.Strings{Close: "Close"}
	_ = headless.Spec{Example: ui.ButtonConfig{Label: "Save changes"}}
}
