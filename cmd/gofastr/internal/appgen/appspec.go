package appgen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"image"
	"image/color"
	"image/png"
	"io"
	"maps"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/project"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/internal/dsnredact"
)

// appSpecHasEntityAccess reports whether any entity declares an `access:`
// block, i.e. its auto-CRUD API is permission-gated and therefore needs a
// RolePolicy installed for the signed-in user, or every write 403s.
func appSpecHasEntityAccess(bp appspec.AppSpec) bool {
	for _, e := range bp.Entities {
		if appspec.EntityDeclarationExposure(e).Access != nil {
			return true
		}
	}
	return false
}

// appSpecHasOwnerScopedEntity reports whether any entity declares an
// owner_field. When it does and an admin account is bootstrapped, the seed
// runs *as* that admin so demo rows belong to them and a fresh signup starts
// with an empty, owner-scoped workspace.
// appSpecLoginRoute returns the route of the screen hosting the login form,
// defaulting to "/login". Used to point the auth battery's form-error redirect
// at the login page (so a failed login lands back on the form, not raw JSON).
func appSpecLoginRoute(bp appspec.AppSpec) string {
	// Sections render nested children, and a login form can sit anywhere in
	// that tree; a flat scan misses it and falls back to "/login" — an
	// unregistered route when the app spec's sign-in screen lives elsewhere.
	// Same recursive walk appSpecNeedsResource uses.
	var hasLogin func([]appspec.AppSpecBlock) bool
	hasLogin = func(blocks []appspec.AppSpecBlock) bool {
		for _, b := range blocks {
			if isLoginFormBlock(b) {
				return true
			}
			if hasLogin(b.Children) {
				return true
			}
		}
		return false
	}
	for _, s := range bp.Screens {
		if hasLogin(s.Body) {
			return s.Route
		}
	}
	return "/login"
}

// appSpecAppHome returns the post-login landing route: the first app-layout
// screen, defaulting to "/". Used for the auth-aware header's "Dashboard" link
// and to bounce already-signed-in visitors off the login/signup screens.
func appSpecAppHome(bp appspec.AppSpec) string {
	for _, s := range bp.Screens {
		if s.Layout == "app" {
			return s.Route
		}
	}
	return "/"
}

// appSpecMarketingLink is one entry of the marketing chrome's canonical
// link table. The header nav and the footer columns both project out of it,
// so the two surfaces cannot drift apart again.
type appSpecMarketingLink struct {
	Label  string
	Route  string
	Nav    bool   // included in the header nav when its route is registered
	Column string // footer column title when registered; "" = not in the footer
}

// appSpecMarketingLinkTable is the single source of truth for the marketing
// chrome's links. A link is emitted only when a screen registers its route —
// the rule appSpecLoginRoute established for "/login". Before that, the
// chrome shipped four literal hrefs, so a one-screen marketing app spec
// generated a footer full of 404s (#312).
var appSpecMarketingLinkTable = []appSpecMarketingLink{
	{Label: "Pricing", Route: "/pricing", Nav: true, Column: "Product"},
	{Label: "About", Route: "/about", Nav: true, Column: "Company"},
	{Label: "Terms", Route: "/terms", Column: "Legal"},
	{Label: "Privacy", Route: "/privacy", Column: "Legal"},
}

// appSpecRegisteredMarketingLinks returns the canonical marketing links
// whose routes bp.Screens actually registers, in table order (which is also
// the footer's column order). Screens with path params mount at the
// colon-style route, so the same normalization appSpecScreenMountStmt uses
// applies here.
func appSpecRegisteredMarketingLinks(bp appspec.AppSpec) []appSpecMarketingLink {
	registered := make(map[string]bool, len(bp.Screens))
	for _, s := range bp.Screens {
		registered[appSpecScreenRoutePath(s.Route)] = true
	}
	var out []appSpecMarketingLink
	for _, l := range appSpecMarketingLinkTable {
		if registered[l.Route] {
			out = append(out, l)
		}
	}
	return out
}

// screenHasAuthForm reports whether a screen hosts a login or signup form, so
// the generator gates it with a guest policy (signed-in users are redirected
// to the app instead of seeing a sign-in form they don't need).
func screenHasAuthForm(s appspec.AppSpecScreen) bool {
	// Same tree as appSpecLoginRoute walks: a form nested in a section's
	// children still makes the screen guest-gated.
	var any func([]appspec.AppSpecBlock) bool
	any = func(blocks []appspec.AppSpecBlock) bool {
		for _, b := range blocks {
			if isLoginFormBlock(b) || isSignupFormBlock(b) || any(b.Children) {
				return true
			}
		}
		return false
	}
	return any(s.Body)
}

// appSpecHasAuthFormScreen reports whether any screen hosts an auth form.
func appSpecHasAuthFormScreen(bp appspec.AppSpec) bool {
	for _, s := range bp.Screens {
		if screenHasAuthForm(s) {
			return true
		}
	}
	return false
}

// appSpecNavHasRoles reports whether any nav item (at any depth) is
// role-restricted, so the generated app wires ui.SetRolesExtractor to filter
// the sidebar/drawer by the signed-in user's roles.
func appSpecNavHasRoles(items []appspec.AppSpecNavItem) bool {
	for _, it := range items {
		if it.Role != "" || appSpecNavHasRoles(it.Items) {
			return true
		}
	}
	return false
}

func appSpecHasOwnerScopedEntity(bp appspec.AppSpec) bool {
	for _, e := range bp.Entities {
		if appspec.EntityDeclarationScope(e).OwnerField != "" {
			return true
		}
	}
	return false
}

// appSpecExpandSeed appends Count auto-generated demo rows to each seed
// entity that declares one. Generation is deterministic (no runtime
// randomness), so regeneration is stable and the diff never churns. Enum
// columns get a weighted or deterministically-skewed distribution so demo data
// never reads as a flat 1/N-per-value round-robin.
func appSpecExpandSeed(bp appspec.AppSpec) appspec.AppSpec {
	entityMap := make(map[string]framework.EntityDeclaration, len(bp.Entities))
	for _, d := range bp.Entities {
		entityMap[d.Name] = d
	}
	for i := range bp.Seed {
		s := &bp.Seed[i]
		if s.Count <= 0 {
			continue
		}
		decl, ok := entityMap[s.Entity]
		if !ok {
			continue
		}
		s.Rows = append(s.Rows, appspec.AppSpecGenerateSeedRows(decl, s.Count, s.Weights)...)
	}
	return bp
}

func RenderAppSpecFiles(bp appspec.AppSpec) ([]GeneratedFile, error) {
	return renderAppSpec(bp, 0, 0, true)
}

// renderAppSpecFilesWithOrder is the additive variant: entity declaration
// orders start at entityOrderOffset and authored screen orders start at
// screenOrderOffset instead of 0, so --add entities/screens continue after
// the project's existing set.
func renderAppSpecFilesWithOrder(bp appspec.AppSpec, entityOrderOffset, screenOrderOffset int) ([]GeneratedFile, error) {
	return renderAppSpec(bp, entityOrderOffset, screenOrderOffset, true)
}

// RenderAppSpecCode renders the app without fetching webfonts: the render
// is read for its code (validate, pack's check) and never written, so it
// makes no network call.
func RenderAppSpecCode(bp appspec.AppSpec) ([]GeneratedFile, error) {
	return renderAppSpec(bp, 0, 0, false)
}

func renderAppSpec(bp appspec.AppSpec, entityOrderOffset, screenOrderOffset int, fonts bool) ([]GeneratedFile, error) {
	bp = appspec.AppSpecSynthesizeCRUDScreens(bp)
	bp = appSpecExpandSeed(bp)
	var files []GeneratedFile
	if bp.App.Module != "" {
		files = append(files, GeneratedFile{Name: "main.go", Content: RenderAppSpecMain(bp)})
		// The translator main.go installs, and the directory its
		// catalog embeds. The generator never writes a <tag>.json, so
		// a regenerate keeps every translation.
		files = append(files, GeneratedFile{Name: "i18n.go", Content: appSpecI18nGo})
		files = append(files, GeneratedFile{Name: "i18n_test.go", Content: appSpecI18nTestGo})
		files = append(files, GeneratedFile{Name: "locales/README.md", Content: strings.Replace(appSpecLocalesReadme, "{{pick}}", appSpecLocalesPick(bp), 1)})
	}
	if len(bp.Entities) > 0 {
		decls := make([]framework.EntityDeclaration, len(bp.Entities))
		copy(decls, bp.Entities)
		for i := range decls {
			decls[i].Endpoints = nil
		}
		entityFiles, err := renderGeneratedProjectWithOrder(decls, entityOrderOffset)
		if err != nil {
			return nil, err
		}
		endpointFiles, client, err := renderAppSpecEndpoints(bp, decls, entityFiles)
		if err != nil {
			return nil, err
		}
		for _, file := range entityFiles {
			if client != "" && filepath.ToSlash(file.Name) == "client/client.go" {
				file.Content = client
			}
			files = append(files, GeneratedFile{Name: filepath.Join("entities", file.Name), Content: file.Content})
		}
		if len(endpointFiles) > 0 {
			files = append(files, endpointFiles...)
			// An input type a stub declares meets the entities' own names
			// in one package.
			if err := refuseDuplicateDecls(slices.DeleteFunc(slices.Clone(files), func(f GeneratedFile) bool {
				return path.Dir(filepath.ToSlash(f.Name)) != "entities"
			})); err != nil {
				return nil, fmt.Errorf("app spec: %w", err)
			}
		}
	} else if len(bp.Endpoints) > 0 {
		return nil, fmt.Errorf("app spec: endpoints hang under entities, and the app spec declares none")
	} else if bp.App.Module != "" {
		// No entities yet, but main.go imports the entities package and
		// calls entities.RegisterAll unconditionally. Emit the empty seam
		// so the project compiles AND stays additive-ready: a later
		// `--add`/scaffold entity is a new file that self-registers, with
		// no edit to any owned file.
		files = append(files, GeneratedFile{Name: filepath.Join("entities", "register.go"), Content: RenderRegisterSeam()})
	}
	emitsApp := bp.App.Name != "" || bp.App.Module != "" || bp.App.DBDriver != "" || bp.App.DBURL != "" || bp.App.StaticDir != "" || bp.App.OutputDir != "" || appspec.AppSpecHasTheme(bp.App) || len(bp.Screens) > 0 || len(bp.Middleware) > 0 || len(bp.Plugins) > 0
	// Per-screen layout: the fixed screens_register.go seam + one file per
	// authored screen + one per-entity crud file (list/record/create screens).
	files = append(files, appSpecScreenFiles(bp, screenOrderOffset)...)
	if len(bp.Screens) == 0 && emitsApp {
		// Same additive-readiness for screens: app.go calls mountGenerated
		// unconditionally, so a screen-less app still ships the seam.
		files = append(files, GeneratedFile{Name: "screens_register.go", Content: appSpecScreensRegisterGo})
	}
	if bp.App.Admin.Enabled {
		// Always ship the admin wiring seam when the back-office is on, so a
		// later `gofastr generate app --add` or a hand-added admin_rbac.go can
		// wire RBAC into admin.Config without editing generated files.
		files = append(files, GeneratedFile{Name: "admin_register.go", Content: appSpecAdminRegisterGo})
	}
	if len(bp.Middleware) > 0 || len(bp.Plugins) > 0 || len(bp.Helpers) > 0 || len(bp.Seed) > 0 || len(bp.Hooks) > 0 {
		files = append(files, GeneratedFile{Name: "stubs.go", Content: RenderAppSpecStubs(bp)})
	}
	if emitsApp {
		files = append(files, GeneratedFile{Name: "app.go", Content: RenderAppSpecApp(bp)})
	}
	// Marketing chrome ships as the app's own packages (siteheader/,
	// sitefooter/), copied from the same canonical packages
	// `gofastr generate package` copies: plain html + framework parts,
	// an owned .style.css each, the _style.gen.go / _tokens.gen.go
	// committed beside them, and the tests — the app compiles, vets and
	// tests with no extra step. app.go imports them by module path,
	// which the module must therefore declare.
	if appSpecHasMarketing(bp) {
		if bp.App.Module == "" {
			return nil, fmt.Errorf("marketing screens need a Go module (set app.module, or generate inside a go.mod): the generated header/footer live in subpackages the app imports by module path")
		}
		chromeImport := chromePackageImport(bp)
		for _, pkg := range []string{"siteheader", "sitefooter"} {
			chrome, _, _, cerr := copyCanonicalPackage(pkg, chromeImport+"/"+pkg)
			if cerr != nil {
				return nil, cerr
			}
			for _, f := range chrome {
				files = append(files, GeneratedFile{Name: pkg + "/" + f.Name, Content: f.Content})
			}
		}
	}
	// The entityui seam ships beside app.go even with zero entities: the
	// appUI wiring in app.go references it, and a later `--add` entity
	// screen needs no edit to any owned file.
	if emitsApp {
		files = append(files, GeneratedFile{Name: "extensions.go", Content: appSpecExtensionsGo})
	}
	if appSpecNeedsAuthHooks(bp) {
		files = append(files, GeneratedFile{Name: "auth_hooks.go", Content: appSpecAuthHooksGo(bp)})
	}
	if bp.App.Module != "" && len(bp.Screens) > 0 {
		files = append(files, GeneratedFile{Name: "e2e_test.go", Content: renderAppSpecE2ETest(bp)})
		// The runtime axe gate doubles as the strict-mode coverage source:
		// its scans write .gofastr/axe-coverage.json, which the generated
		// app's uihost.WithStrict verifies on dev boot.
		files = append(files, GeneratedFile{Name: "axe_test.go", Content: renderAppSpecAxeTest(bp)})
	}
	// Secrets never land in committed source: the generated Go reads them
	// via env (JWT_SECRET, DATABASE_URL, ADMIN_SEED_PASSWORD) and the
	// generated .env, excluded by the generated .gitignore, carries the
	// app spec's values so the app runs out of the box.
	if env := RenderAppSpecEnv(bp); env != "" {
		// 0600: this file carries the session signing secret and the
		// database URL. See codegen.GeneratedFile.Mode.
		files = append(files, GeneratedFile{Name: ".env", Content: env, Mode: 0o600})
	}
	// Always ship the .gitignore: even a secret-less app writes local
	// build artifacts (.gofastr/, the axe-coverage manifest) that must
	// not land in source control.
	files = append(files, GeneratedFile{Name: ".gitignore", Content: appSpecGitignore})
	// Self-hosted webfonts: fetch the theme's families at generate time so a
	// named font WORKS in a fresh app with no manual step. An offline fetch
	// drops the file (generateFromAppSpec warns with the exact path); the
	// generated app boot-checks for it too, so no silent /fonts/*.woff2 404
	// path remains.
	if fonts {
		fontFiles, _ := appSpecFontAssets(bp)
		files = append(files, fontFiles...)
	}
	// PWA placeholder icons: generated deterministic PNGs so a fresh
	// app.pwa app spec is installable immediately. Replaceable; swap
	// the files under <static>/icons/ with real branding.
	files = append(files, appSpecPWAIconAssets(bp)...)
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	if err := assertAppSpecGoParses(files); err != nil {
		return nil, err
	}
	return files, nil
}

// assertAppSpecGoParses is the emitter-side backstop for the property that
// validateAppSpec enforces field by field: no app spec string escapes the Go
// literal, identifier, or comment it is emitted into. Every such field has an
// identifier or quoting guard at validate time, but relation names had none
// until the 2026-08-04 pass, and the way that gap presented was Go that does
// not parse. So check the whole emitted tree once, here, and refuse to hand
// back a broken file: the next missing guard fails the generate with a
// reportable message instead of silently writing attacker-shaped Go that the
// developer then builds and runs.
//
// This is deliberately stricter than formatGenerated, which documents the
// opposite policy (fall back to unformatted so a parse failure surfaces in the
// compiler). The two differ because the inputs differ: formatGenerated also
// serves `gofastr generate` over entity declarations the developer wrote in
// Go, where a parse failure is a generator bug worth inspecting. An app spec is
// transcribed text. The documented workflow has an agent authoring it from
// natural-language requirements (see appspec_emitter_injection_test.go), so
// on that path unparseable output is a security signal and fails closed.
//
// Parsing is per file, so it cannot see a name declared twice across the
// package: a helper named register_generated emits func RegisterGenerated
// in stubs.go beside the one in app.go, and the app then fails to compile
// after a generate that reported success. The parsed files go on to
// assertAppSpecDeclsUnique, which checks each package as a whole.
func assertAppSpecGoParses(files []GeneratedFile) error {
	parsed := make([]parsedGoFile, 0, len(files))
	for _, file := range files {
		if !strings.HasSuffix(file.Name, ".go") {
			continue
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, file.Name, file.Content, parser.SkipObjectResolution)
		if err != nil {
			return fmt.Errorf("app spec: generated %s does not parse (%w): an app spec value escaped the Go literal it was emitted into. Please report this with the app spec that produced it", file.Name, err)
		}
		parsed = append(parsed, parsedGoFile{name: file.Name, file: f})
	}
	return assertAppSpecDeclsUnique(parsed)
}

type parsedGoFile struct {
	name string
	file *ast.File
}

// assertAppSpecDeclsUnique refuses an emitted package that declares one
// package-level name twice (two funcs, a func and a type, a method twice on
// one receiver), or declares a name a file of the same package imports: Go
// keeps file-scope import names and package-level names in one space. The
// package is the directory plus the package clause, so an external test
// package (package main_test) is checked apart from the one it tests. The
// error names both files, so an app spec author can rename the stub.
func assertAppSpecDeclsUnique(files []parsedGoFile) error {
	type pkgKey struct{ dir, name string }
	type site struct{ file, what string }
	decls := map[pkgKey]map[string]site{}
	imports := map[pkgKey]map[string]string{} // import name -> first file importing it
	var order []pkgKey
	for _, pf := range files {
		key := pkgKey{dir: path.Dir(filepath.ToSlash(pf.name)), name: pf.file.Name.Name}
		if decls[key] == nil {
			decls[key] = map[string]site{}
			imports[key] = map[string]string{}
			order = append(order, key)
		}
		claim := func(name, what string) error {
			if name == "_" || (name == "init" && what == "func") {
				return nil
			}
			if prev, dup := decls[key][name]; dup {
				return fmt.Errorf("app spec: generated %s declares %s %s, which %s already declares as a %s; rename the app spec item that produces one of them", pf.name, what, name, prev.file, prev.what)
			}
			decls[key][name] = site{file: pf.name, what: what}
			return nil
		}
		for _, imp := range pf.file.Imports {
			if name := goImportName(imp); name != "" {
				if _, seen := imports[key][name]; !seen {
					imports[key][name] = pf.name
				}
			}
		}
		for _, d := range pf.file.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				name, what := d.Name.Name, "func"
				if d.Recv != nil && len(d.Recv.List) > 0 {
					name, what = goRecvTypeName(d.Recv.List[0].Type)+"."+name, "method"
				}
				if err := claim(name, what); err != nil {
					return err
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch spec := spec.(type) {
					case *ast.TypeSpec:
						if err := claim(spec.Name.Name, "type"); err != nil {
							return err
						}
					case *ast.ValueSpec:
						what := "var"
						if d.Tok == token.CONST {
							what = "const"
						}
						for _, n := range spec.Names {
							if err := claim(n.Name, what); err != nil {
								return err
							}
						}
					}
				}
			}
		}
	}
	for _, key := range order {
		for _, name := range slices.Sorted(maps.Keys(imports[key])) {
			if d, dup := decls[key][name]; dup {
				return fmt.Errorf("app spec: generated %s declares %s %s, which %s imports as a package name; rename the app spec item that produces it", d.file, d.what, name, imports[key][name])
			}
		}
	}
	return nil
}

// goImportName is the file-scope name an import binds: its alias, or the
// last path element (skipping a /vN major-version suffix). Blank and dot
// imports bind no name. A path whose package clause differs from its last
// element is read by that element, which can only miss a collision.
func goImportName(imp *ast.ImportSpec) string {
	if imp.Name != nil {
		if imp.Name.Name == "_" || imp.Name.Name == "." {
			return ""
		}
		return imp.Name.Name
	}
	p, err := strconv.Unquote(imp.Path.Value)
	if err != nil {
		return ""
	}
	elems := strings.Split(p, "/")
	last := elems[len(elems)-1]
	if len(elems) > 1 && len(last) > 1 && last[0] == 'v' && strings.Trim(last[1:], "0123456789") == "" {
		last = elems[len(elems)-2]
	}
	return last
}

// goRecvTypeName is a method receiver's base type name: T for T, *T, T[K]
// and *T[K].
func goRecvTypeName(e ast.Expr) string {
	for {
		switch t := e.(type) {
		case *ast.StarExpr:
			e = t.X
		case *ast.IndexExpr:
			e = t.X
		case *ast.IndexListExpr:
			e = t.X
		case *ast.ParenExpr:
			e = t.X
		case *ast.Ident:
			return t.Name
		default:
			return ""
		}
	}
}

// appSpecGitignore keeps the generated .env (and local overrides), the dev
// SQLite database (with its WAL/SHM files) and the build output out of source
// control. TestAppSpecNeverInlinesSecrets pins the contract.
const appSpecGitignore = `# Generated by gofastr.
.env
.env.local
# Local build artifacts (axe-coverage manifest, dev state). Regenerated
# by go test / the gofastr CLI; never ship or commit them.
.gofastr/
# gofastr build output.
bin/
# The default SQLite database and the WAL/SHM files beside it.
*.db
*.db-shm
*.db-wal
`

// RenderAppSpecEnv collects every app spec value that must not be
// committed as a Go literal. Empty when the app spec holds no secrets;
// then no .env is emitted at all.
func RenderAppSpecEnv(bp appspec.AppSpec) string {
	var b strings.Builder
	writeSecret := func(key, value string) {
		// A value that IS a reference to this same key cannot be written
		// back as the key's value. `kiln freeze` emits ${JWT_SECRET} into
		// the app spec precisely so the real secret stays out of the
		// committed file, and rendering that into .env produced
		// JWT_SECRET=${JWT_SECRET} -- a self-reference dotenv returns
		// verbatim, so the app booted signing session JWTs with the
		// literal string printed in the committed gofastr.yml. Nothing
		// fails closed on it either: battery/auth rejects only an EMPTY
		// secret, and the literal is non-empty.
		//
		// Emit the key as a commented placeholder instead. dotenv then
		// sets nothing, a real exported value still wins (Apply never
		// overrides the process env), and an absent one leaves the secret
		// empty, which IS the condition auth refuses to boot on.
		if isSelfEnvRef(key, value) {
			b.WriteString("# " + key + " must be supplied by the environment;\n" +
				"# the app spec carries a ${" + key + "} reference, not a value.\n" +
				"# " + key + "=\n")
			return
		}
		b.WriteString(key + "=" + envQuote(value) + "\n")
	}
	if bp.App.Auth.Enabled && bp.App.Auth.JWTSecret != "" {
		writeSecret("JWT_SECRET", bp.App.Auth.JWTSecret)
	}
	if dsnredact.HasSecret(bp.App.DBURL) {
		writeSecret("DATABASE_URL", bp.App.DBURL)
	}
	if bp.App.Auth.Enabled && bp.App.Admin.SeedEmail != "" && bp.App.Admin.SeedPassword != "" {
		writeSecret("ADMIN_SEED_PASSWORD", bp.App.Admin.SeedPassword) // not-a-secret: renders the app spec's value into the generated (gitignored) .env
	}
	if b.Len() == 0 {
		return ""
	}
	return "# Generated by gofastr from your app spec. Keep out of source\n" +
		"# control (the generated .gitignore already excludes it).\n" +
		"# Regenerating with --force rewrites this file from the app spec.\n" +
		b.String()
}

// isSelfEnvRef reports whether value is exactly "${key}" -- a reference
// naming the very variable it would be assigned to.
func isSelfEnvRef(key, value string) bool {
	return strings.TrimSpace(value) == "${"+key+"}"
}

