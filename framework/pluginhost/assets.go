package pluginhost

import (
	"io/fs"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/static"
	"github.com/DonaldMurillo/gofastr/internal/clientip"
	"github.com/DonaldMurillo/gofastr/internal/rootfs"
)

func framedCSP(origin string, csp []string) string {
	scriptSrc := origin
	seen := map[string]bool{}
	for _, kw := range csp {
		if !allowedCSPKeywords[kw] || seen[kw] {
			continue
		}
		seen[kw] = true
		scriptSrc += " " + kw
	}
	return "sandbox allow-scripts" +
		"; default-src " + origin +
		"; script-src " + scriptSrc +
		"; style-src " + origin + " 'unsafe-inline'" +
		"; img-src " + origin + " data:" +
		"; font-src " + origin + " data:" +
		"; connect-src 'none'" +
		"; form-action 'none'" +
		"; frame-ancestors " + origin +
		"; base-uri " + origin
}

func requestOrigin(r *http.Request) (string, bool) {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if clientip.TrustedFrom(r.Context()).ContainsAddr(r.RemoteAddr) {
		if xf := r.Header.Get("X-Forwarded-Proto"); xf != "" {
			switch xf {
			case "http", "https":
				scheme = xf
			default:
				return "", false
			}
		}
	}
	host := r.Host
	if host == "" {
		host = "localhost"
	}
	if !validHost(host) {
		return "", false
	}
	return scheme + "://" + host, true
}

func validHost(host string) bool {
	if host == "" || len(host) > 253 {
		return false
	}
	for i := 0; i < len(host); i++ {
		c := host[i]
		ok := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') ||
			c == '.' || c == '-' || c == ':' || c == '[' || c == ']'
		if !ok {
			return false
		}
	}
	return true
}

// AssetSpec describes one asset served from a filesystem by [AssetServer].
type AssetSpec struct {
	// Name is the file in the fs.FS, served at prefix + "/" + Name.
	Name string

	// ContentType overrides the type derived from Name's extension.
	ContentType string

	// Framed marks the frame document and its sub-resources.
	Framed bool
}

// AssetServer serves a plugin's client assets with the platform's headers.
type AssetServer struct {
	prefix string
	specs  []AssetSpec
	fsys   fs.FS
	extra  []loadedAsset
	csp    []string
}

type loadedAsset struct {
	path        string
	contentType string
	framed      bool
	worker      *WorkerCSP
	cache       CacheProfile
	bytes       []byte
}

// NewAssetServer serves specs from fsys, which must confine reads, under prefix.
func NewAssetServer(fsys fs.FS, prefix string, specs []AssetSpec) *AssetServer {
	return &AssetServer{prefix: prefix, specs: specs, fsys: fsys}
}

// WithCSP sets the framed script-src keywords from [Manifest.CSP].
func (s *AssetServer) WithCSP(tokens []string) *AssetServer {
	s.csp = tokens
	return s
}

// AddBytes serves b at route. It panics on an invalid option.
func (s *AssetServer) AddBytes(route, contentType string, framed bool, b []byte, opts ...AssetOption) {
	var o assetOptions
	for _, opt := range opts {
		opt(&o)
	}
	if o.worker != nil {
		if framed {
			panic("pluginhost: WithWorkerCSP on a framed asset: the framed policy is fixed by the platform — register the worker separately with framed=false")
		}
		if err := validateWorkerProfile(*o.worker); err != nil {
			panic(err.Error())
		}
	}
	if !o.cache.valid() {
		panic("pluginhost: unknown cache profile " + strconv.Itoa(int(o.cache)))
	}
	s.extra = append(s.extra, loadedAsset{
		path:        route,
		contentType: resolveContentType(route, contentType),
		framed:      framed,
		worker:      o.worker,
		cache:       o.cache,
		bytes:       b,
	})
}

