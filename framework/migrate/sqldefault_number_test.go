package migrate

import (
	"encoding/json"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// A JSON declaration keeps a default past 2^53 as a json.Number, and a Go
// one may hold an int64; both fell to the quoted fallback. A json.Number
// that is not a number literal must stay quoted: it is caller-built text.
func TestSQLDefaultWritesNumbersBare(t *testing.T) {
	for _, tc := range []struct {
		name string
		def  any
		want string
	}{
		{"int64", int64(9007199254740993), "9007199254740993"},
		{"json.Number", json.Number("9007199254740993"), "9007199254740993"},
		{"json.Number exponent", json.Number("-1.5e300"), "-1.5e300"},
		{"json.Number not a number", json.Number("1); DROP TABLE t; --"), "'1); DROP TABLE t; --'"},
		{"json.Number hex", json.Number("0x10"), "'0x10'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := schema.Field{Name: "n", Type: schema.Int, Default: tc.def}
			if got := SQLDefault(f, DialectPostgres); got != tc.want {
				t.Errorf("SQLDefault = %s, want %s", got, tc.want)
			}
		})
	}
}
