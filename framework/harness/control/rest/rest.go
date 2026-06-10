// Package rest implements the HTTP+SSE control-plane transport.
//
// Routes (per § REST surface):
//
//	GET    /v1/handshake
//	GET    /v1/health
//	GET    /v1/sessions                          (catalog)
//	GET    /v1/sessions/{id}/events  (SSE)       stream
//	POST   /v1/sessions/{id}/input               SendInput
//	POST   /v1/sessions/{id}/cancel              CancelTurn
//	POST   /v1/sessions/{id}/permission          AnswerPermission
//	POST   /v1/sessions/{id}/model               SetModel
//	GET    /v1/profiles                          (catalog)
//	GET    /v1/providers                         (catalog)
//	GET    /v1/tools                             (catalog)
//	GET    /v1/skills                            (catalog)
//	GET    /v1/slash-commands                    (catalog)
//	GET    /v1/auth/token                        token mint (Unix-socket-only)
//
// Security per § Threat model:
//
//   - Token via X-Harness-Token header (anti-DNS-rebinding).
//   - Host header must match localhost / 127.0.0.1 / configured.
//   - Origin header must be in the allowlist for non-curl callers.
package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/control/auth"
	"github.com/DonaldMurillo/gofastr/framework/harness/control/multiplex"
	"github.com/DonaldMurillo/gofastr/framework/harness/control/resources"
	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/harness/session"
	"github.com/DonaldMurillo/gofastr/framework/harness/tool/builtins"
)

// Server is the REST transport.
type Server struct {
	Mux            *multiplex.Mux
	Catalog        *resources.Catalog
	Encoder        *auth.Encoder
	Revocations    *auth.RevocationList
	Features       []string
	AllowedHosts   []string // exact-match Host headers permitted (e.g., "127.0.0.1:8421")
	AllowedOrigins []string // exact-match Origin headers permitted

	// SessionStore, when non-nil, backs the ?past=true query on
	// /v1/sessions to surface historical sessions from disk.
	SessionStore session.Store

	// Handler caches the constructed mux.
	handler http.Handler
}

// Handler returns the http.Handler. Lazy-built on first call.
func (s *Server) Handler() http.Handler {
	if s.handler != nil {
		return s.handler
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/handshake", s.handle(s.handleHandshake, false))
	mux.HandleFunc("/v1/health", s.handle(s.handleHealth, false))
	mux.HandleFunc("/v1/sessions", s.handle(s.handleSessions, true))
	mux.HandleFunc("/v1/sessions/", s.handle(s.handleSessionItem, true))
	mux.HandleFunc("/v1/profiles", s.handle(s.handleProfiles, true))
	mux.HandleFunc("/v1/providers", s.handle(s.handleProviders, true))
	mux.HandleFunc("/v1/tools", s.handle(s.handleTools, true))
	mux.HandleFunc("/v1/skills", s.handle(s.handleSkills, true))
	mux.HandleFunc("/v1/slash-commands", s.handle(s.handleSlashCommands, true))
	s.handler = mux
	return mux
}

// handle is the request wrapper: Host/Origin checks + optional token verification.
func (s *Server) handle(inner http.HandlerFunc, requireToken bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.hostOK(r) {
			writeError(w, http.StatusForbidden, "InvalidHost", "Host header not in allowlist")
			return
		}
		if !s.originOK(r) {
			writeError(w, http.StatusForbidden, "InvalidOrigin", "Origin header not in allowlist")
			return
		}
		if requireToken {
			claims, err := s.verifyToken(r)
			if err != nil {
				writeError(w, http.StatusUnauthorized, control.ReasonTokenExpired, err.Error())
				return
			}
			// Stash the verified claims so per-session handlers can
			// enforce the token's session/command scope (mirrors ws.go).
			r = r.WithContext(context.WithValue(r.Context(), claimsKey{}, claims))
		}
		inner(w, r)
	}
}

func (s *Server) hostOK(r *http.Request) bool {
	if len(s.AllowedHosts) == 0 {
		// No restriction configured — typical for Unix socket.
		return true
	}
	for _, h := range s.AllowedHosts {
		if r.Host == h {
			return true
		}
	}
	return false
}

func (s *Server) originOK(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		// curl-style callers without Origin are accepted.
		return true
	}
	for _, o := range s.AllowedOrigins {
		if origin == o {
			return true
		}
	}
	return len(s.AllowedOrigins) == 0
}

// claimsKey is the context key under which verified token claims are
// stashed by handle() for downstream scope enforcement.
type claimsKey struct{}

