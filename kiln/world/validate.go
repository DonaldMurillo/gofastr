package world

import (
	"fmt"
	"slices"
	"strings"
)

// The checks in this file are the shared half of a parity contract: the
// live tool API (kiln/protocol) and journal replay (kiln/journal) must
// accept exactly the same world states. They had drifted -- every rule
// below existed only on the live path, so a hand-authored
// .kiln.session.jsonl installed world state the API refuses, and the
// running server booted a world `kiln freeze` then rejects. Keeping the
// rules here, below both callers, is what stops them drifting again.

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
	for _, label := range sortedKeys(stubGroups) {
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
	return nil
}

// EntityMountPath returns the URL an entity's auto-CRUD routes mount at,
// the same prefix+'/'+table the framework's App.Mount computes. The
// collision guard has to key on this and not on the entity NAME: Mount
// compares paths, and an entity whose Table differs from its Name
// collides at the table's path.
func EntityMountPath(prefix string, e *Entity) string {
	if e == nil {
		return ""
	}
	table := e.Table
	if table == "" {
		table = e.Name
	}
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
	prefix := w.App.APIPrefix
	if prefix == "" {
		prefix = "api"
	}
	for _, name := range sortedEntityNames(w) {
		if EntityMountPath(prefix, w.Entities[name]) == path {
			return name
		}
	}
	return ""
}

// sortedKeys returns map keys in a stable order so an error names the
// same offending group across runs.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// sortedEntityNames keeps collision reporting deterministic when more
// than one entity could match.
func sortedEntityNames(w *World) []string {
	out := make([]string, 0, len(w.Entities))
	for k := range w.Entities {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
