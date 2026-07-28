package semantic

import (
	"net/http"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework"
)

// Plugin is the [framework.Plugin] adapter for the semantic-search battery. It
// owns no state of its own — callers construct an [Index] and hand it
// to [NewPlugin], which then registers /semantic/* routes on the app's
// router during framework.App.Init.
//
// Mount via:
//
//	idx, _ := semantic.Open(semantic.Options{Embedder: e, Path: "~/.gofastr/semantic/myapp"})
//	app.Plugins.Register(semantic.NewPlugin(idx))
type Plugin struct {
	idx    Index
	prefix string
}

// NewPlugin returns a Plugin that mounts routes under "/semantic".
// Use [Plugin.WithPrefix] to change the mount point.
func NewPlugin(idx Index) *Plugin {
	return &Plugin{idx: idx, prefix: "/semantic"}
}

// WithPrefix overrides the URL prefix. Leading slash required.
func (p *Plugin) WithPrefix(prefix string) *Plugin {
	if prefix == "" || prefix[0] != '/' {
		prefix = "/" + prefix
	}
	p.prefix = strings.TrimRight(prefix, "/")
	if p.prefix == "" {
		p.prefix = "/semantic"
	}
	return p
}

// Name implements [framework.Plugin].
func (p *Plugin) Name() string { return "semantic" }

// Init implements [framework.Plugin]. Mounts the stdlib [Handler] under
// the configured prefix on the app's router; routing semantics match
// Go 1.22 ServeMux.
func (p *Plugin) Init(app *framework.App) error {
	h := Handler(p.idx)
	stripped := http.StripPrefix(p.prefix, h)
	app.Router().Post(p.prefix+"/index", stripped)
	app.Router().Post(p.prefix+"/query", stripped)
	app.Router().Get(p.prefix+"/stats", stripped)
	app.Router().Delete(p.prefix+"/doc/{id}", stripped)
	app.Router().Delete(p.prefix+"/doc", stripped)
	return nil
}

// Index returns the underlying [Index] so other plugins or the app
// can perform direct calls without going through HTTP.
func (p *Plugin) Index() Index { return p.idx }

var _ framework.Plugin = (*Plugin)(nil)
