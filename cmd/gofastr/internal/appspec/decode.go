package appspec

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/battery/auth"
	"github.com/DonaldMurillo/gofastr/core/settings"
	coreyaml "github.com/DonaldMurillo/gofastr/core/yaml"
	"github.com/DonaldMurillo/gofastr/framework"
)

func DecodeAppSpec(node *coreyaml.Node) (AppSpec, error) {
	m, err := expectMap(node, "app spec")
	if err != nil {
		return AppSpec{}, err
	}
	allowed := map[string]bool{"app": true, "entities": true, "screens": true, "nav": true, "seed": true, "endpoints": true, "hooks": true, "middleware": true, "plugins": true, "helpers": true}
	if err := rejectUnknownKeys(m, allowed, "app spec"); err != nil {
		return AppSpec{}, err
	}
	var bp AppSpec
	if child := m["app"]; child != nil {
		app, err := decodeAppSpecApp(child)
		if err != nil {
			return AppSpec{}, err
		}
		bp.App = app
	} else {
		bp.appless = true
	}
	if child := m["entities"]; child != nil {
		entities, err := decodeAppSpecEntities(child)
		if err != nil {
			return AppSpec{}, err
		}
		bp.Entities = entities
	}
	if child := m["screens"]; child != nil {
		screens, err := decodeAppSpecScreens(child)
		if err != nil {
			return AppSpec{}, err
		}
		bp.Screens = screens
	}
	if child := m["nav"]; child != nil {
		nav, err := decodeAppSpecNav(child)
		if err != nil {
			return AppSpec{}, err
		}
		bp.Nav = nav
	}
	if child := m["seed"]; child != nil {
		seed, err := decodeAppSpecSeed(child)
		if err != nil {
			return AppSpec{}, err
		}
		bp.Seed = seed
	}
	if child := m["endpoints"]; child != nil {
		endpoints, err := settings.DecodeEndpoints(child, "app spec: endpoints")
		if err != nil {
			return AppSpec{}, err
		}
		bp.Endpoints = endpoints
	}
	if child := m["hooks"]; child != nil {
		hooks, err := decodeAppSpecHooks(child)
		if err != nil {
			return AppSpec{}, err
		}
		bp.Hooks = append(bp.Hooks, hooks...)
	}
	if child := m["middleware"]; child != nil {
		stubs, err := decodeNamedStubs(child, "middleware")
		if err != nil {
			return AppSpec{}, err
		}
		bp.Middleware = stubs
	}
	if child := m["plugins"]; child != nil {
		stubs, err := decodeNamedStubs(child, "plugins")
		if err != nil {
			return AppSpec{}, err
		}
		bp.Plugins = stubs
	}
	if child := m["helpers"]; child != nil {
		stubs, err := decodeNamedStubs(child, "helpers")
		if err != nil {
			return AppSpec{}, err
		}
		bp.Helpers = stubs
	}
	normalizeStatFilters(&bp)
	return bp, nil
}

// normalizeStatFilters rewrites a stat_card source.filter authored in the
// legacy `column=value` spelling into the query DSL the emitted
// appUI.StatValue parses (`column = "value"`). entityui.StatValue hands
// the string to dsl.ParsePredicate, which refuses bare words; the
// app spec's filter spelling predates that engine. Both spellings carry
// the same equality, and a filter already written in the DSL (spaces,
// quotes) passes through untouched. Normalizing at decode (not at emit)
// keeps pack exact: parse(yml) and parse(pack(yml)) both see the
// normalized form, so the round-trip invariant holds without a second
// translation on the way back.
func normalizeStatFilters(bp *AppSpec) {
	var walk func(blocks []AppSpecBlock)
	walk = func(blocks []AppSpecBlock) {
		for i := range blocks {
			walk(blocks[i].Children)
			if !strings.EqualFold(strings.TrimSpace(blocks[i].Kind), "stat_card") {
				continue
			}
			if src, ok := blocks[i].Props["source"].(map[string]any); ok {
				if f, ok := src["filter"].(string); ok {
					if dsl, changed := statFilterDSL(f); changed {
						src["filter"] = dsl
					}
				}
			}
		}
	}
	for i := range bp.Screens {
		walk(bp.Screens[i].Body)
	}
}

