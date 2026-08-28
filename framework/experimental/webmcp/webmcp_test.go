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

func validTool() Tool {
	return Tool{
		Name:        "echo",
		Description: "Echoes its input.",
		Method:      http.MethodPost,
		Path:        "/api/echo",
	}
}

func TestRegisterEnforcesToolNameGrammar(t *testing.T) {
	h := New()
	// The WebMCP spec grammar: 1–128 of [A-Za-z0-9_.-]. Outside it the
	// browser rejects registerTool and the tool silently never appears.
	for _, bad := range []string{
		"", " echo", "create note", "café", "a/b", "a:b",
		strings.Repeat("x", 129),
	} {
		tl := validTool()
		tl.Name = bad
		if err := h.Register(tl); err == nil {
			t.Fatalf("name %q accepted", bad)
		}
	}
	for i, good := range []string{"echo", "Create.Note_v2-x", strings.Repeat("x", 128)} {
		tl := validTool()
		tl.Name = good
		tl.Path = fmt.Sprintf("/api/echo%d", i)
		if err := h.Register(tl); err != nil {
			t.Fatalf("name %q rejected: %v", good, err)
		}
	}
}

func TestRegisterRejectsEmptyDescription(t *testing.T) {
	h := New()
	tl := validTool()
	tl.Description = "  "
	if err := h.Register(tl); err == nil {
		t.Fatal("blank description accepted")
	}
}

func TestRegisterRejectsDuplicateName(t *testing.T) {
	h := New()
	if err := h.Register(validTool()); err != nil {
		t.Fatalf("first register: %v", err)
	}
	err := h.Register(validTool())
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate name accepted: %v", err)
	}
}

func TestRegisterRejectsBadMethod(t *testing.T) {
	h := New()
	tl := validTool()
	tl.Method = "TRACE"
	if err := h.Register(tl); err == nil {
		t.Fatal("TRACE accepted")
	}
}

func TestRegisterRejectsBadPaths(t *testing.T) {
	h := New()
	for _, p := range []string{
		"", "api/echo", "//evil.example/x", "https://evil.example/x",
		"/api/../admin", "/api/./echo", "/api\\echo", "/api/echo#frag",
		"/api/\x00echo",
		// percent-encoded forms: browser URL normalization decodes
		// these, so the declared path and the fetched path would differ
		"/api/%2e%2e/admin", "/api/%2E%2E/admin", "/api/echo%5Cadmin",
		"/api/%00echo", "/api/%zzbad",
	} {
		tl := validTool()
		tl.Path = p
		if err := h.Register(tl); err == nil {
			t.Fatalf("path %q accepted", p)
		}
	}
	// query strings are fine
	tl := validTool()
	tl.Path = "/api/echo?mode=fast"
	if err := h.Register(tl); err != nil {
		t.Fatalf("query path rejected: %v", err)
	}
}

func TestRegisterRejectsNonObjectSchema(t *testing.T) {
	h := New()
	for _, s := range []string{`"str"`, `[1]`, `{broken`} {
		tl := validTool()
		tl.InputSchema = json.RawMessage(s)
		if err := h.Register(tl); err == nil {
			t.Fatalf("schema %q accepted", s)
		}
	}
}

func TestMountRefusesZeroTools(t *testing.T) {
	if _, err := New().Mount(router.New(), nil); err == nil {
		t.Fatal("zero-tool mount accepted")
	}
}

func TestMountRefusesDoubleMount(t *testing.T) {
	h := New()
	if err := h.Register(validTool()); err != nil {
		t.Fatal(err)
	}
	rt := router.New()
	if _, err := h.Mount(rt, nil); err != nil {
		t.Fatalf("first mount: %v", err)
	}
	if _, err := h.Mount(rt, nil); err == nil {
		t.Fatal("second mount accepted")
	}
}

func TestRegisterAfterMountRefused(t *testing.T) {
	h := New()
	if err := h.Register(validTool()); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Mount(router.New(), nil); err != nil {
		t.Fatal(err)
	}
	tl := validTool()
	tl.Name = "late"
	if err := h.Register(tl); err == nil {
		t.Fatal("post-mount register accepted")
	}
}

type recordingRegistrar struct{ srcs []string }

func (r *recordingRegistrar) RegisterExternalScript(src string) error {
	r.srcs = append(r.srcs, src)
	return nil
}

type failingRegistrar struct{}

func (failingRegistrar) RegisterExternalScript(string) error {
	return fmt.Errorf("rail closed")
}

