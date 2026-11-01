package sdkdocs

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/app/decide"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/sdk"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

type fakeRegistry struct{ entities []*entity.Entity }

func (f *fakeRegistry) All() map[string]*entity.Entity {
	out := map[string]*entity.Entity{}
	for _, e := range f.entities {
		out[e.Config.Name] = e
	}
	return out
}
func (f *fakeRegistry) AllSorted() []*entity.Entity { return f.entities }
func (f *fakeRegistry) Get(name string) (*entity.Entity, error) {
	for _, e := range f.entities {
		if e.Config.Name == name {
			return e, nil
		}
	}
	return nil, fmt.Errorf("entity %q not registered", name)
}

func testRegistry() *fakeRegistry {
	posts := entity.Define("posts", entity.EntityConfig{Exposure: &entity.ExposureConfig{Public: true}, SearchFields: []string{"title"},
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "views", Type: schema.Int},
			{Name: "status", Type: schema.Enum, Values: []string{"draft", "live"}},
			{Name: "secret_note", Type: schema.String, Hidden: true},
		},
	})
	invoices := entity.Define("invoices", entity.EntityConfig{
		// Gated: not Public → excluded by default.
		Fields: []schema.Field{{Name: "amount", Type: schema.Decimal}},
	})
	return &fakeRegistry{entities: []*entity.Entity{invoices, posts}}
}

// sdkSum is the manifest's SHA256 spelling for a fixture file.
func sdkSum(data string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(data))) }

// testArtifacts builds a dist FS whose manifest matches (or mismatches) the
// live posts entity.
func testArtifacts(t *testing.T, reg *fakeRegistry, matchLive bool) fstest.MapFS {
	t.Helper()
	hash := "sha256:deadbeef"
	if matchLive {
		hash = sdk.SchemaHash(sdk.RegistryNamedConfigs(reg, []string{"posts"}))
	}
	m := sdk.Manifest{
		SchemaVersion:  sdk.SchemaVersion,
		App:            "testapp",
		SDKVersion:     "1.0.0",
		GofastrVersion: "v0.33.0",
		GeneratedAt:    time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC),
		Entities:       []string{"posts"},
		SchemaHash:     hash,
		Artifacts: map[string]sdk.Artifact{
			"go":       {File: sdk.GoArtifact, SHA256: sdkSum("zip"), Bytes: 3, Module: "local/testapp-sdk"},
			"js":       {File: sdk.JSArtifact, SHA256: sdkSum("//js\n"), Bytes: 5},
			"js-types": {File: sdk.JSTypesArtifact, SHA256: sdkSum("//ts"), Bytes: 4},
		},
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return fstest.MapFS{
		sdk.ManifestFile:    {Data: raw},
		sdk.GoArtifact:      {Data: []byte("zip")},
		sdk.JSArtifact:      {Data: []byte("//js\n")},
		sdk.JSTypesArtifact: {Data: []byte("//ts")},
	}
}

