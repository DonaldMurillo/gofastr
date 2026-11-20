package webmcp

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"
)

// MarkerHeader is sent by the bridge on every tool call; it attributes a
// call and never authorizes one.
const MarkerHeader = "X-Gofastr-WebMCP"

// InvocationHeader carries the invocation id on observed responses.
const InvocationHeader = "X-Gofastr-WebMCP-Invocation"

// HostOption configures a Host at New.
type HostOption func(*Host)

// ToolPhase says whether an event is a refused declaration or an invocation.
type ToolPhase string

// Tool event phases.
const (
	PhaseRegister ToolPhase = "register"
	PhaseInvoke   ToolPhase = "invoke"
)

// ToolEvent describes one refused declaration or one marked invocation by
// name, declared path, status, duration and error class, never by input.
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

// WithObserver installs fn to receive ToolEvents inline; it must not panic
// or call back into the Host.
func WithObserver(fn func(ToolEvent)) HostOption {
	return func(h *Host) { h.observer = fn }
}

type invocationKey struct{}

// InvocationID returns the invocation id on an observed marked request, or "".
func InvocationID(ctx context.Context) string {
	id, _ := ctx.Value(invocationKey{}).(string)
	return id
}

func randomInvocationID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("webmcp: invocation id: %v", err))
	}
	return hex.EncodeToString(b[:])
}

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
				panic(p)
			}
		}()
		next.ServeHTTP(rec, req)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.status == 0 && !(code >= 100 && code < 200 && code != http.StatusSwitchingProtocols) {
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

func (s *statusRecorder) ReadFrom(src io.Reader) (int64, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	return io.Copy(s.ResponseWriter, src)
}

func (s *statusRecorder) Flush() {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

func (s *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := s.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, errors.New("webmcp: underlying ResponseWriter does not support hijacking")
}
