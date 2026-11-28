package appspec

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
)

// AppSpecSynthesizeCRUDScreens returns bp with the create screens of its writable lists and, with auth on, its sign-in screen appended.
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
	bp.Screens = append(slices.Clip(bp.Screens), extra...)
	return bp
}

// AppSpecLoginScreen is the /login screen synthesized for bp, and false when bp needs none.
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

// IsSynthesizedLoginScreen reports whether screen is exactly the sign-in screen AppSpecLoginScreen adds to bp.
func IsSynthesizedLoginScreen(bp AppSpec, screen AppSpecScreen) bool {
	if screen.Route != "/login" {
		return false
	}
	rest := bp
	rest.Screens = slices.DeleteFunc(slices.Clone(bp.Screens), func(s AppSpecScreen) bool { return s.Route == "/login" })
	synth, ok := AppSpecLoginScreen(rest)
	return ok && reflect.DeepEqual(synth, screen)
}

// ScreenFileName is the generated file of a screen or entity-crud base name.
func ScreenFileName(base string) string {
	snake := ToSnakeCase(base)
	name := "screen_" + snake + ".go"
	if name == "screen_shared.go" {
		return "screen_screen_shared.go"
	}
	return name
}

// ScreenEntityRef returns the entity a CRUD screen's list, detail or create block renders, and false for any other screen.
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

// AppSpecEntityListScreen returns the entity's home list screen, or nil when no screen lists it.
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

func entityDetailsOn(blocks []AppSpecBlock) []string {
	var out []string
	for _, b := range AppSpecBlocksDeep(blocks) {
		if IsEntityDetailBlock(b) {
			out = append(out, strings.Trim(b.Entity, "/"))
		}
	}
	return out
}

func entityListOn(blocks []AppSpecBlock, entity string) (found, create bool) {
	for _, b := range AppSpecBlocksDeep(blocks) {
		if IsEntityListBlock(b) && strings.Trim(b.Entity, "/") == entity {
			found = true
			create = create || b.Create
		}
	}
	return found, create
}

// AppSpecHasRecordScreen reports whether some screen shows an entity_detail of entity.
func AppSpecHasRecordScreen(bp AppSpec, entity string) bool {
	_, ok := appSpecDetailBase(bp, entity)
	return ok
}

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