// envQuote renders a value for the generated .env so both readers of
// that file, core/dotenv (the generated app's loader and its e2e test)
// and `gofastr pack`, read back exactly the app spec's value. Bare
// values survive verbatim unless they lead with a quote character or
// carry edge whitespace; those are double-quoted with backslash escapes
// ('$' included, so no ${VAR} expansion fires on the way back in).
func envQuote(v string) string {
	if v == "" || (v == strings.TrimSpace(v) && v[0] != '"' && v[0] != '\'' && !strings.ContainsAny(v, "\n\r")) {
		return v
	}
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(v); i++ {
		switch c := v[i]; c {
		case '\\', '"', '$':
			b.WriteByte('\\')
			b.WriteByte(c)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// redactDSN strips the password from a DSN so the remainder can appear in
// committed source (host/db name are configuration, not secrets). The
// rules live in internal/dsnredact — the one canonical redactor, lifted
// from here and proven against the round-5 probes — so this is a
// delegation, not a second spelling that can drift.
func redactDSN(dsn string) string {
	return dsnredact.Redact(dsn)
}

// appSpecAuthPolicyFunc is the generated authPolicy. It composes
// battery/auth's policies rather than reading the user's roles itself: a
// hand-written role loop never asked an access Decider, so a host's
// DeciderMiddleware deny did not bind generated role-gated screens.
func appSpecAuthPolicyFunc() string {
	return `// authPolicy gates a screen: redirect an anonymous visitor to the login
// page (with ?next=) and 403 a signed-in user missing the required role.
// The role check is auth.RolePolicy, so an access Decider binds it.
func authPolicy(loginPath, role string) app.Policy {
	signedIn := auth.SessionPolicy(auth.WithRedirect(loginPath))
	if role == "" {
		return signedIn
	}
	hasRole := auth.RolePolicy(auth.Roles(role))
	return app.PolicyFunc(func(ctx context.Context) app.Decision {
		if d := signedIn.Decide(ctx); d.Kind != app.DecisionAllow {
			return d
		}
		return hasRole.Decide(ctx)
	})
}

`
}

// appSpecE2EScreenRoutes splits the app spec's STATIC screen routes (those
// without a `{param}`) into public (anonymous-renderable) and gated (auth) sets.
// Dynamic routes are exercised by the CRUD lifecycle, which has a real id.
func appSpecE2EScreenRoutes(bp appspec.AppSpec) (public, gated []string) {
	for _, s := range bp.Screens {
		route := appSpecScreenRoutePath(s.Route)
		if strings.ContainsAny(route, "{:") {
			continue
		}
		if s.Access.Auth {
			gated = append(gated, route)
		} else {
			public = append(public, route)
		}
	}
	return public, gated
}

// appSpecSeedAdminRoles are the roles the bootstrap admin is created with:
// the admin role (default "admin") and "user".
func appSpecSeedAdminRoles(bp appspec.AppSpec) []string {
	role := bp.App.Admin.Role
	if role == "" {
		role = "admin"
	}
	return []string{role, "user"}
}

// appSpecE2ERoleSplit splits the static gated routes into those the seeded
// admin can open and those whose screen requires a role it does not hold.
// authPolicy's RolePolicy matches roles exactly, so the second set answers
// the admin 403, and the e2e expects that instead of a 200.
func appSpecE2ERoleSplit(bp appspec.AppSpec, gated []string) (open, locked []string) {
	held := appSpecSeedAdminRoles(bp)
	lockedRoute := map[string]bool{}
	for _, s := range bp.Screens {
		if s.Access.Auth && s.Access.Role != "" && !slices.Contains(held, s.Access.Role) {
			lockedRoute[appSpecScreenRoutePath(s.Route)] = true
		}
	}
	for _, r := range gated {
		if lockedRoute[r] {
			locked = append(locked, r)
		} else {
			open = append(open, r)
		}
	}
	return open, locked
}

// appSpecCRUDTarget is the entity the generated e2e exercises with a full
// create→read→update→delete lifecycle.
type appSpecCRUDTarget struct {
	entity      string
	apiPath     string   // /api/<table>
	newRoute    string   // /app/<entity>/create
	detailBase  string   // /app/<entity>  (detail = detailBase + "/" + id), "" if no detail screen
	createJSON  string   // a valid create payload
	updateJSON  string   // a payload mutating one field
	probe       string   // a value present in the created record (to find in record HTML)
	accessGated bool     // entity is access- or owner-scoped → anonymous writes must be refused
	keys        []string // wire names of the key columns; nil for the generated id
}

// appSpecE2EWritableTarget picks an entity to lifecycle-test: one whose list
// is `create: true` (so a /create form exists) and whose required fields are all
// simple scalars (no required relation), so a valid body can be synthesized.
func appSpecE2EWritableTarget(bp appspec.AppSpec) (appSpecCRUDTarget, bool) {
	entityMap := make(map[string]framework.EntityDeclaration, len(bp.Entities))
	for _, d := range bp.Entities {
		entityMap[d.Name] = d
	}
	createOf := map[string]string{}
	detailOf := map[string]string{}
	for _, s := range bp.Screens {
		for _, b := range appspec.AppSpecBlocksDeep(s.Body) {
			e := strings.Trim(b.Entity, "/")
			if appspec.IsEntityListBlock(b) && b.Create {
				createOf[e] = appspec.AppSpecEntityListScreen(bp, e).Route
			}
			if appspec.IsEntityDetailBlock(b) {
				detailOf[e] = s.Route
			}
		}
	}
	names := make([]string, 0, len(createOf))
	for e := range createOf {
		names = append(names, e)
	}
	sort.Strings(names)
	apiBase := appSpecAPIBase(bp.App.APIPrefix)
	for _, e := range names {
		decl, ok := entityMap[e]
		if !ok || appSpecUnfillableRelation(decl) {
			continue
		}
		createJSON, updateJSON, probe := appSpecE2ECreateBody(decl)
		t := appSpecCRUDTarget{
			entity: e, apiPath: appSpecEntityAPIPath(apiBase, decl),
			newRoute:    strings.TrimRight(createOf[e], "/") + "/create",
			createJSON:  createJSON,
			updateJSON:  updateJSON,
			probe:       probe,
			accessGated: appspec.EntityDeclarationExposure(decl).Access != nil || appspec.EntityDeclarationScope(decl).OwnerField != "",
		}
		if !appspec.DeclKeyIsID(decl) {
			for _, k := range decl.PrimaryKey {
				t.keys = append(t.keys, appspec.WireKey(k))
			}
		}
		if dr, ok := detailOf[e]; ok {
			r := dr
			if i := strings.Index(r, "/{"); i >= 0 {
				r = r[:i]
			}
			t.detailBase = r
		}
		return t, true
	}
	return appSpecCRUDTarget{}, false
}

// appSpecUnfillableRelation reports a relation the e2e create body cannot
// fill (it sends no relation) and the record cannot do without: a
// required one, or one in the declared key, which the id is made of.
func appSpecUnfillableRelation(decl framework.EntityDeclaration) bool {
	keyCols := appspec.AppSpecKeyColNames(decl)
	for _, f := range decl.Fields {
		if (f.Required || slices.Contains(keyCols, f.Name)) && strings.EqualFold(strings.TrimSpace(f.Type), "relation") {
			return true
		}
	}
	return false
}

// appSpecE2ECreateBody synthesizes a valid create payload (JSON literal) for
// decl, plus an update payload mutating one string field, and a probe value that
// the created record will contain (for asserting record HTML). To stay valid
// across any schema it sends only the entity's REQUIRED fields (best-effort per
// type). Optional fields with exotic validators (json, image, file) are left
// out; the screens still exercise all-field rendering.
func appSpecE2ECreateBody(decl framework.EntityDeclaration) (createJSON, updateJSON, probe string) {
	var parts []string
	probeField := ""
	for _, f := range decl.Fields {
		// A declared key's columns are sent whether required or not:
		// the record has no id without them. The update never changes
		// one, since that would move the record off the path it names.
		isKey := !appspec.DeclKeyIsID(decl) && slices.Contains(decl.PrimaryKey, f.Name)
		if appspec.AppSpecFieldSystem(f.Name) || f.Hidden || (!f.Required && !isKey) {
			continue
		}
		var v string
		switch strings.ToLower(f.Type) {
		case "enum":
			if len(f.Values) == 0 {
				continue
			}
			v = `"` + f.Values[0] + `"`
		case "relation":
			// The create sends no relation: one the record needs (a
			// required or key relation) disqualifies the entity upstream.
			continue
		case "uuid":
			// The server fills only an auto_generate uuid: any other is
			// sent, one value per field.
			if appspec.FieldAutoGenerated(f) {
				continue
			}
			v = fmt.Sprintf(`"00000000-0000-4000-8000-%012d"`, len(parts)+1)
		case "bool", "boolean":
			v = "true"
		case "int", "integer":
			v = "1"
		case "float":
			v = "1"
		case "decimal":
			v = `"1"` // decimal is validated as a string
		case "date":
			v = `"2025-01-01"`
		case "timestamp", "datetime":
			v = `"2025-01-01T00:00:00Z"`
		case "json":
			v = "[]"
		case "image", "file", "blob", "binary":
			v = `"https://e2e.test/x.png"`
		default: // string, text, email, ...
			if strings.EqualFold(f.Type, "email") || strings.Contains(f.Name, "email") {
				v = `"e2e-` + f.Name + `@example.com"`
			} else {
				val := "e2e-" + f.Name
				v = `"` + val + `"`
				if probeField == "" && !isKey {
					probeField, probe = f.Name, val
				}
			}
		}
		parts = append(parts, `"`+f.Name+`": `+v)
	}
	createJSON = "{" + strings.Join(parts, ", ") + "}"
	if probeField != "" {
		updateJSON = `{"` + probeField + `": "e2e-updated"}`
	} else {
		updateJSON = createJSON
	}
	return createJSON, updateJSON, probe
}

// renderAppSpecE2ETest emits an end-to-end test (package main) that builds and
// boots the generated binary, then asserts the real flow: every static public +
// gated screen renders (gated ones redirect anonymous callers and render once
// authed), a full create→read→update→delete lifecycle runs against a writable
// entity through its CRUD API + screens, and anonymous writes to an
// access-gated entity are refused.
// renderAppSpecAxeTest emits the generated app's runtime accessibility
// gate: boot the built binary, discover public pages from /sitemap.xml,
// scan them with an ANONYMOUS browser, then scan access-gated screens
// with a separately-authenticated browser. One shared login state
// would make guest-only pages (login) redirect and record the wrong
// screen. Gated DYNAMIC patterns classify concrete sitemap pages (a
// StaticPaths-bearing "/orders/:id" puts "/orders/42" in the sitemap;
// it must go through the authenticated pass, not bounce anonymously).
// Every scan asserts it landed on the intended route before running
// axe, so an unexpected redirect fails loudly instead of silently
// covering a different page. Scans record the axe-coverage manifest
// uihost strict mode (which generated apps ship with) verifies on dev
// boot, so this test IS the strict axe gate. It shares the
// e2eFreeAddr / e2eWaitReady / e2ePostgresDSN helpers emitted in
// e2e_test.go.
func renderAppSpecAxeTest(bp appspec.AppSpec) string {
	_, gatedAll := appSpecE2EScreenRoutes(bp)
	// Static gated routes are scanned directly; dynamic gated PATTERNS
	// classify concrete sitemap pages into the authenticated pass.
	var gated, gatedPatterns []string
	for _, r := range gatedAll {
		if isAppSpecDynamicRoute(r) {
			gatedPatterns = append(gatedPatterns, r)
		} else {
			gated = append(gated, r)
		}
	}
	login := bp.App.Auth.Enabled && bp.App.Admin.SeedEmail != "" && bp.App.Admin.SeedPassword != ""
	authBase := appSpecAuthBase(bp)
	dbDriver := strings.ToLower(strings.TrimSpace(bp.App.DBDriver))
	isPostgres := dbDriver == "postgres" || dbDriver == "postgresql"

	goSlice := func(ss []string) string {
		if len(ss) == 0 {
			return "[]string{}"
		}
		q := make([]string, len(ss))
		for i, v := range ss {
			q[i] = fmt.Sprintf("%q", v)
		}
		return "[]string{" + strings.Join(q, ", ") + "}"
	}

	var b strings.Builder
	b.WriteString("package main\n\n")
	b.WriteString("import (\n")
	// net/http rides the login arm (http.Client for the cookie-jar
	// login): the sitemap fetch goes through e2eClient (declared in
	// e2e_test.go), so a login-less axe file has no other http use.
	imports := []string{"context", "io", "net/url", "os", "os/exec", "path/filepath", "regexp", "runtime", "strings", "testing", "time"}
	if login {
		imports = append(imports, "net/http")
	}
	for _, imp := range imports {
		b.WriteString("\t\"" + imp + "\"\n")
	}
	if login {
		b.WriteString("\t\"net/http/cookiejar\"\n")
	}
	b.WriteString("\n")
	if login {
		b.WriteString("\t\"github.com/chromedp/cdproto/network\"\n")
	}
	b.WriteString("\t\"github.com/chromedp/chromedp\"\n\n")
	if login {
		b.WriteString("\t\"github.com/DonaldMurillo/gofastr/core/dotenv\"\n")
		b.WriteString("\t\"github.com/DonaldMurillo/gofastr/framework\"\n")
	}
	b.WriteString("\t\"github.com/DonaldMurillo/gofastr/framework/testkit/axetest\"\n")
	b.WriteString(")\n\n")

	b.WriteString("// axeExtraPages lists concrete URLs the sitemap cannot discover:\n")
	b.WriteString("// instances of dynamic routes (\"/orders/42\"). Owned: append entries as\n")
	b.WriteString("// you add dynamic screens; they are scanned with the authenticated\n")
	b.WriteString("// browser when one exists, so gated instances work too.\n")
	b.WriteString("var axeExtraPages []string\n\n")
	b.WriteString("// axeGatedPages are the access-gated static screen routes; they are\n")
	b.WriteString("// scanned with an authenticated browser: an anonymous scan would\n")
	b.WriteString("// only ever cover the login redirect, never the real screen.\n")
	b.WriteString("var axeGatedPages = " + goSlice(gated) + "\n\n")
	b.WriteString("// axeGatedPatterns are the access-gated DYNAMIC route patterns; any\n")
	b.WriteString("// concrete sitemap page matching one (via StaticPaths expansion) is\n")
	b.WriteString("// routed through the authenticated pass instead of bouncing.\n")
	b.WriteString("var axeGatedPatterns = " + goSlice(gatedPatterns) + "\n\n")

	b.WriteString("// TestAxeEveryScreen is the runtime accessibility gate AND the strict-mode\n")
	b.WriteString("// axe-coverage source: every page it scans is recorded into the\n")
	b.WriteString("// axe-coverage manifest, which uihost.WithStrict checks on dev boot.\n")
	b.WriteString("// Public pages come from /sitemap.xml, so a new static screen is picked\n")
	b.WriteString("// up automatically on the next run; dynamic screens need StaticPaths\n")
	b.WriteString("// (sitemap + strict demand) or an entry in axeExtraPages.\n")
	b.WriteString("func TestAxeEveryScreen(t *testing.T) {\n")
	b.WriteString("\t// allow-skip: chromedp suite, boots the app + headless Chrome; runs in the full (non-short) pass.\n")
	b.WriteString("\tif testing.Short() { t.Skip(\"boots the app + headless Chrome\") }\n")
	if login {
		b.WriteString("\t_ = dotenv.LoadAndApply(framework.DefaultDotEnvPaths()...)\n")
		b.WriteString("\tadminPass := os.Getenv(\"ADMIN_SEED_PASSWORD\")\n")
		b.WriteString("\tif adminPass == \"\" {\n")
		b.WriteString("\t\tadminPass = \"axe-seed-admin-pw\"\n")
		b.WriteString("\t\tt.Setenv(\"ADMIN_SEED_PASSWORD\", adminPass)\n")
		b.WriteString("\t}\n")
	}
	b.WriteString("\tdir := t.TempDir()\n")
	b.WriteString("\tbin := filepath.Join(dir, \"app\")\n")
	b.WriteString("\tif runtime.GOOS == \"windows\" {\n\t\tbin += \".exe\"\n\t}\n")
	b.WriteString("\tbuild := exec.Command(\"go\", \"build\", \"-o\", bin, \".\")\n")
	b.WriteString("\tbuild.Stderr = os.Stderr\n")
	b.WriteString("\tif err := build.Run(); err != nil { t.Fatalf(\"build: %v\", err) }\n")
	b.WriteString("\taddr := e2eFreeAddr(t)\n")
	b.WriteString("\tsrv := exec.Command(bin)\n")
	b.WriteString("\tsrv.Dir = dir\n")
	if isPostgres {
		b.WriteString("\tdbURL := e2ePostgresDSN(t)\n")
		b.WriteString("\tsrv.Env = append(os.Environ(), \"PORT=\"+addr, \"DATABASE_URL=\"+dbURL, \"DB_DRIVER=postgres\")\n")
	} else {
		b.WriteString("\tsrv.Env = append(os.Environ(), \"PORT=\"+addr, \"DATABASE_URL=file:\"+filepath.Join(dir, \"axe.db\"))\n")
	}
	b.WriteString("\te2eCaptureServerLog(t, srv, dir)\n")
	b.WriteString("\tif err := srv.Start(); err != nil { t.Fatalf(\"start: %v\", err) }\n")
	b.WriteString("\tt.Cleanup(func() { _ = srv.Process.Kill(); _, _ = srv.Process.Wait() })\n")
	b.WriteString("\tbase := \"http://\" + addr\n")
	b.WriteString("\te2eWaitReady(t, base)\n\n")
	b.WriteString("\tgated := map[string]bool{}\n")
	b.WriteString("\tfor _, p := range axeGatedPages {\n\t\tgated[p] = true\n\t}\n\n")
	b.WriteString("\t// Anonymous pass: every public (and guest-only) page. Pages that\n")
	b.WriteString("\t// are gated, directly or by matching a gated dynamic pattern,\n")
	b.WriteString("\t// are queued for the authenticated pass instead.\n")
	b.WriteString("\tvar needsAuth []string\n")
	b.WriteString("\tanon := axetest.NewBrowser(t)\n")
	b.WriteString("\tfor _, page := range axePagesFromSitemap(t, base) {\n")
	b.WriteString("\t\tif gated[page] || axeMatchesGatedPattern(page) {\n")
	b.WriteString("\t\t\tneedsAuth = append(needsAuth, page)\n")
	b.WriteString("\t\t\tcontinue\n")
	b.WriteString("\t\t}\n")
	b.WriteString("\t\tfor _, scheme := range axetest.Schemes {\n")
	b.WriteString("\t\t\taxeScanOne(t, anon, base, page, scheme)\n")
	b.WriteString("\t\t}\n")
	b.WriteString("\t}\n\n")
	if login {
		b.WriteString("\t// Authenticated pass: gated screens + owned extras, in a SEPARATE\n")
		b.WriteString("\t// browser so the anonymous pass above stays honest.\n")
		b.WriteString("\tfor _, p := range axeGatedPages {\n")
		b.WriteString("\t\tneedsAuth = append(needsAuth, p) // belt & braces when excluded from the sitemap\n")
		b.WriteString("\t}\n")
		b.WriteString("\tneedsAuth = append(needsAuth, axeExtraPages...)\n")
		b.WriteString("\tif len(needsAuth) > 0 {\n")
		b.WriteString("\t\tauthed := axetest.NewBrowser(t)\n")
		b.WriteString(fmt.Sprintf("\t\taxeLogin(t, authed, base, %q, adminPass)\n", bp.App.Admin.SeedEmail))
		b.WriteString("\t\tseen := map[string]bool{}\n")
		b.WriteString("\t\tfor _, page := range needsAuth {\n")
		b.WriteString("\t\t\tif seen[page] {\n\t\t\t\tcontinue\n\t\t\t}\n")
		b.WriteString("\t\t\tseen[page] = true\n")
		b.WriteString("\t\t\tfor _, scheme := range axetest.Schemes {\n")
		b.WriteString("\t\t\t\taxeScanOne(t, authed, base, page, scheme)\n")
		b.WriteString("\t\t\t}\n")
		b.WriteString("\t\t}\n")
		b.WriteString("\t}\n")
	} else {
		b.WriteString("\tneedsAuth = append(needsAuth, axeGatedPages...)\n")
		b.WriteString("\tif len(needsAuth) > 0 {\n")
		b.WriteString("\t\tt.Errorf(\"gated screens %v cannot be scanned: no seeded admin. Set app.admin.seed_email/seed_password (or extend this test with your own login) so the gate can cover them; unscanned gated screens fail strict dev boot\", needsAuth)\n")
		b.WriteString("\t}\n")
		b.WriteString("\tfor _, page := range axeExtraPages {\n")
		b.WriteString("\t\tfor _, scheme := range axetest.Schemes {\n")
		b.WriteString("\t\t\taxeScanOne(t, anon, base, page, scheme)\n")
		b.WriteString("\t\t}\n")
		b.WriteString("\t}\n")
	}
	b.WriteString("}\n\n")

	b.WriteString("// axeMatchesGatedPattern reports whether a concrete page path matches\n")
	b.WriteString("// one of the gated dynamic route patterns (segment-wise; \":\" and \"{\"\n")
	b.WriteString("// segments are wildcards).\n")
	b.WriteString("func axeMatchesGatedPattern(page string) bool {\n")
	b.WriteString("\tfor _, pattern := range axeGatedPatterns {\n")
	b.WriteString("\t\tps := strings.Split(pattern, \"/\")\n")
	b.WriteString("\t\tgs := strings.Split(page, \"/\")\n")
	b.WriteString("\t\tif len(ps) != len(gs) {\n\t\t\tcontinue\n\t\t}\n")
	b.WriteString("\t\tmatch := true\n")
	b.WriteString("\t\tfor i := range ps {\n")
	b.WriteString("\t\t\tif strings.HasPrefix(ps[i], \":\") || strings.HasPrefix(ps[i], \"{\") {\n\t\t\t\tcontinue\n\t\t\t}\n")
	b.WriteString("\t\t\tif ps[i] != gs[i] {\n\t\t\t\tmatch = false\n\t\t\t\tbreak\n\t\t\t}\n")
	b.WriteString("\t\t}\n")
	b.WriteString("\t\tif match {\n\t\t\treturn true\n\t\t}\n")
	b.WriteString("\t}\n")
	b.WriteString("\treturn false\n")
	b.WriteString("}\n\n")

	b.WriteString("func axeScanOne(t *testing.T, browser context.Context, base, page, scheme string) {\n")
	b.WriteString("\tt.Helper()\n")
	b.WriteString("\ttab, done := axetest.NewTab(t, browser)\n")
	b.WriteString("\tdefer done()\n")
	b.WriteString("\tvar loc string\n")
	b.WriteString("\tif err := chromedp.Run(tab,\n")
	b.WriteString("\t\tchromedp.Navigate(base+page),\n")
	b.WriteString("\t\tchromedp.Sleep(400*time.Millisecond), // hydration settle\n")
	b.WriteString("\t\tchromedp.Location(&loc),\n")
	b.WriteString("\t); err != nil {\n\t\tt.Fatalf(\"navigate %s (%s): %v\", page, scheme, err)\n\t}\n")
	b.WriteString("\t// A redirect means axe would audit, and the coverage manifest would\n")
	b.WriteString("\t// record, a DIFFERENT screen. Fail loudly instead of silently\n")
	b.WriteString("\t// covering the wrong page: the page either needs the authenticated\n")
	b.WriteString("\t// pass (axeGatedPages / axeGatedPatterns) or the route changed.\n")
	b.WriteString("\tif u, err := url.Parse(loc); err != nil || u.Path != page {\n")
	b.WriteString("\t\tt.Errorf(\"%s redirected to %s: page not scanned\", page, loc)\n")
	b.WriteString("\t\treturn\n")
	b.WriteString("\t}\n")
	b.WriteString("\tif err := chromedp.Run(tab, axetest.Prepare(scheme)); err != nil {\n")
	b.WriteString("\t\tt.Fatalf(\"prepare %s (%s): %v\", page, scheme, err)\n\t}\n")
	b.WriteString("\tviolations, err := axetest.Scan(tab, scheme, nil)\n")
	b.WriteString("\tif err != nil {\n\t\tt.Fatalf(\"axe scan %s (%s): %v\", page, scheme, err)\n\t}\n")
	b.WriteString("\tfor _, v := range violations {\n")
	b.WriteString("\t\tt.Errorf(\"%s (%s scheme): [%s] %s: %s (%s)\", page, scheme, v.Impact, v.ID, v.Help, v.HelpURL)\n")
	b.WriteString("\t}\n")
	b.WriteString("}\n\n")

	b.WriteString("// axePagesFromSitemap derives the public scan list from the app's own\n")
	b.WriteString("// sitemap, the same registry the router serves, so a new static\n")
	b.WriteString("// screen can never drift out of the gate.\n")
	b.WriteString("func axePagesFromSitemap(t *testing.T, base string) []string {\n")
	b.WriteString("\tt.Helper()\n")
	b.WriteString("\tresp, err := e2eClient.Get(base + \"/sitemap.xml\")\n")
	b.WriteString("\tif err != nil {\n\t\tt.Fatalf(\"sitemap: %v\", err)\n\t}\n")
	b.WriteString("\tdefer resp.Body.Close()\n")
	b.WriteString("\tbody, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))\n")
	b.WriteString("\tif err != nil {\n\t\tt.Fatalf(\"sitemap read: %v\", err)\n\t}\n")
	b.WriteString("\tlocs := regexp.MustCompile(`<loc>([^<]+)</loc>`).FindAllStringSubmatch(string(body), -1)\n")
	b.WriteString("\tvar pages []string\n")
	b.WriteString("\tseen := map[string]bool{}\n")
	b.WriteString("\tfor _, m := range locs {\n")
	b.WriteString("\t\tu, err := url.Parse(strings.TrimSpace(m[1]))\n")
	b.WriteString("\t\tif err != nil {\n\t\t\tcontinue\n\t\t}\n")
	b.WriteString("\t\tp := u.Path\n")
	b.WriteString("\t\tif p == \"\" {\n\t\t\tp = \"/\"\n\t\t}\n")
	b.WriteString("\t\tif !seen[p] {\n\t\t\tseen[p] = true\n\t\t\tpages = append(pages, p)\n\t\t}\n")
	b.WriteString("\t}\n")
	b.WriteString("\tif len(pages) == 0 {\n\t\tt.Fatal(\"sitemap listed no pages. Is uihost.WithSitemap configured?\")\n\t}\n")
	b.WriteString("\treturn pages\n")
	b.WriteString("}\n")

	if login {
		b.WriteString("\n// axeLogin authenticates the given browser as the seeded admin so\n")
		b.WriteString("// access-gated screens render their real content instead of bouncing\n")
		b.WriteString("// (a bounced scan would cover the wrong page). Login goes through the\n")
		b.WriteString("// auth battery's HTTP endpoint: it exists whether or not the app has\n")
		b.WriteString("// a login screen, and the session cookies are transplanted into the\n")
		b.WriteString("// browser. Prefixed cookies (__Host-/__Secure-) must stay host-only\n")
		b.WriteString("// and Secure or Chrome silently rejects them, so the transplant sets\n")
		b.WriteString("// them by URL, never by Domain.\n")
		b.WriteString("func axeLogin(t *testing.T, browser context.Context, base, email, password string) {\n")
		b.WriteString("\tt.Helper()\n")
		b.WriteString("\tjar, _ := cookiejar.New(nil)\n")
		b.WriteString("\tclient := &http.Client{Jar: jar, Timeout: 10 * time.Second}\n")
		b.WriteString(fmt.Sprintf("\tresp, err := client.PostForm(base+%q, url.Values{\"email\": {email}, \"password\": {password}})\n", authBase+"/login"))
		b.WriteString("\tif err != nil {\n\t\tt.Fatalf(\"login: %v\", err)\n\t}\n")
		b.WriteString("\tresp.Body.Close()\n")
		b.WriteString("\tu, err := url.Parse(base)\n")
		b.WriteString("\tif err != nil {\n\t\tt.Fatalf(\"login: parse base: %v\", err)\n\t}\n")
		b.WriteString("\tcookies := jar.Cookies(u)\n")
		b.WriteString("\tif len(cookies) == 0 {\n")
		b.WriteString("\t\tt.Fatal(\"login set no cookies: wrong seeded credentials, or the auth battery is not mounted\")\n")
		b.WriteString("\t}\n")
		b.WriteString("\ttab, done := axetest.NewTab(t, browser)\n")
		b.WriteString("\tdefer done()\n")
		b.WriteString("\t// Cookies attach to an origin; open it once before setting them.\n")
		b.WriteString("\tif err := chromedp.Run(tab, chromedp.Navigate(base+\"/\")); err != nil {\n")
		b.WriteString("\t\tt.Fatalf(\"login: open origin: %v\", err)\n\t}\n")
		b.WriteString("\tfor _, c := range cookies {\n")
		b.WriteString("\t\tck := c\n")
		b.WriteString("\t\taction := network.SetCookie(ck.Name, ck.Value).WithURL(base).WithPath(\"/\")\n")
		b.WriteString("\t\tif strings.HasPrefix(ck.Name, \"__Host-\") || strings.HasPrefix(ck.Name, \"__Secure-\") {\n")
		b.WriteString("\t\t\t// The cookie jar strips attributes; the prefix itself is the\n")
		b.WriteString("\t\t\t// contract (Secure + host-only + Path=/). localhost counts as\n")
		b.WriteString("\t\t\t// a secure context, so Secure cookies work over http there.\n")
		b.WriteString("\t\t\taction = action.WithSecure(true)\n")
		b.WriteString("\t\t}\n")
		b.WriteString("\t\tif err := chromedp.Run(tab, chromedp.ActionFunc(func(ctx context.Context) error {\n")
		b.WriteString("\t\t\treturn action.Do(ctx)\n")
		b.WriteString("\t\t})); err != nil {\n")
		b.WriteString("\t\t\tt.Fatalf(\"login: set cookie %s: %v\", ck.Name, err)\n")
		b.WriteString("\t\t}\n")
		b.WriteString("\t}\n")
		b.WriteString("}\n")
	}
	return b.String()
}

// isAppSpecDynamicRoute reports whether a route pattern has a
// parameter segment and therefore cannot be scanned without a concrete
// instance.
func isAppSpecDynamicRoute(route string) bool {
	return strings.Contains(route, ":") || strings.Contains(route, "{")
}

func renderAppSpecE2ETest(bp appspec.AppSpec) string {
	appName := bp.App.Name
	if appName == "" {
		appName = "GoFastr"
	}
	public, gated := appSpecE2EScreenRoutes(bp)
	adminEmail := bp.App.Admin.SeedEmail
	adminPass := bp.App.Admin.SeedPassword
	// The cookie-jar login client (and its net/url + net/http/cookiejar imports)
	// is only emitted when there's a gated screen AND a seeded admin to log in as.
	needsAuthClient := len(gated) > 0 && adminEmail != "" && adminPass != ""
	// Whether the APP seeds an admin at boot, which is a different question
	// from whether this TEST logs in. An app with a seeded admin refuses to
	// start on a fresh database without ADMIN_SEED_PASSWORD, so the test has to
	// supply one even when it never authenticates. Gating the environment
	// preamble on needsAuthClient meant an app spec with a seeded admin and no
	// gated screen generated a test that could not boot its own app: it passed
	// wherever the gitignored .env happened to exist and failed on every fresh
	// checkout, which is the worst place for the difference to show up.
	adminSeeded := bp.App.Auth.Enabled && adminEmail != "" && adminPass != ""
	target, hasTarget := appSpecE2EWritableTarget(bp)
	crud := hasTarget && needsAuthClient // the lifecycle needs an authed client
	// The generated e2e test boots against the app spec's declared driver.
	// A postgres app spec links only lib/pq, so it cannot open a SQLite
	// file DSN. Provision a throwaway Postgres database instead, and skip
	// when Postgres is unreachable so driverless CI stays green-by-skip.
	dbDriver := strings.ToLower(strings.TrimSpace(bp.App.DBDriver))
	isPostgres := dbDriver == "postgres" || dbDriver == "postgresql"

	goSlice := func(ss []string) string {
		q := make([]string, len(ss))
		for i, s := range ss {
			q[i] = fmt.Sprintf("%q", s)
		}
		return "[]string{" + strings.Join(q, ", ") + "}"
	}

	var b strings.Builder
	b.WriteString("package main\n\n")
	b.WriteString("import (\n")
	imports := []string{"context", "io", "net", "net/http", "os", "os/exec", "path/filepath", "runtime", "strings", "testing", "time"}
	if isPostgres {
		imports = append(imports, "crypto/rand", "database/sql", "encoding/hex", "net/url")
	}
	if needsAuthClient {
		imports = append(imports, "net/http/cookiejar", "net/url")
	}
	switch {
	case crud && len(target.keys) > 0:
		imports = append(imports, "encoding/json", "fmt", "net/url")
	case crud:
		imports = append(imports, "regexp")
	}
	seen := map[string]bool{}
	for _, imp := range imports {
		if seen[imp] {
			continue
		}
		seen[imp] = true
		b.WriteString("\t\"" + imp + "\"\n")
	}
	if adminSeeded {
		b.WriteString("\n\t\"github.com/DonaldMurillo/gofastr/core/dotenv\"\n")
		b.WriteString("\t\"github.com/DonaldMurillo/gofastr/framework\"\n")
	}
	b.WriteString(")\n\n")
	b.WriteString("func TestE2E(t *testing.T) {\n")
	b.WriteString("\tif testing.Short() { t.Skip(\"builds + boots the binary\") }\n")
	if adminSeeded {
		b.WriteString("\t// The seeded admin's password lives in the generated .env, not in\n")
		b.WriteString("\t// committed source. Load it before the server child inherits the\n")
		b.WriteString("\t// environment: the app refuses to boot on a fresh database when\n")
		b.WriteString("\t// ADMIN_SEED_PASSWORD is missing, whether or not this test logs in.\n")
		b.WriteString("\t_ = dotenv.LoadAndApply(framework.DefaultDotEnvPaths()...)\n")
		if needsAuthClient {
			b.WriteString("\tadminPass := os.Getenv(\"ADMIN_SEED_PASSWORD\")\n")
			b.WriteString("\tif adminPass == \"\" {\n")
			b.WriteString("\t\t// Fresh checkout: the gitignored .env is absent. The child seeds\n")
			b.WriteString("\t\t// the admin from whatever ADMIN_SEED_PASSWORD it inherits, so a\n")
			b.WriteString("\t\t// test-local value keeps the suite self-contained.\n")
			b.WriteString("\t\tadminPass = \"e2e-seed-admin-pw\"\n")
			b.WriteString("\t\tt.Setenv(\"ADMIN_SEED_PASSWORD\", adminPass)\n")
			b.WriteString("\t}\n")
		} else {
			// No login here, so no adminPass variable to declare and leave unused.
			b.WriteString("\tif os.Getenv(\"ADMIN_SEED_PASSWORD\") == \"\" {\n")
			b.WriteString("\t\tt.Setenv(\"ADMIN_SEED_PASSWORD\", \"e2e-seed-admin-pw\")\n")
			b.WriteString("\t}\n")
		}
	}
	b.WriteString("\tdir := t.TempDir()\n")
	b.WriteString("\tbin := filepath.Join(dir, \"app\")\n")
	b.WriteString("\tif runtime.GOOS == \"windows\" {\n\t\tbin += \".exe\"\n\t}\n")
	b.WriteString("\tbuild := exec.Command(\"go\", \"build\", \"-o\", bin, \".\")\n")
	b.WriteString("\tbuild.Stderr = os.Stderr\n")
	b.WriteString("\tif err := build.Run(); err != nil { t.Fatalf(\"build: %v\", err) }\n")
	b.WriteString("\taddr := e2eFreeAddr(t)\n")
	b.WriteString("\tsrv := exec.Command(bin)\n")
	b.WriteString("\tsrv.Dir = dir\n")
	if bp.App.PWA.Enabled {
		// The server runs from dir and serves its static directory, the
		// PWA icons the suite asserts below included, relative to where
		// it runs: from dir alone every icon answered 404.
		staticDir := appSpecEffectiveStaticDir(bp)
		b.WriteString("\t// The server serves its static directory relative to where it runs.\n")
		b.WriteString(fmt.Sprintf("\tif err := os.CopyFS(filepath.Join(dir, %q), os.DirFS(%q)); err != nil { t.Fatalf(\"copy static dir: %%v\", err) }\n", staticDir, staticDir))
	}
	if isPostgres {
		b.WriteString("\t// postgres app spec: carve a throwaway DB from TEST_POSTGRES_DSN\n")
		b.WriteString("\t// (skips when Postgres is unreachable). DATABASE_URL + DB_DRIVER point\n")
		b.WriteString("\t// the child at the carved DB so its postgres driver opens cleanly.\n")
		b.WriteString("\tdbURL := e2ePostgresDSN(t)\n")
		b.WriteString("\tsrv.Env = append(os.Environ(), \"PORT=\"+addr, \"DATABASE_URL=\"+dbURL, \"DB_DRIVER=postgres\")\n")
	} else {
		// GOFASTR_SEMANTIC_COVERAGE makes the server record which routes,
		// entities, hooks, and events this run actually exercised. Without
		// it the e2e suite drives a subprocess that never touches the test
		// harness, so `gofastr verify` reports every route it covers as
		// unreached.
		b.WriteString("\tsrv.Env = append(os.Environ(), \"PORT=\"+addr, \"GOFASTR_SEMANTIC_COVERAGE=1\", \"DATABASE_URL=file:\"+filepath.Join(dir, \"e2e.db\"))\n")
	}
	b.WriteString("\te2eCaptureServerLog(t, srv, dir)\n")
	b.WriteString("\tif err := srv.Start(); err != nil { t.Fatalf(\"start: %v\", err) }\n")
	b.WriteString("\tt.Cleanup(func() { _ = srv.Process.Kill(); _, _ = srv.Process.Wait() })\n")
	b.WriteString("\tbase := \"http://\" + addr\n")
	b.WriteString("\te2eWaitReady(t, base)\n\n")
	b.WriteString(fmt.Sprintf("\tif code, body := e2eDo(t, e2eClient, \"GET\", base+\"/\", \"\"); code != http.StatusOK || !strings.Contains(body, %q) {\n\t\tt.Errorf(\"home page = %%d, missing brand? %%v\", code, !strings.Contains(body, %q))\n\t}\n", appName, appName))

	// Every static public screen renders.
	b.WriteString("\n\t// Public screens render for anonymous visitors.\n")
	b.WriteString(fmt.Sprintf("\tfor _, p := range %s {\n", goSlice(public)))
	b.WriteString("\t\tif code, body := e2eDo(t, e2eClient, \"GET\", base+p, \"\"); code != http.StatusOK {\n")
	b.WriteString("\t\t\tt.Errorf(\"public screen %s = %d, want 200\", p, code)\n")
	b.WriteString("\t\t} else if len(body) < 120 { t.Errorf(\"public screen %s body suspiciously short (%d bytes)\", p, len(body)) }\n")
	b.WriteString("\t}\n")

	if bp.App.PWA.Enabled {
		b.WriteString("\n\t// Installable-PWA surface (app.pwa).\n")
		b.WriteString("\tif code, body := e2eDo(t, e2eClient, \"GET\", base+\"/manifest.webmanifest\", \"\"); code != http.StatusOK || !strings.Contains(body, `\"name\"`) {\n")
		b.WriteString("\t\tt.Errorf(\"manifest.webmanifest = %d, want 200 with a name\", code)\n")
		b.WriteString("\t}\n")
		b.WriteString("\tif code, body := e2eDo(t, e2eClient, \"GET\", base+\"/service-worker.js\", \"\"); code != http.StatusOK || !strings.Contains(body, \"gofastr-pwa-\") {\n")
		b.WriteString("\t\tt.Errorf(\"service-worker.js = %d, want 200 with a versioned cache\", code)\n")
		b.WriteString("\t}\n")
		b.WriteString("\tfor _, p := range []string{\"/icons/icon-192.png\", \"/icons/icon-512.png\", \"/icons/icon-maskable.png\", \"/__gofastr/pwa/register.js\", \"/__gofastr/pwa/offline\"} {\n")
		b.WriteString("\t\tif code, _ := e2eDo(t, e2eClient, \"GET\", base+p, \"\"); code != http.StatusOK {\n")
		b.WriteString("\t\t\tt.Errorf(\"pwa asset %s = %d, want 200\", p, code)\n")
		b.WriteString("\t\t}\n")
		b.WriteString("\t}\n")
	}

	if bp.App.LLMMD {
		b.WriteString("\n\t// Public LLM markdown (app.llm_md): the index and every screen document resolve.\n")
		b.WriteString("\tif code, body := e2eDo(t, e2eClient, \"GET\", base+\"/llm-pages.md\", \"\"); code != http.StatusOK || body == \"\" {\n")
		b.WriteString("\t\tt.Errorf(\"llm-pages.md = %d, want 200\", code)\n")
		b.WriteString("\t}\n")
		b.WriteString(fmt.Sprintf("\tfor _, p := range %s {\n", goSlice(public)))
		b.WriteString("\t\tmd := strings.TrimRight(p, \"/\") + \"/llm.md\"\n")
		b.WriteString("\t\tif code, _ := e2eDo(t, e2eClient, \"GET\", base+md, \"\"); code != http.StatusOK {\n")
		b.WriteString("\t\t\tt.Errorf(\"llm.md for %s = %d, want 200\", p, code)\n")
		b.WriteString("\t\t}\n")
		b.WriteString("\t}\n")
	}

	if len(gated) > 0 {
		b.WriteString("\n\t// Gated screens redirect anonymous callers to the login page.\n")
		b.WriteString("\tnoRedir := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}\n")
		b.WriteString(fmt.Sprintf("\tfor _, p := range %s {\n", goSlice(gated)))
		b.WriteString("\t\tif r, err := noRedir.Get(base + p); err == nil {\n")
		b.WriteString("\t\t\tr.Body.Close()\n")
		b.WriteString("\t\t\tif r.StatusCode != http.StatusSeeOther { t.Errorf(\"gated %s anon = %d, want 303 redirect\", p, r.StatusCode) }\n")
		b.WriteString("\t\t}\n")
		b.WriteString("\t}\n")
	}

	if needsAuthClient {
		// The client stops at a redirect to a login page: with no session
		// each gated screen bounced there, the client followed, and the
		// login page's 200 passed for the screen (2026-10-27 G14).
		b.WriteString("\n\t// Sign in, then every gated screen renders. A redirect to a login\n")
		b.WriteString("\t// page is returned, not followed: it means the session did not take.\n")
		b.WriteString("\tjar, _ := cookiejar.New(nil)\n")
		b.WriteString("\tclient := &http.Client{Jar: jar, Timeout: 10 * time.Second, CheckRedirect: func(req *http.Request, _ []*http.Request) error {\n")
		// authPolicy sends an anonymous caller to the login screen's route.
		b.WriteString(fmt.Sprintf("\t\tif req.URL.Path == %q { return http.ErrUseLastResponse }\n", appSpecLoginRoute(bp)))
		b.WriteString("\t\treturn nil\n")
		b.WriteString("\t}}\n")
		b.WriteString(fmt.Sprintf("\tif _, err := client.PostForm(base+%q, url.Values{\"email\": {%q}, \"password\": {adminPass}}); err != nil { t.Fatalf(\"login: %%v\", err) }\n", appSpecAuthBase(bp)+"/login", adminEmail))
		open, locked := appSpecE2ERoleSplit(bp, gated)
		if len(open) > 0 {
			b.WriteString(fmt.Sprintf("\tfor _, p := range %s {\n", goSlice(open)))
			b.WriteString("\t\tif code, body := e2eDo(t, client, \"GET\", base+p, \"\"); code != http.StatusOK {\n")
			b.WriteString("\t\t\tt.Errorf(\"authed screen %s = %d, want 200\", p, code)\n")
			b.WriteString("\t\t} else if len(body) < 120 { t.Errorf(\"authed screen %s body suspiciously short (%d bytes)\", p, len(body)) }\n")
			b.WriteString("\t}\n")
		}
		if len(locked) > 0 {
			b.WriteString("\t// These screens require a role the seeded admin does not hold.\n")
			b.WriteString(fmt.Sprintf("\tfor _, p := range %s {\n", goSlice(locked)))
			b.WriteString("\t\tif code, _ := e2eDo(t, client, \"GET\", base+p, \"\"); code != http.StatusForbidden {\n")
			b.WriteString("\t\t\tt.Errorf(\"authed screen %s = %d, want 403: the seeded admin lacks its role\", p, code)\n")
			b.WriteString("\t\t}\n")
			b.WriteString("\t}\n")
		}
	}

	if crud {
		b.WriteString(fmt.Sprintf("\n\t// Full CRUD lifecycle for %s through its API + form screens.\n", target.entity))
		b.WriteString(fmt.Sprintf("\tcode, body := e2eDo(t, client, \"POST\", base+%q, %q)\n", target.apiPath, target.createJSON))
		b.WriteString(fmt.Sprintf("\tif code != http.StatusCreated { t.Fatalf(\"create %s = %%d, want 201: %%s\", code, body) }\n", target.entity))
		b.WriteString("\tid := e2eExtractID(body)\n")
		b.WriteString(fmt.Sprintf("\tif id == \"\" { t.Fatalf(\"create %s: no id in response: %%s\", body) }\n", target.entity))
		b.WriteString(fmt.Sprintf("\tif code, body := e2eDo(t, client, \"GET\", base+%q, \"\"); code != http.StatusOK || !strings.Contains(body, \"<form\") {\n", target.newRoute))
		b.WriteString(fmt.Sprintf("\t\tt.Errorf(\"new form %s = %%d (has <form>? %%v)\", code, strings.Contains(body, \"<form\"))\n", target.entity))
		b.WriteString("\t}\n")
		if target.detailBase != "" {
			b.WriteString(fmt.Sprintf("\tif code, body := e2eDo(t, client, \"GET\", base+%q+\"/\"+id, \"\"); code != http.StatusOK {\n", target.detailBase))
			b.WriteString(fmt.Sprintf("\t\tt.Errorf(\"detail %s = %%d, want 200\", code)\n", target.entity))
			if target.probe != "" {
				b.WriteString(fmt.Sprintf("\t} else if !strings.Contains(body, %q) { t.Errorf(\"detail %s missing created value\") }\n", target.probe, target.entity))
			} else {
				b.WriteString("\t}\n")
			}
		}
		b.WriteString(fmt.Sprintf("\tif code, body := e2eDo(t, client, \"PUT\", base+%q+\"/\"+id, %q); code/100 != 2 {\n", target.apiPath, target.updateJSON))
		b.WriteString(fmt.Sprintf("\t\tt.Errorf(\"update %s = %%d: %%s\", code, body)\n", target.entity))
		b.WriteString("\t}\n")
		b.WriteString(fmt.Sprintf("\tif code, _ := e2eDo(t, client, \"DELETE\", base+%q+\"/\"+id, \"\"); code/100 != 2 {\n", target.apiPath))
		b.WriteString(fmt.Sprintf("\t\tt.Errorf(\"delete %s = %%d, want 2xx\", code)\n", target.entity))
		b.WriteString("\t}\n")
		b.WriteString(fmt.Sprintf("\tif code, _ := e2eDo(t, client, \"GET\", base+%q+\"/\"+id, \"\"); code != http.StatusNotFound {\n", target.apiPath))
		b.WriteString(fmt.Sprintf("\t\tt.Errorf(\"get deleted %s = %%d, want 404\", code)\n", target.entity))
		b.WriteString("\t}\n")
		if target.accessGated {
			b.WriteString(fmt.Sprintf("\n\t// Scoping: an anonymous write to the access-/owner-scoped %s API is refused.\n", target.entity))
			b.WriteString(fmt.Sprintf("\tif code, _ := e2eDo(t, e2eClient, \"POST\", base+%q, %q); code != http.StatusUnauthorized && code != http.StatusForbidden {\n", target.apiPath, target.createJSON))
			b.WriteString(fmt.Sprintf("\t\tt.Errorf(\"anonymous write to %s = %%d, want 401/403\", code)\n", target.entity))
			b.WriteString("\t}\n")
		}
	}
	b.WriteString("}\n\n")

	// helpers
	b.WriteString("func e2eCaptureServerLog(t *testing.T, srv *exec.Cmd, dir string) {\n")
	b.WriteString("\tt.Helper()\n")
	b.WriteString("\tserverLog, err := os.CreateTemp(dir, \"server-*.log\")\n")
	b.WriteString("\tif err != nil { t.Fatal(err) }\n")
	b.WriteString("\tsrv.Stdout, srv.Stderr = serverLog, serverLog\n")
	b.WriteString("\tt.Cleanup(func() {\n")
	b.WriteString("\t\t_ = serverLog.Close()\n")
	b.WriteString("\t\tdata, err := os.ReadFile(serverLog.Name())\n")
	b.WriteString("\t\tif err != nil { t.Errorf(\"read server log: %v\", err); return }\n")
	b.WriteString("\t\tif t.Failed() { t.Logf(\"server output:\\n%s\", data) }\n")
	b.WriteString("\t\tfor _, line := range strings.Split(string(data), \"\\n\") {\n")
	b.WriteString("\t\t\tif strings.Contains(line, \"component render panic:\") { t.Errorf(\"server render failure: %s\", line) }\n")
	b.WriteString("\t\t}\n")
	b.WriteString("\t})\n")
	b.WriteString("}\n\n")
	b.WriteString("// e2eClient bounds every probe this suite makes: a generated app that\n")
	b.WriteString("// accepts the connection but never responds fails the attempt at the\n")
	b.WriteString("// deadline instead of hanging the suite forever (the bootProbeClient\n")
	b.WriteString("// shape). e2eWaitReady, the sitemap fetch in axe_test.go, and every\n")
	b.WriteString("// e2eDo call site share it.\n")
	b.WriteString("var e2eClient = &http.Client{Timeout: 10 * time.Second}\n\n")
	b.WriteString("func e2eFreeAddr(t *testing.T) string {\n\tt.Helper()\n\tl, err := net.Listen(\"tcp\", \"127.0.0.1:0\")\n\tif err != nil { t.Fatal(err) }\n\tdefer l.Close()\n\treturn l.Addr().String()\n}\n\n")
	b.WriteString("func e2eWaitReady(t *testing.T, base string) {\n\tt.Helper()\n\tctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)\n\tdefer cancel()\n\tfor ctx.Err() == nil {\n\t\treq, err := http.NewRequestWithContext(ctx, http.MethodGet, base+\"/\", nil)\n\t\tif err != nil { t.Fatal(err) }\n\t\tif r, err := e2eClient.Do(req); err == nil { r.Body.Close(); return }\n\t\tselect {\n\t\tcase <-ctx.Done():\n\t\tcase <-time.After(100 * time.Millisecond):\n\t\t}\n\t}\n\tt.Fatal(\"server did not become ready\")\n}\n\n")
	b.WriteString("// e2eDo runs one request and returns the status code + body. A non-empty\n")
	b.WriteString("// body is sent as JSON. Network errors fail the test.\n")
	b.WriteString("func e2eDo(t *testing.T, client *http.Client, method, u, body string) (int, string) {\n")
	b.WriteString("\tt.Helper()\n")
	b.WriteString("\tvar rdr io.Reader\n")
	b.WriteString("\tif body != \"\" { rdr = strings.NewReader(body) }\n")
	b.WriteString("\treq, err := http.NewRequest(method, u, rdr)\n")
	b.WriteString("\tif err != nil { t.Fatalf(\"%s %s: %v\", method, u, err) }\n")
	b.WriteString("\tif body != \"\" { req.Header.Set(\"Content-Type\", \"application/json\") }\n")
	b.WriteString("\tr, err := client.Do(req)\n")
	b.WriteString("\tif err != nil { t.Fatalf(\"%s %s: %v\", method, u, err) }\n")
	b.WriteString("\tdefer r.Body.Close()\n")
	b.WriteString("\tbb, _ := io.ReadAll(r.Body)\n")
	b.WriteString("\treturn r.StatusCode, string(bb)\n}\n")
	if crud && len(target.keys) > 0 {
		// A declared key's record path is each key value path-escaped and
		// joined with "/", the path the API and the detail screen take.
		// The values come from the decoded response, so a comma, quote or
		// number in a key value reads whole.
		b.WriteString("\nvar e2eKeyCols = " + goSlice(target.keys) + "\n\n")
		b.WriteString("func e2eExtractID(body string) string {\n\tvar env struct{ Data map[string]any `json:\"data\"` }\n\td := json.NewDecoder(strings.NewReader(body))\n\td.UseNumber()\n\tif d.Decode(&env) != nil { return \"\" }\n\tparts := make([]string, len(e2eKeyCols))\n\tfor i, k := range e2eKeyCols {\n\t\tv, ok := env.Data[k]\n\t\tif !ok || v == nil { return \"\" }\n\t\tparts[i] = url.PathEscape(fmt.Sprint(v))\n\t}\n\treturn strings.Join(parts, \"/\")\n}\n")
	} else if crud {
		b.WriteString("\nvar e2eIDRe = regexp.MustCompile(`\"id\"\\s*:\\s*\"([^\"]+)\"`)\n\n")
		b.WriteString("func e2eExtractID(body string) string {\n\tif m := e2eIDRe.FindStringSubmatch(body); len(m) == 2 { return m[1] }\n\treturn \"\"\n}\n")
	}
	if isPostgres {
		b.WriteString("\n// e2ePostgresDSN provisions a throwaway Postgres database from an\n")
		b.WriteString("// env-provided admin DSN (TEST_POSTGRES_DSN) so the e2e flow exercises the\n")
		b.WriteString("// real postgres driver instead of a SQLite file that driver can't open. It\n")
		b.WriteString("// t.Skips when Postgres is unreachable, keeping driverless CI green-by-skip\n")
		b.WriteString("// instead of timing out with a misleading \"server did not become ready\".\n")
		b.WriteString("func e2ePostgresDSN(t *testing.T) string {\n")
		b.WriteString("\tt.Helper()\n")
		b.WriteString("\tadminDSN := os.Getenv(\"TEST_POSTGRES_DSN\")\n")
		b.WriteString("\tif adminDSN == \"\" {\n\t\tt.Skip(\"TEST_POSTGRES_DSN unset; skipping postgres e2e\")\n\t}\n")
		b.WriteString("\tadmin, err := sql.Open(\"postgres\", adminDSN)\n")
		b.WriteString("\tif err != nil {\n\t\tt.Skipf(\"postgres e2e: open admin: %v\", err)\n\t}\n")
		b.WriteString("\tprobeCtx, probeCancel := context.WithTimeout(context.Background(), 5*time.Second)\n")
		b.WriteString("\tdefer probeCancel()\n")
		b.WriteString("\tif err := admin.PingContext(probeCtx); err != nil {\n\t\t_ = admin.Close()\n\t\tt.Skipf(\"postgres e2e: admin unreachable: %v\", err)\n\t}\n")
		b.WriteString("\tbuf := make([]byte, 6)\n")
		b.WriteString("\tif _, err := rand.Read(buf); err != nil {\n\t\t_ = admin.Close()\n\t\tt.Fatalf(\"postgres e2e: rand: %v\", err)\n\t}\n")
		b.WriteString("\tname := \"e2e_\" + hex.EncodeToString(buf)\n")
		b.WriteString("\tif _, err := admin.ExecContext(probeCtx, \"CREATE DATABASE \\\"\"+name+\"\\\"\"); err != nil {\n\t\t_ = admin.Close()\n\t\tt.Skipf(\"postgres e2e: CREATE DATABASE %q failed (role needs CREATEDB): %v\", name, err)\n\t}\n")
		b.WriteString("\tt.Cleanup(func() {\n")
		b.WriteString("\t\tdctx, dcancel := context.WithTimeout(context.Background(), 10*time.Second)\n\t\tdefer dcancel()\n")
		b.WriteString("\t\t_, _ = admin.ExecContext(dctx, \"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1\", name)\n")
		b.WriteString("\t\tif _, derr := admin.ExecContext(dctx, \"DROP DATABASE IF EXISTS \\\"\"+name+\"\\\"\"); derr != nil {\n\t\t\tt.Errorf(\"postgres e2e: DROP DATABASE %q: %v\", name, derr)\n\t\t}\n")
		b.WriteString("\t\t_ = admin.Close()\n")
		b.WriteString("\t})\n")
		b.WriteString("\tu, err := url.Parse(adminDSN)\n")
		b.WriteString("\tif err != nil {\n\t\tt.Fatalf(\"postgres e2e: parse admin DSN: %v\", err)\n\t}\n")
		b.WriteString("\tu.Path = \"/\" + name\n")
		b.WriteString("\treturn u.String()\n")
		b.WriteString("}\n")
	}
	return b.String()
}

// appSpecI18nGo installs the generated app's translator. The screens and
// app.go declare their words as phrases (i18n.Define); this file loads
// the catalog that translates them.
const appSpecI18nGo = `package main

import (
	"embed"
	"log"
	"os"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// localeFS holds the app's translations: one locales/<tag>.json per
// language (es.json, pt-br.json), keyed like the phrases the screens
// declare. English is the phrases themselves and needs no file.
//
//go:embed locales
var localeFS embed.FS

// pseudoLocale is the tag GOFASTR_PSEUDO_LOCALE=1 adds: every phrase
// accented and lengthened, for finding layouts that cannot take longer
// words. A browser asking for en-XA gets it, or /en-xa/ under
// app.locale_prefixes (the catalog stores tags lowercase).
const pseudoLocale = "en-XA"

// translatorOption installs the app's translator.
func translatorOption() framework.AppOption {
	c, err := i18n.LoadJSONCatalog(localeFS, "locales")
	if err != nil {
		log.Fatalf("locales: %v", err)
	}
	if os.Getenv("GOFASTR_PSEUDO_LOCALE") == "1" {
		i18nui.AddPseudo(c, pseudoLocale)
	}
	return framework.WithI18n(i18n.NewTranslator(c, "en"))
}
`

// appSpecI18nTestGo checks each locale file against the phrases the
// app links in. It lives in package main, which wires the whole app, so
// i18n.Phrases() knows every screen's words and the framework's.
const appSpecI18nTestGo = `package main

import (
	"io/fs"
	"path"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// Each locales/<tag>.json names only phrases the app and the framework
// define, and keeps every {{placeholder}} of a phrase's English. Entity
// keys name the app's data rather than a defined phrase, so the check
// leaves them alone. Pass i18n.Require("<app>.") to also fail on a
// phrase a locale does not translate yet.
func TestCatalogsMatchPhrases(t *testing.T) {
	c, err := i18n.LoadJSONCatalog(localeFS, "locales")
	if err != nil {
		t.Fatal(err)
	}
	names, err := fs.Glob(localeFS, "locales/*.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		tag := strings.TrimSuffix(path.Base(name), ".json")
		for _, is := range i18n.CheckCatalog(c, tag, i18n.Skip("entity.")) {
			t.Errorf("%s: %s: %s (English %q, translation %q)", tag, is.Key, is.Problem, is.English, is.Text)
		}
	}
}
`

// appSpecLocalesReadme says what goes in locales/. It is also the file
// that keeps the directory non-empty, which go:embed requires.
const appSpecLocalesReadme = `# Translations

Add one JSON file per language, named by its tag: ` + "`es.json`" + `,
` + "`pt-br.json`" + `. Each key is a phrase key. The screens declare theirs at
the top of each screen file (` + "`i18n.Define(\"<key>\", \"<English>\")`" + `).
` + "`i18n.Phrases()`" + ` returns every key the app knows with its English,
the framework's own words (form buttons, table headers, the admin)
included. Dots nest:

` + "```json" + `
{
  "shop": {"screens": {"home": {"welcome": "Bienvenido"}}},
  "entity": {"orders": {"name": "Pedidos"}}
}
` + "```" + `

A key with no translation renders its English. {{pick}}
`

// appSpecLocalesPick is the README's line on how a reader gets a
// locale: the browser's Accept-Language, or the path under
// app.locale_prefixes.
func appSpecLocalesPick(bp appspec.AppSpec) string {
	if bp.App.LocalePrefixes {
		return "Each language lives under its own\n" +
			"path: `/es/` serves es.json, and English keeps the plain paths. Run\n" +
			"with `GOFASTR_PSEUDO_LOCALE=1` and open `/en-xa/` to see every phrase\n" +
			"accented and lengthened."
	}
	return "The browser's\n" +
		"Accept-Language picks the locale. Run with `GOFASTR_PSEUDO_LOCALE=1` and\n" +
		"ask for `en-XA` to see every phrase accented and lengthened."
}

// appSpecNeedsAuthHooks reports whether generated screens render an auth
// form, whose error copy lives in the owned auth_hooks.go seam.
func appSpecNeedsAuthHooks(bp appspec.AppSpec) bool {
	for _, s := range bp.Screens {
		if screenHasAuthForm(s) {
			return true
		}
	}
	return false
}

// appSpecAuthHooksGo is the thin owned seam for the auth screens' error
// copy. Edit the hook when the app needs different copy or extra codes.
// The copy is phrases under <app>.auth, so the app's catalog translates
// it like every other word the app says.
func appSpecAuthHooksGo(bp appspec.AppSpec) string {
	return strings.ReplaceAll(appSpecAuthHooksTemplate, "{{app}}", appSpecPhraseApp(bp.App.Name))
}

const appSpecAuthHooksTemplate = `package main

import (
	"context"

	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// The auth screens' error copy, in English. The app's catalog
// (locales/*.json) translates it by key.
var (
	authErrInvalidCredentials = i18n.Define("{{app}}.auth.invalid_credentials", "Invalid email or password.")
	authErrCredentialsMissing = i18n.Define("{{app}}.auth.credentials_required", "Enter your email and password.")
	authErrRateLimit          = i18n.Define("{{app}}.auth.rate_limit", "Too many attempts. Please wait a moment and try again.")
	authErrEmailTaken         = i18n.Define("{{app}}.auth.email_taken", "That email is already registered.")
	authErrUnknown            = i18n.Define("{{app}}.auth.unknown", "Sorry, something went wrong. Please try again.")
)

// authError maps auth redirect codes to the alert rendered by generated auth
// screens. Edit this hook when the app needs different copy or extra codes.
func authError(ctx context.Context) render.HTML {
	var p i18n.Phrase
	switch appui.QueryFromContext(ctx).Get("error") {
	case "":
		return ""
	case "invalid_credentials":
		p = authErrInvalidCredentials
	case "credentials_required":
		p = authErrCredentialsMissing
	case "rate_limit":
		p = authErrRateLimit
	case "email_taken", "user_exists", "duplicate":
		p = authErrEmailTaken
	default:
		p = authErrUnknown
	}
	return render.Text(p.Text(ctx))
}
`

// appSpecExtensionsGo is the owned seam for the app's entityui code:
// field kinds, view funcs, record tabs and actions. The entity says what
// to show (display:, states:); this file is where code that draws or acts
// goes. app.go builds the app's UI from it once at boot.
const appSpecExtensionsGo = `package main

import "github.com/DonaldMurillo/gofastr/framework/entityui"

// appExtensions holds the entityui extensions this app adds beside its
// screens. Everything starts empty: a view whose filter depends on the
// caller, a field kind, a record tab or an action each lands here, keyed
// by entity. See 'gofastr docs app-spec' ("Extending the screens") and
// the framework/entityui package doc for each extension point.
//
//	var appExtensions = entityui.Extensions{
//		Entities: map[string]entityui.Extension{
//			"invoices": {
//				Views: map[string]entityui.ViewFunc{
//					"overdue": {Filter: overdueFilter},
//				},
//			},
//		},
//	}
var appExtensions = entityui.Extensions{}
`

// appSpecSiteDescription resolves the generated app's site-level meta
// description: the app spec's app.description when set, otherwise an
// honest derivation from what the generator actually knows: the app
// name and its entities. Never empty: uihost strict mode (which
// generated apps ship with) requires a site description.
func appSpecSiteDescription(bp appspec.AppSpec) string {
	if bp.App.Description != "" {
		return bp.App.Description
	}
	name := bp.App.Name
	if name == "" {
		name = "GoFastr"
	}
	if len(bp.Entities) == 0 {
		return name + ", built with GoFastr."
	}
	plurals := make([]string, 0, len(bp.Entities))
	for _, ent := range bp.Entities {
		plurals = append(plurals, appSpecPluralWord(ent.Name))
	}
	return name + ": manage " + joinNaturalList(plurals) + "."
}

// appSpecPluralWord is a naive English pluralizer, good enough for
// entity names in a derived description ("product" → "products",
// "category" → "categories"). Names already ending in s pass through.
func appSpecPluralWord(word string) string {
	w := strings.ToLower(strings.TrimSpace(word))
	switch {
	case w == "":
		return w
	case strings.HasSuffix(w, "s"):
		return w
	case strings.HasSuffix(w, "y") && len(w) > 1 && !strings.ContainsRune("aeiou", rune(w[len(w)-2])):
		return w[:len(w)-1] + "ies"
	default:
		return w + "s"
	}
}

// joinNaturalList joins words as prose: "a", "a and b", "a, b, and c".
func joinNaturalList(words []string) string {
	switch len(words) {
	case 0:
		return ""
	case 1:
		return words[0]
	case 2:
		return words[0] + " and " + words[1]
	default:
		return strings.Join(words[:len(words)-1], ", ") + ", and " + words[len(words)-1]
	}
}

// appSpecHasStatesEntity reports whether any entity declares states.
func appSpecHasStatesEntity(bp appspec.AppSpec) bool {
	for _, decl := range bp.Entities {
		if decl.States != nil {
			return true
		}
	}
	return false
}

func RenderAppSpecMain(bp appspec.AppSpec) string {
	name := bp.App.Name
	if name == "" {
		name = "GoFastr"
	}
	staticDir := appSpecEffectiveStaticDir(bp)
	dbURL := bp.App.DBURL
	if dbURL == "" && len(bp.Entities) > 0 {
		dbURL = "file:gofastr.db"
	}
	driver := bp.App.DBDriver
	if driver == "" && (len(bp.Entities) > 0 || dbURL != "") {
		driver = "sqlite"
	}
	// Owned layout: module root by default (baseImport == module),
	// or a subpackage when app.output_dir / --out names one.
	outputDir := strings.TrimSuffix(strings.TrimPrefix(filepath.ToSlash(bp.App.OutputDir), "./"), "/")
	baseImport := strings.TrimSuffix(bp.App.Module, "/")
	if outputDir != "" && outputDir != "." {
		baseImport += "/" + outputDir
	}

	hasSeed := len(bp.Seed) > 0
	// ownerSeed: any owner_field entity + a bootstrapped admin → seed runs as
	// that admin so demo rows are owned and a fresh signup starts empty.
	ownerSeed := hasSeed && bp.App.Auth.Enabled && bp.App.Admin.SeedEmail != "" &&
		bp.App.Admin.SeedPassword != "" && appSpecHasOwnerScopedEntity(bp)
	var sb strings.Builder
	sb.WriteString("package main\n\n")
	sb.WriteString("import (\n")
	if hasSeed {
		sb.WriteString("\t\"context\"\n")
		sb.WriteString("\t\"errors\"\n")
	}
	sb.WriteString("\t\"database/sql\"\n")
	sb.WriteString("\t\"fmt\"\n")
	sb.WriteString("\t\"log\"\n")
	sb.WriteString("\t\"net/http\"\n")
	sb.WriteString("\t\"os\"\n")
	if hasSeed {
		sb.WriteString("\t\"sort\"\n")
		sb.WriteString("\t\"strings\"\n")
	}
	sb.WriteString("\n")
	sb.WriteString("\tuiapp \"github.com/DonaldMurillo/gofastr/core-ui/app\"\n")
	if ownerSeed {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/battery/auth\"\n")
	}
	if bp.App.Admin.Enabled {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/battery/admin\"\n")
	}
	sb.WriteString("\tgflog \"github.com/DonaldMurillo/gofastr/battery/log\"\n")
	sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/core/dotenv\"\n")
	if ownerSeed {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/core/handler\"\n")
	}
	sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/framework\"\n")
	if hasSeed {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/framework/crud\"\n")
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/framework/filter\"\n")
	}
	sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/framework/docs/mcptools\"\n")
	sb.WriteString("\tfwimage \"github.com/DonaldMurillo/gofastr/framework/image\"\n")
	sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/framework/isolation\"\n")
	sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/framework/uihost\"\n")
	if imp := appSpecDriverImport(driver); imp != "" {
		sb.WriteString(fmt.Sprintf("\t_ %q\n", imp))
	}
	// Unconditional: the entities seam ships even with zero entities, so a
	// later `--add`/scaffold entity registers without editing this owned file.
	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf("\t%q\n", baseImport+"/entities"))
	sb.WriteString(")\n\n")

	sb.WriteString("func main() {\n")
	sb.WriteString("\t// Load .env before anything reads the environment: the DB (and\n")
	sb.WriteString("\t// its DATABASE_URL) opens before NewApp's own dotenv auto-load\n")
	sb.WriteString("\t// would run. Existing process env always wins over the files.\n")
	sb.WriteString("\t//\n")
	sb.WriteString("\t// framework.DefaultDotEnvPaths(), not a hardcoded pair: dotenv.Apply\n")
	sb.WriteString("\t// never overwrites an existing variable, so loading a SHORTER list\n")
	sb.WriteString("\t// here would pin .env's DATABASE_URL and NewApp's higher-precedence\n")
	sb.WriteString("\t// .env.<APP_ENV> could no longer win.\n")
	sb.WriteString("\t_ = dotenv.LoadAndApply(framework.DefaultDotEnvPaths()...)\n")
	sb.WriteString("\truntimeIsolation, err := isolation.Resolve(\".\")\n")
	sb.WriteString("\tif err != nil {\n\t\tlog.Fatal(err)\n\t}\n")
	sb.WriteString("\tdb, err := openDB(runtimeIsolation)\n")
	sb.WriteString("\tif err != nil {\n\t\tlog.Fatal(err)\n\t}\n")
	sb.WriteString("\tif db != nil {\n\t\tdefer db.Close()\n\t}\n\n")
	sb.WriteString("\toptions := []framework.AppOption{\n")
	sb.WriteString("\t\tframework.WithConfig(framework.AppConfig{Name: appName, APIPrefix: apiPrefix}),\n")
	sb.WriteString("\t\t// The app's translations (i18n.go): locales/<tag>.json, picked\n")
	sb.WriteString("\t\t// by the browser's Accept-Language.\n")
	sb.WriteString("\t\ttranslatorOption(),\n")
	if bp.App.LocalePrefixes {
		sb.WriteString("\t\t// Each locale under its own path (/es/pricing), with hreflang,\n")
		sb.WriteString("\t\t// sitemap and robots entries; English keeps the plain paths.\n")
		sb.WriteString("\t\tframework.WithLocalePrefixes(),\n")
	}
	sb.WriteString("\t\t// Agent-ready MCP surface: WithMCP mounts /mcp (POST JSON-RPC +\n")
	sb.WriteString("\t\t// GET SSE) plus the discovery well-knowns (/.well-known/mcp/*);\n")
	sb.WriteString("\t\t// WithMCPIntrospection adds read-only orientation tools\n")
	sb.WriteString("\t\t// (app_routes, app_readiness, app_config, …). The\n")
	sb.WriteString("\t\t// introspection tools reveal the app's shape: remove the option\n")
	sb.WriteString("\t\t// if /mcp is reachable by untrusted callers in production.\n")
	sb.WriteString("\t\t// WithMCPTools(mcptools.Register) adds framework_docs_list/get/search over the\n")
	sb.WriteString("\t\t// embedded framework docs; drop it (and the import) to shrink the binary.\n")
	sb.WriteString("\t\t// Under `gofastr dev` the framework additionally auto-enables the\n")
	sb.WriteString("\t\t// mutating control tools + log debug tools (opt-out:\n")
	sb.WriteString("\t\t// GOFASTR_DEV_MCP=0); add framework.WithMCPControl() here to opt a\n")
	sb.WriteString("\t\t// trusted production /mcp into runtime control.\n")
	sb.WriteString("\t\tframework.WithMCP(),\n")
	sb.WriteString("\t\tframework.WithMCPIntrospection(),\n")
	sb.WriteString("\t\tframework.WithMCPTools(mcptools.Register),\n")
	if bp.App.PublicOpenAPI {
		sb.WriteString("\t\t// Explicit schema disclosure requested by app.public_openapi.\n")
		sb.WriteString("\t\tframework.WithPublicOpenAPI(),\n")
	}
	sb.WriteString("\t}\n")
	sb.WriteString("\tif db != nil {\n\t\toptions = append(options, framework.WithDB(db))\n\t}\n")
	sb.WriteString("\tfwApp := framework.NewApp(options...)\n")
	sb.WriteString("\t// Structured logging (battery/log zero-value canon): per-app file\n")
	sb.WriteString("\t// sink, access log, panic recovery, colorized dev console. Under\n")
	sb.WriteString("\t// `gofastr dev` its MCP debug tools (log_recent, log_filter,\n")
	sb.WriteString("\t// log_metrics, log_set_level) auto-register so a connected agent\n")
	sb.WriteString("\t// can read recent requests and errors; they stay OFF outside dev,\n")
	sb.WriteString("\t// access logs carry client IPs. Set EnableMCP: true here only when\n")
	sb.WriteString("\t// a production /mcp is reachable solely by trusted callers.\n")
	sb.WriteString("\tfwApp.RegisterPlugin(gflog.New(gflog.Config{}))\n")
	if fams := appSpecConfiguredFontFamilies(bp.App.Theme); len(fams) > 0 {
		// Boot-time guard: a self-hosted webfont whose file is missing from
		// the static dir would 404 and silently fall back to system fonts.
		// Warn loudly (once at startup) with the exact path so it never fails
		// silently; the generate-time fetch normally supplies these.
		sb.WriteString("\tfor _, fontFile := range []string{\n")
		for _, fam := range fams {
			sb.WriteString(fmt.Sprintf("\t\t%q,\n", appSpecFontRelPath(staticDir, fam)))
		}
		sb.WriteString("\t} {\n")
		sb.WriteString("\t\tif _, statErr := os.Stat(fontFile); statErr != nil {\n")
		sb.WriteString("\t\t\tlog.Printf(\"gofastr: webfont %s is missing, falling back to system fonts. Add the .woff2 there (the strict CSP blocks the Google CDN, so fonts must be self-hosted). See `gofastr docs app-spec`.\", fontFile)\n")
		sb.WriteString("\t\t}\n")
		sb.WriteString("\t}\n")
	}
	sb.WriteString("\tentities.RegisterAll(fwApp)\n")
	switch {
	case hasSeed && appSpecHasStatesEntity(bp):
		sb.WriteString("\t// An entity declares states and the seed writes rows: seeded rows may\n")
		sb.WriteString("\t// start at any state under the audited state override below, and an\n")
		sb.WriteString("\t// override is refused on an entity with no audit log, so enable it\n")
		sb.WriteString("\t// here (WithAuditLog creates audit_log when it does not exist).\n")
		sb.WriteString("\tfwApp.WithAuditLog(framework.AuditConfig{})\n")
	case bp.App.Admin.Enabled:
		// The admin writes under elevation and its Audit log page and
		// dashboard read audit_log: every entity write leaves a row.
		sb.WriteString("\t// The admin writes entities under elevation, and its Audit log page and\n")
		sb.WriteString("\t// dashboard read audit_log: record every entity write there\n")
		sb.WriteString("\t// (WithAuditLog creates audit_log when it does not exist).\n")
		sb.WriteString("\tif db != nil {\n\t\tfwApp.WithAuditLog(framework.AuditConfig{})\n\t}\n")
	}
	for _, hook := range bp.Hooks {
		handler := strings.TrimSpace(hook.Handler)
		lifecycle, known := appspec.AppSpecHookTypes[hook.When]
		// The emitter is reachable without validateAppSpec
		// (loadAppSpecPath(path, false)): never wire a handler that is
		// not a Go identifier or a lifecycle point outside the closed set;
		// validateAppSpec is the loud gate for both.
		if !project.IsGoIdentifier(handler) || !known {
			continue
		}
		sb.WriteString(fmt.Sprintf("\tfwApp.HookRegistry(%q).RegisterHook(%s, %s)\n", hook.Entity, lifecycle, handler))
	}
	sb.WriteString("\tsite := uiapp.NewApp(appName)\n")
	sb.WriteString("\tRegisterGenerated(fwApp, site, db)\n")
	if hasSeed {
		// Seed data runs AFTER RegisterGenerated on purpose. RegisterGenerated
		// (when auth+admin are configured) registers the bootstrap-admin seed
		// hook first; App.WithSeed fires hooks in registration order, so by
		// the time this hook runs the admin already exists and the owner-
		// scoped rows below can resolve it as their owner. Registering the
		// data seed before RegisterGenerated would run it before the admin
		// exists, leaving owner-scoped rows silently unseeded. Seeding is
		// idempotent (skip entities that already have rows) and fail-fast: a
		// row that fails aborts startup, because the CountAll gate would
		// mark the entity seeded and the dropped row would never retry.
		sb.WriteString("\tfwApp.WithSeed(func(ctx context.Context) error {\n")
		// Seeded rows may start at any state (a demo invoice already
		// paid): the override is the one trusted-write path, and it is
		// audited per row.
		sb.WriteString("\t\tctx = framework.WithStateOverride(ctx, \"seed\")\n")
		if ownerSeed {
			sb.WriteString("\t\t// Resolve the bootstrap admin (created by the earlier-registered\n")
			sb.WriteString("\t\t// admin seed hook) so the demo rows are owned by them; a fresh\n")
			sb.WriteString("\t\t// signup then starts with an empty workspace and adds its own.\n")
			sb.WriteString("\t\t// CreateOne stamps the owner column from this user.\n")
			sb.WriteString(fmt.Sprintf("\t\tif u, _, err := auth.NewEntityUserStore(db, \"auth_users\").FindByEmail(ctx, %q); err == nil && u != nil {\n", bp.App.Admin.SeedEmail))
			sb.WriteString("\t\t\tctx = handler.SetUser(ctx, u)\n")
			sb.WriteString("\t\t}\n")
		}
		sb.WriteString("\t\tfor _, s := range seedData() {\n")
		sb.WriteString("\t\t\tch, err := fwApp.CrudHandler(s.Entity)\n")
		sb.WriteString("\t\t\tif err != nil {\n\t\t\t\treturn fmt.Errorf(\"seed %s: no handler: %w\", s.Entity, err)\n\t\t\t}\n")
		sb.WriteString("\t\t\tn, err := ch.CountAll(ctx, framework.ListOptions{})\n")
		sb.WriteString("\t\t\tif err != nil {\n\t\t\t\treturn fmt.Errorf(\"seed %s: count: %w\", s.Entity, err)\n\t\t\t}\n")
		sb.WriteString("\t\t\tif n > 0 {\n\t\t\t\tcontinue\n\t\t\t}\n")
		sb.WriteString("\t\t\tfor i, row := range s.Rows {\n")
		sb.WriteString("\t\t\t\tresolveSeedRefs(ctx, fwApp, row)\n")
		sb.WriteString("\t\t\t\tif _, err := ch.CreateOne(ctx, row); err != nil {\n")
		sb.WriteString("\t\t\t\t\treturn seedCreateError(s.Entity, i+1, err)\n")
		sb.WriteString("\t\t\t\t}\n")
		sb.WriteString("\t\t\t}\n")
		sb.WriteString("\t\t}\n")
		sb.WriteString("\t\treturn nil\n")
		sb.WriteString("\t})\n")
	}
	// appBaseCSS ships first so the user's static/app.css (loaded
	// after) overrides it; it gives the generated entity blocks modern,
	// responsive defaults out of the box (scrollable tables, form rhythm).
	// Opt-in host surfaces (PWA, public LLM markdown) append to the same
	// uihost.New call. Independent by design: enabling one never emits
	// the other.
	// Always-on SEO/icon defaults: a generated app ships a site
	// description (app-spec-provided or derived from the app's
	// entities), a favicon (derived at startup from the theme's primary
	// color; replace appIconPNG with real logo bytes when there is
	// one), a sitemap rooted at appBaseURL(), and a robots.txt that
	// keeps the framework's internal endpoints (and the admin
	// back-office, when enabled) out of the index while allowing
	// everything else. Explicit PWA icons (app.pwa) still win the
	// manifest. WithStrict goes last: the generated surface passes
	// every strict check, so a later screen that forgets its SEO (or
	// its axe scan, in dev) fails boot with the guided finding list
	// instead of shipping bare.
	extraUIOpts := fmt.Sprintf(", uihost.WithDescription(%q)", appSpecSiteDescription(bp))
	extraUIOpts += ", uihost.WithAppIcon(appIconPNG())"
	sitemapExclude := ""
	robotsDisallow := `"/__gofastr/"`
	if bp.App.Admin.Enabled {
		// TrimRight matches the admin battery's own normalization
		// (admin.New trims the PathPrefix), so "/admin/" in the
		// app spec excludes the same routes the battery mounts.
		adminPath := strings.TrimRight(bp.App.Admin.Path, "/")
		if adminPath == "" {
			adminPath = "/admin"
		}
		sitemapExclude = fmt.Sprintf(", ExcludePaths: []string{%q}", adminPath)
		robotsDisallow += fmt.Sprintf(", %q", adminPath)
	}
	extraUIOpts += fmt.Sprintf(", uihost.WithSitemap(uihost.SitemapConfig{BaseURL: appBaseURL()%s})", sitemapExclude)
	extraUIOpts += fmt.Sprintf(", uihost.WithRobots(uihost.RobotsConfig{Disallow: []string{%s}})", robotsDisallow)
	if bp.App.PWA.Enabled {
		extraUIOpts += ", " + appSpecPWAOptionLiteral(bp)
	}
	if bp.App.LLMMD {
		extraUIOpts += ", uihost.WithPublicLLMMD()"
	}
	extraUIOpts += ", uihost.WithStrict()"
	if staticDir != "" {
		sb.WriteString(fmt.Sprintf("\tfwApp.Mount(uihost.New(site, uihost.WithStaticDir(%q), uihost.WithCustomCSS(fontFaceCSS+appBaseCSS()+uihost.ReadCustomCSSFile(%q))%s))\n", staticDir, staticDir+"/app.css", extraUIOpts))
	} else {
		sb.WriteString(fmt.Sprintf("\tfwApp.Mount(uihost.New(site, uihost.WithCustomCSS(fontFaceCSS+appBaseCSS())%s))\n", extraUIOpts))
	}
	if bp.App.Admin.Enabled {
		// Auto-generated back-office over every CRUD entity. Registered as a
		// battery so it Inits after Mount and discovers the UI host. Gated by
		// the admin role; unauthenticated GETs bounce to the login page.
		adminPath := bp.App.Admin.Path
		if adminPath == "" {
			adminPath = "/admin"
		}
		adminRole := bp.App.Admin.Role
		if adminRole == "" {
			adminRole = "admin"
		}
		// Build the base admin config, then route it through the
		// adminBatteryConfigurators seam (admin_register.go) so a new file
		// can wire Policy/GrantStore/Auth additively, no edits here. The
		// admin draws through the app's UI host and its entity screens
		// through appUI, so it inherits the app's theme and fonts.
		sb.WriteString(fmt.Sprintf("\tadminCfg := admin.Config{PathPrefix: %q, Title: appName, AdminRole: %q, LoginPath: %q, UI: appUI, DB: db, AuditTable: \"audit_log\", AllEntities: true, SavedViews: true}\n",
			adminPath, adminRole, bp.App.Admin.LoginPath))
		sb.WriteString("\tapplyAdminBatteryConfigurators(&adminCfg)\n")
		sb.WriteString("\tfwApp.RegisterBattery(admin.New(adminCfg))\n")
	}
	sb.WriteString("\taddr, err := runtimeIsolation.Addr(getEnv(\"PORT\", \"localhost:8080\"))\n")
	sb.WriteString("\tif err != nil {\n\t\tlog.Fatal(err)\n\t}\n")
	sb.WriteString("\t// Banner fires via OnReady: only after auto-migrate, hooks, and the\n")
	sb.WriteString("\t// port bind all succeeded. Printing before Start would announce a\n")
	sb.WriteString("\t// server that may never come up.\n")
	sb.WriteString("\tfwApp.OnReady(func(boundAddr string) {\n\t\tfmt.Printf(\"Server running at http://%s\\n\", boundAddr)\n\t})\n")
	sb.WriteString("\tif err := fwApp.Start(addr); err != nil && err != http.ErrServerClosed {\n\t\tlog.Fatal(err)\n\t}\n")
	sb.WriteString("}\n\n")

	sb.WriteString("func openDB(runtimeIsolation *isolation.Runtime) (*sql.DB, error) {\n")
	if driver == "" && dbURL == "" {
		sb.WriteString("\treturn nil, nil\n")
	} else {
		sb.WriteString(fmt.Sprintf("\tdriver := getEnv(\"DB_DRIVER\", %q)\n", driver))
		// A credentialed DSN never appears as a committed fallback literal;
		// the generated .env supplies DATABASE_URL instead. A ${DATABASE_URL}
		// reference (what kiln freeze emits for a credentialed DSN) is not a
		// DSN either: as a fallback it would hand the driver the literal
		// ref string when the variable is unset, so it fails closed the
		// same way with a message naming the variable.
		fallbackDSN := dbURL
		if dsnredact.HasSecret(dbURL) || isSelfEnvRef("DATABASE_URL", dbURL) {
			fallbackDSN = ""
		}
		sb.WriteString(fmt.Sprintf("\tdsn := getEnv(\"DATABASE_URL\", %q)\n", fallbackDSN))
		if fallbackDSN == "" {
			sb.WriteString("\tif dsn == \"\" {\n\t\treturn nil, fmt.Errorf(\"DATABASE_URL is not set: the generated .env supplies it; copy it next to the binary or export the variable\")\n\t}\n")
		}
		sb.WriteString("\tresolvedDriver, resolvedDSN, err := runtimeIsolation.Database(driver, dsn)\n")
		sb.WriteString("\tif err != nil {\n\t\treturn nil, err\n\t}\n")
		sb.WriteString("\tdriver, dsn = resolvedDriver, resolvedDSN\n")
		sb.WriteString("\tswitch driver {\n")
		sb.WriteString("\tcase \"\", \"none\":\n\t\treturn nil, nil\n")
		sb.WriteString("\tcase \"sqlite\", \"sqlite3\":\n\t\treturn sql.Open(\"sqlite3\", dsn)\n")
		sb.WriteString("\tcase \"postgres\", \"postgresql\":\n\t\treturn sql.Open(\"postgres\", dsn)\n")
		sb.WriteString("\tdefault:\n\t\treturn nil, fmt.Errorf(\"unsupported db driver %q\", driver)\n")
		sb.WriteString("\t}\n")
	}
	sb.WriteString("}\n\n")
	sb.WriteString("func getEnv(key, fallback string) string {\n")
	sb.WriteString("\tif v := os.Getenv(key); v != \"\" {\n\t\treturn v\n\t}\n")
	sb.WriteString("\treturn fallback\n")
	sb.WriteString("}\n\n")
	iconFrom, iconTo := appSpecIconStops(bp)
	sb.WriteString("// appIconPNG generates the app's icon source at startup: a diagonal\n")
	sb.WriteString("// gradient in the app spec's primary color. uihost.WithAppIcon derives\n")
	sb.WriteString("// /favicon.ico, the sized PNGs, and the head links from this one image.\n")
	sb.WriteString("// Have a real logo? Embed it and return those bytes instead:\n")
	sb.WriteString("//\n")
	sb.WriteString("//\t//go:embed logo.png\n")
	sb.WriteString("//\tvar logo []byte\n")
	sb.WriteString("func appIconPNG() []byte {\n")
	sb.WriteString(fmt.Sprintf("\timg, err := fwimage.NewGradient(512, 512, %q, %q)\n", iconFrom, iconTo))
	sb.WriteString("\tif err != nil {\n\t\treturn nil // WithAppIcon warns and skips on undecodable input\n\t}\n")
	sb.WriteString("\tb, err := img.PNG().Bytes()\n")
	sb.WriteString("\tif err != nil {\n\t\treturn nil\n\t}\n")
	sb.WriteString("\treturn b\n")
	sb.WriteString("}\n\n")
	sb.WriteString("// appBaseURL is the canonical origin used in sitemap.xml <loc> entries\n")
	sb.WriteString("// (they must be absolute URLs). Set APP_BASE_URL when deploying; the\n")
	sb.WriteString("// fallback keeps local dev and tests working out of the box.\n")
	sb.WriteString("func appBaseURL() string {\n")
	fallbackBase := strings.TrimRight(bp.App.BaseURL, "/")
	if fallbackBase == "" {
		fallbackBase = "http://localhost:8080"
	}
	sb.WriteString(fmt.Sprintf("\treturn getEnv(\"APP_BASE_URL\", %q)\n", fallbackBase))
	sb.WriteString("}\n")
	if hasSeed {
		sb.WriteString("\n// resolveSeedRefs rewrites \"@entity.field=value\" reference strings in a\n")
		sb.WriteString("// seed row into the resolved primary-key id of the matching row. This lets\n")
		sb.WriteString("// relational seed data point at rows created earlier in the same pass\n")
		sb.WriteString("// (e.g. a subscription's customer_id: \"@customers.email=ada@acme.io\").\n")
		sb.WriteString("// Unresolvable references are left as-is so the create fails loudly.\n")
		sb.WriteString("func resolveSeedRefs(ctx context.Context, fwApp *framework.App, row map[string]any) {\n")
		sb.WriteString("\tfor k, v := range row {\n")
		sb.WriteString("\t\ts, ok := v.(string)\n")
		sb.WriteString("\t\tif !ok || !strings.HasPrefix(s, \"@\") {\n\t\t\tcontinue\n\t\t}\n")
		sb.WriteString("\t\trest := s[1:]\n")
		sb.WriteString("\t\tdot := strings.IndexByte(rest, '.')\n")
		sb.WriteString("\t\teq := strings.IndexByte(rest, '=')\n")
		sb.WriteString("\t\tif dot < 1 || eq <= dot+1 {\n\t\t\tcontinue\n\t\t}\n")
		sb.WriteString("\t\tent, field, val := rest[:dot], rest[dot+1:eq], rest[eq+1:]\n")
		sb.WriteString("\t\tch, err := fwApp.CrudHandler(ent)\n")
		sb.WriteString("\t\tif err != nil {\n\t\t\tcontinue\n\t\t}\n")
		sb.WriteString("\t\trows, err := ch.ListAll(ctx, framework.ListOptions{Filters: []filter.ParsedFilter{{Field: field, Op: filter.OpEq, Value: val}}, Limit: 1})\n")
		sb.WriteString("\t\tif err != nil || len(rows) == 0 {\n\t\t\tcontinue\n\t\t}\n")
		sb.WriteString("\t\tpk, err := ch.Entity.KeyColumn()\n")
		sb.WriteString("\t\tif err != nil {\n\t\t\tcontinue\n\t\t}\n")
		sb.WriteString("\t\tif id, ok := rows[0][ch.WireKey(pk)]; ok {\n\t\t\trow[k] = id\n\t\t}\n")
		sb.WriteString("\t}\n")
		sb.WriteString("}\n")
		sb.WriteString("\n// seedCreateError renders a failed seed insert with the per-field detail\n")
		sb.WriteString("// the CRUD validator already computed. ValidationError.Error() is the\n")
		sb.WriteString("// bare string \"validation failed\": the messages worth reading live in\n")
		sb.WriteString("// Fields(), so a plain %w would abort the boot with zero actionable\n")
		sb.WriteString("// information. Unwrap, print every field message (sorted, so the output\n")
		sb.WriteString("// is stable), and name the row by its one-based position in the\n")
		sb.WriteString("// app spec's seed: block. The position, never a row value, is the\n")
		sb.WriteString("// label: this error aborts boot through log.Fatal, so its text lands\n")
		sb.WriteString("// in application logs, and seed rows carry admin emails and passwords.\n")
		sb.WriteString("func seedCreateError(entity string, index int, err error) error {\n")
		sb.WriteString("\tvar ve *crud.ValidationError\n")
		sb.WriteString("\tif !errors.As(err, &ve) || len(ve.Fields()) == 0 {\n")
		sb.WriteString("\t\treturn fmt.Errorf(\"seed %s: %w\", entity, err)\n")
		sb.WriteString("\t}\n")
		sb.WriteString("\tnames := make([]string, 0, len(ve.Fields()))\n")
		sb.WriteString("\tfor name := range ve.Fields() {\n")
		sb.WriteString("\t\tnames = append(names, name)\n")
		sb.WriteString("\t}\n")
		sb.WriteString("\tsort.Strings(names)\n")
		sb.WriteString("\tvar b strings.Builder\n")
		sb.WriteString("\tfmt.Fprintf(&b, \"seed %s row %d failed validation:\", entity, index)\n")
		sb.WriteString("\tfor _, name := range names {\n")
		sb.WriteString("\t\tfor _, msg := range ve.Fields()[name] {\n")
		sb.WriteString("\t\t\tfmt.Fprintf(&b, \"\\n  - %s: %s\", name, msg)\n")
		sb.WriteString("\t\t}\n")
		sb.WriteString("\t}\n")
		sb.WriteString("\treturn fmt.Errorf(\"%s\\ncause: %w\", b.String(), err)\n")
		sb.WriteString("}\n")
	}
	return sb.String()
}

