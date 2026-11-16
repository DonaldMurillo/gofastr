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

// Session represents a connected browser session. ID ("sess-…") is the
// bare identifier used as the SSE stream / presence key and embedded in
// page chrome; Token is the signed credential stored in the session
// cookie: the ID plus mint time plus an HMAC any replica sharing the
// key can verify. Only the Token proves anything; the bare ID is public.
type Session struct {
	ID      string
	Created time.Time
	Token   string
}

// sessionMaxAge bounds how old a session token may be. Expiry is
// transparent: page render re-mints and re-sets the cookie whenever the
// presented token fails verification, so a rollover costs one render.
const sessionMaxAge = 30 * 24 * time.Hour

// selfMintedSessionKey returns a random per-boot signing key: the
// single-replica zero-config default. Restart invalidates outstanding
// tokens; page render re-mints transparently. Multi-replica deployments
// overwrite it via SetSessionKey (framework fails boot if they don't).
func selfMintedSessionKey() []byte {
	var b [32]byte
	if _, err := cryptorand.Read(b[:]); err != nil {
		// A wedged CSPRNG at construction time is unrecoverable for a
		// signing key; every session check will fail closed on the
		// short key rather than sign with something guessable.
		slog.Default().Error("uihost: session key mint failed", "err", err)
		return nil
	}
	return b[:]
}

// SetSessionKey replaces the session-signing key and clears any
// previous (rotation) keys: the single-secret path. Called once by
// framework.App.Mount (before any traffic) with the HKDF-derived key
// from the app secret; every replica configured with the same secret
// then accepts every other replica's session tokens. Not intended for
// per-request use. For graceful rotation use SetSessionKeys.
func (ds *UIHost) SetSessionKey(key []byte) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.sessionKey = key
	ds.previousSessionKeys = nil
}

// SetSessionKeys sets the current session-signing key and the
// verify-only previous keys for graceful GOFASTR_SECRET rotation,
// mirroring the CSRF AdditionalKeys idiom. New tokens are signed with
// the current key only; tokens signed by any previous key still verify
// for the drain window (one session TTL), after which the operator
// drops the old key and calls SetSessionKey (or SetSessionKeys with no
// previous keys) to close the window. framework.App.Mount prefers this
// seam when previousSecrets are configured. Not for per-request use.
func (ds *UIHost) SetSessionKeys(current []byte, previous ...[]byte) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.sessionKey = current
	ds.previousSessionKeys = previous
}

// verifySessionToken authenticates a cookie token and returns the bare
// session id embedded in it. It accepts the current key OR any previous
// (rotation) key, so a GOFASTR_SECRET rotation drains over a session
// TTL instead of logging every user out at once.
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

// CreateSession mints a new stateless session token. Nothing is stored
// server-side: the returned Session is a parsed view of the token, and
// validity is a signature check (verifySessionToken), not a lookup.
func (ds *UIHost) CreateSession() *Session {
	ds.mu.RLock()
	key := ds.sessionKey
	ds.mu.RUnlock()
	now := time.Now()
	token, id, err := sessiontoken.Mint(key, now)
	if err != nil {
		// Only a wedged kernel CSPRNG can land here. Degrade to an
		// unusable empty-token session rather than panic mid-request;
		// every check on it fails closed and the next render retries.
		slog.Default().Error("uihost: session mint failed", "err", err)
		return &Session{Created: now}
	}
	return &Session{ID: id, Created: now, Token: token}
}

// sessionCookieSecureName is the cookie name used over a secure (TLS)
// origin. The __Host- prefix locks the cookie to a Secure, Path=/,
// Domain-less scope: the browser refuses to accept it unless those
// hold, which is the defense against subdomain/fixation attacks.
const sessionCookieSecureName = "__Host-gofastr-session"

// sessionCookieDevName is used on a plaintext loopback dev origin.
// Browsers won't store (and reject the __Host- prefix on) a Secure
// cookie sent over http://, so a dev server on http://localhost would
// never get the cookie back. Every island RPC and the SSE stream would
// 401, and the console fills with reconnect errors. On loopback the
// connection is already trusted, so we drop Secure and the prefix there.
// Any non-loopback or TLS origin keeps the hardened __Host- form.
const sessionCookieDevName = "gofastr-session"