func TestMountFailureLeavesHostRemountable(t *testing.T) {
	h := New()
	if err := h.Register(validTool()); err != nil {
		t.Fatal(err)
	}
	rt := router.New()
	if _, err := h.Mount(rt, failingRegistrar{}); err == nil {
		t.Fatal("mount with refusing registrar succeeded")
	}
	// Nothing half-mounted: the failed Mount registered no routes...
	rec := httptest.NewRecorder()
	rt.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, ScriptRoute, nil))
	if rec.Code == http.StatusOK {
		t.Fatal("failed Mount left the script route serving")
	}
	// ...the Host still accepts registrations...
	late := validTool()
	late.Name = "late"
	late.Path = "/api/late"
	if err := h.Register(late); err != nil {
		t.Fatalf("register after failed mount: %v", err)
	}
	// ...and a retry mounts cleanly.
	if _, err := h.Mount(rt, nil); err != nil {
		t.Fatalf("re-mount after failure: %v", err)
	}
	rec = httptest.NewRecorder()
	rt.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, ScriptRoute, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("script route after re-mount: %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `\"name\":\"late\"`) {
		t.Fatal("re-mounted script is missing the late-registered tool")
	}
}

func TestMountOnGroupedRouterCarriesThePrefix(t *testing.T) {
	h := New()
	if err := h.Register(validTool()); err != nil {
		t.Fatal(err)
	}
	root := router.New()
	reg := &recordingRegistrar{}
	url, err := h.Mount(root.Group("/v1"), reg)
	if err != nil {
		t.Fatal(err)
	}
	// The rail URL must point where the route actually serves: under
	// the group prefix. An unprefixed URL is a guaranteed 404.
	if !strings.HasPrefix(url, "/v1"+ScriptRoute+"?v=") {
		t.Fatalf("script URL %q does not carry the group prefix", url)
	}
	rec := httptest.NewRecorder()
	root.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1"+ScriptRoute, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("prefixed script route: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	root.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1"+ManifestRoute, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("prefixed manifest route: %d", rec.Code)
	}
}

func TestMountConflictDetectionSeesGroupPrefix(t *testing.T) {
	h := New()
	if err := h.Register(validTool()); err != nil {
		t.Fatal(err)
	}
	root := router.New()
	grp := root.Group("/v1")
	grp.Get(ManifestRoute, http.NotFoundHandler())
	reg := &recordingRegistrar{}
	if _, err := h.Mount(grp, reg); err == nil {
		t.Fatal("conflict on the prefixed manifest route escaped the pre-flight")
	}
	if len(reg.srcs) != 0 {
		t.Fatalf("failed grouped Mount touched the script rail: %v", reg.srcs)
	}
}

func TestRegisterClonesInputSchema(t *testing.T) {
	h := New()
	schema := json.RawMessage(`{"type":"object"}`)
	tl := validTool()
	tl.InputSchema = schema
	if err := h.Register(tl); err != nil {
		t.Fatal(err)
	}
	// The caller still owns its slice; corrupting it after Register
	// must not reach the manifest frozen at Mount.
	copy(schema, `X"type":"objec`)
	rt := router.New()
	if _, err := h.Mount(rt, nil); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	rt.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, ManifestRoute, nil))
	var m struct{ Tools []Tool }
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("manifest corrupted by caller-side mutation: %v\n%s", err, rec.Body.String())
	}
	if string(m.Tools[0].InputSchema) != `{"type":"object"}` {
		t.Fatalf("manifest schema aliased the caller's buffer: %s", m.Tools[0].InputSchema)
	}
}

func TestMountRefusesConflictingRoutes(t *testing.T) {
	h := New()
	if err := h.Register(validTool()); err != nil {
		t.Fatal(err)
	}
	rt := router.New()
	rt.Get(ManifestRoute, http.NotFoundHandler())
	reg := &recordingRegistrar{}
	if _, err := h.Mount(rt, reg); err == nil {
		t.Fatal("Mount onto a router already holding the manifest route succeeded; the router would have panicked mid-mount")
	}
	if len(reg.srcs) != 0 {
		t.Fatalf("failed Mount touched the script rail: %v", reg.srcs)
	}
	if _, err := h.Mount(router.New(), nil); err != nil {
		t.Fatalf("re-mount on a clean router: %v", err)
	}
}

