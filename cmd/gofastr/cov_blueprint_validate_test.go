package main

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

func TestIsGoIdentifier(t *testing.T) {
	good := []string{"_x", "Foo", "bar9", "A_b2"}
	for _, s := range good {
		if !isGoIdentifier(s) {
			t.Errorf("isGoIdentifier(%q) = false, want true", s)
		}
	}
	bad := []string{"", "9x", "a-b", "a b", "a.b"}
	for _, s := range bad {
		if isGoIdentifier(s) {
			t.Errorf("isGoIdentifier(%q) = true, want false", s)
		}
	}
}

func covT_entity(name string) framework.EntityDeclaration {
	return framework.EntityDeclaration{Name: name, Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}}}
}

func TestValidateBlueprintErrors(t *testing.T) {
	cases := []struct {
		name string
		bp   Blueprint
	}{
		{"bad theme token", Blueprint{App: BlueprintApp{Theme: map[string]string{"nope": "#x"}}}},
		{"empty entity name", Blueprint{Entities: []framework.EntityDeclaration{{Fields: []framework.FieldDeclaration{{Name: "f", Type: "string"}}}}}},
		{"duplicate entity", Blueprint{Entities: []framework.EntityDeclaration{covT_entity("a"), covT_entity("a")}}},
		{"relation missing target", Blueprint{Entities: []framework.EntityDeclaration{{Name: "a", Fields: []framework.FieldDeclaration{{Name: "f", Type: "string"}}, Relations: []framework.Relation{{Name: "r"}}}}}},
		{"relation unknown target", Blueprint{Entities: []framework.EntityDeclaration{{Name: "a", Fields: []framework.FieldDeclaration{{Name: "f", Type: "string"}}, Relations: []framework.Relation{{Name: "r", Entity: "ghost"}}}}}},
		{"screen empty name", Blueprint{Screens: []BlueprintScreen{{Route: "/"}}}},
		{"screen bad identifier", Blueprint{Screens: []BlueprintScreen{{Name: "9bad", Route: "/"}}}},
		{"screen missing route", Blueprint{Screens: []BlueprintScreen{{Name: "home"}}}},
		{"duplicate route", Blueprint{Screens: []BlueprintScreen{{Name: "a", Route: "/"}, {Name: "b", Route: "/"}}}},
		{"bad screen type", Blueprint{Screens: []BlueprintScreen{{Name: "a", Route: "/", Type: "bogus"}}}},
		{"endpoint missing name", Blueprint{Endpoints: []BlueprintEndpoint{{Handler: "H", Path: "/x", Method: "GET"}}}},
		{"duplicate endpoint", Blueprint{Endpoints: []BlueprintEndpoint{{Name: "e", Handler: "H1", Path: "/a", Method: "GET"}, {Name: "e", Handler: "H2", Path: "/b", Method: "GET"}}}},
		{"endpoint missing handler", Blueprint{Endpoints: []BlueprintEndpoint{{Name: "e", Path: "/x", Method: "GET"}}}},
		{"endpoint bad handler ident", Blueprint{Endpoints: []BlueprintEndpoint{{Name: "e", Handler: "9bad", Path: "/x", Method: "GET"}}}},
		{"endpoint missing path", Blueprint{Endpoints: []BlueprintEndpoint{{Name: "e", Handler: "H", Method: "GET"}}}},
		{"endpoint missing method", Blueprint{Endpoints: []BlueprintEndpoint{{Name: "e", Handler: "H", Path: "/x"}}}},
		{"endpoint bad method", Blueprint{Endpoints: []BlueprintEndpoint{{Name: "e", Handler: "H", Path: "/x", Method: "FLY"}}}},
		{"endpoint unknown entity", Blueprint{Endpoints: []BlueprintEndpoint{{Name: "e", Handler: "H", Path: "/x", Method: "GET", Entity: "ghost"}}}},
		{"endpoint mcp", Blueprint{Endpoints: []BlueprintEndpoint{{Name: "e", Handler: "H", Path: "/x", Method: "GET", MCP: true}}}},
		{"middleware empty name", Blueprint{Middleware: []BlueprintNamedStub{{}}}},
		{"middleware bad ident", Blueprint{Middleware: []BlueprintNamedStub{{Name: "9x"}}}},
		{"duplicate middleware", Blueprint{Middleware: []BlueprintNamedStub{{Name: "log"}, {Name: "log"}}}},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			if err := validateBlueprint(c.bp); err == nil {
				t.Fatalf("expected error for %q", c.name)
			}
		})
	}
}

// An auth-enabled blueprint with dev_mode: false and no jwt_secret
// would generate an app whose auth battery refuses to boot (Init fails
// closed on an empty production signing key). Catch it at validate /
// generate time instead, with the remedy in the error.
func TestValidateProdAuthNeedsJWTSecret(t *testing.T) {
	bp := Blueprint{App: BlueprintApp{Auth: BlueprintAuth{Enabled: true, DevMode: false}}}
	err := validateBlueprint(bp)
	if err == nil {
		t.Fatal("auth enabled + dev_mode: false + no jwt_secret must fail validation")
	}
	for _, want := range []string{"jwt_secret", "dev_mode"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q, got: %v", want, err)
		}
	}
}

