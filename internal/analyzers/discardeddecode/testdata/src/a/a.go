// Package a holds the discardeddecode fixture reduced from the real
// bug sites: kiln/chat/panel.go serveSend/serveApprove/serveReject,
// kiln/chat/server.go serveToolDispatch (journaling before the parse
// is validated), and examples/site/main.go servePaletteSearch (probes
// TestPanelSendRedRejectsMalformed and
// TestKilnDispatchRedRejectsMalformed, 2026-09 rounds, no fix
// applied), with the checked control server.go serveChatMessage next
// to them.
package a

import (
	"encoding/json"
	"encoding/xml"
	"net/http"
)

// chatArgs mirrors protocol.ChatArgs, reduced.
type chatArgs struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

// tools is the reduced tool surface the handlers drive.
type tools struct{}

// Chat runs the tool with whatever it was handed.
func (tools) Chat(ctx http.Request, args chatArgs) {}

// ApprovePlan is the human-approval control the ack contract lies
// about.
func (tools) ApprovePlan(planID string) {}

// RejectPlan is the reject twin.
func (tools) RejectPlan(planID, reason string) {}

func ack(w http.ResponseWriter) {
	w.Write([]byte(`{"ok":true}`))
}

// serveSend, pre-fix (panel.go serveSend): the decode error is
// blanked and the tool call runs on the zero value; the ack reports
// an operation that never happened.
func serveSend(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Text string `json:"text"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	_ = json.NewDecoder(r.Body).Decode(&body) // want `Decode error discarded: the parse failure marches on as zero-value data; check the error and refuse the input`
	tools{}.Chat(*r, chatArgs{Role: "user", Text: body.Text})
	ack(w)
}

// serveApprove, pre-fix (panel.go serveApprove): ApprovePlan runs
// with PlanID "" on a malformed body.
func serveApprove(w http.ResponseWriter, r *http.Request) {
	var body struct {
		PlanID string `json:"plan_id"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	_ = json.NewDecoder(r.Body).Decode(&body) // want `Decode error discarded: the parse failure marches on as zero-value data; check the error and refuse the input`
	tools{}.ApprovePlan(body.PlanID)
	ack(w)
}

// serveReject, pre-fix (panel.go serveReject).
func serveReject(w http.ResponseWriter, r *http.Request) {
	var body struct {
		PlanID string `json:"plan_id"`
		Reason string `json:"reason"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	_ = json.NewDecoder(r.Body).Decode(&body) // want `Decode error discarded: the parse failure marches on as zero-value data; check the error and refuse the input`
	tools{}.RejectPlan(body.PlanID, body.Reason)
	ack(w)
}

// serveChatMessage is the checked control (server.go:394): the decode
// error is assigned and gated, the request refused.
func serveChatMessage(w http.ResponseWriter, r *http.Request) {
	var args chatArgs
	r.Body = http.MaxBytesReader(w, r.Body, 65536)
	if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	tools{}.Chat(*r, args)
}

// serveToolDispatch, pre-fix (server.go:424): the journal envelope is
// minted from a body whose Unmarshal error was discarded, before any
// parse validation runs.
func serveToolDispatch(body []byte) map[string]any {
	var args map[string]any
	if len(body) > 0 {
		_ = json.Unmarshal(body, &args) // want `json.Unmarshal error discarded: the parse failure marches on as zero-value data; check the error and refuse the input`
	}
	return args
}

// servePaletteSearch, pre-fix (examples/site/main.go:781): the form
// parse error is blanked and the empty query returns everything.
func servePaletteSearch(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm() // want `ParseForm error discarded: the parse failure marches on as zero-value data; check the error and refuse the input`
	q := r.FormValue("q")
	w.Write([]byte(q))
}

// bareStatement is the spelling that drops the result with no blank
// assignment at all.
func bareStatement(body []byte) chatArgs {
	var args chatArgs
	json.Unmarshal(body, &args) // want `json.Unmarshal error discarded: the parse failure marches on as zero-value data; check the error and refuse the input`
	return args
}

// xmlSpelling: the xml twin of the family.
func xmlSpelling(body []byte) {
	var v struct{ N int }
	_ = xml.Unmarshal(body, &v) // want `xml.Unmarshal error discarded: the parse failure marches on as zero-value data; check the error and refuse the input`
	_ = v
}

// multipartSpelling: the multipart twin.
func multipartSpelling(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseMultipartForm(32 << 20) // want `ParseMultipartForm error discarded: the parse failure marches on as zero-value data; check the error and refuse the input`
	_ = w
}

// checkedNil is the compared-against-nil control (framework/ui
// toast.go): the error is consulted, not dropped.
func checkedNil(existing []byte) bool {
	var single struct{ N int }
	if json.Unmarshal(existing, &single) == nil {
		return true
	}
	return false
}

// mapProbe is the optional-field probe (core/webbotauth jwks.go
// parseJWK): one possibly-absent map entry, zero vetted afterwards;
// the envelope around it was decoded with its error checked.
func mapProbe(m map[string]json.RawMessage) string {
	var kty, crv string
	_ = json.Unmarshal(m["kty"], &kty)
	_ = json.Unmarshal(m["crv"], &crv)
	if kty != "OKP" {
		return ""
	}
	return crv
}
