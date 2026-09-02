package webmcp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/router"
)

func TestExamplesValidateAgainstSchema(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"body":{"type":"string"},"count":{"type":"integer"}},"required":["body"]}`)
	h := New()
	good := validTool()
	good.InputSchema = schema
	good.Examples = []Example{
		{Summary: "One body", Input: json.RawMessage(`{"body":"hi","count":2}`)},
		{Summary: "Body only"},
	}
	if err := h.Register(good); err != nil {
		t.Fatalf("valid examples rejected: %v", err)
	}

	cases := []struct {
		name string
		in   string
	}{
		{"non-object", `["body"]`},
		{"missing required", `{"count":1}`},
		{"wrong type", `{"body":13}`},
		{"fractional integer", `{"body":"hi","count":1.5}`},
		{"broken json", `{`},
	}
	for _, tc := range cases {
		h := New()
		tl := validTool()
		tl.InputSchema = schema
		tl.Examples = []Example{{Input: json.RawMessage(tc.in)}}
		if err := h.Register(tl); err == nil || !strings.Contains(err.Error(), "example 0") {
			t.Fatalf("%s example accepted: %v", tc.name, err)
		}
	}

	// Preserved in the manifest for server-side agents.
	rt := router.New()
	if _, err := h.Mount(rt, nil); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	rt.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, ManifestRoute, nil))
	if !strings.Contains(rec.Body.String(), `"summary":"One body"`) {
		t.Fatalf("manifest lost the examples: %s", rec.Body.String())
	}
}

func TestExamplesClonedFromCaller(t *testing.T) {
	h := New()
	in := json.RawMessage(`{"body":"hi"}`)
	tl := validTool()
	tl.InputSchema = json.RawMessage(`{"type":"object","properties":{"body":{"type":"string"}},"required":["body"]}`)
	tl.Examples = []Example{{Summary: "a", Input: in}}
	if err := h.Register(tl); err != nil {
		t.Fatal(err)
	}
	copy(in, `X"body":"by`)
	// The slice elements are the caller's too: a Summary reassigned after
	// Register must not rewrite the frozen manifest.
	tl.Examples[0].Summary = "b"
	rt := router.New()
	if _, err := h.Mount(rt, nil); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	rt.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, ManifestRoute, nil))
	var m struct {
		Tools []Tool `json:"tools"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("manifest corrupted by caller-side mutation: %v", err)
	}
	if got := m.Tools[0].Examples[0].Summary; got != "a" {
		t.Fatalf("manifest example summary = %q, want the value at Register time (the caller's slice was not cloned)", got)
	}
	if got := string(m.Tools[0].Examples[0].Input); got != `{"body":"hi"}` {
		t.Fatalf("example aliased the caller's buffer: %s", got)
	}
}

func TestOutputSchemaValidatedAndPreserved(t *testing.T) {
	h := New()
	tl := validTool()
	tl.OutputSchema = json.RawMessage(`["not","an","object"]`)
	if err := h.Register(tl); err == nil {
		t.Fatal("non-object OutputSchema accepted")
	}

	h = New()
	out := json.RawMessage(`{"type":"object","properties":{"msg":{"type":"string"}}}`)
	tl = validTool()
	tl.OutputSchema = out
	if err := h.Register(tl); err != nil {
		t.Fatal(err)
	}
	copy(out, `XXXX{"type":"objec`)
	rt := router.New()
	if _, err := h.Mount(rt, nil); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	rt.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, ManifestRoute, nil))
	var m struct {
		Tools []struct {
			OutputSchema json.RawMessage `json:"outputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if got := string(m.Tools[0].OutputSchema); got != `{"type":"object","properties":{"msg":{"type":"string"}}}` {
		t.Fatalf("output schema lost or aliased: %s", got)
	}
}

func TestGroupsOrganizeWithoutRenaming(t *testing.T) {
	h := New()
	scene := h.Group("scene",
		WithDescription("Ground targets before guidance."),
		WithPreferredFirst("inspect_scene"),
	)
	rt := router.New()
	if err := scene.Register(Tool{
		Name:        "inspect_scene",
		Description: "Inspects the scene.",
		Method:      http.MethodGet,
		Path:        "/api/scene/inspect",
	}); err != nil {
		t.Fatal(err)
	}
	if err := scene.Handle(rt, Tool{
		Name:        "draw_arrow",
		Description: "Points at a target.",
		Method:      http.MethodPost,
		Path:        "/api/scene/arrow",
	}, okHandler()); err != nil {
		t.Fatal(err)
	}
	// An ungrouped tool coexists.
	if err := h.Register(validTool()); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Mount(rt, nil); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	rt.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, ManifestRoute, nil))
	var m struct {
		Groups []groupInfo `json:"groups"`
		Tools  []struct {
			Name  string `json:"name"`
			Group string `json:"group"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if len(m.Groups) != 1 || m.Groups[0].Name != "scene" ||
		m.Groups[0].Description != "Ground targets before guidance." ||
		m.Groups[0].PreferredFirst != "inspect_scene" {
		t.Fatalf("manifest groups: %+v", m.Groups)
	}
	// Names unchanged; grouping is a tag, not a rename.
	if m.Tools[0].Name != "inspect_scene" || m.Tools[0].Group != "scene" {
		t.Fatalf("grouped tool renamed or untagged: %+v", m.Tools[0])
	}
	if m.Tools[2].Name != "echo" || m.Tools[2].Group != "" {
		t.Fatalf("ungrouped tool gained a group: %+v", m.Tools[2])
	}
	// The grouped Handle tool's route still serves.
	rec = httptest.NewRecorder()
	rt.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/scene/arrow", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("grouped handle route: %d", rec.Code)
	}
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
}

