package builtins

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/harness/tool"
)

// helper: build a registry with the given tools.
func regWith(t *testing.T, tools ...tool.Tool) *tool.Registry {
	t.Helper()
	r := tool.NewRegistry()
	src := staticToolSrc{tools: tools}
	if err := r.Register(context.Background(), src); err != nil {
		t.Fatal(err)
	}
	return r
}

type staticToolSrc struct{ tools []tool.Tool }

func (staticToolSrc) Name() string                                  { return "static-test" }
func (s staticToolSrc) Tools(_ context.Context) ([]tool.Tool, error) { return s.tools, nil }

func TestToolSearchByName(t *testing.T) {
	reg := regWith(t, Read{}, Write{}, Bash{}, WebFetch{}, TaskList{})
	ctx := tool.WithRegistry(context.Background(), reg)
	raw, _ := json.Marshal(map[string]any{"query": "write"})
	res, err := ToolSearch{}.Run(ctx, tool.ToolCall{Input: raw}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("unexpected error: %+v", res)
	}
	body := res.Content[0].Text
	if !strings.Contains(body, `"name": "Write"`) {
		t.Errorf("query 'write' did not surface the Write tool: %s", body)
	}
}

func TestToolSearchByDescription(t *testing.T) {
	reg := regWith(t, Read{}, Write{}, Bash{}, WebFetch{})
	ctx := tool.WithRegistry(context.Background(), reg)
	raw, _ := json.Marshal(map[string]any{"query": "fetch URL"})
	res, _ := ToolSearch{}.Run(ctx, tool.ToolCall{Input: raw}, nil)
	if !strings.Contains(res.Content[0].Text, "WebFetch") {
		t.Errorf("query 'fetch URL' missed WebFetch: %s", res.Content[0].Text)
	}
}

func TestToolSearchRanking(t *testing.T) {
	// 'plan' should rank TaskList above WebFetch.
	reg := regWith(t, Read{}, Write{}, WebFetch{}, TaskList{})
	ctx := tool.WithRegistry(context.Background(), reg)
	raw, _ := json.Marshal(map[string]any{"query": "plan"})
	res, _ := ToolSearch{}.Run(ctx, tool.ToolCall{Input: raw}, nil)
	body := res.Content[0].Text
	tlIdx := strings.Index(body, `"name": "TaskList"`)
	wfIdx := strings.Index(body, `"name": "WebFetch"`)
	if tlIdx == -1 {
		t.Fatalf("TaskList missing from results: %s", body)
	}
	if wfIdx != -1 && wfIdx < tlIdx {
		t.Errorf("WebFetch ranked above TaskList for query 'plan'")
	}
}

func TestToolSearchExcludesItself(t *testing.T) {
	reg := regWith(t, Read{}, ToolSearch{}, Write{})
	ctx := tool.WithRegistry(context.Background(), reg)
	raw, _ := json.Marshal(map[string]any{"query": "search"})
	res, _ := ToolSearch{}.Run(ctx, tool.ToolCall{Input: raw}, nil)
	if strings.Contains(res.Content[0].Text, `"name": "ToolSearch"`) {
		t.Errorf("ToolSearch should not appear in its own results: %s", res.Content[0].Text)
	}
}

func TestToolSearchEmptyResults(t *testing.T) {
	reg := regWith(t, Read{}, Write{})
	ctx := tool.WithRegistry(context.Background(), reg)
	raw, _ := json.Marshal(map[string]any{"query": "deploy kubernetes cluster"})
	res, _ := ToolSearch{}.Run(ctx, tool.ToolCall{Input: raw}, nil)
	if !strings.Contains(res.Content[0].Text, "no tools matched") {
		t.Errorf("expected 'no tools matched' hint, got: %s", res.Content[0].Text)
	}
}

func TestToolSearchNoRegistryInCtxIsError(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{"query": "read"})
	res, _ := ToolSearch{}.Run(context.Background(), tool.ToolCall{Input: raw}, nil)
	if !res.IsError || !strings.Contains(res.Content[0].Text, "no registry") {
		t.Errorf("expected no-registry error, got %+v", res)
	}
}

func TestToolSearchReturnsSchema(t *testing.T) {
	reg := regWith(t, Read{}, Write{})
	ctx := tool.WithRegistry(context.Background(), reg)
	raw, _ := json.Marshal(map[string]any{"query": "read"})
	res, _ := ToolSearch{}.Run(ctx, tool.ToolCall{Input: raw}, nil)
	// The body should include the Read tool's inputSchema JSON so the
	// model can call it the next round without further inspection.
	if !strings.Contains(res.Content[0].Text, `"inputSchema"`) {
		t.Errorf("results missing inputSchema field: %s", res.Content[0].Text)
	}
	if !strings.Contains(res.Content[0].Text, `"path"`) {
		t.Errorf("Read's schema's path property not surfaced: %s", res.Content[0].Text)
	}
}