// claimsFrom returns the verified claims stashed by handle(). The
// second result is false when the route did not require a token.
func claimsFrom(r *http.Request) (auth.Claims, bool) {
	c, ok := r.Context().Value(claimsKey{}).(auth.Claims)
	return c, ok
}

func (s *Server) verifyToken(r *http.Request) (auth.Claims, error) {
	tok := r.Header.Get("X-Harness-Token")
	if tok == "" {
		// Authorization: Bearer <token> fallback — common header.
		if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
			tok = strings.TrimPrefix(h, "Bearer ")
		}
	}
	if tok == "" {
		// Query-param fallback. EventSource (SSE) can't set custom
		// headers in browsers, so we accept ?token=... for GET-style
		// long-lived connections. POSTs should still prefer headers.
		tok = r.URL.Query().Get("token")
	}
	if tok == "" {
		return auth.Claims{}, fmt.Errorf("missing token (X-Harness-Token header or ?token=)")
	}
	if s.Encoder == nil {
		return auth.Claims{}, fmt.Errorf("server has no encoder configured")
	}
	return auth.Verify(s.Encoder, s.Revocations, tok, time.Now())
}

// ---------- Handlers ----------

func (s *Server) handleHandshake(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.Catalog.Handshake(s.Features))
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"version": control.ProtocolVersion,
	})
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "MethodNotAllowed", "")
		return
	}
	// ?past=true returns historical sessions from the on-disk log
	// (useful for the sidebar to show prior conversations); default
	// returns just the active in-memory sessions.
	if r.URL.Query().Get("past") == "true" && s.SessionStore != nil {
		past, err := s.SessionStore.ListPastSessions(r.Context(), 50)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "StoreError", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, past)
		return
	}
	writeJSON(w, http.StatusOK, s.Catalog.ListSessions())
}

// handleSessionItem handles /v1/sessions/{id}/<verb>.
func (s *Server) handleSessionItem(w http.ResponseWriter, r *http.Request) {
	// Path: /v1/sessions/{id}/...
	rest := strings.TrimPrefix(r.URL.Path, "/v1/sessions/")
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusBadRequest, "InvalidPath", "missing session ID")
		return
	}
	sessID, err := ids.ParseSession(parts[0])
	if err != nil {
		writeError(w, http.StatusBadRequest, "InvalidSessionID", err.Error())
		return
	}
	// Enforce the token's session scope before touching the session
	// at all — a token bound to session A must not drive session B,
	// nor read its event stream/tasks. Mirrors ws.go:AllowsSession.
	claims, ok := claimsFrom(r)
	if !ok || !claims.AllowsSession(sessID) {
		writeError(w, http.StatusForbidden, "Forbidden", "token not bound to session")
		return
	}
	verb := ""
	if len(parts) > 1 {
		verb = parts[1]
	}
	switch {
	case verb == "" && r.Method == http.MethodGet:
		// Session meta — minimal for v0.1: just whatever the catalog
		// already exposes for the matching SessionID.
		for _, si := range s.Catalog.ListSessions() {
			if si.SessionID == sessID {
				writeJSON(w, http.StatusOK, si)
				return
			}
		}
		writeError(w, http.StatusNotFound, "UnknownSession", "")
	case verb == "events" && r.Method == http.MethodGet:
		s.handleSSE(w, r, sessID)
	case verb == "input" && r.Method == http.MethodPost:
		s.handlePOST(w, r, sessID, control.SendInput{})
	case verb == "cancel" && r.Method == http.MethodPost:
		s.handlePOST(w, r, sessID, control.CancelTurn{})
	case verb == "permission" && r.Method == http.MethodPost:
		s.handlePOST(w, r, sessID, control.AnswerPermission{})
	case verb == "model" && r.Method == http.MethodPost:
		s.handlePOST(w, r, sessID, control.SetModel{})
	case verb == "tasks" && r.Method == http.MethodGet:
		s.handleTasks(w, r, sessID)
	default:
		writeError(w, http.StatusNotFound, "UnknownRoute", r.Method+" "+r.URL.Path)
	}
}

