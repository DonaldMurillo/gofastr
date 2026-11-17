package entityui

import "github.com/DonaldMurillo/gofastr/core/schema"

var builtinKinds = map[string][]schema.FieldType{
	"email":    {schema.String},
	"url":      {schema.String},
	"color":    {schema.String},
	"markdown": {schema.String, schema.Text},
	"code":     {schema.String, schema.Text},
	"money":    {schema.Int, schema.Float, schema.Decimal},
}

func isBuiltinKind(name string) bool {
	_, ok := builtinKinds[name]
	return ok
}
