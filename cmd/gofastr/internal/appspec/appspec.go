package appspec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"maps"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/battery/auth"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/project"
	"github.com/DonaldMurillo/gofastr/codegen"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/core/settings"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	coreyaml "github.com/DonaldMurillo/gofastr/core/yaml"
	"github.com/DonaldMurillo/gofastr/framework"
	fwentity "github.com/DonaldMurillo/gofastr/framework/entity"
	fwmigrate "github.com/DonaldMurillo/gofastr/framework/migrate"
	"github.com/DonaldMurillo/gofastr/internal/inflect"
)

type AppSpec struct {
	App        AppSpecApp
	Entities   []framework.EntityDeclaration
	Screens    []AppSpecScreen
	Nav        []AppSpecNavItem
	Seed       []AppSpecSeedEntity
	Endpoints  []settings.Endpoint
	Hooks      []AppSpecHook
	Middleware []AppSpecNamedStub
	Plugins    []AppSpecNamedStub
	Helpers    []AppSpecNamedStub

	// phrases collects the reader-facing text of the file being
	// emitted (appSpecTx). Each file renderer sets it on its own copy;
	// nil outside one, where text is emitted as a literal. Unexported so
	// it never reads as a spec field (reflection over AppSpec, the
	// serializer's coverage test); appgen reaches it through
	// PhraseCollector and SetPhraseCollector.
	phrases *AppSpecPhrases

	// appless is set by the decoders when the file (or every file of a
	// directory) has no app: section: a fragment for generate app --add,
	// whose App holds no api_prefix default, so the checks that need the
	// project's app settings skip it. Unexported for the reason phrases is.
	appless bool
}

// PhraseCollector returns the phrase collector of the file being
// rendered, nil outside a file renderer.
func (bp *AppSpec) PhraseCollector() *AppSpecPhrases { return bp.phrases }

// MarkFragment records that bp is a fragment added to an existing
// project (generate app --add, generate entity|screen): its App does not
// carry the project's settings, so validation skips the checks that read
// them.
func (bp *AppSpec) MarkFragment() { bp.appless = true }

// SetPhraseCollector installs the phrase collector a file renderer
// emits through, on that renderer's own copy of the spec.
func (bp *AppSpec) SetPhraseCollector(p *AppSpecPhrases) { bp.phrases = p }

// AppSpecSeedEntity holds seed data for one entity.
type AppSpecSeedEntity struct {
	Entity string
	Rows   []map[string]any
	// Count, when > 0, auto-generates that many demo rows for the entity
	// (in addition to any explicit Rows) with deterministic, realistic-looking
	// values. Enum columns get a varied (non-uniform) distribution.
	Count int
	// Weights maps an enum column to a value→weight distribution used when
	// auto-generating rows (e.g. {"status": {"open": 5, "closed": 1}}). A
	// column with no weights entry gets a deterministic skew seeded from the
	// entity name so demos never read as a flat round-robin.
	Weights map[string]map[string]int
}

// AppSpecNavItem describes a navigation entry: a link to a screen or URL.
type AppSpecNavItem struct {
	Label string
	Href  string
	Icon  string
	Role  string // optional: only shown to users holding this role
	Items []AppSpecNavItem
}

type AppSpecApp struct {
	Name string
	// Description is the site-level <meta name="description"> content
	// (app.description). Empty derives one from the app name + entities
	// so the generated app passes uihost strict mode with honest copy.
	Description string
	// BaseURL seeds the generated appBaseURL() fallback, the canonical
	// origin for sitemap <loc> entries (app.base_url). The APP_BASE_URL
	// env var always wins at runtime; empty falls back to localhost.
	BaseURL       string
	Module        string
	DBDriver      string
	DBURL         string
	StaticDir     string
	OutputDir     string
	APIPrefix     string
	PublicOpenAPI bool
	Theme         map[string]string
	ThemeDark     map[string]string // optional dark-scheme color overrides (app.theme.dark)
	Auth          AppSpecAuth
	Admin         AppSpecAdmin
	PWA           AppSpecPWA
	// LLMMD emits uihost.WithPublicLLMMD() so every registered screen
	// serves its /llm.md document (plus the /llm-pages.md index).
	// Independent of PWA. A screen inventory is schema disclosure, so
	// it never rides along with another feature.
	LLMMD bool
	// LocalePrefixes emits framework.WithLocalePrefixes()
	// (app.locale_prefixes): every locales/<tag>.json is served under
	// /<tag>/, with hreflang, sitemap and robots entries. English keeps
	// the plain paths.
	LocalePrefixes bool
}

// AppSpecPWA configures the generated app's installable-PWA surface
// (app.pwa). Enabled emits uihost.WithPWA(...) plus replaceable 192px,
// 512px, and maskable icons under <static>/icons/. Every other field is
// optional; the framework derives the defaults (name from the app
// title, start_url/scope "/", standalone display).
type AppSpecPWA struct {
	Enabled         bool
	Name            string
	ShortName       string
	Description     string
	StartURL        string
	Scope           string
	Display         string // standalone | fullscreen | minimal-ui | browser
	ThemeColor      string
	BackgroundColor string
}

// AppSpecAdmin configures the auto-generated admin back-office (battery/admin).
type AppSpecAdmin struct {
	Enabled      bool
	Path         string // URL prefix, default "/admin"
	Role         string // required role, default "admin"
	LoginPath    string // where to bounce unauthenticated visitors, e.g. "/login"
	SeedEmail    string // bootstrap admin account email
	SeedPassword string // bootstrap admin account password
}

// AppSpecAuth configures the built-in authentication system.
type AppSpecAuth struct {
	Enabled   bool
	DevMode   bool
	BasePath  string // defaults to "/auth"
	JWTSecret string
}

type AppSpecScreen struct {
	Name        string
	Route       string
	Title       string
	Description string
	Type        string
	Layout      string // "marketing" | "app" | "" (default)
	Access      AppSpecAccess
	Body        []AppSpecBlock
}

// AppSpecAccess gates a screen: require a logged-in user and/or a role.
type AppSpecAccess struct {
	Auth bool
	Role string
}

type AppSpecBlock struct {
	Type      string
	Kind      string
	Text      string
	Level     int
	Class     string
	Href      string
	Entity    string
	Fields    []string
	Limit     int
	EmptyText string
	Mode      string // "create", "edit" for entity_form; "table", "cards" for entity_list
	Create    bool   // entity_list: show "New" + mount a create screen at <list>/create
	Bulk      bool   // entity_list: row selection, the bulk bar and Export CSV
	Props     map[string]any
	Children  []AppSpecBlock
	Actions   []AppSpecAction
	Island    string
	Widget    string
}

type AppSpecAction struct {
	Name     string
	Event    string
	ClientJS string
}

// AppSpecHook is one entity lifecycle hook carried through
// graduation. It declares a surface the generated app must implement in
// owned Go: the app spec records WHICH hook runs
// WHERE, and Handler names the func to write.
//
// It exists because kiln's freeze dropped hooks entirely. A world's hooks
// are enforced in the live preview (kiln/render registers them on the
// framework HookRegistry), so an operator watches a before_create
// validation reject bad rows and then ships an app that silently has no
// such validation. kiln.md promises the opposite: "freeze emits an
// owned-Go handler stub with a description naming the declarative
// action".
type AppSpecHook struct {
	// ID is the hook's stable identifier from the kiln world; it doubles
	// as the app spec's name for the hook.
	ID string
	// Entity is the entity whose lifecycle the hook runs on.
	Entity string
	// When is the lifecycle point: before_create, after_update, ...
	When string
	// Handler is the owned-Go func name to implement.
	Handler string
	// Description names the declarative action the hook came from.
	Description string
}

type AppSpecNamedStub struct {
	Name        string
	Description string
}

func LoadAppSpec(path string) (AppSpec, error) {
	return LoadAppSpecPath(path, true)
}

func LoadAppSpecPath(path string, validate bool) (AppSpec, error) {
	info, err := os.Stat(path)
	if err != nil {
		return AppSpec{}, err
	}
	if info.IsDir() {
		entries, err := os.ReadDir(path)
		if err != nil {
			return AppSpec{}, err
		}
		var merged AppSpec
		foundAppSpec := false
		// The app: blocks merge as YAML, key by key, before one decode.
		// A decoded AppSpecApp cannot tell an unset key from its default
		// (api_prefix "api", auth.dev_mode true), so merging decoded
		// structs would let a file that only sets the theme re-apply
		// those defaults over another file's choice.
		appNode := &coreyaml.Node{Kind: coreyaml.Map, Map: map[string]*coreyaml.Node{}}
		appKeyFile := map[string]string{}
		for _, entry := range entries {
			if entry.IsDir() || !isAppSpecFile(entry.Name()) {
				continue
			}
			foundAppSpec = true
			node, err := parseAppSpecFile(filepath.Join(path, entry.Name()))
			if err != nil {
				return AppSpec{}, err
			}
			next, err := DecodeAppSpec(node)
			if err != nil {
				return AppSpec{}, err
			}
			if node.Kind == coreyaml.Map && node.Map["app"] != nil {
				if err := mergeAppNode(appNode, node.Map["app"], "app", entry.Name(), appKeyFile); err != nil {
					return AppSpec{}, fmt.Errorf("app spec directory %q: %w", path, err)
				}
			}
			merged = MergeAppSpecs(merged, next)
		}
		if !foundAppSpec {
			return AppSpec{}, fmt.Errorf("app spec directory %q does not contain any app spec files", path)
		}
		if len(appKeyFile) > 0 {
			app, err := decodeAppSpecApp(appNode)
			if err != nil {
				return AppSpec{}, fmt.Errorf("app spec directory %q: %w", path, err)
			}
			merged.App = app
		}
		merged.appless = len(appKeyFile) == 0
		if validate {
			if err := ValidateAppSpec(merged); err != nil {
				return AppSpec{}, err
			}
		}
		return merged, nil
	}
	bp, err := DecodeAppSpecFile(path)
	if err != nil {
		return AppSpec{}, err
	}
	if validate {
		if err := ValidateAppSpec(bp); err != nil {
			return AppSpec{}, err
		}
	}
	return bp, nil
}

func DecodeAppSpecFile(path string) (AppSpec, error) {
	node, err := parseAppSpecFile(path)
	if err != nil {
		return AppSpec{}, err
	}
	return DecodeAppSpec(node)
}

// parseAppSpecFile reads one .yml/.yaml/.json app spec into a YAML node.
func parseAppSpecFile(path string) (*coreyaml.Node, error) {
	if !isAppSpecFile(path) {
		return nil, fmt.Errorf("app spec %q must be .yml, .yaml, or .json", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(filepath.Ext(path), ".json") {
		raw, err := strictJSONValue(data)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		return yamlNodeFromJSON(raw), nil
	}
	return coreyaml.Parse(string(data))
}

// mergeAppNode folds one file's app: block into dst. Maps merge key by
// key (auth in one file, theme in another); a key that two files both
// set is an error naming both, the same way a duplicate entity or
// screen name is refused rather than one silently winning. seen maps a
// dotted key path to the file that set it.
func mergeAppNode(dst, src *coreyaml.Node, keyPath, file string, seen map[string]string) error {
	if src.Kind != coreyaml.Map {
		return fmt.Errorf("%s: %s must be a map at line %d", file, keyPath, src.Line)
	}
	for _, key := range slices.Sorted(maps.Keys(src.Map)) {
		child := src.Map[key]
		childPath := keyPath + "." + key
		existing, ok := dst.Map[key]
		if !ok {
			if child != nil && child.Kind == coreyaml.Map {
				existing = &coreyaml.Node{Kind: coreyaml.Map, Map: map[string]*coreyaml.Node{}, Line: child.Line, Column: child.Column}
				dst.Map[key] = existing
				seen[childPath] = file
				if err := mergeAppNode(existing, child, childPath, file, seen); err != nil {
					return err
				}
				continue
			}
			dst.Map[key] = child
			seen[childPath] = file
			continue
		}
		if existing != nil && existing.Kind == coreyaml.Map && child != nil && child.Kind == coreyaml.Map {
			if err := mergeAppNode(existing, child, childPath, file, seen); err != nil {
				return err
			}
			continue
		}
		return fmt.Errorf("%s is set in both %s and %s; set it in one file", childPath, seen[childPath], file)
	}
	return nil
}

func yamlNodeFromJSON(value any) *coreyaml.Node {
	switch v := value.(type) {
	case map[string]any:
		out := &coreyaml.Node{Kind: coreyaml.Map, Map: map[string]*coreyaml.Node{}, Line: 1, Column: 1}
		for key, child := range v {
			out.Map[key] = yamlNodeFromJSON(child)
		}
		return out
	case []any:
		out := &coreyaml.Node{Kind: coreyaml.List, Line: 1, Column: 1}
		for _, child := range v {
			out.List = append(out.List, yamlNodeFromJSON(child))
		}
		return out
	default:
		return &coreyaml.Node{Kind: coreyaml.Scalar, Value: v, Line: 1, Column: 1}
	}
}

// strictJSONValue decodes one JSON document, refusing duplicate mapping
// keys. encoding/json is silent last-wins on duplicates, and the YAML leg
// of this decoder already rejects them at the parser (core/yaml: "silent
// last-wins lets a stale or hostile copy-paste line override the value a
// reviewer believes is in force"); the .json leg must not be the seam
// where a merged or agent-transcribed app spec quietly takes the second
// copy. Numbers decode as float64, exactly as json.Unmarshal would, so
// downstream node values keep their type.
func strictJSONValue(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	v, err := decodeJSONValue(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("unexpected trailing data after JSON value")
		}
		return nil, err
	}
	return v, nil
}

// decodeJSONValue walks json.Decoder tokens into the same any-shape
// json.Unmarshal produces (map[string]any, []any, string, float64, bool,
// nil), erroring on a repeated object key anywhere in the tree.
func decodeJSONValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return tok, nil // string, float64, bool or nil
	}
	switch delim {
	case '{':
		obj := map[string]any{}
		for dec.More() {
			keyTok, err := dec.Token()
			if err != nil {
				return nil, err
			}
			key, ok := keyTok.(string)
			if !ok {
				return nil, fmt.Errorf("object key is not a string")
			}
			if _, dup := obj[key]; dup {
				return nil, fmt.Errorf("duplicate object key %q (silent last-wins would override the value a reviewer believes is in force)", key)
			}
			val, err := decodeJSONValue(dec)
			if err != nil {
				return nil, err
			}
			obj[key] = val
		}
		if _, err := dec.Token(); err != nil { // consume '}'
			return nil, err
		}
		return obj, nil
	case '[':
		arr := []any{}
		for dec.More() {
			val, err := decodeJSONValue(dec)
			if err != nil {
				return nil, err
			}
			arr = append(arr, val)
		}
		if _, err := dec.Token(); err != nil { // consume ']'
			return nil, err
		}
		return arr, nil
	default:
		return nil, fmt.Errorf("unexpected delimiter %v", delim)
	}
}

// AppSpecHasTheme reports whether app.theme declared anything: light
// tokens, a dark: map, or both. A theme with only a dark: map decodes to an
// empty light map, so testing the light map alone dropped the dark palette.
func AppSpecHasTheme(app AppSpecApp) bool {
	return len(app.Theme) > 0 || len(app.ThemeDark) > 0
}

func isAppSpecFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".yml" || ext == ".yaml" || ext == ".json"
}

