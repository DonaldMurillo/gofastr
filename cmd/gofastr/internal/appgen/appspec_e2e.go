package appgen

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// appSpecE2EScreenRoutes splits the app spec's STATIC screen routes (those
// without a `{param}`) into public (anonymous-renderable) and gated (auth) sets.
// Dynamic routes are exercised by the CRUD lifecycle, which has a real id.
func appSpecE2EScreenRoutes(bp appspec.AppSpec) (public, gated []string) {
	for _, s := range bp.Screens {
		route := app.CanonicalRoutePattern(s.Route)
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

// appSpecHasHomeScreen reports whether a screen is routed at /, the
// one page the generated e2e asserts the brand on.
func appSpecHasHomeScreen(bp appspec.AppSpec) bool {
	for _, s := range bp.Screens {
		if s.Route == "/" {
			return true
		}
	}
	return false
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
			lockedRoute[app.CanonicalRoutePattern(s.Route)] = true
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
		if !ok || appSpecUnfillableRelation(decl) || appSpecUnfillableConstraints(decl) {
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

func appSpecUnfillableConstraints(decl framework.EntityDeclaration) bool {
	keyColumns := appspec.AppSpecKeyColNames(decl)
	for _, field := range decl.Fields {
		if (field.Required || slices.Contains(keyColumns, field.Name)) &&
			!appspec.AppSpecFieldSystem(field.Name) && !field.Hidden &&
			(field.Min != nil || field.Max != nil || field.Pattern != "") {
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
	// The generated e2e test boots against the app spec's declared driver,
	// so a postgres spec provisions a throwaway Postgres database (skips
	// when Postgres is unreachable so driverless CI stays green-by-skip):
	// the declared engine is the one the spec ships against, even though
	// the generated app now links both drivers and DB_DRIVER can switch.
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
	if isPostgres {
		b.WriteString("\t\"github.com/DonaldMurillo/gofastr/framework/testkit\"\n")
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
	b.WriteString("\tsrv.Env = append(os.Environ(), \"PORT=\"+addr, \"GOFASTR_SEMANTIC_COVERAGE=1\")\n")
	if isPostgres {
		b.WriteString("\tdbURL := e2ePostgresDSN(t)\n")
		b.WriteString("\tsrv.Env = append(srv.Env, \"DATABASE_URL=\"+dbURL, \"DB_DRIVER=postgres\")\n")
	} else {
		b.WriteString("\tsrv.Env = append(srv.Env, \"DATABASE_URL=file:\"+filepath.Join(dir, \"e2e.db\"))\n")
	}
	b.WriteString("\te2eCaptureServerLog(t, srv, dir)\n")
	b.WriteString("\tif err := srv.Start(); err != nil { t.Fatalf(\"start: %v\", err) }\n")
	b.WriteString("\tt.Cleanup(func() { _ = srv.Process.Kill(); _, _ = srv.Process.Wait() })\n")
	b.WriteString("\tbase := \"http://\" + addr\n")
	b.WriteString("\te2eWaitReady(t, base)\n\n")
	// The home page carries the brand, when a screen sits at /. With none
	// there, / is a 404 the app was never meant to serve, and the public
	// screens below are what renders.
	if appSpecHasHomeScreen(bp) {
		b.WriteString(fmt.Sprintf("\tif code, body := e2eDo(t, e2eClient, \"GET\", base+\"/\", \"\"); code != http.StatusOK || !strings.Contains(body, %q) {\n\t\tt.Errorf(\"home page = %%d, missing brand? %%v\", code, !strings.Contains(body, %q))\n\t}\n", appName, appName))
	}

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
		fmt.Fprintf(&b, `
func e2ePostgresDSN(t *testing.T) string {
	t.Helper()
	adminDSN := os.Getenv(%q)
	if adminDSN == "" {
		t.Skip("TEST_POSTGRES_DSN unset; skipping postgres e2e")
	}
	return testkit.NewIsolatedDBDSN(t, adminDSN)
}
`, pgtest.DSNEnv)
	}
	return b.String()
}