func mountedServer(t *testing.T, cfg Config) *httptest.Server {
	t.Helper()
	coreApp := app.NewApp("Test App")
	host := uihost.New(coreApp)
	r := router.New()
	host.Mount(r)
	if err := Mount(coreApp, r, cfg); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

func get(t *testing.T, srv *httptest.Server, path string) (*http.Response, string) {
	t.Helper()
	resp, err := srv.Client().Get(srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	return resp, string(body)
}

func TestMountRequiresRegistry(t *testing.T) {
	if err := Mount(app.NewApp("x"), router.New(), Config{}); err == nil {
		t.Fatal("nil registry accepted")
	}
}

func TestIndexPageRenders(t *testing.T) {
	reg := testRegistry()
	srv := mountedServer(t, Config{Registry: reg, Artifacts: testArtifacts(t, reg, true), BaseURL: "https://api.test"})
	resp, body := get(t, srv, "/docs/api")
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	for _, want := range []string{
		"Test App SDKs",
		"go.zip", "client.js", "client.d.ts",
		"gofastr v0.33.0",
		"https://api.test",
		"posts", // nav entry
	} {
		if !strings.Contains(body, want) {
			t.Errorf("index missing %q", want)
		}
	}
	if strings.Contains(body, "invoices") {
		t.Error("gated entity leaked into the index/nav")
	}
	if strings.Contains(body, "out of date") {
		t.Error("drift banner shown for a matching manifest")
	}
}

func TestEntityReferencePage(t *testing.T) {
	reg := testRegistry()
	srv := mountedServer(t, Config{Registry: reg, Artifacts: testArtifacts(t, reg, true)})
	resp, body := get(t, srv, "/docs/api/entities/posts")
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	for _, want := range []string{
		"title", "views", "enum",
		"draft, live",
		"/posts/_batch", "/posts/_events",
		"?q=",       // SearchFields present
		"views_gte", // snake filter example (numeric field preferred)
		"ListPosts", // Go example method (token spans break longer substrings)
	} {
		if !strings.Contains(body, want) {
			t.Errorf("entity page missing %q", want)
		}
	}
	if strings.Contains(body, "secret_note") || strings.Contains(body, "secretNote") {
		t.Error("hidden field leaked into public reference")
	}
}

func TestGatedEntity404s(t *testing.T) {
	reg := testRegistry()
	srv := mountedServer(t, Config{Registry: reg})
	for _, path := range []string{"/docs/api/entities/invoices", "/docs/api/entities/nope"} {
		resp, _ := get(t, srv, path)
		if resp.StatusCode != 404 {
			t.Errorf("%s: status %d, want 404 (gated must be indistinguishable from missing)", path, resp.StatusCode)
		}
	}
}

func TestIncludeGatedOptIn(t *testing.T) {
	reg := testRegistry()
	srv := mountedServer(t, Config{Registry: reg, IncludeGated: true})
	resp, body := get(t, srv, "/docs/api/entities/invoices")
	if resp.StatusCode != 200 || !strings.Contains(body, "amount") {
		t.Fatalf("IncludeGated entity page: status %d", resp.StatusCode)
	}
}

func TestExplicitAllowList(t *testing.T) {
	reg := testRegistry()
	srv := mountedServer(t, Config{Registry: reg, Entities: []string{"invoices"}})
	if resp, _ := get(t, srv, "/docs/api/entities/invoices"); resp.StatusCode != 200 {
		t.Errorf("allow-listed gated entity should document: %d", resp.StatusCode)
	}
	if resp, _ := get(t, srv, "/docs/api/entities/posts"); resp.StatusCode != 404 {
		t.Errorf("entity outside the allow-list should 404: %d", resp.StatusCode)
	}
}

func TestArtifactDownloadHeaders(t *testing.T) {
	reg := testRegistry()
	srv := mountedServer(t, Config{Registry: reg, Artifacts: testArtifacts(t, reg, true)})

	resp, body := get(t, srv, "/docs/api/sdk/go.zip")
	if resp.StatusCode != 200 || body != "zip" {
		t.Fatalf("go.zip: %d %q", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/zip" {
		t.Errorf("go.zip content-type %q", ct)
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, "attachment") || !strings.Contains(cd, "1.0.0") {
		t.Errorf("go.zip content-disposition %q", cd)
	}
	if resp.Header.Get("ETag") != `"`+sdkSum("zip")+`"` {
		t.Errorf("go.zip etag %q", resp.Header.Get("ETag"))
	}

	// client.js serves inline (directly importable), no attachment.
	resp, _ = get(t, srv, "/docs/api/sdk/client.js")
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/javascript") {
		t.Errorf("client.js content-type %q", resp.Header.Get("Content-Type"))
	}
	if resp.Header.Get("Content-Disposition") != "" {
		t.Error("client.js must serve inline for direct ESM import")
	}

	// ETag revalidation → 304.
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/docs/api/sdk/go.zip", nil)
	req.Header.Set("If-None-Match", `"stale", "`+sdkSum("zip")+`"`)
	resp304, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp304.Body.Close()
	if resp304.StatusCode != http.StatusNotModified {
		t.Errorf("If-None-Match: status %d, want 304", resp304.StatusCode)
	}
}

func TestMissingArtifactsGuide(t *testing.T) {
	reg := testRegistry()
	srv := mountedServer(t, Config{Registry: reg})
	resp, body := get(t, srv, "/docs/api/sdk/go.zip")
	if resp.StatusCode != 404 || !strings.Contains(body, "gofastr generate sdk") {
		t.Fatalf("missing artifacts should 404 with guidance: %d %q", resp.StatusCode, body)
	}
	_, page := get(t, srv, "/docs/api")
	if !strings.Contains(page, "gofastr generate sdk") {
		t.Error("index should explain how to generate artifacts")
	}
}

func TestDriftBanner(t *testing.T) {
	reg := testRegistry()
	srv := mountedServer(t, Config{Registry: reg, Artifacts: testArtifacts(t, reg, false)})
	_, body := get(t, srv, "/docs/api")
	if !strings.Contains(body, "out of date") {
		t.Error("mismatched schema hash should render the drift banner")
	}
	// Downloads still work, stale beats nothing.
	if resp, _ := get(t, srv, "/docs/api/sdk/go.zip"); resp.StatusCode != 200 {
		t.Errorf("stale artifact should still download: %d", resp.StatusCode)
	}
}

func TestPolicyGatesScreensAndDownloads(t *testing.T) {
	reg := testRegistry()
	deny := app.PolicyFunc(func(context.Context) app.Decision {
		return decide.Block(http.StatusForbidden)
	})
	srv := mountedServer(t, Config{Registry: reg, Artifacts: testArtifacts(t, reg, true), Policy: deny})
	for _, path := range []string{"/docs/api", "/docs/api/entities/posts", "/docs/api/sdk/go.zip", "/docs/api/sdk/client.js"} {
		resp, _ := get(t, srv, path)
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s: status %d, want 403", path, resp.StatusCode)
		}
	}
}

func TestStaticPathsCoverIncludedEntities(t *testing.T) {
	reg := testRegistry()
	s := &site{cfg: Config{Registry: reg, BasePath: "/docs/api"}}
	sc := &entityScreen{site: s}
	paths := sc.StaticPaths(context.Background())
	if len(paths) != 1 || paths[0]["name"] != "posts" {
		t.Fatalf("StaticPaths: %+v", paths)
	}
}

func TestMenuMarksOnlyTheCurrentPage(t *testing.T) {
	s := &site{cfg: Config{Registry: testRegistry(), BasePath: "/docs/api"}}
	marked := func(key string) []string {
		cfg := s.menuConfig(key)
		var out []string
		if cfg.Lead != nil && cfg.Lead.Active {
			out = append(out, cfg.Lead.Label)
		}
		for _, g := range cfg.Groups {
			for _, it := range g.Items {
				if it.Active {
					out = append(out, it.Label)
				}
			}
		}
		return out
	}
	// The drawer is mounted once for every page, and the 404 page is no
	// page in the menu: neither may mark Overview.
	if got := marked(""); len(got) != 0 {
		t.Errorf("drawer/404 menu marks %v; want nothing", got)
	}
	if got := marked("overview"); len(got) != 1 || got[0] != "Overview" {
		t.Errorf("index menu marks %v; want [Overview]", got)
	}
	if got := marked("errors"); len(got) != 1 || got[0] != "Errors" {
		t.Errorf("errors menu marks %v; want [Errors]", got)
	}
}

// A settings-declared endpoint names its typed SDK calls; a
// hand-registered one points at Do.
func TestEndpointRowsNameTypedCalls(t *testing.T) {
	reg := &fakeRegistry{entities: []*entity.Entity{
		entity.Define("posts", entity.EntityConfig{
			Table:    "blog_posts",
			Exposure: &entity.ExposureConfig{Public: true},
			Fields:   []schema.Field{{Name: "title", Type: schema.String}},
			Endpoints: []entity.Endpoint{
				{Method: "POST", Path: "{id}/feature", Name: "feature_post", SDK: true},
				{Method: "GET", Path: "stats", Name: "post_stats"},
			},
		}),
	}}
	srv := mountedServer(t, Config{Registry: reg})
	_, body := get(t, srv, "/docs/api/entities/posts")
	for _, want := range []string{"Go c.FeaturePost, TypeScript api.blogPosts.featurePost", "Custom endpoint (post_stats): use the SDK&#39;s Do escape hatch"} {
		if !strings.Contains(body, want) {
			t.Errorf("entity page lacks %q", want)
		}
	}
}
