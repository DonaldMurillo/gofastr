package appgen

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"slices"
	"strings"
	"testing"
	"unicode"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/framework"
)

// resourceMembers reads the member names client.js's Resource class
// defines: its methods and the fields its constructor sets.
func resourceMembers(t *testing.T) []string {
	t.Helper()
	decl := framework.EntityDeclaration{Name: "posts", Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}}}
	spec := sdkSpec{
		App: "app", SDKVersion: "0.0.0", GofastrVersion: "dev", Module: "local/app-sdk",
		Decls:    []framework.EntityDeclaration{decl},
		Entities: []cliEntity{buildEntityModel(decl, cliVerbs)},
	}
	var js string
	for _, f := range renderSDKJSFiles(spec) {
		if f.Name == "client.js" {
			js = f.Content
		}
	}
	start := strings.Index(js, "\nclass Resource {")
	end := strings.Index(js[start+1:], "\n}\n")
	if start < 0 || end < 0 {
		t.Fatal("client.js has no Resource class")
	}
	body := js[start : start+1+end]
	seen := map[string]bool{}
	var names []string
	for _, re := range []*regexp.Regexp{
		regexp.MustCompile(`(?m)^  (?:async )?([A-Za-z_$][\w$]*)\(`),
		regexp.MustCompile(`this\.([A-Za-z_$][\w$]*) =`),
	} {
		for _, m := range re.FindAllStringSubmatch(body, -1) {
			if !seen[m[1]] {
				seen[m[1]] = true
				names = append(names, m[1])
			}
		}
	}
	if !seen["list"] || !seen["columns"] {
		t.Fatalf("member scan found %v; the Resource class shape changed", names)
	}
	return names
}

// Every Resource member a name can reach is refused: an endpoint binds
// camelCase(name), a move binds its raw key, and either one would
// overwrite the member on every resource. Underscore helpers are out of
// reach of both grammars.
func TestJSResourceMembersReserved(t *testing.T) {
	for _, m := range resourceMembers(t) {
		if strings.HasPrefix(m, "_") {
			continue
		}
		decl := endpointDecls(framework.Endpoint{Name: "x", Method: "GET", Path: "x"})[0]
		if err := checkEndpointName(decl, appspec.ToSnakeCase(m)); err == nil {
			t.Errorf("endpoint name %q binds over Resource.%s", appspec.ToSnakeCase(m), m)
		}
		if strings.ToLower(m) != m {
			continue // no move key reaches a camelCase member
		}
		decl.States.Transitions[0].Key = m
		if err := validateDeclarationStates(decl); err == nil || !strings.Contains(err.Error(), "is reserved") {
			t.Errorf("move key %q binds over Resource.%s: %v", m, m, err)
		}
	}
}

// clientMembers reads the member names client.js's Client class defines:
// its methods and the fields its constructor sets with this.<name> =.
func clientMembers(t *testing.T) []string {
	t.Helper()
	decl := framework.EntityDeclaration{Name: "posts", Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}}}
	spec := sdkSpec{
		App: "app", SDKVersion: "0.0.0", GofastrVersion: "dev", Module: "local/app-sdk",
		Decls:    []framework.EntityDeclaration{decl},
		Entities: []cliEntity{buildEntityModel(decl, cliVerbs)},
	}
	var js string
	for _, f := range renderSDKJSFiles(spec) {
		if f.Name == "client.js" {
			js = f.Content
		}
	}
	start := strings.Index(js, "\nexport class Client {")
	if start < 0 {
		t.Fatal("client.js has no Client class")
	}
	body := js[start:]
	seen := map[string]bool{}
	var names []string
	for _, re := range []*regexp.Regexp{
		regexp.MustCompile(`(?m)^  (?:async )?([A-Za-z_$][\w$]*)\(`),
		regexp.MustCompile(`this\.([A-Za-z_$][\w$]*) =`),
	} {
		for _, m := range re.FindAllStringSubmatch(body, -1) {
			if !seen[m[1]] {
				seen[m[1]] = true
				names = append(names, m[1])
			}
		}
	}
	if !seen["token"] || !seen["do"] {
		t.Fatalf("member scan found %v; the Client class shape changed", names)
	}
	return names
}

// A table's client property binds on the Client itself (this["posts"]),
// so one named after a Client member overwrites it: a "token" table
// replaced the credential and every request sent "Bearer [object
// Object]". Underscore helpers are out of reach: camelCase drops "_".
func TestJSClientMembersReserved(t *testing.T) {
	for _, m := range append(clientMembers(t), jsObjectMembers...) {
		if strings.HasPrefix(m, "_") {
			continue
		}
		decl := framework.EntityDeclaration{Name: m, Table: m, Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}}}
		_, err := buildSDKSpec([]framework.EntityDeclaration{decl}, &SdkOptions{name: "app"})
		if err == nil || !strings.Contains(err.Error(), "is reserved") {
			t.Errorf("table %q binds over Client.%s: %v", m, m, err)
		}
	}
	posts := framework.EntityDeclaration{Name: "tokens", Table: "tokens", Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}}}
	if _, err := buildSDKSpec([]framework.EntityDeclaration{posts}, &SdkOptions{name: "app"}); err != nil {
		t.Errorf("tokens: %v", err)
	}
}

// An endpoint is a method on the Go Client, so one whose name camelCases
// to a Client field (token → Token) declares a field and a method of the
// same name: the SDK parses and only fails when the consumer builds it.
func TestGoClientFieldsReserved(t *testing.T) {
	src := renderClient([]framework.EntityDeclaration{{Name: "posts", Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}}}})
	file, err := parser.ParseFile(token.NewFileSet(), "client.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	var fields []string
	ast.Inspect(file, func(n ast.Node) bool {
		if ts, ok := n.(*ast.TypeSpec); ok && ts.Name.Name == "Client" {
			for _, f := range ts.Type.(*ast.StructType).Fields.List {
				for _, n := range f.Names {
					fields = append(fields, n.Name)
				}
			}
		}
		return true
	})
	if !slices.Contains(fields, "Token") {
		t.Fatalf("Client fields %v; the struct shape changed", fields)
	}
	decl := endpointDecls(framework.Endpoint{Name: "x", Method: "GET", Path: "x"})[0]
	for _, f := range fields {
		// The snake_case name that camelCases back to f: BaseURL ← base_u_r_l.
		var name strings.Builder
		for i, r := range f {
			if i > 0 && unicode.IsUpper(r) {
				name.WriteByte('_')
			}
			name.WriteRune(unicode.ToLower(r))
		}
		if err := checkEndpointName(decl, name.String()); err == nil || !strings.Contains(err.Error(), "Go client") {
			t.Errorf("endpoint %q binds over Client.%s: %v", name.String(), f, err)
		}
	}
}
