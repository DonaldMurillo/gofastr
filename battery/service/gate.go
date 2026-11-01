package service

import (
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/internal/gatepath"
	"github.com/DonaldMurillo/gofastr/internal/localuser"
)

// The loopback gate. Any local process and any page the user's browser
// opens can reach 127.0.0.1:<port>, so every request must pass three
// independent checks. The first two never switch off; the third is
// skipped only for a path under Config.OpenPrefixes, which the app's
// own auth protects:
//
//  1. Host names the daemon's own loopback address (or a configured
//     AllowHosts entry). Pinning Host is what stops DNS rebinding; an
//     Origin check never does.
//  2. An unsafe method is not cross-site (handler.IsCrossSiteRequest).
//  3. A session cookie signed with a key derived from the persisted
//     secret, minted only by redeeming a control-channel token.
//
// Neither a token nor a cookie value is ever logged or put into an
// error string.

const (
	// loginPath is the token door. POST t=<token>; GET ?t= only with
	// Config.InsecureTokenInURL.
	loginPath = "/__gofastr/service/login"
	// loginTokenTTL bounds a minted sign-in token.
	loginTokenTTL = 60 * time.Second
	// maxPendingTokens caps outstanding tokens so a looping client
	// cannot grow the table without bound.
	maxPendingTokens = 64
	// maxLoginBody bounds the login form.
	maxLoginBody = 4 << 10
	// sessionKeyInfo is the HKDF purpose of the session-signing key.
	sessionKeyInfo = "gofastr/service/session/v1"
)

var errTooManyTokens = errors.New("too many outstanding sign-in tokens; wait a minute and try again")

type gate struct {
	openCmd  string // the sign-in command, for hints: "hello service open"
	key      []byte
	ttl      time.Duration
	allowGET bool
	now      func() time.Time
	// user is the local identity put on every signed-in request; nil
	// when battery/auth owns identity. Set before the listener opens.
	user *localuser.User
	// open is Config.OpenPrefixes: paths that pass without a session.
	// Set before the listener opens and never written after.
	open []string

	mu      sync.Mutex
	hosts   []string
	cookie  string
	port    string
	pending map[[32]byte]time.Time
}

// newGate derives the session key from secret. The cookie name is set
// by pin, once the port is known.
func newGate(openCmd, secret string, ttl time.Duration, allowGET bool) (*gate, error) {
	key, err := hkdf.Key(sha256.New, []byte(secret), nil, sessionKeyInfo, 32)
	if err != nil {
		return nil, fmt.Errorf("service: derive session key: %w", err)
	}
	return &gate{
		openCmd:  openCmd,
		key:      key,
		ttl:      ttl,
		allowGET: allowGET,
		now:      time.Now,
		pending:  make(map[[32]byte]time.Time),
	}, nil
}

// pin sets the Host values the gate accepts (the bound loopback
// address under both spellings a browser uses, plus extra) and the
// cookie name. Until pin runs every request is refused. Cookies are
// not isolated by port, so the name carries the port: an installed
// daemon and a dev run of the same app never overwrite each other's
// session.
func (g *gate) pin(port int, extra []string) {
	p := strconv.Itoa(port)
	hosts := []string{"127.0.0.1:" + p, "localhost:" + p}
	hosts = append(hosts, extra...)
	g.mu.Lock()
	g.hosts = hosts
	g.cookie = "gofastr_service_" + p
	g.port = p
	g.mu.Unlock()
}

// canonicalURL is where a safe request that named the app as
// localhost:<port> is sent: the same path on 127.0.0.1:<port>. Cookies
// are per host name, and `open` signs the browser in on 127.0.0.1, so
// a localhost tab (the address `gofastr dev` prints) would otherwise
// stay signed out. "" when r is not such a request.
func (g *gate) canonicalURL(r *http.Request) string {
	if !safeMethod(r.Method) {
		return ""
	}
	g.mu.Lock()
	port := g.port
	g.mu.Unlock()
	if port == "" || !strings.EqualFold(r.Host, "localhost:"+port) {
		return ""
	}
	return "http://127.0.0.1:" + port + r.URL.RequestURI()
}

func (g *gate) cookieName() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.cookie
}

func (g *gate) hostAllowed(host string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, h := range g.hosts {
		if strings.EqualFold(h, host) {
			return true
		}
	}
	return false
}

// mintToken returns a fresh single-use sign-in token. Only its SHA-256
// is kept, so the table can be looked up by key without a timing side
// channel on the token itself.
func (g *gate) mintToken() (string, error) {
	tok, err := randomToken()
	if err != nil {
		return "", err
	}
	now := g.now()
	g.mu.Lock()
	defer g.mu.Unlock()
	for h, exp := range g.pending {
		if !now.Before(exp) {
			delete(g.pending, h)
		}
	}
	if len(g.pending) >= maxPendingTokens {
		return "", errTooManyTokens
	}
	g.pending[sha256.Sum256([]byte(tok))] = now.Add(loginTokenTTL)
	return tok, nil
}

// redeem burns tok and reports whether it was live. A wrong token
// burns nothing; a right one works exactly once.
func (g *gate) redeem(tok string) bool {
	if tok == "" {
		return false
	}
	h := sha256.Sum256([]byte(tok))
	g.mu.Lock()
	exp, ok := g.pending[h]
	if ok {
		delete(g.pending, h)
	}
	g.mu.Unlock()
	return ok && g.now().Before(exp)
}

