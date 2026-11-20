package packcmd

import (
	"go/parser"
	"go/token"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appgen"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
)

func TestEntityLLMMDRoundTrip(t *testing.T) {
	for _, value := range []string{"", "false", "true"} {
		t.Run("value="+value, func(t *testing.T) {
			src := "app:\n  name: Demo\n  module: example.com/demo\n"
			if value != "" {
				src += "  entity_llm_md: " + value + "\n"
			}
			spec, err := decodeAppSpecString(src)
			if err != nil {
				t.Fatal(err)
			}
			main, err := parser.ParseFile(token.NewFileSet(), "main.go", appgen.RenderAppSpecMain(spec), 0)
			if err != nil {
				t.Fatal(err)
			}
			app, err := parser.ParseFile(token.NewFileSet(), "app.go", "package main", 0)
			if err != nil {
				t.Fatal(err)
			}
			read := appspec.ReadGeneratedApp(app, main)
			got, present := appToMap(read)["entity_llm_md"]
			if present != (value != "") || (present && got != (value == "true")) {
				t.Fatalf("packed entity_llm_md = %v (present=%v), input %q", got, present, value)
			}
		})
	}
}
