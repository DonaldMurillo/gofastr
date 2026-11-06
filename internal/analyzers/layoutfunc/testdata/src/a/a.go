// Package a holds the layoutfunc fixtures: the flagged shapes (route
// state read lexically inside a layout build, outside any RouteArea
// closure) and the allowed/silent shapes.
package a

import (
	"context"
	"net/http"

	app "example.app/core-ui/app"
	route "example.app/core-ui/route"
)

// flaggedLiteral covers every read arm in a build literal passed
// straight to NewLayout.
var flaggedLiteral = app.NewLayout("shell", app.LayoutSpec{}, func(ctx context.Context, l *app.LayoutTree) app.HTML {
	m, _ := app.MatchFromContext(ctx)               // want `layout build reads route state outside a RouteArea`
	_ = m.Param("id")                               // want `layout build reads route state outside a RouteArea`
	_ = m.Path()                                    // want `layout build reads route state outside a RouteArea`
	if r := app.RequestFromContext(ctx); r != nil { // want `layout build reads route state outside a RouteArea`
		_ = r.Method
	}
	if s, ok := route.From(ctx); ok { // want `layout build reads route state outside a RouteArea`
		_ = s.Path
	}
	return l.Primary()
})

// areaClosureIsAllowed is the lint's exemption: reads lexically inside
// a RouteArea closure, nested closures included, travel as fills on
// every navigation.
var areaClosureIsAllowed = app.NewLayout("ok", app.LayoutSpec{}, func(ctx context.Context, l *app.LayoutTree) app.HTML {
	inner := func(ctx context.Context, m app.Match) app.HTML {
		m2, _ := app.MatchFromContext(ctx)
		_ = m2.Path()
		_ = m2.Param("x")
		if r := app.RequestFromContext(ctx); r != nil {
			_ = r.URL
		}
		if s, ok := route.From(ctx); ok {
			_ = s.Path
		}
		return ""
	}
	return l.RouteArea("crumbs", inner)
})

// namedBuild passed by identifier: its body is a LayoutFunc body too.
func namedBuild(ctx context.Context, l *app.LayoutTree) app.HTML {
	_, _ = app.MatchFromContext(ctx) // want `layout build reads route state outside a RouteArea`
	return l.Primary()
}

var namedLayout = app.NewLayout("named", app.LayoutSpec{}, namedBuild)

// methodBuild: the selector form of the same resolution.
type shellish struct{}

func (shellish) build(ctx context.Context, l *app.LayoutTree) app.HTML {
	_ = app.RequestFromContext(ctx) // want `layout build reads route state outside a RouteArea`
	return l.Primary()
}

var methodLayout = app.NewLayout("m", app.LayoutSpec{}, shellish{}.build)

// typedVar: the explicit app.LayoutFunc variable form.
var typedBuild app.LayoutFunc = func(ctx context.Context, l *app.LayoutTree) app.HTML {
	_, _ = app.MatchFromContext(ctx) // want `layout build reads route state outside a RouteArea`
	return l.Primary()
}

// typedAssign: assignment whose left side is declared LayoutFunc.
var reassignTarget app.LayoutFunc

func init() {
	reassignTarget = func(ctx context.Context, l *app.LayoutTree) app.HTML {
		_, _ = app.MatchFromContext(ctx) // want `layout build reads route state outside a RouteArea`
		return l.Primary()
	}
}

// converted: the conversion form.
var converted = app.NewLayout("conv", app.LayoutSpec{}, app.LayoutFunc(func(ctx context.Context, l *app.LayoutTree) app.HTML {
	_, _ = app.MatchFromContext(ctx) // want `layout build reads route state outside a RouteArea`
	return l.Primary()
}))

// matchThroughHelper: a Match value that never came from the context —
// the receiver's static type is what the lint keys on.
func matchParam(m app.Match) string { return m.Param("id") } // silent: not lexically inside a build

var matchThroughHelper = app.NewLayout("h", app.LayoutSpec{}, func(ctx context.Context, l *app.LayoutTree) app.HTML {
	_ = matchParam(helperMatch()) // the helper itself is silent; its call carries no Match read here
	return l.Primary()
})