// handlePOST decodes the typed command body, dispatches via the mux.
// cmdSeed is a zero-value of the expected type used to discriminate.
func (s *Server) handlePOST(w http.ResponseWriter, r *http.Request, sessID ids.SessionID, cmdSeed control.Command) {
	defer r.Body.Close()
	// Enforce the token's command scope before decoding/dispatching —
	// a token scoped to SendInput must not answer permission prompts.
	// Session scope was already checked in handleSessionItem.
	if claims, ok := claimsFrom(r); !ok || !claims.AllowsCommand(cmdSeed.CommandKind()) {
		writeError(w, http.StatusForbidden, "Forbidden", "token not permitted for command "+cmdSeed.CommandKind())
		return
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	var cmd control.Command
	switch cmdSeed.(type) {
	case control.SendInput:
		var c control.SendInput
		if err := dec.Decode(&c); err != nil {
			writeError(w, http.StatusBadRequest, "InvalidBody", err.Error())
			return
		}
		c.SessionID = sessID
		cmd = c
	case control.CancelTurn:
		cmd = control.CancelTurn{SessionID: sessID}
	case control.AnswerPermission:
		var c control.AnswerPermission
		if err := dec.Decode(&c); err != nil {
			writeError(w, http.StatusBadRequest, "InvalidBody", err.Error())
			return
		}
		c.SessionID = sessID
		cmd = c
	case control.SetModel:
		var c control.SetModel
		if err := dec.Decode(&c); err != nil {
			writeError(w, http.StatusBadRequest, "InvalidBody", err.Error())
			return
		}
		c.SessionID = sessID
		cmd = c
	default:
		writeError(w, http.StatusInternalServerError, "InternalError", "unknown command seed")
		return
	}

	// Synthesize an ephemeral Client for the dispatch. For v0.1 the
	// REST transport's Client is HTTP-shaped — request-scoped, no
	// persistent ID. In v0.2 we'll persist a per-token attach so
	// originator IDs are stable across HTTP requests.
	c := &restClient{id: ids.NewClientID(), class: control.IdentityHuman}
	if err := s.Mux.Dispatch(r.Context(), c, cmd); err != nil {
		writeError(w, http.StatusConflict, classifyMuxError(err), err.Error())
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// handleSSE streams events for the session via the SSE protocol.
// handleTasks returns the current TaskList snapshot for a session.
// Read-only — the model writes via the TaskList tool dispatch, and
// the snapshot is kept by the tool. Clients (web sidebar, TUI panel,
// external dashboards) poll this endpoint to render the plan.
func (s *Server) handleTasks(w http.ResponseWriter, _ *http.Request, sessID ids.SessionID) {
	items, updated := builtins.TaskListSnapshot(sessID)
	if items == nil {
		items = []builtins.TaskItem{} // serialize as [] not null
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tasks":   items,
		"updated": updated,
	})
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request, sessID ids.SessionID) {
	eng := s.Mux.EngineFor(sessID)
	if eng == nil {
		writeError(w, http.StatusNotFound, "UnknownSession", "")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "NoFlusher", "")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	ch := eng.Bus.Subscribe(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case env, ok := <-ch:
			if !ok {
				return
			}
			body, err := json.Marshal(env)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", env.ID, env.Kind, body)
			flusher.Flush()
		}
	}
}

// Catalog endpoints — read-only.

func (s *Server) handleProfiles(w http.ResponseWriter, _ *http.Request) {
	// v0.1 ships two presets shipped in profile/; their names are exposed here.
	writeJSON(w, http.StatusOK, []string{"framework", "default"})
}

func (s *Server) handleProviders(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Catalog.ListProviders(r.Context()))
}

func (s *Server) handleTools(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.Catalog.ListTools())
}

func (s *Server) handleSkills(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.Catalog.ListSkills())
}

func (s *Server) handleSlashCommands(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.Catalog.ListSlashCommands())
}

// ---------- helpers ----------

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, code int, reason, msg string) {
	writeJSON(w, code, control.Error{Reason: reason, Message: msg})
}

// classifyMuxError maps multiplex error types to canonical Reason codes.
func classifyMuxError(err error) string {
	if tip, ok := err.(*multiplex.TurnInProgressError); ok {
		_ = tip
		return control.ReasonTurnInProgress
	}
	return control.ReasonInvalidCommand
}

// restClient is the per-HTTP-request synthesized Client. For v0.1
// it's request-scoped and doesn't subscribe to events directly —
// callers stream via /events SSE.
type restClient struct {
	id    ids.ClientID
	class control.IdentityClass
}

func (c *restClient) ID() ids.ClientID                     { return c.id }
func (c *restClient) IdentityClass() control.IdentityClass { return c.class }
func (c *restClient) Subscribe(_ context.Context) <-chan control.EventEnvelope {
	ch := make(chan control.EventEnvelope)
	close(ch)
	return ch
}
func (c *restClient) Send(_ context.Context, _ control.Command) error { return nil }
func (c *restClient) Close() error                                    { return nil }

// Compile-time assertion.
var _ control.Client = (*restClient)(nil)
var _ = bytes.NewReader // keep stdlib import that may be used by tests