// appSpecIconStops picks the generated app icon's gradient stops from
// the app spec theme: the primary color when it's a plain #RRGGBB hex
// (theme values may be any CSS color; oklch etc. can't feed the PNG
// generator, so those fall back to the framework's indigo), and a 45%-
// darkened variant of the from-stop for depth.
func appSpecIconStops(bp appspec.AppSpec) (string, string) {
	from := "#4338CA"
	if v, ok := bp.App.Theme["primary"]; ok && isHexRGB(v) {
		from = strings.ToUpper(v)
	}
	var r, g, b int
	fmt.Sscanf(from[1:], "%02X%02X%02X", &r, &g, &b)
	to := fmt.Sprintf("#%02X%02X%02X", r*55/100, g*55/100, b*55/100)
	return from, to
}

func isHexRGB(s string) bool {
	if len(s) != 7 || s[0] != '#' {
		return false
	}
	for _, c := range s[1:] {
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f', c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}

func appSpecDriverImport(driver string) string {
	switch strings.ToLower(strings.TrimSpace(driver)) {
	case "", "sqlite", "sqlite3":
		return "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
	case "postgres", "postgresql":
		return "github.com/lib/pq"
	default:
		return ""
	}
}

func RenderAppSpecScreens(bp appspec.AppSpec) string {
	entityMap := make(map[string]framework.EntityDeclaration, len(bp.Entities))
	for _, decl := range bp.Entities {
		entityMap[decl.Name] = decl
	}
	apiBase := appSpecAPIBase(bp.App.APIPrefix)
	var sb strings.Builder
	sb.WriteString("package main\n\n")
	bp.SetPhraseCollector(appSpecScreenPhrases(bp.App.Name, "screens", "screens"))
	var bodies strings.Builder
	for _, screen := range bp.Screens {
		bodies.WriteString(appSpecScreenBody(bp, screen, entityMap, apiBase))
	}
	needs := appSpecScreenImports(bp)
	needs.readCode(bodies.String(), bp.PhraseCollector())
	writeScreenImportBlock(&sb, needs, false, true)
	sb.WriteString(bp.PhraseCollector().Block())
	if needs.node {
		sb.WriteString("type nodeComponent struct { node uinode.Node }\n\n")
		sb.WriteString("func (c nodeComponent) Render() render.HTML { return noderender.RenderTrustedNode(c.node) }\n\n")
	}
	sb.WriteString(bodies.String())
	return sb.String()
}

// AppSpecScreenStackOpen opens the ui.Stack a screen's blocks sit in.
// pack recognises this exact call (config included) as the screen
// stack, so the two must stay one spelling.
const AppSpecScreenStackOpen = "ui.Stack(ui.StackConfig{Gap: ui.GapXL},"

// emitsHTMLRef reports whether emitted screen code calls the core-ui/html
// package (an `html` identifier selecting an exported name). The import is
// read off the code itself because each block emitter decides on its own
// whether it reaches for html.*: a hand-kept predicate per block kind
// missed the auth form's hidden html.Input and the node screens once the
// screen root stopped being an html.Div that pulled the import in for
// everyone. The code is tokenized, so a string literal or comment that
// mentions html.Div (a callout's copy) does not count as a call.
func emitsHTMLRef(code string) bool { return emitsPkgRef(code, "html") }

// emitsPkgRef reports whether code selects an exported name from the
// package named pkg, read the way emitsHTMLRef reads html.
func emitsPkgRef(code, pkg string) bool {
	type tokLit struct {
		tok token.Token
		lit string
	}
	fset := token.NewFileSet()
	var sc scanner.Scanner
	sc.Init(fset.AddFile("", fset.Base(), len(code)), []byte(code), nil, 0)
	// The last three tokens before the current one: `html` must open its
	// selector (not follow a dot, as in x.html.Div) and select an
	// exported name.
	var last [3]tokLit
	for {
		_, tok, lit := sc.Scan()
		if tok == token.EOF {
			return false
		}
		if last[0].tok != token.PERIOD && last[1].tok == token.IDENT && last[1].lit == pkg &&
			last[2].tok == token.PERIOD && tok == token.IDENT && lit[0] >= 'A' && lit[0] <= 'Z' {
			return true
		}
		last = [3]tokLit{last[1], last[2], {tok, lit}}
	}
}

// writeScreenImportBlock writes the shared import block for a set of screens.
// When withMount is true, database/sql + framework are added (per-screen files
// carry a mount func with that signature); the aggregated test-facing emitter
// passes false to stay byte-compatible with the pre-per-screen screens.go.
func writeScreenImportBlock(sb *strings.Builder, needs screenImportNeeds, withMount, hasScreens bool) {
	sb.WriteString("import (\n")
	// Every screen takes a context: ScreenTitle(ctx) is always emitted.
	if hasScreens {
		sb.WriteString("\t\"context\"\n")
		if needs.url {
			sb.WriteString("\t\"net/url\"\n")
		}
		sb.WriteString("\n")
	}
	if withMount {
		sb.WriteString("\t\"database/sql\"\n")
	}
	sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/core-ui/app\"\n")
	if hasScreens {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/core-ui/component\"\n")
	}
	if hasScreens && needs.island {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/core-ui/island\"\n")
	}
	if hasScreens && needs.html {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/core-ui/html\"\n")
	}
	if hasScreens && needs.interactive {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/core-ui/interactive\"\n")
	}
	if hasScreens && needs.i18n {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/core/i18n\"\n")
	}
	if hasScreens {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/core/render\"\n")
	}
	if hasScreens && needs.ui {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/framework/ui\"\n")
	}
	if hasScreens && needs.i18nui {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/framework/i18nui\"\n")
	}
	if hasScreens && needs.headless {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/framework/headless\"\n")
	}
	if hasScreens && needs.uihost {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/framework/uihost\"\n")
	}
	if hasScreens && needs.node {
		// core-ui/noderender is the first-party leaf node renderer (core-ui/html
		// + core/render + core-ui/node only). The node IR + renderer are UI
		// primitives, so the generated app depends on core-ui, never on the
		// experimental kiln namespace.
		sb.WriteString("\tnoderender \"github.com/DonaldMurillo/gofastr/core-ui/noderender\"\n")
		sb.WriteString("\tuinode \"github.com/DonaldMurillo/gofastr/core-ui/node\"\n")
	}
	if withMount || needs.framework {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/framework\"\n")
	}
	sb.WriteString(")\n\n")
}

