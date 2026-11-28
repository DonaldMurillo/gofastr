package appspec

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/project"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/framework"
	fwentity "github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
	"github.com/DonaldMurillo/gofastr/internal/asciicase"
)

// authBasePathRE matches an app.auth.base_path: one or more "/segment"
// with no trailing slash. A segment cannot start with a dot, so "." and
// ".." never reach a mount.
var authBasePathRE = regexp.MustCompile(`^(/[A-Za-z0-9_-][A-Za-z0-9._-]*)+$`)

// validateDetailRoutes holds every entity_detail screen to the route shape
// entityui links records at: <list route>/{id} under one of the entity's
// list screens, which becomes its home (appSpecEntityListScreen). Every
// list's record links point there, so a detail screen anywhere else
// renders fine but is unreachable. An entity with no list screen keeps
// only the {id} requirement its own screen needs.
func validateDetailRoutes(bp AppSpec) error {
	var errs schemaErrors
	for _, s := range bp.Screens {
		for _, entity := range entityDetailsOn(s.Body) {
			base := strings.TrimRight(s.Route, "/")
			base = strings.TrimSuffix(base, "/{id}")
			base = strings.TrimSuffix(base, "/:id")
			var listRoutes []string
			underList := false
			for _, ls := range bp.Screens {
				if found, _ := entityListOn(ls.Body, entity); found {
					route := strings.TrimRight(ls.Route, "/")
					listRoutes = append(listRoutes, route)
					underList = underList || route == base
				}
			}
			if len(listRoutes) > 0 && !underList {
				errs.add(fmt.Errorf("app spec: screen %q shows entity %q at route %q, but no list of it lives at %q: the detail screen must sit at %q/{id}, where the list's record links point", s.Name, entity, s.Route, base, listRoutes[0]))
			}
		}
	}
	return errs.err()
}

// schemaErrors accumulates validation findings so one pass reports every
// fixable problem rather than only the first. The alternative is a serial
// guess-and-recompile loop: fix one error, rerun, meet the next. A single
// finding returns unchanged (existing exact-shape callers keep their
// message); multiple findings join under a count.
type schemaErrors struct{ errs []error }

func (s *schemaErrors) add(err error) {
	if err != nil {
		s.errs = append(s.errs, err)
	}
}

func (s *schemaErrors) err() error {
	switch len(s.errs) {
	case 0:
		return nil
	case 1:
		return s.errs[0]
	default:
		msgs := make([]string, len(s.errs))
		for i, err := range s.errs {
			msgs[i] = err.Error()
		}
		return fmt.Errorf("app spec has %d validation errors:\n  - %s", len(s.errs), strings.Join(msgs, "\n  - "))
	}
}

