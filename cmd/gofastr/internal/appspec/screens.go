package appspec

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
)

// AppSpecSynthesizeCRUDScreens appends the create screen that makes an
// app-side list writable: <home list>/create for every entity with an
// entity_list flagged `create: true`, where the home list is the one every
// list's New links to (appSpecEntityListScreen). The synthesized screen
// renders appUI.Create(entity), it inherits the home screen's layout +
// access, and it is not added to nav.
// The record page holds the edit form, so no edit screen is synthesized.
//
// It also appends the sign-in screen an app spec with auth on and no
// login_form needs (AppSpecLoginScreen): the gates, the Sign in button
// and the battery's failed-login redirect all point at /login.
func AppSpecSynthesizeCRUDScreens(bp AppSpec) AppSpec {
	existing := map[string]bool{}
	for _, s := range bp.Screens {
		existing[s.Route] = true
	}
	var extra []AppSpecScreen
	add := func(s AppSpecScreen) {
		if existing[s.Route] {
			return
		}
		existing[s.Route] = true
		extra = append(extra, s)
	}
	for _, s := range bp.Screens {
		for _, b := range AppSpecBlocksDeep(s.Body) {
			e := strings.Trim(b.Entity, "/")
			if e == "" {
				continue
			}
			if IsEntityListBlock(b) && b.Create {
				home := AppSpecEntityListScreen(bp, e)
				singular := singularize(ToDisplayName(e))
				add(AppSpecScreen{
					Name:   e + "_create",
					Route:  strings.TrimRight(home.Route, "/") + "/create",
					Layout: home.Layout,
					Access: home.Access,
					Title:  "New " + singular,
					Body:   []AppSpecBlock{{Kind: "entity_create", Entity: e}},
				})
			}
		}
	}
	if s, ok := AppSpecLoginScreen(bp); ok {
		add(s)
	}
	// Clip: an append into spare capacity would write the synthesized
	// screens into the caller's array.
	bp.Screens = append(slices.Clip(bp.Screens), extra...)
	return bp
}

// AppSpecLoginScreen is the /login screen synthesized for an app spec with
// auth on and no screen hosting a login_form. Without it every gated
// screen, the Sign in button and a failed form login sent the visitor to
// a /login nobody registered (ecommerce). The body is the login_form
// block an author would write, so the screen composes ui.AuthCard and
// ui.Form like an authored one and ships no CSS. It takes the marketing
// layout when the app spec has one (where an authored sign-in screen
// sits), links a signup_form screen when there is one, and is not added
// to nav. A screen whose name makes the login file or type, or one
// already routed /login, keeps its place: nothing is synthesized.
func AppSpecLoginScreen(bp AppSpec) (AppSpecScreen, bool) {
	if !bp.App.Auth.Enabled {
		return AppSpecScreen{}, false
	}
	layout, signup := "", ""
	for _, s := range bp.Screens {
		if ToSnakeCase(s.Name) == "login" || ToCamelCase(s.Name) == "Login" || s.Route == "/login" {
			return AppSpecScreen{}, false
		}
		if s.Layout == "marketing" {
			layout = "marketing"
		}
		for _, b := range AppSpecBlocksDeep(s.Body) {
			if AppSpecBlockKindIs(b, "login_form") {
				return AppSpecScreen{}, false
			}
			if AppSpecBlockKindIs(b, "signup_form") && signup == "" && !strings.Contains(s.Route, "{") {
				signup = s.Route
			}
		}
	}
	// The block spells out the defaults the renderer would fill in, the
	// way gofastr pack reads an AuthCard back, so pack recognizes this
	// screen as the synthesized one (IsSynthesizedLoginScreen) and drops it.
	authBase := bp.App.Auth.BasePath
	if authBase == "" {
		authBase = "/auth"
	}
	block := AppSpecBlock{Kind: "login_form", Text: "Sign in", Props: map[string]any{"action": authBase + "/login", "next": "/"}}
	if signup != "" {
		block.Props["register_href"] = signup
	}
	return AppSpecScreen{
		Name:   "login",
		Route:  "/login",
		Layout: layout,
		Title:  "Sign in",
		Body:   []AppSpecBlock{block},
	}, true
}

// IsSynthesizedLoginScreen reports whether screen, read back from a
// generated app, is exactly the sign-in screen AppSpecLoginScreen adds to
// bp without it. gofastr pack drops it, as it drops a synthesized create
// screen: the regenerate adds it again in the same place.
func IsSynthesizedLoginScreen(bp AppSpec, screen AppSpecScreen) bool {
	if screen.Route != "/login" {
		return false
	}
	rest := bp
	rest.Screens = slices.DeleteFunc(slices.Clone(bp.Screens), func(s AppSpecScreen) bool { return s.Route == "/login" })
	synth, ok := AppSpecLoginScreen(rest)
	return ok && reflect.DeepEqual(synth, screen)
}

