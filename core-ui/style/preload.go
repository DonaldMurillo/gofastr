package style

// RouteGraph represents the navigation flow of the application.
type RouteGraph struct {
	Routes map[string]RouteInfo // path → info
}

// RouteInfo holds metadata about a route.
type RouteInfo struct {
	Path     string   // route path
	CSSChunk string   // filename of CSS chunk
	Adjacent []string // paths of routes reachable from this one
}

// RoutePreload describes what to load and what to preload.
type RoutePreload struct {
	CSS     string   // CSS chunk for this route
	Preload []string // CSS chunks to preload
}

// NewRouteGraph creates a new empty route graph.
func NewRouteGraph() *RouteGraph {
	return &RouteGraph{
		Routes: make(map[string]RouteInfo),
	}
}

// AddRoute adds a route with its CSS chunk filename and adjacent route paths.
func (g *RouteGraph) AddRoute(path, cssChunk string, adjacent []string) {
	g.Routes[path] = RouteInfo{
		Path:     path,
		CSSChunk: cssChunk,
		Adjacent: adjacent,
	}
}

// PreloadManifest generates a manifest mapping each route to its preload info.
// For each route, the manifest includes the route's own CSS chunk and the CSS chunks
// of all adjacent routes (for preloading).
func (g *RouteGraph) PreloadManifest() map[string]RoutePreload {
	manifest := make(map[string]RoutePreload, len(g.Routes))

	for path, info := range g.Routes {
		preload := make([]string, 0, len(info.Adjacent))
		for _, adjPath := range info.Adjacent {
			if adj, ok := g.Routes[adjPath]; ok && adj.CSSChunk != "" {
				preload = append(preload, adj.CSSChunk)
			}
		}

		manifest[path] = RoutePreload{
			CSS:     info.CSSChunk,
			Preload: preload,
		}
	}

	return manifest
}
