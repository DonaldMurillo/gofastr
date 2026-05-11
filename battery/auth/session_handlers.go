package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gofastr/gofastr/core/router"
)

// UserRepository is what the session login flow needs from the host app:
// a way to look up a user by email and verify a candidate password.
//
// The framework's typed repo doesn't itself satisfy this — apps build a
// tiny adapter that pulls the password hash out of their users table and
// calls CheckPassword. See examples/api-tour/auth (if added later) for a
// canonical wiring.
type UserRepository interface {
	FindByEmail(ctx context.Context, email string) (User, string, error) // returns user, password hash, error
}

// ErrInvalidCredentials is returned by UserRepository implementations when
// the email is unknown or the password doesn't match. Handlers translate
// it to 401 without leaking which factor failed.
var ErrInvalidCredentials = errors.New("auth: invalid credentials")

// SessionConfig governs the cookie-bound session flow.
type SessionConfig struct {
	CookieName   string
	CookiePath   string
	CookieSecure bool
	SameSite     http.SameSite
	TTL          time.Duration
}

func (c *SessionConfig) defaults() {
	if c.CookieName == "" {
		c.CookieName = "session_id"
	}
	if c.CookiePath == "" {
		c.CookiePath = "/"
	}
	if c.SameSite == 0 {
		c.SameSite = http.SameSiteLaxMode
	}
	if c.TTL == 0 {
		c.TTL = 7 * 24 * time.Hour
	}
}

// MountAuthRoutes registers POST /auth/login, POST /auth/logout, and
// GET /auth/me on r. login expects JSON {email, password}; logout returns
// 204 and clears the session cookie; me returns the current user as JSON
// (401 if no session).
func MountAuthRoutes(r *router.Router, store SessionStore, repo UserRepository, cfg SessionConfig) {
	cfg.defaults()
	r.Post("/auth/login", loginHandler(store, repo, cfg))
	r.Post("/auth/logout", logoutHandler(store, cfg))
	r.Get("/auth/me", meHandler(store, repo, cfg))
}

func loginHandler(store SessionStore, repo UserRepository, cfg SessionConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeAuthError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if body.Email == "" || body.Password == "" {
			writeAuthError(w, http.StatusBadRequest, "email and password required")
			return
		}
		user, hash, err := repo.FindByEmail(r.Context(), body.Email)
		if errors.Is(err, ErrInvalidCredentials) || err != nil {
			// Never tell the client whether the email exists.
			writeAuthError(w, http.StatusUnauthorized, "invalid credentials")
			return
		}
		if !CheckPassword(body.Password, hash) {
			writeAuthError(w, http.StatusUnauthorized, "invalid credentials")
			return
		}
		sess, err := store.Create(r.Context(), user.GetID(), cfg.TTL)
		if err != nil {
			writeAuthError(w, http.StatusInternalServerError, "session create failed")
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name:     cfg.CookieName,
			Value:    sess.Token,
			Path:     cfg.CookiePath,
			HttpOnly: true,
			Secure:   cfg.CookieSecure,
			SameSite: cfg.SameSite,
			Expires:  sess.ExpiresAt,
		})
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"user": map[string]any{
				"id":    user.GetID(),
				"email": user.GetEmail(),
				"roles": user.GetRoles(),
			},
		})
	}
}

func logoutHandler(store SessionStore, cfg SessionConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(cfg.CookieName); err == nil {
			_ = store.Delete(r.Context(), c.Value)
		}
		// Expire the cookie regardless.
		http.SetCookie(w, &http.Cookie{
			Name:     cfg.CookieName,
			Value:    "",
			Path:     cfg.CookiePath,
			HttpOnly: true,
			Secure:   cfg.CookieSecure,
			SameSite: cfg.SameSite,
			Expires:  time.Unix(0, 0),
			MaxAge:   -1,
		})
		w.WriteHeader(http.StatusNoContent)
	}
}

func meHandler(store SessionStore, repo UserRepository, cfg SessionConfig) http.HandlerFunc {
	_ = repo
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(cfg.CookieName)
		if err != nil {
			writeAuthError(w, http.StatusUnauthorized, "no session")
			return
		}
		sess, err := store.Get(r.Context(), c.Value)
		if err != nil {
			writeAuthError(w, http.StatusUnauthorized, "invalid session")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"userId":    sess.UserID,
			"expiresAt": sess.ExpiresAt,
		})
	}
}

func writeAuthError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{
		"error":   msg,
		"success": false,
	})
}
