package uihost

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/i18n"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
	"github.com/DonaldMurillo/gofastr/framework/uihost/internal/sessiontoken"
)

// Session is a browser session: ID is the public stream and presence key,
// Token the signed credential the session cookie carries.
type Session struct {
	ID      string
	Created time.Time
	Token   string
}

const sessionMaxAge = 30 * 24 * time.Hour

func selfMintedSessionKey() []byte {
	var b [32]byte
	if _, err := cryptorand.Read(b[:]); err != nil {
		slog.Default().Error("uihost: session key mint failed", "err", err)
		return nil
	}
	return b[:]
}

// SetSessionKey replaces the session-signing key and drops any previous keys.
func (ds *UIHost) SetSessionKey(key []byte) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.sessionKey = key
	ds.previousSessionKeys = nil
}

// SetSessionKeys signs new tokens with current and still verifies tokens
// signed by any of previous.
func (ds *UIHost) SetSessionKeys(current []byte, previous ...[]byte) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.sessionKey = current
	ds.previousSessionKeys = previous
}

func (ds *UIHost) verifySessionToken(token string) (string, bool) {
	if token == "" {
		return "", false
	}
	ds.mu.RLock()
	current := ds.sessionKey
	previous := ds.previousSessionKeys
	ds.mu.RUnlock()
	return sessiontoken.VerifyAny(current, previous, token, time.Now(), sessionMaxAge)
}

// CreateSession mints a stateless session token; nothing is stored.
func (ds *UIHost) CreateSession() *Session {
	ds.mu.RLock()
	key := ds.sessionKey
	ds.mu.RUnlock()
	now := time.Now()
	token, id, err := sessiontoken.Mint(key, now)
	if err != nil {
		slog.Default().Error("uihost: session mint failed", "err", err)
		return &Session{Created: now}
	}
	return &Session{ID: id, Created: now, Token: token}
}

const sessionCookieSecureName = "__Host-gofastr-session"

const sessionCookieDevName = "gofastr-session"

func requestIsSecure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func requestIsLoopback(r *http.Request) bool {
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
	switch strings.ToLower(host) {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

func useSecureSessionCookie(r *http.Request) bool {
	return requestIsSecure(r) || !requestIsLoopback(r)
}

var plaintextRemoteWarnOnce sync.Once

func setSessionCookie(w http.ResponseWriter, r *http.Request, id string) {
	secure := useSecureSessionCookie(r)
	name := sessionCookieDevName
	if secure {
		name = sessionCookieSecureName
	}
	if secure && !requestIsSecure(r) {
		plaintextRemoteWarnOnce.Do(func() {
			slog.Default().Warn("uihost: session cookie sent Secure over a plaintext non-loopback origin: browsers will drop it and every session check will 401",
				"host", textsafe.ScrubControlBytes(r.Host),
				"fix", "serve over TLS (or a reverse proxy setting X-Forwarded-Proto), or use http://localhost for development")
		})
	}
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    id,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	})
}

func readSessionCookie(r *http.Request) string {
	name := sessionCookieDevName
	if useSecureSessionCookie(r) {
		name = sessionCookieSecureName
	}
	if c, err := r.Cookie(name); err == nil && c.Value != "" {
		return c.Value
	}
	return ""
}

type noMintKey struct{}

func withoutMint(ctx context.Context) context.Context {
	return context.WithValue(ctx, noMintKey{}, true)
}

func mintRefused(ctx context.Context) bool {
	refused, _ := ctx.Value(noMintKey{}).(bool)
	return refused
}

const maxMutatingBodyBytes = 64 * 1024

func (ds *UIHost) requireValidSession(w http.ResponseWriter, r *http.Request) (string, bool) {
	id, ok := ds.verifySessionToken(readSessionCookie(r))
	if !ok {
		i18n.HTTPError(w, r, http.StatusUnauthorized, i18n.StatusPhrase(http.StatusUnauthorized))
		return "", false
	}
	return id, true
}

func (ds *UIHost) requireSessionOrEmbedGrant(w http.ResponseWriter, r *http.Request) bool {
	if token := r.Header.Get(embedGrantHeader); token != "" {
		if !ds.embedGrantOK(r) {
			i18n.HTTPError(w, r, http.StatusUnauthorized, i18n.StatusPhrase(http.StatusUnauthorized))
			return false
		}
		stripAmbientCredentials(r)
		return true
	}
	_, ok := ds.requireValidSession(w, r)
	return ok
}

func rejectCrossOrigin(w http.ResponseWriter, r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		i18n.HTTPError(w, r, http.StatusForbidden, i18n.StatusPhrase(http.StatusForbidden))
		return true
	}
	if !strings.EqualFold(u.Host, r.Host) {
		i18n.HTTPError(w, r, http.StatusForbidden, i18n.StatusPhrase(http.StatusForbidden))
		return true
	}
	return false
}

func decodeBounded(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxMutatingBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			i18n.HTTPError(w, r, http.StatusRequestEntityTooLarge, i18n.StatusPhrase(http.StatusRequestEntityTooLarge))
			return false
		}
		i18n.HTTPError(w, r, http.StatusBadRequest, i18n.ReasonInvalidJSONBody)
		return false
	}
	if err := handler.UnmarshalStrict(body, dst); err != nil {
		i18n.HTTPError(w, r, http.StatusBadRequest, i18n.ReasonInvalidJSONBody)
		return false
	}
	return true
}

func (ds *UIHost) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	if rejectCrossOrigin(w, r) {
		return
	}
	if r.Header.Get(fembed.GrantHeader) != "" {
		i18n.HTTPError(w, r, http.StatusForbidden, i18n.StatusPhrase(http.StatusForbidden))
		return
	}
	sess := ds.CreateSession()
	setSessionCookie(w, r, sess.Token)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"sessionId": sess.ID})
}