// appSpecScreenBody emits one screen's type declaration, Screen* methods,
// and Render/RenderCtx: the screen content that is identical whether it
// lands in its own file or in a per-entity crud file.
func appSpecScreenBody(bp appspec.AppSpec, screen appspec.AppSpecScreen, entityMap map[string]framework.EntityDeclaration, apiBase string) string {
	var sb strings.Builder
	typeName := appspec.ToCamelCase(screen.Name) + "Screen"
	hasActions := screenHasActions(screen, entityMap, apiBase)
	// Every screen renders with the request ctx: its words are phrases
	// the request's locale translates. A dynamic route MUST accept its
	// params; the router fails loudly at registration for any dynamic
	// screen lacking SetParams (silently dropped params were a bug class).
	if screenRecordBase(bp, screen) != "" {
		sb.WriteString(fmt.Sprintf("type %s struct {\n\tcomponent.ContextOnly\n\tid, base string\n}\n\n", typeName))
		sb.WriteString(screenSetParams(bp, screen, typeName))
	} else if screenNeedsParams(screen) {
		sb.WriteString(fmt.Sprintf("type %s struct{ component.ContextOnly; id string }\n\n", typeName))
		sb.WriteString(screenSetParams(bp, screen, typeName))
	} else {
		sb.WriteString(fmt.Sprintf("type %s struct{ component.ContextOnly }\n\n", typeName))
	}
	sb.WriteString(fmt.Sprintf("func (s *%s) ScreenTitle(ctx context.Context) string { return %s }\n", typeName, appSpecTx(bp, screen.TitleOrName())))
	if screen.Description != "" {
		sb.WriteString(fmt.Sprintf("func (s *%s) ScreenDescription(ctx context.Context) string { return %s }\n", typeName, appSpecTx(bp, screen.Description)))
	} else {
		// No description in the app spec. A zero-value ScreenSEO is the
		// framework's documented "deliberately no per-page SEO" opt-out,
		// which keeps uihost strict mode passing without inventing filler
		// copy. Replace with a real ScreenDescription when the page has
		// something to say.
		sb.WriteString(fmt.Sprintf("func (s *%s) ScreenSEO(ctx context.Context) uihost.SEO { return uihost.SEO{} } // deliberate SEO opt-out: set description in the app spec or replace with real copy\n", typeName))
	}
	typeConst, _ := appspec.ScreenTypeConst(screen.Type)
	sb.WriteString(fmt.Sprintf("func (s *%s) ScreenType() app.ScreenType { return %s }\n", typeName, typeConst))
	if hasActions {
		sb.WriteString(fmt.Sprintf("func (s *%s) ComponentID() string { return %q }\n", typeName, screenActionComponentID(screen)))
		sb.WriteString(fmt.Sprintf("func (s *%s) Actions() {\n", typeName))
		for _, action := range screenActions(screen, entityMap, apiBase) {
			sb.WriteString(fmt.Sprintf("\tcomponent.On(%q, func(ctx *component.ComponentContext) { _ = ctx }, component.WithClientJS(%q))\n", appSpecActionName(action), action.ClientJS))
		}
		sb.WriteString("}\n")
	}
	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf("func (s *%s) RenderCtx(ctx context.Context) render.HTML {\n", typeName))
	// A screen with blocks stacks them in a ui.Stack at the xl gap, so a
	// page header, a stat grid, a chart card and a table get the design
	// system's vertical rhythm instead of touching (a bare div gave them
	// none). The action marker is runtime wiring that headless refuses
	// from ExtraAttrs, so a screen with actions carries it on an html.Div
	// (core-ui/html, the 1:1 tag primitive) around the stack; an empty
	// screen's root is that div too.
	divCfg := "html.DivConfig{}"
	if hasActions {
		divCfg = "html.DivConfig{ExtraAttrs: html.Attrs{\"data-component\": s.ComponentID()}}"
	}
	if len(screen.Body) == 0 {
		sb.WriteString(fmt.Sprintf("\treturn html.Div(%s, html.Heading(html.HeadingConfig{Level: 1}, render.Text(%s)))\n", divCfg, appSpecTx(bp, screen.TitleOrName())))
	} else {
		closer := "\t)\n"
		if hasActions {
			sb.WriteString(fmt.Sprintf("\treturn html.Div(%s, %s\n", divCfg, AppSpecScreenStackOpen))
			closer = "\t))\n"
		} else {
			sb.WriteString(fmt.Sprintf("\treturn %s\n", AppSpecScreenStackOpen))
		}
		for i, block := range screen.Body {
			var expr string
			switch {
			case appspec.IsEntityListBlock(block):
				expr = appSpecEntityListExpr(bp, screen, block)
			case appspec.IsEntityDetailBlock(block):
				expr = appSpecDetailExpr(bp, screen, block)
			case appspec.IsEntityCreateBlock(block):
				expr = appSpecCreateExpr(bp, block)
			default:
				expr = renderAppSpecBlockForScreen(bp, screen, block, []int{i}, entityMap, apiBase)
			}
			sb.WriteString("\t\t" + expr + ",\n")
		}
		sb.WriteString(closer)
	}
	sb.WriteString("}\n\n")
	return sb.String()
}

// appSpecScreensRegisterGo is the fixed mount seam. It is byte-identical
// for any screen or entity count: it never carries a screen, entity, or app
// name. Add a screen by dropping in a new screen_<name>.go whose init()
// appends to screenRegistrars; RegisterGenerated (app.go) just calls
// mountGenerated.
const appSpecScreensRegisterGo = `package main

import (
	"database/sql"
	"sort"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework"
)

// screenRegistrar pairs a screen's mount func with its declaration order.
// Each screen file appends one or more screenRegistrars in init();
// mountGenerated runs them in declaration order, so the package behaves
// identically regardless of the lexical order Go runs each file's init()
// in. The order field is also how gofastr pack recovers the app spec's
// authored screen order from source.
type screenRegistrar struct {
	order int
	fn    func(fwApp *framework.App, site *app.App, db *sql.DB)
}

var screenRegistrars []screenRegistrar

// mountGenerated mounts every generated screen with site, in declaration
// order. This file never holds a screen or entity name: add a screen by
// dropping in a new screen_<name>.go that appends to screenRegistrars in
// init(). Entity screens render through the appUI builders, so the wiring
// screen_<entity>_crud.go files, never here.
func mountGenerated(fwApp *framework.App, site *app.App, db *sql.DB) {
	sort.SliceStable(screenRegistrars, func(i, j int) bool {
		return screenRegistrars[i].order < screenRegistrars[j].order
	})
	for _, r := range screenRegistrars {
		r.fn(fwApp, site, db)
	}
}
`

// appSpecAdminRegisterGo is the additive seam for admin battery wiring.
// Emitted as admin_register.go whenever the app spec enables the admin
// back-office. main.go builds the base admin.Config (PathPrefix, Title,
// AdminRole, …) and routes it through applyAdminBatteryConfigurators before
// admin.New, so a NEW file (e.g. admin_rbac.go) can wire
// admin.Config{Policy, GrantStore, Auth} from its init() without editing any
// generated file. The package-level rolePolicy and authMgr handles (declared
// in app.go, populated by RegisterGenerated) are how a new file reaches them.
//
// This mirrors the screens seam (screens_register.go + screenRegistrars):
// the registration is data, not code in an owned file.
const appSpecAdminRegisterGo = `package main

import "github.com/DonaldMurillo/gofastr/battery/admin"

// adminBatteryConfigurators mutates the admin battery config in main.go
// before admin.New is called. Append from a new file's init() to wire
// RBAC handles additively: no edits to generated files. The package-level
// rolePolicy and authMgr handles (in app.go) are how a new file reaches them:
//
//	// admin_rbac.go (your file, additive)
//	func init() {
//	    adminBatteryConfigurators = append(adminBatteryConfigurators, func(c *admin.Config) {
//	        c.Policy = rolePolicy
//	        c.Auth = authMgr
//	        // c.GrantStore = … (build with framework.NewGrantStore(db, rolePolicy))
//	    })
//	}
var adminBatteryConfigurators []func(*admin.Config)

// applyAdminBatteryConfigurators applies every registered mutator to cfg, in
// registration order. Called by main.go after RegisterGenerated (so the
// handles exist) and before admin.New (so mutations take effect).
func applyAdminBatteryConfigurators(cfg *admin.Config) {
	for _, c := range adminBatteryConfigurators {
		c(cfg)
	}
}
`

// appSpecScreenSharedGo holds nodeComponent, the shared adapter that lets a
// raw uinode.Node render as a component.Component. Emitted once (in
// screen_shared.go) when any screen renders a node tree, so no per-screen
// file redefines the type. Like the seam, it carries no screen/entity name.
const appSpecScreenSharedGo = `package main

import (
	"github.com/DonaldMurillo/gofastr/core/render"
	noderender "github.com/DonaldMurillo/gofastr/core-ui/noderender"
	uinode "github.com/DonaldMurillo/gofastr/core-ui/node"
)

// nodeComponent adapts a raw uinode.Node so it renders as a component.
// Shared by every screen that mounts a node tree inside an island/widget.
type nodeComponent struct{ node uinode.Node }

func (c nodeComponent) Render() render.HTML { return noderender.RenderTrustedNode(c.node) }
`

// appSpecScreenLayoutExpr resolves a screen's Layout field to the
// package-level layout identifier app.go declares (appLayout / marketingLayout)
// or "nil". Layouts are package-level vars so per-screen mount funcs can
// reference them without app.go naming any screen.
func appSpecScreenLayoutExpr(screen appspec.AppSpecScreen, bp appspec.AppSpec) string {
	switch screen.Layout {
	case "marketing":
		return "marketingLayout"
	case "app":
		if len(bp.Nav) > 0 {
			return "appLayout"
		}
		return "nil"
	default:
		if len(bp.Nav) > 0 {
			return "appLayout"
		}
		return "nil"
	}
}

// AppSpecScreenMountStmt emits the site.Register / site.RegisterScreen
// statement for one screen. Policies (authPolicy/guestPolicy) and layouts
// (appLayout/marketingLayout) are package-level identifiers declared in
// app.go. A detail screen whose entity also has a list screen at <detail
// route minus /{id}> registers as a drawer over that list: navigating to
// the record from the list slides it over, while a hard load or a shared
// link still renders the full page.
func AppSpecScreenMountStmt(screen appspec.AppSpecScreen, bp appspec.AppSpec) string {
	route := appSpecScreenPattern(bp, screen)
	typeName := appspec.ToCamelCase(screen.Name) + "Screen"
	layoutExpr := appSpecScreenLayoutExpr(screen, bp)
	guestRedirect := bp.App.Auth.Enabled && appSpecHasAuthFormScreen(bp)
	// NewScreen does not probe the component the way site.Register does
	// (no ScreenDescriber discovery), so a declared description must be
	// chained explicitly or uihost strict mode fails the boot with
	// `screen %q: no description`.
	withDescription := ""
	if screen.Description != "" {
		withDescription = fmt.Sprintf(".WithDescription(%q)", screen.Description)
	}
	// The drawer intercept: only when this screen is a detail screen whose
	// base route is its entity's list route (validateDetailRoutes enforces
	// the shape; the guard here keeps the emitter honest on its own).
	intercept := ""
	for _, b := range appspec.AppSpecBlocksDeep(screen.Body) {
		if !appspec.IsEntityDetailBlock(b) {
			continue
		}
		entity := strings.Trim(b.Entity, "/")
		base := strings.TrimSuffix(strings.TrimRight(screen.Route, "/"), "/{id}")
		if list := appspec.AppSpecEntityListScreen(bp, entity); list != nil && strings.TrimRight(list.Route, "/") == base {
			intercept = fmt.Sprintf("\trecord.Intercept = &app.Intercept{From: %q, As: app.ScreenDrawer}\n", base)
		}
		break
	}
	build := fmt.Sprintf("app.NewScreen(%q, &%s{}).WithTitle(%q)%s", route, typeName, screen.TitleOrName(), withDescription)
	if intercept != "" {
		// RegisterScreen takes no options, and the intercept is a field:
		// build the screen, set it, register.
		var policy string
		switch {
		case screen.Access.Auth:
			policy = fmt.Sprintf(".WithPolicy(authPolicy(%q, %q))", appSpecLoginRoute(bp), screen.Access.Role)
		case guestRedirect && screenHasAuthForm(screen):
			policy = fmt.Sprintf(".WithPolicy(guestPolicy(%q))", appSpecAppHome(bp))
		}
		return fmt.Sprintf("\trecord := %s%s\n%s\tsite.RegisterScreen(record, %s)", build, policy, intercept, layoutExpr)
	}
	switch {
	case screen.Access.Auth:
		return fmt.Sprintf("\tsite.RegisterScreen(%s.WithPolicy(authPolicy(%q, %q)), %s)", build, appSpecLoginRoute(bp), screen.Access.Role, layoutExpr)
	case guestRedirect && screenHasAuthForm(screen):
		return fmt.Sprintf("\tsite.RegisterScreen(%s.WithPolicy(guestPolicy(%q)), %s)", build, appSpecAppHome(bp), layoutExpr)
	default:
		return fmt.Sprintf("\tsite.Register(%q, &%s{}, %s)", route, typeName, layoutExpr)
	}
}

