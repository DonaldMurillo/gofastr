package appgen

import (
	"fmt"
	"go/format"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/project"
)

func renderSDKRepositories(spec sdkSpec) ([]GeneratedFile, error) {
	var remote strings.Builder
	remote.WriteString("package client\n\nimport (\n\"context\"\n\"net/url\"\n)\n\n")
	var local strings.Builder
	fmt.Fprintf(&local, `package local

import (
	"net/http"
	"net/http/httptest"
	"time"
	client %q
)

type transport struct { handler http.Handler }

func (local transport) RoundTrip(request *http.Request) (*http.Response, error) {
	if err := request.Context().Err(); err != nil {
		return nil, err
	}
	incoming := request.Clone(request.Context())
	if incoming.Host == "" {
		incoming.Host = incoming.URL.Host
	}
	// A server sees the path form: Host on the request, no scheme or
	// host in URL. Middleware that reads r.URL must see the same here.
	incoming.URL.Scheme, incoming.URL.Host = "", ""
	incoming.RequestURI = incoming.URL.RequestURI()
	recorder := httptest.NewRecorder()
	local.handler.ServeHTTP(recorder, incoming)
	if err := request.Context().Err(); err != nil {
		return nil, err
	}
	response := recorder.Result()
	response.Request = request
	return response, nil
}

func newClient(handler http.Handler) *client.Client {
	if handler == nil {
		panic("local repository requires the application's HTTP handler")
	}
	return client.NewClient(%q, &http.Client{Transport: transport{handler: handler}, Timeout: 30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	})
}

`, spec.Module, "http://gofastr.local"+spec.APIPrefix)
	for _, declaration := range spec.Decls {
		name := appspec.ToCamelCase(declaration.Name)
		if !project.IsGoIdentifier(name) {
			return nil, fmt.Errorf("invalid repository entity identifier %q", name)
		}
		keyArguments := make([]string, 0, len(declKeyCols(declaration)))
		for _, column := range declKeyCols(declaration) {
			keyArguments = append(keyArguments, column.Param)
		}
		keyParams := goKeyParams(declaration)
		keys := strings.Join(keyArguments, ", ")
		receiver := "repo"
		for slices.Contains(keyArguments, receiver) {
			receiver += "Value"
		}
		methods := []struct {
			name, parameters, arguments, result string
		}{
			{"List", "params url.Values", "params", "(" + name + "ListResponse, error)"},
			{"Get", keyParams, keys, "(" + name + ", error)"},
			{"Create", "body " + name + "Input", "body", "(" + name + ", error)"},
			{"Update", keyParams + ", body " + name + "Input", keys + ", body", "(" + name + ", error)"},
			{"Patch", keyParams + ", body " + name + "Patch", keys + ", body", "(" + name + ", error)"},
			{"Delete", keyParams, keys, "error"},
		}
		fmt.Fprintf(&remote, "type %sRepo interface {\n", name)
		for _, method := range methods {
			fmt.Fprintf(&remote, "%s(ctx context.Context, %s) %s\n", method.name, method.parameters, method.result)
		}
		fmt.Fprintf(&remote, "}\n\ntype %sRemote struct { client *Client }\n\n", name)
		fmt.Fprintf(&remote, "func New%sRemote(client *Client) *%sRemote { return &%sRemote{client: client} }\n\n", name, name, name)
		fmt.Fprintf(&remote, "var _ %sRepo = (*%sRemote)(nil)\n\n", name, name)
		for _, method := range methods {
			fmt.Fprintf(&remote, "func (%s *%sRemote) %s(ctx context.Context, %s) %s {\nreturn %s.client.%s%s(ctx, %s)\n}\n\n", receiver, name, method.name, method.parameters, method.result, receiver, method.name, name, method.arguments)
		}
		fmt.Fprintf(&local, "type %sLocal struct { *client.%sRemote }\n\n", name, name)
		fmt.Fprintf(&local, "func New%sLocal(handler http.Handler) *%sLocal {\nreturn &%sLocal{%sRemote: client.New%sRemote(newClient(handler))}\n}\n\n", name, name, name, name, name)
		fmt.Fprintf(&local, "var _ client.%sRepo = (*%sLocal)(nil)\n\n", name, name)
	}
	files := []GeneratedFile{{Name: "repositories.go", Content: remote.String()}, {Name: "local/repositories.go", Content: local.String()}}
	for index := range files {
		formatted, err := format.Source([]byte(files[index].Content))
		if err != nil {
			return nil, fmt.Errorf("generated %s does not parse: %w", files[index].Name, err)
		}
		files[index].Content = string(formatted)
	}
	return files, nil
}