func helperMatch() app.Match { return app.Match{} }

// resolver is silent: a resolver (or any non-layout function) reading
// the match is the legitimate case the lint must not touch.
func resolver(ctx context.Context) (string, error) {
	m, ok := app.MatchFromContext(ctx)
	if !ok {
		return "", nil
	}
	return m.Path(), nil
}

// screenLoad is silent: screens and fills are per-request renders;
// only the layout build's chrome is static.
func screenLoad(ctx context.Context, req *http.Request) {
	m, _ := app.MatchFromContext(ctx)
	_ = m.Param("id")
	_ = app.RequestFromContext(ctx)
}

// queryInBuild: the URL query read straight in a kept build is the same
// stale chrome as the request it comes from.
var queryInBuild = app.NewLayout("q", app.LayoutSpec{}, func(ctx context.Context, l *app.LayoutTree) app.HTML {
	_ = app.QueryFromContext(ctx).Get("filter") // want `layout build reads route state outside a RouteArea`
	return l.Primary()
})

// A resolver that reads the query, read by a layout build outside a
// RouteArea: the tracker's kept project layer drew its issue list from
// such a value, and Reset to the bare project left the list filtered
// (D-37). The same key read inside a RouteArea closure, and a key whose
// resolver reads only the match, are silent.
type issueList struct{ filter string }

var issuesKey = app.NewKey[issueList]("issues")

var projectKey = app.NewKey[string]("project")

func loadIssues(ctx context.Context) (issueList, error) {
	return issueList{filter: app.QueryFromContext(ctx).Get("filter")}, nil
}

var resolvers = []app.Resolver{
	issuesKey.From(loadIssues),
	projectKey.From(func(ctx context.Context) (string, error) {
		m, _ := app.MatchFromContext(ctx)
		return m.Param("project"), nil
	}),
}

var queryResolverInBuild = app.NewLayout("qr", app.LayoutSpec{}, func(ctx context.Context, l *app.LayoutTree) app.HTML {
	_, _ = issuesKey.Get(ctx) // want `layout build reads a resolver that reads the URL query outside a RouteArea`
	_, _ = projectKey.Get(ctx)
	_ = l.RouteArea("issues", func(ctx context.Context, m app.Match) app.HTML {
		_, _ = issuesKey.Get(ctx)
		return ""
	})
	return l.Primary()
})

// ---------- named builds in the other arms (AZ16-04, 2026-11) ----------

// conversionNamed: the conversion of a NAMED build, not a literal. The
// arm resolved literals only, so this flagged body was never walked.
func conversionNamed(ctx context.Context, l *app.LayoutTree) app.HTML {
	m, _ := app.MatchFromContext(ctx) // want `layout build reads route state outside a RouteArea`
	_ = m.Param("id")                 // want `layout build reads route state outside a RouteArea`
	return l.Primary()
}

var convertedNamed = app.NewLayout("conv-named", app.LayoutSpec{}, app.LayoutFunc(conversionNamed))

// typedVarNamed: the explicit app.LayoutFunc variable bound to a named
// build.
func typedVarNamed(ctx context.Context, l *app.LayoutTree) app.HTML {
	m, _ := app.MatchFromContext(ctx) // want `layout build reads route state outside a RouteArea`
	_ = m.Path()                      // want `layout build reads route state outside a RouteArea`
	return l.Primary()
}

var typedBuildNamed app.LayoutFunc = typedVarNamed

// typedAssignNamed: assignment to a LayoutFunc-typed left side.
func typedAssignNamed(ctx context.Context, l *app.LayoutTree) app.HTML {
	m, _ := app.MatchFromContext(ctx) // want `layout build reads route state outside a RouteArea`
	_ = m.Param("slug")               // want `layout build reads route state outside a RouteArea`
	return l.Primary()
}

var reassignTargetNamed app.LayoutFunc

func init() {
	reassignTargetNamed = typedAssignNamed
}

// namedBuildClean is a named build that reads nothing: binding it
// through any arm stays silent.
func namedBuildClean(ctx context.Context, l *app.LayoutTree) app.HTML {
	return l.Primary()
}

var typedClean app.LayoutFunc = namedBuildClean