// appSpecScreenFiles partitions bp's (already-synthesized) screens into
// the per-file generated layout: the fixed screens_register.go seam, one
// screen_<snake>.go per non-CRUD authored screen, and one
// screen_<entity>_crud.go per entity holding that entity's list, record
// and create screens. screenOrderOffset continues authored screen orders
// after an existing project's set (--add).
func appSpecScreenFiles(bp appspec.AppSpec, screenOrderOffset int) []GeneratedFile {
	if len(bp.Screens) == 0 {
		return nil
	}
	entityMap := make(map[string]framework.EntityDeclaration, len(bp.Entities))
	for _, d := range bp.Entities {
		entityMap[d.Name] = d
	}
	apiBase := appSpecAPIBase(bp.App.APIPrefix)
	var files []GeneratedFile
	files = append(files, GeneratedFile{Name: "screens_register.go", Content: appSpecScreensRegisterGo})
	if appSpecScreenImports(bp).node {
		files = append(files, GeneratedFile{Name: "screen_shared.go", Content: appSpecScreenSharedGo})
	}
	// Authored screen order = post-synthesis index (+ offset for --add).
	screenOrder := make(map[string]int, len(bp.Screens))
	for i, s := range bp.Screens {
		screenOrder[s.Name] = i + screenOrderOffset
	}
	// Partition: CRUD screens grouped per entity (declaration order); the
	// rest are standalone.
	crudByEntity := map[string][]appspec.AppSpecScreen{}
	var crudOrder []string // entities that have ≥1 CRUD screen, first-seen order
	var standalone []appspec.AppSpecScreen
	for _, s := range bp.Screens {
		if e, ok := appspec.ScreenEntityRef(s); ok {
			if _, seen := crudByEntity[e]; !seen {
				crudOrder = append(crudOrder, e)
			}
			crudByEntity[e] = append(crudByEntity[e], s)
		} else {
			standalone = append(standalone, s)
		}
	}
	for _, s := range standalone {
		files = append(files, renderAppSpecStandaloneScreenFile(s, bp, entityMap, apiBase, screenOrder[s.Name]))
	}
	for _, e := range crudOrder {
		files = append(files, renderAppSpecCrudFile(e, crudByEntity[e], bp, entityMap, apiBase, screenOrder))
	}
	return files
}

func renderAppSpecStandaloneScreenFile(screen appspec.AppSpecScreen, bp appspec.AppSpec, entityMap map[string]framework.EntityDeclaration, apiBase string, order int) GeneratedFile {
	// Identifier gate: toCamelCase transforms, it does not validate, and
	// the emitter serves unvalidated renders (tests, --add fragments).
	// Never emit a type/mount declaration that is not a Go identifier —
	// same rule as the endpoint/hook stub arms. validateAppSpec is the
	// loud gate; a screen that fails here vanishes from this file only.
	if !screenGoIdentifiers(screen.Name) {
		return GeneratedFile{Name: appspec.ScreenFileName(screen.Name), Content: "package main\n"}
	}
	var sb strings.Builder
	sb.WriteString("package main\n\n")
	bp.SetPhraseCollector(appSpecScreenPhrases(bp.App.Name, screen.Name, appSpecScreenScope(bp, screen.Name)))
	body := appSpecScreenBody(bp, screen, entityMap, apiBase)
	needs := appSpecScreensImportNeeds(bp, []appspec.AppSpecScreen{screen}, entityMap, apiBase)
	needs.readCode(body, bp.PhraseCollector())
	writeScreenImportBlock(&sb, needs, true, true)
	sb.WriteString(bp.PhraseCollector().Block())
	sb.WriteString(body)
	mountName := "mount" + appspec.ToCamelCase(screen.Name) + "Screen"
	sb.WriteString(fmt.Sprintf("// %s mounts the %s screen with site.\nfunc %s(fwApp *framework.App, site *app.App, db *sql.DB) {\n%s\n}\n\n", mountName, screen.Name, mountName, AppSpecScreenMountStmt(screen, bp)))
	sb.WriteString(fmt.Sprintf("func init() {\n\tscreenRegistrars = append(screenRegistrars, screenRegistrar{order: %d, fn: %s})\n}\n", order, mountName))
	return GeneratedFile{Name: appspec.ScreenFileName(screen.Name), Content: sb.String()}
}

// screenGoIdentifiers reports whether the screen's derived type name
// (<Name>Screen) and mount func name (mount<Name>Screen) are Go
// identifiers.
func screenGoIdentifiers(name string) bool {
	typeName := appspec.ToCamelCase(name) + "Screen"
	return project.IsGoIdentifier(typeName) && project.IsGoIdentifier("mount"+typeName)
}

// renderAppSpecCrudFile emits screen_<entity>_crud.go: the entity's
// list, record and create screens (in declaration order) and one mount
// func per screen, plus one init() registering every mount func at its
// authored order. The screens render through appUI builders, which resolve
// the entity at render time, so the file carries no per-entity wiring.
func renderAppSpecCrudFile(entity string, screens []appspec.AppSpecScreen, bp appspec.AppSpec, entityMap map[string]framework.EntityDeclaration, apiBase string, screenOrder map[string]int) GeneratedFile {
	var sb strings.Builder
	sb.WriteString("package main\n\n")
	// Identifier gate, same rule as the standalone screen file and the
	// endpoint/hook stub arms: skip screens whose derived type/mount
	// names are not Go identifiers (validateAppSpec is the loud gate).
	// Imports are computed over the survivors only, or a skipped screen
	// would leave unused imports behind.
	kept := make([]appspec.AppSpecScreen, 0, len(screens))
	for _, s := range screens {
		if screenGoIdentifiers(s.Name) {
			kept = append(kept, s)
		}
	}
	screens = kept
	bp.SetPhraseCollector(appSpecCrudPhrases(bp.App.Name, entity, appSpecCrudScope(bp, entity)))
	var bodies strings.Builder
	for _, s := range screens {
		bodies.WriteString(appSpecScreenBody(bp, s, entityMap, apiBase))
	}
	needs := appSpecScreensImportNeeds(bp, screens, entityMap, apiBase)
	needs.readCode(bodies.String(), bp.PhraseCollector())
	writeScreenImportBlock(&sb, needs, true, len(screens) > 0)
	sb.WriteString(bp.PhraseCollector().Block())
	sb.WriteString(bodies.String())
	// Mount funcs in authored (declaration) order.
	ordered := append([]appspec.AppSpecScreen(nil), screens...)
	sort.SliceStable(ordered, func(i, j int) bool { return screenOrder[ordered[i].Name] < screenOrder[ordered[j].Name] })
	var initLines []string
	for _, s := range ordered {
		mountName := "mount" + appspec.ToCamelCase(s.Name) + "Screen"
		sb.WriteString(fmt.Sprintf("func %s(fwApp *framework.App, site *app.App, db *sql.DB) {\n", mountName))
		sb.WriteString(AppSpecScreenMountStmt(s, bp))
		sb.WriteString("\n}\n\n")
		initLines = append(initLines, fmt.Sprintf("\t\tscreenRegistrar{order: %d, fn: %s},", screenOrder[s.Name], mountName))
	}
	sb.WriteString("func init() {\n\tscreenRegistrars = append(screenRegistrars,\n" + strings.Join(initLines, "\n") + "\n\t)\n}\n")
	return GeneratedFile{Name: appspec.ScreenFileName(entity + "_crud"), Content: sb.String()}
}

// screenRecordBase returns the Go expression, over SetParams' p, for the
// base a record screen's page sits under when route segments follow its
// record param, else "". The record page otherwise takes its base from
// the request path less one segment per key column, which on
// /posts/{id}/edit is /posts/1, and saving there lands on /posts/1/1.
// The base is the route up to the record param, its params escaped the
// way the record page links: one segment per value.
func screenRecordBase(bp appspec.AppSpec, screen appspec.AppSpecScreen) string {
	if detail, edit := appspec.ScreenNeedsRouteID(screen); !detail && !edit {
		return ""
	}
	segs := strings.Split(strings.Trim(screen.Route, "/"), "/")
	at := -1
	for i, seg := range segs {
		if name, ok := appspec.RouteSegParam(seg); ok && name == appspec.ScreenParamName(screen) {
			at = i
		}
	}
	if at < 0 || at == len(segs)-1 {
		return ""
	}
	// A record param first in the route (/{id}/edit) bases at the root.
	if at == 0 {
		return `"/"`
	}
	// Each segment escapes on its own: router params arrive decoded, and
	// a "?" or "/" in one must stay inside its segment of the base.
	parts := make([]string, at)
	for i, seg := range segs[:at] {
		if name, ok := appspec.RouteSegParam(seg); ok {
			parts[i] = fmt.Sprintf(`"/" + url.PathEscape(p[%q])`, name)
		} else {
			parts[i] = fmt.Sprintf("%q", "/"+url.PathEscape(seg))
		}
	}
	return strings.Join(parts, " + ")
}

// appSpecScreenPattern is the pattern a screen mounts at: its route in
// the screen router's colon style, with the record param expanded to one
// segment per key column when the record has a composite key.
func appSpecScreenPattern(bp appspec.AppSpec, screen appspec.AppSpecScreen) string {
	route := appSpecScreenRoutePath(screen.Route)
	keys := appspec.ScreenRecordKeys(bp, screen)
	if keys == nil {
		return route
	}
	param := appspec.ScreenParamName(screen)
	segs := strings.Split(route, "/")
	for i, seg := range segs {
		if names := appspec.RouteParamNames(seg); len(names) == 1 && names[0] == param {
			segs[i] = ":" + strings.Join(keys, "/:")
			break
		}
	}
	return strings.Join(segs, "/")
}

// screenSetParams emits the SetParams a dynamic screen declares: s.id is
// the record param, or for a composite-key record the record id joined
// from one param per key column.
func screenSetParams(bp appspec.AppSpec, screen appspec.AppSpecScreen, typeName string) string {
	id := fmt.Sprintf("p[%q]", appspec.ScreenParamName(screen))
	if keys := appspec.ScreenRecordKeys(bp, screen); keys != nil {
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = fmt.Sprintf("p[%q]", k)
		}
		id = fmt.Sprintf("framework.JoinKey([]string{%s})", strings.Join(parts, ", "))
	}
	if base := screenRecordBase(bp, screen); base != "" {
		return fmt.Sprintf("func (s *%s) SetParams(p map[string]string) {\n\ts.id = %s\n\ts.base = %s\n}\n", typeName, id, base)
	}
	return fmt.Sprintf("func (s *%s) SetParams(p map[string]string) { s.id = %s }\n", typeName, id)
}

// screenNeedsParams reports whether a screen reads a route param (either
// supported syntax). An entity_detail or edit form at any depth needs one
// too, and validation refuses such a screen whose route has no {id}, so
// the route alone answers.
func screenNeedsParams(screen appspec.AppSpecScreen) bool {
	return len(appspec.RouteParamNames(screen.Route)) > 0
}

// appSpecDetailExpr emits the record render expression for an
// entity_detail block. The record page holds the edit form and the move
// buttons come from the entity's states, so there is nothing to configure
// here beyond turning on the two record actions a generated CRUD screen
// wants: delete and duplicate (off by default on app pages).
func appSpecDetailExpr(bp appspec.AppSpec, screen appspec.AppSpecScreen, block appspec.AppSpecBlock) string {
	entity := strings.Trim(block.Entity, "/")
	return fmt.Sprintf("appUI.Record(%q, s.id)%s.Delete().Duplicate().RenderCtx(ctx)", entity, recordBaseCall(bp, screen))
}

// recordBaseCall is the .Base(s.base) a record screen passes when its
// route continues past the record param (screenRecordBase), else "".
func recordBaseCall(bp appspec.AppSpec, screen appspec.AppSpecScreen) string {
	if screenRecordBase(bp, screen) == "" {
		return ""
	}
	return ".Base(s.base)"
}

// appSpecEditExpr emits an edit form's render expression: the record
// page, which loads the record the route names into its form and saves
// it back, read-only on its key. A fields allowlist omits the rest.
func appSpecEditExpr(bp appspec.AppSpec, screen appspec.AppSpecScreen, block appspec.AppSpecBlock, entityMap map[string]framework.EntityDeclaration) string {
	entity := strings.Trim(block.Entity, "/")
	expr := fmt.Sprintf("appUI.Record(%q, s.id)%s", entity, recordBaseCall(bp, screen))
	if len(block.Fields) > 0 {
		var omit []string
		for _, f := range entityMap[entity].Fields {
			if !slices.Contains(block.Fields, f.Name) {
				omit = append(omit, fmt.Sprintf("%q", f.Name))
			}
		}
		if len(omit) > 0 {
			expr += ".Omit(" + strings.Join(omit, ", ") + ")"
		}
	}
	return expr + ".RenderCtx(ctx)"
}

// appSpecCreateExpr emits the create screen's render expression: the
// record form empty, posting a create, with Back and the after-save
// navigation landing on the list. Base is the list screen's route, where
// the synthesized <list>/create screen lives under.
func appSpecCreateExpr(bp appspec.AppSpec, block appspec.AppSpecBlock) string {
	entity := strings.Trim(block.Entity, "/")
	if list := appspec.AppSpecEntityListScreen(bp, entity); list != nil {
		return fmt.Sprintf("appUI.Create(%q).Base(%q).RenderCtx(ctx)", entity, strings.TrimRight(list.Route, "/"))
	}
	return fmt.Sprintf("appUI.Create(%q).RenderCtx(ctx)", entity)
}

// appSpecEntityListExpr emits the appUI.List builder for one entity_list
// block, at any nesting depth. The builder renders with the request
// context, reads through the entity's own gates, and keeps its sort, page
// and filter in the page's query string: there is no island to mount and
// no second route onto the rows.
func appSpecEntityListExpr(bp appspec.AppSpec, screen appspec.AppSpecScreen, block appspec.AppSpecBlock) string {
	entity := strings.Trim(block.Entity, "/")
	expr := fmt.Sprintf("appUI.List(%q)", entity)
	// Two lists on one screen namespace their query params by key; the
	// entity name is unique per screen (validateAppSpec refuses a
	// repeat). One list needs no key.
	if countEntityListBlocks(screen) > 1 {
		expr += fmt.Sprintf(".Key(%q)", entity)
	}
	if len(block.Fields) > 0 {
		args := make([]string, len(block.Fields))
		for i, f := range block.Fields {
			args[i] = fmt.Sprintf("%q", f)
		}
		expr += ".Columns(" + strings.Join(args, ", ") + ")"
	}
	if mode := strings.ToLower(strings.TrimSpace(block.Mode)); mode != "" {
		expr += fmt.Sprintf(".As(%q)", mode)
	}
	if block.Limit > 0 {
		expr += fmt.Sprintf(".PageSize(%d)", block.Limit)
	}
	if !block.Create {
		expr += ".NoCreate()"
	}
	// No record screen means no page at <list route>/<id>: the rows'
	// record links and their row menu's View would each be a 404, so the
	// rows draw as text. A writable list keeps them: NoLinks also takes
	// New away, and its create screen is synthesized beside the list.
	noLinks := !block.Create && !appspec.AppSpecHasRecordScreen(bp, entity)
	if noLinks {
		expr += ".NoLinks()"
	}
	if block.Bulk {
		expr += ".Bulk()"
	}
	if block.Text != "" {
		// A list under an earlier <h1> (a dashboard's page header) is a
		// section of that page, so its title drops to level 2. No layout
		// renders an <h1> of its own, so otherwise the list's title is it.
		level := 1
		if h1, _ := appSpecH1Before(screen.Body, entity); h1 {
			level = 2
		}
		expr += fmt.Sprintf(".Heading(%s, %d)", appSpecTx(bp, block.Text), level)
	}
	if block.EmptyText != "" {
		expr += fmt.Sprintf(".Empty(%s)", appSpecTx(bp, block.EmptyText))
	}
	// Off the entity's home screen the rows still link to the home list,
	// where the detail and create screens sit; the default base is the
	// current path, which has neither.
	if home := appspec.AppSpecEntityListScreen(bp, entity); home != nil && !noLinks {
		if base := strings.TrimRight(home.Route, "/"); base != "" && base != strings.TrimRight(screen.Route, "/") {
			expr += fmt.Sprintf(".Base(%q)", base)
		}
	}
	// The builder is a component; the screen's stack takes HTML.
	return expr + ".RenderCtx(ctx)"
}

// countEntityListBlocks counts the entity_list blocks on a screen at any
// nesting depth.
func countEntityListBlocks(screen appspec.AppSpecScreen) int {
	var n int
	for _, b := range appspec.AppSpecBlocksDeep(screen.Body) {
		if appspec.IsEntityListBlock(b) {
			n++
		}
	}
	return n
}

type screenImportNeeds struct {
	component   bool
	island      bool
	html        bool
	interactive bool
	node        bool
	ui          bool
	// headless: a form field builder names headless.FieldControl (the
	// auth and entity form emitters build ui.Control inside one).
	headless bool
	// uihost: any screen without an app spec description emits the
	// zero-value ScreenSEO opt-out, whose SEO type lives in uihost.
	uihost bool
	// i18n: the file declares phrases. i18nui: the code reads the
	// framework's own phrases or catalog lookups (form labels, auth
	// fields).
	i18n   bool
	i18nui bool
	// framework: a composite-key record screen joins its key params.
	framework bool
	// url: a record screen escapes the route params of its base.
	url bool
}

// readCode sets the needs the emitted code itself decides: which block
// emitters reached for html.*, i18nui.*, and whether the file says any
// phrase.
func (n *screenImportNeeds) readCode(code string, p *appspec.AppSpecPhrases) {
	n.html = emitsHTMLRef(code)
	n.i18nui = emitsPkgRef(code, "i18nui")
	n.framework = emitsPkgRef(code, "framework")
	n.url = emitsPkgRef(code, "url")
	n.i18n = p.Used()
}

func appSpecScreenImports(bp appspec.AppSpec) screenImportNeeds {
	entityMap := make(map[string]framework.EntityDeclaration, len(bp.Entities))
	for _, decl := range bp.Entities {
		entityMap[decl.Name] = decl
	}
	return appSpecScreensImportNeeds(bp, bp.Screens, entityMap, appSpecAPIBase(bp.App.APIPrefix))
}

// appSpecScreensImportNeeds computes the import set for an arbitrary
// subset of screens (one standalone file, one crud file's screens, or the
// whole project). It is the per-file analogue of appSpecScreenImports.
func appSpecScreensImportNeeds(bp appspec.AppSpec, screens []appspec.AppSpecScreen, entityMap map[string]framework.EntityDeclaration, apiBase string) screenImportNeeds {
	var needs screenImportNeeds
	for _, screen := range screens {
		// A screen with blocks stacks them in ui.Stack. (The html import
		// is not decided here: the callers read it off the emitted code,
		// see emitsHTMLRef.)
		if len(screen.Body) > 0 {
			needs.ui = true
		}
		if screen.Description == "" {
			needs.uihost = true
		}
		if screenHasActions(screen, entityMap, apiBase) {
			needs.component = true
		}
		// Walk the body: catalog blocks (framework/ui) and their children are
		// rendered via ui.*/html.*; node blocks render as a uinode tree (don't
		// recurse their needs into children); server-side entity blocks need
		// nothing from the screen.
		var scan func([]appspec.AppSpecBlock, bool)
		scan = func(blocks []appspec.AppSpecBlock, top bool) {
			for _, block := range blocks {
				kind := block.Kind
				if kind == "" {
					kind = block.Type
				}
				if isLoginFormBlock(block) || isSignupFormBlock(block) {
					// Auth forms compose ui.AuthCard + ui.Form + FormField
					// builders with ui.Control.
					needs.ui = true
					needs.headless = true
					continue
				}
				if appspec.IsEditFormBlock(block) {
					continue
				}
				if appspec.IsEntityFormBlock(block) {
					// Entity forms compose ui.Form with the typed fields,
					// with the data-cui-rpc wiring built via
					// interactive.*.Attrs(); a field none of them names
					// builds ui.Control inside a FormField builder.
					needs.ui = true
					needs.interactive = true
					needs.headless = true
					continue
				}
				if appspec.IsEntityListBlock(block) || appspec.IsEntityDetailBlock(block) || appspec.IsEntityCreateBlock(block) {
					// Entity screens render through the appUI builders,
					// package-level in the app: no import needed here.
					continue
				}
				if appspec.AppSpecCatalogKind(kind) {
					needs.ui = true
					scan(block.Children, false)
					continue
				}
				if appspec.AppSpecBlockUsesNodeRenderer(block) {
					needs.node = true
					if block.Island != "" {
						needs.island = true
					}
					if block.Widget != "" {
						needs.component = true
					}
					continue
				}
				if strings.EqualFold(strings.TrimSpace(block.Type), "link") {
					needs.ui = true
				}
			}
		}
		scan(screen.Body, true)
	}
	return needs
}

func screenHasActions(screen appspec.AppSpecScreen, entityMap map[string]framework.EntityDeclaration, apiBase string) bool {
	return len(screenActions(screen, entityMap, apiBase)) > 0
}

func screenActions(screen appspec.AppSpecScreen, entityMap map[string]framework.EntityDeclaration, apiBase string) []appspec.AppSpecAction {
	var actions []appspec.AppSpecAction
	var walk func([]appspec.AppSpecBlock, []int)
	walk = func(blocks []appspec.AppSpecBlock, path []int) {
		for i, block := range blocks {
			blockPath := append(append([]int(nil), path...), i)
			actions = append(actions, block.Actions...)
			switch {
			case appspec.IsEntityListBlock(block), appspec.IsEntityDetailBlock(block), appspec.IsEditFormBlock(block):
				// Entity lists/details are server-rendered via the resource
				// engine at every nesting level, no client island, no action.
			case appspec.IsEntityFormBlock(block):
				// Only forms with relation fields need a mount action to
				// fetch <select> options; submission itself is wired via
				// data-cui-rpc on the form element (no component action).
				if appSpecFormHasRelation(block, entityMap) {
					actions = append(actions, appspec.AppSpecAction{
						Name:     appSpecEntityFormActionName(screen, block, blockPath),
						ClientJS: appSpecEntityFormClientJS(block, entityMap, apiBase),
					})
				}
			}
			walk(block.Children, blockPath)
		}
	}
	walk(screen.Body, nil)
	return actions
}

// appSpecFormHasRelation reports whether the entity behind an entity_form
// block has at least one editable relation field (which renders as a
// client-populated <select>).
func appSpecFormHasRelation(block appspec.AppSpecBlock, entityMap map[string]framework.EntityDeclaration) bool {
	decl, ok := entityMap[strings.Trim(block.Entity, "/")]
	if !ok {
		return false
	}
	for _, field := range decl.Fields {
		if appSpecFormFieldSkipped(field, block.Fields) {
			continue
		}
		if strings.EqualFold(field.Type, "relation") && field.To != "" {
			return true
		}
	}
	return false
}

func screenActionComponentID(screen appspec.AppSpecScreen) string {
	id := strings.ToLower(appspec.ToCamelJSON(screen.Name))
	id = strings.NewReplacer("_", "-", " ", "-", "/", "-", ":", "").Replace(id)
	id = strings.Trim(id, "-")
	if id == "" {
		id = "screen"
	}
	return "screen-" + id
}

func appSpecActionName(action appspec.AppSpecAction) string {
	name := strings.TrimSpace(action.Name)
	if name != "" {
		return name
	}
	return appspec.AppSpecActionEvent(action)
}

func appSpecGapExpr(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "none":
		return "ui.GapNone"
	case "xs":
		return "ui.GapXS"
	case "sm":
		return "ui.GapSM"
	case "lg":
		return "ui.GapLG"
	case "xl":
		return "ui.GapXL"
	case "2xl":
		return "ui.Gap2XL"
	default:
		return "ui.GapMD"
	}
}

// appSpecAxisFields emits a stack's or cluster's Align and Justify
// only when the block sets them. An unset prop leaves the component's
// own default: an explicit AlignStart on every stack shrank each child
// to its content, so a pricing grid inside one held a single column.
func appSpecAxisFields(block appspec.AppSpecBlock) string {
	out := ""
	if v := appspec.AppSpecProp(block, "align"); strings.TrimSpace(v) != "" {
		out += ", Align: " + appSpecAlignExpr(v)
	}
	if v := appspec.AppSpecProp(block, "justify"); strings.TrimSpace(v) != "" {
		out += ", Justify: " + appSpecJustifyExpr(v)
	}
	return out
}

func appSpecAlignExpr(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "center":
		return "ui.AlignCenter"
	case "end":
		return "ui.AlignEnd"
	case "baseline":
		return "ui.AlignBaseline"
	case "stretch":
		return "ui.AlignStretch"
	default:
		return "ui.AlignStart"
	}
}

func appSpecJustifyExpr(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "center":
		return "ui.JustifyCenter"
	case "end":
		return "ui.JustifyEnd"
	case "between":
		return "ui.JustifyBetween"
	case "around":
		return "ui.JustifyAround"
	default:
		return "ui.JustifyStart"
	}
}

// appSpecSectionChildrenAreCards reports whether every child of a section is
// a card/stat_card, the only kinds that belong in the responsive card grid.
// Mixed or non-card children flow in a left-aligned stack instead so a single
// CTA button doesn't stretch the full column width.
// appSpecCardHeadingLevel returns the CardConfig field that sets a
// generated card's heading level. ui.Card defaults to h3, which is right
// under a titled section's h2 but skips a level directly under the page h1
// (axe heading-order on every dashboard). A card with no titled ancestor
// (a section or card carrying a heading) says HeadingLevel: 2; a nested one
// keeps the default.
func appSpecCardHeadingLevel(screen appspec.AppSpecScreen, path []int) string {
	blocks := screen.Body
	for depth, i := range path {
		if depth == len(path)-1 || i < 0 || i >= len(blocks) {
			break
		}
		ancestor := blocks[i]
		switch strings.ToLower(strings.TrimSpace(ancestor.Kind)) {
		case "section", "card":
			if appspec.AppSpecProp(ancestor, "heading") != "" {
				return ""
			}
		}
		blocks = ancestor.Children
	}
	return ", HeadingLevel: 2"
}

func appSpecSectionChildrenAreCards(children []appspec.AppSpecBlock) bool {
	if len(children) == 0 {
		return false
	}
	for _, c := range children {
		switch strings.ToLower(strings.TrimSpace(c.Kind)) {
		case "card", "stat_card":
		default:
			return false
		}
	}
	return true
}

// renderAppSpecCatalogBlock emits a framework/ui component for a catalog block
// kind (page_header, hero, section, card, stat_card, charts, …). Returns
// (expr, true) when handled; ("", false) to fall through to other paths.
func renderAppSpecCatalogBlock(bp appspec.AppSpec, screen appspec.AppSpecScreen, block appspec.AppSpecBlock, path []int, entityMap map[string]framework.EntityDeclaration, apiBase string) (string, bool) {
	kind := strings.ToLower(strings.TrimSpace(block.Kind))
	tx := func(s string) string { return appSpecTx(bp, s) }
	childExprs := func() string {
		parts := make([]string, 0, len(block.Children))
		for i, ch := range block.Children {
			cp := append(append([]int(nil), path...), i)
			parts = append(parts, renderAppSpecBlockForScreen(bp, screen, ch, cp, entityMap, apiBase))
		}
		return strings.Join(parts, ", ")
	}
	if appspec.AppSpecControlKind(kind) {
		var children []string
		for i, ch := range block.Children {
			cp := append(append([]int(nil), path...), i)
			children = append(children, renderAppSpecBlockForScreen(bp, screen, ch, cp, entityMap, apiBase))
		}
		return renderAppSpecControlBlock(bp, block, children)
	}
	switch kind {
	case "stack":
		cfg := fmt.Sprintf("ui.StackConfig{Gap: %s%s}", appSpecGapExpr(appspec.AppSpecProp(block, "gap")), appSpecAxisFields(block))
		children := childExprs()
		if children == "" {
			return "ui.Stack(" + cfg + ")", true
		}
		return "ui.Stack(" + cfg + ", " + children + ")", true
	case "cluster":
		cfg := fmt.Sprintf("ui.ClusterConfig{Gap: %s%s, NoWrap: %t}", appSpecGapExpr(appspec.AppSpecProp(block, "gap")), appSpecAxisFields(block), appspec.AppSpecBoolProp(block, "no_wrap"))
		children := childExprs()
		if children == "" {
			return "ui.Cluster(" + cfg + ")", true
		}
		return "ui.Cluster(" + cfg + ", " + children + ")", true
	case "grid", "stat_grid":
		min := appspec.AppSpecProp(block, "min")
		if min == "" {
			if kind == "stat_grid" {
				min = "12rem"
			} else {
				min = "16rem"
			}
		}
		cfg := fmt.Sprintf("ui.GridConfig{Min: %q, Gap: %s}", min, appSpecGapExpr(appspec.AppSpecProp(block, "gap")))
		children := childExprs()
		if children == "" {
			return "ui.Grid(" + cfg + ")", true
		}
		return "ui.Grid(" + cfg + ", " + children + ")", true
	case "page_header":
		cfg := fmt.Sprintf("ui.PageHeaderConfig{Title: %s, Subtitle: %s, Eyebrow: %s}", tx(appspec.AppSpecProp(block, "title")), tx(appspec.AppSpecProp(block, "subtitle")), tx(appspec.AppSpecProp(block, "eyebrow")))
		return "ui.PageHeader(" + cfg + ")", true
	case "hero":
		title := appspec.AppSpecProp(block, "title")
		var ctas []string
		if t := appspec.AppSpecProp(block, "cta_text"); t != "" {
			ctas = append(ctas, fmt.Sprintf("ui.LinkButton(ui.LinkButtonConfig{Label: %s, Href: %q, Variant: ui.ButtonPrimary})", tx(t), appspec.AppSpecProp(block, "cta_href")))
		}
		if t := appspec.AppSpecProp(block, "secondary_text"); t != "" {
			ctas = append(ctas, fmt.Sprintf("ui.LinkButton(ui.LinkButtonConfig{Label: %s, Href: %q, Variant: ui.ButtonSecondary})", tx(t), appspec.AppSpecProp(block, "secondary_href")))
		}
		actions := ""
		if len(ctas) > 0 {
			actions = ", Actions: []render.HTML{" + strings.Join(ctas, ", ") + "}"
		}
		image := appspec.AppSpecProp(block, "image")
		if image == "" {
			image = appspec.AppSpecProp(block, "media")
		}
		media := ""
		if image != "" {
			media = fmt.Sprintf(", Media: html.Image(html.ImageConfig{Src: %q, Alt: %s})", image, tx(title))
		}
		return fmt.Sprintf("ui.Hero(ui.HeroConfig{Eyebrow: %s, Title: %s, Subtitle: %s%s%s})", tx(appspec.AppSpecProp(block, "eyebrow")), tx(title), tx(appspec.AppSpecProp(block, "subtitle")), actions, media), true
	case "section":
		cfg := fmt.Sprintf("ui.SectionConfig{Ctx: ctx, Heading: %s, Eyebrow: %s, Description: %s, Label: %s, Class: %q, ID: %q}", tx(appspec.AppSpecProp(block, "heading")), tx(appspec.AppSpecProp(block, "eyebrow")), tx(appspec.AppSpecProp(block, "description")), tx(appspec.AppSpecProp(block, "label")), appspec.AppSpecProp(block, "class"), appspec.AppSpecProp(block, "id"))
		if len(block.Children) > 0 {
			// Card-like children flow in a responsive ui.Grid; anything else
			// (a CTA button, prose) flows in a left-aligned ui.Stack so it keeps
			// its natural width instead of stretching across the column.
			wrap := "ui.Stack(ui.StackConfig{Align: ui.AlignStart}, " + childExprs() + ")"
			if appSpecSectionChildrenAreCards(block.Children) {
				wrap = "ui.Grid(ui.GridConfig{Min: \"16rem\"}, " + childExprs() + ")"
			}
			return "ui.Section(" + cfg + ", " + wrap + ")", true
		}
		return "ui.Section(" + cfg + ")", true
	case "card":
		cfg := fmt.Sprintf("ui.CardConfig{Heading: %s, Description: %s%s}", tx(appspec.AppSpecProp(block, "heading")), tx(appspec.AppSpecProp(block, "text")), appSpecCardHeadingLevel(screen, path))
		if len(block.Children) > 0 {
			return "ui.Card(" + cfg + ", " + childExprs() + ")", true
		}
		return "ui.Card(" + cfg + ")", true
	case "stat_row":
		return "ui.Grid(ui.GridConfig{Min: \"12rem\"}, " + childExprs() + ")", true
	case "stat_card":
		label := appspec.AppSpecProp(block, "label")
		if src, ok := block.Props["source"].(map[string]any); ok {
			entity, _ := src["entity"].(string)
			agg, _ := src["agg"].(string)
			field, _ := src["field"].(string)
			filter, _ := src["filter"].(string)
			format := appspec.AppSpecProp(block, "format")
			return fmt.Sprintf("ui.StatCard(ui.StatCardConfig{Label: %s, Value: appUI.StatValue(ctx, %q, %q, %q, %q, %q)})", tx(label), entity, agg, field, filter, format), true
		}
		return fmt.Sprintf("ui.StatCard(ui.StatCardConfig{Label: %s, Value: %q})", tx(label), appspec.AppSpecProp(block, "value")), true
	case "bar_chart", "pie_chart", "line_chart":
		title := appspec.AppSpecProp(block, "title")
		if src, ok := block.Props["source"].(map[string]any); ok {
			entity, _ := src["entity"].(string)
			groupBy, _ := src["group_by"].(string)
			var chart string
			switch kind {
			case "pie_chart":
				chart = fmt.Sprintf("ui.PieChart(ui.PieChartConfig{Ctx: ctx, Slices: appUI.GroupSlices(ctx, %q, %q)})", entity, groupBy)
			case "line_chart":
				chart = fmt.Sprintf("appUI.LineChart(ctx, %q, %q)", entity, groupBy)
			default:
				chart = fmt.Sprintf("ui.BarChart(ui.BarChartConfig{Ctx: ctx, Bars: appUI.GroupBars(ctx, %q, %q), ShowLabels: true})", entity, groupBy)
			}
			// A titled chart is a Card with a heading; design-system
			// composition, zero bespoke classes (Hard rule 7).
			if title != "" {
				return fmt.Sprintf("ui.Card(ui.CardConfig{Heading: %s%s}, %s)", tx(title), appSpecCardHeadingLevel(screen, path), chart), true
			}
			return chart, true
		}
		return "", false
	case "link_button":
		v := "ui.ButtonPrimary"
		switch appspec.AppSpecProp(block, "variant") {
		case "secondary":
			v = "ui.ButtonSecondary"
		case "ghost":
			v = "ui.ButtonGhost"
		}
		return fmt.Sprintf("ui.LinkButton(ui.LinkButtonConfig{Label: %s, Href: %q, Variant: %s})", tx(appspec.AppSpecProp(block, "label")), appspec.AppSpecProp(block, "href"), v), true
	case "callout":
		return fmt.Sprintf("ui.Callout(ui.CalloutConfig{Title: %s}, render.Text(%s))", tx(appspec.AppSpecProp(block, "title")), tx(block.Text)), true
	case "divider":
		return "ui.Divider(ui.DividerConfig{})", true
	case "markdown":
		return fmt.Sprintf("ui.Markdown(ui.MarkdownConfig{Ctx: ctx, Source: %s, Measure: true})", tx(block.Text)), true
	case "pricing":
		plans, _ := block.Props["plans"].([]any)
		cards := make([]string, 0, len(plans))
		for _, p := range plans {
			if pm, ok := p.(map[string]any); ok {
				cards = append(cards, appSpecPricingCardExpr(bp, pm))
			}
		}
		return "ui.Grid(ui.GridConfig{Min: \"16rem\"}, " + strings.Join(cards, ", ") + ")", true
	}
	return "", false
}

