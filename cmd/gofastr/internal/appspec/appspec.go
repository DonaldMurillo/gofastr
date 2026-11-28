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

	// phraseApp is the app name a fragment's phrase keys take, read from
	// the project the fragment joins: a fragment's App.Name is empty, and
	// keys under the fallback "app" namespace would differ from the rest
	// of the project's. Unexported for the reason phrases is; App.Name
	// itself stays empty, since it decides whether app.go is emitted.
	phraseApp string

	// projectApp holds the route-owning settings of the project a
	// fragment joins (SetProjectApp), nil when none were read. Unexported
	// for the reason phrases is: App itself stays empty, since it decides
	// what the fragment emits.
	projectApp *AppSpecApp

	// projectShell holds what the generated app.go of the project a
	// fragment joins declares for screens to mount through
	// (SetProjectShell), nil when none was read. Unexported for the
	// reason phrases is.
	projectShell *ProjectShell
}

// ProjectShell is what an added screen mounts through in the project it
// joins, read off the project's generated source: whether app.go
// declares appLayout and guestPolicy, the login route its authPolicy
// calls redirect to and the home its guestPolicy calls send a signed-in
// visitor to ("" when no call names one).
type ProjectShell struct {
	AppLayout   bool
	GuestPolicy bool
	LoginRoute  string
	Home        string
}

// SetProjectShell records the shell of the project a fragment joins
// (generate app --add). A fragment has no nav and only its own screens,
// so without it an added screen would drop the app layout, the guest
// redirect and the project's login route.
func (bp *AppSpec) SetProjectShell(s ProjectShell) { bp.projectShell = &s }

// ProjectShell returns the shell SetProjectShell recorded; ok is false
// when none was, and the spec renders against its own nav and screens.
func (bp *AppSpec) ProjectShell() (s ProjectShell, ok bool) {
	if bp.projectShell == nil {
		return s, false
	}
	return *bp.projectShell, true
}

// PhraseCollector returns the phrase collector of the file being
// rendered, nil outside a file renderer.
func (bp *AppSpec) PhraseCollector() *AppSpecPhrases { return bp.phrases }

// MarkFragment records that bp is a fragment added to an existing
// project (generate app --add, generate entity|screen): its App does not
// carry the project's settings, so validation skips the checks that read
// them.
func (bp *AppSpec) MarkFragment() { bp.appless = true }

// IsFragment reports whether bp was decoded with no app: section (or
// marked with MarkFragment). A serializer writes no app: section for one,
// so the fragment stays a fragment on the next decode.
func (bp *AppSpec) IsFragment() bool { return bp.appless }

// SetProjectApp records the app settings of the project a fragment
// joins (generate app --add), read off its generated source. Validation
// judges the fragment's screen routes against them: a fragment's own App
// is empty, so a screen at the project's admin path, PWA files, llm.md
// pages, auth routes or entity API would otherwise pass and never
// render.
func (bp *AppSpec) SetProjectApp(app AppSpecApp) { bp.projectApp = &app }

// RouteApp is the app whose settings decide which routes the framework
// mounts: the project's, recorded by SetProjectApp, else App.
func (bp *AppSpec) RouteApp() AppSpecApp {
	if bp.projectApp != nil {
		return *bp.projectApp
	}
	return bp.App
}

// SetPhraseApp records the project's app name for a fragment's phrase
// keys (generate app --add). An empty name changes nothing.
func (bp *AppSpec) SetPhraseApp(name string) { bp.phraseApp = name }

// PhraseApp is the app name phrase keys are namespaced under: the
// project's name recorded by SetPhraseApp, else App.Name.
func (bp *AppSpec) PhraseApp() string {
	if bp.phraseApp != "" {
		return bp.phraseApp
	}
	return bp.App.Name
}

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
	// Authored is the number of leading Rows the author wrote; the
	// generator sets it when it expands Count (ExpandAppSpecSeed) so the
	// stub emitter and pack know where the authored rows end. It is not an
	// app-spec key, and pack never writes it back: a packed spec carries
	// the recipe (count/weights) plus the authored rows only.
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
	// EntityLLMMD (app.entity_llm_md) emits AppConfig.NoLLMMD as its
	// inverse when set; nil leaves entity llm.md routes on.
	EntityLLMMD *bool
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