// MergeAppSpecs appends b's list sections to a's. It keeps a.App, or
// takes b.App when a has none: a decoded AppSpecApp cannot tell an unset
// key from a default, so a directory spec merges its app: blocks as YAML
// in LoadAppSpecPath instead. TestMergeAppSpecsAppendsEverySlice fails
// when a new list section is not appended here.
func MergeAppSpecs(a, b AppSpec) AppSpec {
	if reflect.ValueOf(a.App).IsZero() {
		a.App = b.App
	}
	a.Entities = append(a.Entities, b.Entities...)
	a.Screens = append(a.Screens, b.Screens...)
	a.Nav = append(a.Nav, b.Nav...)
	a.Seed = append(a.Seed, b.Seed...)
	a.Endpoints = append(a.Endpoints, b.Endpoints...)
	a.Hooks = append(a.Hooks, b.Hooks...)
	a.Middleware = append(a.Middleware, b.Middleware...)
	a.Plugins = append(a.Plugins, b.Plugins...)
	a.Helpers = append(a.Helpers, b.Helpers...)
	return a
}

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

// authBasePathRE matches an app.auth.base_path: one or more "/segment"
// with no trailing slash. A segment cannot start with a dot, so "." and
// ".." never reach a mount.
var authBasePathRE = regexp.MustCompile(`^(/[A-Za-z0-9_-][A-Za-z0-9._-]*)+$`)

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
	allowed := map[string]bool{"name": true, "description": true, "base_url": true, "module": true, "db": true, "static_dir": true, "output_dir": true, "api_prefix": true, "public_openapi": true, "theme": true, "auth": true, "admin": true, "pwa": true, "llm_md": true, "locale_prefixes": true}
	if err := rejectUnknownKeys(m, allowed, "app"); err != nil {
		return AppSpecApp{}, err
	}
	app := AppSpecApp{
		Name:        stringValue(m["name"]),
		Description: stringValue(m["description"]),
		BaseURL:     stringValue(m["base_url"]),
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

func decodeAppSpecEntities(node *coreyaml.Node) ([]framework.EntityDeclaration, error) {
	list, err := expectList(node, "entities")
	if err != nil {
		return nil, err
	}
	out := make([]framework.EntityDeclaration, 0, len(list))
	for i, item := range list {
		context := fmt.Sprintf("entities[%d]", i)
		m, err := expectMap(item, context)
		if err != nil {
			return nil, err
		}
		allowed := map[string]bool{
			"name": true, "table": true, "fields": true, "relations": true,
			"endpoints": true, "scope": true, "pagination": true, "exposure": true,
			"soft_delete": true, "multi_tenant": true, "tenant_field": true,
			"owner_field": true, "cross_owner_read": true, "search_fields": true,
			"access": true, "public": true, "timestamps": true, "crud": true,
			"mcp": true, "cursor_field": true, "cursor_fields": true,
			"max_list_limit": true, "indices": true, "properties": true,
			"renames": true, "read_scope": true,
			"display": true, "states": true, "primary_key": true,
		}
		if err := rejectUnknownKeys(m, allowed, context); err != nil {
			return nil, err
		}
		decl := framework.EntityDeclaration{
			Name:         stringValue(m["name"]),
			Table:        stringValue(m["table"]),
			SearchFields: stringListValue(m["search_fields"]),
			Properties:   mapValue(m["properties"]),
			Renames:      stringStringMapValue(m["renames"]),
		}
		if node := m["primary_key"]; node != nil {
			if node.Kind != coreyaml.List || len(node.List) == 0 {
				return nil, fmt.Errorf("%s.primary_key: want a non-empty list of field names", context)
			}
			decl.PrimaryKey = stringListValue(node)
		}
		if m["timestamps"] != nil {
			v := boolValue(m["timestamps"])
			decl.Timestamps = &v
		}

		scope, err := decodeEntityScope(m["scope"], context+".scope")
		if err != nil {
			return nil, err
		}
		decl.Scope, err = mergeEntityScopeDeclaration(decl.Name, m, scope, context)
		if err != nil {
			return nil, err
		}
		pagination, err := decodeEntityPagination(m["pagination"], context+".pagination")
		if err != nil {
			return nil, err
		}
		decl.Pagination, err = mergeEntityPaginationDeclaration(decl.Name, m, pagination, context)
		if err != nil {
			return nil, err
		}
		flatAccess, err := decodeEntityAccess(m["access"], context+".access")
		if err != nil {
			return nil, err
		}
		flatReadScope, err := decodeEntityReadScope(m["read_scope"], context+".read_scope")
		if err != nil {
			return nil, err
		}
		exposure, err := decodeEntityExposure(m["exposure"], context+".exposure")
		if err != nil {
			return nil, err
		}
		decl.Exposure, err = mergeEntityExposureDeclaration(decl.Name, m, exposure, flatAccess, flatReadScope, context)
		if err != nil {
			return nil, err
		}

		fields, err := decodeFields(m["fields"])
		if err != nil {
			return nil, err
		}
		decl.Fields = fields
		// owner_field names a per-row owner column. Auto-inject it as a hidden
		// string column so AutoMigrate creates it and auto-CRUD can stamp/scope
		// it, without the author hand-declaring a field they never want shown in
		// a form or table. pack drops this synthesized column on the way back.
		if decl.Scope != nil && decl.Scope.OwnerField != "" &&
			!appSpecEntityHasField(decl, decl.Scope.OwnerField) {
			decl.Fields = append(decl.Fields, framework.FieldDeclaration{
				Name:   decl.Scope.OwnerField,
				Type:   "string",
				Hidden: true,
			})
		}
		// cross_owner_read only applies to an owner-scoped entity.
		if decl.Scope != nil && decl.Scope.CrossOwnerRead != "" && decl.Scope.OwnerField == "" {
			return nil, fmt.Errorf(
				"app spec: entity %q sets cross_owner_read %q but has no owner_field (cross-owner read only applies to owner-scoped entities)",
				decl.Name, decl.Scope.CrossOwnerRead,
			)
		}
		for _, sf := range decl.SearchFields {
			var found *framework.FieldDeclaration
			for j := range decl.Fields {
				if decl.Fields[j].Name == sf {
					found = &decl.Fields[j]
					break
				}
			}
			if found == nil {
				return nil, fmt.Errorf("app spec: entity %q search_fields entry %q is not a declared field", decl.Name, sf)
			}
			if found.Hidden {
				return nil, fmt.Errorf("app spec: entity %q search_fields entry %q is Hidden (search would disclose its values)", decl.Name, sf)
			}
			if found.NoQuery {
				return nil, fmt.Errorf("app spec: entity %q search_fields entry %q is no_query (?q= would match on the stored value)", decl.Name, sf)
			}
			if found.Type != "string" && found.Type != "text" {
				return nil, fmt.Errorf("app spec: entity %q search_fields entry %q must be string or text, got %q", decl.Name, sf, found.Type)
			}
		}
		// read_scope fields are checked here for the same reason search_fields
		// are: entity.Define panics at registration, which for an app spec
		// means a generated app that dies at boot. Failing at decode names the
		// entity and the column now.
		if rs := EntityDeclarationExposure(decl).ReadScope; rs != nil {
			for _, p := range rs.Filter {
				var found *framework.FieldDeclaration
				for j := range decl.Fields {
					if decl.Fields[j].Name == p.Field {
						found = &decl.Fields[j]
						break
					}
				}
				if found == nil {
					return nil, fmt.Errorf("app spec: entity %q read_scope field %q is not a declared field", decl.Name, p.Field)
				}
				if found.Hidden {
					return nil, fmt.Errorf("app spec: entity %q read_scope field %q is Hidden (a predicate on a masked column leaks its values through the row set)", decl.Name, p.Field)
				}
			}
		}
		var cursorFields []string
		if decl.Pagination != nil {
			cursorFields = append(cursorFields, decl.Pagination.CursorFields...)
			cursorFields = append(cursorFields, decl.Pagination.CursorField)
		}
		for _, cf := range cursorFields {
			if cf == "" {
				continue
			}
			found, ok := appSpecColumn(decl, cf)
			if !ok {
				return nil, fmt.Errorf("app spec: entity %q cursor field %q is not a declared field", decl.Name, cf)
			}
			if found == nil {
				continue
			}
			if found.Hidden {
				return nil, fmt.Errorf("app spec: entity %q cursor field %q is Hidden (it is not in the projection, so paging cannot read it)", decl.Name, cf)
			}
			if found.NoQuery {
				return nil, fmt.Errorf("app spec: entity %q cursor field %q is no_query (the cursor token would carry its stored value)", decl.Name, cf)
			}
			// The rule entity.Define enforces, reported at spec time: a
			// keyset column that can hold NULL skips those rows.
			isKey := slices.Contains(decl.PrimaryKey, cf) || (len(decl.PrimaryKey) == 0 && cf == "id")
			if !isKey && !found.Required && found.AutoGenerate == "" {
				return nil, fmt.Errorf("app spec: entity %q cursor field %q can hold NULL (keyset paging skips NULL rows); mark it required", decl.Name, cf)
			}
		}
		relations, err := decodeRelations(m["relations"])
		if err != nil {
			return nil, err
		}
		decl.Relations = relations
		indices, err := decodeIndices(m["indices"])
		if err != nil {
			return nil, err
		}
		decl.Indices = indices
		display, err := decodeDisplayDeclaration(m["display"], context+".display")
		if err != nil {
			return nil, err
		}
		decl.Display = display
		states, err := decodeStatesDeclaration(m["states"], context+".states")
		if err != nil {
			return nil, err
		}
		decl.States = states
		if m["endpoints"] != nil {
			return nil, fmt.Errorf("%s: an entity no longer lists endpoints; move each to the top-level endpoints: list with entity: %s and a handler link (entities/<file>.go#<Name>)", context, decl.Name)
		}
		out = append(out, decl)
	}
	return out, nil
}

// decodeDisplayDeclaration decodes an entity's display: group through the
// declaration's own strict JSON shape, so the app spec refuses exactly the
// keys the entity package refuses, with its error wrapped in the yaml
// position. A present-but-empty block is a non-nil zero config, matching
// how the scope and exposure groups decode.
func decodeDisplayDeclaration(node *coreyaml.Node, context string) (*fwentity.DisplayConfig, error) {
	if node == nil {
		return nil, nil
	}
	data, err := json.Marshal(codegen.AnyValue(node))
	if err != nil {
		return nil, fmt.Errorf("app spec: %s: %w", context, err)
	}
	var d fwentity.DisplayConfig
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("app spec: %s: %w", context, err)
	}
	return &d, nil
}

// decodeStatesDeclaration is decodeDisplayDeclaration's twin for states:.
func decodeStatesDeclaration(node *coreyaml.Node, context string) (*fwentity.StatesConfig, error) {
	if node == nil {
		return nil, nil
	}
	data, err := json.Marshal(codegen.AnyValue(node))
	if err != nil {
		return nil, fmt.Errorf("app spec: %s: %w", context, err)
	}
	var s fwentity.StatesConfig
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("app spec: %s: %w", context, err)
	}
	return &s, nil
}

func mergeEntityScopeDeclaration(
	name string,
	root map[string]*coreyaml.Node,
	grouped *fwentity.ScopeDeclaration,
	context string,
) (*fwentity.ScopeDeclaration, error) {
	groupMap, err := optionalEntityGroupMap(root["scope"], context+".scope")
	if err != nil {
		return nil, err
	}
	keys := []string{"soft_delete", "multi_tenant", "tenant_field", "owner_field", "cross_owner_read"}
	if grouped == nil && !hasEntityDeclarationKey(root, keys...) {
		return nil, nil
	}
	if grouped == nil {
		grouped = &fwentity.ScopeDeclaration{}
	}
	// soft_delete and multi_tenant are access-gating: their FALSE value is the
	// permissive state (hard deletes, no tenant scoping). The grouped
	// scope.* spellings use protectiveBool; the flat spellings MUST too, or a
	// YAML-1.1 truthy like `multi_tenant: yes` (a string in YAML 1.2) silently
	// reads false and drops tenant isolation. See protectiveBool for the why.
	if node := root["soft_delete"]; node != nil {
		flat, err := protectiveBool(root, "soft_delete", context)
		if err != nil {
			return nil, err
		}
		if groupMap["soft_delete"] != nil && flat != grouped.SoftDelete {
			return nil, entityDeclarationConflict(name, "soft_delete", "scope.soft_delete", flat, grouped.SoftDelete)
		}
		grouped.SoftDelete = flat
	}
	if node := root["multi_tenant"]; node != nil {
		flat, err := protectiveBool(root, "multi_tenant", context)
		if err != nil {
			return nil, err
		}
		if groupMap["multi_tenant"] != nil && flat != grouped.MultiTenant {
			return nil, entityDeclarationConflict(name, "multi_tenant", "scope.multi_tenant", flat, grouped.MultiTenant)
		}
		grouped.MultiTenant = flat
	}
	for key, target := range map[string]*string{
		"tenant_field": &grouped.TenantField, "owner_field": &grouped.OwnerField,
		"cross_owner_read": &grouped.CrossOwnerRead,
	} {
		if node := root[key]; node != nil {
			// requireScalarString: owner_field/tenant_field as a list or
			// map must be an error, not "" — an empty owner field
			// silently no-ops owner scoping and exposes every row to
			// every caller.
			flat, err := requireScalarString(node, context, key)
			if err != nil {
				return nil, err
			}
			if groupMap[key] != nil && flat != *target {
				return nil, entityDeclarationConflict(name, key, "scope."+key, flat, *target)
			}
			*target = flat
		}
	}
	return grouped, nil
}

func mergeEntityPaginationDeclaration(
	name string,
	root map[string]*coreyaml.Node,
	grouped *fwentity.PaginationDeclaration,
	context string,
) (*fwentity.PaginationDeclaration, error) {
	groupMap, err := optionalEntityGroupMap(root["pagination"], context+".pagination")
	if err != nil {
		return nil, err
	}
	keys := []string{"cursor_field", "cursor_fields", "max_list_limit"}
	if grouped == nil && !hasEntityDeclarationKey(root, keys...) {
		return nil, nil
	}
	if grouped == nil {
		grouped = &fwentity.PaginationDeclaration{}
	}
	if node := root["cursor_field"]; node != nil {
		flat := stringValue(node)
		if groupMap["cursor_field"] != nil && flat != grouped.CursorField {
			return nil, entityDeclarationConflict(name, "cursor_field", "pagination.cursor_field", flat, grouped.CursorField)
		}
		grouped.CursorField = flat
	}
	if node := root["cursor_fields"]; node != nil {
		flat := stringListValue(node)
		if groupMap["cursor_fields"] != nil && !slices.Equal(flat, grouped.CursorFields) {
			return nil, entityDeclarationConflict(name, "cursor_fields", "pagination.cursor_fields", flat, grouped.CursorFields)
		}
		grouped.CursorFields = flat
	}
	if node := root["max_list_limit"]; node != nil {
		flat := intValue(node)
		if groupMap["max_list_limit"] != nil && flat != grouped.MaxListLimit {
			return nil, entityDeclarationConflict(name, "max_list_limit", "pagination.max_list_limit", flat, grouped.MaxListLimit)
		}
		grouped.MaxListLimit = flat
	}
	return grouped, nil
}