// appSpecPricingCardExpr emits a ui.PricingCard call from a plan map.
func appSpecPricingCardExpr(bp appspec.AppSpec, p map[string]any) string {
	s := func(k string) string { v, _ := p[k].(string); return v }
	tx := func(k string) string { return appSpecTx(bp, s(k)) }
	feats := ""
	if fl, ok := p["features"].([]any); ok && len(fl) > 0 {
		qs := make([]string, 0, len(fl))
		for _, f := range fl {
			if fs, ok := f.(string); ok {
				qs = append(qs, appSpecTx(bp, fs))
			}
		}
		feats = ", Features: []string{" + strings.Join(qs, ", ") + "}"
	}
	featured := ""
	if b, _ := p["featured"].(bool); b {
		featured = ", Featured: true"
	}
	return fmt.Sprintf("ui.PricingCard(ui.PricingCardConfig{Ctx: ctx, Name: %s, Price: %q, Period: %s, Description: %s%s, CTALabel: %s, CTAHref: %q%s})",
		tx("name"), s("price"), tx("period"), tx("description"), feats, tx("cta_text"), s("cta_href"), featured)
}

func renderAppSpecBlockForScreen(bp appspec.AppSpec, screen appspec.AppSpecScreen, block appspec.AppSpecBlock, path []int, entityMap map[string]framework.EntityDeclaration, apiBase string) string {
	kind := block.Kind
	if kind == "" {
		kind = block.Type
	}
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "login_form":
		return renderAppSpecLoginFormExpr(bp, block)
	case "signup_form":
		return renderAppSpecSignupFormExpr(bp, block)
	case "entity_form":
		if appspec.IsEditFormBlock(block) {
			return appSpecEditExpr(bp, screen, block, entityMap)
		}
		// Island form composed from framework components; its labels
		// translate through the request ctx.
		return appSpecEntityFormExpr(bp, screen, block, path, entityMap, apiBase)
	case "entity_detail":
		// Server-rendered via the resource engine (s.id from the route param).
		return appSpecDetailExpr(bp, screen, block)
	}
	if appspec.IsEntityListBlock(block) {
		// Server-rendered via the resource engine (ui.DataTable).
		return appSpecEntityListExpr(bp, screen, block)
	}
	if expr, ok := renderAppSpecCatalogBlock(bp, screen, block, path, entityMap, apiBase); ok {
		return expr
	}
	if appspec.AppSpecBlockUsesNodeRenderer(block) {
		expr := renderAppSpecNodeExpressionForScreen(bp, screen, block, path, entityMap, apiBase)
		if block.Island != "" {
			return fmt.Sprintf("island.NewIsland(%q, nodeComponent{node: %s}).Render()", block.Island, expr)
		}
		if block.Widget != "" {
			return fmt.Sprintf("component.NewWidget(%q, nodeComponent{node: %s}).Render()", block.Widget, expr)
		}
		return "noderender.RenderTrustedNode(" + expr + ")"
	}
	attrs := "nil"
	if block.Class != "" {
		attrs = fmt.Sprintf("map[string]string{\"class\": %q}", block.Class)
	}
	switch strings.ToLower(block.Type) {
	case "", "text", "p", "paragraph":
		return fmt.Sprintf("html.Paragraph(html.TextConfig{Class: %q}, render.Text(%s))", block.Class, appSpecTx(bp, block.Text))
	case "heading", "h1", "h2", "h3", "h4", "h5", "h6":
		level := block.Level
		if level == 0 {
			switch strings.ToLower(block.Type) {
			case "h2":
				level = 2
			case "h3":
				level = 3
			case "h4":
				level = 4
			case "h5":
				level = 5
			case "h6":
				level = 6
			default:
				level = 1
			}
		}
		return fmt.Sprintf("html.Heading(html.HeadingConfig{Level: %d, Class: %q}, render.Text(%s))", level, block.Class, appSpecTx(bp, block.Text))
	case "link":
		// ui.Link is the design system's anchor: the same unsafe-scheme
		// guard html.Link applies, plus the link styling a bare <a> lacks.
		// It refuses empty text, so a text-less link reads as its href.
		text := appSpecTx(bp, block.Text)
		if block.Text == "" {
			text = strconv.Quote(block.Href)
		}
		return fmt.Sprintf("ui.Link(ui.LinkConfig{Href: %q, Text: %s, Class: %q})", block.Href, text, block.Class)
	case "section":
		return fmt.Sprintf("render.Tag(\"section\", %s, render.Text(%s))", attrs, appSpecTx(bp, block.Text))
	default:
		return fmt.Sprintf("html.Div(html.DivConfig{Class: %q}, render.Text(%s))", block.Class, appSpecTx(bp, block.Text))
	}
}

func renderAppSpecNodeExpression(block appspec.AppSpecBlock) string {
	return renderAppSpecNodeExpressionForScreen(appspec.AppSpec{}, appspec.AppSpecScreen{}, block, nil, nil, "")
}

// appSpecNodeTextProps are the node props a reader sees or hears: the
// node renderer's text and the attributes assistive technology reads.
var appSpecNodeTextProps = map[string]bool{
	"text": true, "label": true, "title": true, "alt": true,
	"placeholder": true, "aria-label": true,
}

// appSpecChildNodeExpr returns the uinode.Node literal for a child block.
// Entity blocks (list/detail/form) are framework UI components, not nodes, so
// they're rendered by renderAppSpecBlockForScreen (in ui-component containers
// like ui.Section), never inside a raw node tree.
func appSpecChildNodeExpr(bp appspec.AppSpec, screen appspec.AppSpecScreen, block appspec.AppSpecBlock, path []int, entityMap map[string]framework.EntityDeclaration, apiBase string) string {
	return renderAppSpecNodeExpressionForScreen(bp, screen, block, path, entityMap, apiBase)
}

func renderAppSpecNodeExpressionForScreen(bp appspec.AppSpec, screen appspec.AppSpecScreen, block appspec.AppSpecBlock, path []int, entityMap map[string]framework.EntityDeclaration, apiBase string) string {
	kind := block.Kind
	if kind == "" {
		kind = block.Type
	}
	if kind == "" {
		kind = "div"
	}
	props := map[string]any{}
	for k, v := range block.Props {
		props[k] = v
	}
	if block.Text != "" {
		props["text"] = block.Text
	}
	if block.Class != "" {
		props["class"] = block.Class
	}
	if block.Href != "" {
		props["href"] = block.Href
	}
	if block.Level != 0 {
		props["level"] = int64(block.Level)
	}
	if len(block.Actions) > 0 {
		for i, action := range block.Actions {
			event := appspec.AppSpecActionEvent(action)
			name := appSpecActionName(action)
			if i == 0 {
				if _, ok := props["data-action"]; !ok {
					props["data-action"] = name
				}
				switch event {
				case "input", "change", "submit":
					if _, ok := props["data-action-type"]; !ok {
						props["data-action-type"] = event
					}
				}
				continue
			}
			if _, ok := props["data-action-"+event]; !ok {
				props["data-action-"+event] = name
			}
		}
	}
	if bp.PhraseCollector() != nil {
		for k, v := range props {
			if s, ok := v.(string); ok && s != "" && appSpecNodeTextProps[k] {
				props[k] = goExpr(appSpecTx(bp, s))
			}
		}
	}
	var sb strings.Builder
	sb.WriteString("uinode.Node{")
	sb.WriteString(fmt.Sprintf("Kind: %q", kind))
	if len(props) > 0 {
		literal, err := renderGoLiteral(props)
		if err == nil {
			sb.WriteString(", Props: " + literal)
		}
	}
	if len(block.Children) > 0 {
		sb.WriteString(", Children: []uinode.Node{")
		for i, child := range block.Children {
			if i > 0 {
				sb.WriteString(", ")
			}
			childPath := append(append([]int(nil), path...), i)
			sb.WriteString(appSpecChildNodeExpr(bp, screen, child, childPath, entityMap, apiBase))
		}
		sb.WriteString("}")
	}
	sb.WriteString("}")
	return sb.String()
}

// appSpecH1Before walks blocks in document order and reports whether
// one renders an <h1> before the list of entity; done is true once the
// walk reached that list or an <h1>. A page header, a hero, an auth
// card, a form or detail page header and a level-1 heading render one,
// as does an earlier list of another entity, which by the same rule
// kept its title at level 1.
func appSpecH1Before(blocks []appspec.AppSpecBlock, entity string) (h1, done bool) {
	for _, b := range blocks {
		if appspec.IsEntityListBlock(b) {
			return strings.Trim(b.Entity, "/") != entity, true
		}
		if appSpecBlockRendersH1(b) {
			return true, true
		}
		if h1, done := appSpecH1Before(b.Children, entity); done {
			return h1, true
		}
	}
	return false, false
}

func appSpecBlockRendersH1(b appspec.AppSpecBlock) bool {
	kind := strings.ToLower(strings.TrimSpace(b.Kind))
	if kind == "" {
		kind = strings.ToLower(strings.TrimSpace(b.Type))
	}
	switch kind {
	case "page_header", "hero", "login_form", "signup_form", "entity_form", "entity_detail", "h1":
		return true
	case "heading":
		return b.Level == 0 || b.Level == 1
	}
	return false
}

func isLoginFormBlock(block appspec.AppSpecBlock) bool {
	return appspec.AppSpecBlockKindIs(block, "login_form")
}

func isSignupFormBlock(block appspec.AppSpecBlock) bool {
	return appspec.AppSpecBlockKindIs(block, "signup_form")
}

// appSpecAuthFormExpr composes a ui.AuthCard around a ui.Form for the auth
// surfaces. The framework owns the card/centering/field CSS, so this emits
// zero bespoke styling. The form posts urlencoded to the auth battery's POST
// <action> handler, which sets the session cookie and 303-redirects to ?next=,
// so it works with no JavaScript.
//
// The fields are built through FormField's builder with ui.Control, so the
// label association and any future description wiring arrive by construction
// rather than by raw input strings.
func appSpecAuthFormExpr(bp appspec.AppSpec, heading, action, next, submitLabel, pwAutocomplete string, pwMinLength int, footerHref, footerText string) string {
	// html.Input, NOT a hand-rolled <input> string through render.Raw:
	// the generator ships zero raw markup (hard rule 7), and the
	// primitive is the same one every other control in the emitted app
	// goes through. gofastr pack reads this expression back —
	// reverseAuthCard knows exactly this shape.
	hidden := fmt.Sprintf(`html.Input(html.InputConfig{Type: "hidden", Name: "next", Value: %q})`, next)
	emailField := `ui.FormField(ui.FormFieldConfig{Label: i18nui.AuthEmail.Text(ctx), For: "auth-email", Required: true,` +
		` Input: func(c headless.FieldControl) render.HTML { return ui.Control(ui.ControlConfig{Field: c, Type: "email", Name: "email", AutoComplete: "email"}) }})`
	minLen := ""
	if pwMinLength > 0 {
		minLen = fmt.Sprintf(", MinLength: %d", pwMinLength)
	}
	pwField := `ui.FormField(ui.FormFieldConfig{Label: i18nui.AuthPassword.Text(ctx), For: "auth-password", Required: true,` +
		fmt.Sprintf(` Input: func(c headless.FieldControl) render.HTML { return ui.Control(ui.ControlConfig{Field: c, Type: "password", Name: "password", AutoComplete: %q%s}) }})`, pwAutocomplete, minLen)
	form := fmt.Sprintf(
		"ui.Form(ui.FormConfig{Ctx: ctx, Action: %q, Method: \"POST\", SubmitLabel: %s, SubmitFullWidth: true}, %s, %s, %s)",
		action, appSpecTx(bp, submitLabel), hidden, emailField, pwField)
	footer := ""
	if footerHref != "" {
		// ui.Link, NOT a hand-rolled <a> inside render.Raw. render.Raw is what
		// took the href around core-ui/html's setURLAttr, the layer whose doc
		// comment says it is "the lowest layer that renders a caller-supplied
		// URL, so it is where the guard belongs". htmlEscapeJSString keeps the
		// value inside the attribute but says nothing about the SCHEME, so
		// `register_href: "javascript:…"` shipped a live javascript: anchor on
		// the login and signup screens. ui.Link runs urlsafe.CleanAnchor and
		// degrades an unsafe href to "#"; every sibling anchor sink in the
		// generator already goes through that guard or ui.LinkButton's
		// isUnsafeScheme. Emitting the component also satisfies Hard rule 7,
		// no hand-rolled structural markup.
		footer = fmt.Sprintf(", Footer: ui.Link(ui.LinkConfig{Href: %q, Text: %s})", footerHref, appSpecTx(bp, footerText))
	}
	// Auth screens render with the request ctx (screenNeedsCtx), so the card
	// surfaces a failed-login / duplicate-email message inline from ?error=.
	return fmt.Sprintf("ui.AuthCard(ui.AuthCardConfig{Title: %s, Alert: authError(ctx), Body: %s%s})", appSpecTx(bp, heading), form, footer)
}

// renderAppSpecSignupFormExpr emits the registration form (posts to the auth
// battery's register endpoint, 303-redirects to ?next= on success).
func renderAppSpecSignupFormExpr(bp appspec.AppSpec, block appspec.AppSpecBlock) string {
	propStr := func(k string) string { s, _ := block.Props[k].(string); return s }
	action := propStr("action")
	if action == "" {
		action = appSpecAuthBase(bp) + "/register"
	}
	next := propStr("next")
	if next == "" {
		next = "/"
	}
	heading := block.Text
	if heading == "" {
		heading = "Create your account"
	}
	return appSpecAuthFormExpr(bp, heading, action, next, "Create account", "new-password", 8, propStr("login_href"), "Already have an account? Sign in")
}

// renderAppSpecLoginFormExpr emits the login form. props: action (default
// <auth base>/login), next (post-login redirect, default /), register_href (optional).
func renderAppSpecLoginFormExpr(bp appspec.AppSpec, block appspec.AppSpecBlock) string {
	propStr := func(k string) string { s, _ := block.Props[k].(string); return s }
	action := propStr("action")
	if action == "" {
		action = appSpecAuthBase(bp) + "/login"
	}
	next := propStr("next")
	if next == "" {
		next = "/"
	}
	heading := block.Text
	if heading == "" {
		heading = "Sign in"
	}
	return appSpecAuthFormExpr(bp, heading, action, next, "Sign in", "current-password", 0, propStr("register_href"), "Create an account")
}

// appSpecScreenRoutePath converts an app spec screen route's framework-style
// path params ("/patients/{id}") to the colon style the core-ui screen router
// matches ("/patients/:id"). Static routes pass through unchanged.
func appSpecScreenRoutePath(route string) string {
	if !strings.Contains(route, "{") {
		return route
	}
	segs := strings.Split(route, "/")
	for i, seg := range segs {
		if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
			segs[i] = ":" + strings.TrimSuffix(strings.TrimPrefix(seg, "{"), "}")
		}
	}
	return strings.Join(segs, "/")
}

// appSpecAPIBase returns the URL path prefix entity CRUD mounts at:
// "/api" for prefix "api", or "" for an empty prefix (bare /{table}).
// Mirrors framework (*App).apiPrefix so generated client fetches/posts hit
// the same path the entity routes are registered at.
func appSpecAPIBase(apiPrefix string) string {
	p := strings.Trim(apiPrefix, "/")
	if p == "" {
		return ""
	}
	return "/" + p
}

// appSpecAuthBase is where the auth battery mounts its routes:
// app.auth.base_path, else the battery's default /auth. Validation keeps
// base_path an absolute path with no trailing slash, so base + "/login"
// is the battery's login route.
func appSpecAuthBase(bp appspec.AppSpec) string {
	if bp.App.Auth.BasePath != "" {
		return bp.App.Auth.BasePath
	}
	return "/auth"
}

// appSpecSignOutExpr is the sign-out control, posting to the battery's
// logout route. ui.SignOut defaults to /auth/logout, so only another
// base names an Action.
func appSpecSignOutExpr(bp appspec.AppSpec) string {
	if base := appSpecAuthBase(bp); base != "/auth" {
		return fmt.Sprintf("ui.SignOut(ui.SignOutConfig{Ctx: ctx, Next: \"/\", Action: %q})", base+"/logout")
	}
	return `ui.SignOut(ui.SignOutConfig{Ctx: ctx, Next: "/"})`
}

// appSpecEntityAPIPath is where App.Entity mounts decl's CRUD routes:
// apiBase plus declTable.
func appSpecEntityAPIPath(apiBase string, decl framework.EntityDeclaration) string {
	return apiBase + "/" + declTable(decl)
}

// declTable is the table App.Entity mounts decl under: the declared Table,
// or the name in snake_case. Define spells the default, so every generated
// caller (SSR forms, CLI, SDKs, typed client) routes on the framework's rule
// and not a copy of it: a raw fallback to the name sent BlogPost's calls to
// /BlogPost while the server mounted /blog_post.
func declTable(decl framework.EntityDeclaration) string {
	return entity.Define(decl.Name, entity.EntityConfig{Table: decl.Table}).GetTable()
}

// appSpecFormInputType maps an entity field type to an <input type=…>.
func appSpecFormInputType(fieldType string) string {
	switch fieldType {
	case "int", "integer", "float", "decimal":
		return "number"
	case "date":
		return "date"
	case "timestamp", "datetime":
		return "datetime-local"
	case "image":
		return "file"
	default:
		return "text"
	}
}

// appSpecEntityFormExpr emits a ui.Form for a create form, composing
// ui.FormField per entity field. The framework owns the form/field CSS, so this
// ships no bespoke styling. The form is an island: data-cui-rpc-* (on the form
// via ExtraAttrs) makes the runtime JSON-encode the body to the CRUD endpoint;
// relation <select>s are populated on mount via data-action-mount. An edit
// form is the record page instead (appSpecEditExpr): it loads the record.
func appSpecEntityFormExpr(bp appspec.AppSpec, screen appspec.AppSpecScreen, block appspec.AppSpecBlock, path []int, entityMap map[string]framework.EntityDeclaration, apiBase string) string {
	entity := strings.Trim(block.Entity, "/")
	decl, ok := entityMap[entity]
	if !ok {
		return fmt.Sprintf("ui.Callout(ui.CalloutConfig{Variant: ui.StatusDanger, Title: \"Form error\"}, render.Text(%q))", "unknown entity "+entity)
	}
	title := block.Text
	if title == "" {
		title = "New " + appspec.ToDisplayName(entity)
	}
	rpcPath := appSpecEntityAPIPath(apiBase, decl)
	// The form carries entity-form markers (data-entity-form/mode, plus
	// data-action-mount when relation selects need populating) alongside
	// the RPC wiring. Build the RPC attrs via the typed interactive layer
	// and merge them into the markers map so render.Tag's sorted writer
	// places every attribute in one consistent order, byte-for-byte the
	// same <form> the raw data-cui-rpc map produced before.
	markerParts := []string{
		fmt.Sprintf("\"data-entity-form\": %q", entity),
		`"data-entity-mode": "create"`,
	}
	if appSpecFormHasRelation(block, entityMap) {
		markerParts = append(markerParts, fmt.Sprintf("\"data-action-mount\": %q", appSpecEntityFormActionName(screen, block, path)))
	}
	markers := "html.Attrs{" + strings.Join(markerParts, ", ") + "}"
	actionExpr := fmt.Sprintf("interactive.Post(%q).OnSuccess(interactive.ResetForm())", rpcPath)
	extraAttrs := "html.MergeAttrs(" + markers + ", " + actionExpr + ".Attrs())"

	var fields []string
	for _, field := range decl.Fields {
		if appSpecFormFieldSkipped(field, block.Fields) {
			continue
		}
		// field.Name is constrained to a Go identifier by
		// validateAppSpec, so %q here is stable and escaped once (the
		// components' own renderers escape again at the value level,
		// never double-escaping the identifier). The label is the
		// catalog's entity.<entity>.fields.<field>.label, the derived
		// display name when the catalog has none: the same lookup the
		// entityui forms make.
		label := fmt.Sprintf("i18nui.FieldLabel(ctx, nil, %q, %q, %q)", entity, field.Name, appspec.ToDisplayName(field.Name))
		fieldID := "field-" + field.Name
		req := fmt.Sprintf("%t", field.Required)
		var expr string
		switch field.Type {
		case "enum":
			var opts strings.Builder
			opts.WriteString("[]ui.SelectOption{")
			for _, v := range field.Values {
				fmt.Fprintf(&opts, "{Value: %q, Text: i18nui.FieldValueLabel(ctx, nil, %q, %q, %q)},", v, entity, field.Name, v)
			}
			opts.WriteString("}")
			expr = fmt.Sprintf("ui.Select(ui.SelectConfig{Name: %q, Label: %s, ID: %q, Placeholder: i18nui.EntitySelect.Text(ctx), Options: %s, Required: %s})",
				field.Name, label, fieldID, opts.String(), req)
		case "relation":
			// The relation select keeps data-rel-entity: the generated
			// mount hook populates it from the related entity's API.
			expr = fmt.Sprintf("ui.Select(ui.SelectConfig{Name: %q, Label: %s, ID: %q, Placeholder: i18nui.EntitySelect.Text(ctx), Required: %s, ExtraAttrs: html.Attrs{\"data-rel-entity\": %q}})",
				field.Name, label, fieldID, req, field.To)
		case "text":
			expr = fmt.Sprintf("ui.TextArea(ui.TextAreaConfig{Ctx: ctx, Name: %q, Label: %s, ID: %q, Required: %s})",
				field.Name, label, fieldID, req)
		case "bool", "boolean":
			// entityui's pair: a checked box submits "true" (with no
			// Value it posted "on", which the bool validator refuses),
			// and the hidden "false" before it is what an unchecked box
			// leaves, so unticked writes false rather than the declared
			// default. The box starts as that default. Required stays off
			// the checkbox: it would make unticked unsubmittable.
			expr = fmt.Sprintf("render.Join(html.Input(html.InputConfig{Type: \"hidden\", Name: %q, Value: \"false\"}), ui.Checkbox(ui.ToggleConfig{Name: %q, Label: %s, ID: %q, Value: \"true\", Checked: %t}))",
				field.Name, field.Name, label, fieldID, fmt.Sprint(field.Default) == "true")
		case "int", "integer", "float", "decimal":
			// The step admits what the column holds, as entityui's form
			// does: at the browser's default of 1, native validation
			// refused 12.99 before the RPC ran. A decimal column keeps
			// four fraction digits; a float takes "any", which
			// NumberField's numeric Step cannot say, so every number
			// draws the styled control with a text step.
			step := map[string]string{"float": "any", "decimal": "0.0001"}[field.Type]
			if step == "" {
				step = "1"
			}
			expr = fmt.Sprintf("ui.FormField(ui.FormFieldConfig{Label: %s, For: %q, Required: %s, Input: func(c headless.FieldControl) render.HTML { return ui.Control(ui.ControlConfig{Field: c, Type: \"number\", Name: %q, Step: %q}) }})",
				label, fieldID, req, field.Name, step)
		case "date":
			expr = fmt.Sprintf("ui.DateField(ui.DateFieldConfig{Name: %q, Label: %s, ID: %q, Required: %s})",
				field.Name, label, fieldID, req)
		default:
			// datetime-local, file, email and anything else the typed
			// fields do not name: the styled native control.
			expr = fmt.Sprintf("ui.FormField(ui.FormFieldConfig{Label: %s, For: %q, Required: %s, Input: func(c headless.FieldControl) render.HTML { return ui.Control(ui.ControlConfig{Field: c, Type: %q, Name: %q}) }})",
				label, fieldID, req, appSpecFormInputType(field.Type), field.Name)
		}
		fields = append(fields, expr)
	}
	form := fmt.Sprintf("ui.Form(ui.FormConfig{Ctx: ctx, Action: %q, Method: \"POST\", SubmitLabel: %s, ExtraAttrs: %s}, %s)", rpcPath, appSpecTx(bp, "Create"), extraAttrs, strings.Join(fields, ", "))
	return fmt.Sprintf("render.Join(ui.PageHeader(ui.PageHeaderConfig{Title: %s}), %s)", appSpecTx(bp, title), form)
}

// appSpecFormFieldSkipped reports whether a field is omitted from a
// generated entity_form (system columns, hidden/auto/readonly fields, or
// fields excluded by an explicit block.fields allowlist).
func appSpecFormFieldSkipped(field framework.FieldDeclaration, filterFields []string) bool {
	switch field.Name {
	case "id", "created_at", "updated_at", "deleted_at":
		return true
	}
	if field.Hidden || appspec.FieldAutoGenerated(field) || field.ReadOnly {
		return true
	}
	if len(filterFields) > 0 {
		for _, f := range filterFields {
			if f == field.Name {
				return false
			}
		}
		return true
	}
	return false
}

// appSpecEntityFormClientJS populates the form's relation <select>s on
// mount by fetching each related entity's collection. targets maps a
// related entity to its route and the wire name of its key, the column a
// relation stores: a target keyed on currency_code is read as
// row.currencyCode, and one on its own table is fetched there. A target
// with a multi-column key has no single value a relation can hold, so it
// gets no entry and its select stays empty.
func appSpecEntityFormClientJS(block appspec.AppSpecBlock, entityMap map[string]framework.EntityDeclaration, apiBase string) string {
	name := strings.Trim(block.Entity, "/")
	type relTarget struct {
		Key  string `json:"key"`
		Path string `json:"path"`
	}
	targets := map[string]relTarget{}
	for _, f := range entityMap[name].Fields {
		if f.Type != "relation" || f.To == "" {
			continue
		}
		decl, ok := entityMap[f.To]
		keys := appspec.AppSpecKeyColNames(decl)
		if !ok || len(keys) != 1 {
			continue
		}
		targets[f.To] = relTarget{Key: appspec.WireKey(keys[0]), Path: appSpecEntityAPIPath(apiBase, decl)}
	}
	// json.Marshal sorts map keys, so the emitted literal is stable.
	targetsJSON, err := json.Marshal(targets)
	if err != nil {
		targetsJSON = []byte("{}")
	}
	//gofastr:allow(emitident) block.Entity is checked against the entities map in appspec validation
	return fmt.Sprintf(`(async () => {
  const targets = %s;
  const form = document.querySelector('[data-entity-form="%s"]');
  if (!form) return;
  const labelFor = (row) => String(row.name ?? row.title ?? row.label ?? row.email ?? row.code ?? row.id ?? '');
  for (const sel of form.querySelectorAll('select[data-rel-entity]')) {
    const rel = sel.getAttribute('data-rel-entity');
    const t = rel && Object.hasOwn(targets, rel) ? targets[rel] : null;
    if (!t || sel.dataset.relLoaded) continue;
    try {
      const res = await fetch(t.path + '?limit=100', { headers: { 'Accept': 'application/json' } });
      if (!res.ok) continue;
      const payload = await res.json();
      const rows = Array.isArray(payload.data) ? payload.data : [];
      for (const row of rows) {
        if (row[t.key] == null) continue;
        const opt = document.createElement('option');
        opt.value = row[t.key];
        opt.textContent = labelFor(row);
        sel.appendChild(opt);
      }
      sel.dataset.relLoaded = '1';
    } catch (_) {}
  }
})();`, targetsJSON, name)
}

func appSpecEntityFormActionName(screen appspec.AppSpecScreen, block appspec.AppSpecBlock, path []int) string {
	parts := []string{"entity_form"}
	if screen.Name != "" {
		parts = append(parts, appspec.ToCamelJSON(screen.Name))
	}
	if block.Entity != "" {
		parts = append(parts, appspec.ToCamelJSON(block.Entity))
	}
	return strings.NewReplacer("-", "_", " ", "_", "/", "_").Replace(strings.Join(parts, "_"))
}

// appSpecSeedFieldIsDecimal reports whether the named field on the given
// entity is a decimal-typed column (which CRUD validates as a decimal string).
func appSpecSeedFieldIsDecimal(bp appspec.AppSpec, entity, field string) bool {
	for _, decl := range bp.Entities {
		if decl.Name != entity {
			continue
		}
		for _, f := range decl.Fields {
			if f.Name == field {
				return strings.EqualFold(f.Type, "decimal")
			}
		}
	}
	return false
}

// appSpecDecimalLiteral renders a seed value as the decimal string the
// validator expects. Already-string values pass through; numbers are
// formatted without scientific notation or trailing-zero noise.
func appSpecDecimalLiteral(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	case int:
		return strconv.Itoa(val)
	case int64:
		return strconv.FormatInt(val, 10)
	default:
		return fmt.Sprint(val)
	}
}

func RenderAppSpecStubs(bp appspec.AppSpec) string {
	var sb strings.Builder
	sb.WriteString("package main\n\n")
	needsHTTP := len(bp.Middleware) > 0
	needsFramework := len(bp.Plugins) > 0
	needsJSON := false
	// Hook stubs take framework.HookFunc's signature (context.Context, any).
	needsContext := len(bp.Hooks) > 0
	if needsHTTP || needsFramework || needsJSON || needsContext {
		sb.WriteString("import (\n")
		if needsContext {
			sb.WriteString("\t\"context\"\n")
		}
		if needsHTTP {
			sb.WriteString("\t\"net/http\"\n")
		}
		if needsJSON {
			sb.WriteString("\t\"encoding/json\"\n")
		}
		if needsFramework {
			sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/framework\"\n")
		}
		sb.WriteString(")\n\n")
	}
	for _, item := range bp.Middleware {
		// Same gate as the endpoint arm above: toCamelCase transforms, it
		// does not validate, and this emitter serves unvalidated renders.
		// Never emit a non-identifier; validateAppSpec is the loud gate.
		if !project.IsGoIdentifier(appspec.ToCamelCase(item.Name)) {
			continue
		}
		name := appspec.ToCamelCase(item.Name)
		sb.WriteString(fmt.Sprintf("func %sMiddleware(next http.Handler) http.Handler {\n", name))
		sb.WriteString("\treturn http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {\n\t\tnext.ServeHTTP(w, r)\n\t})\n")
		sb.WriteString("}\n\n")
	}
	for _, item := range bp.Plugins {
		if !project.IsGoIdentifier(appspec.ToCamelCase(item.Name)) {
			continue // same gate as the middleware stubs above
		}
		name := appspec.ToCamelCase(item.Name) + "Plugin"
		sb.WriteString(fmt.Sprintf("type %s struct{}\n\n", name))
		sb.WriteString(fmt.Sprintf("func (%s) Name() string { return %q }\n", name, item.Name))
		sb.WriteString(fmt.Sprintf("func (%s) Init(app *framework.App) error { return nil }\n\n", name))
	}
	for _, item := range bp.Helpers {
		if !project.IsGoIdentifier(appspec.ToCamelCase(item.Name)) {
			continue // same gate as the middleware stubs above
		}
		name := appspec.ToCamelCase(item.Name)
		sb.WriteString(fmt.Sprintf("func %s() {\n\t// TODO: implement helper %q.\n}\n\n", name, item.Name))
	}
	// Hook handler stubs: the owned-Go seam the AppSpecHook doc comment
	// promises ("Handler names the func to write"). main.go registers each
	// stub on the entity's HookRegistry at the hook's lifecycle point, so
	// the surface exists the moment the app generates; the author fills in
	// the action. Description and ID go through appSpecCommentSafe: a
	// newline in transcribed text would put the rest at statement position.
	for _, hook := range bp.Hooks {
		handler := strings.TrimSpace(hook.Handler)
		if !project.IsGoIdentifier(handler) {
			continue // validateAppSpec refuses it; never emit a non-identifier
		}
		sb.WriteString(fmt.Sprintf("// %s implements hook %q (entity %s, %s): %s\n",
			handler, hook.ID, hook.Entity, hook.When, appSpecCommentSafe(hook.Description)))
		sb.WriteString("// Registered in main.go; returning a non-nil error aborts the operation.\n")
		sb.WriteString(fmt.Sprintf("func %s(ctx context.Context, data any) error {\n", handler))
		sb.WriteString("\t_, _ = ctx, data // TODO: implement the hook's action here.\n")
		sb.WriteString("\treturn nil\n")
		sb.WriteString("}\n\n")
	}
	if len(bp.Seed) > 0 {
		sb.WriteString("// seedEntity is one entity's ordered seed rows.\n")
		sb.WriteString("type seedEntity struct {\n\tEntity string\n\tRows   []map[string]any\n}\n\n")
		sb.WriteString("// seedData returns the initial seed data in declared\n")
		sb.WriteString("// order (so entities that reference others are inserted after them).\n")
		sb.WriteString("func seedData() []seedEntity {\n")
		sb.WriteString("\treturn []seedEntity{\n")
		for _, seed := range bp.Seed {
			sb.WriteString(fmt.Sprintf("\t\t{Entity: %q, Rows: []map[string]any{\n", seed.Entity))
			for _, row := range seed.Rows {
				sb.WriteString("\t\t\t{")
				// Emit keys in sorted order so regeneration is deterministic
				// (map iteration order is random and would otherwise churn the diff).
				keys := make([]string, 0, len(row))
				for k := range row {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				first := true
				for _, k := range keys {
					v := row[k]
					if !first {
						sb.WriteString(", ")
					}
					first = false
					// Decimal columns validate as decimal strings, so a YAML
					// number (12.5) must be emitted as a quoted string.
					if appSpecSeedFieldIsDecimal(bp, seed.Entity, k) {
						sb.WriteString(fmt.Sprintf("%q: %q", k, appSpecDecimalLiteral(v)))
						continue
					}
					switch val := v.(type) {
					case string:
						sb.WriteString(fmt.Sprintf("%q: %q", k, val))
					case int:
						sb.WriteString(fmt.Sprintf("%q: %d", k, val))
					case int64:
						sb.WriteString(fmt.Sprintf("%q: %d", k, val))
					case float64:
						sb.WriteString(fmt.Sprintf("%q: %v", k, val))
					case bool:
						sb.WriteString(fmt.Sprintf("%q: %t", k, val))
					case []any:
						sb.WriteString(fmt.Sprintf("%q: %s", k, appSpecSeedGoLiteral(val)))
					case map[string]any:
						// A map (an object on a json column, an object inside
						// a list) is a value the validator accepts and the
						// boot validator marshals and inserts; emitting nil
						// here would seed NULL where the author wrote data.
						sb.WriteString(fmt.Sprintf("%q: %s", k, appSpecSeedGoLiteral(val)))
					case nil:
						sb.WriteString(fmt.Sprintf("%q: nil", k))
					default:
						sb.WriteString(fmt.Sprintf("%q: nil // unsupported type %T", k, val))
					}
				}
				sb.WriteString("},\n")
			}
			sb.WriteString("\t\t}},\n")
		}
		sb.WriteString("\t}\n}\n\n")
		// Add json import if needed
	}
	return sb.String()
}

