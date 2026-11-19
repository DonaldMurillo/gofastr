package framework

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/moduleproto"
)

func moduleHints(title string) *moduleproto.ToolAnnotations {
	return &moduleproto.ToolAnnotations{
		Title:           title,
		ReadOnlyHint:    mcp.Bool(true),
		DestructiveHint: mcp.Bool(false),
		IdempotentHint:  mcp.Bool(true),
		OpenWorldHint:   mcp.Bool(false),
	}
}

func TestModuleToolAnnotationsReachTheServer(t *testing.T) {
	srv := mcp.NewServer()
	reg := NewModuleToolRegistry(srv, nil)
	if err := reg.RegisterTools("alpha", []moduleproto.Tool{
		{ID: "search", Name: "Search", Annotations: moduleHints("Search the docs")},
		{ID: "plain", Name: "Plain"},
	}); err != nil {
		t.Fatal(err)
	}
	byName := map[string]mcp.Tool{}
	for _, tool := range srv.ListTools() {
		byName[tool.Name] = tool
	}
	got, err := json.Marshal(byName["module.alpha.search"].Annotations)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := json.Marshal(moduleHints("Search the docs"))
	if string(got) != string(want) {
		t.Fatalf("served annotations %s, want the module's %s", got, want)
	}
	if a := byName["module.alpha.plain"].Annotations; a != nil {
		t.Fatalf("a tool the module sent no annotations for serves %+v", a)
	}
}

func TestModuleToolAnnotationsShapeMatchesMCP(t *testing.T) {
	mod, _ := json.Marshal(moduleHints("T"))
	served, _ := json.Marshal(mcp.Annotations{Title: "T", ReadOnlyHint: mcp.Bool(true), DestructiveHint: mcp.Bool(false), IdempotentHint: mcp.Bool(true), OpenWorldHint: mcp.Bool(false)})
	if string(mod) != string(served) {
		t.Fatalf("module annotations encode as %s, MCP's as %s", mod, served)
	}
}

func TestModuleToolAnnotationsRefused(t *testing.T) {
	for name, title := range map[string]string{
		"too long":    strings.Repeat("a", maxModuleToolTitleBytes+1),
		"newline":     "Search\nforged",
		"bidi":        "Search\u202eforged",
		"zero width":  "Sea\u200brch",
		"invalid utf": "Search\xff",
	} {
		t.Run(name, func(t *testing.T) {
			srv := mcp.NewServer()
			reg := NewModuleToolRegistry(srv, nil)
			err := reg.RegisterTools("alpha", []moduleproto.Tool{
				{ID: "ok", Name: "OK", Annotations: moduleHints("Fine")},
				{ID: "bad", Name: "Bad", Annotations: moduleHints(title)},
			})
			if err == nil {
				t.Fatal("RegisterTools accepted the title")
			}
			if strings.Contains(err.Error(), "forged") {
				t.Errorf("error quotes the module's title: %v", err)
			}
			if srv.HasTool("module.alpha.ok") || srv.HasTool("module.alpha.bad") {
				t.Fatal("a tool was installed although the batch was refused")
			}
		})
	}
}

func TestModuleToolDigestPinsAnnotations(t *testing.T) {
	bare := moduleproto.Tool{ID: "lookup", Name: "Lookup", Description: "look up", InputSchema: json.RawMessage(`{"type":"object"}`)}
	legacy := sha256.Sum256([]byte(`{"id":"lookup","name":"Lookup","description":"look up","input_schema":{"type":"object"}}`))
	if ModuleToolDigest(bare) != hex.EncodeToString(legacy[:]) {
		t.Fatal("a tool without annotations no longer hashes as before, which breaks every pinned descriptor")
	}
	hinted := bare
	hinted.Annotations = moduleHints("")
	flipped := bare
	flipped.Annotations = moduleHints("")
	flipped.Annotations.DestructiveHint = mcp.Bool(true)
	if ModuleToolDigest(hinted) == ModuleToolDigest(bare) || ModuleToolDigest(hinted) == ModuleToolDigest(flipped) {
		t.Fatal("the digest does not pin the tool's annotations")
	}
}
