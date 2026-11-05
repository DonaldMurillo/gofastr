package crud

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHiddenStateRefusalsStayPrivate(t *testing.T) {
	for _, state := range []string{"open", "paid", "void"} {
		t.Run(state, func(t *testing.T) {
			ch, database := statesWorld(t, hideStatus)
			seedStateInvoice(t, database, "private", state, nil)
			for _, request := range []struct {
				name, method, path, body string
				status                   int
			}{
				{"move", http.MethodPost, "/invoices/private/transitions/issue", "{}", http.StatusConflict},
				{"stamp", http.MethodPatch, "/invoices/private", `{"paid_on":"2024-02-01"}`, http.StatusUnprocessableEntity},
				{"state", http.MethodPatch, "/invoices/private", `{"status":"draft"}`, http.StatusUnprocessableEntity},
			} {
				t.Run(request.name, func(t *testing.T) {
					recorder := httptest.NewRecorder()
					statesRouter(ch).ServeHTTP(recorder, makeRequest(t, RequestOpts{
						Method: request.method, Path: request.path, Body: request.body,
					}))
					if recorder.Code != request.status {
						t.Fatalf("refusal = %d: %s", recorder.Code, recorder.Body.String())
					}
					var body struct {
						Current string   `json:"current"`
						Moves   []string `json:"moves"`
					}
					if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
						t.Fatal(err)
					}
					if body.Current != "" || len(body.Moves) != 0 || strings.Contains(recorder.Body.String(), `\"`+state+`\"`) {
						t.Fatalf("refusal disclosed hidden state: %s", recorder.Body.String())
					}
					if stored, _ := readStateInvoice(t, database, "private"); stored != state {
						t.Fatalf("refusal wrote state %q, want %q", stored, state)
					}
				})
			}
		})
	}
}
