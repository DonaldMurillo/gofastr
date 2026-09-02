package webmcp

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"
)

// MarkerHeader is the header the bridge sends on every tool fetch
// ("X-Gofastr-WebMCP: 1"). It attributes a call as agent-driven — for
// audit notes, differentiated rate limits, observers — and never grants
// anything: any page script can send it, so it must never be an
// authorization input.
const MarkerHeader = "X-Gofastr-WebMCP"

// InvocationHeader carries the per-call correlation id on responses
// from observer-instrumented routes.
const InvocationHeader = "X-Gofastr-WebMCP-Invocation"

// HostOption configures a Host at construction (New).
type HostOption func(*Host)

// ToolPhase names which half of a tool's life an event describes:
// registration (a declaration refused by Register/Handle) or invocation
// (a marked call executing through a Handle-registered route).
type ToolPhase string

const (
	PhaseRegister ToolPhase = "register"
	PhaseInvoke   ToolPhase = "invoke"
)

// ToolEvent is the metadata-safe observation the framework can make
// about a tool: identity (name), routing (method, path), outcome
// (status, error class), and cost (duration). It deliberately carries
// NOTHING from the request itself — no input body, no headers, no
// query string, no cookies — because tool inputs are exactly where
// secrets live. Path is the DECLARED tool path, never the request URL.
//
// StatusCode is the HTTP status of the endpoint response (0 when a
// handler panic unwound past the observer). ErrClass is empty on
// success; otherwise a stable token: registration failures use the
// validation branch ("path", "duplicate_name", "after_mount",
// "route_conflict", ...), invocations use "http_<status>" or "panic".
// InvocationID is set on invoke events; see InvocationID.
type ToolEvent struct {
	Phase        ToolPhase
	Name         string
	Method       string
	Path         string
	StatusCode   int
	Duration     time.Duration
	ErrClass     string
	InvocationID string
}

// WithObserver installs fn as the observer for registration failures
// and agent-driven invocations of Handle-registered routes. The last
// option wins. fn runs inline (keep it cheap) and must not panic and
// must not call back into the Host. With no observer installed, Handle
// routes carry no instrumentation and events are never built —
// diagnostics are opt-in and free by default.
func WithObserver(fn func(ToolEvent)) HostOption {
	return func(h *Host) { h.observer = fn }
}

type invocationKey struct{}

// InvocationID returns the WebMCP invocation id carried by the marked
// request's context, or "". Handle-registered routes expose it when an
// observer is installed: the id also rides the response as
// X-Gofastr-WebMCP-Invocation and the observer's ToolEvent, so an app
// can correlate the agent's command with its own downstream events
// (delivery, acknowledgement) by logging it from the handler.
func InvocationID(ctx context.Context) string {
	id, _ := ctx.Value(invocationKey{}).(string)
	return id
}

func randomInvocationID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failure is catastrophic by contract; a constant
		// id would silently CORRELATE unrelated calls, which is worse
		// than a loud panic at first use.
		panic(fmt.Sprintf("webmcp: invocation id: %v", err))
	}
	return hex.EncodeToString(b[:])
}

// emitRegister reports a refused declaration. Called after the Host
// lock is released (an observer must be free to inspect anything), with
// the DECLARED values — developer-authored constants, never request
// data.
func (h *Host) emitRegister(t Tool, class string) {
	if h.observer == nil {
		return
	}
	h.observer(ToolEvent{
		Phase:    PhaseRegister,
		Name:     t.Name,
		Method:   t.Method,
		Path:     t.Path,
		ErrClass: class,
	})
}

// observeHandler wraps one Handle-registered route. Requests without
// the marker header pass straight through with zero added work: an
// ordinary call and an agent call to the same handler stay
// distinguishable (only the agent call is an invocation event), and the
// marker never becomes an authorization input. Marked requests get a
// correlation id (context + response header) and end in one ToolEvent
// carrying the declared path, the response status, the duration, and an
// error class — never the input.
func (h *Host) observeHandler(t Tool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(MarkerHeader) == "" {
			next.ServeHTTP(w, r)
			return
		}
		id := randomInvocationID()
		w.Header().Set(InvocationHeader, id)
		rec := &statusRecorder{ResponseWriter: w}
		req := r.WithContext(context.WithValue(r.Context(), invocationKey{}, id))
		start := time.Now()
		defer func() {
			class := ""
			p := recover()
			if p != nil {
				class = "panic"
			} else if rec.status >= 400 {
				class = "http_" + strconv.Itoa(rec.status)
			}
			h.observer(ToolEvent{
				Phase:        PhaseInvoke,
				Name:         t.Name,
				Method:       t.Method,
				Path:         t.Path,
				StatusCode:   rec.status,
				Duration:     time.Since(start),
				ErrClass:     class,
				InvocationID: id,
			})
			if p != nil {
				// Re-panic so the app's recovery middleware (outer, in
				// the router chain) still owns the response.
				panic(p)
			}
		}()
		next.ServeHTTP(rec, req)
	})
}

// statusRecorder remembers the status the handler wrote so the observer
// can report an outcome. It passes Flush and Hijack through so streaming
// (SSE) and upgrade (WebSocket) tool endpoints keep working under
// instrumentation.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	return s.ResponseWriter.Write(b)
}

func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := s.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, errors.New("webmcp: underlying ResponseWriter does not support hijacking")
}
