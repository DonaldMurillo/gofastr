package rest

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/stream"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control/auth"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control/multiplex"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control/resources"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/internal/ulid"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/session"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/tool/builtins"
)

const maxRESTBody = 4 << 20

const revocationRecheck = 10 * time.Second

// Server is the HTTP+SSE control transport.
type Server struct {
	Mux         *multiplex.Mux
	Catalog     *resources.Catalog
	Encoder     *auth.Encoder
	Revocations *auth.RevocationList
	Features    []string
	// AllowedHosts are the exact Host headers admitted; empty admits any.
	AllowedHosts []string
	// AllowedOrigins are the exact browser Origins admitted; empty admits none.
	AllowedOrigins []string
	// Seats caps the /events streams per token; nil uses control.DefaultSeatTable.
	Seats *control.SeatTable
	// SessionStore, when set, serves GET /v1/sessions?past=true.
	SessionStore session.Store

	handler http.Handler
	recheck time.Duration
}

// Handler returns the routes, built on first call.
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

func (s *Server) seatTable() *control.SeatTable {
	if s.Seats != nil {
		return s.Seats
	}
	return control.DefaultSeatTable()
}

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
			r = r.WithContext(context.WithValue(r.Context(), claimsKey{}, claims))
		}
		inner(w, r)
	}
}

func (s *Server) hostOK(r *http.Request) bool {
	if len(s.AllowedHosts) == 0 {
		return true
	}
	return slices.Contains(s.AllowedHosts, r.Host)
}

func (s *Server) originOK(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	return slices.Contains(s.AllowedOrigins, origin)
}

type claimsKey struct{}

func claimsFrom(r *http.Request) (auth.Claims, bool) {
	c, ok := r.Context().Value(claimsKey{}).(auth.Claims)
	return c, ok
}

func (s *Server) verifyToken(r *http.Request) (auth.Claims, error) {
	tok := r.Header.Get("X-Harness-Token")
	if tok == "" {
		if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
			tok = strings.TrimPrefix(h, "Bearer ")
		}
	}
	if tok == "" {
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
	claims, ok := claimsFrom(r)
	if !ok {
		writeError(w, http.StatusForbidden, "Forbidden", "no token claims")
		return
	}
	if r.URL.Query().Get("past") == "true" && s.SessionStore != nil {
		past, err := s.SessionStore.ListPastSessions(r.Context(), 50, claims.Sessions)
		if err != nil {
			log.Printf("rest: store: %v", err)
			writeError(w, http.StatusInternalServerError, "StoreError", "store operation failed")
			return
		}
		writeJSON(w, http.StatusOK, slices.DeleteFunc(past, func(p session.PastSession) bool {
			return !claims.AllowsSession(p.SessionID)
		}))
		return
	}
	writeJSON(w, http.StatusOK, resources.SessionsAllowed(s.Catalog.ListSessions(), claims))
}