// statFilterWord matches one bare word of the legacy filter spelling:
// a column name or an enum-ish value.
var statFilterWord = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// statFilterDSL converts `column=value` to `column = "value"` when the
// filter is exactly that shape: one "=", no spaces, no quotes, bare
// words on both sides. Anything else is DSL already and returns
// unchanged.
func statFilterDSL(f string) (string, bool) {
	f = strings.TrimSpace(f)
	if f == "" || strings.ContainsAny(f, ` "`) || strings.Count(f, "=") != 1 {
		return f, false
	}
	col, val, ok := strings.Cut(f, "=")
	if !ok || !statFilterWord.MatchString(col) || !statFilterWord.MatchString(val) {
		return f, false
	}
	return col + ` = "` + val + `"`, true
}

func decodeAppSpecApp(node *coreyaml.Node) (AppSpecApp, error) {
	m, err := expectMap(node, "app")
	if err != nil {
		return AppSpecApp{}, err
	}
	allowed := map[string]bool{"name": true, "description": true, "base_url": true, "module": true, "db": true, "static_dir": true, "output_dir": true, "api_prefix": true, "public_openapi": true, "theme": true, "auth": true, "admin": true, "pwa": true, "llm_md": true, "entity_llm_md": true, "locale_prefixes": true}
	if err := rejectUnknownKeys(m, allowed, "app"); err != nil {
		return AppSpecApp{}, err
	}
	app := AppSpecApp{
		Name:        stringValue(m["name"]),
		Description: stringValue(m["description"]),
		BaseURL:     strings.TrimRight(stringValue(m["base_url"]), "/"),
		Module:      stringValue(m["module"]),
		StaticDir:   stringValue(m["static_dir"]),
		OutputDir:   stringValue(m["output_dir"]),
		// Mount entity JSON CRUD under /api by default so bare /{entity}
		// routes are free for HTML screens. Set api_prefix: "" to opt out
		// (entity APIs mount at bare /{table}, the historical behavior).
		APIPrefix: "api",
	}
	if v, ok := m["api_prefix"]; ok {
		app.APIPrefix = strings.Trim(stringValue(v), "/")
	}
	if v, ok := m["public_openapi"]; ok {
		app.PublicOpenAPI = boolValue(v)
	}
	if dbNode := m["db"]; dbNode != nil {
		db, err := expectMap(dbNode, "app.db")
		if err != nil {
			return AppSpecApp{}, err
		}
		if err := rejectUnknownKeys(db, map[string]bool{"driver": true, "url": true}, "app.db"); err != nil {
			return AppSpecApp{}, err
		}
		// driver/url answer "" for a map or list node (stringValue), and an
		// empty driver+url silently generates a DB-less app — the theme
		// decoder built requireScalarString for exactly this shape; db is the
		// sharper surface (every CRUD screen 500s at runtime, seed data
		// aborts boot), so it refuses the shape instead of defaulting it.
		driver, err := requireScalarString(db["driver"], "app.db", "driver")
		if err != nil {
			return AppSpecApp{}, err
		}
		url, err := requireScalarString(db["url"], "app.db", "url")
		if err != nil {
			return AppSpecApp{}, err
		}
		app.DBDriver = driver
		app.DBURL = url
	}
	if themeNode := m["theme"]; themeNode != nil {
		theme, dark, err := decodeappTheme(themeNode)
		if err != nil {
			return AppSpecApp{}, err
		}
		app.Theme = theme
		app.ThemeDark = dark
	}
	if authNode := m["auth"]; authNode != nil {
		auth, err := decodeAppSpecAuth(authNode)
		if err != nil {
			return AppSpecApp{}, err
		}
		app.Auth = auth
	}
	if adminNode := m["admin"]; adminNode != nil {
		admin, err := decodeAppSpecAdmin(adminNode)
		if err != nil {
			return AppSpecApp{}, err
		}
		app.Admin = admin
	}
	if pwaNode := m["pwa"]; pwaNode != nil {
		pwa, err := decodeAppSpecPWA(pwaNode)
		if err != nil {
			return AppSpecApp{}, err
		}
		app.PWA = pwa
	}
	if v, ok := m["llm_md"]; ok {
		app.LLMMD = boolValue(v)
	}
	if v, ok := m["entity_llm_md"]; ok {
		b := boolValue(v)
		app.EntityLLMMD = &b
	}
	if v, ok := m["locale_prefixes"]; ok {
		app.LocalePrefixes = boolValue(v)
	}
	return app, nil
}

