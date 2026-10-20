package framework

import (
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"
)

// TLSConfig makes the app serve HTTPS on its listen address. Set exactly
// one source: CertFile and KeyFile, AutoCert, or Config. Every source
// refuses protocol versions below TLS 1.2, and HTTP/2 is offered over
// TLS unless AppConfig.HTTPProtocols says otherwise.
//
// Leave AppConfig.TLS nil behind a proxy or load balancer that
// terminates TLS; the app then serves plain HTTP to it.
type TLSConfig struct {
	// CertFile and KeyFile are PEM files: the certificate chain and its
	// private key. They are read at Start, which fails if they do not
	// load, and read again on the next handshake after either file's
	// modification time or size changes, so a renewal (certbot, a
	// mounted secret) takes effect without a restart. A pair that does
	// not load, such as a new certificate beside the old key mid-copy,
	// keeps the last good one and logs.
	CertFile string
	KeyFile  string

	// AutoCert obtains and renews certificates from an ACME authority
	// (Let's Encrypt by default) using the TLS-ALPN-01 challenge, which
	// runs on the HTTPS port itself: the app must be reachable on :443
	// at every name in Domains.
	AutoCert *AutoCertConfig

	// Config is a caller-built configuration, for what the other two do
	// not cover (client certificates, a certificate store). It must
	// supply a certificate (Certificates, GetCertificate or
	// GetConfigForClient). A zero MinVersion becomes TLS 1.2; a lower
	// one fails Start. A config GetConfigForClient returns with a
	// MinVersion below TLS 1.2 is raised to it. The app works on a clone.
	Config *tls.Config
}

// AutoCertConfig configures ACME certificates for TLSConfig.AutoCert.
// Setting it accepts the ACME authority's terms of service.
type AutoCertConfig struct {
	// Domains are the host names to obtain certificates for. Required:
	// a handshake for any other name is refused, so a scanner cannot
	// make the app request certificates for names it does not serve.
	Domains []string
	// CacheDir is where certificates and the account key persist across
	// restarts, created 0700 if missing. Required: without it every
	// restart asks the authority again and soon hits its rate limits.
	CacheDir string
	// Email is the contact address the authority sends expiry notices
	// to. Optional.
	Email string
	// DirectoryURL is the ACME directory. Empty is Let's Encrypt's
	// production directory; point it at the staging directory while
	// testing a deployment.
	DirectoryURL string
}

// WithTLS serves HTTPS. Equivalent to setting AppConfig.TLS.
func WithTLS(cfg TLSConfig) AppOption {
	return func(a *App) { a.Config.TLS = &cfg }
}

// WithHTTPProtocols sets the HTTP versions the server speaks.
// Equivalent to setting AppConfig.HTTPProtocols.
func WithHTTPProtocols(p http.Protocols) AppOption {
	return func(a *App) { a.Config.HTTPProtocols = &p }
}

// urlScheme is the scheme of the app's own URLs: the banner, the setup
// link and the local run record.
func (a *App) urlScheme() string {
	if a.Config.TLS != nil {
		return "https"
	}
	return "http"
}