func TestGroupValidationFailsMount(t *testing.T) {
	// PreferredFirst naming a tool that is not in the group.
	h := New()
	scene := h.Group("scene", WithPreferredFirst("inspect_scene"))
	if err := scene.Register(validTool()); err != nil {
		t.Fatal(err)
	}
	rt := router.New()
	_, err := h.Mount(rt, nil)
	if err == nil || !strings.Contains(err.Error(), "prefers") {
		t.Fatalf("dangling preferredFirst accepted: %v", err)
	}
	if rec := get(t, rt, ManifestRoute); rec.Code == http.StatusOK {
		t.Fatal("failed group mount left the manifest serving")
	}

	// A tool tagged with a group that was never declared.
	h2 := New()
	tl := validTool()
	tl.Group = "ghost"
	if err := h2.Register(tl); err == nil || !strings.Contains(err.Error(), "never declared") {
		t.Fatalf("undeclared group accepted: %v", err)
	}

	// Duplicate group construction surfaces on use.
	h3 := New()
	if err := h3.Group("dupe").Register(validTool()); err != nil {
		t.Fatal(err)
	}
	if err := h3.Group("dupe").Register(func() Tool {
		tl := validTool()
		tl.Name = "second"
		tl.Path = "/api/second"
		return tl
	}()); err == nil || !strings.Contains(err.Error(), "duplicate group") {
		t.Fatalf("duplicate group accepted: %v", err)
	}
}