func decodeAppSpecPWA(node *coreyaml.Node) (AppSpecPWA, error) {
	m, err := expectMap(node, "app.pwa")
	if err != nil {
		return AppSpecPWA{}, err
	}
	allowed := map[string]bool{"enabled": true, "name": true, "short_name": true, "description": true, "start_url": true, "scope": true, "display": true, "theme_color": true, "background_color": true}
	if err := rejectUnknownKeys(m, allowed, "app.pwa"); err != nil {
		return AppSpecPWA{}, err
	}
	return AppSpecPWA{
		Enabled:         boolValue(m["enabled"]),
		Name:            stringValue(m["name"]),
		ShortName:       stringValue(m["short_name"]),
		Description:     stringValue(m["description"]),
		StartURL:        stringValue(m["start_url"]),
		Scope:           stringValue(m["scope"]),
		Display:         stringValue(m["display"]),
		ThemeColor:      stringValue(m["theme_color"]),
		BackgroundColor: stringValue(m["background_color"]),
	}, nil
}

func decodeAppSpecAdmin(node *coreyaml.Node) (AppSpecAdmin, error) {
	m, err := expectMap(node, "app.admin")
	if err != nil {
		return AppSpecAdmin{}, err
	}
	allowed := map[string]bool{"enabled": true, "path": true, "role": true, "login_path": true, "seed_email": true, "seed_password": true}
	if err := rejectUnknownKeys(m, allowed, "app.admin"); err != nil {
		return AppSpecAdmin{}, err
	}
	// admin.role gates the admin console; requireScalarString so a
	// list/map shape is an error rather than a silent empty role.
	adminRole, err := requireScalarString(m["role"], "app.admin", "role")
	if err != nil {
		return AppSpecAdmin{}, err
	}
	// seed_email reaches the generated FindByEmail/CreateUser literals.
	// Login canonicalizes the submitted address and FindByEmail matches
	// exactly, so the seed is stored in the spelling login looks up: a
	// raw "Ops@Example.com" would mint an admin nobody can sign in as.
	// Generated apps install no AuthConfig.CanonicalizeEmail override,
	// so the package default is the one login applies.
	seedEmail, err := auth.CanonicalEmail(stringValue(m["seed_email"]))
	if err != nil {
		return AppSpecAdmin{}, fmt.Errorf("app.admin.seed_email: %w", err)
	}
	return AppSpecAdmin{
		Enabled:      boolValue(m["enabled"]),
		Path:         stringValue(m["path"]),
		Role:         adminRole,
		LoginPath:    stringValue(m["login_path"]),
		SeedEmail:    seedEmail,
		SeedPassword: stringValue(m["seed_password"]),
	}, nil
}

func decodeAppSpecAuth(node *coreyaml.Node) (AppSpecAuth, error) {
	m, err := expectMap(node, "app.auth")
	if err != nil {
		return AppSpecAuth{}, err
	}
	if err := rejectUnknownKeys(m, map[string]bool{"enabled": true, "dev_mode": true, "base_path": true, "jwt_secret": true}, "app.auth"); err != nil {
		return AppSpecAuth{}, err
	}
	// dev_mode defaults to true when omitted: a freshly generated app
	// serves plain HTTP, where the production cookie defaults
	// (__Host-session + Secure) never round-trip. Login would silently
	// break out of the box. `gofastr generate` warns loudly about the
	// default; set `dev_mode: false` (plus jwt_secret + HTTPS) to deploy.
	devMode := true
	if dm, ok := m["dev_mode"]; ok {
		// Strict bool only: dev_mode is the one app spec bool whose
		// default is true, so the usual lax "anything-but-true → false"
		// coercion would let YAML-1.1 spellings like `yes` silently flip
		// it to prod cookie mode on plain HTTP, the exact broken-login
		// scenario the default exists to prevent, while also
		// suppressing the dev-mode warning.
		v, err := strictBoolValue(dm)
		if err != nil {
			return AppSpecAuth{}, fmt.Errorf("app.auth.dev_mode %w", err)
		}
		devMode = v
	}
	// enabled is access-gating: false means the auth subsystem is OFF and the
	// whole app is public (no login, no JWT, no enforced screen policies). A
	// YAML-1.1 truthy like `enabled: yes` (a string in YAML 1.2) silently
	// reading false is fail-OPEN, so decode it strictly. See protectiveBool.
	enabled, err := protectiveBool(m, "enabled", "app.auth")
	if err != nil {
		return AppSpecAuth{}, err
	}
	return AppSpecAuth{
		Enabled:   enabled,
		DevMode:   devMode,
		BasePath:  stringValue(m["base_path"]),
		JWTSecret: stringValue(m["jwt_secret"]),
	}, nil
}

