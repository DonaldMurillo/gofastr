package appgen

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	coreyaml "github.com/DonaldMurillo/gofastr/core/yaml"
)

// covT_decode parses a YAML app spec body and runs the full decoder.
func covT_decode(t *testing.T, yaml string) (appspec.AppSpec, error) {
	t.Helper()
	node, err := coreyaml.Parse(yaml)
	if err != nil {
		t.Fatalf("parse: %v\n%s", err, yaml)
	}
	return appspec.DecodeAppSpec(node)
}