func mergeEntityExposureDeclaration(
	name string,
	root map[string]*coreyaml.Node,
	grouped *fwentity.ExposureDeclaration,
	flatAccess *fwentity.AccessDeclaration,
	flatReadScope *fwentity.ReadScopeDeclaration,
	context string,
) (*fwentity.ExposureDeclaration, error) {
	groupMap, err := optionalEntityGroupMap(root["exposure"], context+".exposure")
	if err != nil {
		return nil, err
	}
	keys := []string{"crud", "mcp", "public", "access", "read_scope"}
	if grouped == nil && !hasEntityDeclarationKey(root, keys...) {
		return nil, nil
	}
	if grouped == nil {
		grouped = &fwentity.ExposureDeclaration{}
	}
	if node := root["crud"]; node != nil {
		flat := boolValue(node)
		if groupMap["crud"] != nil && (grouped.CRUD == nil || flat != *grouped.CRUD) {
			return nil, entityDeclarationConflict(name, "crud", "exposure.crud", flat, grouped.CRUD)
		}
		grouped.CRUD = &flat
	}
	if node := root["mcp"]; node != nil {
		flat := boolValue(node)
		if groupMap["mcp"] != nil && flat != grouped.MCP {
			return nil, entityDeclarationConflict(name, "mcp", "exposure.mcp", flat, grouped.MCP)
		}
		grouped.MCP = flat
	}
	if node := root["public"]; node != nil {
		flat := boolValue(node)
		if groupMap["public"] != nil && flat != grouped.Public {
			return nil, entityDeclarationConflict(name, "public", "exposure.public", flat, grouped.Public)
		}
		grouped.Public = flat
	}
	if flatAccess != nil {
		if groupMap["access"] != nil && (grouped.Access == nil || *flatAccess != *grouped.Access) {
			return nil, entityDeclarationConflict(name, "access", "exposure.access", flatAccess, grouped.Access)
		}
		grouped.Access = flatAccess
	}
	if flatReadScope != nil {
		// ReadScopeDeclaration holds slices, so != cannot compare it; a
		// shallow-but-wrong conflict must still be a hard error like access.
		if groupMap["read_scope"] != nil && (grouped.ReadScope == nil || !reflect.DeepEqual(*flatReadScope, *grouped.ReadScope)) {
			return nil, entityDeclarationConflict(name, "read_scope", "exposure.read_scope", flatReadScope, grouped.ReadScope)
		}
		grouped.ReadScope = flatReadScope
	}
	return grouped, nil
}

func optionalEntityGroupMap(node *coreyaml.Node, context string) (map[string]*coreyaml.Node, error) {
	if node == nil {
		return map[string]*coreyaml.Node{}, nil
	}
	return expectMap(node, context)
}

func hasEntityDeclarationKey(root map[string]*coreyaml.Node, keys ...string) bool {
	for _, key := range keys {
		if root[key] != nil {
			return true
		}
	}
	return false
}

func entityDeclarationConflict(name, flatPath, groupedPath string, flat, grouped any) error {
	return fmt.Errorf(
		"app spec: entity %q has conflicting values for %s and %s (%v != %v)",
		name, flatPath, groupedPath, flat, grouped,
	)
}

func decodeEntityScope(node *coreyaml.Node, context string) (*fwentity.ScopeDeclaration, error) {
	if node == nil {
		return nil, nil
	}
	m, err := expectMap(node, context)
	if err != nil {
		return nil, err
	}
	if err := rejectUnknownKeys(m, map[string]bool{"soft_delete": true, "multi_tenant": true, "tenant_field": true, "owner_field": true, "cross_owner_read": true}, context); err != nil {
		return nil, err
	}
	// soft_delete and multi_tenant are protective: false means hard deletes and
	// no tenant scoping. See protectiveBool.
	softDelete, err := protectiveBool(m, "soft_delete", context)
	if err != nil {
		return nil, err
	}
	multiTenant, err := protectiveBool(m, "multi_tenant", context)
	if err != nil {
		return nil, err
	}
	tenantField, err := requireScalarString(m["tenant_field"], context, "tenant_field")
	if err != nil {
		return nil, err
	}
	ownerField, err := requireScalarString(m["owner_field"], context, "owner_field")
	if err != nil {
		return nil, err
	}
	crossOwnerRead, err := requireScalarString(m["cross_owner_read"], context, "cross_owner_read")
	if err != nil {
		return nil, err
	}
	return &fwentity.ScopeDeclaration{
		SoftDelete: softDelete, MultiTenant: multiTenant,
		TenantField: tenantField, OwnerField: ownerField,
		CrossOwnerRead: crossOwnerRead,
	}, nil
}

func decodeEntityPagination(node *coreyaml.Node, context string) (*fwentity.PaginationDeclaration, error) {
	if node == nil {
		return nil, nil
	}
	m, err := expectMap(node, context)
	if err != nil {
		return nil, err
	}
	if err := rejectUnknownKeys(m, map[string]bool{"cursor_field": true, "cursor_fields": true, "max_list_limit": true}, context); err != nil {
		return nil, err
	}
	return &fwentity.PaginationDeclaration{
		CursorField: stringValue(m["cursor_field"]), CursorFields: stringListValue(m["cursor_fields"]),
		MaxListLimit: intValue(m["max_list_limit"]),
	}, nil
}

func decodeEntityExposure(node *coreyaml.Node, context string) (*fwentity.ExposureDeclaration, error) {
	if node == nil {
		return nil, nil
	}
	m, err := expectMap(node, context)
	if err != nil {
		return nil, err
	}
	if err := rejectUnknownKeys(m, map[string]bool{"crud": true, "mcp": true, "public": true, "access": true, "read_scope": true}, context); err != nil {
		return nil, err
	}
	var crud *bool
	if m["crud"] != nil {
		v := boolValue(m["crud"])
		crud = &v
	}
	access, err := decodeEntityAccess(m["access"], context+".access")
	if err != nil {
		return nil, err
	}
	readScope, err := decodeEntityReadScope(m["read_scope"], context+".read_scope")
	if err != nil {
		return nil, err
	}
	return &fwentity.ExposureDeclaration{CRUD: crud, MCP: boolValue(m["mcp"]), Public: boolValue(m["public"]), Access: access, ReadScope: readScope}, nil
}

func EntityDeclarationScope(decl framework.EntityDeclaration) fwentity.ScopeDeclaration {
	if decl.Scope == nil {
		return fwentity.ScopeDeclaration{}
	}
	return *decl.Scope
}

func EntityDeclarationExposure(decl framework.EntityDeclaration) fwentity.ExposureDeclaration {
	if decl.Exposure == nil {
		return fwentity.ExposureDeclaration{}
	}
	return *decl.Exposure
}

func EntityDeclarationCRUDEnabled(decl framework.EntityDeclaration) bool {
	exposure := EntityDeclarationExposure(decl)
	return exposure.CRUD == nil || *exposure.CRUD
}

// appSpecEntityHasField reports whether the declaration already lists a field
// of the given name (case-sensitive; column names are verbatim).
func appSpecEntityHasField(decl framework.EntityDeclaration, name string) bool {
	for _, f := range decl.Fields {
		if f.Name == name {
			return true
		}
	}
	return false
}

// decodeEntityAccess decodes an entity's `access:` map: the per-operation
// RBAC permissions mirroring EntityConfig.Access. nil node = no RBAC gating
// (the key is optional and additive; existing app specs are unaffected).
// Every perm is requireScalarString: stringValue answers "" for a list or a
// map, and an empty permission is the UNGATED setting (requirePermission
// treats "" as no gate), so `read: [posts:review]` must be an error, not a
// silently world-open read.
func decodeEntityAccess(node *coreyaml.Node, context string) (*fwentity.AccessDeclaration, error) {
	if node == nil {
		return nil, nil
	}
	m, err := expectMap(node, context)
	if err != nil {
		return nil, err
	}
	if err := rejectUnknownKeys(m, map[string]bool{"read": true, "create": true, "update": true, "delete": true}, context); err != nil {
		return nil, err
	}
	read, err := requireScalarString(m["read"], context, "read")
	if err != nil {
		return nil, err
	}
	create, err := requireScalarString(m["create"], context, "create")
	if err != nil {
		return nil, err
	}
	update, err := requireScalarString(m["update"], context, "update")
	if err != nil {
		return nil, err
	}
	del, err := requireScalarString(m["delete"], context, "delete")
	if err != nil {
		return nil, err
	}
	return &fwentity.AccessDeclaration{
		Read:   read,
		Create: create,
		Update: update,
		Delete: del,
	}, nil
}

// decodeEntityReadScope decodes an entity's `read_scope:` map — the row
// filter mirroring EntityConfig.Exposure.ReadScope. nil node = no row
// filtering. Every level is strict (rejectUnknownKeys plus the op/arity
// checks below): a typo in a read posture must fail the build here, at the
// YAML location, not surface as a panic at app registration or, worse, as a
// predicate that silently matches nothing.
// requireScalarString reads a scalar node, refusing any other node kind.
//
// stringValue answers "" for a list or a map, which is fine for a cosmetic
// field and wrong for a security posture: `unrestricted: [posts:review]`
// decoded to the empty string, and an empty Unrestricted is the WEAKEST
// setting there is, lifting the read scope for every signed-in caller instead
// of only the permission holder. A posture that a YAML shape silently widens
// is the failure this whole decode path exists to prevent, so the shape is an
// error rather than a default.
func requireScalarString(node *coreyaml.Node, context, key string) (string, error) {
	if node == nil {
		return "", nil
	}
	if node.Kind == coreyaml.Map && node.Value == nil && len(node.Map) == 0 {
		// YAML null (a key with no value): the deliberate blank
		// spelling, e.g. `read:` = anonymous read. coreyaml hands null
		// back as an entry-less map node, so the entry count — not the
		// kind alone — separates it from a real (refusable) map.
		return "", nil
	}
	if node.Kind != coreyaml.Scalar {
		return "", fmt.Errorf("%s: %q must be a single value, got a %s", context, key, yamlKindName(node.Kind))
	}
	if node.Value == nil {
		return "", nil
	}
	return fmt.Sprint(node.Value), nil
}

// yamlKindName names a node kind for an error message.
func yamlKindName(k coreyaml.Kind) string {
	switch k {
	case coreyaml.Scalar:
		return "value"
	case coreyaml.List:
		return "list"
	case coreyaml.Map:
		return "map"
	}
	return "node"
}

func decodeEntityReadScope(node *coreyaml.Node, context string) (*fwentity.ReadScopeDeclaration, error) {
	if node == nil {
		return nil, nil
	}
	m, err := expectMap(node, context)
	if err != nil {
		return nil, err
	}
	if err := rejectUnknownKeys(m, map[string]bool{"unrestricted": true, "filter": true}, context); err != nil {
		return nil, err
	}
	var filter []fwentity.RowPredicateDeclaration
	if fn := m["filter"]; fn != nil {
		list, err := expectList(fn, context+".filter")
		if err != nil {
			return nil, err
		}
		for i, item := range list {
			predContext := fmt.Sprintf("%s.filter[%d]", context, i)
			pm, err := expectMap(item, predContext)
			if err != nil {
				return nil, err
			}
			if err := rejectUnknownKeys(pm, map[string]bool{"field": true, "op": true, "value": true, "values": true}, predContext); err != nil {
				return nil, err
			}
			predField, err := requireScalarString(pm["field"], predContext, "field")
			if err != nil {
				return nil, err
			}
			predOp, err := requireScalarString(pm["op"], predContext, "op")
			if err != nil {
				return nil, err
			}
			predValue, err := requireScalarString(pm["value"], predContext, "value")
			if err != nil {
				return nil, err
			}
			pred := fwentity.RowPredicateDeclaration{
				Field:  predField,
				Op:     predOp,
				Value:  predValue,
				Values: stringListValue(pm["values"]),
			}
			// The framework re-validates at registration (entity.Define) and
			// panics; failing here instead turns a boot-time crash into a
			// generate-time error that names the YAML path.
			if pred.Field == "" {
				return nil, fmt.Errorf("%s.field is required (a predicate with no column matches nothing)", predContext)
			}
			switch pred.Op {
			case "", "eq", "neq":
				if pred.Value == "" && len(pred.Values) == 0 {
					return nil, fmt.Errorf("%s: op %q needs a value (eq/neq compare one value; empty means eq)", predContext, pred.Op)
				}
				if len(pred.Values) > 0 {
					return nil, fmt.Errorf("%s: op %q must not set values (only in/not_in take a list)", predContext, pred.Op)
				}
			case "in", "not_in":
				if len(pred.Values) == 0 {
					return nil, fmt.Errorf("%s: op %q requires a non-empty values list", predContext, pred.Op)
				}
				if pred.Value != "" {
					return nil, fmt.Errorf("%s: op %q must not also set value (value and values are mutually exclusive)", predContext, pred.Op)
				}
			default:
				return nil, fmt.Errorf("%s.op is %q; must be one of eq, neq, in, not_in (empty means eq)", predContext, pred.Op)
			}
			filter = append(filter, pred)
		}
	}
	unrestricted, err := requireScalarString(m["unrestricted"], context, "unrestricted")
	if err != nil {
		return nil, err
	}
	if len(filter) == 0 && unrestricted == "" {
		return nil, fmt.Errorf("%s declares no filter and no unrestricted, so it filters nothing; remove the block or add a filter", context)
	}
	return &fwentity.ReadScopeDeclaration{
		Filter:       filter,
		Unrestricted: unrestricted,
	}, nil
}

func decodeIndices(node *coreyaml.Node) ([]framework.Index, error) {
	if node == nil {
		return nil, nil
	}
	list, err := expectList(node, "indices")
	if err != nil {
		return nil, err
	}
	out := make([]framework.Index, 0, len(list))
	for i, item := range list {
		m, err := expectMap(item, fmt.Sprintf("indices[%d]", i))
		if err != nil {
			return nil, err
		}
		if err := rejectUnknownKeys(m, map[string]bool{"name": true, "columns": true, "unique": true}, fmt.Sprintf("indices[%d]", i)); err != nil {
			return nil, err
		}
		out = append(out, framework.Index{
			Name:    stringValue(m["name"]),
			Columns: stringListValue(m["columns"]),
			Unique:  boolValue(m["unique"]),
		})
	}
	return out, nil
}

func decodeFields(node *coreyaml.Node) ([]framework.FieldDeclaration, error) {
	if node == nil {
		return nil, nil
	}
	list, err := expectList(node, "fields")
	if err != nil {
		return nil, err
	}
	out := make([]framework.FieldDeclaration, 0, len(list))
	for i, item := range list {
		m, err := expectMap(item, fmt.Sprintf("fields[%d]", i))
		if err != nil {
			return nil, err
		}
		allowed := map[string]bool{"name": true, "type": true, "required": true, "unique": true, "default": true, "auto_generate": true, "read_only": true, "hidden": true, "no_query": true, "max": true, "min": true, "pattern": true, "values": true, "to": true, "many": true}
		if err := rejectUnknownKeys(m, allowed, fmt.Sprintf("fields[%d]", i)); err != nil {
			return nil, err
		}
		// read_only / hidden / no_query are protective: false exposes the field
		// to clients, to every API response, and to the filter surface
		// respectively. See protectiveBool.
		fieldCtx := fmt.Sprintf("fields[%d]", i)
		readOnly, err := protectiveBool(m, "read_only", fieldCtx)
		if err != nil {
			return nil, err
		}
		hidden, err := protectiveBool(m, "hidden", fieldCtx)
		if err != nil {
			return nil, err
		}
		noQuery, err := protectiveBool(m, "no_query", fieldCtx)
		if err != nil {
			return nil, err
		}
		field := framework.FieldDeclaration{
			Name:         stringValue(m["name"]),
			Type:         stringValue(m["type"]),
			Required:     boolValue(m["required"]),
			Unique:       boolValue(m["unique"]),
			Default:      scalarValue(m["default"]),
			AutoGenerate: stringValue(m["auto_generate"]),
			ReadOnly:     readOnly,
			Hidden:       hidden,
			NoQuery:      noQuery,
			Pattern:      stringValue(m["pattern"]),
			Values:       stringListValue(m["values"]),
			To:           stringValue(m["to"]),
			Many:         boolValue(m["many"]),
		}
		if m["max"] != nil {
			v := floatValue(m["max"])
			field.Max = &v
		}
		if m["min"] != nil {
			v := floatValue(m["min"])
			field.Min = &v
		}
		out = append(out, field)
	}
	return out, nil
}