// decodeappTheme returns the light color/font tokens and, from a nested
// `dark:` map, the dark-scheme color overrides.
func decodeappTheme(node *coreyaml.Node) (light, dark map[string]string, err error) {
	m, err := expectMap(node, "app.theme")
	if err != nil {
		return nil, nil, err
	}
	light = make(map[string]string, len(m))
	for key, value := range m {
		if key == "dark" {
			darkMap, derr := expectMap(value, "app.theme.dark")
			if derr != nil {
				return nil, nil, derr
			}
			dark = make(map[string]string, len(darkMap))
			for dk, dv := range darkMap {
				if dv == nil || dv.Kind != coreyaml.Scalar {
					return nil, nil, fmt.Errorf("app.theme.dark.%s must be a scalar CSS color value", dk)
				}
				dark[dk] = stringValue(dv)
			}
			continue
		}
		if value == nil || value.Kind != coreyaml.Scalar {
			return nil, nil, fmt.Errorf("app.theme.%s must be a scalar CSS token value", key)
		}
		light[key] = stringValue(value)
	}
	return light, dark, nil
}

func decodeAppSpecScreens(node *coreyaml.Node) ([]AppSpecScreen, error) {
	list, err := expectList(node, "screens")
	if err != nil {
		return nil, err
	}
	out := make([]AppSpecScreen, 0, len(list))
	for i, item := range list {
		m, err := expectMap(item, fmt.Sprintf("screens[%d]", i))
		if err != nil {
			return nil, err
		}
		allowed := map[string]bool{"name": true, "route": true, "title": true, "description": true, "type": true, "layout": true, "access": true, "body": true}
		if err := rejectUnknownKeys(m, allowed, fmt.Sprintf("screens[%d]", i)); err != nil {
			return nil, err
		}
		screen := AppSpecScreen{
			Name:        stringValue(m["name"]),
			Route:       stringValue(m["route"]),
			Title:       stringValue(m["title"]),
			Description: stringValue(m["description"]),
			Type:        stringValue(m["type"]),
			Layout:      stringValue(m["layout"]),
		}
		if accNode := m["access"]; accNode != nil {
			accM, err := expectMap(accNode, fmt.Sprintf("screens[%d].access", i))
			if err != nil {
				return nil, err
			}
			if err := rejectUnknownKeys(accM, map[string]bool{"auth": true, "role": true}, fmt.Sprintf("screens[%d].access", i)); err != nil {
				return nil, err
			}
			// access.auth is THE protective flag: false registers the screen with
			// no policy, i.e. public. `auth: yes` used to land there silently.
			auth, err := protectiveBool(accM, "auth", fmt.Sprintf("screens[%d].access", i))
			if err != nil {
				return nil, err
			}
			// role names the gate; a map/list role decodes to "" via
			// stringValue, which leaves Auth=false and mounts the screen via
			// bare site.Register — world-readable where the author wrote an
			// access block. requireScalarString refuses the shape.
			role, err := requireScalarString(accM["role"], fmt.Sprintf("screens[%d].access", i), "role")
			if err != nil {
				return nil, err
			}
			screen.Access = AppSpecAccess{Auth: auth, Role: role}
			if screen.Access.Role != "" {
				screen.Access.Auth = true
			}
		}
		body, err := decodeBlocks(m["body"])
		if err != nil {
			return nil, err
		}
		screen.Body = body
		out = append(out, screen)
	}
	return out, nil
}

func decodeAppSpecNav(node *coreyaml.Node) ([]AppSpecNavItem, error) {
	list, err := expectList(node, "nav")
	if err != nil {
		return nil, err
	}
	return decodeNavItems(list, "nav")
}