func TestScriptEscapesHostileToolFields(t *testing.T) {
	h := New()
	tl := validTool()
	// Name is grammar-locked, so hostile content can only arrive via
	// the free-text fields and schema strings. The served script must
	// keep all of it inside the JS string literals: encoding/json
	// escapes < > & (to \u003c etc.) and U+2028/U+2029, and this pins
	// that property against a future serializer swap.
	tl.Title = "</script><script>alert(1)</script>"
	tl.Description = "line sep \u2028 quoted \" back\\slash \u2029 end"
	tl.InputSchema = json.RawMessage("{\"type\":\"object\",\"description\":\"</script> end\"}")
	if err := h.Register(tl); err != nil {
		t.Fatal(err)
	}
	rt := router.New()
	if _, err := h.Mount(rt, nil); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	rt.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, ScriptRoute, nil))
	body := rec.Body.String()
	if strings.Contains(body, "</script>") {
		t.Fatal("raw </script> reached the served script")
	}
	if strings.Contains(body, "\u2028") || strings.Contains(body, "\u2029") {
		t.Fatal("raw U+2028/U+2029 reached the served script")
	}
	// The escaped forms prove the hostile content IS in the manifest,
	// inert inside the quoted JSON.parse payload, not silently dropped.
	// The manifest is double-encoded (JSON string inside a JSON string),
	// so the inner \u escapes appear with doubled backslashes.
	if !strings.Contains(body, `\\u003c/script\\u003e`) {
		t.Fatal("escaped script-close sequence missing; manifest not embedded?")
	}
	if !strings.Contains(body, `\\u2028`) || !strings.Contains(body, `\\u2029`) {
		t.Fatal("escaped U+2028/U+2029 missing; manifest not embedded?")
	}
}

func TestMountServesScriptAndManifest(t *testing.T) {
	h := New()
	tl := validTool()
	tl.Title = "Echo"
	tl.InputSchema = json.RawMessage(`{"type":"object","properties":{"msg":{"type":"string"}}}`)
	if err := h.Register(tl); err != nil {
		t.Fatal(err)
	}
	rt := router.New()
	reg := &recordingRegistrar{}
	url, err := h.Mount(rt, reg)
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.srcs) != 1 || reg.srcs[0] != url {
		t.Fatalf("registrar got %v, Mount returned %q", reg.srcs, url)
	}
	if !strings.HasPrefix(url, ScriptRoute+"?v=") {
		t.Fatalf("script URL %q is not hash-versioned", url)
	}

	// script: placeholder replaced with the tool set
	rec := httptest.NewRecorder()
	rt.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, ScriptRoute, nil))
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("script route: %d", rec.Code)
	}
	if strings.Contains(body, toolsPlaceholder) {
		t.Fatal("script still contains the tools placeholder")
	}
	// The manifest embeds as a quoted JSON string fed to JSON.parse, so
	// the tool fields appear with escaped quotes.
	if !strings.Contains(body, "JSON.parse(") {
		t.Fatal("script does not parse the manifest via JSON.parse")
	}
	if !strings.Contains(body, `\"name\":\"echo\"`) || !strings.Contains(body, `\"title\":\"Echo\"`) {
		t.Fatalf("script does not carry the tool manifest:\n%s", body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/javascript") {
		t.Fatalf("script content type %q", ct)
	}

	// manifest: JSON with the same tool, ETag revalidation works
	rec = httptest.NewRecorder()
	rt.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, ManifestRoute, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("manifest route: %d", rec.Code)
	}
	var m struct{ Tools []Tool }
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("manifest not JSON: %v", err)
	}
	if len(m.Tools) != 1 || m.Tools[0].Name != "echo" || m.Tools[0].Method != http.MethodPost {
		t.Fatalf("manifest tools: %+v", m.Tools)
	}
	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Fatal("manifest has no ETag")
	}
	req := httptest.NewRequest(http.MethodGet, ManifestRoute, nil)
	req.Header.Set("If-None-Match", etag)
	rec = httptest.NewRecorder()
	rt.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotModified {
		t.Fatalf("If-None-Match revalidation: %d", rec.Code)
	}
}

func TestMountDefaultsMethodAndSchema(t *testing.T) {
	h := New()
	tl := validTool()
	tl.Method = ""
	tl.InputSchema = nil
	if err := h.Register(tl); err != nil {
		t.Fatal(err)
	}
	rt := router.New()
	if _, err := h.Mount(rt, nil); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	rt.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, ManifestRoute, nil))
	var m struct{ Tools []Tool }
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if m.Tools[0].Method != http.MethodPost {
		t.Fatalf("method not defaulted: %q", m.Tools[0].Method)
	}
	if string(m.Tools[0].InputSchema) != defaultInputSchema {
		t.Fatalf("schema not defaulted: %s", m.Tools[0].InputSchema)
	}
}
