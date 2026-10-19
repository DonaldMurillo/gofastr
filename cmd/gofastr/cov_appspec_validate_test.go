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

func TestValidateAppSpecErrors(t *testing.T) {
	cases := []struct {
		name string
		bp   AppSpec
	}{
		{"bad theme token", AppSpec{App: AppSpecApp{Theme: map[string]string{"nope": "#x"}}}},
		{"empty entity name", AppSpec{Entities: []framework.EntityDeclaration{{Fields: []framework.FieldDeclaration{{Name: "f", Type: "string"}}}}}},
		{"duplicate entity", AppSpec{Entities: []framework.EntityDeclaration{covT_entity("a"), covT_entity("a")}}},
		{"relation missing target", AppSpec{Entities: []framework.EntityDeclaration{{Name: "a", Fields: []framework.FieldDeclaration{{Name: "f", Type: "string"}}, Relations: []framework.Relation{{Name: "r"}}}}}},
		{"relation unknown target", AppSpec{Entities: []framework.EntityDeclaration{{Name: "a", Fields: []framework.FieldDeclaration{{Name: "f", Type: "string"}}, Relations: []framework.Relation{{Name: "r", Entity: "ghost"}}}}}},
		{"screen empty name", AppSpec{Screens: []AppSpecScreen{{Route: "/"}}}},
		{"screen bad identifier", AppSpec{Screens: []AppSpecScreen{{Name: "9bad", Route: "/"}}}},
		{"screen missing route", AppSpec{Screens: []AppSpecScreen{{Name: "home"}}}},
		{"duplicate route", AppSpec{Screens: []AppSpecScreen{{Name: "a", Route: "/"}, {Name: "b", Route: "/"}}}},
		{"bad screen type", AppSpec{Screens: []AppSpecScreen{{Name: "a", Route: "/", Type: "bogus"}}}},
		{"endpoint missing name", AppSpec{Endpoints: []AppSpecEndpoint{{Handler: "H", Path: "/x", Method: "GET"}}}},
		{"duplicate endpoint", AppSpec{Endpoints: []AppSpecEndpoint{{Name: "e", Handler: "H1", Path: "/a", Method: "GET"}, {Name: "e", Handler: "H2", Path: "/b", Method: "GET"}}}},
		{"endpoint missing handler", AppSpec{Endpoints: []AppSpecEndpoint{{Name: "e", Path: "/x", Method: "GET"}}}},
		{"endpoint bad handler ident", AppSpec{Endpoints: []AppSpecEndpoint{{Name: "e", Handler: "9bad", Path: "/x", Method: "GET"}}}},
		{"endpoint missing path", AppSpec{Endpoints: []AppSpecEndpoint{{Name: "e", Handler: "H", Method: "GET"}}}},
		{"endpoint missing method", AppSpec{Endpoints: []AppSpecEndpoint{{Name: "e", Handler: "H", Path: "/x"}}}},
		{"endpoint bad method", AppSpec{Endpoints: []AppSpecEndpoint{{Name: "e", Handler: "H", Path: "/x", Method: "FLY"}}}},
		{"endpoint unknown entity", AppSpec{Endpoints: []AppSpecEndpoint{{Name: "e", Handler: "H", Path: "/x", Method: "GET", Entity: "ghost"}}}},
		{"endpoint mcp", AppSpec{Endpoints: []AppSpecEndpoint{{Name: "e", Handler: "H", Path: "/x", Method: "GET", MCP: true}}}},
		{"middleware empty name", AppSpec{Middleware: []AppSpecNamedStub{{}}}},
		{"middleware bad ident", AppSpec{Middleware: []AppSpecNamedStub{{Name: "9x"}}}},
		{"duplicate middleware", AppSpec{Middleware: []AppSpecNamedStub{{Name: "log"}, {Name: "log"}}}},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			if err := validateAppSpec(c.bp); err == nil {
				t.Fatalf("expected error for %q", c.name)
			}
		})
	}
}

// An auth-enabled app spec with dev_mode: false and no jwt_secret
// would generate an app whose auth battery refuses to boot (Init fails
// closed on an empty production signing key). Catch it at validate /
// generate time instead, with the remedy in the error.
func TestValidateProdAuthNeedsJWTSecret(t *testing.T) {
	bp := AppSpec{App: AppSpecApp{Auth: AppSpecAuth{Enabled: true, DevMode: false}}}
	err := validateAppSpec(bp)
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
	bp := AppSpec{App: AppSpecApp{Auth: AppSpecAuth{Enabled: true, DevMode: false, JWTSecret: "s3cr3t"}}}
	if err := validateAppSpec(bp); err != nil {
		t.Fatalf("jwt_secret set should validate: %v", err)
	}
}

