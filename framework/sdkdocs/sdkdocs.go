// Package sdkdocs serves a public SDK documentation site for a GoFastr
// app: install guides, a per-entity API reference rendered from the live
// registry, auth and error guides, and download routes for the artifacts
// `gofastr generate sdk` emits. Hosts call Mount beside their UI host:
//
//	sdkdocs.Mount(coreApp, fwApp.Router(), sdkdocs.Config{
//	    Registry:        fwApp.Registry,
//	    CRUDMounted:     fwApp.EntityCRUDMounted,
//	    ReadOnlyMounted: fwApp.EntityReadOnlyMounted,
//	    Artifacts:       os.DirFS("gen/sdk/dist"),
//	})
package sdkdocs

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/app/decide"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/router"
	corestatic "github.com/DonaldMurillo/gofastr/core/static"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/sdk"
	"github.com/DonaldMurillo/gofastr/internal/asciicase"
)

// Config configures the SDK docs site.
type Config struct {
	// BasePath mounts the site (default "/docs/api"). Artifact downloads
	// live under BasePath+"/sdk/".
	BasePath string

	// Registry is the live entity registry (pass fwApp.Registry). Required.
	Registry entity.Registry

	// CRUDMounted reports whether an entity's auto-CRUD routes were
	// registered (pass fwApp.EntityCRUDMounted). Nil documents every
	// entity whose Exposure allows CRUD.
	CRUDMounted func(*entity.Entity) bool

	// ReadOnlyMounted reports whether an entity's mounted CRUD routes are
	// read-only (pass fwApp.EntityReadOnlyMounted). Nil assumes writable.
	ReadOnlyMounted func(*entity.Entity) bool

	// Artifacts holds the dist directory `gofastr generate sdk` wrote.
	// Nil mounts the site without downloads.
	Artifacts fs.FS

	// OpenAPI is the JSON OpenAPI document the app publishes. SDKs
	// generated with `gofastr generate sdk --from-openapi` are served
	// only with it, and are checked for drift against it.
	OpenAPI []byte

	// AppName labels the site; defaults to the core-ui app's name.
	AppName string
	// BaseURL is the public origin in install and usage snippets. Empty
	// renders a placeholder origin.
	BaseURL string
	// APIPrefix mirrors framework.AppConfig.APIPrefix ("" or "api").
	APIPrefix string
	// SnakeCase mirrors a server configured with crud.CaseSnake.
	SnakeCase bool

	// Entities lists the entity names or tables to document. Nil
	// documents Public entities, or every entity with IncludeGated.
	Entities     []string
	IncludeGated bool

	// AuthBasePath is where battery/auth is mounted (default "/auth").
	AuthBasePath string
	// HasAPITokens enables the token-minting walkthrough (the app wires
	// auth.TokensPlugin + TokenMiddleware).
	HasAPITokens bool

	// Policy, when set, gates every docs screen, artifact download and
	// the section-menu drawer's routes. Nil is a public site.
	Policy app.Policy
}

type site struct {
	cfg Config

	manifestOnce sync.Once
	manifest     *sdk.Manifest
	drift        bool
	provenance   bool

	undocumentedOnce sync.Once
	unpublishedOnce  sync.Once
}

func (s *site) readOnly(e *entity.Entity) bool {
	return s.cfg.ReadOnlyMounted != nil && s.cfg.ReadOnlyMounted(e)
}

// Mount registers the docs screens on the core-ui app and the artifact
// routes on r. It errors without the app, the router or Config.Registry.
func Mount(coreApp *app.App, r *router.Router, cfg Config) error {
	if coreApp == nil || r == nil {
		return fmt.Errorf("sdkdocs: Mount requires the core-ui app and the router")
	}
	if cfg.Registry == nil {
		return fmt.Errorf("sdkdocs: Config.Registry is required (pass fwApp.Registry)")
	}
	cfg.BasePath = "/" + strings.Trim(cfg.BasePath, "/")
	if cfg.BasePath == "/" {
		cfg.BasePath = "/docs/api"
	}
	if cfg.AuthBasePath == "" {
		cfg.AuthBasePath = "/auth"
	}
	if cfg.AppName == "" {
		if coreApp.Name != "" {
			cfg.AppName = coreApp.Name
		} else {
			cfg.AppName = "This app"
		}
	}
	cfg.APIPrefix = strings.Trim(cfg.APIPrefix, "/")
	if cfg.APIPrefix != "" {
		cfg.APIPrefix = "/" + cfg.APIPrefix
	}

	s := &site{cfg: cfg}
	base := cfg.BasePath

	screens := []*app.Screen{
		app.NewScreen(base, &indexScreen{site: s}).
			WithTitle(cfg.AppName + " SDKs").
			WithDescription("Client SDKs and API reference for " + cfg.AppName),
		app.NewScreen(base+"/auth", &authScreen{site: s}).
			WithTitle("API authentication: " + cfg.AppName).
			WithDescription("API-token authentication for the " + cfg.AppName + " API"),
		app.NewScreen(base+"/errors", &errorsScreen{site: s}).
			WithTitle("API errors: " + cfg.AppName).
			WithDescription("Error envelope and status codes for the " + cfg.AppName + " API"),
		app.NewScreen(base+"/entities/:name", &entityScreen{site: s}).
			WithTitle("API reference: " + cfg.AppName).
			WithDescription("Entity API reference for " + cfg.AppName).
			WithPolicy(s.entityVisibilityPolicy()),
	}
	for _, sc := range screens {
		if cfg.Policy != nil {
			sc.WithPolicy(cfg.Policy)
		}
		coreApp.RegisterScreen(sc, nil)
	}

	drawerRouter := r
	if cfg.Policy != nil {
		drawerRouter = r.Group("", func(next http.Handler) http.Handler { return s.policyGate(false, next) })
	}
	widget.MountBuilder(drawerRouter, sectionMenuDrawer(s))

	r.Get(base+"/sdk/go.zip", s.artifactHandler("go", sdk.GoArtifact))
	r.Get(base+"/sdk/client.js", s.artifactHandler("js", sdk.JSArtifact))
	r.Get(base+"/sdk/client.d.ts", s.artifactHandler("js-types", sdk.JSTypesArtifact))
	r.Get(base+"/sdk/manifest.json", s.artifactHandler("", sdk.ManifestFile))
	return nil
}

