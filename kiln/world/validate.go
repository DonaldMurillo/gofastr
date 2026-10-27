package world

import (
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// The checks in this file are the shared half of a parity contract: the
// live tool API (kiln/protocol) and journal replay (kiln/journal) must
// accept exactly the same world states. They had drifted -- every rule
// below existed only on the live path, so a hand-authored
// .kiln.session.jsonl installed world state the API refuses, and the
// running server booted a world `kiln freeze` then rejects. Keeping the
// rules here, below both callers, is what stops them drifting again.
//
// One deliberate gap: replay keeps the shared action-kind set and the
// released page-path rule, because released builds journaled routes and
// hooks with kinds their surface cannot run, and relative page paths,
// and a session holding one must still load. The live tool refuses new
// ones (ValidateHook, ValidateRouteAction, ValidatePagePath).

// ValidatePageTree rejects styling and event-handler props anywhere in a
// page's element tree.
//
// Kiln pages compose the design system; a class, style, or on* prop is
// either bespoke CSS smuggled into the IR or inline script. The ban was
// written three times (protocol's validatePageTree, freeze's
// validateNodeGraduation, and nowhere at all in replay), which is how
// replay ended up without it.
func ValidatePageTree(n Node) error {
	for key := range n.Props {
		normalized := strings.ToLower(strings.TrimSpace(key))
		if normalized == "class" || normalized == "style" || strings.HasPrefix(normalized, "on") {
			return fmt.Errorf("node kind %q uses forbidden styling or handler prop %q", n.Kind, key)
		}
	}
	for _, child := range n.Children {
		if err := ValidatePageTree(child); err != nil {
			return err
		}
	}
	return nil
}

// NormalizeAPIPrefix trims slashes off api_prefix and defaults it to
// "api", in place. It applies to EVERY app-config write, however the
// write arrived.
//
// The prefix is load-bearing rather than cosmetic. It decides where
// entity CRUD mounts and therefore which page paths collide with it, so
// a replayed "/api/" or "" left verbatim produces a different app from
// the one the same input would have produced through the API -- an empty
// prefix silently moves every entity to a bare-root mount.
func NormalizeAPIPrefix(c *AppConfig) {
	if c == nil {
		return
	}
	c.APIPrefix = strings.Trim(c.APIPrefix, "/")
	if c.APIPrefix == "" {
		c.APIPrefix = "api"
	}
}

// ValidateAppConfig normalizes the prefix and additionally requires a
// name. It is the rule for a write that SETS the configuration, which is
// what the live set_app_config tool refuses without a name.
//
// It deliberately does not cover every app-config write. Derived edits
// (set_theme) re-emit the world's existing config with one field
// changed, and a fresh world has no name yet, so demanding one there
// would refuse an edit the live API produces. Those carry a Prev
// snapshot; see the replay caller for how the two are told apart, and
// for the separate rule that a derived edit may not blank the name.
func ValidateAppConfig(c *AppConfig) error {
	if c == nil {
		return fmt.Errorf("app config: nil")
	}
	if strings.TrimSpace(c.Name) == "" {
		return fmt.Errorf("app config: name is required")
	}
	NormalizeAPIPrefix(c)
	return nil
}

// ValidateScaffold checks the shape of the nav/endpoint/stub surfaces:
// nav items need a label and an href, endpoints a method and a path, and
// every named stub a name. A stub with no name generates nothing and
// names nothing, so it is a typo rather than a declaration.
func ValidateScaffold(nav []NavItem, endpoints []*EndpointStub, stubGroups map[string][]NamedStub) error {
	if err := validateNav(nav, ""); err != nil {
		return err
	}
	for i, ep := range endpoints {
		if ep == nil {
			return fmt.Errorf("endpoints[%d]: null entry", i)
		}
		if strings.TrimSpace(ep.Method) == "" {
			return fmt.Errorf("endpoints[%d] (%q): method is required", i, ep.Name)
		}
		if strings.TrimSpace(ep.Path) == "" {
			return fmt.Errorf("endpoints[%d] (%q): path is required", i, ep.Name)
		}
	}
	for _, label := range slices.Sorted(maps.Keys(stubGroups)) {
		for i, s := range stubGroups[label] {
			if strings.TrimSpace(s.Name) == "" {
				return fmt.Errorf("%s[%d]: name is required", label, i)
			}
		}
	}
	return nil
}

// validateNav walks nav items and their children, reporting the path to
// an offending entry so a deep tree is diagnosable.
func validateNav(items []NavItem, prefix string) error {
	for i, it := range items {
		where := fmt.Sprintf("%snav[%d]", prefix, i)
		if strings.TrimSpace(it.Label) == "" {
			return fmt.Errorf("%s: label is required", where)
		}
		if strings.TrimSpace(it.Href) == "" && len(it.Items) == 0 {
			return fmt.Errorf("%s (%q): href is required", where, it.Label)
		}
		if err := validateNav(it.Items, where+"."); err != nil {
			return err
		}
	}
	return nil
}

// routeMethods is the method enum the add_route descriptor advertises.
// net/http matches methods case-sensitively and every real client sends
// them uppercase, so a lowercase method registers on the mux and then
// never matches anything: the tool confirms and journals a route that
// cannot fire. Registering is not the same as working, which is why this
// is checked rather than left to the mux.
var routeMethods = []string{"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS"}

// ValidateRoute checks a custom route's shape before anything tries to
// register it. The method must be one of the advertised verbs, spelled
// exactly, and the path must be rooted: net/http's pattern parser PANICS
// on a path with no leading slash, and a panic during rebuild is not a
// refusal — it takes the caller with it.
func ValidateRoute(r *Route) error {
	if r == nil {
		return fmt.Errorf("route: nil")
	}
	if !slices.Contains(routeMethods, r.Method) {
		return fmt.Errorf("route %q %q: method must be one of %s (uppercase; net/http matches case-sensitively)",
			r.Method, r.Path, strings.Join(routeMethods, ", "))
	}
	if !strings.HasPrefix(r.Path, "/") {
		return fmt.Errorf("route %q %q: path must start with /", r.Method, r.Path)
	}
	if err := routePatternErr(r.Method + " " + r.Path); err != nil {
		return fmt.Errorf("route %q %q: %v", r.Method, r.Path, err)
	}
	return nil
}

// routePatternErr parses pattern with net/http's mux, the parser the
// rebuild registers routes through. It panics on a bad pattern
// ("/x/{oops"), so the panic is turned into the refusal here.
func routePatternErr(pattern string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%v", r)
		}
	}()
	http.NewServeMux().HandleFunc(pattern, func(http.ResponseWriter, *http.Request) {})
	return nil
}

