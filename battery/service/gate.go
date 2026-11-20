package service

import (
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/framework/localapp"
	"github.com/DonaldMurillo/gofastr/internal/gatepath"
)

const (
	loginPath        = "/__gofastr/service/login"
	loginTokenTTL    = 60 * time.Second
	maxPendingTokens = 64
	maxLoginBody     = 4 << 10
	sessionKeyInfo   = "gofastr/service/session/v1"
)

var errTooManyTokens = errors.New("too many outstanding sign-in tokens; wait a minute and try again")

type gate struct {
	openCmd  string
	key      []byte
	ttl      time.Duration
	allowGET bool
	now      func() time.Time
	user     *localapp.User
	open     []string

	mu      sync.Mutex
	hosts   []string
	cookie  string
	port    string
	browser string
	pending map[[32]byte]time.Time
}

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

func (g *gate) pin(port int, extra []string) {
	g.pinAt(net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), port, extra)
}

func (g *gate) pinAt(browser string, port int, extra []string) {
	p := strconv.Itoa(port)
	hosts := []string{"127.0.0.1:" + p, "localhost:" + p}
	hosts = append(hosts, extra...)
	g.mu.Lock()
	g.hosts = hosts
	g.cookie = "gofastr_service_" + p
	g.port = p
	g.browser = browser
	g.mu.Unlock()
}

func (g *gate) canonicalURL(r *http.Request) string {
	if !safeMethod(r.Method) {
		return ""
	}
	g.mu.Lock()
	port, browser := g.port, g.browser
	g.mu.Unlock()
	if port == "" || !strings.EqualFold(r.Host, "localhost:"+port) {
		return ""
	}
	return "http://" + browser + r.URL.RequestURI()
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

func (g *gate) hasSession(r *http.Request) bool {
	name := g.cookieName()
	for _, c := range r.Cookies() {
		if c.Name == name && g.validSession(c.Value) {
			return true
		}
	}
	return false
}

func (g *gate) wrap(next http.Handler) http.Handler {
	login := g.loginHandler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !g.hostAllowed(r.Host) {
			i18n.HTTPError(w, r, http.StatusForbidden, reasonNotOwnLoopback)
			return
		}
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