// appSpecSeedGoLiteral renders a decoded seed value as a Go expression
// that reproduces it: the emitter-side guarantee that every value the
// validator accepted reaches seedData() verbatim instead of being nulled.
// Maps emit their keys sorted so regeneration stays deterministic.
func appSpecSeedGoLiteral(v any) string {
	switch val := v.(type) {
	case string:
		return fmt.Sprintf("%q", val)
	case int:
		return strconv.Itoa(val)
	case int64:
		return strconv.FormatInt(val, 10)
	case float64:
		return fmt.Sprintf("%v", val)
	case bool:
		return fmt.Sprintf("%t", val)
	case nil:
		return "nil"
	case []any:
		parts := make([]string, len(val))
		for i, item := range val {
			parts[i] = appSpecSeedGoLiteral(item)
		}
		return "[]any{" + strings.Join(parts, ", ") + "}"
	case map[string]any:
		parts := make([]string, 0, len(val))
		for _, k := range slices.Sorted(maps.Keys(val)) {
			parts = append(parts, fmt.Sprintf("%q: %s", k, appSpecSeedGoLiteral(val[k])))
		}
		return "map[string]any{" + strings.Join(parts, ", ") + "}"
	default:
		return fmt.Sprintf("nil // unsupported type %T", val)
	}
}

func RenderAppSpecApp(bp appspec.AppSpec) string {
	name := bp.App.Name
	if name == "" {
		name = "GoFastr"
	}
	var sb strings.Builder
	// The chrome's words (nav labels, the header's auth action, footer
	// columns) are phrases under <app>.chrome; the brand name stays as
	// written.
	bp.SetPhraseCollector(appSpecChromePhrases(bp.App.Name))
	tx := func(s string) string { return appSpecTx(bp, s) }
	adminSeed := bp.App.Auth.Enabled && bp.App.Admin.SeedEmail != "" && bp.App.Admin.SeedPassword != ""
	hasMarketing := appSpecHasMarketing(bp)
	hasAccess := false
	for _, s := range bp.Screens {
		if s.Access.Auth {
			hasAccess = true
		}
	}
	needWidget := len(bp.Nav) > 0 || appSpecNeedsToasts(bp)
	needUI := len(bp.Nav) > 0 || hasMarketing
	roleNav := bp.App.Auth.Enabled && appSpecNavHasRoles(bp.Nav)
	// authHeader: an auth-aware marketing header (Sign in ↔ account + Sign out).
	// guestRedirect: bounce already-signed-in visitors off the auth screens.
	authHeader := hasMarketing && bp.App.Auth.Enabled
	guestRedirect := bp.App.Auth.Enabled && appSpecHasAuthFormScreen(bp)
	// rbac gates the access.Middleware block, whose role-resolver closure is
	// `func(ctx context.Context) []string`. It is computed HERE, above the
	// import block, rather than beside the framework/access import below:
	// leaving it below meant the "context" condition could not see it, so an
	// app with `access:` entities but no auth-gated screen emitted
	// context.Context without importing context. Every app spec that reached
	// that path shipped an app.go that did not compile.
	rbac := bp.App.Auth.Enabled && appSpecHasEntityAccess(bp)
	// authSidebar gates the per-request app-shell sidebar: with auth on, the
	// sidebar footer resolves Sign out vs Sign in from the live session, so the
	// generated app.go needs context/render/handler exactly like the auth-aware
	// marketing header does. Computed here, above the import block, for the same
	// reason rbac is: a condition declared below it cannot be seen by it.
	authSidebar := bp.App.Auth.Enabled && len(bp.Nav) > 0
	sb.WriteString("package main\n\n")
	sb.WriteString("import (\n")
	// hasShell: app.go declares a layout build (the app shell when the
	// app spec has nav, the marketing shell when it has marketing
	// screens); every build fn is func(ctx context.Context, l
	// *app.LayoutTree) render.HTML, so the three imports below follow it.
	hasShell := len(bp.Nav) > 0 || hasMarketing
	if adminSeed || hasAccess || roleNav || authHeader || guestRedirect || rbac || authSidebar || hasShell {
		sb.WriteString("\t\"context\"\n")
	}
	sb.WriteString("\t\"database/sql\"\n")
	if adminSeed {
		sb.WriteString("\t\"fmt\"\n")
	}
	if bp.App.Auth.Enabled {
		// An auth Init error stops the boot.
		sb.WriteString("\t\"log\"\n")
		// JWT_SECRET + ADMIN_SEED_PASSWORD are read from the environment.
		sb.WriteString("\t\"os\"\n")
	}
	if adminSeed {
		// The seed checks an existing account's roles.
		sb.WriteString("\t\"slices\"\n")
	}
	sb.WriteString("\n")
	sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/core-ui/app\"\n")
	if guestRedirect {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/core-ui/app/decide\"\n")
	}
	if hasShell {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/core-ui/component\"\n")
	}
	// Marketing chrome: the app's own siteheader / sitefooter packages
	// (emitted beside app.go by this same generation), imported by module
	// path like entities. Owned layout: module root by default, or a
	// subpackage when app.output_dir / --out names one.
	if hasMarketing {
		chromeImport := chromePackageImport(bp)
		sb.WriteString(fmt.Sprintf("\t%q\n", chromeImport+"/siteheader"))
		sb.WriteString(fmt.Sprintf("\t%q\n", chromeImport+"/sitefooter"))
	}
	if hasMarketing || authSidebar || hasShell {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/core/render\"\n")
	}
	// Always imported: app.go now declares fontFaceCSS by CALLING
	// style.FontFaceCSS rather than carrying a baked CSS string, so the
	// import is needed whether or not a theme is declared.
	sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/core-ui/style\"\n")
	// A theme-less marketing app starts from framework/ui/theme.Default,
	// the adaptive theme with a dark palette; style.DefaultTheme is the
	// light-only core-ui baseline. A declared app.theme keeps the light
	// baseline: its palette is the author's, and a stock dark palette
	// would replace the brand whenever the OS is dark.
	if len(bp.App.Theme) == 0 && hasMarketing {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/framework/ui/theme\"\n")
	}
	if needWidget {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/core-ui/widget\"\n")
	}
	if appSpecNeedsToasts(bp) {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/core-ui/widget/preset\"\n")
	}
	if len(bp.Nav) > 0 {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/core/router\"\n")
	}
	if rbac || roleNav || authHeader || guestRedirect || authSidebar {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/core/handler\"\n")
	}
	if needUI {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/framework/ui\"\n")
	}
	if len(bp.App.ThemeDark) > 0 {
		// appTheme seeds the dark palette from the framework theme.
		sb.WriteString("\tuitheme \"github.com/DonaldMurillo/gofastr/framework/ui/theme\"\n")
	}
	if rbac {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/framework/access\"\n")
	}
	if bp.App.Auth.Enabled || hasAccess {
		sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/battery/auth\"\n")
	}
	sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/framework\"\n")
	sb.WriteString("\t\"github.com/DonaldMurillo/gofastr/framework/entityui\"\n)\n\n")
	sb.WriteString("const (\n")
	sb.WriteString(fmt.Sprintf("\tappName = %q\n", name))
	sb.WriteString(fmt.Sprintf("\tappModule = %q\n", bp.App.Module))
	sb.WriteString(fmt.Sprintf("\tdbDriver = %q\n", bp.App.DBDriver))
	// Credentials never land in committed source: the DSN is emitted
	// password-stripped; the runtime reads the full one from
	// DATABASE_URL (see the generated .env).
	sb.WriteString(fmt.Sprintf("\tdbURL = %q\n", redactDSN(bp.App.DBURL)))
	sb.WriteString(fmt.Sprintf("\tstaticDir = %q\n", appSpecEffectiveStaticDir(bp)))
	sb.WriteString(fmt.Sprintf("\tapiPrefix = %q\n", bp.App.APIPrefix))
	sb.WriteString(")\n\n")
	sb.WriteString(appSpecBaseCSSFunc())
	if hasAccess {
		sb.WriteString(appSpecAuthPolicyFunc())
	}
	if guestRedirect {
		sb.WriteString("// guestPolicy gates a guest-only screen (login / signup): a\n")
		sb.WriteString("// signed-in visitor is redirected to the app instead of seeing a sign-in\n")
		sb.WriteString("// form they're already past.\n")
		sb.WriteString("func guestPolicy(appHome string) app.Policy {\n")
		sb.WriteString("\treturn app.PolicyFunc(func(ctx context.Context) app.Decision {\n")
		sb.WriteString("\t\tif u, ok := handler.GetUser(ctx); ok && u != nil {\n")
		sb.WriteString("\t\t\treturn decide.Redirect(appHome)\n")
		sb.WriteString("\t\t}\n")
		sb.WriteString("\t\treturn decide.Allow()\n")
		sb.WriteString("\t})\n")
		sb.WriteString("}\n\n")
	}
	if hasMarketing {
		sb.WriteString("// marketingHeader / marketingFooter build the marketing shell's\n")
		sb.WriteString("// chrome from siteheader / sitefooter, the packages this same\n")
		sb.WriteString("// generation wrote as the app's own code. Edit them there; this\n")
		sb.WriteString("// file only decides what they say.\n")
		// Chrome links come from the registered screen set, never literals: a
		// chrome href to a route no screen mounts is a shipped 404. The same
		// rule appSpecLoginRoute established for the Sign in link (#312).
		chrome := appSpecRegisteredMarketingLinks(bp)
		var navLinks []string
		for _, l := range chrome {
			if l.Nav {
				navLinks = append(navLinks, fmt.Sprintf("{Label: %s, Href: %q}", tx(l.Label), l.Route))
			}
		}
		// The app declares a dark scheme: surface the toggle as a real
		// ui.ThemeToggle in the bar's actions, never hand-rolled.
		themeToggle := len(bp.App.ThemeDark) > 0
		if authHeader {
			// Auth-aware header. Dashboard is a plain nav link (cohesive with
			// Pricing/About, and it collapses into the phone menu like them); the
			// single auth action, Sign out when signed in and Sign in when not, is the
			// one button, sized to match. No "Sign in" loop for users already past it.
			appHome := appSpecAppHome(bp)
			sb.WriteString("func marketingHeader(ctx context.Context) render.HTML {\n")
			// links holds only registered chrome links; with none it starts
			// empty (valid Go for the Dashboard append below).
			sb.WriteString(fmt.Sprintf("\tlinks := []siteheader.Link{%s}\n", strings.Join(navLinks, ", ")))
			sb.WriteString("\tvar actions render.HTML\n")
			sb.WriteString("\tif u, ok := handler.GetUser(ctx); ok && u != nil {\n")
			sb.WriteString(fmt.Sprintf("\t\tlinks = append(links, siteheader.Link{Label: %s, Href: %q})\n", tx("Dashboard"), appHome))
			sb.WriteString("\t\tactions = " + barActions(appSpecSignOutExpr(bp), themeToggle) + "\n")
			sb.WriteString("\t} else {\n")
			// appSpecLoginRoute, not a hardcoded "/login": the app-shell
			// sidebar had the same literal and it pointed at a route six of
			// the seven shipped app specs never register. Dormant here today
			// only because meridian is the sole app spec with both marketing
			// screens and auth, and it happens to route /login, which is luck,
			// not design.
			sb.WriteString("\t\tactions = " + barActions(fmt.Sprintf("ui.LinkButton(ui.LinkButtonConfig{Label: %s, Href: %q, Variant: ui.ButtonSecondary, Size: ui.ButtonSizeSmall})", tx("Sign in"), appSpecLoginRoute(bp)), themeToggle) + "\n")
			sb.WriteString("\t}\n")
			sb.WriteString("\treturn siteheader.Render(siteheader.Config{\n")
			sb.WriteString("\t\tCtx:     ctx,\n")
			sb.WriteString("\t\tName:    appName,\n")
			sb.WriteString("\t\tLinks:   links,\n")
			sb.WriteString("\t\tActions: actions,\n")
			sb.WriteString("\t})\n}\n\n")
		} else {
			sb.WriteString("func marketingHeader(ctx context.Context) render.HTML {\n")
			sb.WriteString("\treturn siteheader.Render(siteheader.Config{\n")
			sb.WriteString("\t\tCtx:  ctx,\n")
			sb.WriteString("\t\tName: appName,\n")
			// No Links line when no chrome link survives: an omitted field
			// renders the brand + actions bar; an empty slice would add noise.
			if len(navLinks) > 0 {
				sb.WriteString(fmt.Sprintf("\t\tLinks: []siteheader.Link{%s},\n", strings.Join(navLinks, ", ")))
			}
			if themeToggle {
				sb.WriteString("\t\tActions: ui.ThemeToggle(ui.ThemeToggleConfig{Ctx: ctx, Variant: ui.ThemeToggleIcon}),\n")
			}
			sb.WriteString("\t})\n}\n\n")
		}
		sb.WriteString("func marketingFooter(ctx context.Context) render.HTML {\n")
		sb.WriteString("\treturn sitefooter.Render(sitefooter.Config{\n")
		sb.WriteString("\t\tName: appName,\n")
		// A footer column is emitted only when at least one of its links
		// survives: a titled empty list for any column present is a heading
		// over nothing. Column order follows the link table's declaration
		// order.
		type footerColumn struct {
			title string
			links []string
		}
		var columns []footerColumn
		columnIndex := map[string]int{}
		for _, l := range chrome {
			if l.Column == "" {
				continue
			}
			expr := fmt.Sprintf("{Label: %s, Href: %q}", tx(l.Label), l.Route)
			if i, ok := columnIndex[l.Column]; ok {
				columns[i].links = append(columns[i].links, expr)
				continue
			}
			columns = append(columns, footerColumn{title: l.Column, links: []string{expr}})
			columnIndex[l.Column] = len(columns) - 1
		}
		if len(columns) > 0 {
			sb.WriteString("\t\tColumns: []sitefooter.Column{\n")
			for _, col := range columns {
				sb.WriteString(fmt.Sprintf("\t\t\t{Title: %s, Links: []sitefooter.Link{%s}},\n", tx(col.title), strings.Join(col.links, ", ")))
			}
			sb.WriteString("\t\t},\n")
		}
		// A reader reaches another language from any marketing page.
		// The footer renders once per page, where the header's actions
		// render twice (bar and phone menu).
		if bp.App.LocalePrefixes {
			sb.WriteString("\t\tLanguage: ui.LanguageMenu(ui.LanguageMenuConfig{Ctx: ctx, Position: ui.MenuTopStart}),\n")
		}
		sb.WriteString(fmt.Sprintf("\t\tNote: %s,\n", tx(fmt.Sprintf("© %d %s.", time.Now().Year(), name))))
		sb.WriteString("\t})\n}\n\n")
	}
	if appspec.AppSpecHasTheme(bp.App) {
		sb.WriteString("func appTheme() style.Theme {\n")
		sb.WriteString("\ttheme := style.DefaultTheme()\n")
		for _, key := range slices.Sorted(maps.Keys(bp.App.Theme)) {
			if path, ok := appspec.AppSpecThemeColorPath(key); ok {
				if reason := appSpecUnsafeColorNote(key, bp.App.Theme[key]); reason != "" {
					sb.WriteString(reason)
					continue
				}
				sb.WriteString(fmt.Sprintf("\ttheme.Colors.%s.Value = %q\n", path, bp.App.Theme[key]))
			}
		}
		if body, heading := appSpecFontStacks(bp.App.Theme); body != "" || heading != "" {
			if body != "" {
				sb.WriteString(fmt.Sprintf("\ttheme.Fonts.Body.Value = %q\n", body))
			}
			if heading != "" {
				sb.WriteString(fmt.Sprintf("\ttheme.Fonts.Heading.Value = %q\n", heading))
			}
		}
		if len(bp.App.ThemeDark) > 0 {
			// Dark-scheme palette, emitted as a [data-color-scheme="dark"] token
			// block so the header's ui.ThemeToggle recolors the whole app.
			// app.theme.dark is an overlay: the palette starts from the
			// framework's complete dark palette and the app spec's keys win.
			// Emitted as the whole map, every token the app spec did not name
			// kept its light value under the dark scheme.
			sb.WriteString("\ttheme.DarkColors = uitheme.Default().DarkColors\n")
			for _, key := range slices.Sorted(maps.Keys(bp.App.ThemeDark)) {
				if reason := appSpecUnsafeColorNote("dark."+key, bp.App.ThemeDark[key]); reason != "" {
					sb.WriteString(reason)
					continue
				}
				sb.WriteString(fmt.Sprintf("\ttheme.DarkColors[%q] = %q\n", key, bp.App.ThemeDark[key]))
			}
			// The seeded inks were chosen for the framework's own dark fills.
			// A fill the app spec overrides without its ink keeps the light
			// ink it was paired with before seeding, so a mid-tone dark
			// primary that clears AA on white does not meet the framework's
			// near-black ink and fail the boot contrast check.
			for _, pair := range [][2]string{{"primary", "PrimaryFg"}, {"secondary", "SecondaryFg"}, {"danger", "DangerFg"}} {
				fill, ink := pair[0], pair[0]+"-fg"
				if _, ok := bp.App.ThemeDark[fill]; !ok {
					continue
				}
				if _, ok := bp.App.ThemeDark[ink]; ok {
					continue
				}
				sb.WriteString(fmt.Sprintf("\ttheme.DarkColors[%q] = theme.Colors.%s.Value\n", ink, pair[1]))
			}
		}
		if hasMarketing {
			// siteheader's own tokens (the phone menu's stagger) join the
			// theme here; the sheet reads them through var().
			sb.WriteString("\treturn theme.Extend(siteheader.Tokens)\n")
		} else {
			sb.WriteString("\treturn theme\n")
		}
		sb.WriteString("}\n\n")
	}
	// fontFaceCSS holds the @font-face rules for the theme's configured
	// fonts (self-hosted from <static>/fonts/<slug>.woff2). It is the single
	// font-loading source, shared verbatim by the UI host and the admin battery
	// so every surface loads identical fonts. Empty when no fonts are declared.
	sb.WriteString(appSpecFontFaceDecl(bp.App.Theme))
	if len(bp.Nav) > 0 {
		sb.WriteString("// sidebarConfig returns the navigation sidebar configuration.\n")
		// The sidebar footer's auth control has to be resolved per request. A
		// static sidebar showed "Sign out" to anonymous visitors, because the
		// control was emitted whenever auth was *enabled*, never asked whether
		// anyone was signed *in*, so the app shell told every first-time visitor
		// they had a session to end. The marketing header already resolved this
		// correctly from handler.GetUser(ctx); the app shell now does too.
		sb.WriteString("func sidebarConfig(ctx context.Context) ui.SidebarConfig {\n")
		sb.WriteString(fmt.Sprintf("\tcfg := ui.SidebarConfig{Ctx: ctx, Title: %q, Items: []ui.SidebarItem{\n", name))
		for _, item := range bp.Nav {
			renderNavItemGo(bp, &sb, item, "\t\t")
		}
		sb.WriteString("\t}}\n")
		// The app shell has no top bar, so account/appearance controls live in
		// the sidebar footer (visible on desktop, in the drawer on mobile): the
		// theme toggle always, and the auth action only for the session that has
		// one. Assigned after the literal so pack's AST nav reader still sees a
		// plain Items slice.
		themeToggle := ""
		if len(bp.App.ThemeDark) > 0 {
			themeToggle = "ui.ThemeToggle(ui.ThemeToggleConfig{Ctx: ctx, Variant: ui.ThemeToggleIcon})"
		}
		switch {
		case bp.App.Auth.Enabled:
			sb.WriteString("\tvar authAction render.HTML\n")
			sb.WriteString("\tif u, ok := handler.GetUser(ctx); ok && u != nil {\n")
			sb.WriteString("\t\tauthAction = " + appSpecSignOutExpr(bp) + "\n")
			// Only offer Sign in when a screen actually hosts the login form.
			// Six of the seven shipped app specs never route /login, so an
			// unconditional button would make the app shell's only auth
			// affordance lead a first-time visitor to a 404. With no login
			// screen there is nothing to emit, so the else arm is omitted
			// rather than emitted empty.
			if appSpecHasAuthFormScreen(bp) {
				sb.WriteString("\t} else {\n")
				sb.WriteString(fmt.Sprintf("\t\tauthAction = ui.LinkButton(ui.LinkButtonConfig{Label: %s, Href: %q, Variant: ui.ButtonSecondary, Size: ui.ButtonSizeSmall})\n", tx("Sign in"), appSpecLoginRoute(bp)))
			}
			sb.WriteString("\t}\n")
			if themeToggle != "" {
				// One row, the marketing header's pairing: the account action
				// leads, the appearance glyph closes the row.
				sb.WriteString("\tcfg.Footer = ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM, Align: ui.AlignCenter, Justify: ui.JustifyBetween, NoWrap: true}, authAction, " + themeToggle + ")\n")
			} else {
				sb.WriteString("\tcfg.Footer = authAction\n")
			}
		case themeToggle != "":
			sb.WriteString("\tcfg.Footer = " + themeToggle + "\n")
		}
		sb.WriteString("\treturn cfg\n}\n\n")
	}
	// Layouts are package-level so per-screen mount funcs (screen_<name>.go) can
	// reference them without app.go naming any screen. RegisterGenerated assigns
	// them before calling mountGenerated.
	if len(bp.Nav) > 0 || hasMarketing {
		sb.WriteString("var (\n")
		if len(bp.Nav) > 0 {
			sb.WriteString("\tappLayout *app.Layout\n")
		}
		if hasMarketing {
			sb.WriteString("\tmarketingLayout *app.Layout\n")
		}
		sb.WriteString(")\n\n")
	}
	// Auth + RBAC handles are package-level so a new file (e.g. admin_rbac.go)
	// can wire admin.Config{Policy: rolePolicy, Auth: authMgr}, or extend the
	// policy with finer-grained grants, purely additively, never by editing
	// this generated file. Populated by RegisterGenerated below; nil otherwise.
	if bp.App.Auth.Enabled || rbac {
		sb.WriteString("var (\n")
		if bp.App.Auth.Enabled {
			sb.WriteString("\t// authMgr is the app's auth manager, set by RegisterGenerated when\n")
			sb.WriteString("\t// auth is enabled. Read it from a new file (e.g. to wire admin.Config.Auth).\n")
			sb.WriteString("\tauthMgr *auth.AuthManager\n")
		}
		if rbac {
			sb.WriteString("\t// rolePolicy is the app's RBAC policy, set by RegisterGenerated when an\n")
			sb.WriteString("\t// entity declares access: permissions. Read it from a new file (e.g. to\n")
			sb.WriteString("\t// wire admin.Config.Policy or append finer-grained grants).\n")
			sb.WriteString("\trolePolicy *access.RolePolicy\n")
		}
		sb.WriteString(")\n\n")
	}
	// appUI is the app's entityui value: every generated entity screen
	// renders through it. RegisterGenerated builds it once, after the
	// entities registered (entities.RegisterAll in main.go) and before any
	// screen mounts. extensions.go owns what goes into it.
	sb.WriteString("var appUI *entityui.UI\n\n")
	sb.WriteString("// RegisterGenerated wires generated screens, endpoints, middleware, and plugins.\n")
	sb.WriteString("func RegisterGenerated(fwApp *framework.App, site *app.App, db *sql.DB) {\n")
	sb.WriteString("\tif site == nil {\n")
	sb.WriteString(fmt.Sprintf("\t\tsite = app.NewApp(%q)\n", name))
	sb.WriteString("\t}\n")
	if appspec.AppSpecHasTheme(bp.App) {
		sb.WriteString("\tsite.WithTheme(appTheme())\n")
	} else if hasMarketing {
		// No declared palette, but the marketing chrome's sheets read their
		// own tokens (siteheader's menu stagger): even the default theme
		// must carry them, or every var() in the owned sheets is unset.
		sb.WriteString("\tsite.WithTheme(theme.Default().Extend(siteheader.Tokens))\n")
	}
	if len(bp.Nav) > 0 {
		{
			// Per-request sidebar: its labels translate per request and the
			// footer's auth action depends on the live session, so the shell
			// resolves it the same way the marketing header does. The phone
			// drawer renders the same sidebar, so it is mounted with the
			// builder (MountSidebarFunc) below, never with a config built
			// once at startup.
			sb.WriteString("\tsbComponent :=app.NewContextComponent(func(ctx context.Context) render.HTML {\n")
			// SafeRenderCtx, never Render(): the sidebar component's
			// ctx-aware path is what filters role-gated nav items, and the
			// layout used to invoke it for us. Calling Render() here rendered
			// an admin-only entry for every visitor, including anonymous ones.
			// Mirrors core-ui/app.Layout's own sidebar slot: keep whatever
			// SafeRenderCtx produced (it renders an error boundary on panic)
			// and never swallow it into an empty sidebar.
			sb.WriteString("\t\thtml, _ := component.SafeRenderCtx(ctx, ui.Sidebar(sidebarConfig(ctx)))\n")
			sb.WriteString("\t\treturn html\n")
			sb.WriteString("\t})\n")
		}
		// No standalone app top bar: the sidebar owns brand + nav + the theme
		// toggle (in its footer), so content fills from the top with no empty
		// header band, the way real app shells (Linear/Notion/Stripe) read.
		// The frame is the app's OWN code, composed from the framework's
		// structural pieces (the page-tall stack, the content row): the
		// generated app owns its layout the way it owns its screens, and the
		// design system owns the styling.
		sb.WriteString("\tappLayout = app.NewLayout(\"app\", app.LayoutSpec{}, func(ctx context.Context, l *app.LayoutTree) render.HTML {\n")
		sb.WriteString("\t\tnav, _ := component.SafeRenderCtx(ctx, sbComponent)\n")
		sb.WriteString("\t\treturn ui.Stack(ui.StackConfig{Screen: true, Gap: ui.GapNone},\n")
		sb.WriteString("\t\t\tui.ContentRow(ui.ContentRowConfig{Ctx: ctx, Sidebar: nav}, l.Primary()))\n")
		sb.WriteString("\t})\n")
		sb.WriteString("\tsite.SetDefaultLayout(appLayout)\n")
		sb.WriteString("\tui.MountSidebarFunc(routerMounter{fwApp.Router()}, sidebarConfig)\n")
	}
	if hasMarketing {
		// Rendered per request: the header's words translate per request
		// and its auth action reflects the live session.
		sb.WriteString("\tmarketingLayout = app.NewLayout(\"marketing\", app.LayoutSpec{}, func(ctx context.Context, l *app.LayoutTree) render.HTML {\n")
		sb.WriteString("\t\theader, _ := component.SafeRenderCtx(ctx, app.NewContextComponent(marketingHeader))\n")
		// siteheader / sitefooter render their own <header>/<footer>
		// landmarks (the scope root of each owned style), so the stack
		// takes them directly; the header is the direct child of the
		// page-tall stack, which pins it for the whole page
		// (StickyHeader) and tells the sticky parts below where it ends.
		sb.WriteString("\t\treturn ui.Stack(ui.StackConfig{Screen: true, StickyHeader: true, Gap: ui.GapNone},\n")
		sb.WriteString("\t\t\theader,\n")
		sb.WriteString("\t\t\tui.Container(ui.ContainerConfig{Width: ui.ContainerPage, Pad: ui.ContainerPadPage}, l.Primary()),\n")
		sb.WriteString("\t\t\tmarketingFooter(ctx),\n")
		sb.WriteString("\t\t)\n")
		sb.WriteString("\t})\n")
	}
	// Toast stack: mount a global toast stack so server-side handlers
	// and client-side JS can fire toasts via X-Gofastr-Toast header or
	// window.__gofastr.toast().
	if appSpecNeedsToasts(bp) {
		sb.WriteString("\t{\n")
		sb.WriteString("\t\tstack := preset.ToastStack(\"app-toasts\").Build()\n")
		sb.WriteString("\t\twidget.Mount(fwApp.Router(), &stack)\n")
		sb.WriteString("\t}\n")
	}
	// Auth: wire up the built-in auth system with login, register, logout.
	if bp.App.Auth.Enabled {
		sb.WriteString("\t{\n")
		if bp.App.Auth.DevMode {
			sb.WriteString("\t\t// WARNING: auth runs in DEV MODE: HTTP-friendly cookies (no\n")
			sb.WriteString("\t\t// Secure flag, plain session_id name) and a per-process JWT\n")
			sb.WriteString("\t\t// secret minted at startup. Do NOT deploy like this: set\n")
			sb.WriteString("\t\t// `dev_mode: false` and `jwt_secret` under app.auth in the\n")
			sb.WriteString("\t\t// app spec, serve over HTTPS, then regenerate.\n")
			sb.WriteString("\t\tauthCfg := auth.AuthConfig{DevMode: true")
		} else {
			sb.WriteString("\t\t// Production auth defaults: Secure __Host-session cookie.\n")
			sb.WriteString("\t\t// Requires HTTPS end-to-end: over plain HTTP the browser\n")
			sb.WriteString("\t\t// never echoes the cookie back and login silently breaks.\n")
			sb.WriteString("\t\tauthCfg := auth.AuthConfig{DevMode: false")
		}
		if bp.App.Auth.BasePath != "" {
			sb.WriteString(fmt.Sprintf(", BasePath: %q", bp.App.Auth.BasePath))
		}
		// The signing key is never a committed literal: it comes from
		// JWT_SECRET (the generated .env carries the app spec's value).
		// Empty in DevMode mints a per-process secret; empty in
		// production fails closed at boot.
		sb.WriteString(", JWTSecret: os.Getenv(\"JWT_SECRET\")")
		sb.WriteString("}\n")
		sb.WriteString("\t\tauthCfg.UserStore = auth.NewEntityUserStore(db, \"auth_users\")\n")
		sb.WriteString("\t\tauthCfg.SessionStore = auth.NewEntitySessionStore(db, \"auth_sessions\")\n")
		sb.WriteString("\t\tauthMgr = auth.New(authCfg)\n")
		sb.WriteString("\t\tauthMgr.Use(auth.NewCorePlugin())\n")
		// auth_users / auth_sessions are the auth battery's own tables; it
		// creates them in Init (EnsureSchema). The generated app ships no DDL.
		// Init refuses a bad config (a production app with no JWT key)
		// before it mounts a route, so its error stops the boot: dropped,
		// the app served its screens with no /auth/login.
		sb.WriteString("\t\tif err := authMgr.Init(fwApp); err != nil {\n\t\t\tlog.Fatal(err)\n\t\t}\n")
		// Point the auth battery's form-error redirect at the login page, so a
		// failed form login (wrong password, no Referer) lands back on the login
		// form with ?error=… instead of rendering the raw JSON error body.
		sb.WriteString(fmt.Sprintf("\t\tauth.SetDefaultLoginErrorPath(%q)\n", appSpecLoginRoute(bp)))
		if bp.App.Admin.SeedEmail != "" && bp.App.Admin.SeedPassword != "" {
			sb.WriteString("\t\t// Bootstrap the admin account after auto-migration. The hook runs\n")
			sb.WriteString("\t\t// before the server accepts traffic and returns errors to Start,\n")
			sb.WriteString("\t\t// so a fresh database can never fail silently.\n")
			sb.WriteString("\t\tfwApp.WithSeed(func(ctx context.Context) error {\n")
			adminRole := bp.App.Admin.Role
			if adminRole == "" {
				adminRole = "admin"
			}
			sb.WriteString(fmt.Sprintf("\t\t\tu, _, err := authCfg.UserStore.FindByEmail(ctx, %q)\n", bp.App.Admin.SeedEmail))
			sb.WriteString("\t\t\tif err == nil {\n")
			sb.WriteString("\t\t\t\t// An admin seeded before email_verified existed reads as\n")
			sb.WriteString("\t\t\t\t// unverified. Only an account holding the admin role came\n")
			sb.WriteString("\t\t\t\t// from this seed; one without it was registered by someone\n")
			sb.WriteString("\t\t\t\t// else and stays unproven.\n")
			sb.WriteString(fmt.Sprintf("\t\t\t\tif v, ok := authCfg.UserStore.(auth.EmailVerifier); ok && slices.Contains(u.GetRoles(), %q) {\n", adminRole))
			sb.WriteString("\t\t\t\t\tif err := v.MarkEmailVerified(ctx, u.GetID()); err != nil {\n")
			sb.WriteString("\t\t\t\t\t\treturn fmt.Errorf(\"verify bootstrap admin email: %w\", err)\n")
			sb.WriteString("\t\t\t\t\t}\n")
			sb.WriteString("\t\t\t\t}\n")
			sb.WriteString("\t\t\t\treturn nil\n")
			sb.WriteString("\t\t\t}\n")
			sb.WriteString("\t\t\tif err != auth.ErrUserNotFound {\n\t\t\t\treturn fmt.Errorf(\"find bootstrap admin: %w\", err)\n\t\t\t}\n")
			sb.WriteString("\t\t\t// Only a genuinely fresh database (admin absent) needs the seed\n")
			sb.WriteString("\t\t\t// password; an already-seeded deployment boots without it.\n")
			sb.WriteString("\t\t\tseedPw := os.Getenv(\"ADMIN_SEED_PASSWORD\")\n")
			sb.WriteString("\t\t\tif seedPw == \"\" {\n\t\t\t\treturn fmt.Errorf(\"ADMIN_SEED_PASSWORD is not set: admin %q cannot be seeded on a fresh database\", " + fmt.Sprintf("%q", bp.App.Admin.SeedEmail) + ")\n\t\t\t}\n")
			sb.WriteString("\t\t\th, err := auth.HashPassword(seedPw)\n")
			sb.WriteString("\t\t\tif err != nil {\n\t\t\t\treturn fmt.Errorf(\"hash bootstrap admin password: %w\", err)\n\t\t\t}\n")
			seedRoles := appSpecSeedAdminRoles(bp)
			sb.WriteString(fmt.Sprintf("\t\t\tu, err = authCfg.UserStore.CreateUser(ctx, %q, h, []string{%q, %q})\n", bp.App.Admin.SeedEmail, seedRoles[0], seedRoles[1]))
			sb.WriteString("\t\t\tif err != nil && err != auth.ErrEmailTaken {\n")
			sb.WriteString("\t\t\t\treturn fmt.Errorf(\"create bootstrap admin: %w\", err)\n")
			sb.WriteString("\t\t\t}\n")
			sb.WriteString("\t\t\t// The operator chose this address, so it counts as proven.\n")
			sb.WriteString("\t\t\t// Left unverified, the first magic link to it would claim\n")
			sb.WriteString("\t\t\t// the account and clear the seeded password.\n")
			sb.WriteString("\t\t\tif v, ok := authCfg.UserStore.(auth.EmailVerifier); ok && u != nil {\n")
			sb.WriteString("\t\t\t\tif err := v.MarkEmailVerified(ctx, u.GetID()); err != nil {\n")
			sb.WriteString("\t\t\t\t\treturn fmt.Errorf(\"verify bootstrap admin email: %w\", err)\n")
			sb.WriteString("\t\t\t\t}\n")
			sb.WriteString("\t\t\t}\n")
			sb.WriteString("\t\t\treturn nil\n")
			sb.WriteString("\t\t})\n")
		}
		sb.WriteString("\t\t// Resolve the session cookie to a user on every request so\n")
		sb.WriteString("\t\t// owner/access-scoped CRUD sees the logged-in user. Without\n")
		sb.WriteString("\t\t// this, authorized requests fail closed (401) just like\n")
		sb.WriteString("\t\t// anonymous ones.\n")
		sb.WriteString("\t\tfwApp.Use(auth.SessionMiddleware(authMgr))\n")
		if roleNav {
			// Role-aware nav: the sidebar/drawer filter role-restricted items
			// (e.g. an admin-only link) by the signed-in user's roles, so a
			// link the user can't use never appears.
			sb.WriteString("\t\tui.SetRolesExtractor(func(ctx context.Context) []string {\n")
			sb.WriteString("\t\t\tif u, ok := handler.GetUser(ctx); ok && u != nil {\n")
			sb.WriteString("\t\t\t\tif rh, ok := u.(interface{ GetRoles() []string }); ok {\n")
			sb.WriteString("\t\t\t\t\treturn rh.GetRoles()\n")
			sb.WriteString("\t\t\t\t}\n")
			sb.WriteString("\t\t\t}\n")
			sb.WriteString("\t\t\treturn nil\n")
			sb.WriteString("\t\t})\n")
		}
		if appSpecHasEntityAccess(bp) {
			adminRole := bp.App.Admin.Role
			if adminRole == "" {
				adminRole = "admin"
			}
			sb.WriteString("\t\t// Entities declare `access:` permissions; install a RolePolicy so the\n")
			sb.WriteString("\t\t// signed-in user's roles resolve to those permissions on the gated\n")
			sb.WriteString("\t\t// CRUD API. The admin role holds the wildcard (full access, the same\n")
			sb.WriteString("\t\t// surface the back-office manages). Add finer per-role Grants here,\n")
			sb.WriteString("\t\t// OR from a new file that appends to rolePolicy: both are additive\n")
			sb.WriteString("\t\t// now that rolePolicy is package-level. Without this, every write 403s.\n")
			sb.WriteString("\t\trolePolicy = access.NewRolePolicy()\n")
			sb.WriteString(fmt.Sprintf("\t\trolePolicy.Grant(%q, access.Wildcard)\n", adminRole))
			sb.WriteString("\t\tfwApp.Use(access.Middleware(rolePolicy, func(ctx context.Context) []string {\n")
			sb.WriteString("\t\t\tif u, ok := handler.GetUser(ctx); ok && u != nil {\n")
			sb.WriteString("\t\t\t\tif rh, ok := u.(interface{ GetRoles() []string }); ok {\n")
			sb.WriteString("\t\t\t\t\treturn rh.GetRoles()\n")
			sb.WriteString("\t\t\t\t}\n")
			sb.WriteString("\t\t\t}\n")
			sb.WriteString("\t\t\treturn nil\n")
			sb.WriteString("\t\t}))\n")
		}
		sb.WriteString("\t\t// auth.CSRF is intentionally NOT mounted: this generated surface\n")
		sb.WriteString("\t\t// is JSON-first (REST CRUD + /mcp), and the CSRF middleware 403s\n")
		sb.WriteString("\t\t// any unsafe-method request that doesn't echo the csrf cookie as\n")
		sb.WriteString("\t\t// an X-CSRF-Token header, which plain JSON/MCP clients don't.\n")
		sb.WriteString("\t\t// Session cookies are SameSite=Strict, so cross-site form posts\n")
		sb.WriteString("\t\t// don't carry the session in modern browsers. If you add browser\n")
		sb.WriteString("\t\t// HTML forms, mount auth.CSRF: see `gofastr docs app-spec`\n")
		sb.WriteString("\t\t// (Auth section) and `gofastr docs auth`.\n")
		sb.WriteString("\t}\n")
	}
	// Screens (authored + synthesized CRUD) mount themselves: each screen_*.go
	// self-registers a mount func, and mountGenerated runs them in declaration
	// order. app.go names no screen type or entity resource. Adding a screen
	// or an entity fragment is a new file, never an edit here. The call is
	// unconditional (the seam ships even with zero screens) so a later
	// `--add`/scaffold screen mounts without editing this owned file.
	// One UI per app, built after the entities registered and before any
	// screen mounts. EntityUI panics at boot on a bad name in
	// appExtensions, the way App.Entity refuses a bad declaration.
	sb.WriteString("\tappUI = fwApp.EntityUI(appExtensions)\n")
	sb.WriteString("\tmountGenerated(fwApp, site, db)\n")
	for _, item := range bp.Middleware {
		// Same gate as the stub emitter: never reference a name that is
		// not a Go identifier (the stub was skipped, so referencing it
		// here would not compile).
		if !project.IsGoIdentifier(appspec.ToCamelCase(item.Name)) {
			continue
		}
		sb.WriteString(fmt.Sprintf("\tfwApp.Use(%sMiddleware)\n", appspec.ToCamelCase(item.Name)))
	}
	for _, item := range bp.Plugins {
		if !project.IsGoIdentifier(appspec.ToCamelCase(item.Name)) {
			continue // same gate as the middleware registration above
		}
		sb.WriteString(fmt.Sprintf("\tfwApp.RegisterPlugin(%sPlugin{})\n", appspec.ToCamelCase(item.Name)))
	}
	if len(bp.Nav) > 0 {
		sb.WriteString("\t_ = routerMounter{}\n")
	}
	sb.WriteString("}\n\n")
	if len(bp.Nav) > 0 {
		sb.WriteString("// routerMounter adapts framework's *router.Router to ui.WidgetMounter.\n")
		sb.WriteString("type routerMounter struct{ r *router.Router }\n\n")
		sb.WriteString("func (m routerMounter) MountWidget(def *widget.Definition) {\n")
		sb.WriteString("\twidget.Mount(m.r, def)\n")
		sb.WriteString("}\n")
	}
	return appSpecChromeWithPhrases(sb.String(), bp.PhraseCollector())
}

