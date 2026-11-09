package framework

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDiscoveryAnswersEncodeFailure(t *testing.T) {
	for _, value := range []any{math.NaN(), json.RawMessage(`{broken`)} {
		app := NewApp(WithWebBotAuth(WebBotAuthConfig{Keys: []map[string]any{{"bad": value}}}))
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/.well-known/http-message-signatures-directory", nil)
		r.Header.Set("Accept", "application/json")
		app.handleWebBotAuthDirectory(w, r)
		if w.Code != http.StatusInternalServerError || w.Body.Len() == 0 {
			t.Fatalf("response = %d %q; want a nonempty 500 response", w.Code, w.Body.String())
		}
	}
}

func TestBrokerQueryRejectsUnencodableRows(t *testing.T) {
	for _, value := range []any{math.NaN(), map[string]any{"data": math.NaN()}} {
		if result, err := mapQueryResult(value); err == nil {
			t.Fatalf("mapQueryResult(%v) = %v, want an encoding error", value, result)
		}
	}
}