// OverlappingPattern returns the first of existing that pattern cannot
// register beside on net/http's mux: an exact duplicate, or a pattern
// that matches some of the same paths with neither more specific
// ("GET /x/{id}" and "GET /{name}/y"). Each is valid alone, so
// ValidateRoute passes both, and the rebuild then panics registering the
// second. "" means pattern registers beside every one of them. The mux's
// own panic text names source files, so the caller words the refusal
// from the returned pattern instead.
func OverlappingPattern(existing []string, pattern string) string {
	for _, p := range existing {
		if patternsConflict(p, pattern) {
			return p
		}
	}
	return ""
}

func patternsConflict(a, b string) (conflict bool) {
	h := func(http.ResponseWriter, *http.Request) {}
	mux := http.NewServeMux()
	if routePatternErr(a) != nil {
		return false
	}
	mux.HandleFunc(a, h)
	defer func() {
		if recover() != nil {
			conflict = true
		}
	}()
	mux.HandleFunc(b, h)
	return false
}

// HookEvents is the closed set of hook `when` values, the ones
// kiln/render's mapHookType binds to a framework hook type. Anything else
// fails the rebuild, so it is refused before the hook enters the world.
var HookEvents = []string{
	"before_create", "after_create", "before_update", "after_update",
	"before_delete", "after_delete", "before_list", "after_list",
}

