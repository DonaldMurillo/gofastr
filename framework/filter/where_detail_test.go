package filter

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/schema"
)

// A ?where= decode refusal carries handler.JSONErrorDetail's phrase as
// its {{detail}}, never err.Error(): a string detail does not translate,
// so a Spanish caller read "where: JSON no válido: field: expected a
// string, got a number" half in English.
func TestWhereDecodeDetailIsPhrased(t *testing.T) {
	fields := []schema.Field{{Name: "status", Type: schema.String}}
	cat := i18n.NewMapCatalog()
	cat.Set("es", "filter.where_invalid_node", i18n.Message{Text: "where: nodo no válido: {{detail}}"})
	cat.Set("es", "filter.where_invalid_json", i18n.Message{Text: "where: JSON no válido: {{detail}}"})
	cat.Set("es", "error.invalid_json", i18n.Message{Text: "JSON no válido: {{detail}}"})
	cat.Set("es", "json.decode.wrong_type", i18n.Message{Text: "{{path}}: se esperaba {{expected}}, se recibió {{got}}"})
	cat.Set("es", "json.decode.unexpected_end", i18n.Message{Text: "el cuerpo termina antes de que el valor JSON esté completo"})
	cat.Set("es", "json.type.string", i18n.Message{Text: "una cadena"})
	cat.Set("es", "json.type.number", i18n.Message{Text: "un número"})
	ctx := i18n.WithTranslator(i18n.WithContext(context.Background(), i18n.Locale{Tag: "es"}), i18n.NewTranslator(cat, "en"))

	for _, tc := range []struct{ raw, reason, want string }{
		{`{"field":5}`, "filter.where_invalid_node", "where: nodo no válido: JSON no válido: field: se esperaba una cadena, se recibió un número"},
		{`{"field":"status"`, "filter.where_invalid_json", "where: JSON no válido: el cuerpo termina antes de que el valor JSON esté completo"},
	} {
		_, err := ParseWhere(tc.raw, fields)
		var ie *i18n.Error
		if !errors.As(err, &ie) || ie.Reason.Key() != tc.reason {
			t.Fatalf("%s: %v, want %s", tc.raw, err, tc.reason)
		}
		if _, ok := ie.Params["detail"].(i18n.Phrased); !ok {
			t.Fatalf("%s: detail %T %q is not a phrase", tc.raw, ie.Params["detail"], ie.Params["detail"])
		}
		if got, _ := i18n.ErrorText(ctx, err, i18n.Phrase{}); got != tc.want {
			t.Fatalf("%s: es text %q, want %q", tc.raw, got, tc.want)
		}
		if strings.Contains(err.Error(), "rawPred") || strings.Contains(err.Error(), "Go ") {
			t.Fatalf("%s: %q names a Go type", tc.raw, err)
		}
	}
}