// Register mounts every asset on rt; it panics on specs with a nil fs.FS.
func (s *AssetServer) Register(rt *router.Router) {
	if s.fsys == nil && len(s.specs) > 0 {
		panic("pluginhost: AssetServer has " + strconv.Itoa(len(s.specs)) +
			" spec(s) but a nil fs.FS to read them from — pass the plugin's " +
			"embedded assets (ClientModule.Assets) to NewAssetServer, or use " +
			"ClientModule.AssetServer which supplies them")
	}
	for _, spec := range s.specs {
		path := joinPath(s.prefix, spec.Name)
		rt.Get(path, s.serveFS(spec))
	}
	for _, a := range s.extra {
		rt.Get(a.path, s.serveBytes(a))
	}
}

func joinPath(prefix, name string) string {
	if prefix == "" {
		return "/" + strings.TrimPrefix(name, "/")
	}
	if strings.HasSuffix(prefix, "/") {
		return prefix + strings.TrimPrefix(name, "/")
	}
	return prefix + "/" + strings.TrimPrefix(name, "/")
}

func (s *AssetServer) serveFS(spec AssetSpec) http.HandlerFunc {
	contentType := resolveContentType(spec.Name, spec.ContentType)
	assets, bindErr := confinedAssetFS(s.fsys)
	return func(w http.ResponseWriter, r *http.Request) {
		if bindErr != nil {
			http.NotFound(w, r)
			return
		}
		b, err := fs.ReadFile(assets, spec.Name) //gofastr:allow(rootread) confinedAssetFS wraps disk roots with os.Root; other filesystems must be embedded or root-confined by the NewAssetServer contract
		if err != nil {
			http.NotFound(w, r)
			return
		}
		writeAsset(w, r, b, contentType, responsePolicy{framed: spec.Framed, csp: s.csp})
	}
}

func confinedAssetFS(fsys fs.FS) (fs.FS, error) {
	if _, ok := rootfs.DiskFS(fsys); !ok {
		return fsys, nil
	}
	directory, err := fsys.Open(".")
	if err != nil {
		return nil, err
	}
	defer directory.Close()
	file, disk := directory.(*os.File)
	if !disk {
		return fsys, nil
	}
	identity, err := file.Stat()
	if err != nil {
		return nil, err
	}
	root, err := rootfs.Open(file.Name())
	if err != nil {
		return nil, err
	}
	bound, err := root.Stat(".")
	if err != nil {
		return nil, err
	}
	if !os.SameFile(identity, bound) {
		return nil, fs.ErrNotExist
	}
	return root.FS(), nil
}

func resolveContentType(name, declared string) string {
	if declared != "" {
		return declared
	}
	return static.DetectFromName(name)
}

func (s *AssetServer) serveBytes(a loadedAsset) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeAsset(w, r, a.bytes, a.contentType, responsePolicy{
			framed: a.framed,
			csp:    s.csp,
			worker: a.worker,
			cache:  a.cache,
		})
	}
}

type responsePolicy struct {
	framed bool
	csp    []string
	worker *WorkerCSP
	cache  CacheProfile
}

func writeAsset(w http.ResponseWriter, r *http.Request, b []byte, contentType string, p responsePolicy) {
	h := w.Header()
	var origin string
	if p.framed {
		var ok bool
		origin, ok = requestOrigin(r)
		if !ok {
			i18n.HTTPError(w, r, http.StatusBadRequest, i18n.ReasonOriginRefused)
			return
		}
	}
	h.Set("Content-Type", contentType)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", p.cache.cacheControl())
	if p.framed {
		h.Add("Vary", "Host, X-Forwarded-Proto")
		h.Del("X-Frame-Options")
		h.Set("Content-Security-Policy", framedCSP(origin, p.csp))
		h.Set("Cross-Origin-Resource-Policy", "cross-origin")
	}
	if p.worker != nil {
		h.Set("Content-Security-Policy", workerCSP(*p.worker))
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b)
}
