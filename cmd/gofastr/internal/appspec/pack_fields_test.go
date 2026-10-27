package appspec

import (
	"go/ast"
	"go/parser"
	"strings"
	"testing"
)

func packFieldsOf(t *testing.T, field string) error {
	t.Helper()
	src := `app.Entity("posts", framework.EntityConfig{Fields: []schema.Field{` + field + `}})`
	call, err := parser.ParseExpr(src)
	if err != nil {
		t.Fatal(err)
	}
	_, err = PackEntityDeclFromCall(call.(*ast.CallExpr))
	return err
}

// A schema.Field key the app spec cannot hold fails the pack, naming the
// key: WireName was dropped, so the packed spec's client read "bodyText"
// while CRUD served "content", and every read came back empty.
func TestPackRefusesUnheldFieldKey(t *testing.T) {
	for key, field := range map[string]string{
		"WireName":     `{Name: "body_text", Type: schema.Text, WireName: "content"}`,
		"RawType":      `{Name: "price", Type: schema.Decimal, RawType: "NUMERIC(10,2)"}`,
		"Type":         `{Name: "price", Type: schema.Money}`,
		"Max":          `{Name: "n", Type: schema.Int, Max: limit}`,
		"AutoGenerate": `{Name: "n", Type: schema.Int, AutoGenerate: mode}`,
	} {
		err := packFieldsOf(t, field)
		if err == nil || !strings.Contains(err.Error(), key) {
			t.Errorf("pack of %s = %v, want an error naming %s", field, err, key)
		}
	}
	for _, field := range []string{
		`{Name: "body_text", Type: schema.Text, WireName: ""}`,
		`{Name: "n", Type: schema.Int, Required: true, Max: new(10.0), AutoGenerate: schema.AutoIncrement}`,
	} {
		if err := packFieldsOf(t, field); err != nil {
			t.Errorf("pack of %s = %v, want it read", field, err)
		}
	}
}