func decodeNavItems(list []*coreyaml.Node, label string) ([]AppSpecNavItem, error) {
	out := make([]AppSpecNavItem, 0, len(list))
	for i, item := range list {
		m, err := expectMap(item, fmt.Sprintf("%s[%d]", label, i))
		if err != nil {
			return nil, err
		}
		allowed := map[string]bool{"label": true, "href": true, "icon": true, "role": true, "items": true}
		if err := rejectUnknownKeys(m, allowed, fmt.Sprintf("%s[%d]", label, i)); err != nil {
			return nil, err
		}
		navItem := AppSpecNavItem{
			Label: stringValue(m["label"]),
			Href:  stringValue(m["href"]),
			Icon:  stringValue(m["icon"]),
			Role:  stringValue(m["role"]),
		}
		if child := m["items"]; child != nil {
			subList, err := expectList(child, fmt.Sprintf("%s[%d].items", label, i))
			if err != nil {
				return nil, err
			}
			items, err := decodeNavItems(subList, fmt.Sprintf("%s[%d].items", label, i))
			if err != nil {
				return nil, err
			}
			navItem.Items = items
		}
		out = append(out, navItem)
	}
	return out, nil
}

func decodeBlocks(node *coreyaml.Node) ([]AppSpecBlock, error) {
	if node == nil {
		return nil, nil
	}
	list, err := expectList(node, "body")
	if err != nil {
		return nil, err
	}
	out := make([]AppSpecBlock, 0, len(list))
	for i, item := range list {
		m, err := expectMap(item, fmt.Sprintf("body[%d]", i))
		if err != nil {
			return nil, err
		}
		kind := strings.ToLower(strings.TrimSpace(stringValue(m["kind"])))
		if kind == "" {
			kind = strings.ToLower(strings.TrimSpace(stringValue(m["type"])))
		}
		switch kind {
		case "entity_create", "entity_edit":
			what := "entity_detail (the record page holds the edit form)"
			if kind == "entity_create" {
				what = "the list's create: (a create screen is synthesized at <list>/create)"
			}
			return nil, fmt.Errorf("app spec: body[%d]: %s is no longer a block kind; use %s", i, kind, what)
		}
		// The screen keys the entityui screens replaced are refused by
		// name, with the error naming where the setting moved, so an
		// author holding an old app spec hears the fix rather than
		// "unknown key". island:/widget: keep their meaning on node
		// blocks; on an entity screen they name a model that is gone.
		if err := refuseReplacedBlockKeys(m, kind, fmt.Sprintf("body[%d]", i)); err != nil {
			return nil, err
		}
		allowed := map[string]bool{"type": true, "kind": true, "text": true, "level": true, "class": true, "href": true, "entity": true, "fields": true, "limit": true, "empty_text": true, "mode": true, "create": true, "bulk": true, "filter": true, "props": true, "children": true, "actions": true, "island": true, "widget": true}
		if err := rejectUnknownKeys(m, allowed, fmt.Sprintf("body[%d]", i)); err != nil {
			return nil, err
		}
		children, err := decodeBlocks(m["children"])
		if err != nil {
			return nil, err
		}
		actions, err := decodeActions(m["actions"])
		if err != nil {
			return nil, err
		}
		// A catalog kind spelled `type:` is that kind: the block decodes
		// as if `kind:` had named it, so the validator, the import scan
		// and the emitter all read one field. They read different ones
		// before: the load validated `type: card` as a card, and the
		// emitter, which draws the catalog from Kind alone, sent it to the
		// node renderer in a file that did not import it. `type: section`
		// keeps its older meaning, the plain <section> tag.
		blockType, blockKind := stringValue(m["type"]), stringValue(m["kind"])
		if strings.TrimSpace(blockKind) == "" && typeNamesCatalogKind(blockType) {
			blockType, blockKind = "", blockType
		}
		out = append(out, AppSpecBlock{
			Type:      blockType,
			Kind:      blockKind,
			Text:      stringValue(m["text"]),
			Level:     intValue(m["level"]),
			Class:     stringValue(m["class"]),
			Href:      stringValue(m["href"]),
			Entity:    stringValue(m["entity"]),
			Fields:    stringListValue(m["fields"]),
			Limit:     intValue(m["limit"]),
			EmptyText: stringValue(m["empty_text"]),
			Mode:      stringValue(m["mode"]),
			Create:    boolValue(m["create"]),
			Bulk:      boolValue(m["bulk"]),
			Filter:    stringValue(m["filter"]),
			Props:     normalizeControlProps(kind, mapValue(m["props"])),
			Children:  children,
			Actions:   actions,
			Island:    stringValue(m["island"]),
			Widget:    stringValue(m["widget"]),
		})
	}
	return out, nil
}