// ScreenFileName maps a screen (or entity-crud) base name to its generated
// filename. The screen_ prefix avoids the fixed package files (main.go,
// app.go, stubs.go, resource.go, e2e_test.go, screens_register.go,
// screen_shared.go) by construction; the guard below additionally prefixes a
// name that would otherwise collide with the seam or shared helper.
func ScreenFileName(base string) string {
	snake := ToSnakeCase(base)
	name := "screen_" + snake + ".go"
	// Guard the fixed helper files. A screen named "shared" would shadow
	// screen_shared.go (the nodeComponent adapter); re-prefix it. ("register"
	// cannot collide: the seam is screens_register.go, a different prefix.)
	if name == "screen_shared.go" {
		return "screen_screen_shared.go"
	}
	return name
}

// ScreenEntityRef returns the entity a CRUD screen renders (its
// entity_list/entity_detail/entity_create/entity_edit block target) and
// whether the screen is a CRUD screen at all. Non-CRUD authored screens
// (marketing, dashboards, auth forms) return ("", false) and land in their
// own screen_<snake>.go.
func ScreenEntityRef(s AppSpecScreen) (entity string, isCrud bool) {
	for _, b := range AppSpecBlocksDeep(s.Body) {
		e := strings.Trim(b.Entity, "/")
		if e == "" {
			continue
		}
		if IsEntityListBlock(b) || IsEntityDetailBlock(b) || IsEntityCreateBlock(b) {
			return e, true
		}
	}
	return "", false
}

// AppSpecEntityListScreen returns the entity's home list screen, or nil
// when no screen lists it. Its record links, the synthesized create screen
// and the detail screen's drawer all hang off it, and a list on any other
// screen (a dashboard's recent rows) links there too. The home is the list
// screen the detail screen sits under (<route>/{id}), else the first list
// screen with create: true, else the first list screen, in declaration
// order: a dashboard declared before the entity's own screen never wins.
func AppSpecEntityListScreen(bp AppSpec, entity string) *AppSpecScreen {
	var lists []int
	for i := range bp.Screens {
		if found, _ := entityListOn(bp.Screens[i].Body, entity); found {
			lists = append(lists, i)
		}
	}
	if len(lists) == 0 {
		return nil
	}
	if base, ok := appSpecDetailBase(bp, entity); ok {
		for _, i := range lists {
			if strings.TrimRight(bp.Screens[i].Route, "/") == base {
				return &bp.Screens[i]
			}
		}
	}
	for _, i := range lists {
		if _, create := entityListOn(bp.Screens[i].Body, entity); create {
			return &bp.Screens[i]
		}
	}
	return &bp.Screens[lists[0]]
}

// entityDetailsOn returns the entities blocks show an entity_detail of,
// at any nesting depth, in block order.
func entityDetailsOn(blocks []AppSpecBlock) []string {
	var out []string
	for _, b := range AppSpecBlocksDeep(blocks) {
		if IsEntityDetailBlock(b) {
			out = append(out, strings.Trim(b.Entity, "/"))
		}
	}
	return out
}

// entityListOn reports whether blocks hold an entity_list of entity at
// any nesting depth, and whether one of them sets create: true.
func entityListOn(blocks []AppSpecBlock, entity string) (found, create bool) {
	for _, b := range AppSpecBlocksDeep(blocks) {
		if IsEntityListBlock(b) && strings.Trim(b.Entity, "/") == entity {
			found = true
			create = create || b.Create
		}
	}
	return found, create
}

// AppSpecHasRecordScreen reports whether some screen shows a record of
// entity (an entity_detail block). validateDetailRoutes holds that screen
// to <list route>/{id} under one of the entity's lists, the home list's
// row links point there, and without one every row link is a 404.
func AppSpecHasRecordScreen(bp AppSpec, entity string) bool {
	_, ok := appSpecDetailBase(bp, entity)
	return ok
}

// appSpecDetailBase returns the route the entity's first detail screen
// sits under: its route minus the trailing /{id}.
func appSpecDetailBase(bp AppSpec, entity string) (string, bool) {
	for _, s := range bp.Screens {
		if slices.Contains(entityDetailsOn(s.Body), entity) {
			base := strings.TrimRight(s.Route, "/")
			base = strings.TrimSuffix(base, "/{id}")
			return strings.TrimSuffix(base, "/:id"), true
		}
	}
	return "", false
}

var screenTypeConsts = map[string]string{
	"page":   "app.ScreenPage",
	"drawer": "app.ScreenDrawer",
	"sheet":  "app.ScreenSheet",
	"dialog": "app.ScreenDialog",
}

// ScreenTypeConst returns the generated Go constant for an app-spec screen type.
func ScreenTypeConst(value string) (string, error) {
	kind := strings.ToLower(strings.TrimSpace(value))
	switch kind {
	case "":
		kind = "page"
	case "modal":
		kind = "dialog"
	}
	if constant, ok := screenTypeConsts[kind]; ok {
		return constant, nil
	}
	return "", fmt.Errorf("app spec: unknown screen type %q", value)
}

// ScreenTypeFromConst recovers the canonical type, omitting the default page.
func ScreenTypeFromConst(constant string) string {
	for kind, name := range screenTypeConsts {
		if name == constant && kind != "page" {
			return kind
		}
	}
	return ""
}

// AppHome returns the first app-layout screen's route, or "/" if absent.
func AppHome(screens []AppSpecScreen) string {
	for _, s := range screens {
		if s.Layout == "app" {
			return s.Route
		}
	}
	return "/"
}