func TestInstructionsGenerateOrientationTool(t *testing.T) {
	const text = "Inspect before mutating. Verify delivery from backend state, never from command success."
	h := New(WithInstructions(text))
	if err := h.Register(validTool()); err != nil {
		t.Fatal(err)
	}
	// The orientation tool's name is reserved while instructions are on.
	tl := validTool()
	tl.Name = InstructionsToolName
	tl.Path = "/api/mine"
	if err := h.Register(tl); err == nil || !strings.Contains(err.Error(), "reserves") {
		t.Fatalf("reserved name accepted: %v", err)
	}
	rt := router.New()
	if _, err := h.Mount(rt, nil); err != nil {
		t.Fatal(err)
	}

	// The manifest preserves the instructions and appends the tool.
	rec := httptest.NewRecorder()
	rt.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, ManifestRoute, nil))
	var m struct {
		Instructions string `json:"instructions"`
		Tools        []Tool `json:"tools"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if m.Instructions != text {
		t.Fatalf("manifest instructions: %q", m.Instructions)
	}
	if len(m.Tools) != 2 {
		t.Fatalf("orientation tool not appended: %+v", m.Tools)
	}
	ot := m.Tools[1]
	if ot.Name != InstructionsToolName || !ot.ReadOnlyHint ||
		ot.Method != http.MethodGet || ot.Path != InstructionsRoute {
		t.Fatalf("orientation tool: %+v", ot)
	}
	// The generated declaration skips Register, so it must carry the
	// input schema Register defaults: the browser refuses a tool whose
	// inputSchema is null, and the manifest is what the bridge feeds it.
	if string(ot.InputSchema) != defaultInputSchema {
		t.Fatalf("orientation tool inputSchema = %s, want %s", ot.InputSchema, defaultInputSchema)
	}

	// The route serves them, as the tool's endpoint.
	rec = httptest.NewRecorder()
	rt.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, InstructionsRoute, nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), text) {
		t.Fatalf("instructions route: %d %s", rec.Code, rec.Body.String())
	}

	// Without WithInstructions there is no route and the name is free.
	h2 := New()
	tl.Name = InstructionsToolName
	if err := h2.Register(tl); err != nil {
		t.Fatalf("name reserved without instructions: %v", err)
	}
	rt2 := router.New()
	if _, err := h2.Mount(rt2, nil); err != nil {
		t.Fatal(err)
	}
	if rec := get(t, rt2, InstructionsRoute); rec.Code != http.StatusNotFound {
		t.Fatalf("instructions route mounted without instructions: %d", rec.Code)
	}
}

func TestInstructionsRouteFollowsAssetPolicies(t *testing.T) {
	h := New(WithInstructions("wired"))
	if err := h.Register(validTool()); err != nil {
		t.Fatal(err)
	}
	rt := router.New()
	if _, err := h.Mount(rt, nil,
		WithAssetAuthorization(requireRole("support")),
		WithPageScope(func(r *http.Request) bool {
			c, err := r.Cookie("session")
			return err == nil && c.Value == "support"
		})); err != nil {
		t.Fatal(err)
	}
	rec := get(t, rt, InstructionsRoute)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous instructions fetch: %d", rec.Code)
	}
	rec = get(t, rt, InstructionsRoute, &http.Cookie{Name: "session", Value: "support"})
	if rec.Code != http.StatusOK {
		t.Fatalf("support instructions fetch: %d", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "private, no-store" {
		t.Fatalf("instructions cache policy: %q", cc)
	}
}

// The Field Assist shape: a large catalog where the instructions name
// the first action and groups mark it preferred, so an agent landing on
// the page has one place to learn the workflow.
func TestLargeCatalogCarriesInstructions(t *testing.T) {
	const first = "inspect_scene"
	const text = "Always inspect the scene before drawing anything. Verify delivery from backend state."
	h := New(WithInstructions(text))
	scene := h.Group("scene",
		WithDescription("Ground targets before guidance."),
		WithPreferredFirst(first),
	)
	if err := scene.Register(Tool{
		Name:         first,
		Description:  "Inspects the scene and returns verified targets.",
		Method:       http.MethodGet,
		Path:         "/api/scene/inspect",
		ReadOnlyHint: true,
	}); err != nil {
		t.Fatal(err)
	}
	// 24 more tools: the catalog crosses the 20-tool mark where
	// descriptions alone stop teaching the workflow.
	for i := range 24 {
		tl := Tool{
			Name:        fmt.Sprintf("draw_shape_%02d", i),
			Description: fmt.Sprintf("Draws guidance shape %d.", i),
			Method:      http.MethodPost,
			Path:        fmt.Sprintf("/api/scene/shape/%d", i),
			Examples: []Example{{
				Summary: "Point at a confirmed target",
				Input:   json.RawMessage(`{"target":"power-button"}`),
			}},
			OutputSchema:         json.RawMessage(`{"type":"object","properties":{"drawn":{"type":"boolean"}}}`),
			UntrustedContentHint: true,
		}
		var err error
		if i%2 == 0 {
			err = scene.Register(tl)
		} else {
			err = h.Register(tl)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	rt := router.New()
	if _, err := h.Mount(rt, nil); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	rt.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, ManifestRoute, nil))
	var m struct {
		Instructions string      `json:"instructions"`
		Groups       []groupInfo `json:"groups"`
		Tools        []Tool      `json:"tools"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if len(m.Tools) != 26 { // 25 declared + the orientation tool
		t.Fatalf("tool count: %d", len(m.Tools))
	}
	if m.Instructions != text || m.Groups[0].PreferredFirst != first {
		t.Fatalf("workflow metadata lost: %+v", m)
	}
	// The richer fields survive into the bridge payload too (the bridge
	// ignores what the browser proposal cannot forward; the browser test
	// proves registration still succeeds with them present).
	rec = httptest.NewRecorder()
	rt.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, ScriptRoute, nil))
	for _, want := range []string{`\"group\":\"scene\"`, `\"summary\":\"Point at a confirmed target\"`, `\"outputSchema\"`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("bridge payload lost %s", want)
		}
	}
}