// refuseReplacedBlockKeys refuses the entity-block keys the entityui
// screens replaced, naming the replacement for each: filters moved to the
// entity's display.facets, transitions to the entity's states, search to
// the entity's search_fields, and island/widget have no replacement
// because entity lists are query-param pages now. The three data keys are
// refused wherever they appear (no other block kind ever read them);
// island/widget only on the entity screens.
func refuseReplacedBlockKeys(m map[string]*coreyaml.Node, kind, context string) error {
	entityBlock := kind == "entity_list" || kind == "entity_detail" || kind == "entity_form" ||
		kind == "entity_create" || kind == "entity_edit"
	for _, key := range []struct {
		name string
		why  string
	}{
		{"filters", "the entity's display: facets:"},
		{"transitions", "the entity's states:"},
		{"search", "the entity's search_fields:"},
		{"island", "lists are query-param pages, no island"},
		{"widget", "lists are query-param pages, no island"},
	} {
		if m[key.name] == nil {
			continue
		}
		if (key.name == "island" || key.name == "widget") && !entityBlock {
			continue
		}
		return fmt.Errorf("app spec: %s: %s is no longer a block key; use %s", context, key.name, key.why)
	}
	return nil
}

func decodeActions(node *coreyaml.Node) ([]AppSpecAction, error) {
	if node == nil {
		return nil, nil
	}
	list, err := expectList(node, "actions")
	if err != nil {
		return nil, err
	}
	out := make([]AppSpecAction, 0, len(list))
	for i, item := range list {
		m, err := expectMap(item, fmt.Sprintf("actions[%d]", i))
		if err != nil {
			return nil, err
		}
		if err := rejectUnknownKeys(m, map[string]bool{"name": true, "event": true, "client_js": true}, fmt.Sprintf("actions[%d]", i)); err != nil {
			return nil, err
		}
		out = append(out, AppSpecAction{
			Name:     stringValue(m["name"]),
			Event:    stringValue(m["event"]),
			ClientJS: stringValue(m["client_js"]),
		})
	}
	return out, nil
}

func decodeAppSpecHooks(node *coreyaml.Node) ([]AppSpecHook, error) {
	list, err := expectList(node, "hooks")
	if err != nil {
		return nil, err
	}
	out := make([]AppSpecHook, 0, len(list))
	for i, item := range list {
		m, err := expectMap(item, fmt.Sprintf("hooks[%d]", i))
		if err != nil {
			return nil, err
		}
		allowed := map[string]bool{"id": true, "entity": true, "when": true, "handler": true, "description": true}
		if err := rejectUnknownKeys(m, allowed, fmt.Sprintf("hooks[%d]", i)); err != nil {
			return nil, err
		}
		out = append(out, AppSpecHook{
			ID:          stringValue(m["id"]),
			Entity:      stringValue(m["entity"]),
			When:        stringValue(m["when"]),
			Handler:     stringValue(m["handler"]),
			Description: stringValue(m["description"]),
		})
	}
	return out, nil
}

func decodeNamedStubs(node *coreyaml.Node, label string) ([]AppSpecNamedStub, error) {
	list, err := expectList(node, label)
	if err != nil {
		return nil, err
	}
	out := make([]AppSpecNamedStub, 0, len(list))
	for i, item := range list {
		if item.Kind == coreyaml.Scalar {
			out = append(out, AppSpecNamedStub{Name: fmt.Sprint(item.Value)})
			continue
		}
		m, err := expectMap(item, fmt.Sprintf("%s[%d]", label, i))
		if err != nil {
			return nil, err
		}
		if err := rejectUnknownKeys(m, map[string]bool{"name": true, "description": true}, fmt.Sprintf("%s[%d]", label, i)); err != nil {
			return nil, err
		}
		out = append(out, AppSpecNamedStub{Name: stringValue(m["name"]), Description: stringValue(m["description"])})
	}
	return out, nil
}