func (s *site) includedEntities() []*entity.Entity {
	var out []*entity.Entity
	for _, e := range s.cfg.Registry.AllSorted() {
		if e == nil {
			continue
		}
		if e.Config.Exposure.CRUD != nil && !*e.Config.Exposure.CRUD {
			continue
		}
		if s.cfg.CRUDMounted != nil && !s.cfg.CRUDMounted(e) {
			continue
		}
		if len(s.cfg.Entities) > 0 {
			if !nameMatches(e, s.cfg.Entities) {
				continue
			}
		} else if !s.cfg.IncludeGated && !e.Config.Exposure.Public {
			continue
		}
		out = append(out, e)
	}
	return out
}

func (s *site) firstIncludedEntityPerTable() []*entity.Entity {
	seen := map[string]bool{}
	var out []*entity.Entity
	for _, e := range s.includedEntities() {
		if !seen[e.Config.Table] {
			seen[e.Config.Table] = true
			out = append(out, e)
		}
	}
	return out
}

func nameMatches(e *entity.Entity, names []string) bool {
	for _, n := range names {
		if matchesFoldedName(e, asciicase.Lower(strings.TrimSpace(n))) {
			return true
		}
	}
	return false
}

func matchesFoldedName(e *entity.Entity, folded string) bool {
	return folded == asciicase.Lower(e.Config.Name) || folded == asciicase.Lower(e.Config.Table)
}

func (s *site) lookup(segment string) (*entity.Entity, bool) {
	all := s.lookupAll(segment)
	if len(all) == 0 {
		return nil, false
	}
	return all[0], true
}

func (s *site) lookupAll(segment string) []*entity.Entity {
	segment = asciicase.Lower(segment)
	var out []*entity.Entity
	for _, e := range s.includedEntities() {
		if matchesFoldedName(e, segment) {
			out = append(out, e)
		}
	}
	return out
}

func (s *site) entityVisibilityPolicy() app.Policy {
	prefix := s.cfg.BasePath + "/entities/"
	return app.PolicyFunc(func(ctx context.Context) app.Decision {
		r := app.RequestFromContext(ctx)
		if r == nil {
			return decide.Allow()
		}
		segment := strings.Trim(strings.TrimPrefix(r.URL.Path, prefix), "/")
		if _, ok := s.lookup(segment); !ok {
			return decide.Block(http.StatusNotFound)
		}
		return decide.Allow()
	})
}

func (s *site) resolved() (m *sdk.Manifest, drift, unknownProvenance bool) {
	s.manifestOnce.Do(func() {
		if s.cfg.Artifacts == nil {
			return
		}
		mf, err := sdk.ReadManifest(s.cfg.Artifacts)
		if err != nil {
			if !anyArtifactPresent(s.cfg.Artifacts) {
				return
			}
			slog.Warn("sdkdocs: artifacts present but manifest unreadable: treating downloads as unknown provenance", "err", err)
			s.provenance = true
			return
		}
		s.manifest = mf
		if mf.SchemaVersion != sdk.SchemaVersion {
			s.provenance = true
			return
		}
		if mf.Source == sdk.SourceOpenAPI {
			if s.cfg.OpenAPI == nil {
				return
			}
			live, err := sdk.OpenAPIHash(s.cfg.OpenAPI)
			if err != nil || live != mf.SchemaHash {
				s.drift = true
				slog.Warn("sdkdocs: SDK artifacts were generated from another OpenAPI document: re-run `gofastr generate sdk --from-openapi`",
					"manifest", mf.SchemaHash, "live", live, "err", err)
			}
			return
		}
		jsonCase := ""
		if s.cfg.SnakeCase {
			jsonCase = "snake_case"
		}
		live := sdk.SchemaHash(sdk.RegistryNamedConfigs(s.cfg.Registry, mf.Entities), jsonCase)
		if live != mf.SchemaHash {
			s.drift = true
			slog.Warn("sdkdocs: SDK artifacts were generated from an older schema: re-run `gofastr generate sdk`",
				"manifest", mf.SchemaHash, "live", live)
		}
	})
	return s.manifest, s.drift, s.provenance
}

