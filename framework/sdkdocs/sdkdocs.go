// Package sdkdocs serves a public SDK documentation site for a GoFastr app
// — install guides, a live per-entity API reference, auth and error guides —
// plus download routes for the pregenerated SDK artifacts that
// `gofastr generate sdk` emits (see framework/sdk for the shared contract).
//
// The reference pages render from the live entity registry on every request,
// so they cannot drift from the running API. Only the downloadable artifacts
// can go stale; the manifest's schema hash is compared against the live
// registry once and a banner + one WARN surface the mismatch.
//
// It is deliberately NOT part of framework/uihost: the screens compose
// framework/ui, and uihost must never import framework/ui (its LoadAlways
// styles would leak into every host's CSS bundle). Hosts wire it beside the
// UI host instead:
//
//	coreApp := app.NewApp("My App")
//	// … screens, uihost.New, fwApp.Mount(host) …
//	sdkdocs.Mount(coreApp, fwApp.Router(), sdkdocs.Config{
//	    Registry:  fwApp.Registry,
//	    Artifacts: os.DirFS("gen/sdk/dist"),
//	})
package sdkdocs

import (
	"context"
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
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/sdk"
)

// Config configures the SDK docs site.
type Config struct {
	// BasePath mounts the site (default "/docs/api"). Artifact downloads
	// live under BasePath+"/sdk/".
	BasePath string

	// Registry is the live entity registry (pass fwApp.Registry).
	// Required — reference pages render from it per request.
	Registry entity.Registry

	// Artifacts holds the pregenerated dist directory from
	// `gofastr generate sdk` (os.DirFS("gen/sdk/dist") or an embed.FS
	// subtree). Nil mounts the docs site without downloads; pages show
	// how to generate them instead.
	Artifacts fs.FS

	// AppName labels the site; defaults to the core-ui app's name.
	AppName string
	// BaseURL is the public origin baked into install/usage snippets
	// (e.g. "https://api.example.com"). Empty renders an honest
	// placeholder.
	BaseURL string
	// APIPrefix mirrors framework.AppConfig.APIPrefix ("" or "api") so
	// documented paths match the live mount.
	APIPrefix string
	// SnakeCase mirrors a server configured with crud.CaseSnake: example
	// payload keys render snake_case instead of the camelCase default.
	SnakeCase bool

	// Entities is an explicit allow-list of entity names (or tables) to
	// document. Nil documents Public entities only; IncludeGated
	// documents everything registered. Entities outside the included set
	// 404 on their reference URL and never appear in the nav — the site
	// must not leak the existence of gated schema.
	Entities     []string
	IncludeGated bool

	// AuthBasePath is where battery/auth is mounted (default "/auth").
	AuthBasePath string
	// HasAPITokens enables the token-minting walkthrough (the app wires
	// auth.TokensPlugin + TokenMiddleware).
	HasAPITokens bool

	// Policy, when set, gates every docs screen AND artifact download —
	// for internal/partner deployments. Nil = public site.
	Policy app.Policy
}

// site is the resolved, memoized runtime state shared by screens and
// artifact handlers.
type site struct {
	cfg Config

	manifestOnce sync.Once
	manifest     *sdk.Manifest
	drift        bool
	provenance   bool // true = manifest schema version unknown → "unknown provenance", not "stale"
}

// Mount registers the docs screens on the core-ui app and the artifact
// routes on the router. Call it during wiring, alongside (before or after)
// fwApp.Mount(host) — screens resolve per request, so ordering only affects
// llm.md indexing.
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
			WithTitle("API authentication — " + cfg.AppName).
			WithDescription("Bearer-token authentication for the " + cfg.AppName + " API"),
		app.NewScreen(base+"/errors", &errorsScreen{site: s}).
			WithTitle("API errors — " + cfg.AppName).
			WithDescription("Error envelope and status codes for the " + cfg.AppName + " API"),
		app.NewScreen(base+"/entities/:name", &entityScreen{site: s}).
			WithTitle("API reference — " + cfg.AppName).
			WithDescription("Entity API reference for " + cfg.AppName).
			WithPolicy(s.entityVisibilityPolicy()),
	}
	for _, sc := range screens {
		if cfg.Policy != nil {
			sc.WithPolicy(cfg.Policy)
		}
		coreApp.RegisterScreen(sc, nil)
	}

	// Mobile nav drawer for the SectionMenu rail.
	widget.MountBuilder(r, sectionMenuDrawer(s))

	r.Get(base+"/sdk/go.zip", s.artifactHandler("go", sdk.GoArtifact))
	r.Get(base+"/sdk/client.js", s.artifactHandler("js", sdk.JSArtifact))
	r.Get(base+"/sdk/client.d.ts", s.artifactHandler("js-types", sdk.JSTypesArtifact))
	r.Get(base+"/sdk/manifest.json", s.artifactHandler("", sdk.ManifestFile))
	return nil
}

// includedEntities returns the documented entity set, sorted by name. It
// walks the live registry per call so entities registered after Mount still
// appear.
func (s *site) includedEntities() []*entity.Entity {
	var out []*entity.Entity
	for _, e := range s.cfg.Registry.AllSorted() {
		if e == nil {
			continue
		}
		if e.Config.CRUD != nil && !*e.Config.CRUD {
			continue // no HTTP surface to document
		}
		if len(s.cfg.Entities) > 0 {
			if !nameMatches(e, s.cfg.Entities) {
				continue
			}
		} else if !s.cfg.IncludeGated && !e.Config.Public {
			continue
		}
		out = append(out, e)
	}
	return out
}

func nameMatches(e *entity.Entity, names []string) bool {
	for _, n := range names {
		n = strings.ToLower(strings.TrimSpace(n))
		if n == strings.ToLower(e.Config.Name) || n == strings.ToLower(e.Config.Table) {
			return true
		}
	}
	return false
}

