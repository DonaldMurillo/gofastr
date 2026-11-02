package crud

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// A _batch envelope refusal (checkEnvelopeKeys) is invalid_body whose
// {{detail}} is a phrase, so the whole message reaches the caller in
// their locale; it was a fixed English fmt.Errorf ("duplicate key %q in
// request body") under a translated frame.
func TestBatchEnvelopeRefusalTranslates(t *testing.T) {
	cat := i18n.NewMapCatalog()
	cat.Set("es", "error.invalid_body", i18n.Message{Text: "cuerpo no válido: {{detail}}"})
	cat.Set("es", "error.envelope.not_object", i18n.Message{Text: "el cuerpo debe ser un objeto JSON"})
	cat.Set("es", "error.envelope.duplicate_key", i18n.Message{Text: "clave {{key}} repetida"})
	cat.Set("es", "error.envelope.unknown_key", i18n.Message{Text: "clave {{key}} desconocida (se espera {{expected}})"})
	for _, tc := range []struct{ body, want string }{
		{`[1]`, "cuerpo no válido: el cuerpo debe ser un objeto JSON"},
		{`{"items":[{"title":"a"}],"items":[{"title":"b"}]}`, `cuerpo no válido: clave "items" repetida`},
		{`{"Items":[{"title":"a"}]}`, `cuerpo no válido: clave "Items" desconocida (se espera "items")`},
	} {
		ch, _ := batchEnvelopeHandler(t)
		req := batchEnvelopeRequest(t, http.MethodPost, tc.body)
		ctx := i18n.WithContext(req.Context(), i18n.Locale{Tag: "es"})
		req = req.WithContext(i18n.WithTranslator(ctx, i18n.NewTranslator(cat, "en")))
		rec := httptest.NewRecorder()
		ch.BatchCreate()(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want 400; body=%s", tc.body, rec.Code, rec.Body.String())
		}
		var body struct{ Error, Reason string }
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.Error != tc.want || body.Reason != "invalid_body" {
			t.Fatalf("%s: body = %+v, want %q and invalid_body", tc.body, body, tc.want)
		}
	}
}
