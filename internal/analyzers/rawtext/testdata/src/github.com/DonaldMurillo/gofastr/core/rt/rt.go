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
	"strconv"

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

// uitext arm, a default one assignment away from its sink.
func defaulted(title string, n int) {
	aria := title
	if aria == "" {
		aria = "Page steps"
	}
	_ = map[string]string{"aria-label": aria} // want `aria-label "Page steps" is English written`
	attrs := map[string]string{}
	attrs["aria-label"] = aria     // want `aria-label "Page steps" is English written`
	attrs["title"] = "Open drawer" // want `title "Open drawer" is English written`
	attrs["class"] = "Open drawer"
	label := "Next page"
	_ = render.Text(label) // want `render.Text\("Next page"\) writes English`
	// A variable that only ever holds a class or a caller's text: quiet.
	class := "fui-steps"
	_ = render.Text(class)
	_ = render.Text(title)
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

// numtext arm.
type seconds int64

func (s seconds) String() string { return "s" }

func counts(ctx context.Context, n int, size int64, ratio float64, d seconds, tr *i18n.Translator) {
	_ = reasonGone.Text(ctx, map[string]any{"count": strconv.Itoa(n)})            // want `strconv.Itoa turns a number into Go's digits`
	_ = reasonGone.Text(ctx, map[string]any{"size": strconv.FormatInt(size, 10)}) // want `strconv.FormatInt turns a number`
	_ = reasonGone.Text(ctx, map[string]any{"ratio": fmt.Sprintf("%.1f", ratio)}) // want `fmt.Sprintf turns a number`
	_ = reasonGone.Text(ctx, map[string]any{"n": "#" + strconv.Itoa(n)})          // want `strconv.Itoa turns a number`
	_ = i18n.FillContext(ctx, "{{n}} left", map[string]any{"n": fmt.Sprint(n)})   // want `fmt.Sprint turns a number`
	_ = tr.T(ctx, "rt.left", map[string]any{"n": strconv.Itoa(n)})                // want `strconv.Itoa turns a number`
	_ = render.Text(strconv.Itoa(n))                                              // want `strconv.Itoa writes a number into the page`
	_ = map[string]string{"aria-label": strconv.Itoa(n)}                          // want `strconv.Itoa writes a number into aria-label`
	_ = ui.ButtonConfig{Label: strconv.FormatFloat(ratio, 'f', 1, 64)}            // want `strconv.FormatFloat writes a number into Label`
	// The number itself, a Stringer, mixed text, and machine slots: quiet.
	_ = reasonGone.Text(ctx, map[string]any{"count": n})
	_ = reasonGone.Text(ctx, map[string]any{"d": fmt.Sprint(d)})
	_ = reasonGone.Text(ctx, map[string]any{"id": fmt.Sprintf("%s-%d", "rt", n)})
	_ = map[string]string{"data-count": strconv.Itoa(n)}
	_ = ui.ButtonConfig{Class: strconv.Itoa(n)}
	//gofastr:allow(rawtext) a port the reader copies into a config file
	_ = render.Text(strconv.Itoa(n))
	// A helper that builds the params map is a sink too.
	_ = say(ctx, "{{n}} left", "n", strconv.Itoa(n)) // want `strconv.Itoa turns a number`
	_ = say(ctx, "{{n}} left", "n", n)
	_ = join("a", strconv.Itoa(n))
	// The digits hoisted into a local first reach the sink the same way.
	digits := strconv.Itoa(n)
	_ = reasonGone.Text(ctx, map[string]any{"n": digits}) // want `strconv.Itoa turns a number`
	_ = render.Text("+" + digits)                         // want `strconv.Itoa writes a number into the page`
	_ = map[string]string{"data-count": digits}
}

// say forwards kv to a phrase sink.
func say(ctx context.Context, tpl string, kv ...any) string {
	params := map[string]any{}
	for i := 0; i+1 < len(kv); i += 2 {
		params[kv[i].(string)] = kv[i+1]
	}
	return i18n.FillContext(ctx, tpl, params)
}

// join takes a variadic but never reaches a phrase: quiet.
func join(parts ...string) string { return parts[0] }

// textparam: empty writes its message into a label attribute and the
// page, the chartEmpty shape, so its callers' literals are page text.
func empty(class, message string) render.HTML {
	attrs := map[string]string{"class": class}
	attrs["aria-label"] = message
	_ = attrs
	return render.Text(message)
}

// emptyChart hands its label on to empty: a sink one call further.
func emptyChart(label string) render.HTML { return empty("c", label) }

func useEmpty(n int) {
	_ = empty("fui-empty", "No data yet") // want `empty's message parameter writes "No data yet"`
	_ = emptyChart("Nothing here")        // want `emptyChart's label parameter writes "Nothing here"`
	_ = empty("Wide Class", "")
	_ = empty("x", strconv.Itoa(n)) // want `strconv.Itoa writes a number into empty`
}