func TestValidateDevAuthNoSecretOK(t *testing.T) {
	bp := AppSpec{App: AppSpecApp{Auth: AppSpecAuth{Enabled: true, DevMode: true}}}
	if err := validateAppSpec(bp); err != nil {
		t.Fatalf("dev_mode auth without jwt_secret should validate: %v", err)
	}
}

func TestValidateAuthDisabledIgnoresSecret(t *testing.T) {
	// Zero-value AppSpecAuth (auth omitted entirely) has DevMode=false;
	// the check must gate on Enabled or every auth-less app spec breaks.
	if err := validateAppSpec(AppSpec{}); err != nil {
		t.Fatalf("app spec without auth should validate: %v", err)
	}
}

func TestValidateAppSpecHappy(t *testing.T) {
	bp := AppSpec{
		Entities:   []framework.EntityDeclaration{covT_entity("posts")},
		Screens:    []AppSpecScreen{{Name: "home", Route: "/", Type: "page"}},
		Endpoints:  []AppSpecEndpoint{{Name: "health", Handler: "Health", Path: "/health", Method: "GET"}},
		Middleware: []AppSpecNamedStub{{Name: "logging"}},
	}
	if err := validateAppSpec(bp); err != nil {
		t.Fatalf("happy validate: %v", err)
	}
}

func TestValidateAppSpecBlockErrors(t *testing.T) {
	ents := map[string]framework.EntityDeclaration{"posts": covT_entity("posts")}
	cases := []struct {
		name  string
		block AppSpecBlock
	}{
		{"bad heading level", AppSpecBlock{Type: "heading", Level: 9}},
		{"link missing href", AppSpecBlock{Type: "link"}},
		{"entity_list missing entity", AppSpecBlock{Kind: "entity_list"}},
		{"entity_list unknown entity", AppSpecBlock{Kind: "entity_list", Entity: "ghost", Fields: []string{"title"}}},
		{"entity_list missing fields", AppSpecBlock{Kind: "entity_list", Entity: "posts"}},
		{"entity_list unknown field", AppSpecBlock{Kind: "entity_list", Entity: "posts", Fields: []string{"nope"}}},
		{"entity_list negative limit", AppSpecBlock{Kind: "entity_list", Entity: "posts", Fields: []string{"title"}, Limit: -1}},
		{"unsupported type", AppSpecBlock{Type: "frobnicate"}},
		{"bad child", AppSpecBlock{Type: "div", Children: []AppSpecBlock{{Type: "frobnicate"}}}},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			if err := validateAppSpecBlock("s", ents, c.block); err == nil {
				t.Fatalf("expected error for %q", c.name)
			}
		})
	}
	// Happy: link with href via Props, heading default level, entity_list valid.
	if err := validateAppSpecBlock("s", ents, AppSpecBlock{Type: "link", Props: map[string]any{"href": "/x"}}); err != nil {
		t.Fatalf("link via props: %v", err)
	}
	if err := validateAppSpecBlock("s", ents, AppSpecBlock{Type: "heading"}); err != nil {
		t.Fatalf("heading default: %v", err)
	}
}

func TestValidateAppSpecActionsErrors(t *testing.T) {
	cases := []struct {
		name   string
		blocks []AppSpecBlock
	}{
		{"bad event", []AppSpecBlock{{Actions: []AppSpecAction{{Name: "a", Event: "hover", ClientJS: "x"}}}}},
		{"duplicate event", []AppSpecBlock{{Actions: []AppSpecAction{{Name: "a", Event: "click", ClientJS: "x"}, {Name: "b", Event: "click", ClientJS: "y"}}}}},
		{"missing client_js", []AppSpecBlock{{Actions: []AppSpecAction{{Name: "a", Event: "input"}}}}},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			if err := validateAppSpecActions("s", c.blocks); err == nil {
				t.Fatalf("expected error for %q", c.name)
			}
		})
	}
	// Happy: distinct events with client_js.
	ok := []AppSpecBlock{{Actions: []AppSpecAction{{Name: "save", Event: "click", ClientJS: "a"}, {Name: "live", Event: "input", ClientJS: "b"}}}}
	if err := validateAppSpecActions("s", ok); err != nil {
		t.Fatalf("happy actions: %v", err)
	}
}