func decodeRelations(node *coreyaml.Node) ([]framework.Relation, error) {
	if node == nil {
		return nil, nil
	}
	list, err := expectList(node, "relations")
	if err != nil {
		return nil, err
	}
	out := make([]framework.Relation, 0, len(list))
	for i, item := range list {
		m, err := expectMap(item, fmt.Sprintf("relations[%d]", i))
		if err != nil {
			return nil, err
		}
		allowed := map[string]bool{"type": true, "name": true, "entity": true, "foreign_key": true, "through": true, "local_key": true, "foreign_key_target": true, "on_delete": true, "cascade_write": true}
		if err := rejectUnknownKeys(m, allowed, fmt.Sprintf("relations[%d]", i)); err != nil {
			return nil, err
		}
		relType, err := relationTypeFromString(stringValue(m["type"]))
		if err != nil {
			return nil, err
		}
		var onDelete framework.OnDeleteAction
		if odVal := stringValue(m["on_delete"]); odVal != "" {
			switch strings.ToLower(odVal) {
			case "cascade":
				onDelete = framework.OnDeleteCascade
			case "set_null", "set null":
				onDelete = framework.OnDeleteSetNull
			case "restrict":
				onDelete = framework.OnDeleteRestrict
			case "no_action", "no action":
				onDelete = framework.OnDeleteNoAction
			default:
				return nil, fmt.Errorf("relations[%d]: unknown on_delete action %q", i, odVal)
			}
		}
		cascadeWrite := boolValue(m["cascade_write"])
		// foreign_key is one column, or a list of one column per key
		// column of a composite key it references.
		var foreignKey []string
		if node := m["foreign_key"]; node != nil && node.Kind == coreyaml.List {
			foreignKey = stringListValue(node)
			if len(foreignKey) == 0 || slices.Contains(foreignKey, "") {
				return nil, fmt.Errorf("relations[%d].foreign_key: want a column name or a non-empty list of them", i)
			}
		} else if fk := stringValue(m["foreign_key"]); fk != "" {
			foreignKey = []string{fk}
		}
		out = append(out, framework.Relation{
			Type:             relType,
			Name:             stringValue(m["name"]),
			Entity:           stringValue(m["entity"]),
			ForeignKey:       foreignKey,
			Through:          stringValue(m["through"]),
			LocalKey:         stringValue(m["local_key"]),
			ForeignKeyTarget: stringValue(m["foreign_key_target"]),
			OnDelete:         onDelete,
			CascadeWrite:     cascadeWrite,
		})
	}
	return out, nil
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

func decodeAppSpecSeed(node *coreyaml.Node) ([]AppSpecSeedEntity, error) {
	list, err := expectList(node, "seed")
	if err != nil {
		return nil, err
	}
	out := make([]AppSpecSeedEntity, 0, len(list))
	for i, item := range list {
		m, err := expectMap(item, fmt.Sprintf("seed[%d]", i))
		if err != nil {
			return nil, err
		}
		if err := rejectUnknownKeys(m, map[string]bool{"entity": true, "rows": true, "count": true, "weights": true}, fmt.Sprintf("seed[%d]", i)); err != nil {
			return nil, err
		}
		entity := stringValue(m["entity"])
		if entity == "" {
			return nil, fmt.Errorf("seed[%d]: entity is required", i)
		}
		var rows []map[string]any
		if rowsNode := m["rows"]; rowsNode != nil {
			rowList, err := expectList(rowsNode, fmt.Sprintf("seed[%d].rows", i))
			if err != nil {
				return nil, err
			}
			for j, rowNode := range rowList {
				rowMap, err := expectMap(rowNode, fmt.Sprintf("seed[%d].rows[%d]", i, j))
				if err != nil {
					return nil, err
				}
				row := make(map[string]any, len(rowMap))
				for k, v := range rowMap {
					row[k] = codegen.AnyValue(v)
				}
				rows = append(rows, row)
			}
		}
		count := 0
		if cn := m["count"]; cn != nil {
			var err error
			count, err = strictIntValue(cn, fmt.Sprintf("seed[%d].count", i))
			if err != nil {
				return nil, err
			}
			if count < 0 {
				return nil, fmt.Errorf("seed[%d].count must be >= 0", i)
			}
		}
		weights, err := decodeSeedWeights(m["weights"], fmt.Sprintf("seed[%d].weights", i))
		if err != nil {
			return nil, err
		}
		out = append(out, AppSpecSeedEntity{Entity: entity, Rows: rows, Count: count, Weights: weights})
	}
	return out, nil
}

// decodeSeedWeights parses the optional per-column weight map used when
// auto-generating rows: `weights: { <column>: { <value>: <weight> } }`.
func decodeSeedWeights(node *coreyaml.Node, label string) (map[string]map[string]int, error) {
	if node == nil {
		return nil, nil
	}
	cols, err := expectMap(node, label)
	if err != nil {
		return nil, err
	}
	out := make(map[string]map[string]int, len(cols))
	for col, child := range cols {
		valMap, err := expectMap(child, label+"."+col)
		if err != nil {
			return nil, err
		}
		vw := make(map[string]int, len(valMap))
		for val, wNode := range valMap {
			w, err := strictIntValue(wNode, fmt.Sprintf("%s.%s.%s", label, col, val))
			if err != nil {
				return nil, err
			}
			if w < 0 {
				return nil, fmt.Errorf("%s.%s.%s must be >= 0", label, col, val)
			}
			vw[val] = w
		}
		out[col] = vw
	}
	return out, nil
}

// strictIntValue reads a whole-number scalar, refusing every other shape.
//
// intValue, the lax decoder, maps a string ("5"), a fractional float (3.7),
// or a list to 0 — and 0 is legal, so `count: "5"` silently generated no
// rows at all while the author asked for five. Same coercion on a weight
// silently zeroed it. This is the count/weights sibling of protectiveBool: a
// lax decoder guessing at a value the author demonstrably tried to set.
// Integral floats decode (mirroring seedValueRejection's toInt64, which
// accepts them, so the generate-time verdict never disagrees with boot).
func strictIntValue(node *coreyaml.Node, label string) (int, error) {
	if node == nil {
		return 0, nil
	}
	if node.Kind != coreyaml.Scalar {
		return 0, fmt.Errorf("%s must be a whole number, got a %s", label, yamlKindName(node.Kind))
	}
	switch v := node.Value.(type) {
	case int64:
		return int(v), nil
	case float64:
		if v != math.Trunc(v) {
			return 0, fmt.Errorf("%s must be a whole number, got %v", label, v)
		}
		return int(v), nil
	default:
		return 0, fmt.Errorf("%s must be a whole number, got %q", label, fmt.Sprint(node.Value))
	}
}

// AppSpecGenerateSeedRows builds n demo rows for an entity. Each enum column
// is filled from a precomputed distribution; scalar columns get plausible
// deterministic values keyed off the row index. System columns (id/timestamps/
// auto-generated/read-only), hidden columns, and relations are skipped.
// Relations can't be safely fabricated, so count-seeding suits scalar/enum
// entities (use explicit rows: for entities with required relations).
func AppSpecGenerateSeedRows(decl framework.EntityDeclaration, n int, weights map[string]map[string]int) []map[string]any {
	// Precompute enum distributions once per column.
	dist := map[string][]string{}
	for _, f := range decl.Fields {
		if strings.EqualFold(f.Type, "enum") && len(f.Values) > 0 {
			dist[f.Name] = appSpecEnumDistribution(decl.Name, f.Name, f.Values, weights[f.Name], n)
		}
	}
	// A key made only of enums takes one combination per row instead,
	// counted from the second: the e2e create sends the first.
	enumKey := appSpecSeedEnumKey(decl)
	for _, f := range enumKey {
		delete(dist, f.Name)
	}
	singular := singularize(ToDisplayName(decl.Name))
	rows := make([]map[string]any, 0, n)
	for r := 0; r < n; r++ {
		row := map[string]any{}
		for i, rest := len(enumKey)-1, r+1; i >= 0; i-- {
			vals := enumKey[i].Values
			row[enumKey[i].Name] = vals[rest%len(vals)]
			rest /= len(vals)
		}
		for _, f := range decl.Fields {
			if AppSpecFieldSystem(f.Name) || f.Hidden || f.ReadOnly || FieldAutoGenerated(f) {
				continue
			}
			switch strings.ToLower(strings.TrimSpace(f.Type)) {
			case "enum":
				if seq := dist[f.Name]; len(seq) == n {
					row[f.Name] = seq[r]
				}
			case "", "string", "text":
				row[f.Name] = appSpecSeedStringValue(f, singular, r)
			case "int", "integer", "bigint":
				// A key column needs one value per row.
				if slices.Contains(decl.PrimaryKey, f.Name) {
					row[f.Name] = appSpecSeedIntKeyStart(f) + r
					continue
				}
				// A gently-skewed spread rather than 1..n so charts summing a
				// numeric column show variation.
				row[f.Name] = 10 + int(appSpecSeedHash(decl.Name+f.Name+strconv.Itoa(r))%90)
			case "float", "double", "decimal", "money", "numeric":
				v := 5 + float64(appSpecSeedHash(decl.Name+f.Name+strconv.Itoa(r))%9950)/10.0
				row[f.Name] = math.Round(v*100) / 100
			case "bool", "boolean":
				// Skew towards true so demos look "active".
				row[f.Name] = appSpecSeedHash(decl.Name+f.Name+strconv.Itoa(r))%4 != 0
			case "uuid":
				// A key column the server does not fill is never null:
				// one value per row, in a block apart from the e2e
				// create's 8000 so the test's insert never collides.
				if slices.Contains(decl.PrimaryKey, f.Name) {
					row[f.Name] = fmt.Sprintf("00000000-0000-4000-9000-%012d", r+1)
				}
			default:
				// date/timestamp/uuid/relation/json: leave to auto-generation
				// or nullable columns.
			}
		}
		rows = append(rows, row)
	}
	return rows
}

// appSpecSeedEnumKey returns the key columns of an entity keyed only on
// enums, in key order, each with its values once; nil when any key column
// is another type. Seed rows for such a key can repeat only once they run
// out of combinations.
func appSpecSeedEnumKey(decl framework.EntityDeclaration) []framework.FieldDeclaration {
	if DeclKeyIsID(decl) {
		return nil
	}
	cols := make([]framework.FieldDeclaration, 0, len(decl.PrimaryKey))
	for _, name := range decl.PrimaryKey {
		i := slices.IndexFunc(decl.Fields, func(f framework.FieldDeclaration) bool { return f.Name == name })
		if i < 0 || !strings.EqualFold(strings.TrimSpace(decl.Fields[i].Type), "enum") || len(decl.Fields[i].Values) == 0 {
			return nil
		}
		f := decl.Fields[i]
		f.Values = nil
		for _, v := range decl.Fields[i].Values {
			if !slices.Contains(f.Values, v) {
				f.Values = append(f.Values, v)
			}
		}
		cols = append(cols, f)
	}
	return cols
}

// appSpecSeedIntKeyStart is the value count seeding gives an integer key
// column's first row: 1001 for an unbounded column, else the lowest value
// min allows above the 1 the e2e create sends.
func appSpecSeedIntKeyStart(f framework.FieldDeclaration) int {
	if f.Min == nil && f.Max == nil {
		return 1001
	}
	if f.Min != nil && *f.Min > 2 {
		return int(math.Ceil(math.Min(*f.Min, 1<<62)))
	}
	return 2
}

// appSpecCheckSeedKeys builds the rows a seed count generates and refuses
// one that would abort startup on its key: a key column count seeding
// cannot fill, an integer key outside the column's bounds, or a key an
// explicit row or an earlier generated row already holds. A key column the
// server fills (auto_generate, or the id the entity injects) never
// repeats, so a key holding one is not checked.
func appSpecCheckSeedKeys(decl framework.EntityDeclaration, seed AppSpecSeedEntity) error {
	keyCols := decl.PrimaryKey
	if DeclKeyIsID(decl) {
		keyCols = []string{"id"}
	}
	cols := make([]framework.FieldDeclaration, 0, len(keyCols))
	for _, c := range keyCols {
		i := slices.IndexFunc(decl.Fields, func(f framework.FieldDeclaration) bool { return f.Name == c })
		if i < 0 || FieldAutoGenerated(decl.Fields[i]) {
			return nil
		}
		cols = append(cols, decl.Fields[i])
	}
	keyOf := func(row map[string]any) (key, missing string) {
		parts := make([]string, len(cols))
		for i, f := range cols {
			v, ok := row[f.Name]
			if !ok || v == nil {
				return "", f.Name
			}
			text := fmt.Sprint(v)
			// The store reads "01001", 1001.0 and 1001 as one integer.
			if appSpecIntType(f.Type) {
				switch n := v.(type) {
				case float64:
					if n == math.Trunc(n) && math.Abs(n) < 1<<63 {
						text = strconv.FormatInt(int64(n), 10)
					}
				case string:
					if i, err := strconv.ParseInt(strings.TrimSpace(n), 10, 64); err == nil {
						text = strconv.FormatInt(i, 10)
					}
				}
			}
			parts[i] = strconv.Quote(text)
		}
		return strings.Join(parts, ", "), ""
	}
	held := map[string]bool{}
	for _, row := range seed.Rows {
		if k, missing := keyOf(row); missing == "" {
			held[k] = true
		}
	}
	for r, row := range AppSpecGenerateSeedRows(decl, seed.Count, seed.Weights) {
		k, missing := keyOf(row)
		if missing != "" {
			return fmt.Errorf("app spec: seed count for entity %q cannot fill the key column %q: every generated row would lack it and abort startup. Seed explicit rows instead, or let the server fill it with auto_generate", seed.Entity, missing)
		}
		if held[k] {
			return fmt.Errorf("app spec: seed count for entity %q generates the key (%s) a seeded row already holds: the repeated insert aborts startup. Change that explicit row's key or lower the count", seed.Entity, k)
		}
		held[k] = true
		for _, f := range cols {
			// The boot validator judges the value, bounds compared the
			// way startup compares them. The generator's ints are the
			// int64 the generated code stores.
			val := row[f.Name]
			switch n := val.(type) {
			case int:
				val = int64(n)
			}
			if msg := seedConstraintRejection(f, val); msg != "" {
				return fmt.Errorf("app spec: seed count %d for entity %q gives key %q the value %v on row %d, which startup refuses (%s), aborting it. Lower the count or widen the bounds", seed.Count, seed.Entity, f.Name, row[f.Name], r+1, msg)
			}
		}
	}
	return nil
}

// appSpecIntType reports whether a declared field type is an integer.
func appSpecIntType(t string) bool {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "int", "integer", "bigint":
		return true
	}
	return false
}

