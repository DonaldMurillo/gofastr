package appspec

import (
	"github.com/DonaldMurillo/gofastr/core/settings"
	"github.com/DonaldMurillo/gofastr/framework"
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

	phrases *AppSpecPhrases

	appless bool

	phraseApp string

	projectApp *AppSpecApp

	projectShell *ProjectShell
}

// ProjectShell is what the project an added screen joins declares in its generated app.go for screens to mount through.
type ProjectShell struct {
	AppLayout   bool
	GuestPolicy bool
	LoginRoute  string
	Home        string
}

// SetProjectShell records the shell of the project a fragment joins (generate app --add).
func (bp *AppSpec) SetProjectShell(s ProjectShell) { bp.projectShell = &s }

// ProjectShell returns the shell SetProjectShell recorded, and false when none was.
func (bp *AppSpec) ProjectShell() (s ProjectShell, ok bool) {
	if bp.projectShell == nil {
		return s, false
	}
	return *bp.projectShell, true
}

// PhraseCollector returns the phrase collector of the file being rendered, nil outside a file renderer.
func (bp *AppSpec) PhraseCollector() *AppSpecPhrases { return bp.phrases }

// MarkFragment records that bp is a fragment added to an existing project, so checks that read its app settings skip it.
func (bp *AppSpec) MarkFragment() { bp.appless = true }

// IsFragment reports whether bp was decoded with no app section or marked with MarkFragment.
func (bp *AppSpec) IsFragment() bool { return bp.appless }

// SetProjectApp records the app settings of the project a fragment joins, so its routes are judged against them.
func (bp *AppSpec) SetProjectApp(app AppSpecApp) { bp.projectApp = &app }

// RouteApp is the app whose settings decide which routes the framework mounts: SetProjectApp's, else App.
func (bp *AppSpec) RouteApp() AppSpecApp {
	if bp.projectApp != nil {
		return *bp.projectApp
	}
	return bp.App
}

// SetPhraseApp records the project's app name for a fragment's phrase keys; empty changes nothing.
func (bp *AppSpec) SetPhraseApp(name string) { bp.phraseApp = name }

// PhraseApp is the app name phrase keys are namespaced under: SetPhraseApp's, else App.Name.
func (bp *AppSpec) PhraseApp() string {
	if bp.phraseApp != "" {
		return bp.phraseApp
	}
	return bp.App.Name
}

// SetPhraseCollector installs the phrase collector a file renderer emits through, on its own copy of the spec.
func (bp *AppSpec) SetPhraseCollector(p *AppSpecPhrases) { bp.phrases = p }

// AppSpecSeedEntity holds seed data for one entity.
type AppSpecSeedEntity struct {
	Entity string
	Rows   []map[string]any
	// Count, when > 0, generates that many demo rows besides Rows.
	Count int
	// Weights maps an enum column to a value to weight distribution for generated rows.
	Weights map[string]map[string]int
	// Authored is how many leading Rows the author wrote; set by the generator, never an app spec key.
	Authored int
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
	// Description is the site's meta description (app.description); empty derives one.
	Description string
	// BaseURL is the generated appBaseURL fallback (app.base_url); APP_BASE_URL wins at run time.
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
	// LLMMD serves every screen's /llm.md and /llm-pages.md (app.llm_md), never implied by another setting.
	LLMMD bool
	// EntityLLMMD (app.entity_llm_md) is emitted as the inverse NoLLMMD; nil leaves entity llm.md on.
	EntityLLMMD *bool
	// LocalePrefixes serves each locales/<tag>.json under /<tag>/ (app.locale_prefixes).
	LocalePrefixes bool
}

// AppSpecPWA configures the generated app's installable PWA (app.pwa); unset fields take the framework's defaults.
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
	Filter    string // entity_list: a pinned `field=value` equality (stat_card's filter spelling), emitted as ListBuilder.Where
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

// AppSpecHook is an entity lifecycle hook the generated app implements in owned Go.
type AppSpecHook struct {
	// ID is the hook's id in the kiln world and its name in the spec.
	ID     string
	Entity string
	// When is the lifecycle point, a key of AppSpecHookTypes.
	When string
	// Handler is the owned-Go func the generated app calls.
	Handler string
	// Description names the declarative action the hook came from.
	Description string
}

type AppSpecNamedStub struct {
	Name        string
	Description string
}