func (s *Server) handleSessionItem(w http.ResponseWriter, r *http.Request) {
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

func (s *Server) handlePOST(w http.ResponseWriter, r *http.Request, sessID ids.SessionID, cmdSeed control.Command) {
	defer r.Body.Close()
	if claims, ok := claimsFrom(r); !ok || !claims.AllowsCommand(cmdSeed.CommandKind()) {
		writeError(w, http.StatusForbidden, "Forbidden", "token not permitted for command "+cmdSeed.CommandKind())
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRESTBody)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "InvalidBody", err.Error())
		return
	}

	var cmd control.Command
	switch cmdSeed.(type) {
	case control.SendInput:
		var c control.SendInput
		if err := handler.UnmarshalStrict(raw, &c); err != nil {
			writeError(w, http.StatusBadRequest, "InvalidBody", err.Error())
			return
		}
		c.SessionID = sessID
		cmd = c
	case control.CancelTurn:
		cmd = control.CancelTurn{SessionID: sessID}
	case control.AnswerPermission:
		var c control.AnswerPermission
		if err := handler.UnmarshalStrict(raw, &c); err != nil {
			writeError(w, http.StatusBadRequest, "InvalidBody", err.Error())
			return
		}
		c.SessionID = sessID
		cmd = c
	case control.SetModel:
		var c control.SetModel
		if err := handler.UnmarshalStrict(raw, &c); err != nil {
			writeError(w, http.StatusBadRequest, "InvalidBody", err.Error())
			return
		}
		c.SessionID = sessID
		cmd = c
	default:
		writeError(w, http.StatusInternalServerError, "InternalError", "unknown command seed")
		return
	}

	claims, _ := claimsFrom(r)
	c := &restClient{id: tokenClientID(claims.JTI), class: claims.IdentityClass}
	if err := s.Mux.Dispatch(r.Context(), c, cmd); err != nil {
		writeError(w, http.StatusConflict, classifyMuxError(err), err.Error())
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) handleTasks(w http.ResponseWriter, _ *http.Request, sessID ids.SessionID) {
	items, updated := builtins.TaskListSnapshot(sessID)
	if items == nil {
		items = []builtins.TaskItem{}
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
	since, resume, ok := lastEventID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "InvalidLastEventID", "")
		return
	}
	claims, _ := claimsFrom(r)
	seat, ok := s.seatTable().AcquireSeat(string(claims.JTI), 0, stream.SeatOverflowRefuse)
	if !ok {
		w.Header().Set("Retry-After", "1")
		writeError(w, http.StatusTooManyRequests, "TooManyStreams",
			"token already holds the maximum number of concurrent event streams")
		return
	}
	defer s.seatTable().ReleaseSeat(seat)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	var ch <-chan control.EventEnvelope
	if resume {
		ch = eng.Bus.SubscribeSince(ctx, since)
	} else {
		ch = eng.Bus.Subscribe(ctx)
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	sse := stream.NewSSEWriter(w)
	recheck := time.NewTicker(cmp.Or(s.recheck, revocationRecheck))
	defer recheck.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-seat.Done:
			return
		case <-recheck.C:
			if _, err := s.verifyToken(r); err != nil {
				return
			}
		case env, ok := <-ch:
			if !ok {
				return
			}
			if _, err := s.verifyToken(r); err != nil {
				return
			}
			body, err := json.Marshal(env)
			if err != nil {
				continue
			}
			if env.ID != 0 {
				sse.SetID(strconv.FormatUint(env.ID, 10))
			}
			if err := sse.WriteEvent(env.Kind, string(body)); err != nil {
				return
			}
		}
	}
}

func lastEventID(r *http.Request) (since uint64, resume, ok bool) {
	raw := r.Header.Get("Last-Event-ID")
	if raw == "" {
		return 0, false, true
	}
	n, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, false, false
	}
	return n, true, true
}

func (s *Server) handleProfiles(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.Catalog.ListProfiles())
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

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	b, err := json.Marshal(body)
	if err != nil {
		log.Printf("harness rest: encode %T: %v", body, err)
		w.WriteHeader(http.StatusInternalServerError)
		//gofastr:allow(rawtext) the control API is an agent contract, writeError's posture: reason is the stable code, message the English detail
		_, _ = w.Write([]byte(`{"reason":"InternalError","message":"the response could not be encoded"}` + "\n"))
		return
	}
	w.WriteHeader(code)
	_, _ = w.Write(append(b, '\n'))
}

//gofastr:allow(rawtext) the control API is an agent contract: reason is its stable code and msg the English detail an agent reads, never a page
func writeError(w http.ResponseWriter, code int, reason, msg string) {
	writeJSON(w, code, control.Error{Reason: reason, Message: msg})
}

func classifyMuxError(err error) string {
	if _, ok := err.(*multiplex.TurnInProgressError); ok {
		return control.ReasonTurnInProgress
	}
	return control.ReasonInvalidCommand
}

func tokenClientID(jti ids.JTI) ids.ClientID {
	if _, u, err := ulid.SplitPrefixed(string(jti)); err == nil {
		return ids.ClientID(ids.PrefixClient + "_" + u.String())
	}
	return ids.ClientID(ids.PrefixClient + "_" + string(jti))
}

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

var _ control.Client = (*restClient)(nil)