// ValidateHook checks a hook the way the render step will need it: an
// id, an entity to attach to, a known event, and an action kind a hook
// can run. The live tool and replay both call it, so a hook either side
// accepts is one the rebuild can register.
func ValidateHook(h *Hook) error {
	if h == nil || h.ID == "" {
		return fmt.Errorf("hook: missing id")
	}
	if h.Entity == "" {
		return fmt.Errorf("hook %q: missing entity", h.ID)
	}
	if !slices.Contains(HookEvents, h.When) {
		return fmt.Errorf("hook %q: unknown hook event %q (want one of %s)", h.ID, h.When, strings.Join(HookEvents, ", "))
	}
	if err := ValidateHookAction(h.Action); err != nil {
		return fmt.Errorf("hook %q: %w", h.ID, err)
	}
	return nil
}

// ValidatePagePath refuses a page path the screen router cannot serve.
// A relative path ("about") registers, but no request path matches it, so
// the tool confirmed a page no browser can reach.
func ValidatePagePath(path string) error {
	if path == "" {
		return fmt.Errorf("page: missing path")
	}
	if !strings.HasPrefix(path, "/") {
		return fmt.Errorf("page %q: path must start with /", path)
	}
	// The screen router reads ":id" and "{id}" as parameters, and a world
	// page cannot take any: registering one fails the rebuild.
	if strings.ContainsAny(path, ":{}") {
		return fmt.Errorf("page %q: path parameters are not supported; use a static path", path)
	}
	return nil
}

// TableName returns the table the framework registry gives e: its
// declared Table, else the framework's default for its name (snake_case,
// so blogPosts lives in blog_posts). It asks framework/entity.Define
// rather than re-deriving the rule, so the seed INSERT, the CRUD mount
// path, and the migrated table cannot disagree about where rows live.
func (e *Entity) TableName() string {
	if e == nil {
		return ""
	}
	if e.Name == "" {
		return e.Table
	}
	return entity.Define(e.Name, entity.EntityConfig{Table: e.Table}).GetTable()
}

// EntityMountPath returns the URL an entity's auto-CRUD routes mount at,
// the same prefix+'/'+table the framework's App.Mount computes. The
// collision guard has to key on this and not on the entity NAME: Mount
// compares paths, and an entity whose table differs from its Name
// collides at the table's path.
func EntityMountPath(prefix string, e *Entity) string {
	if e == nil {
		return ""
	}
	table := e.TableName()
	if table == "" {
		return ""
	}
	prefix = strings.Trim(prefix, "/")
	if prefix == "" {
		return "/" + table
	}
	return "/" + prefix + "/" + table
}

// PageCollidesWithEntity reports the entity whose CRUD mount a page path
// would land on, or "" when the path is free.
//
// App.Mount panics on this collision, and a panic during boot is not a
// refusal -- it takes the process with it, which is how a hand-authored
// journal wedged the server. Refusing the edit is the same answer given
// earlier and survivably.
func PageCollidesWithEntity(w *World, path string) string {
	if w == nil {
		return ""
	}
	prefix := apiPrefix(w)
	for _, name := range slices.Sorted(maps.Keys(w.Entities)) {
		if EntityMountPath(prefix, w.Entities[name]) == path {
			return name
		}
	}
	return ""
}

// EntityCollidesWithPage is PageCollidesWithEntity's mirror: the page
// whose path e's CRUD mount would land on, or "" when the mount is free.
// It is the same App.Mount panic, reached by adding the entity second.
func EntityCollidesWithPage(w *World, e *Entity) string {
	if w == nil || e == nil {
		return ""
	}
	mount := EntityMountPath(apiPrefix(w), e)
	if _, ok := w.Pages[mount]; ok && mount != "" {
		return mount
	}
	return ""
}

// apiPrefix is the prefix entity CRUD mounts under: the configured one,
// defaulting to "api" the way NormalizeAPIPrefix does.
func apiPrefix(w *World) string {
	if p := strings.Trim(w.App.APIPrefix, "/"); p != "" {
		return p
	}
	return "api"
}
