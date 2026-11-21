package semantic

import (
	"net/http"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework"
)

// Plugin is the framework.Plugin that mounts Handler for an Index under a
// prefix.
type Plugin struct {
	idx       Index
	prefix    string
	authToken string
	insecure  bool
}

// NewPlugin returns a Plugin that mounts routes under "/semantic".
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

// WithAuthToken requires a bearer token on the mounted routes; without it or
// WithInsecureNoAuth every request is refused.
func (p *Plugin) WithAuthToken(token string) *Plugin {
	p.authToken = token
	p.insecure = false
	return p
}

// WithInsecureNoAuth disables authentication on the mounted routes, for local
// development only.
func (p *Plugin) WithInsecureNoAuth() *Plugin {
	p.insecure = true
	return p
}

// Name implements [framework.Plugin].
func (p *Plugin) Name() string { return "semantic" }

// Init implements framework.Plugin.
func (p *Plugin) Init(app *framework.App) error {
	var opts []HandlerOption
	if p.insecure {
		opts = append(opts, WithInsecureDisabledAuth())
	} else if p.authToken != "" {
		opts = append(opts, WithAuthToken(p.authToken))
	}
	h := Handler(p.idx, opts...)
	stripped := http.StripPrefix(p.prefix, h)
	app.Router().Post(p.prefix+"/index", stripped)
	app.Router().Post(p.prefix+"/query", stripped)
	app.Router().Get(p.prefix+"/stats", stripped)
	app.Router().Delete(p.prefix+"/doc/{id}", stripped)
	app.Router().Delete(p.prefix+"/doc", stripped)
	return nil
}

// Index returns the underlying Index.
func (p *Plugin) Index() Index { return p.idx }

var _ framework.Plugin = (*Plugin)(nil)