func anyArtifactPresent(fsys fs.FS) bool {
	for _, name := range []string{sdk.GoArtifact, sdk.JSArtifact, sdk.JSTypesArtifact} {
		if f, err := fsys.Open(name); err == nil {
			f.Close()
			return true
		}
	}
	return false
}

func (s *site) artifactHandler(key, file string) http.Handler {
	contentType := map[string]string{
		sdk.GoArtifact:      "application/zip",
		sdk.JSArtifact:      "application/javascript; charset=utf-8",
		sdk.JSTypesArtifact: "text/plain; charset=utf-8",
		sdk.ManifestFile:    "application/json; charset=utf-8",
	}[file]

	return s.policyGate(true, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.Artifacts == nil {
			i18n.HTTPError(w, r, http.StatusNotFound, reasonSDKNotGenerated)
			return
		}
		m, _, _ := s.resolved()
		if m != nil && m.Source == sdk.SourceOpenAPI && s.cfg.OpenAPI == nil {
			s.unpublishedOnce.Do(func() {
				slog.Warn("sdkdocs: SDK downloads withheld: they were generated from an OpenAPI document and Config.OpenAPI does not publish one")
			})
			i18n.HTTPError(w, r, http.StatusNotFound, reasonSDKUnpublishedOpenAPI)
			return
		}
		if !s.artifactsDocumented(m) {
			i18n.HTTPError(w, r, http.StatusNotFound, reasonSDKUndocumented)
			return
		}
		data, err := fs.ReadFile(s.cfg.Artifacts, file) //gofastr:allow(rootread) the fs is host configuration and file is one of the fixed sdk artifact names bound at route registration; the request cannot type it
		if err != nil {
			i18n.HTTPError(w, r, http.StatusNotFound, reasonSDKArtifactMissing)
			return
		}

		w.Header().Set("Cache-Control", "no-cache")
		if s.cfg.Policy != nil {
			w.Header().Set("Cache-Control", "private, no-cache")
		}
		if key != "" {
			a, ok := m.Artifacts[key]
			if !ok || a.File != file || a.SHA256 == "" || fmt.Sprintf("%x", sha256.Sum256(data)) != a.SHA256 {
				i18n.HTTPError(w, r, http.StatusNotFound, reasonSDKArtifactMissing)
				return
			}
			etag := `"` + a.SHA256 + `"`
			w.Header().Set("ETag", etag)
			if corestatic.IfNoneMatch(r, etag) {
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}
		w.Header().Set("Content-Type", contentType)
		if file == sdk.GoArtifact {
			name := s.cfg.AppName + "-sdk-go"
			if m != nil && m.SDKVersion != "" {
				name += "-" + m.SDKVersion
			}
			w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", sanitizeFilename(name)+".zip"))
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(data)))
		if r.Method == http.MethodHead {
			return
		}
		_, _ = w.Write(data)
	}))
}

func (s *site) artifactsDocumented(m *sdk.Manifest) bool {
	if m == nil {
		return false
	}
	documented := map[string]bool{}
	for _, e := range s.includedEntities() {
		documented[e.Config.Name] = true
	}
	var undocumented []string
	for _, name := range m.Entities {
		if !documented[name] {
			undocumented = append(undocumented, name)
		}
	}
	if len(undocumented) == 0 {
		return true
	}
	s.undocumentedOnce.Do(func() {
		slog.Warn("sdkdocs: SDK downloads withheld: the artifacts cover entities this site does not document; "+
			"regenerate with `gofastr generate sdk --only=<documented entities>`, or widen Config.Entities / IncludeGated",
			"entities", textsafe.StripUnsafe(strings.Join(undocumented, ",")))
	})
	return false
}

func (s *site) policyGate(redirect bool, next http.Handler) http.Handler {
	if s.cfg.Policy == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d := s.cfg.Policy.Decide(app.WithRequest(r.Context(), r))
		switch {
		case d.Kind == app.DecisionAllow:
			next.ServeHTTP(w, r)
		case d.Kind == app.DecisionRedirect && redirect:
			http.Redirect(w, r, textsafe.StripUnsafe(d.URL), http.StatusSeeOther)
		default:
			status := d.Status
			if status == 0 || d.Kind == app.DecisionRedirect {
				status = http.StatusForbidden
			}
			i18n.HTTPError(w, r, status, i18n.StatusPhrase(status))
		}
	})
}

func sanitizeFilename(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return b.String()
}

func fenceWord(v string) string {
	var b strings.Builder
	b.Grow(len(v))
	for _, r := range v {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.', r == '~', r == '/':
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (s *site) sortedNames() []string {
	var out []string
	for _, e := range s.includedEntities() {
		out = append(out, e.Config.Name)
	}
	sort.Strings(out)
	return out
}
