package factory_test

import (
	"os"
	"strings"
	"testing"
)

//gofastr:test-reads /framework/crud/crud_api.go
func TestPublicDocsMatchFactoryAPI(t *testing.T) {
	for path, stale := range map[string]string{
		"factory.go":          "Use [Suite] as the entry point",
		"../crud/crud_api.go": "Returns the created row as a snake_cased map",
	} {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(source), stale) {
			t.Errorf("%s documents an absent API or incorrect result casing: %s", path, stale)
		}
	}
}