func relationTypeFromString(value string) (framework.RelationType, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "many_to_one", "belongs_to", "belongsto":
		return framework.RelManyToOne, nil
	case "has_one", "hasone":
		return framework.RelHasOne, nil
	case "has_many", "hasmany":
		return framework.RelHasMany, nil
	case "many_to_many", "manytomany":
		return framework.RelManyToMany, nil
	default:
		return framework.RelManyToOne, fmt.Errorf("app spec: unknown relation type %q", value)
	}
}

func expectMap(node *coreyaml.Node, label string) (map[string]*coreyaml.Node, error) {
	if node == nil {
		return nil, fmt.Errorf("app spec: %s is required", label)
	}
	if node.Kind != coreyaml.Map {
		return nil, fmt.Errorf("app spec: %s must be a map at line %d", label, node.Line)
	}
	return node.Map, nil
}

func expectList(node *coreyaml.Node, label string) ([]*coreyaml.Node, error) {
	if node == nil {
		return nil, nil
	}
	if node.Kind != coreyaml.List {
		return nil, fmt.Errorf("app spec: %s must be a list at line %d", label, node.Line)
	}
	return node.List, nil
}

// appLevelKeys are settings that live under `app:`. The most common
// misplacement is writing them at the app spec root, where they silently
// do nothing (an agent or a YAML-merge tool has no way to know).
var appLevelKeys = map[string]bool{
	"name": true, "description": true, "base_url": true, "module": true,
	"db": true, "static_dir": true, "output_dir": true, "api_prefix": true,
	"public_openapi": true, "theme": true, "auth": true, "admin": true,
	"pwa": true, "llm_md": true, "entity_llm_md": true, "locale_prefixes": true,
}

// topLevelKeys are the app spec's root sections; written under `app:`
// they are equally dead.
var topLevelKeys = map[string]bool{
	"entities": true, "screens": true, "nav": true, "seed": true,
	"endpoints": true, "middleware": true, "plugins": true, "helpers": true,
}

// unknownKeyLocationHint names the correct nesting for a known key found
// at the wrong level, so the fix is one edit instead of a schema hunt.
func unknownKeyLocationHint(key, label string) string {
	switch label {
	case "app spec":
		if settings.IsSection(key) {
			return fmt.Sprintf("%s is project configuration, not part of the app: move it under \"%s:\" in %s", key, key, settings.FileName)
		}
		if appLevelKeys[key] {
			return fmt.Sprintf("did you mean \"app.%s:\"? %s is an app-level setting and has no effect at the app spec root", key, key)
		}
	case "app":
		if topLevelKeys[key] {
			return fmt.Sprintf("did you mean \"%s:\" at the top level? %s is an app spec section, not an app setting", key, key)
		}
	}
	return ""
}

// rejectUnknownKeys reports EVERY unknown key at the level in one pass,
// sorted for determinism, each with a location hint when the key is a
// known setting nested at the wrong level. Reporting one per run turned
// fixing an app spec into a serial guess-and-recompile loop.
func rejectUnknownKeys(m map[string]*coreyaml.Node, allowed map[string]bool, label string) error {
	var unknown []string
	for key, node := range m {
		if !allowed[key] && !strings.HasPrefix(key, "x_") && !strings.HasPrefix(key, "x-") {
			unknown = append(unknown, fmt.Sprintf("unknown key %q in %s at line %d", key, label, node.Line))
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	msgs := make([]string, len(unknown))
	for i, msg := range unknown {
		if idx := strings.IndexByte(msg, '"'); idx >= 0 {
			key := msg[idx+1:]
			if end := strings.IndexByte(key, '"'); end >= 0 {
				key = key[:end]
				if hint := unknownKeyLocationHint(key, label); hint != "" {
					msg += ": " + hint
				}
			}
		}
		msgs[i] = msg
	}
	if len(msgs) == 1 {
		return fmt.Errorf("app spec: %s", msgs[0])
	}
	return fmt.Errorf("app spec: %d unknown keys in %s:\n  - %s", len(msgs), label, strings.Join(msgs, "\n  - "))
}