func TestValidateProdAuthWithSecretOK(t *testing.T) {
	bp := Blueprint{App: BlueprintApp{Auth: BlueprintAuth{Enabled: true, DevMode: false, JWTSecret: "s3cr3t"}}}
	if err := validateBlueprint(bp); err != nil {
		t.Fatalf("jwt_secret set should validate: %v", err)
	}
}

func TestValidateDevAuthNoSecretOK(t *testing.T) {
	bp := Blueprint{App: BlueprintApp{Auth: BlueprintAuth{Enabled: true, DevMode: true}}}
	if err := validateBlueprint(bp); err != nil {
		t.Fatalf("dev_mode auth without jwt_secret should validate: %v", err)
	}
}

func TestValidateAuthDisabledIgnoresSecret(t *testing.T) {
	// Zero-value BlueprintAuth (auth omitted entirely) has DevMode=false —
	// the check must gate on Enabled or every auth-less blueprint breaks.
	if err := validateBlueprint(Blueprint{}); err != nil {
		t.Fatalf("blueprint without auth should validate: %v", err)
	}
}

func TestValidateBlueprintHappy(t *testing.T) {
	bp := Blueprint{
		Entities:   []framework.EntityDeclaration{covT_entity("posts")},
		Screens:    []BlueprintScreen{{Name: "home", Route: "/", Type: "page"}},
		Endpoints:  []BlueprintEndpoint{{Name: "health", Handler: "Health", Path: "/health", Method: "GET"}},
		Middleware: []BlueprintNamedStub{{Name: "logging"}},
	}
	if err := validateBlueprint(bp); err != nil {
		t.Fatalf("happy validate: %v", err)
	}
}

func TestValidateBlueprintBlockErrors(t *testing.T) {
	ents := map[string]framework.EntityDeclaration{"posts": covT_entity("posts")}
	cases := []struct {
		name  string
		block BlueprintBlock
	}{
		{"bad heading level", BlueprintBlock{Type: "heading", Level: 9}},
		{"link missing href", BlueprintBlock{Type: "link"}},
		{"entity_list missing entity", BlueprintBlock{Kind: "entity_list"}},
		{"entity_list unknown entity", BlueprintBlock{Kind: "entity_list", Entity: "ghost", Fields: []string{"title"}}},
		{"entity_list missing fields", BlueprintBlock{Kind: "entity_list", Entity: "posts"}},
		{"entity_list unknown field", BlueprintBlock{Kind: "entity_list", Entity: "posts", Fields: []string{"nope"}}},
		{"entity_list negative limit", BlueprintBlock{Kind: "entity_list", Entity: "posts", Fields: []string{"title"}, Limit: -1}},
		{"unsupported type", BlueprintBlock{Type: "frobnicate"}},
		{"bad child", BlueprintBlock{Type: "div", Children: []BlueprintBlock{{Type: "frobnicate"}}}},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			if err := validateBlueprintBlock("s", ents, c.block); err == nil {
				t.Fatalf("expected error for %q", c.name)
			}
		})
	}
	// Happy: link with href via Props, heading default level, entity_list valid.
	if err := validateBlueprintBlock("s", ents, BlueprintBlock{Type: "link", Props: map[string]any{"href": "/x"}}); err != nil {
		t.Fatalf("link via props: %v", err)
	}
	if err := validateBlueprintBlock("s", ents, BlueprintBlock{Type: "heading"}); err != nil {
		t.Fatalf("heading default: %v", err)
	}
}

func TestValidateBlueprintActionsErrors(t *testing.T) {
	cases := []struct {
		name   string
		blocks []BlueprintBlock
	}{
		{"bad event", []BlueprintBlock{{Actions: []BlueprintAction{{Name: "a", Event: "hover", ClientJS: "x"}}}}},
		{"duplicate event", []BlueprintBlock{{Actions: []BlueprintAction{{Name: "a", Event: "click", ClientJS: "x"}, {Name: "b", Event: "click", ClientJS: "y"}}}}},
		{"missing client_js", []BlueprintBlock{{Actions: []BlueprintAction{{Name: "a", Event: "input"}}}}},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			if err := validateBlueprintActions("s", c.blocks); err == nil {
				t.Fatalf("expected error for %q", c.name)
			}
		})
	}
	// Happy: distinct events with client_js.
	ok := []BlueprintBlock{{Actions: []BlueprintAction{{Name: "save", Event: "click", ClientJS: "a"}, {Name: "live", Event: "input", ClientJS: "b"}}}}
	if err := validateBlueprintActions("s", ok); err != nil {
		t.Fatalf("happy actions: %v", err)
	}
}