// mintSession returns a signed session value:
// v1.<expiry unix>.<nonce>.<HMAC-SHA256 over the first three parts>.
func (g *gate) mintSession() (string, time.Time, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", time.Time{}, err
	}
	exp := g.now().Add(g.ttl)
	body := "v1." + strconv.FormatInt(exp.Unix(), 10) + "." + base64.RawURLEncoding.EncodeToString(nonce[:])
	return body + "." + g.sign(body), exp, nil
}

func (g *gate) sign(body string) string {
	m := hmac.New(sha256.New, g.key)
	m.Write([]byte(body))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// validSession checks the signature first and the expiry second.
func (g *gate) validSession(v string) bool {
	i := strings.LastIndexByte(v, '.')
	if i < 0 || !strings.HasPrefix(v, "v1.") {
		return false
	}
	body, sig := v[:i], v[i+1:]
	if !hmac.Equal([]byte(sig), []byte(g.sign(body))) {
		return false
	}
	parts := strings.Split(body, ".")
	if len(parts) != 3 {
		return false
	}
	exp, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return false
	}
	return g.now().Unix() < exp
}

// hasSession checks every cookie of the gate's name: a stale duplicate
// in front of the live one must not log the user out.
func (g *gate) hasSession(r *http.Request) bool {
	name := g.cookieName()
	for _, c := range r.Cookies() {
		if c.Name == name && g.validSession(c.Value) {
			return true
		}
	}
	return false
}

// wrap is the gate, installed with App.WrapServer so it stands in
// front of everything the listener serves: every route, the SSE bus,
// the UI host's pages, and a setup wizard served in the router's
// place. It answers the sign-in path itself, so that door exists even
// when the router is not what is being served.
func (g *gate) wrap(next http.Handler) http.Handler {
	login := g.loginHandler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !g.hostAllowed(r.Host) {
			i18n.HTTPError(w, r, http.StatusForbidden, reasonNotOwnLoopback)
			return
		}
		// An open path is for a client with no browser: it skips the
		// localhost-to-127.0.0.1 hop (that exists for the cookie) and
		// the session, never the Host pin or the cross-site refusal.
		open := gatepath.Open(r, g.open)
		if u := g.canonicalURL(r); u != "" && !open {
			w.Header().Set("Cache-Control", "no-store")
			http.Redirect(w, r, u, http.StatusTemporaryRedirect)
			return
		}
		if r.URL.Path == loginPath {
			login.ServeHTTP(w, r)
			return
		}
		// An open path refuses only a FORGED cross-site write (a form-shaped
		// body another site's page could send without a preflight): a
		// browser client on another origin sending JSON is preflighted, so
		// CORS decides it, and the app's own auth does the rest. Gated
		// paths keep the strict check.
		if open && handler.IsForgedCrossSiteWrite(r) || !open && !safeMethod(r.Method) && handler.IsCrossSiteRequest(r) {
			i18n.HTTPError(w, r, http.StatusForbidden, handler.ReasonCrossSiteRefused)
			return
		}
		signed := g.hasSession(r)
		if open && !signed {
			w.Header().Set("Cache-Control", "no-store")
			next.ServeHTTP(w, r)
			return
		}
		if !signed {
			w.Header().Set("Cache-Control", "no-store")
			i18n.HTTPError(w, r, http.StatusUnauthorized, reasonNotSignedIn, map[string]any{"command": g.openCmd})
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		if g.user != nil {
			r = r.WithContext(handler.SetUser(r.Context(), g.user))
		}
		next.ServeHTTP(w, r)
	})
}

func safeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}

// loginHandler redeems a token for a session cookie. It is exempt from
// the cross-site check on purpose: `open` POSTs from a file:// page,
// whose Origin is opaque, and the single-use token is itself the proof
// (a page without one has nothing to redeem).
//
// Success answers 200 with a page that refreshes to "/" instead of a
// redirect. The cookie is SameSite=Strict, and a redirect chain that
// began on a file:// page is cross-site, so a 303 would arrive at "/"
// without the cookie it just set. A refresh is a new navigation started
// by this same-origin page, and carries it.
func (g *gate) loginHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		var tok string
		switch {
		case r.Method == http.MethodPost:
			r.Body = http.MaxBytesReader(w, r.Body, maxLoginBody)
			if err := r.ParseForm(); err != nil {
				i18n.HTTPError(w, r, http.StatusBadRequest, i18n.StatusPhrase(http.StatusBadRequest))
				return
			}
			tok = r.PostForm.Get("t")
		case r.Method == http.MethodGet && g.allowGET:
			tok = r.URL.Query().Get("t")
		default:
			w.Header().Set("Allow", http.MethodPost)
			i18n.HTTPError(w, r, http.StatusMethodNotAllowed, i18n.StatusPhrase(http.StatusMethodNotAllowed))
			return
		}
		if !g.redeem(tok) {
			i18n.HTTPError(w, r, http.StatusForbidden, reasonSignInLinkUsed, map[string]any{"command": g.openCmd})
			return
		}
		val, exp, err := g.mintSession()
		if err != nil {
			i18n.HTTPError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
			return
		}
		//gofastr:allow(security/insecure-cookie) plain loopback origin only, Secure would make the browser drop this cookie over http://127.0.0.1
		http.SetCookie(w, &http.Cookie{
			Name:     g.cookieName(),
			Value:    val,
			Path:     "/",
			Expires:  exp,
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
		})
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(signedInPage(r.Context())))
	})
}
