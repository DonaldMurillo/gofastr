package appspec

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
)

// RouteParamNames returns the params a route declares, in {name} or :name syntax, as the router reads them.
func RouteParamNames(route string) []string {
	var names []string
	for _, seg := range strings.Split(route, "/") {
		if name, ok := RouteSegParam(seg); ok {
			names = append(names, name)
		}
	}
	return names
}

// RouteShape is the router's canonical pattern for route with parameter names erased.
func RouteShape(route string) string {
	parts := strings.Split(app.CanonicalRoutePattern(route), "/")
	for i, seg := range parts {
		if _, ok := RouteSegParam(seg); !ok {
			continue
		}
		inner := seg[1:]
		inner, catchAll := strings.CutSuffix(inner, "*")
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

// RouteSegParam returns the param a route segment declares and whether it declares one.
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
		name = name[:i]
	}
	return name, true
}

// ScreenParamName is the route param the generated SetParams reads: id when declared, else the last param, else id.
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

// ScreenRecordKeys returns the key columns of the composite-keyed record a screen shows, else nil.
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

// ScreenNeedsRouteID reports whether a screen holds an entity_detail and whether it holds an edit-mode entity_form.
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