// requestIsSecure reports whether the request arrived over TLS, directly
// or terminated at a proxy that set X-Forwarded-Proto: https.
func requestIsSecure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// requestIsLoopback reports whether the request targets a loopback host.
// Only loopback origins are eligible for the relaxed dev cookie.
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

// useSecureSessionCookie decides whether this request should carry the
// hardened __Host- cookie: Secure everywhere EXCEPT a plaintext loopback
// dev origin, where a Secure cookie can't round-trip.
func useSecureSessionCookie(r *http.Request) bool {
	return requestIsSecure(r) || !requestIsLoopback(r)
}

// setSessionCookie writes the session cookie with the name + Secure flag
// appropriate to this request's origin.
// plaintextRemoteWarnOnce gates the one-time WARN below.
var plaintextRemoteWarnOnce sync.Once

func setSessionCookie(w http.ResponseWriter, r *http.Request, id string) {
	secure := useSecureSessionCookie(r)
	name := sessionCookieDevName
	if secure {
		name = sessionCookieSecureName
	}
	// Audible footgun: a plaintext NON-loopback origin (http://192.168.…)
	// gets the hardened Secure/__Host- cookie, which browsers refuse to
	// store over plain HTTP, so the session never lands and every island
	// RPC / SSE connect 401s in a loop. The cookie policy is deliberate
	// (never a relaxed cookie off loopback); the WARN makes the resulting
	// symptom diagnosable instead of silent. Serve over TLS or loopback.
	if secure && !requestIsSecure(r) {
		plaintextRemoteWarnOnce.Do(func() {
			slog.Default().Warn("uihost: session cookie sent Secure over a plaintext non-loopback origin: browsers will drop it and every session check will 401",
				// r.Host is request-borne: scrub it with the full textsafe
				// set (C0/DEL plus C1 and bidi):
				// net/http's header reader passes every byte >= 0x80
				// through, and slog's JSON handler leaves those runes raw
				// in the encoded line. scrubCtl is not enough here: it
				// stops at C0/DEL.
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

// readSessionCookie returns the session id from the cookie that matches
// this request's security mode. An origin's mode is stable (loopback
// http is always dev, TLS/remote is always hardened), so reading only
// the mode-appropriate name avoids picking up a stale cross-mode cookie,
// e.g. a __Host- cookie left over from a prior https run won't shadow
// the dev cookie on a later http://localhost run.
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

// withoutMint marks ctx so finishPageDocument reads a live session but
// never mints one (the screen-path 405).
func withoutMint(ctx context.Context) context.Context {
	return context.WithValue(ctx, noMintKey{}, true)
}

// mintRefused reports whether ctx carries the withoutMint mark.
func mintRefused(ctx context.Context) bool {
	refused, _ := ctx.Value(noMintKey{}).(bool)
	return refused
}

// maxMutatingBodyBytes bounds JSON bodies accepted by mutating
// /__gofastr/* endpoints (server actions, session creation). Anything
// past 64 KiB is rejected with 413: these endpoints take small
// structured commands, not file uploads.
const maxMutatingBodyBytes = 64 * 1024

// requireValidSession resolves the session token from the session cookie
// (hardened __Host- form on TLS/non-loopback origins, relaxed dev form
// on plaintext localhost), verifies its signature, and returns the bare
// session id. On failure writes 401 and returns "", false. Mutating
// /__gofastr/* endpoints call this so attackers can't pass a forged
// session via query string or invent a cookie that was never signed by
// a replica holding the session key.
func (ds *UIHost) requireValidSession(w http.ResponseWriter, r *http.Request) (string, bool) {
	id, ok := ds.verifySessionToken(readSessionCookie(r))
	if !ok {
		i18n.HTTPError(w, r, http.StatusUnauthorized, i18n.StatusPhrase(http.StatusUnauthorized))
		return "", false
	}
	return id, true
}

// requireSessionOrEmbedGrant gates a /__gofastr/* endpoint that an embedded
// surface must also be able to reach.
//
// These endpoints were written when the session cookie was the only way to be
// somebody, and they all failed the same way once surfaces could be framed: a
// frame carries no cookie, so cross-site the endpoint refused it and the
// feature was dead, while same-site an unrelated ambient session answered it,
// making "does my embed work" depend on whether the viewer happens to be logged
// into the app in another tab.
//
// When the grant is what authorises the request, the cookie is discarded first.
// A same-site frame really does send one, and answering on the strength of a
// credential the caller did not present is the identity confusion the embed
// routes strip cookies to prevent.
func (ds *UIHost) requireSessionOrEmbedGrant(w http.ResponseWriter, r *http.Request) bool {
	if token := r.Header.Get(embedGrantHeader); token != "" {
		// A grant was PRESENTED. Its verdict is final: refuse rather than fall
		// back to whatever else the request happens to carry.
		//
		// Falling through was the bug: the runtime deliberately keeps sending an
		// expired grant so the server answers 401 instead of silently serving an
		// anonymous render. In a same-site framing the browser also sends the
		// viewer's Strict session cookie, so the fallback answered that expired
		// embed request on the strength of an unrelated logged-in session, the
		// identity confusion the whole feature strips cookies to prevent, and
		// the opposite of the rule Host.Middleware states for the same case.
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

// rejectCrossOrigin returns true (and has already written a 403) when
// the request carries an Origin header whose host differs from r.Host.
// Used by mutating /__gofastr/* endpoints to deny CSRF from
// attacker-controlled origins. Requests without an Origin header are
// allowed through: same-origin XHR / fetch may legitimately omit it,
// and non-browser callers (curl, server-to-server) never set it.
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

// decodeBounded decodes a JSON body capped at maxMutatingBodyBytes. On
// oversize, writes 413 and returns false; on JSON error writes 400 and
// returns false. Used by mutating /__gofastr/* endpoints.
func decodeBounded(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxMutatingBodyBytes)
	// Read the body out first so an oversize read still surfaces as
	// *http.MaxBytesError (io.ReadAll propagates it raw; DecodeStrict
	// wraps decode errors in its own 400 type).
	body, err := io.ReadAll(r.Body)
	if err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			i18n.HTTPError(w, r, http.StatusRequestEntityTooLarge, i18n.StatusPhrase(http.StatusRequestEntityTooLarge))
			return false
		}
		i18n.HTTPError(w, r, http.StatusBadRequest, i18n.ReasonInvalidJSONBody)
		return false
	}
	// handler.UnmarshalStrict: duplicate and case-folded top-level keys
	// decode last-wins under stdlib json, so {"action":"safe",
	// "Action":"danger"} would execute the second action while any
	// first-read intermediary saw the first. Refuse the ambiguity.
	if err := handler.UnmarshalStrict(body, dst); err != nil {
		i18n.HTTPError(w, r, http.StatusBadRequest, i18n.ReasonInvalidJSONBody)
		return false
	}
	return true
}

// handleCreateSession mints a new session, sets the signed token cookie,
// and returns the bare session id. The token itself travels only in the
// Set-Cookie header. The JSON body carries the public id, which is all
// a same-origin caller needs (the cookie rides along automatically).
func (ds *UIHost) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	if rejectCrossOrigin(w, r) {
		return
	}
	// Not from inside a frame.
	//
	// This route mints an anonymous session to anyone who asks; that is its
	// job, since island state needs an id before any user exists. But
	// handleServerAction gates on "has a valid session", so a frame that could
	// mint one would defeat that gate in one extra request: POST here, then
	// POST /__gofastr/action with the cookie. Refusing here is what makes the
	// refusal there mean something.
	//
	// It does not make server actions privileged: any anonymous same-origin
	// caller can still do both requests. It makes the grant confer nothing
	// extra, which is the actual claim.
	if r.Header.Get(fembed.GrantHeader) != "" {
		i18n.HTTPError(w, r, http.StatusForbidden, i18n.StatusPhrase(http.StatusForbidden))
		return
	}
	sess := ds.CreateSession()
	setSessionCookie(w, r, sess.Token)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"sessionId": sess.ID})
}