// appSpecSeedStringValue produces a plausible value for a scalar text column
// based on common naming conventions (name/title/email/slug), falling back to a
// humanized "<Singular> <n>".
func appSpecSeedStringValue(f framework.FieldDeclaration, singular string, r int) string {
	name := strings.ToLower(f.Name)
	switch {
	case strings.Contains(name, "email"):
		return fmt.Sprintf("%s%d@example.com", strings.ToLower(strings.ReplaceAll(singular, " ", "")), r+1)
	case strings.Contains(name, "slug"):
		return fmt.Sprintf("%s-%d", strings.ToLower(strings.ReplaceAll(singular, " ", "-")), r+1)
	case strings.Contains(name, "url"), strings.Contains(name, "link"):
		return fmt.Sprintf("https://example.com/%d", r+1)
	case name == "name" || name == "title" || strings.HasSuffix(name, "_name") || strings.HasSuffix(name, "_title"):
		return fmt.Sprintf("%s %d", singular, r+1)
	case strings.EqualFold(f.Type, "text") || strings.Contains(name, "description") || strings.Contains(name, "body") || strings.Contains(name, "notes"):
		return fmt.Sprintf("Sample %s for %s %d.", strings.ReplaceAll(name, "_", " "), strings.ToLower(singular), r+1)
	default:
		return fmt.Sprintf("%s %d", humanizeFieldLabel(f.Name), r+1)
	}
}

// appSpecSeedHash is a small deterministic FNV-1a hash used to derive stable
// pseudo-random-looking (but reproducible) seed values.
func appSpecSeedHash(s string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return h.Sum32()
}