// lookup resolves a reference-URL segment (entity table or name) within the
// included set only — excluded entities are indistinguishable from
// nonexistent ones.
func (s *site) lookup(segment string) (*entity.Entity, bool) {
	segment = strings.ToLower(segment)
	for _, e := range s.includedEntities() {
		if segment == strings.ToLower(e.Config.Table) || segment == strings.ToLower(e.Config.Name) {
			return e, true
		}
	}
	return nil, false
}

// entityVisibilityPolicy 404s reference URLs whose entity is not in the
// included set — a gated entity's page must be indistinguishable from a
// nonexistent one. It reads the segment from the request URL because
// policies run before route params are injected. With no request in
// context (SSG / direct RenderPage) it allows: static builds only
// enumerate included entities via StaticPaths, and the screen itself
// re-checks inclusion before rendering any schema.
func (s *site) entityVisibilityPolicy() app.Policy {
	prefix := s.cfg.BasePath + "/entities/"
	return app.PolicyFunc(func(ctx context.Context) app.Decision {
		r := app.RequestFromContext(ctx)
		if r == nil {
			return decide.Allow()
		}
		segment := strings.Trim(strings.TrimPrefix(r.URL.Path, prefix), "/")
		if _, ok := s.lookup(segment); !ok {
			return decide.Block(http.StatusNotFound, "not found")
		}
		return decide.Allow()
	})
}

// resolved returns the manifest (nil when absent/invalid) and whether the
// live schema drifted from it. Memoized: the registry is fixed after wiring
// and the artifacts are regenerated only via redeploy/re-run.
func (s *site) resolved() (m *sdk.Manifest, drift, unknownProvenance bool) {
	s.manifestOnce.Do(func() {
		if s.cfg.Artifacts == nil {
			return
		}
		mf, err := sdk.ReadManifest(s.cfg.Artifacts)
		if err != nil {
			// No manifest AND no artifacts = nothing generated yet — the
			// "not published" state, not a provenance problem.
			if !anyArtifactPresent(s.cfg.Artifacts) {
				return
			}
			slog.Warn("sdkdocs: artifacts present but manifest unreadable — treating downloads as unknown provenance", "err", err)
			s.provenance = true
			return
		}
		s.manifest = mf
		if mf.SchemaVersion != sdk.SchemaVersion {
			s.provenance = true
			return
		}
		live := sdk.SchemaHash(sdk.RegistryNamedConfigs(s.cfg.Registry, mf.Entities))
		if live != mf.SchemaHash {
			s.drift = true
			slog.Warn("sdkdocs: SDK artifacts were generated from an older schema — re-run `gofastr generate sdk`",
				"manifest", mf.SchemaHash, "live", live)
		}
	})
	return s.manifest, s.drift, s.provenance
}

// anyArtifactPresent reports whether any known artifact file exists in the
// dist FS (distinguishes "never generated" from "manifest lost").
func anyArtifactPresent(fsys fs.FS) bool {
	for _, name := range []string{sdk.GoArtifact, sdk.JSArtifact, sdk.JSTypesArtifact} {
		if f, err := fsys.Open(name); err == nil {
			f.Close()
			return true
		}
	}
	return false
}

// artifactHandler serves one dist file. key is the manifest artifact key
// ("" for the manifest itself).
func (s *site) artifactHandler(key, file string) http.Handler {
	contentType := map[string]string{
		sdk.GoArtifact:      "application/zip",
		sdk.JSArtifact:      "application/javascript; charset=utf-8",
		sdk.JSTypesArtifact: "text/plain; charset=utf-8",
		sdk.ManifestFile:    "application/json; charset=utf-8",
	}[file]

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.Policy != nil {
			d := s.cfg.Policy.Decide(app.WithRequest(r.Context(), r))
			switch d.Kind {
			case app.DecisionAllow:
			case app.DecisionRedirect:
				http.Redirect(w, r, d.URL, http.StatusSeeOther)
				return
			default:
				status := d.Status
				if status == 0 {
					status = http.StatusForbidden
				}
				http.Error(w, "forbidden", status)
				return
			}
		}
		if s.cfg.Artifacts == nil {
			http.Error(w, "SDK artifacts are not generated yet — run `gofastr generate sdk` and point sdkdocs.Config.Artifacts at gen/sdk/dist", http.StatusNotFound)
			return
		}
		data, err := fs.ReadFile(s.cfg.Artifacts, file)
		if err != nil {
			http.Error(w, "SDK artifact missing — re-run `gofastr generate sdk`", http.StatusNotFound)
			return
		}

		// ETag from the manifest's recorded hash when available (free
		// revalidation on a stable URL); fall back to no validator.
		m, _, _ := s.resolved()
		if m != nil && key != "" {
			if a, ok := m.Artifacts[key]; ok && a.SHA256 != "" {
				etag := `"` + a.SHA256 + `"`
				w.Header().Set("ETag", etag)
				if match := r.Header.Get("If-None-Match"); match != "" && strings.Contains(match, etag) {
					w.WriteHeader(http.StatusNotModified)
					return
				}
			}
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "no-cache")
		if file == sdk.GoArtifact {
			// The zip downloads; client.js/client.d.ts serve inline so
			// `import "<url>/client.js"` works straight off the app.
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
	})
}

// sanitizeFilename keeps Content-Disposition filenames to a safe charset.
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

// sortedNames returns the included entities' display names (for tests/log).
func (s *site) sortedNames() []string {
	var out []string
	for _, e := range s.includedEntities() {
		out = append(out, e.Config.Name)
	}
	sort.Strings(out)
	return out
}