func ValidateAppSpec(bp AppSpec) error {
	var errs schemaErrors
	// Production auth without a signing key: the generated app's auth
	// battery fails closed at boot (battery/auth Init refuses an empty
	// JWTSecret with DevMode=false). Fail at generate/validate time
	// instead, with the same remedy.
	if bp.App.Auth.Enabled && !bp.App.Auth.DevMode && bp.App.Auth.JWTSecret == "" {
		errs.add(fmt.Errorf("app spec: app.auth has dev_mode: false but no jwt_secret: the generated app would refuse to boot (production auth requires a signing key); set jwt_secret from your secret store, or set dev_mode: true for local development"))
	}
	// The battery mounts its routes at base_path + "/login" and the
	// generated forms, sign-out button and e2e test post there. A trailing
	// slash mounts "//login", a missing leading one a relative path, and
	// "//host" would be a protocol-relative form action.
	if p := bp.App.Auth.BasePath; p != "" && !authBasePathRE.MatchString(p) {
		errs.add(fmt.Errorf("app spec: app.auth.base_path %q must be an absolute path such as /account: segments of letters, digits, '-', '_' and '.', none starting with a dot, and no trailing slash", p))
	}
	if why := staticDirProblem(bp.App.StaticDir, bp.App.DBURL, bp.App.OutputDir); why != "" {
		errs.add(fmt.Errorf("app spec: app.static_dir %q %s", bp.App.StaticDir, why))
	}
	// The admin battery mounts every route under admin.path the same way:
	// "/" trims to an empty prefix and panics its Init at boot.
	if p := bp.App.Admin.Path; p != "" && !authBasePathRE.MatchString(p) {
		errs.add(fmt.Errorf("app spec: app.admin.path %q must be an absolute path such as /admin: segments of letters, digits, '-', '_' and '.', none starting with a dot, and no trailing slash; / is refused because the back office needs a prefix of its own", p))
	}
	// The sitemap origin: the generated app's always-on strict mode
	// panics at boot on a base_url it cannot use.
	if why := uihost.InvalidSitemapBaseURL(bp.App.BaseURL); bp.App.BaseURL != "" && why != "" {
		errs.add(fmt.Errorf("app spec: app.base_url %q must be a bare origin, with no path, query or fragment: write an origin such as https://demo.example (the generated app uses it for sitemap URLs)", bp.App.BaseURL))
	}
	// A multi-tenant entity needs a request-time tenant resolver, and the
	// right one is host-specific (subdomain, JWT claim, user's org). The
	// generator can't emit it, and ApplyTenantScope is fail-closed, so a
	// generated multi_tenant app with no resolver reads empty and stamps an
	// empty tenant on writes: silently broken while looking secure. Refuse
	// to generate rather than ship that.
	for _, decl := range bp.Entities {
		if EntityDeclarationScope(decl).MultiTenant {
			errs.add(fmt.Errorf("app spec: entity %q sets multi_tenant: true, but the generator cannot emit a tenant resolver (the strategy, subdomain, JWT claim, user's org, is app-specific). A generated app with none reads empty and stamps an empty tenant on every write. Wire tenant.TenantMiddleware + SetTenantID in your own main (see `gofastr docs multi-tenant`) and drop multi_tenant from the app spec, or use owner_field for per-user scoping", decl.Name))
		}
	}
	// One screen cannot list the same entity twice: both lists would share
	// the entity's one query-param namespace, so sorting either table
	// rewrites both from the first block's columns. Two lists of DIFFERENT
	// entities on one screen are fine; each carries its own key.
	for _, s := range bp.Screens {
		seen := map[string]bool{}
		var walk func(blocks []AppSpecBlock)
		walk = func(blocks []AppSpecBlock) {
			for _, b := range blocks {
				if IsEntityListBlock(b) {
					e := strings.Trim(b.Entity, "/")
					if seen[e] {
						errs.add(fmt.Errorf("app spec: screen %q lists entity %q more than once: two lists of one entity share its query params, so sorting either rewrites both; split them across screens", s.Name, e))
					}
					seen[e] = true
				}
				walk(b.Children)
			}
		}
		walk(s.Body)
	}
	// Every create-flagged list's New opens <home list route>/create, the
	// one create screen AppSpecSynthesizeCRUDScreens adds per entity. Two
	// entities whose home list is the same screen would share that route:
	// the second create screen was dropped and its New opened the first
	// entity's form, so a team was saved as a player.
	createFor := map[string]string{}
	for _, s := range bp.Screens {
		for _, b := range AppSpecBlocksDeep(s.Body) {
			e := strings.Trim(b.Entity, "/")
			if !IsEntityListBlock(b) || !b.Create || e == "" {
				continue
			}
			home := AppSpecEntityListScreen(bp, e)
			if home == nil {
				continue
			}
			route := strings.TrimRight(home.Route, "/") + "/create"
			if other, ok := createFor[route]; ok && other != e {
				errs.add(fmt.Errorf("app spec: entities %q and %q both create from lists on screen %q, so both create screens would sit at %s and one entity's New would open the other's form; move one list to its own screen or drop its create: true", other, e, home.Name, route))
				continue
			}
			createFor[route] = e
		}
	}
	// entityui links a record at <base>/<id> with <base> the list's route,
	// so a detail screen must sit exactly at <list route>/{id} when the
	// entity also has a list screen. Anything else strands the links.
	errs.add(validateDetailRoutes(bp))
	// An explicit login_form/signup_form action must be the mounted
	// auth route, or the generated sign-in form posts to a 404.
	validateAuthFormActions(bp, &errs)
	if bp.App.PWA.Enabled {
		switch bp.App.PWA.Display {
		case "", "standalone", "fullscreen", "minimal-ui", "browser":
		default:
			errs.add(fmt.Errorf("app spec: app.pwa.display %q is not a web-app-manifest display mode; use standalone, fullscreen, minimal-ui, or browser (or omit it for standalone)", bp.App.PWA.Display))
		}
		if u := bp.App.PWA.StartURL; u != "" && !strings.HasPrefix(u, "/") {
			errs.add(fmt.Errorf("app spec: app.pwa.start_url %q must be a root-relative path (start with /)", u))
		}
		if s := bp.App.PWA.Scope; s != "" && !strings.HasPrefix(s, "/") {
			errs.add(fmt.Errorf("app spec: app.pwa.scope %q must be a root-relative path (start with /)", s))
		}
	}
	// Theme VALUES need the same boundary as theme keys. The emitter writes
	// them as direct struct assignments (theme.Colors.<T>.Value = …,
	// theme.DarkColors = map[string]string{…}), which bypass every
	// core-ui/style setter, so ApplyTokens' grammar never runs on them, and
	// the value reaches `--color-<token>: <value>;` on the stylesheet the UI
	// host (/app.css) and the admin battery serve. A `;` or `}` there closes
	// :root and appends rules of the author's choosing; `url(` is an outbound
	// fetch on every page load. style.ValidateColorValue is that one grammar,
	// called here rather than copied. Keys are sorted so the multi-error
	// report is deterministic (map iteration order is not).
	for _, key := range slices.Sorted(maps.Keys(bp.App.Theme)) {
		value := bp.App.Theme[key]
		if _, ok := AppSpecThemeColorPath(key); ok {
			if err := style.ValidateColorValue(value); err != nil {
				errs.add(fmt.Errorf("app spec: app.theme %s %q: %w", key, value, err))
			}
			continue
		}
		// Font tokens are valid theme keys too. They drive the theme's
		// font-family tokens and the webfont <link>s, not colors. Their value
		// is reduced to an allow-listed family name by appSpecFontFamilyName
		// before it reaches CSS, so the color grammar does not apply.
		if key == "font_body" || key == "font_heading" || key == "font_display" {
			continue
		}
		errs.add(fmt.Errorf("app spec: app.theme has unsupported token %q", key))
	}
	for _, key := range slices.Sorted(maps.Keys(bp.App.ThemeDark)) {
		value := bp.App.ThemeDark[key]
		if _, ok := AppSpecThemeColorPath(key); !ok {
			errs.add(fmt.Errorf("app spec: app.theme.dark has unsupported color token %q", key))
			continue
		}
		if err := style.ValidateColorValue(value); err != nil {
			errs.add(fmt.Errorf("app spec: app.theme.dark %s %q: %w", key, value, err))
		}
	}
	entityNames := map[string]bool{}
	entitiesByName := map[string]framework.EntityDeclaration{}
	for entityIndex, decl := range bp.Entities {
		if decl.Name == "" {
			errs.add(fmt.Errorf("app spec: entity name is required"))
			continue
		}
		if !project.IsGoIdentifier(ToCamelCase(decl.Name)) {
			errs.add(fmt.Errorf("app spec: entity %q does not produce a valid Go identifier: the generated code would not compile; rename it to start with a letter (e.g. \"two_fa_tokens\" instead of \"2fa_tokens\")", decl.Name))
			continue
		}
		// The table name reaches two sinks that neither re-escape nor
		// re-validate it: the generated typed client emits it into Go string
		// literals, and the runtime interpolates it into DDL/queries as a
		// bare SQL identifier. An entity name is already constrained to a Go
		// identifier above; `table:` was the way around that.
		if decl.Table != "" {
			if _, err := query.SafeIdent(decl.Table); err != nil {
				errs.add(fmt.Errorf("app spec: entity %q table %q must be letters, digits and underscores (it becomes a SQL identifier and is emitted into generated Go)", decl.Name, decl.Table))
			}
		}
		if entityNames[decl.Name] {
			errs.add(fmt.Errorf("app spec: duplicate entity %q", decl.Name))
			continue
		}
		entityNames[decl.Name] = true
		entitiesByName[decl.Name] = decl
		// A field declared twice was caught only by the render pass, as
		// "generated code declares Products.Title twice
		// (entities/client/client.go and entities/client/client.go)",
		// naming a file the author never wrote instead of the spec line.
		seenField := map[string]bool{}
		for _, f := range decl.Fields {
			if seenField[f.Name] {
				errs.add(fmt.Errorf("app spec: entity %q declares field %q twice: remove or rename one", decl.Name, f.Name))
			}
			seenField[f.Name] = true
		}
		errs.add(ownerScopedKeyProblem(decl))
		// Field names and enum values are transcribed into emitted Go,
		// SQL identifiers and CSS. Nothing downstream re-escapes them,
		// so they are validated here, at the generate-time boundary,
		// where the error can name the offending field.
		// Two field names one Go struct field or one Postgres column would
		// carry (Title and title, _note and note) are refused: AutoMigrate
		// writes names unquoted, so Postgres folds them together.
		fieldOwner := map[string]string{}
		for _, f := range decl.Fields {
			if !project.IsGoIdentifier(ToCamelCase(f.Name)) {
				errs.add(fmt.Errorf("app spec: entity %q field %q does not produce a valid Go identifier: rename it to letters, digits and underscores starting with a letter", decl.Name, f.Name))
			} else {
				for _, key := range []string{"go:" + ToCamelCase(f.Name), "sql:" + asciicase.Lower(f.Name)} {
					if prev, dup := fieldOwner[key]; dup && prev != f.Name {
						errs.add(fmt.Errorf("app spec: entity %q field %q collides with field %q: both become one Go struct field or one Postgres column; rename one", decl.Name, f.Name, prev))
						break
					}
					fieldOwner[key] = f.Name
				}
			}
			for _, v := range f.Values {
				if strings.ContainsAny(v, "`\"\\\n\r") {
					errs.add(fmt.Errorf("app spec: entity %q field %q enum value %q contains a quote, backtick, backslash or newline: these break the generated Go source", decl.Name, f.Name, v))
				}
				// The value is served verbatim in llm.md field notes and
				// the OpenAPI enum array; a C1 control or invisible/bidi
				// codepoint there forges or hides text for the agent/human
				// reviewing it (2026-09-05 red-probe round). The generated
				// Go itself is inert (%q escapes the rune) — the served
				// docs are the sink.
				if strings.ContainsFunc(v, textsafe.IsC1) || textsafe.ContainsInvisible(v) {
					errs.add(fmt.Errorf("app spec: entity %q field %q enum value %q contains a C1 control or invisible character (bidi overrides, zero-width, BOM, U+0080–U+009F): it would reach llm.md and the OpenAPI enum verbatim", decl.Name, f.Name, v))
				}
			}
		}
		cfg, err := decl.Config()
		if err != nil {
			errs.add(fmt.Errorf("app spec: entity %q: %w", decl.Name, err))
			continue
		}
		if err := framework.ValidateExposure(decl.Name, cfg); err != nil {
			errs.add(fmt.Errorf("app spec: entities[%d].exposure: %w", entityIndex, err))
		}
		// The display: block names fields, facets, views and form slots
		// registration checks against the entity; run that check here.
		if decl.Display != nil {
			if err := framework.ValidateDisplay(decl.Name, cfg); err != nil {
				errs.add(fmt.Errorf("app spec: %w", err))
			}
		}
		// A field default the field's own validator refuses (an enum
		// value not in values:, "notanumber" on an int, "" on a required
		// field) fails registration, which panicked the generated app at
		// boot. Run that one check here, from framework/entity.
		if slices.ContainsFunc(decl.Fields, func(f framework.FieldDeclaration) bool { return f.Default != nil }) {
			if err := fwentity.ValidateDefaults(decl.Name, cfg); err != nil {
				errs.add(fmt.Errorf("app spec: %w", err))
			}
		}
	}
	errs.add(validateAppSpecSQLNames(bp))
	for _, decl := range bp.Entities {
		// Relation-TYPED FIELDS (`type: relation, to: X`) become BelongsTo
		// relations at runtime. Catch a dangling target here so the failure
		// is a generate-time error, not an auto-migrate crash in the built
		// app ("entity has BelongsTo to unknown entity").
		for _, field := range decl.Fields {
			if !strings.EqualFold(strings.TrimSpace(field.Type), "relation") {
				continue
			}
			if strings.TrimSpace(field.To) == "" {
				errs.add(fmt.Errorf("app spec: entity %q field %q has type \"relation\" but no target: add `to: <entity>` naming a declared entity", decl.Name, field.Name))
				continue
			}
			if !entityNames[field.To] {
				errs.add(fmt.Errorf("app spec: entity %q field %q is a relation to unknown entity %q: declare an entity named %q under entities: (or fix the field's to: value)", decl.Name, field.Name, field.To, field.To))
			}
		}
		// Both sides of a rename become column names in emitted
		// ALTER TABLE … RENAME COLUMN DDL, so they get the same identifier
		// guard as table: an app spec is agent-transcribed text, not
		// developer-authored SQL. A rename whose target is not a declared
		// field would never fire, so catch that here rather than at migrate
		// time.
		declaredFields := map[string]bool{}
		for _, field := range decl.Fields {
			declaredFields[field.Name] = true
		}
		for _, old := range slices.Sorted(maps.Keys(decl.Renames)) {
			next := decl.Renames[old]
			if _, err := query.SafeIdent(old); err != nil {
				errs.add(fmt.Errorf("app spec: entity %q rename key %q is not a safe column name: %w", decl.Name, old, err))
			}
			if _, err := query.SafeIdent(next); err != nil {
				errs.add(fmt.Errorf("app spec: entity %q rename target %q is not a safe column name: %w", decl.Name, next, err))
			}
			if old == next {
				errs.add(fmt.Errorf("app spec: entity %q renames %q to itself: drop the entry", decl.Name, old))
			}
			if !declaredFields[next] {
				errs.add(fmt.Errorf("app spec: entity %q renames %q to %q, but %q is not a declared field: a rename only fires when the new name is declared on the entity", decl.Name, old, next, next))
			}
			if declaredFields[old] {
				errs.add(fmt.Errorf("app spec: entity %q renames %q to %q while still declaring %q as a field: the diff would see both columns; remove the old field", decl.Name, old, next, old))
			}
		}
		// An index is CREATE INDEX over the entity's own columns. A column
		// the entity does not have passed the load and failed the first
		// `migrate up` and the generated app's boot with "no such column",
		// and an index with no column is no statement at all.
		for i, idx := range decl.Indices {
			label := idx.Name
			if label == "" {
				label = fmt.Sprintf("indices[%d]", i)
			}
			if len(idx.Columns) == 0 {
				errs.add(fmt.Errorf("app spec: entity %q index %q names no columns: list at least one under columns:", decl.Name, label))
			}
			seenCol := map[string]bool{}
			for _, col := range idx.Columns {
				if seenCol[col] {
					errs.add(fmt.Errorf("app spec: entity %q index %q lists column %q twice", decl.Name, label, col))
					continue
				}
				seenCol[col] = true
				if !appSpecIndexableColumn(decl, col) {
					errs.add(fmt.Errorf("app spec: entity %q index %q names column %q, which the entity does not have: an index covers declared fields and the columns the framework adds (created_at, updated_at, deleted_at, the tenant column)", decl.Name, label, col))
				}
			}
		}
		// A relation name is an identifier, not a label: the entity emitter
		// puts toCamelCase(name) in struct-field position AND inside the
		// backtick `json:"…"` tag (a raw literal, which has no escape
		// mechanism at all), and the typed-column emitter puts it in
		// const-identifier position. It was the last name in the IR with no
		// identifier guard. Every sibling above (entity, field, screen,
		// endpoint handler, middleware, plugin, helper) already has one.
		relNames := map[string]string{}
		for _, field := range decl.Fields {
			relNames[ToCamelCase(field.Name)] = "field " + field.Name
		}
		for _, rel := range decl.Relations {
			if rel.Name == "" {
				errs.add(fmt.Errorf("app spec: entity %q has a relation with no name: add `name: <name>`; it becomes a struct field and an include name in the generated Go", decl.Name))
				continue
			}
			camel := ToCamelCase(rel.Name)
			if !project.IsGoIdentifier(camel) {
				errs.add(fmt.Errorf("app spec: entity %q relation %q does not produce a valid Go identifier: it is emitted as a struct field name and an include constant, so the generated code would not compile; use letters, digits and underscores (e.g. \"author\" or \"line_items\")", decl.Name, rel.Name))
				continue
			}
			if owner, dup := relNames[camel]; dup {
				errs.add(fmt.Errorf("app spec: entity %q relation %q collides with %s: both become the Go struct field %s, which would not compile; rename one", decl.Name, rel.Name, owner, camel))
				continue
			}
			relNames[camel] = "relation " + rel.Name
			if rel.Entity == "" {
				errs.add(fmt.Errorf("app spec: entity %q relation %q target entity is required: add `entity: <name>` referencing a declared entity", decl.Name, rel.Name))
				continue
			}
			if !entityNames[rel.Entity] {
				errs.add(fmt.Errorf("app spec: entity %q relation %q targets unknown entity %q: declare an entity named %q under entities: (or fix the relation's entity: value)", decl.Name, rel.Name, rel.Entity, rel.Entity))
			} else if target, ok := entitiesByName[rel.Entity]; ok {
				errs.add(appSpecRelationKeys(decl, rel, target))
			}
		}
	}
	// Seed values are typed inserts the generated app runs through the
	// CRUD validators at boot. Check them here. See validateAppSpecSeedTypes.
	errs.add(validateAppSpecSeedTypes(bp, entitiesByName))
	errs.add(ValidateAppSpecSeedStructure(bp, entitiesByName))
	routes := map[string]bool{}
	routeShapes := map[string]string{} // routeShape → the first route with it
	screenFiles := map[string]string{} // file name → what writes it
	screenTypes := map[string]string{} // generated type name → screen name
	routesOf := map[string]string{}    // screen name → its route, for the error
	// Two writers of one generated file emit it twice; the second write
	// silently replaces the first and its routes never mount. A
	// standalone screen writes screen_<name>.go, and an entity's screens
	// share screen_<entity>_crud.go, so a screen named "Posts Crud"
	// collides with the posts screens.
	claimFile := func(screen AppSpecScreen) {
		file, owner := ScreenFileName(screen.Name), fmt.Sprintf("screen %q", screen.Name)
		if e, ok := ScreenEntityRef(screen); ok {
			file, owner = ScreenFileName(e+"_crud"), fmt.Sprintf("the %s entity's screens", e)
		}
		if file == "" {
			return
		}
		if prev, dup := screenFiles[file]; dup && prev != owner {
			errs.add(fmt.Errorf("app spec: %s and %s both write the screen file %s: the second write would replace the first and its routes would never mount; rename the screen", prev, owner, file))
		} else {
			screenFiles[file] = owner
		}
	}
	for _, screen := range bp.Screens {
		if screen.Name == "" {
			errs.add(fmt.Errorf("app spec: screen name is required"))
			continue
		}
		if !project.IsGoIdentifier(ToCamelCase(screen.Name)) {
			errs.add(fmt.Errorf("app spec: screen %q does not produce a valid Go identifier", screen.Name))
			continue
		}
		if screen.Route == "" {
			errs.add(fmt.Errorf("app spec: screen %q route is required", screen.Name))
			continue
		}
		if why := screenRouteProblem(screen.Route); why != "" {
			errs.add(fmt.Errorf("app spec: screen %q route %q %s", screen.Name, screen.Route, why))
			continue
		}
		if mount := screenRouteOnEntityAPI(bp, screen.Route); mount != "" {
			errs.add(fmt.Errorf("app spec: screen %q route %q is at or under %s, where an entity's CRUD API mounts: the app would refuse to boot or its API would answer instead of the screen; move the screen", screen.Name, screen.Route, mount))
			continue
		}
		if owner := screenRouteOnFrameworkRoute(bp, screen.Route); owner != "" {
			errs.add(fmt.Errorf("app spec: screen %q route %q is at or under %s, a route the framework serves itself: the screen would never render, or would take the route over; move the screen", screen.Name, screen.Route, owner))
			continue
		}
		if app := bp.RouteApp(); app.Auth.Enabled {
			base := app.Auth.BasePath
			if base == "" {
				base = "/auth"
			}
			if strings.TrimRight(screen.Route, "/") == base+"/me" {
				errs.add(fmt.Errorf("app spec: screen %q route %q is the auth battery's GET %s/me route; move the screen", screen.Name, screen.Route, base))
				continue
			}
		}
		if routes[screen.Route] {
			errs.add(fmt.Errorf("app spec: duplicate screen route %q", screen.Route))
			continue
		}
		routes[screen.Route] = true
		// The router tries dynamic screens in registration order, so
		// /posts/{id} and /posts/{slug} (or /posts/:id) match the same
		// URLs and the second never renders.
		shape := RouteShape(screen.Route)
		if prev, dup := routeShapes[shape]; dup {
			errs.add(fmt.Errorf("app spec: screen routes %q and %q differ only in parameter names, so the router always matches the first and screen %q never renders; give one a literal segment or remove it", prev, screen.Route, screen.Name))
			continue
		}
		routeShapes[shape] = screen.Route
		claimFile(screen)
		// Every screen declares <Name>Screen and mount<Name>Screen in one
		// package, whichever file it lands in, so the name is the app's.
		typeName := ToCamelCase(screen.Name) + "Screen"
		if prev, dup := screenTypes[typeName]; dup {
			errs.add(fmt.Errorf("app spec: screens %q (%s) and %q (%s) both generate the type %s, so the app would not build; rename one", prev, routesOf[prev], screen.Name, screen.Route, typeName))
		} else {
			screenTypes[typeName] = screen.Name
			routesOf[screen.Name] = screen.Route
		}
		if _, err := ScreenTypeConst(screen.Type); err != nil {
			errs.add(err)
		}
		// appSpecScreenLayoutExpr maps ANYTHING that is not "marketing" to
		// appLayout, so a typo ("markting") silently renders a public marketing
		// page inside the authenticated app shell; sidebar, account controls and
		// all. Gating is `access`, not `layout`, so this is a wrong-chrome bug
		// rather than a gate bypass; it is still a value the author believed they
		// set and did not. Fail on it instead of guessing.
		switch screen.Layout {
		case "", "app", "marketing":
			// recognized
		default:
			errs.add(fmt.Errorf("app spec: screen %q layout %q is not a layout; use \"app\", \"marketing\", or omit it", screen.Name, screen.Layout))
		}
		for _, block := range screen.Body {
			errs.add(validateAppSpecBlock(screen.Name, entitiesByName, block))
		}
		for _, block := range AppSpecBlocksDeep(screen.Body) {
			errs.add(validateCreateFormCoversRequired(screen.Name, entitiesByName, bp.Hooks, block))
		}
		// entity_detail renders .Detail(ctx, s.id) and the list's "View" link is
		// BasePath+"/"+id; entity_form mode:edit posts to {entity}/{id}. Both
		// need the record id from a route param. A route with no {id} produces
		// an empty render and a dead link that matches no registered route.
		if detail, edit := ScreenNeedsRouteID(screen); detail || edit {
			if len(RouteParamNames(screen.Route)) == 0 {
				what := "entity_detail"
				if !detail {
					what = "entity_form (mode: edit)"
				}
				errs.add(fmt.Errorf("app spec: screen %q has an %s block but its route %q has no {id} parameter: add {id} (e.g. %s/{id}) so the record id is in the URL", screen.Name, what, screen.Route, strings.TrimRight(screen.Route, "/")))
			}
		}
		// A composite-key record screen mounts its record param as one
		// segment per key column, named after the columns: another param
		// of the same name would be read for the key.
		if keys := ScreenRecordKeys(bp, screen); keys != nil {
			param := ScreenParamName(screen)
			for _, n := range RouteParamNames(screen.Route) {
				if n != param && slices.Contains(keys, n) {
					errs.add(fmt.Errorf("app spec: screen %q route %q has a {%s} parameter, but %s is a key column of the record it shows and {%s} expands to one segment per key column: rename the parameter", screen.Name, screen.Route, n, n, param))
				}
			}
		}
		errs.add(validateAppSpecActions(screen.Name, screen.Body))
	}
	// The render adds a create screen per writable list; its file and its
	// type join the same package as the authored ones.
	for _, screen := range AppSpecSynthesizeCRUDScreens(bp).Screens[len(bp.Screens):] {
		claimFile(screen)
		typeName := ToCamelCase(screen.Name) + "Screen"
		if prev, dup := screenTypes[typeName]; dup {
			errs.add(fmt.Errorf("app spec: screen %q (%s) and the create screen a writable %s list adds at %s both generate the type %s, so the app would not build; rename the screen", prev, routesOf[prev], screen.Body[0].Entity, screen.Route, typeName))
		}
	}
	// The render adds a create screen only where no authored screen sits,
	// so an authored screen at <list>/create replaced the create form:
	// the list's New opened a page with no form, and the generated e2e
	// stayed green because it creates through the API.
	for _, screen := range bp.Screens {
		if e, ok := createFor[strings.TrimRight(screen.Route, "/")]; ok {
			errs.add(fmt.Errorf("app spec: screen %q route %q is where the %s list's create: true adds the create screen, so the authored screen would replace the create form and New would open a page without one; move the screen, or drop the list's create: true to keep your own", screen.Name, screen.Route, e))
		}
	}
	errs.add(ValidateAppSpecEndpoints(bp))
	// Hooks are the sibling construct of endpoints: a surface the generated
	// app must implement in owned Go (the handler stub) and the registry
	// wiring. They get the same boundary checks endpoints get: a handler
	// string that lands in identifier position must derive an identifier,
	// and a hook whose entity or lifecycle point is wrong can never fire —
	// the author believes a validation is enforced and no such hook exists.
	// The kiln live preview already rejects these shapes (applyHooks:
	// "missing entity" / mapHookType), so the generator refusing them keeps
	// graduation from preview to app honest.
	hookHandlers := map[string]bool{}
	for _, hook := range bp.Hooks {
		if hook.Entity == "" {
			errs.add(fmt.Errorf("app spec: hook %q entity is required: name the entity whose lifecycle the hook runs on (it becomes the HookRegistry the generated app registers the handler on)", hook.ID))
			continue
		}
		if !entityNames[hook.Entity] {
			errs.add(fmt.Errorf("app spec: hook %q targets unknown entity %q: declare an entity named %q under entities: (or fix the hook's entity: value)", hook.ID, hook.Entity, hook.Entity))
			continue
		}
		if _, ok := AppSpecHookTypes[hook.When]; !ok {
			errs.add(fmt.Errorf("app spec: hook %q when %q is not a lifecycle point; use one of %s", hook.ID, hook.When, strings.Join(slices.Sorted(maps.Keys(AppSpecHookTypes)), ", ")))
			continue
		}
		handler := strings.TrimSpace(hook.Handler)
		if handler == "" {
			errs.add(fmt.Errorf("app spec: hook %q handler is required: name the owned-Go func the generated app calls (a stub is emitted for you to implement)", hook.ID))
			continue
		}
		if !project.IsGoIdentifier(handler) {
			errs.add(fmt.Errorf("app spec: hook %q handler %q does not produce a valid Go identifier: it is emitted as the stub func the app registers, so the generated code would not compile; use letters, digits and underscores starting with a letter", hook.ID, hook.Handler))
			continue
		}
		if hookHandlers[handler] {
			errs.add(fmt.Errorf("app spec: duplicate hook handler %q: two hooks would emit the same stub func, which would not compile", hook.Handler))
			continue
		}
		hookHandlers[handler] = true
	}
	// A nav item's icon is rendered as raw HTML inside the sidebar's icon
	// slot (ui.SidebarItem.Icon is render.HTML, emitted unescaped), so an
	// icon carrying markup is not styling, it is an injection into every
	// page the shell renders. Icons are names or emoji; anything with
	// markup characters is refused rather than sanitized.
	var validateNavIcons func(items []AppSpecNavItem)
	validateNavIcons = func(items []AppSpecNavItem) {
		for _, item := range items {
			if item.Icon != "" && !AppSpecNavIconSafe(item.Icon) {
				errs.add(fmt.Errorf("app spec: nav item %q icon %q contains markup or control characters: the icon renders as raw HTML in the sidebar icon slot, so only plain text or an emoji is allowed (write markup in your own sidebar code)", item.Label, item.Icon))
			}
			validateNavIcons(item.Items)
		}
	}
	validateNavIcons(bp.Nav)
	for _, group := range []struct {
		label string
		items []AppSpecNamedStub
	}{
		{"middleware", bp.Middleware},
		{"plugins", bp.Plugins},
		{"helpers", bp.Helpers},
	} {
		names := map[string]bool{}
		for _, item := range group.items {
			if item.Name == "" {
				errs.add(fmt.Errorf("app spec: %s name is required", group.label))
				continue
			}
			if !project.IsGoIdentifier(ToCamelCase(item.Name)) {
				errs.add(fmt.Errorf("app spec: %s %q does not produce a valid Go identifier", group.label, item.Name))
				continue
			}
			if names[item.Name] {
				errs.add(fmt.Errorf("app spec: duplicate %s %q", group.label, item.Name))
				continue
			}
			names[item.Name] = true
		}
	}
	return errs.err()
}

// AppSpecHookTypes maps the app spec/kiln `when` spellings to the
// framework lifecycle constants the generated app registers. It is the
// closed set mapHookType (kiln/render) accepts; the preview and the
// generator must agree or a hook that fires in the preview fires nowhere
// in the shipped app.
var AppSpecHookTypes = map[string]string{
	"before_create": "framework.BeforeCreate",
	"after_create":  "framework.AfterCreate",
	"before_update": "framework.BeforeUpdate",
	"after_update":  "framework.AfterUpdate",
	"before_delete": "framework.BeforeDelete",
	"after_delete":  "framework.AfterDelete",
	"before_list":   "framework.BeforeList",
	"after_list":    "framework.AfterList",
}

// AppSpecNavIconSafe reports whether a nav icon can render in the
// sidebar's raw-HTML icon slot without carrying markup with it.
func AppSpecNavIconSafe(icon string) bool {
	for _, r := range icon {
		switch {
		case r < 0x20 || r == 0x7f:
			return false
		case r == '<' || r == '>' || r == '&' || r == '"' || r == '\'' || r == '`' || r == '\\':
			return false
		}
	}
	return true
}
