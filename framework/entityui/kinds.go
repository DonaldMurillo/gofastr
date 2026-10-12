package entityui

import "github.com/DonaldMurillo/gofastr/core/schema"

// builtinKinds are the field kinds entityui draws itself, each with the
// field types it fits; an app kind of the same name replaces the
// built-in one.
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
