package appgen

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

func TestCLISpecRefusals(t *testing.T) {
	badStates := framework.EntityDeclaration{Name: "orders", Fields: []framework.FieldDeclaration{{Name: "status", Type: "enum", Values: []string{"a", "b"}}},
		States: &framework.StatesConfig{Field: "status", Transitions: []framework.Transition{{Key: "go!", From: []string{"a"}, To: "b"}}}}
	for _, tc := range []struct {
		name   string
		decls  []framework.EntityDeclaration
		binary string
		want   string
	}{
		{"binary newline", []framework.EntityDeclaration{sdkDecl("posts", "posts")}, "my\napp", "control character"},
		{"binary carriage return", []framework.EntityDeclaration{sdkDecl("posts", "posts")}, "my\rapp", "control character"},
		{"binary DEL", []framework.EntityDeclaration{sdkDecl("posts", "posts")}, "my\x7fapp", "control character"},
		{"struct collision", []framework.EntityDeclaration{sdkDecl("events", "events_a"), sdkDecl("Events", "events_b")}, "myapp", "both derive the Go struct name"},
		{"states", []framework.EntityDeclaration{badStates}, "myapp", "orders"},
	} {
		opts := defaultCLIOptions()
		opts.Binary = tc.binary
		if _, err := buildCLISpec(tc.decls, opts, "example.com/app/entities/client"); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want a refusal naming %q", tc.name, err, tc.want)
		}
	}
}
