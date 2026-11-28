package appgen

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

func sdkDecl(name, table string) framework.EntityDeclaration {
	return framework.EntityDeclaration{Name: name, Table: table, Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}}}
}

func TestSDKRefusesNamesTheClientOwns(t *testing.T) {
	for _, tc := range []struct {
		decls []framework.EntityDeclaration
		want  string
	}{
		{[]framework.EntityDeclaration{sdkDecl("tokens", "token")}, "reserved"},
		{[]framework.EntityDeclaration{sdkDecl("strings", "to_string")}, "reserved"},
		{[]framework.EntityDeclaration{sdkDecl("events", "events_a"), sdkDecl("Events", "events_b")}, "both derive the Go type"},
		{[]framework.EntityDeclaration{{Name: "orders", Fields: []framework.FieldDeclaration{{Name: "status", Type: "enum", Values: []string{"a", "b"}}},
			States: &framework.StatesConfig{Field: "status", Transitions: []framework.Transition{{Key: "go!", From: []string{"a"}, To: "b"}}}}}, "orders"},
	} {
		opts := SdkOptions{name: "app"}
		if _, err := buildSDKSpec(tc.decls, &opts); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want a refusal naming %q", tc.decls[0].Table, err, tc.want)
		}
	}
}

func TestSDKModulePathRefusesEmptyElements(t *testing.T) {
	for _, module := range []string{"local//app-sdk", "local/app-sdk/"} {
		if err := validateSDKModulePath(module); err == nil {
			t.Errorf("module %q accepted", module)
		}
	}
}