// serverTLSConfig builds the listener's TLS configuration from
// AppConfig.TLS, or returns nil for plain HTTP.
func (a *App) serverTLSConfig() (*tls.Config, error) {
	c := a.Config.TLS
	if c == nil {
		return nil, nil
	}
	files := c.CertFile != "" || c.KeyFile != ""
	sources := 0
	for _, set := range []bool{files, c.AutoCert != nil, c.Config != nil} {
		if set {
			sources++
		}
	}
	if sources != 1 {
		return nil, errors.New("framework: TLS needs exactly one of CertFile and KeyFile, AutoCert, or Config")
	}
	switch {
	case files:
		if c.CertFile == "" || c.KeyFile == "" {
			return nil, errors.New("framework: TLS needs both CertFile and KeyFile")
		}
		r := &certReloader{certFile: c.CertFile, keyFile: c.KeyFile, logger: a.Logger()}
		if err := r.reload(); err != nil {
			return nil, fmt.Errorf("framework: TLS certificate: %w", err)
		}
		return &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: r.get}, nil
	case c.AutoCert != nil:
		ac := c.AutoCert
		if len(ac.Domains) == 0 {
			return nil, errors.New("framework: TLS AutoCert needs Domains")
		}
		for _, d := range ac.Domains {
			if d == "" {
				return nil, errors.New("framework: TLS AutoCert has an empty domain")
			}
		}
		if ac.CacheDir == "" {
			return nil, errors.New("framework: TLS AutoCert needs CacheDir")
		}
		m := &autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			HostPolicy: autocert.HostWhitelist(ac.Domains...),
			Cache:      autocert.DirCache(ac.CacheDir),
			Email:      ac.Email,
		}
		if ac.DirectoryURL != "" {
			m.Client = &acme.Client{DirectoryURL: ac.DirectoryURL}
		}
		cfg := m.TLSConfig()
		cfg.MinVersion = tls.VersionTLS12
		return cfg, nil
	default:
		cfg := c.Config.Clone()
		if cfg.MinVersion == 0 {
			cfg.MinVersion = tls.VersionTLS12
		}
		if cfg.MinVersion < tls.VersionTLS12 {
			return nil, errors.New("framework: TLS Config.MinVersion is below TLS 1.2")
		}
		if len(cfg.Certificates) == 0 && cfg.GetCertificate == nil && cfg.GetConfigForClient == nil {
			return nil, errors.New("framework: TLS Config has no certificate")
		}
		if get := cfg.GetConfigForClient; get != nil {
			// The callback's config replaces this one for the handshake,
			// so it gets the same floor: a MinVersion below TLS 1.2 is
			// raised to it on a clone.
			cfg.GetConfigForClient = func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
				c, err := get(hello)
				if err != nil || c == nil || c.MinVersion >= tls.VersionTLS12 {
					return c, err
				}
				c = c.Clone()
				c.MinVersion = tls.VersionTLS12
				return c, nil
			}
		}
		return cfg, nil
	}
}

// fileStamp is what a certificate file looked like when it was read.
type fileStamp struct {
	mod  time.Time
	size int64
}

// certReloader serves a certificate pair from disk and reads it again
// when either file changes.
type certReloader struct {
	certFile, keyFile string
	logger            *slog.Logger

	mu      sync.Mutex
	cert    *tls.Certificate
	loaded  [2]fileStamp // the stamps cert was read at
	refused [2]fileStamp // the last stamps that failed to load
}

// stamps stats both files.
func (r *certReloader) stamps() ([2]fileStamp, error) {
	var out [2]fileStamp
	for i, f := range []string{r.certFile, r.keyFile} {
		info, err := os.Stat(f)
		if err != nil {
			return out, err
		}
		out[i] = fileStamp{mod: info.ModTime(), size: info.Size()}
	}
	return out, nil
}

// reload reads the pair. The caller holds mu or owns r exclusively.
func (r *certReloader) reload() error {
	st, err := r.stamps()
	if err != nil {
		return err
	}
	cert, err := tls.LoadX509KeyPair(r.certFile, r.keyFile)
	if err != nil {
		r.refused = st
		return err
	}
	r.cert, r.loaded = &cert, st
	return nil
}

// get is tls.Config.GetCertificate: the loaded pair, read again first
// when a file changed since it was loaded and since the last refusal.
func (r *certReloader) get(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	st, err := r.stamps()
	if err == nil && !sameStamps(st, r.loaded) && !sameStamps(st, r.refused) {
		if err := r.reload(); err != nil {
			r.logger.Error("framework: TLS certificate changed but did not load; serving the previous one",
				"cert_file", r.certFile, "error", err)
		}
	}
	return r.cert, nil
}

func sameStamps(a, b [2]fileStamp) bool {
	for i := range a {
		if !a[i].mod.Equal(b[i].mod) || a[i].size != b[i].size {
			return false
		}
	}
	return true
}