// appSpecBaseCSSFunc emits the appBaseCSS() function. It returns the
// empty string: every generated surface, marketing, app, entity list/detail,
// entity forms, auth, composes framework/ui components and core-ui/app layouts
// that ship their own CSS (auto-injected by the UI host). The generator ships
// ZERO bespoke styling, which is the proof the design system is cohesive and
// composable. This owned function stays as an extension point: an app can add
// its own base CSS here (or, preferably, in static/app.css).
func appSpecBaseCSSFunc() string {
	var sb strings.Builder
	sb.WriteString("// appBaseCSS is an owned extension point for app-specific base CSS.\n")
	sb.WriteString("// It's empty by default: every generated surface composes framework/ui\n")
	sb.WriteString("// components and core-ui/app layouts that ship their own CSS, so the\n")
	sb.WriteString("// generated app ships no bespoke styling. Add app CSS here or in static/app.css.\n")
	sb.WriteString("func appBaseCSS() string {\n")
	sb.WriteString("\treturn \"\"\n")
	sb.WriteString("}\n\n")
	return sb.String()
}

func renderNavItemGo(bp appspec.AppSpec, sb *strings.Builder, item appspec.AppSpecNavItem, indent string) {
	sb.WriteString(fmt.Sprintf("%s{Label: %s, Href: %q", indent, appSpecTx(bp, item.Label), item.Href))
	if item.Role != "" {
		sb.WriteString(fmt.Sprintf(", Roles: []string{%q}", item.Role))
	}
	// Icon renders in the sidebar's raw-HTML icon slot (render.HTML), so
	// it is emitted only when it passes the same grammar validateAppSpec
	// enforces: plain text or an emoji, never markup. validateAppSpec is
	// the loud gate; this backstop keeps an unvalidated app spec from
	// shipping an icon that escapes the slot.
	if item.Icon != "" && appspec.AppSpecNavIconSafe(item.Icon) {
		// Through the design system's icon registry, never as raw
		// markup: a registered name renders its SVG, an unregistered
		// one renders nothing (ui.Icon's contract), so the literal name
		// can never appear beside the label.
		sb.WriteString(fmt.Sprintf(", Icon: ui.Icon(%q, ui.IconConfig{})", item.Icon))
	}
	if len(item.Items) > 0 {
		sb.WriteString(", Children: []ui.SidebarItem{\n")
		for _, child := range item.Items {
			renderNavItemGo(bp, sb, child, indent+"\t")
		}
		sb.WriteString(indent + "}")
	}
	sb.WriteString("},\n")
}

// appSpecFontFamilyName extracts the primary family name from a theme font
// value like "Hanken Grotesk" (or a full stack the author wrote), stripping
// quotes and any trailing fallback so it can be turned into a Google Fonts query.
// appSpecFontFamilyName extracts the first family from a font stack
// and reduces it to characters that are safe inside the emitted CSS
// string literal.
//
// Trimming quotes only at the edges was not enough: a comma-free value
// whose quote sits in the INTERIOR passed through untouched and closed
// the `font-family: '…'` literal, letting spec text append arbitrary
// rules to the app-wide stylesheet (which is fed to both
// uihost.WithCustomCSS and the admin battery). Real Google Fonts
// families are exactly this character set, so the allow-list costs
// nothing legitimate.
func appSpecFontFamilyName(v string) string {
	v = strings.TrimSpace(v)
	if i := strings.IndexByte(v, ','); i >= 0 {
		v = v[:i]
	}
	v = strings.Trim(strings.TrimSpace(v), "'\"")
	var b strings.Builder
	for _, r := range v {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

// appSpecUnsafeColorNote is the emitter-side backstop for theme color values.
// validateAppSpec is the loud gate (it fails generation and names the token),
// but the emitter is reachable from `loadAppSpecPath(path, false)` and from a
// hand-built App spec, and a theme value that reaches CSS unchecked is a
// stylesheet-injection sink. When the value fails style.ValidateColorValue this
// returns a generated comment to emit INSTEAD of the assignment, so the token
// falls back to the DefaultTheme value.
//
// Dropping is deliberate: the value cannot be sanitized into a color, and a
// comment in the generated source is visible to whoever reads app.go, unlike a
// silent skip. The empty string means "safe, emit the assignment".
func appSpecUnsafeColorNote(key, value string) string {
	err := style.ValidateColorValue(value)
	if err == nil {
		return ""
	}
	// The rejected value never enters the comment; a value carrying "*/"
	// would close it and the rest would become live Go.
	return fmt.Sprintf("\t// theme %s dropped: %s (see validateAppSpec)\n", appSpecCommentSafe(key), appSpecCommentSafe(err.Error()))
}

// appSpecCommentSafe reduces s to bytes that cannot terminate or escape the
// single-line Go comment it is interpolated into.
func appSpecCommentSafe(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\n' || r == '\r':
			b.WriteByte(' ')
		case r == '*' || r == '/' || r == '\\':
			b.WriteByte('.')
		case r < 0x20 || r == 0x7f:
			// dropped
		default:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

// appSpecConfiguredFonts returns the heading + body family names declared in
// the app spec theme (font_heading / font_display alias, font_body).
func appSpecConfiguredFonts(theme map[string]string) (body, heading string) {
	body = appSpecFontFamilyName(theme["font_body"])
	heading = appSpecFontFamilyName(theme["font_heading"])
	if heading == "" {
		heading = appSpecFontFamilyName(theme["font_display"])
	}
	return body, heading
}

const appSpecFontSysStack = "ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif"

// appSpecFontStacks builds the full CSS font-family stacks for the theme's
// body and heading tokens, appending a standard system font fallback (and
// letting the heading fall back through the body family first).
func appSpecFontStacks(theme map[string]string) (bodyStack, headingStack string) {
	body, heading := appSpecConfiguredFonts(theme)
	if body != "" {
		bodyStack = fmt.Sprintf("'%s', %s", body, appSpecFontSysStack)
	}
	if heading != "" {
		if body != "" {
			headingStack = fmt.Sprintf("'%s', '%s', %s", heading, body, appSpecFontSysStack)
		} else {
			headingStack = fmt.Sprintf("'%s', %s", heading, appSpecFontSysStack)
		}
	}
	return bodyStack, headingStack
}

// appSpecFontSlug turns a font family name into the self-hosted file slug,
// e.g. "Bricolage Grotesque" -> "bricolage-grotesque".
// It delegates to style.FontSlug so the slug in the emitted `src: url(…)`
// and the slug in the woff2 path the developer is told to write to are
// produced by ONE function. They were two implementations that agreed on
// space-separated names and diverged on anything with punctuation; a
// family like "Libre Baskerville, Display" would have been served from a
// path nothing wrote to, with no error anywhere.
func appSpecFontSlug(family string) string {
	return style.FontSlug(family)
}

// appSpecConfiguredFontFamilies returns the theme's declared font families in
// @font-face order (heading first, then body), de-duplicated. Empty when the
// theme names no fonts.
func appSpecConfiguredFontFamilies(theme map[string]string) []string {
	body, heading := appSpecConfiguredFonts(theme)
	var out []string
	seen := map[string]bool{}
	for _, f := range []string{heading, body} {
		if f == "" || seen[f] {
			continue
		}
		seen[f] = true
		out = append(out, f)
	}
	return out
}

// appSpecEffectiveStaticDir is the directory the generated app serves static
// files from. It's the app spec's static_dir when set; otherwise "static" when
// the theme declares self-hosted fonts (so the emitted woff2 files have a home
// and a serving route). Empty only when neither applies.
func appSpecEffectiveStaticDir(bp appspec.AppSpec) string {
	if bp.App.StaticDir != "" {
		return bp.App.StaticDir
	}
	if len(appSpecConfiguredFontFamilies(bp.App.Theme)) > 0 {
		return "static"
	}
	// A PWA needs a home for its scaffolded icons (and a serving route
	// for them), same as self-hosted fonts.
	if bp.App.PWA.Enabled {
		return "static"
	}
	return ""
}

// FontFetcher resolves a Google Fonts family name to a self-hostable woff2 file
// (the regular-weight latin subset). It's a package var so the test suite can
// stub it; the real implementation makes network calls, which tests never do.
var FontFetcher = fetchGoogleFontWoff2

// appSpecFontHTTPClient bounds the generate-time font fetch so an unreachable
// or slow CDN degrades to the offline warning path instead of hanging generate.
var appSpecFontHTTPClient = &http.Client{Timeout: 20 * time.Second}

// fetchGoogleFontWoff2 downloads the latin woff2 subset for a Google Fonts
// family. It queries the Google Fonts CSS API with a modern User-Agent (so the
// API returns woff2, not ttf), extracts the first gstatic .woff2 URL (preferring
// the latin subset), and downloads it. Any network/parse failure returns an
// error; generation then falls back to a loud warning rather than a silent 404.
func fetchGoogleFontWoff2(family string) ([]byte, error) {
	q := url.Values{}
	q.Set("family", family+":wght@400;700")
	q.Set("display", "swap")
	cssURL := "https://fonts.googleapis.com/css2?" + q.Encode()
	css, err := appSpecFontHTTPGet(cssURL)
	if err != nil {
		return nil, err
	}
	woffURL := appSpecFirstWoff2URL(string(css))
	if woffURL == "" {
		return nil, fmt.Errorf("no woff2 url in Google Fonts response for %q", family)
	}
	return appSpecFontHTTPGet(woffURL)
}

// appSpecFontHTTPGet fetches a URL with a browser-like User-Agent and returns
// its body, erroring on any non-2xx status.
func appSpecFontHTTPGet(u string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	// The CSS API serves woff2 only to modern browsers; identify as one.
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36")
	resp, err := appSpecFontHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("GET %s: status %d", u, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
}

// appSpecFirstWoff2URL pulls the first gstatic .woff2 URL out of a Google
// Fonts css2 response, preferring the block tagged `/* latin */`.
//
// The returned URL is handed straight to appSpecFontHTTPGet, so the fetch
// target is chosen by the RESPONSE BODY, not by us. The comment above has always
// said "gstatic"; the code only checked for ".woff2" anywhere in the string, so
// a hijacked or redirected css2 response could point the fetch at any host it
// liked. The app spec author cannot reach this (the `family` query param is
// reduced to [A-Za-z0-9 _-] first), which is why it is a contract gap rather
// than a app-spec-driven SSRF, closed by making the code enforce what the
// comment claims.
func appSpecFirstWoff2URL(css string) string {
	extract := func(s string) string {
		i := strings.Index(s, "url(")
		if i < 0 {
			return ""
		}
		s = s[i+len("url("):]
		j := strings.Index(s, ")")
		if j < 0 {
			return ""
		}
		u := strings.Trim(strings.TrimSpace(s[:j]), "'\"")
		if appSpecIsGstaticWoff2(u) {
			return u
		}
		return ""
	}
	if i := strings.Index(css, "/* latin */"); i >= 0 {
		if u := extract(css[i:]); u != "" {
			return u
		}
	}
	// Fall back to the first woff2 anywhere in the response.
	for rest := css; ; {
		i := strings.Index(rest, "url(")
		if i < 0 {
			return ""
		}
		if u := extract(rest[i:]); u != "" {
			return u
		}
		rest = rest[i+len("url("):]
	}
}

// appSpecHasMarketing reports whether any screen mounts the marketing
// layout: the condition the marketing chrome (the siteheader/sitefooter
// packages and the marketingLayout shell) is emitted under.
func appSpecHasMarketing(bp appspec.AppSpec) bool {
	for _, s := range bp.Screens {
		if s.Layout == "marketing" {
			return true
		}
	}
	return false
}

// barActions renders the Go expression for the header bar's actions:
// the auth action the live session decides, plus the theme toggle when
// the app declares a dark scheme. The owned header's .end cell lays its
// children out with the bar's own gap, so the two are joined as
// siblings rather than nested in a wrapper cluster.
func barActions(action string, toggle bool) string {
	if !toggle {
		return action
	}
	return fmt.Sprintf("render.Join(\n\t\t\t%s,\n\t\t\tui.ThemeToggle(ui.ThemeToggleConfig{Ctx: ctx, Variant: ui.ThemeToggleIcon}),\n\t\t)", action)
}

// chromePackageImport returns the import-path base of the app's chrome
// packages: the module path, plus the output dir when app.output_dir /
// --out places the app in a subpackage. renderAppSpecApp emits the
// two imports from it and renderAppSpecFilesWithOrder copies the
// canonical packages to the same paths, so the two can never disagree.
func chromePackageImport(bp appspec.AppSpec) string {
	outputDir := strings.TrimSuffix(strings.TrimPrefix(filepath.ToSlash(bp.App.OutputDir), "./"), "/")
	imp := strings.TrimSuffix(bp.App.Module, "/")
	if outputDir != "" && outputDir != "." {
		imp += "/" + outputDir
	}
	return imp
}

// appSpecIsGstaticWoff2 reports whether u is an https fonts.gstatic.com URL
// whose path ends in .woff2: the only thing generation is willing to fetch out
// of a Google Fonts css2 response.
//
// Parsed, not prefix-matched: a `strings.HasPrefix(u, "https://fonts.gstatic.com")`
// test also accepts https://fonts.gstatic.com.attacker.test/x.woff2. And the
// ".woff2" test is on the PATH's suffix, not on the whole URL, so it cannot be
// satisfied by a query string or fragment (…/evil?x=.woff2).
//
// If Google ever moves the font CDN, generation reports the family in `missing`
// and warns; the app still builds, minus the self-hosted font.
func appSpecIsGstaticWoff2(u string) bool {
	parsed, err := url.Parse(u)
	if err != nil {
		return false
	}
	if parsed.Scheme != "https" || parsed.Host != "fonts.gstatic.com" {
		return false
	}
	return strings.HasSuffix(parsed.Path, ".woff2")
}

// appSpecFontAssets fetches every configured font family and returns the
// woff2 files to emit under <static>/fonts/. Families whose fetch fails are
// reported in `missing` (as their emitted relative path) so the caller can warn;
// generation still proceeds so an offline run produces a working app minus the
// custom fonts.
func appSpecFontAssets(bp appspec.AppSpec) (files []GeneratedFile, missing []string) {
	dir := appSpecEffectiveStaticDir(bp)
	for _, fam := range appSpecConfiguredFontFamilies(bp.App.Theme) {
		rel := appSpecFontRelPath(dir, fam)
		data, err := FontFetcher(fam)
		if err != nil || len(data) == 0 {
			missing = append(missing, rel)
			continue
		}
		files = append(files, GeneratedFile{Name: rel, Content: string(data)})
	}
	return files, missing
}

// appSpecPWAOptionLiteral renders the uihost.WithPWA(...) call for the
// generated main.go. Only author-declared fields are emitted (so pack
// recovers exactly the authored app spec); the framework applies the
// defaults at serve time. The icon set always points at the scaffolded
// placeholder files.
func appSpecPWAOptionLiteral(bp appspec.AppSpec) string {
	p := bp.App.PWA
	var fields []string
	if p.Name != "" {
		fields = append(fields, fmt.Sprintf("Name: %q", p.Name))
	}
	if p.ShortName != "" {
		fields = append(fields, fmt.Sprintf("ShortName: %q", p.ShortName))
	}
	if p.Description != "" {
		fields = append(fields, fmt.Sprintf("Description: %q", p.Description))
	}
	if p.StartURL != "" {
		fields = append(fields, fmt.Sprintf("StartURL: %q", p.StartURL))
	}
	if p.Scope != "" {
		fields = append(fields, fmt.Sprintf("Scope: %q", p.Scope))
	}
	if p.Display != "" {
		fields = append(fields, "Display: "+appSpecPWADisplayConst(p.Display))
	}
	if p.ThemeColor != "" {
		fields = append(fields, fmt.Sprintf("ThemeColor: %q", p.ThemeColor))
	}
	if p.BackgroundColor != "" {
		fields = append(fields, fmt.Sprintf("BackgroundColor: %q", p.BackgroundColor))
	}
	// The framework's built-in deny list covers the default /api and
	// /auth mounts; a custom api_prefix or auth base_path must follow
	// its app's real mounts or the never-precache/never-intercept
	// guarantee silently stops covering the API.
	var deny []string
	if prefix := strings.Trim(bp.App.APIPrefix, "/"); prefix != "" && prefix != "api" {
		deny = append(deny, "/"+prefix)
	}
	if bp.App.Auth.Enabled {
		if base := appSpecAuthBase(bp); base != "/auth" {
			deny = append(deny, base)
		}
	}
	if len(deny) > 0 {
		quoted := make([]string, len(deny))
		for i, d := range deny {
			quoted[i] = fmt.Sprintf("%q", d)
		}
		fields = append(fields, "DenyPaths: []string{"+strings.Join(quoted, ", ")+"}")
	}
	fields = append(fields, `Icons: []uihost.PWAIcon{{Src: "/icons/icon-192.png", Sizes: "192x192", Type: "image/png"}, {Src: "/icons/icon-512.png", Sizes: "512x512", Type: "image/png"}, {Src: "/icons/icon-maskable.png", Sizes: "512x512", Type: "image/png", Purpose: uihost.PWAIconPurposeMaskable}}`)
	return "uihost.WithPWA(uihost.PWAConfig{" + strings.Join(fields, ", ") + "})"
}

// appSpecPWADisplayConst maps a validated app spec display mode to
// the typed uihost constant emitted into generated code.
func appSpecPWADisplayConst(display string) string {
	switch display {
	case "fullscreen":
		return "uihost.PWADisplayFullscreen"
	case "minimal-ui":
		return "uihost.PWADisplayMinimalUI"
	case "browser":
		return "uihost.PWADisplayBrowser"
	default:
		return "uihost.PWADisplayStandalone"
	}
}

// appSpecPWAIconAssets emits the replaceable placeholder icons the app
// generator scaffolds for app.pwa: 192px and 512px "any" icons plus a 512px
// maskable variant, colored from the declared theme_color (falling back
// to the theme's primary token, then the framework indigo). Generated
// in-process. Deterministic PNG bytes, no network fetch.
func appSpecPWAIconAssets(bp appspec.AppSpec) []GeneratedFile {
	if !bp.App.PWA.Enabled {
		return nil
	}
	dir := appSpecEffectiveStaticDir(bp)
	fill := bp.App.PWA.ThemeColor
	if fill == "" {
		fill = bp.App.Theme["primary"]
	}
	bg, ok := appSpecParseHexColor(fill)
	if !ok {
		bg = color.NRGBA{R: 0x4f, G: 0x46, B: 0xe5, A: 0xff} // indigo fallback
	}
	rel := func(name string) string {
		return filepath.ToSlash(filepath.Join(dir, "icons", name))
	}
	return []GeneratedFile{
		{Name: rel("icon-192.png"), Content: string(appSpecPWAIconPNG(192, bg))},
		{Name: rel("icon-512.png"), Content: string(appSpecPWAIconPNG(512, bg))},
		// The maskable icon uses the same full-bleed art: a solid
		// background with a centered dot keeps all content inside the
		// mask safe zone regardless of the platform's crop shape.
		{Name: rel("icon-maskable.png"), Content: string(appSpecPWAIconPNG(512, bg))},
	}
}

// appSpecPWAIconPNG draws the placeholder icon: a solid brand-color
// square with a centered translucent-white dot, well inside the
// maskable safe zone.
func appSpecPWAIconPNG(size int, bg color.NRGBA) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	dot := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xd9}
	c := float64(size) / 2
	r := float64(size) * 0.22
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx, dy := float64(x)+0.5-c, float64(y)+0.5-c
			if dx*dx+dy*dy <= r*r {
				img.SetNRGBA(x, y, dot)
			} else {
				img.SetNRGBA(x, y, bg)
			}
		}
	}
	var buf bytes.Buffer
	// Encode cannot fail on an in-memory buffer with a valid image.
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

// appSpecParseHexColor parses #rgb / #rrggbb into an opaque color.
func appSpecParseHexColor(s string) (color.NRGBA, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	switch len(s) {
	case 3:
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	case 6:
	default:
		return color.NRGBA{}, false
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return color.NRGBA{}, false
	}
	return color.NRGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xff}, true
}

// appSpecFontRelPath is the emitted path for a family's woff2 file, matching
// the /fonts/<slug>.woff2 URL in fontFaceCSS (the static dir is served at root).
func appSpecFontRelPath(staticDir, family string) string {
	return filepath.ToSlash(filepath.Join(staticDir, "fonts", appSpecFontSlug(family)+".woff2"))
}

// appSpecMissingFontSlugs reports which configured font files are absent from
// the rendered file set (e.g. an offline fetch dropped them) so generate can
// warn with the exact paths the user must supply.
func appSpecMissingFontSlugs(bp appspec.AppSpec, files []GeneratedFile) []string {
	fams := appSpecConfiguredFontFamilies(bp.App.Theme)
	if len(fams) == 0 {
		return nil
	}
	present := make(map[string]bool, len(files))
	for _, f := range files {
		present[f.Name] = true
	}
	dir := appSpecEffectiveStaticDir(bp)
	var missing []string
	for _, fam := range fams {
		if rel := appSpecFontRelPath(dir, fam); !present[rel] {
			missing = append(missing, rel)
		}
	}
	return missing
}

// appSpecFontFaceCSS returns the @font-face rules for the theme's declared
// families, self-hosted from <static>/fonts/<slug>.woff2. This is the SINGLE
// font-loading source; it's prepended to the UI host's stylesheet AND handed to
// the admin battery, so every surface (marketing, app, back-office) loads the
// exact same fonts with no external CDN dependency. Drop the matching woff2 into
// <static_dir>/fonts/. Returns "" when the theme declares no fonts.
// The rule itself is emitted by core-ui/style, not spelled out here: a
// generator that writes its own `@font-face` string owns a second
// styling surface, which is the drift CLAUDE.md hard rule 7 is about.
// `gofastr verify` flags this exact shape (GOFASTR1801), and
// style.FontFaceCSS is the upstream primitive that finding asked for.
func appSpecFontFaceCSS(theme map[string]string) string {
	body, heading := appSpecConfiguredFonts(theme)
	// Heading first, then body: the declaration order callers rely on.
	return style.FontFaceCSS("", style.WebFont{Family: heading}, style.WebFont{Family: body})
}

// appSpecFontFaceDecl emits the generated app's fontFaceCSS declaration.
//
// It emits a CALL to style.FontFaceCSS rather than a baked CSS string.
// Emitting the string would move the violation rather than fix it: the
// generator would stop shipping CSS only by writing the CSS into every
// app it generates, where `gofastr verify` would correctly flag it and
// the developer could do nothing about it. Composing the design system
// is the shape hard rule 7 asks for, and the generated file already
// imports core-ui/style for its theme.
func appSpecFontFaceDecl(theme map[string]string) string {
	// Emitted even when no fonts are declared: main.go references
	// fontFaceCSS unconditionally, so omitting the declaration does not
	// produce a smaller app, it produces one that does not compile. With
	// no fonts the call returns "".
	families := appSpecConfiguredFontFamilies(theme)
	var fonts strings.Builder
	for _, f := range families {
		fmt.Fprintf(&fonts, ", style.WebFont{Family: %q}", f)
	}
	return fmt.Sprintf(
		"// fontFaceCSS holds the @font-face rules for the app's fonts, shared by\n"+
			"// the UI host and the admin battery so every surface loads identical fonts.\n"+
			"// Drop the matching woff2 files into <static>/fonts/.\n"+
			"var fontFaceCSS = style.FontFaceCSS(\"\"%s)\n\n", fonts.String())
}

// appSpecNeedsToasts returns true when any screen uses entity_form or
// entity_list; these blocks benefit from toast feedback on CRUD actions.
func appSpecNeedsToasts(bp appspec.AppSpec) bool {
	for _, screen := range bp.Screens {
		for _, block := range screen.Body {
			if appSpecBlockNeedsToasts(block) {
				return true
			}
		}
	}
	return false
}

func appSpecBlockNeedsToasts(block appspec.AppSpecBlock) bool {
	kind := strings.ToLower(strings.TrimSpace(block.Kind))
	if kind == "" {
		kind = strings.ToLower(strings.TrimSpace(block.Type))
	}
	if kind == "entity_form" || kind == "entity_list" || kind == "entity_detail" {
		return true
	}
	for _, child := range block.Children {
		if appSpecBlockNeedsToasts(child) {
			return true
		}
	}
	return false
}