// appSpecEnumDistribution returns a length-n slice of enum values whose
// frequencies follow `weights` when given, otherwise a deterministic skew
// derived from the entity+column+value names. The result is always non-uniform
// for the default (no-weights) path, so demo data never reads as a flat
// round-robin. Values are interleaved (round-robin over the assigned counts) so
// the first page of a list isn't a single value.
func appSpecEnumDistribution(seedKey, col string, values []string, weights map[string]int, n int) []string {
	if len(values) == 0 || n <= 0 {
		return nil
	}
	w := make([]int, len(values))
	total := 0
	explicit := len(weights) > 0
	for i, v := range values {
		if explicit {
			w[i] = weights[v]
		} else {
			w[i] = 1 + int(appSpecSeedHash(seedKey+"|"+col+"|"+v)%7) // 1..7
		}
		total += w[i]
	}
	if total == 0 {
		for i := range w {
			w[i] = 1
		}
		total = len(w)
	}
	// Guard the default skew against a hash collision leaving every weight
	// equal (which would round-trip to a uniform split).
	if !explicit && len(w) > 1 {
		allEqual := true
		for i := 1; i < len(w); i++ {
			if w[i] != w[0] {
				allEqual = false
				break
			}
		}
		if allEqual {
			w[0]++
			total++
		}
	}
	// Largest-remainder apportionment: exact integer counts summing to n.
	counts := make([]int, len(values))
	type rem struct {
		idx  int
		frac float64
	}
	rems := make([]rem, len(values))
	assigned := 0
	for i := range values {
		q := float64(n) * float64(w[i]) / float64(total)
		counts[i] = int(math.Floor(q))
		rems[i] = rem{i, q - math.Floor(q)}
		assigned += counts[i]
	}
	sort.SliceStable(rems, func(a, b int) bool { return rems[a].frac > rems[b].frac })
	for k := 0; assigned < n && len(rems) > 0; k++ {
		counts[rems[k%len(rems)].idx]++
		assigned++
	}
	// Interleave for readability.
	out := make([]string, 0, n)
	remaining := make([]int, len(counts))
	copy(remaining, counts)
	for len(out) < n {
		progressed := false
		for i := range values {
			if remaining[i] > 0 {
				out = append(out, values[i])
				remaining[i]--
				progressed = true
				if len(out) == n {
					break
				}
			}
		}
		if !progressed {
			break
		}
	}
	return out
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
		allowed := map[string]bool{"type": true, "kind": true, "text": true, "level": true, "class": true, "href": true, "entity": true, "fields": true, "limit": true, "empty_text": true, "mode": true, "create": true, "bulk": true, "props": true, "children": true, "actions": true, "island": true, "widget": true}
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
		out = append(out, AppSpecBlock{
			Type:      stringValue(m["type"]),
			Kind:      stringValue(m["kind"]),
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
			Props:     mapValue(m["props"]),
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
	// entityui links a record at <base>/<id> with <base> the list's route,
	// so a detail screen must sit exactly at <list route>/{id} when the
	// entity also has a list screen. Anything else strands the links.
	errs.add(validateDetailRoutes(bp))
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
	for _, decl := range bp.Entities {
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
		// Field names and enum values are transcribed into emitted Go,
		// SQL identifiers and CSS. Nothing downstream re-escapes them,
		// so they are validated here, at the generate-time boundary,
		// where the error can name the offending field.
		for _, f := range decl.Fields {
			if !project.IsGoIdentifier(ToCamelCase(f.Name)) {
				errs.add(fmt.Errorf("app spec: entity %q field %q does not produce a valid Go identifier: rename it to letters, digits and underscores starting with a letter", decl.Name, f.Name))
			} else if !startsLower(f.Name) {
				errs.add(fmt.Errorf("app spec: entity %q field %q must start with a lower-case letter: %s", decl.Name, f.Name, wireCaseWhy))
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
		if _, err := decl.Config(); err != nil {
			errs.add(fmt.Errorf("app spec: entity %q: %w", decl.Name, err))
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
			if !startsLower(rel.Name) {
				errs.add(fmt.Errorf("app spec: entity %q relation %q must start with a lower-case letter: %s", decl.Name, rel.Name, wireCaseWhy))
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
		if routes[screen.Route] {
			errs.add(fmt.Errorf("app spec: duplicate screen route %q", screen.Route))
			continue
		}
		routes[screen.Route] = true
		// The router tries dynamic screens in registration order, so
		// /posts/{id} and /posts/{slug} (or /posts/:id) match the same
		// URLs and the second never renders.
		shape := routeShape(screen.Route)
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

// validateAppSpecSeedTypes rejects seed values whose YAML type can never
// satisfy the target field's runtime validator (core/schema/validate.go).
//
// Without this check, an app spec like the blog's, where comments.post_id is a
// relation (a UUID string column) seeded with `post_id: 1`, sails through
// generate, compiles, and then the app DIES at boot with
// "seed comments: validation failed": the least discoverable point in the
// pipeline, after the author has done everything the tool told them to.
// Every generated entity has a UUID string primary key, so a numeric
// literal on a relation column can never work; the fix the author needs is
// the "@entity.field=value" reference form resolveSeedRefs resolves at
// boot. The check mirrors the runtime's coercion rules exactly (a string
// "42" on an int column is fine, a fractional float is not) so it never
// rejects a seed that would have booted.
func validateAppSpecSeedTypes(bp AppSpec, entitiesByName map[string]framework.EntityDeclaration) error {
	var errs schemaErrors
	for _, seed := range bp.Seed {
		decl, known := entitiesByName[seed.Entity]
		if !known {
			// A seed for an undeclared entity fails at boot with
			// "no handler"; same fail-late shape, catch it here too.
			errs.add(fmt.Errorf("app spec: seed targets unknown entity %q: declare an entity named %q under entities: (or fix the seed's entity: value)", seed.Entity, seed.Entity))
			continue
		}
		for i, row := range seed.Rows {
			// Rows are maps: iterate their keys sorted so a row with two
			// bad values reports both, in a stable order, instead of one
			// picked by map-iteration luck.
			for _, name := range slices.Sorted(maps.Keys(row)) {
				value := row[name]
				field, fieldKind := appSpecSeedFieldKind(decl, name)
				if !fieldKind {
					continue // not a declared column: not this check's business
				}
				reason := seedValueRejection(field, value)
				if reason == "" {
					continue
				}
				if strings.EqualFold(strings.TrimSpace(field.Type), "relation") && strings.TrimSpace(field.To) != "" {
					if target, ok := entitiesByName[strings.TrimSpace(field.To)]; ok {
						errs.add(fmt.Errorf("app spec: seed row %d for entity %q sets %q to %v: a relation column holds the target row's id, and every generated entity's id is a UUID string, so a number can never satisfy it; reference a row seeded earlier in the same pass instead: %q", i+1, seed.Entity, name, value, "@"+strings.TrimSpace(field.To)+"."+appSpecDisplayField(target)+"=<value>"))
						continue
					}
				}
				errs.add(fmt.Errorf("app spec: seed row %d for entity %q sets %q (type %s) to %v: the generated app would reject it at boot: %s", i+1, seed.Entity, name, field.Type, value, reason))
			}
		}
	}
	return errs.err()
}

// ValidateAppSpecSeedStructure holds the seed checks that are about
// WHICH rows run and in what order, not about value types (those live in
// validateAppSpecSeedTypes). Every check here exists because the
// generated seed hook is fail-fast at boot: a block the hook silently
// skips, a row whose key matches no column, or a reference that cannot
// resolve in declaration order each generate green and then abort (or
// quietly lose data) on every fresh database.
func ValidateAppSpecSeedStructure(bp AppSpec, entitiesByName map[string]framework.EntityDeclaration) error {
	var errs schemaErrors
	seeded := map[string]bool{}
	for _, seed := range bp.Seed {
		decl, known := entitiesByName[seed.Entity]
		if seeded[seed.Entity] {
			// The generated hook gates each block on CountAll(entity) > 0:
			// the first block inserts, the second is skipped wholesale, and
			// rows the author wrote never land. Directory merges concatenate
			// seed blocks before validating, so this also catches the same
			// duplicate arriving as two files.
			errs.add(fmt.Errorf("app spec: duplicate seed blocks for entity %q: the generated seed hook skips later blocks once the entity has rows, so only the first block's rows would land; merge them into one block", seed.Entity))
		}
		seeded[seed.Entity] = true
		if !known {
			continue // validateAppSpecSeedTypes names the unknown entity
		}
		// Count-seeding fabricates scalars but never relations
		// (appSpecGenerateSeedRows skips them), so a required relation
		// leaves every fabricated row failing CreateOne's required check —
		// and a failed seed row aborts startup.
		if seed.Count > 0 {
			keyCols := AppSpecKeyColNames(decl)
			fits := true
			for _, f := range decl.Fields {
				isKey := slices.Contains(keyCols, f.Name)
				if isKey && strings.EqualFold(strings.TrimSpace(f.Type), "relation") {
					fits = false
				}
				if strings.EqualFold(strings.TrimSpace(f.Type), "relation") && (f.Required || isKey) {
					errs.add(fmt.Errorf("app spec: seed count for entity %q cannot fabricate the required relation %q: every generated row would lack it and fail CreateOne's required check, aborting startup. Seed explicit rows instead (reference a row seeded earlier with %q)", seed.Entity, f.Name, "@"+strings.TrimSpace(f.To)+"."+appSpecSeedRefFieldHint(entitiesByName, f.To)+"=<value>"))
				}
			}
			// An enum-only key has a fixed number of combinations, and
			// the e2e create takes the first: a row past the rest
			// repeats a key, and the failed insert aborts startup.
			if key := appSpecSeedEnumKey(decl); key != nil {
				combos, enough := 1, false
				for _, f := range key {
					if combos > seed.Count/len(f.Values) {
						enough = true // more combinations than rows
						break
					}
					combos *= len(f.Values)
				}
				if !enough && seed.Count > combos-1 {
					fits = false
					errs.add(fmt.Errorf("app spec: seed count %d for entity %q exceeds its key: (%s) has %d values, one of them kept for the generated e2e test, so a generated row would repeat a key and abort startup. Lower the count to %d or seed explicit rows", seed.Count, seed.Entity, strings.Join(decl.PrimaryKey, ", "), combos, combos-1))
				}
			}
			if fits {
				if err := appSpecCheckSeedKeys(decl, seed); err != nil {
					errs.add(err)
				}
			}
		}
		for i, row := range seed.Rows {
			for _, name := range slices.Sorted(maps.Keys(row)) {
				// A key that matches no declared column is silently dropped
				// by CreateOne (it writes declared columns only), and when
				// the real column is required the boot error names the
				// missing column, never the typo the author wrote. Every
				// other transcribed name in the app spec gets an
				// unknown-target check; the seed row key is the same class.
				if _, ok := appSpecSeedFieldKind(decl, name); !ok {
					errs.add(fmt.Errorf("app spec: seed row %d for entity %q sets %q, which matches no declared column: the value would be silently dropped at boot. Fix the key, or declare the column on the entity", i+1, seed.Entity, name))
					continue
				}
				// "@entity.field=value" references resolve at boot, in
				// block order. A forward reference stays unresolved, fails
				// UUID validation on the relation column, and aborts every
				// fresh database. Only app spec entities are checkable
				// here: a target registered by app code is outside the
				// generator's knowledge.
				if ref, ok := appSpecSeedRefTarget(fmt.Sprint(row[name])); ok {
					if _, isAppSpecEntity := entitiesByName[ref]; isAppSpecEntity && !seeded[ref] {
						errs.add(fmt.Errorf("app spec: seed row %d for entity %q references %q, but %s is seeded later (or never): resolveSeedRefs runs at boot in block order and leaves unresolvable references as-is, so the shipped app aborts on a fresh database. Seed %s first, or in the same block", i+1, seed.Entity, fmt.Sprint(row[name]), ref, ref))
					}
				}
			}
		}
	}
	return errs.err()
}

// appSpecSeedRefTarget extracts the entity named by a
// "@entity.field=value" seed reference (the form resolveSeedRefs rewrites
// at boot), mirroring its exact grammar: leading @, a dot at index >= 1,
// and an = after the dot. ok is false for any other string.
func appSpecSeedRefTarget(v string) (entity string, ok bool) {
	if !strings.HasPrefix(v, "@") {
		return "", false
	}
	rest := v[1:]
	dot := strings.IndexByte(rest, '.')
	eq := strings.IndexByte(rest, '=')
	if dot < 1 || eq <= dot+1 {
		return "", false
	}
	return rest[:dot], true
}

// appSpecSeedRefFieldHint names the target entity's display field for
// the "@entity.field=value" example in refusal messages, mirroring the
// hint validateAppSpecSeedTypes already gives.
func appSpecSeedRefFieldHint(entitiesByName map[string]framework.EntityDeclaration, target string) string {
	if decl, ok := entitiesByName[strings.TrimSpace(target)]; ok {
		return appSpecDisplayField(decl)
	}
	return "id"
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

// appSpecSeedFieldKind resolves a seed row key to the field declaration
// it targets. Synthesized system columns (the UUID id) get a synthesized
// declaration. The second return is false for keys that match no column.
func appSpecSeedFieldKind(decl framework.EntityDeclaration, name string) (framework.FieldDeclaration, bool) {
	for _, f := range decl.Fields {
		if f.Name == name {
			return f, true
		}
	}
	if name == "id" {
		// Every generated model carries `ID string` (renderEntityModel);
		// seeding it directly is legal but takes a UUID string.
		return framework.FieldDeclaration{Name: "id", Type: "uuid"}, true
	}
	return framework.FieldDeclaration{}, false
}

// seedValueRejection returns "" when value can satisfy the field's runtime
// validator, else the message that validator would produce. Mirrors the
// per-type validators in core/schema/validate.go, including their
// coercions (toString accepts only strings; toInt64 accepts integral
// floats and integer-parsable strings; toFloat64 accepts no strings),
// so the generate-time verdict never disagrees with boot.
func seedValueRejection(field framework.FieldDeclaration, value any) string {
	if value == nil {
		return "" // absent value; required-ness fails at boot with field detail
	}
	switch strings.ToLower(strings.TrimSpace(field.Type)) {
	case "relation", "uuid":
		if _, ok := value.(string); !ok {
			return "must be a string reference"
		}
	case "string", "text", "enum", "timestamp", "date", "image", "file":
		s, ok := value.(string)
		if !ok {
			return "must be a string"
		}
		// An enum's Values list is closed; a string outside it can never
		// satisfy the validator either.
		if strings.EqualFold(strings.TrimSpace(field.Type), "enum") && len(field.Values) > 0 {
			for _, allowed := range field.Values {
				if s == allowed {
					return ""
				}
			}
			return fmt.Sprintf("must be one of %v", field.Values)
		}
	case "int", "integer":
		switch v := value.(type) {
		case int64:
		case float64:
			if v != math.Trunc(v) {
				return "must be an integer"
			}
		case string:
			if _, err := strconv.ParseInt(v, 10, 64); err != nil {
				return "must be an integer"
			}
		default:
			return "must be an integer"
		}
	case "float", "number":
		switch value.(type) {
		case int64, float64:
		default:
			return "must be a number"
		}
	case "decimal":
		// Numbers are fine: the stub emitter re-quotes them as the decimal
		// string the validator expects (appSpecSeedFieldIsDecimal).
		switch value.(type) {
		case string, int64, float64:
		default:
			return "must be a decimal string"
		}
	case "bool", "boolean":
		switch v := value.(type) {
		case bool:
		case int64:
			if v != 0 && v != 1 {
				return "must be true or false"
			}
		case float64:
			if v != 0 && v != 1 {
				return "must be true or false"
			}
		case string:
			switch strings.ToLower(v) {
			case "true", "false", "1", "0":
			default:
				return "must be true or false"
			}
		default:
			return "must be true or false"
		}
	case "json":
		// Any value marshals; the validator accepts strings, maps, lists.
	}
	return seedConstraintRejection(field, value)
}

// seedConstraintRejection asks the BOOT validator itself whether the value
// satisfies the field's constraints -- pattern, min/max, enum membership --
// rather than re-deriving them here.
//
// The type checks above are deliberately coercion-tolerant: the stub
// emitter re-encodes an int given as a string, a decimal given as a
// number, and so on, so a shape mismatch there is not a boot failure. The
// CONSTRAINTS are a different matter: nothing re-encodes "abc" into
// ^[A-Z]{3}$, so a row that violates one generates cleanly, compiles, and
// then aborts the shipped app's startup on a fresh database. That is the
// saw-green-ships-red the docstring above promises never happens.
//
// It runs only when the value is already in the form the field will hold,
// so a coercible value is not judged on a shape the generator is about to
// change. A field declaration that cannot be converted is a separate
// error the caller reports elsewhere.
func seedConstraintRejection(field framework.FieldDeclaration, value any) string {
	bootField, err := field.Field()
	if err != nil {
		return ""
	}
	if !seedValueInFinalForm(bootField, value) {
		return ""
	}
	if err := schema.Validate(bootField, value); err != nil {
		return err.Error()
	}
	return ""
}

// seedValueInFinalForm reports whether value's Go type already matches
// what the generated code will store for this field, so the boot
// validator's verdict on it is the verdict the shipped app will reach.
func seedValueInFinalForm(f schema.Field, value any) bool {
	switch f.Type {
	case schema.String, schema.Text, schema.Enum, schema.UUID, schema.Relation:
		_, ok := value.(string)
		return ok
	case schema.Int:
		switch v := value.(type) {
		case int64:
			return true
		case float64:
			return v == math.Trunc(v)
		}
		return false
	case schema.Float:
		switch value.(type) {
		case int64, float64:
			return true
		}
		return false
	case schema.Bool:
		_, ok := value.(bool)
		return ok
	default:
		// Date, Timestamp, Decimal, JSON, Image, File. Every one of these
		// is re-encoded by the stub emitter before it reaches the shipped
		// app -- a decimal given as a number becomes the decimal string
		// the validator wants, a JSON value is marshalled -- so the value
		// here is not yet in the form the field will hold, and the boot
		// validator's verdict on it would be a verdict on a shape that is
		// about to change. Judging them is what would produce a FALSE
		// rejection of a working app spec.
		return false
	}
}

func validateAppSpecBlock(screenName string, entities map[string]framework.EntityDeclaration, block AppSpecBlock) error {
	kind := block.Kind
	if kind == "" {
		kind = block.Type
	}
	if block.Bulk && strings.ToLower(strings.TrimSpace(kind)) != "entity_list" {
		return fmt.Errorf("app spec: screen %q bulk applies only to an entity_list block, not %q", screenName, kind)
	}
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "", "text", "p", "paragraph", "section", "div", "article", "main", "header", "footer", "nav", "aside", "span", "strong", "em", "code", "pre", "small", "blockquote", "button", "input", "label", "form", "select", "option", "textarea", "fieldset", "image", "img", "list", "ul", "ol", "li", "table", "thead", "tbody", "tr", "th", "td", "raw":
	case "heading", "h1", "h2", "h3", "h4", "h5", "h6":
		level := block.Level
		if level == 0 {
			level = 1
		}
		if level < 1 || level > 6 {
			return fmt.Errorf("app spec: screen %q heading level must be 1-6", screenName)
		}
	case "link", "a":
		href := block.Href
		if href == "" {
			href, _ = block.Props["href"].(string)
		}
		if href == "" {
			return fmt.Errorf("app spec: screen %q link block href is required", screenName)
		}
		// The renderer swaps an unsafe href for "#"; refuse it here so the
		// author hears about it instead of shipping a dead link.
		if !urlsafe.OK(href, urlsafe.Anchor) {
			return fmt.Errorf("app spec: screen %q link href %q is not a safe link: use an http(s), mailto, fragment or relative URL", screenName, href)
		}
	case "entity_list":
		if block.Entity == "" {
			return fmt.Errorf("app spec: screen %q entity_list block entity is required", screenName)
		}
		decl, ok := entities[block.Entity]
		if !ok {
			return fmt.Errorf("app spec: screen %q entity_list targets unknown entity %q", screenName, block.Entity)
		}
		if !EntityDeclarationCRUDEnabled(decl) {
			return fmt.Errorf("app spec: screen %q entity_list target %q must enable crud", screenName, block.Entity)
		}
		if len(block.Fields) == 0 {
			return fmt.Errorf("app spec: screen %q entity_list block fields are required", screenName)
		}
		validFields := map[string]bool{"id": true}
		for _, field := range decl.Fields {
			validFields[field.Name] = true
			validFields[ToCamelJSON(field.Name)] = true
		}
		for _, field := range block.Fields {
			if !validFields[field] {
				return fmt.Errorf("app spec: screen %q entity_list field %q is not defined on entity %q", screenName, field, block.Entity)
			}
		}
		if block.Limit < 0 {
			return fmt.Errorf("app spec: screen %q entity_list limit must be >= 0", screenName)
		}
		// mode picks how rows are drawn; the list builder accepts exactly
		// these presentations, so refuse the rest here, naming the two.
		switch strings.ToLower(strings.TrimSpace(block.Mode)) {
		case "", "table", "cards":
		default:
			return fmt.Errorf("app spec: screen %q entity_list mode %q is not a presentation; use table or cards", screenName, block.Mode)
		}
	case "entity_form":
		if block.Entity == "" {
			return fmt.Errorf("app spec: screen %q entity_form block entity is required", screenName)
		}
		decl, ok := entities[block.Entity]
		if !ok {
			return fmt.Errorf("app spec: screen %q entity_form targets unknown entity %q", screenName, block.Entity)
		}
		if !EntityDeclarationCRUDEnabled(decl) {
			return fmt.Errorf("app spec: screen %q entity_form target %q must enable crud", screenName, block.Entity)
		}
		mode := strings.ToLower(strings.TrimSpace(block.Mode))
		if mode != "" && mode != "create" && mode != "edit" {
			return fmt.Errorf("app spec: screen %q entity_form mode must be \"create\" or \"edit\"", screenName)
		}
	case "entity_detail":
		if block.Entity == "" {
			return fmt.Errorf("app spec: screen %q entity_detail block entity is required", screenName)
		}
		decl, ok := entities[block.Entity]
		if !ok {
			return fmt.Errorf("app spec: screen %q entity_detail targets unknown entity %q", screenName, block.Entity)
		}
		if !EntityDeclarationCRUDEnabled(decl) {
			return fmt.Errorf("app spec: screen %q entity_detail target %q must enable crud", screenName, block.Entity)
		}
		// The record page draws from the entity's own display: block, so
		// a text: or fields: here would be accepted and then never render.
		if block.Text != "" || len(block.Fields) > 0 {
			return fmt.Errorf("app spec: screen %q entity_detail takes no text or fields: the record page draws the entity's display: block, so name the record with display.title_fields and lay out its fields with display.form", screenName)
		}
	case "entity_create":
		return fmt.Errorf("app spec: screen %q uses entity_create, which is no longer a block kind: the list's create: synthesizes the create screen at <list>/create", screenName)
	case "entity_edit":
		return fmt.Errorf("app spec: screen %q uses entity_edit, which is no longer a block kind: entity_detail's record page holds the edit form", screenName)
	case "login_form", "signup_form":
		// Static HTML auth form posting to the auth battery.
		if v, ok := block.Props["action"]; ok {
			action, isStr := v.(string)
			if !isStr {
				return fmt.Errorf("app spec: screen %q %s action must be a string", screenName, kind)
			}
			// Same predicate the renderer applies (ui.Form runs Action
			// through the html.Form primitive's setURLAttr guard, which
			// degrades an unsafe scheme to a form that goes nowhere).
			// Safe, but silent: the author asked for a form and got one
			// that does not post. Reject here instead, the way every
			// sibling problem in this validator does, so validate and
			// render cannot disagree about what a safe form action is.
			if !urlsafe.OK(action, urlsafe.Anchor) {
				return fmt.Errorf("app spec: screen %q %s action %q is not a safe form action: use an http(s) or root-relative URL", screenName, kind, action)
			}
		}
		// The footer href becomes an anchor on the login/signup screen. The
		// emitter routes it through ui.Link, which degrades an unsafe scheme to
		// a dead link. Safe, but silent: the author asked for a link and got
		// one that goes nowhere. Reject here instead, the way every sibling
		// problem in this validator does. urlsafe.Anchor is the SAME predicate
		// the renderer applies (http(s), relative, fragment, mailto, tel), so
		// validate and render cannot disagree about what is safe.
		for _, key := range []string{"register_href", "login_href"} {
			href, _ := block.Props[key].(string)
			if href == "" {
				continue
			}
			if !urlsafe.OK(href, urlsafe.Anchor) {
				return fmt.Errorf("app spec: screen %q %s %s %q is not a safe link: use an http(s), root-relative, fragment, mailto or tel URL", screenName, kind, key, href)
			}
		}
	case "bar_chart", "pie_chart", "line_chart":
		// Charts render from grouped entity data; without a valid source
		// the block would silently vanish from the page. Reject upfront.
		src, ok := block.Props["source"].(map[string]any)
		if !ok {
			return fmt.Errorf("app spec: screen %q %s requires source: {entity: ..., group_by: ...}", screenName, kind)
		}
		srcEntity, _ := src["entity"].(string)
		groupBy, _ := src["group_by"].(string)
		if srcEntity == "" || groupBy == "" {
			return fmt.Errorf("app spec: screen %q %s source needs both entity and group_by", screenName, kind)
		}
		decl, known := entities[strings.Trim(srcEntity, "/")]
		if !known {
			return fmt.Errorf("app spec: screen %q %s source targets unknown entity %q", screenName, kind, srcEntity)
		}
		if !EntityDeclarationCRUDEnabled(decl) {
			return fmt.Errorf("app spec: screen %q %s source entity %q must enable crud (the chart reads its rows via the CRUD handler)", screenName, kind, srcEntity)
		}
		// group_by is the chart's LABEL, not an aggregate: groupCounts
		// prints each distinct stored value as a bar or slice label. Pointed at a masked column that renders the stored
		// value verbatim on the page while the API masks it: the full value
		// set, not the one-bit oracle the stat_card filter guard below closes.
		// Masking groupCounts instead would collapse every row into a single
		// "****" bucket, so the bar belongs here.
		if err := requireGroupableColumn(screenName, kind, "group_by", groupBy,
			"the chart renders each distinct stored value as a bar or slice label", decl); err != nil {
			return err
		}
	case "stat_card":
		// A stat_card with a source binds to live entity data; without a
		// registered CRUD handler statValue would render a silent "—".
		// Reject an unknown or crud-disabled source upfront. (A static
		// value: stat_card has no source and is fine.)
		if src, ok := block.Props["source"].(map[string]any); ok {
			srcEntity, _ := src["entity"].(string)
			if srcEntity == "" {
				return fmt.Errorf("app spec: screen %q stat_card source needs entity", screenName)
			}
			decl, known := entities[strings.Trim(srcEntity, "/")]
			if !known {
				return fmt.Errorf("app spec: screen %q stat_card source targets unknown entity %q", screenName, srcEntity)
			}
			if !EntityDeclarationCRUDEnabled(decl) {
				return fmt.Errorf("app spec: screen %q stat_card source entity %q must enable crud", screenName, srcEntity)
			}
			// source.filter is split on "=" and handed to CountAll as a raw
			// ParsedFilter, bypassing ParseFilters, the same unvalidated
			// route entity_list facets take. A Hidden or NoQuery column there
			// turns the rendered count into a one-bit oracle, so bar it here,
			// where entity_list's filters: are barred.
			// src["filter"], not block.Props["filter"]: the filter lives in
			// the nested source map, which is what statValue reads. Checking
			// the outer props meant this guard never fired.
			if f, ok := src["filter"].(string); ok && f != "" {
				col := strings.TrimSpace(strings.SplitN(f, "=", 2)[0])
				for i := range decl.Fields {
					if decl.Fields[i].Name != col {
						continue
					}
					if decl.Fields[i].Hidden {
						return fmt.Errorf("app spec: screen %q stat_card filter column %q is hidden (the count would disclose its values)", screenName, col)
					}
					if decl.Fields[i].NoQuery {
						return fmt.Errorf("app spec: screen %q stat_card filter column %q is no_query (the count would match on the stored value)", screenName, col)
					}
					break
				}
			}
			// agg is StatValue's exact spelling: count (or empty) or sum. sum
			// renders the total of a numeric column. At one row that total IS
			// the stored value, so the masked-column bar applies here too.
			agg, _ := src["agg"].(string)
			switch agg {
			case "", "count":
			case "sum":
				f, _ := src["field"].(string)
				if err := requireGroupableColumn(screenName, "stat_card", "source.field", f,
					"the card renders the total of the stored values, which at one row is the value itself", decl); err != nil {
					return err
				}
				if col, _ := appSpecColumn(decl, f); col == nil || !appSpecNumericType(col.Type) {
					return fmt.Errorf("app spec: screen %q stat_card sums source.field %q, which is not an int, float or decimal field", screenName, f)
				}
			default:
				return fmt.Errorf("app spec: screen %q stat_card source agg %q is not count or sum", screenName, agg)
			}
		}
	case "stack", "cluster", "grid", "stat_grid":
		if err := validateAppSpecLayoutBlock(screenName, kind, block.Props); err != nil {
			return err
		}
	case "page_header", "hero", "card", "stat_row",
		"link_button", "callout",
		"markdown", "pricing", "divider":
		// framework/ui catalog blocks. Props are validated leniently (the
		// generator reads only the props each component understands).
	case "action_button", "custom_form", "text_field", "number_field",
		"checkbox", "switch", "select_field", "radio_group", "data_table",
		"item_list", "nav_links":
		if err := validateAppSpecControlBlock(screenName, strings.ToLower(strings.TrimSpace(kind)), block); err != nil {
			return err
		}
	default:
		return fmt.Errorf("app spec: screen %q has unsupported block type %q", screenName, kind)
	}
	// A node-tree block hands its whole subtree to the node renderer, which
	// draws only node kinds: a catalog block anywhere under it would render
	// as an "unknown kind" comment, so it is refused here.
	if !AppSpecCatalogKind(block.Kind) && AppSpecBlockUsesNodeRenderer(block) {
		if inner := appSpecCatalogUnderNode(block.Children); inner != "" {
			return fmt.Errorf("app spec: screen %q %s block cannot hold a %s block: node blocks draw only node kinds; put it in a stack, cluster, card or section", screenName, kind, inner)
		}
	}
	for _, child := range block.Children {
		if err := validateAppSpecBlock(screenName, entities, child); err != nil {
			return err
		}
	}
	return nil
}

// appSpecCatalogUnderNode returns the first catalog kind among blocks the
// node renderer draws. section is a node kind as well as a catalog one, so
// it renders either way.
func appSpecCatalogUnderNode(blocks []AppSpecBlock) string {
	for _, b := range blocks {
		if AppSpecCatalogKind(b.Kind) && !strings.EqualFold(strings.TrimSpace(b.Kind), "section") {
			return b.Kind
		}
		if inner := appSpecCatalogUnderNode(b.Children); inner != "" {
			return inner
		}
	}
	return ""
}

func validateAppSpecLayoutBlock(screenName, kind string, props map[string]any) error {
	allowed := func(value string, values ...string) bool {
		if value == "" {
			return true
		}
		for _, candidate := range values {
			if value == candidate {
				return true
			}
		}
		return false
	}
	stringProp := func(name string) (string, error) {
		value, ok := props[name]
		if !ok {
			return "", nil
		}
		text, ok := value.(string)
		if !ok {
			return "", fmt.Errorf("app spec: screen %q %s prop %q must be a string", screenName, kind, name)
		}
		return strings.ToLower(strings.TrimSpace(text)), nil
	}
	gap, err := stringProp("gap")
	if err != nil {
		return err
	}
	if !allowed(gap, "none", "xs", "sm", "md", "lg", "xl", "2xl") {
		return fmt.Errorf("app spec: screen %q %s gap %q is not a design token", screenName, kind, gap)
	}
	if kind == "stack" || kind == "cluster" {
		align, err := stringProp("align")
		if err != nil {
			return err
		}
		if !allowed(align, "start", "center", "end", "baseline", "stretch") {
			return fmt.Errorf("app spec: screen %q %s align %q is unsupported", screenName, kind, align)
		}
		justify, err := stringProp("justify")
		if err != nil {
			return err
		}
		if !allowed(justify, "start", "center", "end", "between", "around") {
			return fmt.Errorf("app spec: screen %q %s justify %q is unsupported", screenName, kind, justify)
		}
	}
	if kind == "cluster" {
		if value, ok := props["no_wrap"]; ok {
			if _, ok := value.(bool); !ok {
				return fmt.Errorf("app spec: screen %q cluster prop %q must be a boolean", screenName, "no_wrap")
			}
		}
	}
	if kind == "grid" || kind == "stat_grid" {
		if _, err := stringProp("min"); err != nil {
			return err
		}
	}
	return nil
}

func validateAppSpecActions(screenName string, blocks []AppSpecBlock) error {
	names := map[string]bool{}
	var walk func(AppSpecBlock) error
	walk = func(block AppSpecBlock) error {
		events := map[string]bool{}
		for i, action := range block.Actions {
			name := strings.TrimSpace(action.Name)
			event := AppSpecActionEvent(action)
			if name == "" {
				name = event
			}
			if !isGoFastrActionEvent(event) {
				return fmt.Errorf("app spec: screen %q action %q event %q is not supported", screenName, name, event)
			}
			if events[event] {
				return fmt.Errorf("app spec: screen %q duplicate event %q on one block", screenName, event)
			}
			events[event] = true
			if event == "click" && len(block.Actions) > 1 && i != 0 {
				return fmt.Errorf("app spec: screen %q click action must be first when combined with other actions", screenName)
			}
			if names[name] {
				return fmt.Errorf("app spec: screen %q duplicate action %q", screenName, name)
			}
			names[name] = true
			if action.ClientJS == "" {
				return fmt.Errorf("app spec: screen %q action %q client_js is required", screenName, name)
			}
		}
		for _, child := range block.Children {
			if err := walk(child); err != nil {
				return err
			}
		}
		return nil
	}
	for _, block := range blocks {
		if err := walk(block); err != nil {
			return err
		}
	}
	return nil
}

func isGoFastrActionEvent(event string) bool {
	switch strings.ToLower(strings.TrimSpace(event)) {
	case "click", "input", "change", "submit":
		return true
	default:
		return false
	}
}

// AppSpecSynthesizeCRUDScreens appends the create screen that makes an
// app-side list writable: <home list>/create for every entity with an
// entity_list flagged `create: true`, where the home list is the one every
// list's New links to (appSpecEntityListScreen). The synthesized screen
// renders appUI.Create(entity), it inherits the home screen's layout +
// access, and it is not added to nav.
// The record page holds the edit form, so no edit screen is synthesized.
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
	bp.Screens = append(bp.Screens, extra...)
	return bp
}

// AppSpecKeyColNames lists the columns the record's id is made of: the
// declared primary key, or "id" for an entity keyed on id alone (which
// a declared id field, a relation included, fills).
func AppSpecKeyColNames(decl framework.EntityDeclaration) []string {
	if DeclKeyIsID(decl) {
		return []string{"id"}
	}
	return decl.PrimaryKey
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

// appSpecEntityHasSystemColumn reports whether a framework-managed column
// actually exists on THIS entity.
//
// appSpecFieldSystem answers "is this the name of a system column", which is
// the right question for hiding one from a generated form and the wrong one
// for validating a screen against a schema: created_at/updated_at exist only
// under timestamps:, deleted_at only under soft_delete:, tenant_id only under
// multi_tenant:. Treating the name as proof of existence let a screen name a
// column the table does not have. Validation passed and the generated app
// issued `WHERE created_at LIKE …` against a table without one, turning a
// named generate-time error into an anonymous runtime failure.
func appSpecEntityHasSystemColumn(decl framework.EntityDeclaration, name string) bool {
	switch name {
	case "id":
		return true
	case "created_at", "updated_at":
		return decl.Timestamps == nil || *decl.Timestamps
	case "deleted_at":
		return EntityDeclarationScope(decl).SoftDelete
	case "tenant_id":
		return EntityDeclarationScope(decl).MultiTenant
	}
	return false
}

// AppSpecFieldSystem reports auto/system columns hidden from generated UIs.
func AppSpecFieldSystem(name string) bool {
	switch name {
	case "id", "created_at", "updated_at", "deleted_at", "tenant_id":
		return true
	}
	return false
}

// appSpecColumn resolves a column named in a screen block against the
// entity's declaration, accounting for the columns the framework adds that
// never appear in decl.Fields.
//
// Three of those exist, and every one of them was a legal, working screen
// column before any of this validation existed: `id` (always present),
// `created_at` / `updated_at` / `deleted_at` (from timestamps:/soft_delete:),
// and a FK column declared by a top-level relations: block rather than a
// type: relation field. Rejecting them as undefined broke app specs that
// generated fine, and the message asserted something false. The column does
// exist. None of them can carry a hidden or no_query flag of its own, so
// found==nil with ok==true means "exists, unflagged".
func appSpecColumn(decl framework.EntityDeclaration, name string) (found *framework.FieldDeclaration, ok bool) {
	for i := range decl.Fields {
		if decl.Fields[i].Name == name {
			return &decl.Fields[i], true
		}
	}
	if appSpecEntityHasSystemColumn(decl, name) {
		return nil, true
	}
	if _, isRelCol := appSpecEntityRelations(decl)[name]; isRelCol {
		return nil, true
	}
	return nil, false
}

// appSpecNumericType reports a declared field type a sum can total.
func appSpecNumericType(t string) bool {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "int", "integer", "float", "number", "decimal":
		return true
	}
	return false
}

// requireGroupableColumn refuses a screen column whose stored values would be
// rendered or matched on: a chart's group_by label, a summed stat_card field.
// Hidden and no_query are both barred, for the reasons stated at each call
// site.
func requireGroupableColumn(screenName, kind, key, col, why string, decl framework.EntityDeclaration) error {
	found, ok := appSpecColumn(decl, col)
	if !ok {
		return fmt.Errorf("app spec: screen %q %s %s %q is not defined on entity %q", screenName, kind, key, col, decl.Name)
	}
	if found == nil {
		return nil // framework-managed column; carries no flags
	}
	if found.Hidden {
		return fmt.Errorf("app spec: screen %q %s %s %q is hidden (it is not in the projection, so every row would group under the same empty value)", screenName, kind, key, col)
	}
	if found.NoQuery {
		return fmt.Errorf("app spec: screen %q %s %s %q is no_query (%s)", screenName, kind, key, col, why)
	}
	return nil
}

// humanizeFieldLabel turns a column name into a header: "customer_id" →
// "Customer", "due_on" → "Due", "generic_name" → "Generic Name".
func humanizeFieldLabel(name string) string {
	s := strings.TrimSuffix(name, "_id")
	s = strings.TrimSuffix(s, "_on")
	return ToDisplayName(s)
}

// appSpecRelationKeys refuses at load the relation shapes
// entity.CheckRelationKeys refuses when the generated app boots: a
// foreign key needs one column per column of the key it references, in
// key order, and a key of several columns names each one as a declared
// field of the entity holding it. A many_to_many links two one-column
// keys; a composite end goes through a join entity instead.
func appSpecRelationKeys(decl framework.EntityDeclaration, rel framework.Relation, target framework.EntityDeclaration) error {
	keyOf := func(d framework.EntityDeclaration) []string {
		if DeclKeyIsID(d) {
			return []string{"id"}
		}
		return d.PrimaryKey
	}
	holder, ref := decl, target
	switch rel.Type {
	case fwentity.RelManyToMany:
		if len(keyOf(decl)) > 1 || len(keyOf(target)) > 1 {
			return fmt.Errorf("app spec: entity %q relation %q is many_to_many, and a many_to_many links two one-column keys: declare a join entity that belongs to both sides instead", decl.Name, rel.Name)
		}
		return nil
	case fwentity.RelHasOne, fwentity.RelHasMany:
		holder, ref = target, decl
	}
	fks, key := rel.ForeignKey, keyOf(ref)
	if len(fks) == 0 {
		return nil
	}
	if len(fks) != len(key) {
		return fmt.Errorf("app spec: entity %q relation %q names foreign key (%s), and %q is keyed on (%s): name one foreign-key column per key column, in key order", decl.Name, rel.Name, strings.Join(fks, ", "), ref.Name, strings.Join(key, ", "))
	}
	if len(fks) == 1 {
		return nil
	}
	for i, c := range fks {
		if slices.ContainsFunc(fks[:i], func(p string) bool { return strings.EqualFold(p, c) }) {
			return fmt.Errorf("app spec: entity %q relation %q names foreign-key column %q twice", decl.Name, rel.Name, c)
		}
		if !slices.ContainsFunc(holder.Fields, func(f framework.FieldDeclaration) bool { return f.Name == c }) {
			return fmt.Errorf("app spec: entity %q relation %q names foreign-key column %q, which is not a declared field of %q: declare each column of a multi-column foreign key", decl.Name, rel.Name, c, holder.Name)
		}
		if st := holder.States; st != nil && (st.Field == c || slices.ContainsFunc(st.Transitions, func(t fwentity.Transition) bool { return t.Stamp == c })) {
			return fmt.Errorf("app spec: entity %q relation %q names foreign-key column %q, which %q's transitions write: a move would change the reference with no check, so keep state fields out of multi-column foreign keys", decl.Name, rel.Name, c, holder.Name)
		}
	}
	// The migrator refuses an integer column against a text key column
	// and the reverse; refuse it here, before the app is generated.
	for i, c := range fks {
		f, _ := appSpecColumn(holder, c)
		k, _ := appSpecColumn(ref, key[i])
		if f == nil || k == nil {
			continue
		}
		if ft, kt := appSpecTypeFamily(f), appSpecTypeFamily(k); ft != "" && kt != "" && ft != kt {
			return fmt.Errorf("app spec: entity %q relation %q names foreign-key column %q, typed %s, for key column %q of %q, typed %s: type each column like the key column it references", decl.Name, rel.Name, c, f.Type, key[i], ref.Name, k.Type)
		}
	}
	return nil
}

// appSpecTypeFamily groups a declared field by the Postgres column the
// migrator gives it, as its foreign-key type check does: every integer
// width is one family, text of any length (uuid, enum, image and file
// among it) another, and any other type its own. A relation holds its
// target's key, and a type the loader refuses elsewhere has no column,
// so both are "" and the check skips them.
func appSpecTypeFamily(f *framework.FieldDeclaration) string {
	if appSpecIntType(f.Type) {
		return "integer"
	}
	sf, err := f.Field()
	if err != nil || sf.Type == schema.Relation {
		return ""
	}
	t := fwmigrate.SQLType(sf, fwmigrate.DialectPostgres)
	if i := strings.IndexByte(t, '('); i >= 0 {
		t = t[:i]
	}
	switch strings.ToUpper(t) {
	case "SMALLINT", "INTEGER", "INT", "BIGINT", "SERIAL", "BIGSERIAL":
		return "integer"
	case "TEXT", "VARCHAR", "CHAR", "CHARACTER VARYING":
		return "text"
	}
	return strings.ToLower(t)
}

// appSpecEntityRelations maps each belongs_to FK column → target entity.
func appSpecEntityRelations(decl framework.EntityDeclaration) map[string]string {
	out := map[string]string{}
	for _, f := range decl.Fields {
		if strings.EqualFold(f.Type, "relation") && f.To != "" {
			out[f.Name] = f.To
		}
	}
	for _, r := range decl.Relations {
		if r.Type == fwentity.RelManyToOne && r.Entity != "" {
			for _, c := range r.ForeignKey {
				out[c] = r.Entity
			}
		}
	}
	return out
}

// appSpecDisplayField picks the human label column for an entity.
func appSpecDisplayField(decl framework.EntityDeclaration) string {
	for _, pref := range []string{"name", "title", "label", "email", "slug", "number", "code"} {
		for _, f := range decl.Fields {
			if f.Name == pref {
				return pref
			}
		}
	}
	for _, f := range decl.Fields {
		if !AppSpecFieldSystem(f.Name) && (f.Type == "string" || f.Type == "text") {
			return f.Name
		}
	}
	return "id"
}

// singularize is the naive English singularizer for entity display names,
// shared with the admin battery so both spell "Category" the same way.
func singularize(s string) string { return inflect.Singular(s) }

// appSpecBlockHasSource reports whether a block binds to live entity data.
func appSpecBlockHasSource(b AppSpecBlock) bool {
	_, ok := b.Props["source"].(map[string]any)
	return ok
}

// RouteParamNames parses the param names a route declares, accepting
// both supported syntaxes, "{slug}" / "{path...}" / "{id:int}" and
// ":slug" / ":path*" / ":id:int", mirroring the router's
// normalizeRoutePath + segParamName rules.
func RouteParamNames(route string) []string {
	var names []string
	for _, seg := range strings.Split(route, "/") {
		if name, ok := RouteSegParam(seg); ok {
			names = append(names, name)
		}
	}
	return names
}

// routeShape rewrites every param segment of a route to what the router
// matches on: ":" plus its constraint and a catch-all marker, without the
// name. Two routes with one shape match the same URLs.
func routeShape(route string) string {
	parts := strings.Split(route, "/")
	for i, seg := range parts {
		if _, ok := RouteSegParam(seg); !ok {
			continue
		}
		inner := seg[1:]
		if seg[0] == '{' {
			inner = seg[1 : len(seg)-1]
		}
		catchAll := false
		if rest, ok := strings.CutSuffix(inner, "..."); ok {
			inner, catchAll = rest, true
		} else if rest, ok := strings.CutSuffix(inner, "*"); ok {
			inner, catchAll = rest, true
		}
		shape := ":"
		if _, constraint, ok := strings.Cut(inner, ":"); ok {
			shape += ":" + constraint
		}
		if catchAll {
			shape += "*"
		}
		parts[i] = shape
	}
	return strings.Join(parts, "/")
}

// RouteSegParam returns the param a route segment declares, in either
// supported syntax, and whether it declares one.
func RouteSegParam(seg string) (string, bool) {
	var name string
	switch {
	case len(seg) >= 3 && seg[0] == '{' && seg[len(seg)-1] == '}':
		name = strings.TrimSuffix(seg[1:len(seg)-1], "...")
	case strings.HasPrefix(seg, ":") && len(seg) > 1:
		name = strings.TrimSuffix(seg[1:], "*")
	default:
		return "", false
	}
	if i := strings.IndexByte(name, ':'); i >= 0 {
		name = name[:i] // strip a typed constraint suffix
	}
	return name, true
}

// ScreenParamName returns the param key the generated SetParams reads:
// a param literally named "id" when the route declares one (the
// entity-record convention), else the LAST declared param: on a nested
// route like /organizations/{organization_id}/users/{user_id} the final
// segment identifies the record the screen shows. Falls back to "id"
// for entity detail/edit screens whose {id} lives on a synthesized route.
func ScreenParamName(screen AppSpecScreen) string {
	names := RouteParamNames(screen.Route)
	for _, n := range names {
		if n == "id" {
			return n
		}
	}
	if len(names) > 0 {
		return names[len(names)-1]
	}
	return "id"
}

// ScreenRecordKeys returns the key columns of the record a screen shows
// when that record's entity keys on more than one column, else nil. The
// record is the screen's first entity_detail, else its first edit form.
// Such a screen's {id} stands for the whole key: it mounts with one
// segment per key column, the record path entityui links to.
func ScreenRecordKeys(bp AppSpec, screen AppSpecScreen) []string {
	entity := ""
	for _, b := range AppSpecBlocksDeep(screen.Body) {
		if IsEntityDetailBlock(b) {
			entity = strings.Trim(b.Entity, "/")
			break
		}
		if entity == "" && IsEditFormBlock(b) {
			entity = strings.Trim(b.Entity, "/")
		}
	}
	for _, d := range bp.Entities {
		if d.Name == entity && len(d.PrimaryKey) > 1 {
			return d.PrimaryKey
		}
	}
	return nil
}

// ScreenNeedsRouteID reports whether a screen's body tree contains an
// entity_detail block or an entity_form with mode "edit". Both render a
// specific record identified by a route {id} param. The first return is the
// detail case, the second the edit-form case, so the validator can name it.
func ScreenNeedsRouteID(screen AppSpecScreen) (detail, editForm bool) {
	for _, b := range AppSpecBlocksDeep(screen.Body) {
		if IsEntityDetailBlock(b) {
			detail = true
		}
		if IsEditFormBlock(b) {
			editForm = true
		}
	}
	return
}

// IsEditFormBlock reports whether block is an entity_form in edit mode.
func IsEditFormBlock(block AppSpecBlock) bool {
	return IsEntityFormBlock(block) && strings.EqualFold(strings.TrimSpace(block.Mode), "edit")
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

// AppSpecBlocksDeep returns blocks and every block nested under them,
// depth first, in document order. A screen's entity blocks count wherever
// they sit, so every question about them (create synthesis, the drawer,
// the CRUD file, the e2e target, route params) walks this, never Body
// alone.
func AppSpecBlocksDeep(blocks []AppSpecBlock) []AppSpecBlock {
	var out []AppSpecBlock
	for _, b := range blocks {
		out = append(out, b)
		out = append(out, AppSpecBlocksDeep(b.Children)...)
	}
	return out
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

// AppSpecCatalogKind reports whether a block kind maps to a framework/ui
// catalog component (handled by renderAppSpecCatalogBlock).
func AppSpecCatalogKind(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	// The layout blocks (stack/cluster/grid/stat_grid) are catalog
	// compositions too: renderAppSpecCatalogBlock emits them as
	// ui.Stack/Cluster/Grid calls, so a screen whose body is one needs
	// the ui import like any hero or card. Meridian's about screen
	// (a stack of page_header + markdown) is the fixture that exposed
	// the gap: the emitted file referenced ui.Stack with no import.
	case "stack", "cluster", "grid", "stat_grid",
		"page_header", "hero", "section", "card", "stat_row", "stat_card",
		"bar_chart", "pie_chart", "line_chart", "link_button", "callout", "divider",
		"markdown", "pricing":
		return true
	}
	return AppSpecControlKind(kind)
}

func AppSpecBlockUsesNodeRenderer(block AppSpecBlock) bool {
	return block.Kind != "" || len(block.Props) > 0 || len(block.Children) > 0 || len(block.Actions) > 0 || block.Island != "" || block.Widget != ""
}

func AppSpecActionEvent(action AppSpecAction) string {
	event := strings.ToLower(strings.TrimSpace(action.Event))
	if event != "" {
		return event
	}
	return "click"
}

func (s AppSpecScreen) TitleOrName() string {
	if s.Title != "" {
		return s.Title
	}
	return s.Name
}

// AppSpecProp reads a string prop from a block.
func AppSpecProp(b AppSpecBlock, key string) string {
	if b.Props == nil {
		return ""
	}
	s, _ := b.Props[key].(string)
	return s
}

func AppSpecBoolProp(b AppSpecBlock, key string) bool {
	if b.Props == nil {
		return false
	}
	value, _ := b.Props[key].(bool)
	return value
}

func IsEntityListBlock(block AppSpecBlock) bool {
	return AppSpecBlockKindIs(block, "entity_list")
}

func IsEntityFormBlock(block AppSpecBlock) bool {
	return AppSpecBlockKindIs(block, "entity_form")
}

func IsEntityDetailBlock(block AppSpecBlock) bool {
	return AppSpecBlockKindIs(block, "entity_detail")
}

// entity_create is synthesized (never authored) by
// appSpecSynthesizeCRUDScreens for the <list>/create screen. It renders
// the appUI.Create builder. Authored entity_create blocks are refused at
// decode and at validate.
func IsEntityCreateBlock(block AppSpecBlock) bool {
	return AppSpecBlockKindIs(block, "entity_create")
}

func AppSpecBlockKindIs(block AppSpecBlock, want string) bool {
	kind := block.Kind
	if kind == "" {
		kind = block.Type
	}
	return strings.EqualFold(strings.TrimSpace(kind), want)
}

// toDisplayName converts a snake_case field/entity name to a human-readable Title Case display name.
// displayAcronyms are words that read better fully uppercased in a label
// ("mrr" → "MRR", not "Mrr"). Lowercase keys; matched case-insensitively per word.
var displayAcronyms = map[string]bool{
	"id": true, "url": true, "uri": true, "api": true, "mrr": true, "arr": true,
	"ltv": true, "sku": true, "kpi": true, "roi": true, "sla": true, "ui": true,
	"ux": true, "css": true, "html": true, "json": true, "csv": true, "pdf": true,
	"http": true, "https": true, "ip": true, "dns": true, "cdn": true, "sso": true,
	"faq": true, "seo": true, "cta": true, "vat": true, "ein": true, "ssn": true,
}

func ToDisplayName(s string) string {
	s = strings.ReplaceAll(s, "_", " ")
	s = strings.ReplaceAll(s, "-", " ")
	words := strings.Fields(s)
	for i, w := range words {
		if len(w) == 0 {
			continue
		}
		if displayAcronyms[strings.ToLower(w)] {
			words[i] = strings.ToUpper(w)
			continue
		}
		words[i] = strings.ToUpper(w[:1]) + strings.ToLower(w[1:])
	}
	return strings.Join(words, " ")
}

func AppSpecThemeColorPath(key string) (string, bool) {
	switch key {
	case "primary":
		return "Primary", true
	case "primary-fg":
		return "PrimaryFg", true
	case "secondary":
		return "Secondary", true
	case "background":
		return "Background", true
	case "surface":
		return "Surface", true
	case "surface-soft":
		return "SurfaceSoft", true
	case "text":
		return "Text", true
	case "text-muted":
		return "TextMuted", true
	case "text-subtle":
		return "TextSubtle", true
	case "border":
		return "Border", true
	case "border-strong":
		return "BorderStrong", true
	case "accent":
		return "Accent", true
	case "success":
		return "Success", true
	case "warning":
		return "Warning", true
	case "danger":
		return "Danger", true
	case "danger-fg":
		return "DangerFg", true
	case "info":
		return "Info", true
	default:
		return "", false
	}
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

func ScreenTypeConst(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "page":
		return "app.ScreenPage", nil
	case "drawer":
		return "app.ScreenDrawer", nil
	case "sheet":
		return "app.ScreenSheet", nil
	case "dialog", "modal":
		return "app.ScreenDialog", nil
	default:
		return "", fmt.Errorf("app spec: unknown screen type %q", value)
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
	"pwa": true, "llm_md": true, "locale_prefixes": true,
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

func stringValue(node *coreyaml.Node) string {
	if node == nil || node.Kind != coreyaml.Scalar || node.Value == nil {
		return ""
	}
	return fmt.Sprint(node.Value)
}

func boolValue(node *coreyaml.Node) bool {
	if node == nil || node.Kind != coreyaml.Scalar {
		return false
	}
	if v, ok := node.Value.(bool); ok {
		return v
	}
	return strings.EqualFold(fmt.Sprint(node.Value), "true")
}

// protectiveBool decodes a flag whose FALSE value is the PERMISSIVE state, so a
// coerced-to-false read is fail-open. Missing is fine (that is the documented
// default); present-but-unreadable is an error naming the key.
//
// boolValue, the lax decoder, maps anything that is not the bool true or the
// string "true" to false. core/yaml is YAML 1.2, so `yes` / `on` / `y` / `1` are
// STRINGS there, and they are how people, and agents transcribing a spec, most
// often write "true" in YAML. `access: {auth: yes}` therefore decoded to
// Auth=false and registered the screen with no policy at all: publicly
// reachable, no warning, no error.
//
// strictBoolValue already existed for this and was wired to exactly one key
// (app.auth.dev_mode). Its comment justified itself as "the one app spec bool
// whose default is true", and that framing is what let these through. The
// polarity that matters is not what the default is, it is which direction the
// coercion moves safety. Every key routed through here defaults to false, and
// false is the unsafe side.
//
// Deliberately NOT applied to flags whose false value is the safe/inert one
// (`exposure.public`, `exposure.mcp`, `exposure.crud`, `app.public_openapi`,
// `pwa.enabled`, `admin.enabled`, `llm_md`, `timestamps`, `many`, `create`):
// there, a coerced false turns a feature OFF, which is the direction we would
// want anyway. NOTE: `*.enabled` is NOT a blanket exception; app.auth.enabled
// IS routed here: false means the auth subsystem is off and the app is public.
func protectiveBool(m map[string]*coreyaml.Node, key, context string) (bool, error) {
	node, ok := m[key]
	if !ok || node == nil {
		return false, nil
	}
	v, err := strictBoolValue(node)
	if err != nil {
		return false, fmt.Errorf("%s.%s %w: this flag gates access or exposure, so it is not guessed (YAML 1.2 reads yes/on/y/1 as strings, not booleans)", context, key, err)
	}
	return v, nil
}

// strictBoolValue accepts only a genuine bool node (or the literal
// strings "true"/"false") and errors on anything else, for keys where
// lax coercion would silently invert a safe default. See protectiveBool
// for the larger set of keys where the unsafe direction is FALSE.
func strictBoolValue(node *coreyaml.Node) (bool, error) {
	if node != nil && node.Kind == coreyaml.Scalar {
		if v, ok := node.Value.(bool); ok {
			return v, nil
		}
		switch fmt.Sprint(node.Value) {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
	}
	got := "missing"
	line := 0
	if node != nil {
		got = fmt.Sprintf("%q", fmt.Sprint(node.Value))
		line = node.Line
	}
	return false, fmt.Errorf("must be true or false (got %s) at line %d", got, line)
}

func intValue(node *coreyaml.Node) int {
	if node == nil || node.Kind != coreyaml.Scalar {
		return 0
	}
	switch v := node.Value.(type) {
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}

func floatValue(node *coreyaml.Node) float64 {
	if node == nil || node.Kind != coreyaml.Scalar {
		return 0
	}
	switch v := node.Value.(type) {
	case int64:
		return float64(v)
	case float64:
		return v
	default:
		return 0
	}
}

func scalarValue(node *coreyaml.Node) any {
	if node == nil || node.Kind != coreyaml.Scalar {
		return nil
	}
	return node.Value
}

func stringListValue(node *coreyaml.Node) []string {
	if node == nil || node.Kind != coreyaml.List {
		return nil
	}
	out := make([]string, 0, len(node.List))
	for _, item := range node.List {
		out = append(out, stringValue(item))
	}
	return out
}

func mapValue(node *coreyaml.Node) map[string]any {
	if node == nil || node.Kind != coreyaml.Map {
		return nil
	}
	out := make(map[string]any, len(node.Map))
	for key, child := range node.Map {
		out[key] = codegen.AnyValue(child)
	}
	return out
}

// stringStringMapValue decodes a YAML map whose values are all scalars, e.g.
// an entity's `renames:` (old column -> new column).
func stringStringMapValue(node *coreyaml.Node) map[string]string {
	if node == nil || node.Kind != coreyaml.Map {
		return nil
	}
	out := make(map[string]string, len(node.Map))
	for key, child := range node.Map {
		out[key] = stringValue(child)
	}
	return out
}

func appSpecSeedHasComplexValues(bp AppSpec) bool {
	for _, seed := range bp.Seed {
		for _, row := range seed.Rows {
			for _, v := range row {
				switch v.(type) {
				case string, int, int64, float64, bool, nil:
				default:
					return true
				}
			}
		}
	}
	return false
}
