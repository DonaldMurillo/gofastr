package appgen

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// oaFuncs maps each function and method in src to its source text.
func oaFuncs(t *testing.T, src string) map[string]string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "client.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse: %v\n%s", err, src)
	}
	out := map[string]string{}
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok {
			continue
		}
		out[fn.Name.Name] = src[fset.Position(fn.Pos()).Offset:fset.Position(fn.End()).Offset]
	}
	return out
}

// The Go SDK's client.go is the --from-openapi CLI's internal/client:
// every function the CLI's client declares, the SDK's declares with the
// same text, and the SDK adds only DoStream.
func TestOpenAPISDKClientIsTheCLIClient(t *testing.T) {
	spec, err := oaSDKSpecFor(t, oaSDKFixture)
	if err != nil {
		t.Fatal(err)
	}
	files, err := renderOASDKGoFiles(spec)
	if err != nil {
		t.Fatal(err)
	}
	cliSpec := spec.API
	cliSpec.Binary = "barc"
	cli := oaFuncs(t, renderCLISelfClient(cliSpec))
	sdk := oaFuncs(t, files[0].Content)
	for name, body := range cli {
		if sdk[name] != body {
			t.Errorf("%s differs between the CLI client and the SDK client:\nCLI:\n%s\nSDK:\n%s", name, body, sdk[name])
		}
	}
	for name := range sdk {
		if _, shared := cli[name]; !shared && name != "DoStream" && name != "Close" {
			t.Errorf("the SDK client declares %s, which the CLI client does not", name)
		}
	}
	if _, ok := sdk["DoStream"]; !ok {
		t.Error("the SDK client has no DoStream")
	}
}

// Every read of resp.Body in the SDK's client is capped, except the one
// DoStream hands back unread after the status check: the caller of a
// binary operation reads it.
func TestOpenAPISDKClientBodiesBounded(t *testing.T) {
	spec, err := oaSDKSpecFor(t, oaSDKFixture)
	if err != nil {
		t.Fatal(err)
	}
	files, err := renderOASDKGoFiles(spec)
	if err != nil {
		t.Fatal(err)
	}
	src := files[0].Content
	streams := 0
	for i, line := range strings.Split(src, "\n") {
		if !strings.Contains(line, "resp.Body") {
			continue
		}
		switch trimmed := strings.TrimSpace(line); {
		case strings.Contains(line, "io.LimitReader(resp.Body,"), strings.Contains(line, "resp.Body.Close()"):
		case trimmed == `return &streamBody{ReadCloser: resp.Body, cancel: cancel}, resp.Header.Get("Content-Type"), nil`:
			streams++
		default:
			t.Errorf("SECURITY: the SDK client touches resp.Body with no size bound (line %d): %s", i+1, trimmed)
		}
	}
	if streams != 1 {
		t.Errorf("want one unread 2xx body (DoStream's), found %d", streams)
	}
	stream := oaFuncs(t, src)["DoStream"]
	refuse := strings.Index(stream, `return nil, "", statusError(resp)`)
	if refuse < 0 || refuse > strings.Index(stream, "return &streamBody{") || !strings.Contains(stream, "resp.StatusCode < 200 || resp.StatusCode >= 300") {
		t.Errorf("DoStream hands back a body before refusing a non-2xx status:\n%s", stream)
	}
	for _, f := range files[1:2] {
		if strings.Contains(f.Content, "resp.Body") || strings.Contains(f.Content, "io.ReadAll") {
			t.Errorf("%s reads a response itself instead of through the client", f.Name)
		}
	}
}
