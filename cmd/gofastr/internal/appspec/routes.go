package appspec

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
)

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

// RouteShape erases parameter names from the router's canonical route pattern.
// It keeps literal segments, constraints and catch-all markers.
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
